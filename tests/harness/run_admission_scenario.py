#!/usr/bin/env python3
"""Black-box trusted-admission scenario with a disposable MUD home.

The legacy creation phase produces a real player file.  A second process then
uses MUD_ADMISSION_SECRET and exercises fragmented ticket admission without
ever serializing the secret, ticket, MAC, or password into its evidence JSON.
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
import signal
import socket
import subprocess
import sys
import time
from typing import Any, Dict, Iterable, Optional
import urllib.request

from run_scenario import (
    DEFAULT_SCENARIO,
    DEFAULT_TIMEOUT,
    ScenarioFailure,
    Transcript,
    build_current_binary,
    build_fixture,
    choose_port,
    connect_first,
    find_repo_root,
    install_legacy_root,
    load_scenario,
    now_ms,
    remove_legacy_root,
    run_game,
)


SECRET = "trusted-admission-secret-0123456789"


class StreamingRedactor:
    """Retain only a redacted TCP transcript, including split secret matches."""

    def __init__(self, sensitive: list[str]) -> None:
        self.sensitive = sensitive
        self.tail = b""
        self.output = bytearray()

    def feed(self, chunk: bytes) -> None:
        raw = self.tail + chunk
        hold = 0
        for item in self.sensitive:
            token = item.encode("utf-8")
            for size in range(1, min(len(token) - 1, len(raw)) + 1):
                if raw[-size:] == token[:size]:
                    hold = max(hold, size)
        emit = raw[:-hold] if hold else raw
        for item in self.sensitive:
            emit = emit.replace(item.encode("utf-8"), b"<REDACTED>")
        self.output.extend(emit)
        self.tail = raw[-hold:] if hold else b""

    def finish(self) -> bytes:
        emit = self.tail
        self.tail = b""
        for item in self.sensitive:
            emit = emit.replace(item.encode("utf-8"), b"<REDACTED>")
        self.output.extend(emit)
        return bytes(self.output)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenario", type=Path, default=DEFAULT_SCENARIO)
    parser.add_argument("--repo-root", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=None)
    parser.add_argument("--fixture", type=Path, default=None)
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT)
    return parser.parse_args()


def ticket(secret: str, name: str, nonce: str, expires_at: int) -> tuple[str, str]:
    signed = (
        f"MUD1|{expires_at}|{nonce}|123e4567-e89b-12d3-a456-426614174000|"
        f"123e4567-e89b-12d3-a456-426614174001|{name.encode('utf-8').hex()}"
    )
    mac = hmac.new(secret.encode("ascii"), signed.encode("ascii"), hashlib.sha256).hexdigest()
    return f"{signed}|{mac}\n", mac


def ensure_no_sensitive(data: bytes | str, sensitive: Iterable[str], where: str) -> None:
    value = data.encode("utf-8") if isinstance(data, str) else data
    for item in sensitive:
        if item.encode("utf-8") in value:
            raise ScenarioFailure(f"sensitive admission material appeared in {where}")


def receive_until(sock: socket.socket, marker: bytes, timeout: float,
                  capture: StreamingRedactor) -> bytes:
    deadline = time.monotonic() + timeout
    received = bytearray()
    while marker not in received:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ScenarioFailure("admission response timed out")
        readable, _, _ = select.select([sock], [], [], min(remaining, 0.25))
        if not readable:
            continue
        chunk = sock.recv(4096)
        if not chunk:
            raise ScenarioFailure("admission socket closed before expected response")
        capture.feed(chunk)
        received.extend(chunk)
    return bytes(received)


def receive_until_close(sock: socket.socket, timeout: float,
                        capture: StreamingRedactor) -> bytes:
    deadline = time.monotonic() + timeout
    received = bytearray()
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ScenarioFailure("admission error socket did not close")
        readable, _, _ = select.select([sock], [], [], min(remaining, 0.25))
        if not readable:
            continue
        chunk = sock.recv(4096)
        if not chunk:
            return bytes(received)
        capture.feed(chunk)
        received.extend(chunk)


def assert_no_initial_prompt(sock: socket.socket, capture: StreamingRedactor) -> None:
    readable, _, _ = select.select([sock], [], [], 0.35)
    if readable:
        data = sock.recv(4096)
        if data:
            capture.feed(data)
            raise ScenarioFailure("ticket mode emitted a banner or legacy prompt")


def start_ticket_server(binary: Path, fixture: Path, port: int, log_path: Path,
                        secret: Optional[str] = SECRET,
                        required: bool = True) -> subprocess.Popen[bytes]:
    env = os.environ.copy()
    env["MUHAN_HOME"] = str(fixture)
    if secret is None:
        env.pop("MUD_ADMISSION_SECRET", None)
    else:
        env["MUD_ADMISSION_SECRET"] = secret
    if required:
        env["MUD_REQUIRE_TRUSTED_ADMISSION"] = "1"
    else:
        env.pop("MUD_REQUIRE_TRUSTED_ADMISSION", None)
    env["LC_ALL"] = "C.UTF-8"
    with log_path.open("wb") as stream:
        return subprocess.Popen(
            [str(binary), "-r", str(port)],
            cwd=str(fixture),
            env=env,
            stdout=stream,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )


def start_gateway(repo_root: Path, mud_port: int, gateway_port: int,
                  log_path: Path) -> subprocess.Popen[bytes]:
    tsx = repo_root / "services" / "gateway" / "node_modules" / ".bin" / "tsx"
    if not tsx.is_file():
        raise ScenarioFailure("Gateway dependencies are not installed")
    env = os.environ.copy()
    env.update({
        "NODE_ENV": "test",
        "AUTH_DISABLED": "true",
        "HOST": "127.0.0.1",
        "PORT": str(gateway_port),
        "MUD_HOST": "127.0.0.1",
        "MUD_PORT": str(mud_port),
        "MUD_ADMISSION_SECRET": SECRET,
        "GATEWAY_INSTANCE_ID": "gateway-real-c-contract",
        "ALLOWED_ORIGINS": "http://localhost:3000",
    })
    with log_path.open("wb") as stream:
        return subprocess.Popen(
            [str(tsx), str(repo_root / "services" / "gateway" / "src" / "index.ts")],
            cwd=str(repo_root),
            env=env,
            stdout=stream,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )


def wait_gateway_ready(port: int, process: subprocess.Popen[bytes], timeout: float) -> None:
    deadline = time.monotonic() + timeout
    url = f"http://127.0.0.1:{port}/healthz"
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise ScenarioFailure("Gateway exited before its health endpoint became ready")
        try:
            with urllib.request.urlopen(url, timeout=0.25) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(0.05)
    raise ScenarioFailure("Gateway health endpoint timed out")


def run_gateway_real_c_client(repo_root: Path, gateway_port: int,
                              sensitive: Iterable[str]) -> None:
    tsx = repo_root / "services" / "gateway" / "node_modules" / ".bin" / "tsx"
    client = repo_root / "services" / "gateway" / "test" / "harness" / "real-c-client.ts"
    try:
        completed = subprocess.run(
            [str(tsx), str(client), f"ws://127.0.0.1:{gateway_port}/ws"],
            cwd=str(repo_root),
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=10,
        )
    except subprocess.TimeoutExpired as exc:
        raise ScenarioFailure("Gateway real-C client timed out") from exc
    ensure_no_sensitive(completed.stdout, sensitive, "Gateway real-C client output")
    if completed.returncode != 0 or b'"status":"passed"' not in completed.stdout:
        raise ScenarioFailure("Gateway could not relay through the real C admission server")


def stop_server(process: Optional[subprocess.Popen[bytes]]) -> None:
    if process is None:
        return
    if process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=2)


def expect_err(port: int, process: subprocess.Popen[bytes], line: str, timeout: float,
               sensitive: Iterable[str], capture: StreamingRedactor) -> None:
    sock = connect_first(port, timeout, process)
    try:
        assert_no_initial_prompt(sock, capture)
        sock.sendall(line.encode("ascii"))
        response = receive_until_close(sock, timeout, capture)
        ensure_no_sensitive(response, sensitive, "TCP error response")
        if response != b"MUD1 ERR\n":
            raise ScenarioFailure("invalid ticket did not receive the sole MUD1 ERR response")
    finally:
        sock.close()


def write_result(path: Optional[Path], result: Dict[str, Any], sensitive: Iterable[str]) -> None:
    if path is None:
        return
    encoded = json.dumps(result, ensure_ascii=False, sort_keys=True)
    ensure_no_sensitive(encoded, sensitive, "admission evidence")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(encoded + "\n", encoding="utf-8")


def main() -> int:
    args = parse_args()
    started = now_ms()
    result: Dict[str, Any] = {"schema": 1, "status": "failed", "events": []}
    fixture: Optional[Path] = None
    process: Optional[subprocess.Popen[bytes]] = None
    gateway_process: Optional[subprocess.Popen[bytes]] = None
    legacy_created = False
    sensitive: list[str] = [SECRET]
    capture = StreamingRedactor(sensitive)
    try:
        repo_root = find_repo_root(args.repo_root)
        scenario = dict(load_scenario(args.scenario.resolve()))
        # The test-only Gateway authorizer intentionally exposes one fixed
        # character.  The regular legacy scenario separately covers Korean
        # names, while this scenario proves Node ticket bytes against real C.
        scenario["player_name"] = "Test"
        sensitive.append(scenario["password"])
        external_binary = os.environ.get("AI_SCENARIO_BINARY", "")
        if external_binary:
            if os.environ.get("AI_SCENARIO_ALLOW_EXTERNAL_BINARY") != "1":
                raise ScenarioFailure("external binary execution was not explicitly enabled")
            binary = Path(external_binary).resolve()
        else:
            binary = build_current_binary(repo_root)
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise ScenarioFailure("real C binary is not executable")
        fixture = build_fixture(repo_root, args.fixture)
        legacy_created = install_legacy_root(fixture, False)

        # Explicitly remove a caller-provided secret for the compatibility
        # setup run.  It is restored before this harness returns.
        inherited_secret = os.environ.pop("MUD_ADMISSION_SECRET", None)
        inherited_required = os.environ.pop("MUD_REQUIRE_TRUSTED_ADMISSION", None)
        try:
            run_game(binary, fixture, scenario, Transcript(scenario["password"]), args.timeout,
                     1_735_689_600, 424242, fixture / "legacy-server.log",
                     verify_corrupt_player=False)
        finally:
            if inherited_secret is not None:
                os.environ["MUD_ADMISSION_SECRET"] = inherited_secret
            if inherited_required is not None:
                os.environ["MUD_REQUIRE_TRUSTED_ADMISSION"] = inherited_required

        missing_log_path = fixture / "missing-admission-secret-server.log"
        missing_process = start_ticket_server(
            binary, fixture, choose_port(), missing_log_path, None, required=True
        )
        try:
            missing_status = missing_process.wait(timeout=3)
        except subprocess.TimeoutExpired as exc:
            stop_server(missing_process)
            raise ScenarioFailure("required admission mode accepted a missing secret") from exc
        if missing_status == 0:
            raise ScenarioFailure("required admission mode with no secret exited successfully")
        result["events"].append({"case": "required-secret-missing", "response": "startup-rejected"})

        invalid_secret = "invalid-config-secret"
        sensitive.append(invalid_secret)
        invalid_log_path = fixture / "invalid-admission-server.log"
        invalid_process = start_ticket_server(
            binary, fixture, choose_port(), invalid_log_path, invalid_secret
        )
        try:
            invalid_status = invalid_process.wait(timeout=3)
        except subprocess.TimeoutExpired as exc:
            stop_server(invalid_process)
            raise ScenarioFailure("invalid admission secret unexpectedly opened a listener") from exc
        if invalid_status == 0:
            raise ScenarioFailure("invalid admission secret exited successfully")
        ensure_no_sensitive(invalid_log_path.read_bytes(), sensitive, "invalid-config server log")
        result["events"].append({"case": "invalid-config", "response": "startup-rejected"})

        port = choose_port()
        log_path = fixture / "admission-server.log"
        process = start_ticket_server(binary, fixture, port, log_path)
        good = connect_first(port, args.timeout, process)
        try:
            assert_no_initial_prompt(good, capture)
            now = int(time.time())
            good_line, good_mac = ticket(SECRET, scenario["player_name"],
                                         "00112233445566778899aabbccddeeff", now + 10)
            sensitive.extend([good_line.strip(), good_mac])
            encoded = good_line.encode("ascii")
            good.sendall(encoded[:7])
            good.sendall(encoded[7:41])
            good.sendall(encoded[41:])
            response = receive_until(good, b"MUD1 OK\n", args.timeout, capture)
            ensure_no_sensitive(response, sensitive, "TCP success response")
            if not response.startswith(b"MUD1 OK\n"):
                raise ScenarioFailure("ticket acknowledgement was not the first response bytes")
            result["events"].append({"case": "fragmented-valid-ticket", "response": "MUD1 OK"})

            good.sendall("건강\n".encode("utf-8"))
            command_response = receive_until(good, "체력".encode("utf-8"), args.timeout, capture)
            ensure_no_sensitive(command_response, sensitive, "post-admission command response")
            result["events"].append({"case": "post-admission-command", "response": "health"})
            good.sendall("끝\n".encode("utf-8"))
            receive_until_close(good, args.timeout, capture)
        finally:
            good.close()

        expect_err(port, process, good_line, args.timeout, sensitive, capture)
        result["events"].append({"case": "replay", "response": "MUD1 ERR"})
        bad_line, bad_mac = ticket(SECRET, scenario["player_name"],
                                   "10112233445566778899aabbccddeeff", int(time.time()) + 10)
        bad_line = bad_line[:-2] + ("0" if bad_line[-2] != "0" else "1") + "\n"
        sensitive.extend([bad_line.strip(), bad_mac])
        expect_err(port, process, bad_line, args.timeout, sensitive, capture)
        result["events"].append({"case": "bad-signature", "response": "MUD1 ERR"})
        expired_line, expired_mac = ticket(SECRET, scenario["player_name"],
                                           "20112233445566778899aabbccddeeff", int(time.time()) - 1)
        sensitive.extend([expired_line.strip(), expired_mac])
        expect_err(port, process, expired_line, args.timeout, sensitive, capture)
        result["events"].append({"case": "expired", "response": "MUD1 ERR"})

        gateway_port = choose_port()
        gateway_log_path = fixture / "gateway-real-c.log"
        gateway_process = start_gateway(repo_root, port, gateway_port, gateway_log_path)
        wait_gateway_ready(gateway_port, gateway_process, args.timeout)
        run_gateway_real_c_client(repo_root, gateway_port, sensitive)
        stop_server(gateway_process)
        gateway_process = None
        gateway_log = gateway_log_path.read_bytes()
        ensure_no_sensitive(gateway_log, sensitive, "Gateway real-C log")
        if b"MUD1|" in gateway_log:
            raise ScenarioFailure("admission ticket appeared in Gateway log")
        result["events"].append({"case": "gateway-real-c-relay", "response": "ready-health-normal-close"})

        stop_server(process)
        stop_server(gateway_process)
        process = None
        ensure_no_sensitive(log_path.read_bytes(), sensitive, "server log")
        redacted_transcript = capture.finish()
        ensure_no_sensitive(redacted_transcript, sensitive, "redacted TCP transcript")
        result.update({"status": "passed", "scenario": "trusted-admission",
                       "legacy_player_created": True, "ticket_log_checked": True,
                       "redacted_tcp_transcript_b64": base64.b64encode(redacted_transcript).decode("ascii")})
    except (ScenarioFailure, OSError, ValueError):
        result["error"] = "trusted admission scenario failed"
    finally:
        stop_server(process)
        stop_server(gateway_process)
        remove_legacy_root(legacy_created)
        result["duration_ms"] = now_ms() - started
        try:
            write_result(args.output, result, sensitive)
        except (OSError, ScenarioFailure):
            result = {"schema": 1, "status": "failed", "error": "safe evidence write failed",
                      "duration_ms": result["duration_ms"], "events": []}
            if args.output is not None:
                args.output.write_text(json.dumps(result) + "\n", encoding="utf-8")
        if fixture is not None:
            import shutil
            shutil.rmtree(fixture, ignore_errors=True)

    print(json.dumps({"status": result["status"], "scenario": "trusted-admission",
                      "duration_ms": result["duration_ms"]}))
    return 0 if result["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
