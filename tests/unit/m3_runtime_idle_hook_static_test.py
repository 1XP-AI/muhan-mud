#!/usr/bin/env python3
"""Audit the M3-only durable idle-hook boundary without a live database."""

from __future__ import annotations

import argparse
import subprocess
from pathlib import Path


def between(text: str, first: str, second: str) -> str:
    start = text.index(first)
    end = text.index(second, start)
    return text[start:end]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--main-source", type=Path, required=True)
    parser.add_argument("--io-source", type=Path, required=True)
    parser.add_argument("--native-source", type=Path, required=True)
    parser.add_argument("--default-io-object", type=Path, required=True)
    args = parser.parse_args()

    main_source = args.main_source.read_text(encoding="utf-8")
    io_source = args.io_source.read_text(encoding="utf-8")
    native_source = args.native_source.read_text(encoding="utf-8")
    loop = between(io_source, "void sock_loop()", "/**********************************************************************/\n/*\t\t\t\tio_check")

    required_main = (
        "static character_save_journal_v2_runtime_native m3_native;",
        "character_save_journal_v2_runtime_native_snapshot_idle_configure(&m3_native,",
        "m3_runtime_install_idle_hook(m3_runtime_snapshot_idle_hook);",
        "m3_runtime_remove_idle_hook();",
    )
    if any(item not in main_source for item in required_main):
        raise SystemExit("main must own and install/uninstall the M3 native idle hook")
    handler_arm = main_source.index("install_graceful_shutdown_handler();")
    runtime_init = main_source.index("character_save_journal_v2_runtime_native_init(&m3_native);")
    atexit_registration = main_source.index("atexit(m3_runtime_shutdown_at_exit)")
    if handler_arm >= runtime_init or handler_arm >= atexit_registration:
        raise SystemExit("SIGTERM handling must be armed before M3 startup and atexit registration")
    ordered = ("output_buf();", "handle_commands();", "update_game();", "m3_runtime_run_idle_hook();")
    offsets = [loop.index(item) for item in ordered]
    if offsets != sorted(offsets):
        raise SystemExit("idle hook must run only after output, commands, and update return")
    if "m3_runtime_idle_start_allowed()" not in io_source:
        raise SystemExit("idle hook must gate tick start at the shutdown boundary")
    if "sigprocmask(SIG_BLOCK" not in io_source:
        raise SystemExit("SIGTERM must not race the idle-hook shutdown guard")
    if "sigpending(&pending)" not in io_source:
        raise SystemExit("blocked SIGTERM must be observed before a native tick starts")
    if "m3_runtime_idle_hook_test_set_sigprocmask_failure" not in io_source:
        raise SystemExit("idle hook must have deterministic sigprocmask failure coverage")
    if "m3_runtime_idle_hook_test_set_sigprocmask_restore_failure" not in io_source:
        raise SystemExit("idle hook must have deterministic mask-restoration failure coverage")
    if "m3_runtime_restore_signal_mask(&previous)!=0" not in io_source:
        raise SystemExit("SIGTERM mask-restoration failure must request graceful shutdown")
    if "m3_runtime_idle_hook_test_set_sigterm_observation_failure" not in io_source:
        raise SystemExit("idle hook must have deterministic pending-observation failure coverage")
    if "#ifdef USE_M3_RUNTIME" not in io_source:
        raise SystemExit("io hook seam must be M3-only")
    native_required = (
        "RUNTIME_NATIVE_SNAPSHOT_IDLE_CADENCE_SECONDS 1L",
        "character_save_journal_v2_runtime_native_snapshot_tick(native,1)",
        "SNAPSHOT_TICK_HANDOFF_FAILED",
        "RUNTIME_NATIVE_SNAPSHOT_IDLE_FAILURE_LOG_SECONDS 60L",
    )
    if any(item not in native_source for item in native_required):
        raise SystemExit("native idle tick must be one-token, cadenced, and retry failed handoffs")

    symbols = subprocess.check_output(["nm", "-P", str(args.default_io_object)], text=True)
    if "m3_runtime_" in symbols or "character_save_journal_v2_runtime" in symbols:
        raise SystemExit("default io object leaked an M3 runtime symbol")
    print("m3_runtime_idle_hook_static_test: ok")


if __name__ == "__main__":
    main()
