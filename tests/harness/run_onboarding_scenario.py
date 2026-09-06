#!/usr/bin/env python3
"""Real-C MUD1O scenario in a disposable MUHAN_HOME.

This intentionally emulates only the private Gateway control peer.  It does
not read or mutate any checked-in player data, and its evidence contains
redacted protocol output only.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import select
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
from typing import Any, Iterable, Optional

from run_scenario import (
    ScenarioFailure,
    build_current_binary,
    build_fixture,
    choose_port,
    connect_first,
    expected_player_path,
    find_repo_root,
    now_ms,
)


SECRET = "onboarding-scenario-secret-0123456789"
PASSWORD = "onb-pass-123"
ACTOR = "11111111-1111-4111-8111-111111111111"
CORRELATION = "22222222-2222-4222-8222-222222222222"
CHARACTER = "33333333-3333-4333-8333-333333333333"
CLAIM_ACTIVATION_COMMAND = "12121212-1212-4121-8121-121212121212"


class Redactor:
    def __init__(self, sensitive: list[str]) -> None:
        self.sensitive = sensitive
        self.tail = b""
        self.data = bytearray()

    def feed(self, chunk: bytes) -> None:
        raw = self.tail + chunk
        hold = 0
        for value in self.sensitive:
            token = value.encode("utf-8")
            for size in range(1, min(len(token) - 1, len(raw)) + 1):
                if raw[-size:] == token[:size]:
                    hold = max(hold, size)
        emit = raw[:-hold] if hold else raw
        for value in self.sensitive:
            emit = emit.replace(value.encode("utf-8"), b"<REDACTED>")
        self.data.extend(emit)
        self.tail = raw[-hold:] if hold else b""

    def finish(self) -> bytes:
        self.feed(b"")
        if self.tail:
            emit = self.tail
            self.tail = b""
            for value in self.sensitive:
                emit = emit.replace(value.encode("utf-8"), b"<REDACTED>")
            self.data.extend(emit)
        return bytes(self.data)


class Session:
    def __init__(self, sock: socket.socket, redactor: Redactor, timeout: float) -> None:
        self.sock = sock
        self.redactor = redactor
        self.timeout = timeout
        self.pending = bytearray()

    def send(self, data: bytes) -> None:
        self.sock.sendall(data)

    def send_fragmented(self, data: bytes, cut: int = 7) -> None:
        if cut <= 0 or cut >= len(data):
            self.send(data)
            return
        self.send(data[:cut])
        self.send(data[cut:])

    def read_until(self, marker: bytes) -> bytes:
        deadline = time.monotonic() + self.timeout
        while True:
            offset = self.pending.find(marker)
            if offset >= 0:
                end = offset + len(marker)
                value = bytes(self.pending[:end])
                del self.pending[:end]
                return value
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ScenarioFailure("onboarding response timed out waiting for " +
                                      marker.decode("utf-8", errors="replace"))
            readable, _, _ = select.select([self.sock], [], [], min(remaining, 0.25))
            if not readable:
                continue
            chunk = self.sock.recv(4096)
            if not chunk:
                raise ScenarioFailure("onboarding socket closed before expected response")
            self.redactor.feed(chunk)
            self.pending.extend(chunk)

    def read_close(self) -> bytes:
        data = bytearray(self.pending)
        self.pending.clear()
        deadline = time.monotonic() + self.timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ScenarioFailure("onboarding error socket did not close")
            readable, _, _ = select.select([self.sock], [], [], min(remaining, 0.25))
            if not readable:
                continue
            chunk = self.sock.recv(4096)
            if not chunk:
                return bytes(data)
            self.redactor.feed(chunk)
            data.extend(chunk)

    def read_available(self, quiet_window: float = 0.25) -> bytes:
        """Drain bytes already queued plus bytes arriving in a short quiet window."""
        data = bytearray(self.pending)
        self.pending.clear()
        deadline = time.monotonic() + quiet_window
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return bytes(data)
            readable, _, _ = select.select([self.sock], [], [], remaining)
            if not readable:
                return bytes(data)
            chunk = self.sock.recv(4096)
            if not chunk:
                return bytes(data)
            self.redactor.feed(chunk)
            data.extend(chunk)
            deadline = time.monotonic() + quiet_window


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=None)
    parser.add_argument("--fixture", type=Path, default=None)
    parser.add_argument("--timeout", type=float, default=12.0)
    return parser.parse_args()


def mud1o_ticket(mode: str, nonce: str, correlation: str) -> tuple[str, str]:
    expires = int(time.time()) + 15
    signed = f"MUD1O|{mode}|{expires}|{nonce}|{ACTOR}|{correlation}"
    mac = hmac.new(SECRET.encode("ascii"), signed.encode("ascii"), hashlib.sha256).hexdigest()
    return f"{signed}|{mac}\n", mac


def mud1_ticket(name: str, nonce: str) -> tuple[str, str]:
    expires = int(time.time()) + 15
    signed = f"MUD1|{expires}|{nonce}|{ACTOR}|{CHARACTER}|{name.encode('utf-8').hex()}"
    mac = hmac.new(SECRET.encode("ascii"), signed.encode("ascii"), hashlib.sha256).hexdigest()
    return f"{signed}|{mac}\n", mac


def start(binary: Path, fixture: Path, port: int, log: Path, enabled: bool) -> subprocess.Popen[bytes]:
    env = os.environ.copy()
    env.update({
        "MUHAN_HOME": str(fixture),
        "MUD_ADMISSION_SECRET": SECRET,
        "MUD_REQUIRE_TRUSTED_ADMISSION": "1",
        "MUD_ENABLE_ONBOARDING": "1" if enabled else "0",
        "LC_ALL": "C.UTF-8",
    })
    with log.open("wb") as stream:
        return subprocess.Popen([str(binary), "-r", str(port)], cwd=str(fixture), env=env,
                                stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)


def stop(process: Optional[subprocess.Popen[bytes]]) -> None:
    if process is not None and process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            try:
                process.kill()
            except ProcessLookupError:
                pass
    if process is not None:
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=2)


def expect_no_banner(session: Session) -> None:
    readable, _, _ = select.select([session.sock], [], [], 0.25)
    if readable:
        data = session.sock.recv(4096)
        if data:
            session.redactor.feed(data)
            raise ScenarioFailure("ticket lane emitted a legacy banner")


def add_sensitive(sensitive: list[str], *values: str) -> None:
    for value in values:
        if value and value not in sensitive:
            sensitive.append(value)


def read_control(session: Session, prefix: bytes) -> bytes:
    for _ in range(32):
        line = session.read_until(b"\n")
        offset = line.find(prefix)
        if offset >= 0:
            return line[offset:]
    raise ScenarioFailure("onboarding control was buried behind unexpected player output")


def expect_claim_challenge(session: Session, name: str, digest: str) -> None:
    """Require the exact challenge and prove the password lane is still closed."""
    expected = f"MUD1O CHALLENGE|{name.encode('utf-8').hex()}|{digest}\n".encode()
    challenge = read_control(session, b"MUD1O CHALLENGE|")
    if challenge != expected:
        raise ScenarioFailure("claim did not emit the exact canonical file challenge")

    # This is deliberately a short protocol-ordering check, not a replacement
    # for the production 90-second allow window (covered by the C unit test).
    pre_allow = session.read_available(quiet_window=0.20)
    password_prompt = "암호를 넣어 주십시요".encode()
    if password_prompt in pre_allow or b"\xff\xfb\x01" in pre_allow:
        raise ScenarioFailure("claim exposed password input or disabled echo before ALLOW")
    if pre_allow:
        raise ScenarioFailure("claim emitted unexpected player output before ALLOW")


def allow_claim_password(session: Session, fragmented: bool) -> None:
    allow = b"MUD1O ALLOW\n"
    if fragmented:
        session.send_fragmented(allow, 6)
    else:
        session.send(allow)
    session.read_until("암호를 넣어 주십시요".encode())
    if b"\xff\xfb\x01" not in session.read_until(b"\xff\xfb\x01"):
        raise ScenarioFailure("claim password prompt did not disable Telnet echo after ALLOW")


def consume_wizard(session: Session, password: str, first_prompt_seen: bool = False) -> None:
    if not first_prompt_seen:
        session.read_until("당신은 남자입니까".encode())
    session.send("남\n".encode())
    session.read_until("직업을 고르세요".encode())
    session.send(b"4\n")
    session.read_until("능력:".encode())
    session.send(b"12 10 12 10 10\n")
    session.read_until("익숙한 무기를".encode())
    session.send(b"1\n")
    session.read_until("성향을 고르십시요".encode())
    session.send("선\n".encode())
    session.read_until("종족".encode())
    session.send(b"7\n")
    session.read_until("새 암호를".encode())
    session.send(password.encode() + b"\n")


def receipt_path(fixture: Path, correlation: str) -> Path:
    return fixture / "onboarding-receipts" / f"{correlation}.receipt"


def expected_receipt(state: str, correlation: str, character: str, name: str,
                     digest: str = "") -> bytes:
    return (
        "version=1\n"
        f"state={state}\n"
        f"actor_uuid={ACTOR}\n"
        f"correlation_uuid={correlation}\n"
        f"character_uuid={character}\n"
        f"canonical_name_hex={name.encode('utf-8').hex()}\n"
        "storage_format=player-v1\n"
        f"saved_file_sha256={digest}\n"
    ).encode("ascii")


def assert_receipt(fixture: Path, state: str, correlation: str, character: str,
                   name: str, sensitive: Iterable[str], digest: str = "") -> None:
    path = receipt_path(fixture, correlation)
    parent = path.parent
    if not path.is_file():
        raise ScenarioFailure(f"missing {state} onboarding receipt")
    if stat.S_IMODE(parent.stat().st_mode) != 0o700:
        raise ScenarioFailure("onboarding receipt directory is not private")
    if stat.S_IMODE(path.stat().st_mode) != 0o600:
        raise ScenarioFailure("onboarding receipt is not 0600")
    content = path.read_bytes()
    if content != expected_receipt(state, correlation, character, name, digest):
        raise ScenarioFailure(f"{state} onboarding receipt escaped the allowlist")
    assert_no_sensitive(content, sensitive, f"{state} onboarding receipt")


def inject_pending_rename_crash_window(fixture: Path, correlation: str,
                                       character: str, name: str) -> None:
    """Model SIGKILL after player-file rename but before mark_saved().

    The real wizard above has already exercised the production atomic rename
    and produced the exact file bytes.  Replacing only the disposable receipt
    with its known pre-transition record lets the next real process exercise
    the otherwise uninjectable instruction-sized crash window deterministically.
    """
    path = receipt_path(fixture, correlation)
    with path.open("wb") as stream:
        stream.write(expected_receipt("pending", correlation, character, name))
        stream.flush()
        os.fsync(stream.fileno())


def provision(session: Session, name: str, nonce: str, correlation: str, character: str,
              sensitive: list[str], fixture: Path, commit: Optional[bool] = True) -> str:
    ticket, mac = mud1o_ticket("P", nonce, correlation)
    add_sensitive(sensitive, ticket.strip(), mac)
    expect_no_banner(session)
    session.send_fragmented(ticket.encode(), 23)
    response = session.read_until(b"MUD1O OK\n")
    if not response.startswith(b"MUD1O OK\n"):
        raise ScenarioFailure("MUD1O OK was not the first onboarding response")
    session.read_until("당신의 이름은 무엇입니까".encode())
    session.send(name.encode() + b"\n")
    session.read_until("하시겠습니까".encode())
    session.send("예\n".encode())
    session.read_until("[엔터]를 누르십시요".encode())
    session.send(b"\n")
    expected = f"MUD1O RESERVE|{name.encode('utf-8').hex()}\n".encode()
    reserve = read_control(session, b"MUD1O RESERVE|")
    if reserve != expected:
        raise ScenarioFailure("provision did not emit the canonical RESERVE control")
    session.send_fragmented(f"MUD1O RESERVED|{character}\n".encode(), 15)
    # The next wizard prompt cannot be emitted until C has written its pending
    # receipt.  No player-save input has been sent at this point.
    session.read_until("당신은 남자입니까".encode())
    assert_receipt(fixture, "pending", correlation, character, name, sensitive)
    if commit is False:
        session.send(b"MUD1O COMMIT\n")
        response = session.read_close()
        if not is_generic_onboarding_error(response):
            raise ScenarioFailure("out-of-order control was not rejected generically")
        return ""
    consume_wizard(session, PASSWORD, first_prompt_seen=True)
    prefix = f"MUD1O SAVED|{character}|".encode()
    suffix = b"|player-v1\n"
    saved_control = read_control(session, prefix)
    if not saved_control.startswith(prefix) or not saved_control.endswith(suffix) or len(saved_control) != len(prefix) + 64 + len(suffix):
        raise ScenarioFailure("provision did not emit a bounded SAVED control")
    digest = saved_control[len(prefix):-len(suffix)].decode("ascii")
    assert_receipt(fixture, "saved", correlation, character, name, sensitive, digest)
    if commit is True:
        session.send_fragmented(b"MUD1O COMMIT\n", 8)
        session.send_fragmented(
            f"MUD1O ACTIVATED|{character}\n".encode(), 13,
        )
        session.read_until("레벨 5".encode())
        session.read_until("도력): ".encode())
        assert_receipt(fixture, "committed", correlation, character, name, sensitive, digest)
    return digest


def assert_no_sensitive(data: bytes | str, sensitive: Iterable[str], where: str) -> None:
    raw = data.encode("utf-8") if isinstance(data, str) else data
    for value in sensitive:
        if value.encode("utf-8") in raw:
            raise ScenarioFailure(f"sensitive value leaked in {where}")


def is_generic_onboarding_error(data: bytes) -> bool:
    return data.endswith(b"MUD1O ERR\n") and b"MUD1O VERIFIED" not in data and b"MUD1O SAVED" not in data


def settled_sha256(path: Path, timeout: float) -> str:
    deadline = time.monotonic() + timeout
    current = hashlib.sha256(path.read_bytes()).hexdigest()
    stable = 0
    while time.monotonic() < deadline:
        time.sleep(0.05)
        next_value = hashlib.sha256(path.read_bytes()).hexdigest()
        if next_value != current:
            current = next_value
            stable = 0
            continue
        stable += 1
        if stable >= 6:
            return current
    raise ScenarioFailure("player save did not settle after relogin")


def login_log_contains(fixture: Path, name: str) -> bool:
    """Return whether the disposable server login log mentions `name`."""
    log = fixture / "log" / "log"
    return log.is_file() and name.encode("utf-8") in log.read_bytes()


def login_log_count(fixture: Path, name: str) -> int:
    """Count legacy `init_ply` login records without matching logout text."""
    log = fixture / "log" / "log"
    return log.read_bytes().count(f" {name} (".encode("utf-8")) if log.is_file() else 0


def main() -> int:
    args = parse_args()
    started = now_ms()
    fixture: Optional[Path] = None
    process: Optional[subprocess.Popen[bytes]] = None
    sensitive = [SECRET, PASSWORD]
    redactor = Redactor(sensitive)
    result: dict[str, Any] = {"schema": 1, "status": "failed", "events": []}
    stage = "setup"
    try:
        stage = "build"
        root = find_repo_root(args.repo_root)
        external_binary = os.environ.get("AI_SCENARIO_BINARY", "")
        if external_binary:
            if os.environ.get("AI_SCENARIO_ALLOW_EXTERNAL_BINARY") != "1":
                raise ScenarioFailure("external binary execution was not explicitly enabled")
            binary = Path(external_binary).resolve()
        else:
            binary = build_current_binary(root)
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise ScenarioFailure("real C binary is not executable")
        fixture = build_fixture(root, args.fixture)

        # Feature flag off: MUD1O is handled as a generic ticket error, never
        # by the human legacy login path.
        stage = "flag-off"
        off_port = choose_port()
        process = start(binary, fixture, off_port, fixture / "flag-off.log", False)
        stage = "flag-off-connect"
        off = Session(connect_first(off_port, args.timeout, process), redactor, args.timeout)
        disabled, disabled_mac = mud1o_ticket("P", "00112233445566778899aabbccddeeff", CORRELATION)
        add_sensitive(sensitive, disabled.strip(), disabled_mac)
        stage = "flag-off-banner"
        expect_no_banner(off)
        stage = "flag-off-response"
        off.send(disabled.encode())
        off_response = off.read_close()
        if off_response != b"MUD1 ERR\n":
            result["flag_off_response_b64"] = base64.b64encode(off_response).decode("ascii")
            raise ScenarioFailure("disabled MUD1O did not fail with the legacy-generic error")
        off.sock.close()
        stop(process)
        process = None
        result["events"].append({"case": "flag-off", "response": "MUD1 ERR"})

        # Startup failures are intentionally one generic line.  The malformed
        # receipt contains values that would be sensitive provenance in a
        # real deployment; none may reach stderr before bind/listen.
        stage = "startup-malformed-receipt"
        malformed_dir = fixture / "onboarding-receipts"
        malformed_dir.mkdir(mode=0o700)
        malformed = malformed_dir / "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.receipt"
        malformed.write_text(
            f"actor_uuid={ACTOR}\npassword={PASSWORD}\nsha256={'0' * 64}\n",
            encoding="utf-8",
        )
        malformed.chmod(0o600)
        startup_log = fixture / "malformed-startup.log"
        process = start(binary, fixture, choose_port(), startup_log, True)
        try:
            returncode = process.wait(timeout=args.timeout)
        except subprocess.TimeoutExpired as exc:
            raise ScenarioFailure("malformed receipt process reached listener startup") from exc
        process = None
        startup_output = startup_log.read_bytes()
        if returncode != 78 or startup_output != b"onboarding recovery failed\n":
            raise ScenarioFailure("malformed receipt did not fail closed with the generic startup error")
        assert_no_sensitive(startup_output,
                            [ACTOR, CORRELATION, CHARACTER, PASSWORD, SECRET, "0" * 64],
                            "malformed receipt startup log")
        malformed.unlink()
        malformed_dir.rmdir()
        result["events"].append({"case": "startup-malformed-receipt", "response": "generic-fail-closed"})

        stage = "provision"
        port = choose_port()
        log = fixture / "onboarding-server.log"
        process = start(binary, fixture, port, log, True)

        # Provisioning preserves the ordinary create confirmation before the
        # private reservation protocol.  Declining a name must leave both the
        # player store and onboarding receipt store untouched.
        stage = "provision-confirmation-no"
        declined = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        declined_name = "Declined"
        declined_correlation = "10101010-1010-4010-8010-101010101010"
        declined_ticket, declined_mac = mud1o_ticket(
            "P", "04112233445566778899aabbccddeeff", declined_correlation,
        )
        add_sensitive(sensitive, declined_ticket.strip(), declined_mac)
        expect_no_banner(declined)
        declined.send(declined_ticket.encode())
        declined.read_until(b"MUD1O OK\n")
        declined.read_until("당신의 이름은 무엇입니까".encode())
        declined.send(declined_name.encode() + b"\n")
        declined.read_until("하시겠습니까".encode())
        if b"MUD1O RESERVE" in declined.read_available():
            raise ScenarioFailure("declined provision emitted RESERVE")
        declined.send("아니오\n".encode())
        declined.read_until("당신의 이름은 무엇입니까".encode())
        if b"MUD1O RESERVE" in declined.read_available() or \
           expected_player_path(fixture, declined_name).exists() or \
           receipt_path(fixture, declined_correlation).exists():
            raise ScenarioFailure("declined provision changed character data")
        declined.sock.close()
        result["events"].append({"case": "provision-confirmation-no", "response": "no-RESERVE/no-data"})

        # Even an accepted name is not reservable until the legacy [enter]
        # gate has been crossed, and the wizard must not start before then.
        stage = "provision-confirmation-await-enter"
        awaiting_enter = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        awaiting_name = "Awaitenter"
        awaiting_correlation = "11111111-1111-4111-8111-111111111110"
        awaiting_ticket, awaiting_mac = mud1o_ticket(
            "P", "06112233445566778899aabbccddeeff", awaiting_correlation,
        )
        add_sensitive(sensitive, awaiting_ticket.strip(), awaiting_mac)
        expect_no_banner(awaiting_enter)
        awaiting_enter.send(awaiting_ticket.encode())
        awaiting_enter.read_until(b"MUD1O OK\n")
        awaiting_enter.read_until("당신의 이름은 무엇입니까".encode())
        awaiting_enter.send(awaiting_name.encode() + b"\n")
        awaiting_enter.read_until("하시겠습니까".encode())
        awaiting_enter.send("예\n".encode())
        awaiting_enter.read_until("[엔터]를 누르십시요".encode())
        waiting_output = awaiting_enter.read_available()
        if b"MUD1O RESERVE" in waiting_output or "당신은 남자입니까".encode() in waiting_output or \
           expected_player_path(fixture, awaiting_name).exists() or \
           receipt_path(fixture, awaiting_correlation).exists():
            raise ScenarioFailure("accepted provision advanced before [enter]")
        awaiting_enter.sock.close()
        result["events"].append({"case": "provision-confirmation-await-enter", "response": "no-RESERVE/no-wizard"})

        # The web-assisted creation lane must not turn legacy DM-name
        # conventions into a fresh administrator character.
        stage = "reserved-admin-name"
        reserved = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        reserved_ticket, reserved_mac = mud1o_ticket(
            "P",
            "05112233445566778899aabbccddeeff",
            "12121212-1212-4212-8212-121212121212",
        )
        add_sensitive(sensitive, reserved_ticket.strip(), reserved_mac)
        expect_no_banner(reserved)
        reserved.send(reserved_ticket.encode())
        reserved.read_until(b"MUD1O OK\n")
        reserved.read_until("당신의 이름은 무엇입니까".encode())
        reserved.send("뽀뽀\n".encode())
        reserved_response = reserved.read_until(b"\n") + reserved.read_close()
        if not is_generic_onboarding_error(reserved_response):
            raise ScenarioFailure("reserved administrator name was accepted by onboarding")
        reserved.sock.close()
        if expected_player_path(fixture, "뽀뽀").exists():
            raise ScenarioFailure("reserved administrator name created a player file")
        result["events"].append({"case": "reserved-admin-name", "response": "MUD1O ERR"})

        # The production wizard emits SAVED only after atomic player-file
        # publication.  Rewrite its disposable receipt to the exact preceding
        # pending record, then SIGKILL: this deterministically models the
        # instruction-sized rename -> mark_saved crash window.
        stage = "pending-rename-crash-window"
        crashed = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        crash_correlation = "23232323-2323-4232-8232-232323232323"
        crash_character = "34343434-3434-4434-8434-343434343434"
        crash_digest = provision(
            crashed, "Crash", "09112233445566778899aabbccddeeff",
            crash_correlation, crash_character, sensitive, fixture, commit=None,
        )
        crash_player = expected_player_path(fixture, "Crash")
        if not crash_player.is_file() or hashlib.sha256(crash_player.read_bytes()).hexdigest() != crash_digest:
            raise ScenarioFailure("crash-window player file did not match SAVED digest")
        inject_pending_rename_crash_window(fixture, crash_correlation,
                                           crash_character, "Crash")
        assert_receipt(fixture, "pending", crash_correlation, crash_character,
                       "Crash", sensitive)
        stop(process)
        process = None
        crashed.sock.close()
        port = choose_port()
        process = start(binary, fixture, port, fixture / "post-pending-crash-server.log", True)
        # Listener readiness proves the startup scan ran before bind/listen.
        crash_relogin = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        assert_receipt(fixture, "saved", crash_correlation, crash_character,
                       "Crash", sensitive, crash_digest)
        # The file was saved before C died, so a normal MUD1 login must still
        # perform the legacy initialization successfully after restart.
        crash_legacy, crash_legacy_mac = mud1_ticket("Crash", "0a112233445566778899aabbccddeeff")
        add_sensitive(sensitive, crash_legacy.strip(), crash_legacy_mac)
        expect_no_banner(crash_relogin)
        crash_relogin.send_fragmented(crash_legacy.encode(), 13)
        crash_relogin.read_until(b"MUD1 OK\n")
        crash_relogin.send("건강\n".encode())
        crash_relogin.read_until("체력".encode())
        crash_relogin.send("끝\n".encode())
        crash_relogin.read_until("안녕히".encode())
        crash_relogin.read_close()
        crash_relogin.sock.close()
        result["events"].append({"case": "pending-rename-crash-window", "response": "pending-to-saved-at-startup"})
        result["events"].append({"case": "pending-rename-crash-relogin-mud1", "response": "MUD1 OK"})

        good = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        digest = provision(good, "Alice", "10112233445566778899aabbccddeeff", CORRELATION,
                           CHARACTER, sensitive, fixture)
        player = expected_player_path(fixture, "Alice")
        if not player.is_file() or hashlib.sha256(player.read_bytes()).hexdigest() != digest:
            raise ScenarioFailure("SAVED digest did not match the final player file")

        # A saved-but-uncommitted character must not be present in the world.
        # Keep Alice live as an observer while a second real wizard reaches
        # SAVED, then prove visibility begins only after Gateway COMMIT.
        stage = "pre-commit-world-invisibility"
        good.read_available()
        staged = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        staged_digest = provision(
            staged,
            "Staged",
            "15112233445566778899aabbccddeeff",
            "34343434-3434-4434-8434-343434343434",
            "45454545-4545-4454-8454-454545454545",
            sensitive, fixture,
            commit=None,
        )
        staged_player = expected_player_path(fixture, "Staged")
        if not staged_player.is_file() or hashlib.sha256(staged_player.read_bytes()).hexdigest() != staged_digest:
            raise ScenarioFailure("staged SAVED digest did not match the player file")
        before_commit = good.read_available()
        if b"Staged" in before_commit:
            raise ScenarioFailure("saved character became world-visible before COMMIT")
        if login_log_contains(fixture, "Staged"):
            raise ScenarioFailure("saved character wrote a login/logout log before COMMIT")

        staged.send_fragmented(b"MUD1O COMMIT\n", 8)
        staged.send_fragmented(
            "MUD1O ACTIVATED|45454545-4545-4454-8454-454545454545\n".encode(), 13,
        )
        staged.read_until("레벨 5".encode())
        staged.read_until("도력): ".encode())
        after_commit = good.read_until(b"Staged") + good.read_available()
        if b"Staged" not in after_commit:
            raise ScenarioFailure("committed character did not become world-visible")
        if login_log_count(fixture, "Staged") != 1:
            raise ScenarioFailure("committed character did not run legacy login initialization exactly once")
        staged.send("건강\n".encode())
        staged.read_until("체력".encode())
        staged.send("끝\n".encode())
        staged.read_until("안녕히".encode())
        staged.read_close()
        staged.sock.close()
        good.read_available()
        result["events"].append({
            "case": "pre-commit-world-invisibility",
            "response": "hidden-before-COMMIT/visible-after-COMMIT",
        })

        good.send("건강\n".encode())
        good.read_until("체력".encode())
        good.send("끝\n".encode())
        good.read_until("안녕히".encode())
        good.read_close()
        good.sock.close()
        result["events"].append({"case": "provision-fragmented-controls", "response": "SAVED/COMMIT"})

        # A control while the original wizard owns input cannot become player
        # text and must fail before command state.
        stage = "out-of-order"
        order = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        provision(order, "Order", "20112233445566778899aabbccddeeff",
                  "44444444-4444-4444-8444-444444444444",
                  "55555555-5555-4555-8555-555555555555", sensitive, fixture, commit=False)
        order.sock.close()
        result["events"].append({"case": "out-of-order-control", "response": "MUD1O ERR"})

        # Make only the disposable player directory non-writable after
        # RESERVE.  This is a safe real FileStore failure injection.
        stage = "save-failure"
        failing = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        ticket, mac = mud1o_ticket("P", "30112233445566778899aabbccddeeff",
                                   "66666666-6666-4666-8666-666666666666")
        add_sensitive(sensitive, ticket.strip(), mac)
        expect_no_banner(failing)
        failing.send(ticket.encode())
        failing.read_until(b"MUD1O OK\n")
        failing.read_until("당신의 이름은 무엇입니까".encode())
        failing.send(b"Failed\n")
        failing.read_until("하시겠습니까".encode())
        failing.send("예\n".encode())
        failing.read_until("[엔터]를 누르십시요".encode())
        failing.send(b"\n")
        read_control(failing, b"MUD1O RESERVE|")
        failing.send(f"MUD1O RESERVED|77777777-7777-4777-8777-777777777777\n".encode())
        failing.read_until("당신은 남자입니까".encode())
        old_mode = (fixture / "player").stat().st_mode
        (fixture / "player").chmod(0o500)
        try:
            failing.send("남\n".encode())
            failing.read_until("직업을 고르세요".encode())
            failing.send(b"4\n")
            failing.read_until("능력:".encode())
            failing.send(b"12 10 12 10 10\n")
            failing.read_until("익숙한 무기를".encode())
            failing.send(b"1\n")
            failing.read_until("성향을 고르십시요".encode())
            failing.send("선\n".encode())
            failing.read_until("종족".encode())
            failing.send(b"7\n")
            failing.read_until("새 암호를".encode())
            failing.send(PASSWORD.encode() + b"\n")
            if not is_generic_onboarding_error(failing.read_close()):
                raise ScenarioFailure("save failure entered gameplay or exposed detail")
        finally:
            (fixture / "player").chmod(old_mode)
        failing.sock.close()
        if expected_player_path(fixture, "Failed").exists():
            raise ScenarioFailure("failed save created a player file")
        assert_receipt(fixture, "pending", "66666666-6666-4666-8666-666666666666",
                       "77777777-7777-4777-8777-777777777777", "Failed", sensitive)
        result["events"].append({"case": "save-failure", "response": "MUD1O ERR"})

        # Restart and use untouched MUD1 ticket admission to prove both the
        # persisted file and the pre-existing lane remain usable.
        stage = "restart-relogin"
        stop(process)
        process = None
        port = choose_port()
        process = start(binary, fixture, port, fixture / "restart-server.log", True)
        relogin = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        legacy, legacy_mac = mud1_ticket("Alice", "40112233445566778899aabbccddeeff")
        add_sensitive(sensitive, legacy.strip(), legacy_mac)
        expect_no_banner(relogin)
        relogin.send_fragmented(legacy.encode(), 13)
        relogin.read_until(b"MUD1 OK\n")
        relogin.send("건강\n".encode())
        relogin.read_until("체력".encode())
        relogin.send("끝\n".encode())
        relogin.read_close()
        relogin.sock.close()
        current_digest = settled_sha256(player, args.timeout)
        result["events"].append({"case": "restart-relogin-mud1", "response": "MUD1 OK"})

        # A peer may disappear while C waits for the Gateway ALLOW.  This
        # exercises the central disconnect fail-safe with a loaded claim
        # creature, without sending or recording a password.
        stage = "claim-peer-eof-await-allow"
        eof_allow = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        eof_allow_ticket, eof_allow_mac = mud1o_ticket(
            "C", "45112233445566778899aabbccddeeff",
            "85858585-8585-4858-8858-858585858585")
        add_sensitive(sensitive, eof_allow_ticket.strip(), eof_allow_mac)
        expect_no_banner(eof_allow)
        eof_allow.send(eof_allow_ticket.encode())
        eof_allow.read_until(b"MUD1O OK\n")
        eof_allow.read_until("당신의 이름은 무엇입니까".encode())
        eof_allow.send(b"Alice\n")
        expect_claim_challenge(eof_allow, "Alice", current_digest)
        eof_allow.sock.shutdown(socket.SHUT_RDWR)
        eof_allow.sock.close()
        time.sleep(0.20)
        result["events"].append({"case": "claim-peer-eof-await-allow", "response": "closed"})

        # Repeat the EOF probe after ALLOW, while the private C process is
        # holding the loaded legacy password and waiting for one input line.
        stage = "claim-peer-eof-await-password"
        eof_password = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        eof_password_ticket, eof_password_mac = mud1o_ticket(
            "C", "46112233445566778899aabbccddeeff",
            "86868686-8686-4868-8868-868686868686")
        add_sensitive(sensitive, eof_password_ticket.strip(), eof_password_mac)
        expect_no_banner(eof_password)
        eof_password.send(eof_password_ticket.encode())
        eof_password.read_until(b"MUD1O OK\n")
        eof_password.read_until("당신의 이름은 무엇입니까".encode())
        eof_password.send(b"Alice\n")
        expect_claim_challenge(eof_password, "Alice", current_digest)
        allow_claim_password(eof_password, fragmented=False)
        eof_password.sock.shutdown(socket.SHUT_RDWR)
        eof_password.sock.close()
        time.sleep(0.20)
        result["events"].append({"case": "claim-peer-eof-await-password", "response": "closed"})

        stage = "claim-success"
        claim_ticket, claim_mac = mud1o_ticket("C", "50112233445566778899aabbccddeeff",
                                                "88888888-8888-4888-8888-888888888888")
        add_sensitive(sensitive, claim_ticket.strip(), claim_mac)
        claim = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        expect_no_banner(claim)
        claim.send_fragmented(claim_ticket.encode(), 19)
        claim.read_until(b"MUD1O OK\n")
        claim.read_until("당신의 이름은 무엇입니까".encode())
        claim.send(b"Alice\n")
        expect_claim_challenge(claim, "Alice", current_digest)
        allow_claim_password(claim, fragmented=True)
        claim.send(PASSWORD.encode() + b"\n")
        verified = read_control(claim, b"MUD1O VERIFIED|")
        expected_verified = f"MUD1O VERIFIED|416c696365|{current_digest}\n".encode()
        if verified != expected_verified:
            raise ScenarioFailure("claim did not digest the verified on-disk player file")
        claim.send_fragmented(f"MUD1O CLAIMED|{CHARACTER}\n".encode(), 11)
        # The private control peer models the real Gateway only after its
        # finalized ownership handoff. Use a distinct command ID: the earlier
        # provision activation has already bound CHARACTER on disk.
        claim.send_fragmented(
            f"MUD1O ACTIVATED|{CLAIM_ACTIVATION_COMMAND}\n".encode(), 13,
        )
        active = read_control(claim, b"MUD1O ACTIVE|")
        expected_active = f"MUD1O ACTIVE|{CLAIM_ACTIVATION_COMMAND}\n".encode()
        if active != expected_active:
            raise ScenarioFailure("claim did not acknowledge its finalized activation command")
        if claim.read_close() != b"":
            raise ScenarioFailure("successful claim emitted player text instead of closing")
        claim.sock.close()
        result["events"].append({
            "case": "claim-success",
            "response": "VERIFIED/CLAIMED/ACTIVATED/ACTIVE-close",
        })

        stage = "claim-wrong-password"
        wrong = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        wrong_ticket, wrong_mac = mud1o_ticket("C", "60112233445566778899aabbccddeeff",
                                                "99999999-9999-4999-8999-999999999999")
        add_sensitive(sensitive, wrong_ticket.strip(), wrong_mac, "wrong-game-password")
        expect_no_banner(wrong)
        wrong.send(wrong_ticket.encode())
        wrong.read_until(b"MUD1O OK\n")
        wrong.read_until("당신의 이름은 무엇입니까".encode())
        wrong.send(b"Alice\n")
        expect_claim_challenge(wrong, "Alice", current_digest)
        allow_claim_password(wrong, fragmented=False)
        wrong.send(b"wrong-game-password\n")
        if not is_generic_onboarding_error(wrong.read_close()):
            raise ScenarioFailure("wrong claim password had a non-generic response")
        wrong.sock.close()
        result["events"].append({"case": "claim-wrong-password", "response": "MUD1O ERR"})

        # A password supplied in the challenge state is not an implicit ALLOW:
        # it must fail closed without exposing the password prompt or VERIFIED.
        stage = "claim-password-before-allow"
        before_allow = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        before_allow_ticket, before_allow_mac = mud1o_ticket(
            "C", "70112233445566778899aabbccddeeff",
            "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
        add_sensitive(sensitive, before_allow_ticket.strip(), before_allow_mac)
        expect_no_banner(before_allow)
        before_allow.send(before_allow_ticket.encode())
        before_allow.read_until(b"MUD1O OK\n")
        before_allow.read_until("당신의 이름은 무엇입니까".encode())
        before_allow.send(b"Alice\n")
        expect_claim_challenge(before_allow, "Alice", current_digest)
        before_allow.send(PASSWORD.encode() + b"\n")
        if not is_generic_onboarding_error(before_allow.read_close()):
            raise ScenarioFailure("password before ALLOW was not rejected generically")
        before_allow.sock.close()
        result["events"].append({"case": "claim-password-before-allow", "response": "MUD1O ERR"})

        # The digest is bound to the exact file observed in the challenge.  A
        # mutation after challenge but before password verification may never
        # produce VERIFIED, even when the original password is correct.
        stage = "claim-file-mutation"
        mutated = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        mutated_ticket, mutated_mac = mud1o_ticket(
            "C", "80112233445566778899aabbccddeeff",
            "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
        add_sensitive(sensitive, mutated_ticket.strip(), mutated_mac)
        expect_no_banner(mutated)
        mutated.send_fragmented(mutated_ticket.encode(), 17)
        mutated.read_until(b"MUD1O OK\n")
        mutated.read_until("당신의 이름은 무엇입니까".encode())
        mutated.send(b"Alice\n")
        expect_claim_challenge(mutated, "Alice", current_digest)
        original_player = player.read_bytes()
        try:
            player.write_bytes(original_player + b"\nclaim-digest-mutation\n")
            if hashlib.sha256(player.read_bytes()).hexdigest() == current_digest:
                raise ScenarioFailure("claim mutation fixture did not change the player digest")
            allow_claim_password(mutated, fragmented=False)
            mutated.send(PASSWORD.encode() + b"\n")
            mutation_response = mutated.read_close()
        finally:
            player.write_bytes(original_player)
        if not is_generic_onboarding_error(mutation_response):
            raise ScenarioFailure("mutated claim file did not fail generically without VERIFIED")
        mutated.sock.close()
        result["events"].append({"case": "claim-file-mutation", "response": "MUD1O ERR/no-VERIFIED"})

        stage = "replay"
        replay = Session(connect_first(port, args.timeout, process), redactor, args.timeout)
        expect_no_banner(replay)
        replay.send(claim_ticket.encode())
        if not is_generic_onboarding_error(replay.read_close()):
            raise ScenarioFailure("MUD1O replay was not rejected")
        replay.sock.close()
        result["events"].append({"case": "replay", "response": "MUD1O ERR"})

        stop(process)
        process = None
        transcript = redactor.finish()
        for candidate in fixture.glob("*.log"):
            assert_no_sensitive(candidate.read_bytes(), sensitive, candidate.name)
        assert_no_sensitive(transcript, sensitive, "redacted TCP transcript")
        result.update({
            "status": "passed",
            "scenario": "onboarding-real-c",
            "saved_sha256": digest,
            "redacted_tcp_transcript_b64": base64.b64encode(transcript).decode("ascii"),
        })
    except (OSError, ScenarioFailure, ValueError) as exc:
        result["error"] = "onboarding scenario failed at " + stage
        if isinstance(exc, ScenarioFailure):
            result["detail"] = str(exc)
    finally:
        stop(process)
        result["duration_ms"] = now_ms() - started
        if args.output is not None:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(result, sort_keys=True) + "\n", encoding="utf-8")
        if fixture is not None and os.environ.get("KEEP_TEST_FIXTURE") != "1":
            shutil.rmtree(fixture, ignore_errors=True)

    print(json.dumps({"status": result["status"], "scenario": "onboarding-real-c",
                      "duration_ms": result["duration_ms"]}))
    return 0 if result["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
