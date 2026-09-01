#!/usr/bin/env python3
"""Exercise a real C MUD SIGTERM save and subsequent player relogin.

The runner builds the checked-out source, creates a disposable MUHAN_HOME,
logs a player in, sends SIGTERM to the actual frp process, and requires a
bounded successful exit.  It redacts the scenario password before any log or
transcript can be retained as test evidence.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import time
from typing import Any, Dict, Optional

from run_scenario import (
    DEFAULT_SCENARIO,
    DEFAULT_TIMEOUT,
    ScenarioFailure,
    Session,
    Transcript,
    assert_artifact_safe,
    build_current_binary,
    build_fixture,
    choose_port,
    connect_first,
    expected_player_path,
    find_repo_root,
    install_legacy_root,
    load_scenario,
    now_ms,
    redact_file,
    redact_json,
    redact_text,
    remove_legacy_root,
    run_game,
    server_diagnostics,
    sha256_file,
    write_result,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenario", type=Path, default=DEFAULT_SCENARIO)
    parser.add_argument("--repo-root", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=None)
    parser.add_argument("--fixture", type=Path, default=None)
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT)
    parser.add_argument("--shutdown-timeout", type=float, default=5.0)
    parser.add_argument("--epoch", type=int, default=1_735_689_600)
    parser.add_argument("--seed", type=int, default=424242)
    parser.add_argument("--allow-legacy-root-link", action="store_true")
    return parser.parse_args()


def start_server(binary: Path, fixture: Path, port: int, log_path: Path) -> subprocess.Popen[bytes]:
    env = os.environ.copy()
    env["MUHAN_HOME"] = str(fixture)
    env["LC_ALL"] = "C.UTF-8"
    # This is specifically the legacy login/save regression.  Do not inherit a
    # developer's trusted-admission deployment values into its fixture.
    env.pop("MUD_ADMISSION_SECRET", None)
    env.pop("MUD_REQUIRE_TRUSTED_ADMISSION", None)
    with log_path.open("ab") as stream:
        try:
            return subprocess.Popen(
                [str(binary), "-r", str(port)],
                cwd=str(fixture),
                env=env,
                stdout=stream,
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )
        except OSError as exc:
            raise ScenarioFailure(f"cannot start C MUD binary: {exc}") from exc


def login_existing(process: subprocess.Popen[bytes], port: int, timeout: float,
                   scenario: Dict[str, Any], transcript: Transcript, session_number: int) -> Session:
    sock = connect_first(port, timeout, process)
    session = Session(sock, transcript, session_number, timeout)
    session.read_until("[엔터]를 누르세요.", "relogin enter prompt")
    session.enter()
    session.read_until("당신의 이름은 무엇입니까?", "relogin name prompt")
    session.send_line(scenario["player_name"], "relogin player name")
    session.read_until("암호", "relogin password prompt")
    session.send_line(scenario["password"], "relogin password")
    session.read_until("체력", "relogin health state")
    return session


def force_stop(process: Optional[subprocess.Popen[bytes]]) -> None:
    if process is None:
        return
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


def main() -> int:
    args = parse_args()
    started = now_ms()
    scenario: Dict[str, Any] = {}
    transcript = Transcript("")
    fixture: Optional[Path] = None
    server_log_path: Optional[Path] = None
    process: Optional[subprocess.Popen[bytes]] = None
    relogin_process: Optional[subprocess.Popen[bytes]] = None
    sessions: list[Session] = []
    legacy_created = False
    output_path = args.output
    result: Dict[str, Any] = {
        "schema": 1,
        "status": "failed",
        "started_at_ms": started,
        "seed": args.seed,
        "epoch": args.epoch,
    }

    try:
        repo_root = find_repo_root(args.repo_root)
        scenario = load_scenario(args.scenario.resolve())
        transcript = Transcript(scenario["password"])
        result.update({"scenario": scenario["name"], "player_name": scenario["player_name"]})
        if output_path is None:
            output_path = Path(f"/tmp/muhan-shutdown-{scenario['name']}-{os.getpid()}.json")

        binary = build_current_binary(repo_root)
        result["binary"] = {
            "sha256": sha256_file(binary),
            "build_provenance": "current-source",
        }
        fixture = build_fixture(repo_root, args.fixture)
        legacy_created = install_legacy_root(fixture, args.allow_legacy_root_link)
        server_log_path = fixture / "server.log"

        # Seed a real player file.  Skip the separate corruption branch because
        # this test needs that same valid record for the SIGTERM relogin.
        run_game(
            binary, fixture, scenario, transcript, args.timeout, args.epoch,
            args.seed, server_log_path, verify_corrupt_player=False,
        )
        saved = expected_player_path(fixture, scenario["player_name"])
        if not saved.is_file():
            raise ScenarioFailure("seed player file was not created")

        port = choose_port()
        process = start_server(binary, fixture, port, server_log_path)
        active = login_existing(process, port, args.timeout, scenario, transcript, 3)
        sessions.append(active)

        # Make the pre-SIGTERM timestamp unambiguously old.  save_all_ply()
        # must replace this record as part of the normal shutdown path.
        previous_mtime_ns = time.time_ns() - 5_000_000_000
        os.utime(saved, ns=(previous_mtime_ns, previous_mtime_ns))
        sent_at = time.monotonic()
        process.send_signal(signal.SIGTERM)
        try:
            returncode = process.wait(timeout=args.shutdown_timeout)
        except subprocess.TimeoutExpired as exc:
            raise ScenarioFailure("SIGTERM did not produce a bounded server exit") from exc
        elapsed_ms = int((time.monotonic() - sent_at) * 1000)
        if returncode != 0:
            raise ScenarioFailure(f"SIGTERM server exit was not clean (returncode={returncode})")
        if saved.stat().st_mtime_ns <= previous_mtime_ns:
            raise ScenarioFailure("SIGTERM shutdown did not replace the active player record")
        process = None
        active.sock.close()

        # A fresh process must still accept the player whose active record was
        # just saved by SIGTERM, proving both write validity and relogin.
        relogin_port = choose_port()
        relogin_process = start_server(binary, fixture, relogin_port, server_log_path)
        relogin = login_existing(relogin_process, relogin_port, args.timeout, scenario, transcript, 4)
        sessions.append(relogin)
        relogin.send_line("끝", "relogin quit command")
        relogin.read_until("안녕히", "relogin quit response")
        relogin.read_until_close("relogin connection close")
        relogin.sock.close()

        result.update({
            "status": "passed",
            "fixture_relative": "disposable",
            "sigterm": {
                "exit_code": returncode,
                "exit_within_ms": elapsed_ms,
                "player_replaced": True,
                "relogin": True,
            },
        })
    except (ScenarioFailure, OSError, ValueError) as exc:
        result["error"] = redact_text(str(exc), transcript.password)
        if server_log_path is not None:
            result["server_log_tail"] = server_diagnostics(server_log_path, transcript.password)
    finally:
        for session in sessions:
            try:
                session.sock.close()
            except OSError:
                pass
        force_stop(process)
        force_stop(relogin_process)
        result["duration_ms"] = now_ms() - started
        result.update(transcript.as_json())
        if server_log_path is not None:
            try:
                redact_file(server_log_path, transcript.password)
            except OSError:
                result["status"] = "failed"
                result["error"] = "could not redact server log"
        remove_legacy_root(legacy_created)
        keep_fixture = os.environ.get("KEEP_TEST_FIXTURE", "") in ("1", "true", "TRUE", "yes", "YES")
        if fixture is not None:
            if keep_fixture:
                result["fixture"] = str(fixture)
            else:
                shutil.rmtree(fixture, ignore_errors=True)
        try:
            result = redact_json(result, transcript.password)
            assert_artifact_safe(result, transcript.password)
        except (AssertionError, ScenarioFailure):
            result = {
                "schema": 1,
                "status": "failed",
                "error": "artifact redaction self-check failed",
                "duration_ms": result.get("duration_ms", 0),
                "events": [],
                "raw_transcript_b64": "",
            }
        try:
            write_result(output_path, result)
        except OSError as exc:
            result["status"] = "failed"
            result["error"] = redact_text(f"could not write JSON result: {exc}", transcript.password)

    summary = {
        "status": result["status"],
        "scenario": result.get("scenario", str(args.scenario)),
        "duration_ms": result["duration_ms"],
    }
    if output_path is not None:
        summary["result"] = str(output_path.resolve())
    print(json.dumps(redact_json(summary, transcript.password), ensure_ascii=False))
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
