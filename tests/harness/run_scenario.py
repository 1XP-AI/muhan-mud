#!/usr/bin/env python3
"""Deterministic black-box scenario runner for the legacy C MUD.

The runner deliberately uses only the Python standard library.  It starts one
server process per run, gives it a disposable MUHAN_HOME, and synchronizes on
server prompts instead of sleeping for a guessed amount of time.

The current C build still has a few legacy absolute paths (/home/muhan).  A
Linux container may pass --allow-legacy-root-link to make that exact path point
at the disposable fixture for the duration of this process.  The runner will
never replace an existing path.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
from typing import Any, Dict, List, Optional
import select


DEFAULT_SCENARIO = Path(__file__).resolve().parents[1] / "scenarios" / "create_and_relogin.json"
DEFAULT_TIMEOUT = 12.0
LEGACY_ROOT = Path("/home/muhan")


class ScenarioFailure(RuntimeError):
    pass


def now_ms() -> int:
    return int(time.time() * 1000)


def decode_text(data: bytes) -> str:
    return data.decode("utf-8", errors="replace")


REDACTED = "<REDACTED>"
REDACTED_BYTES = REDACTED.encode("utf-8")


def redact_text(value: str, secret: str) -> str:
    """Redact every occurrence, including secrets embedded in diagnostics."""
    if not secret:
        return value
    return value.replace(secret, REDACTED)


def redact_bytes(value: bytes, secret: str) -> bytes:
    """Byte-level counterpart used before anything enters a transcript/log."""
    if not secret:
        return value
    return value.replace(secret.encode("utf-8"), REDACTED_BYTES)


def redact_json(value: Any, secret: str) -> Any:
    """Recursively scrub JSON-compatible values at the artifact boundary."""
    if isinstance(value, str):
        return redact_text(value, secret)
    if isinstance(value, list):
        return [redact_json(item, secret) for item in value]
    if isinstance(value, dict):
        return {redact_text(str(key), secret): redact_json(item, secret) for key, item in value.items()}
    return value


def assert_artifact_safe(value: Dict[str, Any], secret: str) -> None:
    """Fail closed if a secret reaches any uploaded JSON or encoded stream."""
    if not secret:
        return
    serialized = json.dumps(value, ensure_ascii=False, sort_keys=True)
    secret_bytes = secret.encode("utf-8")
    assert secret not in serialized
    assert secret_bytes not in serialized.encode("utf-8")
    if secret in serialized or secret_bytes in serialized.encode("utf-8"):
        raise ScenarioFailure("artifact redaction self-check failed")
    def check_encoded(item: Any, location: str) -> None:
        if isinstance(item, dict):
            for key, child in item.items():
                child_location = f"{location}.{key}"
                if str(key).endswith("_b64") and isinstance(child, str):
                    try:
                        decoded = base64.b64decode(child, validate=True)
                    except (ValueError, UnicodeError):
                        decoded = b""
                    assert secret_bytes not in decoded
                    if secret_bytes in decoded:
                        raise ScenarioFailure(f"artifact redaction self-check failed for {child_location}")
                check_encoded(child, child_location)
        elif isinstance(item, list):
            for index, child in enumerate(item):
                check_encoded(child, f"{location}[{index}]")

    check_encoded(value, "result")


def redact_file(path: Path, secret: str) -> None:
    """Scrub disposable server stderr/stdout before KEEP_TEST_FIXTURE exposes it."""
    if not secret or not path.exists():
        return
    data = path.read_bytes()
    scrubbed = redact_bytes(data, secret)
    if scrubbed != data:
        path.write_bytes(scrubbed)


class Transcript:
    def __init__(self, password: str) -> None:
        self.password = password
        self.events: List[Dict[str, Any]] = []
        self.server_bytes = bytearray()
        self._redaction_tail = b""
        self._redaction_tail_session = 0

    def input(self, session: int, label: str, value: str) -> None:
        self.events.append(
            {
                "direction": "client",
                "session": session,
                "label": redact_text(label, self.password),
                "value": redact_text(value, self.password),
            }
        )

    def output(self, session: int, data: bytes) -> None:
        self._redaction_tail_session = session
        if self.password:
            combined = self._redaction_tail + data
            secret_bytes = self.password.encode("utf-8")
            scrubbed = redact_bytes(combined, self.password)
            hold = 0
            for size in range(min(len(secret_bytes) - 1, len(combined)), 0, -1):
                if combined[-size:] == secret_bytes[:size]:
                    hold = size
                    break
            emit = scrubbed[:-hold] if hold else scrubbed
            self._redaction_tail = combined[-hold:] if hold else b""
            data = emit
        if not data:
            return
        self.server_bytes.extend(data)
        self.events.append(
            {
                "direction": "server",
                "session": session,
                "raw_b64": base64.b64encode(data).decode("ascii"),
                "text": decode_text(data),
            }
        )

    def as_json(self) -> Dict[str, Any]:
        if self._redaction_tail:
            tail = redact_bytes(self._redaction_tail, self.password)
            self.server_bytes.extend(tail)
            self.events.append(
                {
                    "direction": "server",
                    "session": self._redaction_tail_session,
                    "raw_b64": base64.b64encode(tail).decode("ascii"),
                    "text": decode_text(tail),
                }
            )
            self._redaction_tail = b""
        return {
            "events": self.events,
            # This is the concatenated server-output stream after centralized
            # secret redaction; passwords are never uploaded in raw form.
            "raw_transcript_b64": base64.b64encode(self.server_bytes).decode("ascii"),
        }


class Session:
    def __init__(self, sock: socket.socket, transcript: Transcript, number: int, timeout: float) -> None:
        self.sock = sock
        self.transcript = transcript
        self.number = number
        self.timeout = timeout
        self.pending = bytearray()

    def send_line(self, value: str, label: Optional[str] = None) -> None:
        payload = value.encode("utf-8") + b"\n"
        self.sock.sendall(payload)
        self.transcript.input(self.number, label or "line", value)

    def enter(self) -> None:
        self.send_line("", "enter")

    def read_until(self, marker: str, label: str) -> bytes:
        expected = marker.encode("utf-8")
        deadline = time.monotonic() + self.timeout
        while True:
            found = self.pending.find(expected)
            if found >= 0:
                end = found + len(expected)
                result = bytes(self.pending[:end])
                del self.pending[:end]
                return result

            remaining = deadline - time.monotonic()
            if remaining <= 0:
                visible = redact_text(decode_text(bytes(self.pending[-1_000:])), self.transcript.password)
                raise ScenarioFailure(
                    f"session {self.number}: timed out waiting for {label!r} ({marker!r}); "
                    f"pending tail={visible!r}"
                )

            readable, _, _ = select.select([self.sock], [], [], min(remaining, 0.25))
            if not readable:
                continue
            try:
                chunk = self.sock.recv(4096)
            except socket.timeout:
                continue
            if not chunk:
                raise ScenarioFailure(
                    f"session {self.number}: server closed connection while waiting for {label!r}"
                )
            self.pending.extend(chunk)
            self.transcript.output(self.number, chunk)

    def read_until_close(self, label: str) -> bytes:
        """Drain this session until the peer closes, bounded by the test timeout."""
        drained = bytearray(self.pending)
        self.pending.clear()
        deadline = time.monotonic() + self.timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ScenarioFailure(f"session {self.number}: timed out waiting for {label!r}")
            readable, _, _ = select.select([self.sock], [], [], min(remaining, 0.25))
            if not readable:
                continue
            try:
                chunk = self.sock.recv(4096)
            except socket.timeout:
                continue
            if not chunk:
                return bytes(drained)
            drained.extend(chunk)
            self.transcript.output(self.number, chunk)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenario", type=Path, default=DEFAULT_SCENARIO)
    parser.add_argument(
        "--binary",
        type=Path,
        default=None,
        help="external frp.new path; requires AI_SCENARIO_ALLOW_EXTERNAL_BINARY=1",
    )
    parser.add_argument("--repo-root", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=None, help="JSON result path; stdout is always a compact summary")
    parser.add_argument("--fixture", type=Path, default=None, help="parent directory for the disposable fixture")
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT)
    parser.add_argument("--epoch", type=int, default=1_735_689_600, help="scenario metadata clock value")
    parser.add_argument("--seed", type=int, default=424242, help="scenario metadata RNG seed")
    parser.add_argument(
        "--allow-legacy-root-link",
        action="store_true",
        help="in an isolated container, create /home/muhan -> fixture (only if absent)",
    )
    return parser.parse_args()


def find_repo_root(argument: Optional[Path]) -> Path:
    if argument is not None:
        return argument.resolve()
    return Path(__file__).resolve().parents[2]


def build_current_binary(repo_root: Path) -> Path:
    """Build the executable inside the runner so current-source is not self-attested."""
    configured = os.environ.get("AI_SCENARIO_BINARY", "")
    binary = (
        Path(configured).expanduser().resolve()
        if configured
        else Path(tempfile.gettempdir()).resolve() / f"muhan-ai-frp-{os.getpid()}"
    )
    binary.parent.mkdir(parents=True, exist_ok=True)
    completed = subprocess.run(
        [
            "make",
            "-B",
            "-C",
            str(repo_root / "src"),
            "-j2",
            f"OUTFILE={binary}",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
    )
    if completed.returncode != 0:
        build_log = decode_text(completed.stdout)
        if len(build_log) > 12_000:
            build_log = build_log[-12_000:]
        raise ScenarioFailure(
            f"current-source C build failed with exit {completed.returncode}:\n{build_log}"
        )
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ScenarioFailure(f"current-source build did not produce an executable: {binary}")
    return binary


def load_scenario(path: Path) -> Dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ScenarioFailure(f"cannot read scenario {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ScenarioFailure("scenario root must be a JSON object")
    required = ("name", "player_name", "password")
    missing = [key for key in required if not isinstance(value.get(key), str) or not value[key]]
    if missing:
        raise ScenarioFailure(f"scenario is missing non-empty string fields: {', '.join(missing)}")
    return value


def copy_tree(source: Path, destination: Path) -> None:
    if not source.exists():
        return
    shutil.copytree(source, destination, symlinks=True)


def build_fixture(repo_root: Path, fixture_parent: Optional[Path]) -> Path:
    if fixture_parent is None:
        # The legacy C player struct has an 80-byte temporary path field.  A
        # short Unix temp root keeps resolved help/resource paths within that
        # field even on macOS, whose default tempfile directory is very long.
        short_tmp = Path("/tmp")
        if short_tmp.is_dir():
            fixture = Path(tempfile.mkdtemp(prefix="muhan-ai-", dir=str(short_tmp)))
        else:
            fixture = Path(tempfile.mkdtemp(prefix="muhan-ai-"))
    else:
        fixture_parent.mkdir(parents=True, exist_ok=True)
        fixture = Path(tempfile.mkdtemp(prefix="muhan-ai-", dir=str(fixture_parent)))

    # Resources are copied into the fixture so a test can never mutate the
    # checkout through a symlink.  The manifest trees are read-only inputs and
    # can be linked to avoid another 25MB copy.
    for directory in ("rooms", "objmon", "help", "post", "log", "bin"):
        copy_tree(repo_root / directory, fixture / directory)
        (fixture / directory).mkdir(parents=True, exist_ok=True)

    copy_tree(repo_root / "resources_utf8" / "player", fixture / "player")
    (fixture / "player").mkdir(parents=True, exist_ok=True)
    for subdir in ("alias", "bank", "simul", "suic", "fal", "invite", "vote", "marriage", "family"):
        (fixture / "player" / subdir).mkdir(parents=True, exist_ok=True)
    (fixture / "log" / "auth").mkdir(parents=True, exist_ok=True)
    (fixture / "bin").mkdir(parents=True, exist_ok=True)

    for directory in ("resources_utf8", "resources_manifest"):
        source = repo_root / directory
        target = fixture / directory
        if source.exists():
            target.symlink_to(source, target_is_directory=True)
    return fixture


def install_legacy_root(fixture: Path, enabled: bool) -> bool:
    """Point hard-coded legacy paths at fixture, never replacing a path."""
    try:
        if LEGACY_ROOT.is_symlink():
            if LEGACY_ROOT.resolve() != fixture.resolve():
                raise ScenarioFailure(
                    f"refusing to use existing {LEGACY_ROOT}; it does not point at fixture {fixture}"
                )
            return False
        if LEGACY_ROOT.exists():
            raise ScenarioFailure(
                f"{LEGACY_ROOT} already exists; run in an isolated container or use a binary built with a runtime root"
            )
        if not enabled:
            # A runtime-root-aware build only needs MUHAN_HOME.  Older builds
            # can still be exercised in a container by opting into the exact
            # legacy path link below; do not create anything on a developer's
            # host implicitly.
            return False
        LEGACY_ROOT.parent.mkdir(parents=True, exist_ok=True)
        LEGACY_ROOT.symlink_to(fixture, target_is_directory=True)
        return True
    except OSError as exc:
        raise ScenarioFailure(f"cannot prepare disposable legacy root {LEGACY_ROOT}: {exc}") from exc


def remove_legacy_root(created: bool) -> None:
    if not created:
        return
    try:
        if LEGACY_ROOT.is_symlink():
            LEGACY_ROOT.unlink()
    except OSError:
        # Preserve the original failure; diagnostics will tell the operator
        # that cleanup needs attention.
        pass


def choose_port() -> int:
    probe = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        probe.bind(("127.0.0.1", 0))
        return int(probe.getsockname()[1])
    finally:
        probe.close()


def connect_first(port: int, timeout: float, process: subprocess.Popen[bytes]) -> socket.socket:
    deadline = time.monotonic() + timeout
    last_error: Optional[BaseException] = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise ScenarioFailure(f"server exited before accepting a connection (returncode={process.returncode})")
        try:
            sock = socket.create_connection(("127.0.0.1", port), timeout=0.5)
            sock.setblocking(False)
            return sock
        except OSError as exc:
            last_error = exc
            select.select([], [], [], 0.05)
    raise ScenarioFailure(f"server port {port} was not ready: {last_error}")


def expected_player_path(fixture: Path, name: str) -> Path:
    shard = hashlib.sha1(name.encode("utf-8")).hexdigest()[:2]
    return fixture / "player" / shard / name


def assert_file(path: Path) -> Dict[str, Any]:
    if not path.is_file():
        raise ScenarioFailure(f"expected saved player file does not exist: {path}")
    data = path.read_bytes()
    if not data:
        raise ScenarioFailure(f"expected saved player file is empty: {path}")
    return {
        "path": str(path.relative_to(path.parents[2])),
        "size": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
    }


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def run_game(
    binary: Path,
    fixture: Path,
    scenario: Dict[str, Any],
    transcript: Transcript,
    timeout: float,
    epoch: int,
    seed: int,
    server_log_path: Path,
) -> Dict[str, Any]:
    port = choose_port()
    env = os.environ.copy()
    env["MUHAN_HOME"] = str(fixture)
    env["MUD_PORT"] = str(port)
    env["PYTHONHASHSEED"] = "0"
    env["TZ"] = "UTC"
    env["LC_ALL"] = "C.UTF-8"
    env["SOURCE_DATE_EPOCH"] = str(epoch)
    # These metadata variables are intentionally harmless to the current
    # binary and provide a stable contract for a future clock/RNG seam.
    env["MUHAN_TEST_EPOCH"] = str(epoch)
    env["MUHAN_TEST_SEED"] = str(seed)

    with server_log_path.open("wb") as server_log:
        try:
            process = subprocess.Popen(
                [str(binary), "-r", str(port)],
                cwd=str(fixture),
                env=env,
                stdout=server_log,
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )
        except OSError as exc:
            raise ScenarioFailure(f"cannot start C MUD binary {binary}: {exc}") from exc

    sessions: List[Session] = []
    try:
        commands = scenario.get("commands", ["?", "건강", "저장", "끝"])
        if not isinstance(commands, list) or len(commands) < 4 or not all(isinstance(item, str) and item for item in commands[:4]):
            raise ScenarioFailure("scenario commands must contain four non-empty strings")

        first_sock = connect_first(port, timeout, process)
        first = Session(first_sock, transcript, 1, timeout)
        sessions.append(first)
        first.read_until("[엔터]를 누르세요.", "initial enter prompt")
        first.enter()
        first.read_until("당신의 이름은 무엇입니까?", "name prompt")
        first.send_line(scenario["player_name"], "player name")
        first.read_until("하시겠습니까", "create confirmation")
        first.send_line("y", "create confirmation answer")
        first.read_until("[엔터]를 누르십시요.", "creation enter prompt")
        first.enter()
        first.read_until("당신은 남자입니까", "gender prompt")
        first.send_line(scenario.get("gender", "남"), "gender")
        first.read_until("직업을 고르세요", "class prompt")
        first.send_line(scenario.get("class", "4"), "class")
        first.read_until("능력:", "stats prompt")
        first.send_line(scenario.get("stats", "12 10 12 10 10"), "stats")
        first.read_until("익숙한 무기를", "weapon prompt")
        first.send_line(scenario.get("weapon", "1"), "weapon")
        first.read_until("성향을 고르십시요", "alignment prompt")
        first.send_line(scenario.get("alignment", "선"), "alignment")
        first.read_until("종족", "race prompt")
        first.send_line(scenario.get("race", "7"), "race")
        first.read_until("새 암호를", "password prompt")
        first.send_line(scenario["password"], "password")
        first.read_until("레벨 5", "creation welcome")
        first.read_until("도력): ", "command prompt after creation")

        first.send_line(commands[0], "command")
        first.read_until("명령어 도움말", "help response")
        first.read_until("그만보시려면", "help pagination prompt")
        first.send_line(".", "help pagination stop")
        first.read_until("도력): ", "help command completion")
        first.send_line(commands[1], "command")
        first.read_until("체력", "health response")
        first.read_until("도력): ", "health command completion")
        first.send_line(commands[2], "command")
        first.read_until("저장하였습니다", "save response")
        first.read_until("도력): ", "save command completion")
        first.send_line(commands[3], "command")
        first.read_until("안녕히", "quit response")
        first.read_until_close("quit connection close")
        first.sock.close()

        saved = expected_player_path(fixture, scenario["player_name"])
        file_info = assert_file(saved)

        second_sock = connect_first(port, timeout, process)
        second = Session(second_sock, transcript, 2, timeout)
        sessions.append(second)
        second.read_until("[엔터]를 누르세요.", "relogin enter prompt")
        second.enter()
        second.read_until("당신의 이름은 무엇입니까?", "relogin name prompt")
        second.send_line(scenario["player_name"], "relogin player name")
        second.read_until("암호", "relogin password prompt")
        second.send_line(scenario["password"], "relogin password")
        second.read_until("체력", "relogin health state")
        second.send_line(commands[3], "relogin command")
        second.read_until("안녕히", "relogin quit response")
        second.read_until_close("relogin connection close")
        second.sock.close()

        # A truncated player record must be rejected as corrupt data.  This is
        # intentionally done only inside the disposable fixture, after the
        # successful create/save/relogin assertions above.
        third_sock = connect_first(port, timeout, process)
        third = Session(third_sock, transcript, 3, timeout)
        sessions.append(third)
        third.read_until("[엔터]를 누르세요.", "corrupt relogin enter prompt")
        saved.write_bytes(b"")
        third.enter()
        third.read_until("당신의 이름은 무엇입니까?", "corrupt relogin name prompt")
        third.send_line(scenario["player_name"], "corrupt relogin player name")
        corrupt_response = third.read_until(
            "캐릭터 데이터를 읽을 수 없습니다", "corrupt player rejection"
        )
        corrupt_tail = third.read_until_close("corrupt relogin connection close")
        if "하시겠습니까" in decode_text(corrupt_response + corrupt_tail):
            raise ScenarioFailure("corrupt player was offered new-character creation")
        third.sock.close()

        return {"port": port, "saved_player": file_info, "corrupt_player_rejected": True}
    finally:
        for session in sessions:
            try:
                session.sock.close()
            except OSError:
                pass
        if process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        try:
            process.wait(timeout=2.0)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=2.0)


def server_diagnostics(path: Path, secret: str) -> str:
    try:
        text = decode_text(redact_bytes(path.read_bytes(), secret))
    except OSError as exc:
        return f"<cannot read server log: {exc}>"
    if len(text) > 12_000:
        text = text[-12_000:]
    return text


def write_result(path: Optional[Path], result: Dict[str, Any]) -> None:
    if path is None:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    args = parse_args()
    started = now_ms()
    scenario: Dict[str, Any] = {}
    transcript = Transcript("")
    output_path: Optional[Path] = args.output
    fixture: Optional[Path] = None
    legacy_created = False
    server_log_path: Optional[Path] = None
    result: Dict[str, Any] = {
        "schema": 1,
        "status": "failed",
        "started_at_ms": started,
        "seed": args.seed,
        "epoch": args.epoch,
        "corrupt_player_rejected": False,
    }

    try:
        repo_root = find_repo_root(args.repo_root)
        scenario = load_scenario(args.scenario.resolve())
        transcript = Transcript(scenario["password"])
        result.update({"scenario": scenario["name"], "player_name": scenario["player_name"]})
        if output_path is None:
            output_path = Path(
                os.environ.get(
                    "AI_SCENARIO_OUTPUT",
                    f"/tmp/muhan-ai-{scenario['name']}-{os.getpid()}.json",
                )
            )
        external_value = args.binary or (
            Path(os.environ["FRP_BIN"]) if os.environ.get("FRP_BIN") else None
        )
        if external_value is not None:
            if os.environ.get("AI_SCENARIO_ALLOW_EXTERNAL_BINARY", "0") != "1":
                raise ScenarioFailure(
                    "external binary execution requires AI_SCENARIO_ALLOW_EXTERNAL_BINARY=1"
                )
            binary = external_value.expanduser().resolve()
            provenance = "external"
        else:
            binary = build_current_binary(repo_root)
            provenance = "current-source"
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise ScenarioFailure(f"frp.new is not executable: {binary}")
        binary_sha256 = sha256_file(binary)
        result["binary"] = {
            "path": str(binary),
            "sha256": binary_sha256,
            "build_provenance": provenance,
        }

        fixture = build_fixture(repo_root, args.fixture)
        legacy_created = install_legacy_root(fixture, args.allow_legacy_root_link)
        server_log_path = fixture / "server.log"
        result["fixture_relative"] = "disposable"
        run_info = run_game(
            binary,
            fixture,
            scenario,
            transcript,
            args.timeout,
            args.epoch,
            args.seed,
            server_log_path,
        )
        result.update(run_info)
        result["status"] = "passed"
    except (ScenarioFailure, OSError, ValueError) as exc:
        result["error"] = redact_text(str(exc), transcript.password)
        if server_log_path is not None:
            result["server_log_tail"] = server_diagnostics(server_log_path, transcript.password)
        result["status"] = "failed"
    finally:
        result["duration_ms"] = now_ms() - started
        result.update(transcript.as_json())
        remove_legacy_root(legacy_created)
        if server_log_path is not None:
            try:
                redact_file(server_log_path, transcript.password)
            except OSError as exc:
                result["error"] = f"could not redact server log: {exc}"
                result["status"] = "failed"
        keep_fixture = os.environ.get("KEEP_TEST_FIXTURE", "") in ("1", "true", "TRUE", "yes", "YES")
        if fixture is not None:
            if keep_fixture:
                result["fixture"] = str(fixture)
            else:
                shutil.rmtree(fixture, ignore_errors=True)
        try:
            safe_result = redact_json(result, transcript.password)
            assert_artifact_safe(safe_result, transcript.password)
            result = safe_result
        except (AssertionError, ScenarioFailure):
            # Never emit the unsafe candidate if a future field bypasses the
            # redaction helpers.  Preserve a minimal safe failure artifact.
            result = {
                "schema": 1,
                "status": "failed",
                "error": "artifact redaction self-check failed",
                "duration_ms": result.get("duration_ms", 0),
                "events": [],
                "raw_transcript_b64": "",
            }
            assert_artifact_safe(result, transcript.password)
        try:
            write_result(output_path, result)
        except OSError as exc:
            result["error"] = redact_text(f"could not write JSON result: {exc}", transcript.password)
            result["status"] = "failed"

    summary = {
        "status": result["status"],
        "scenario": result.get("scenario", str(args.scenario)),
        "duration_ms": result["duration_ms"],
    }
    if output_path is not None:
        summary["result"] = str(output_path.resolve())
    summary = redact_json(summary, transcript.password)
    print(json.dumps(summary, ensure_ascii=False))
    if result["status"] != "passed":
        if result.get("error"):
            print(f"scenario failed: {result['error']}", file=sys.stderr)
        if result.get("server_log_tail"):
            print("----- server log tail -----", file=sys.stderr)
            print(result["server_log_tail"], file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
