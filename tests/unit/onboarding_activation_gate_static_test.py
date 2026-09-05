#!/usr/bin/env python3
"""Audit the narrow M3 onboarding gate without native resources."""
from pathlib import Path

root = Path(__file__).resolve().parents[2]
command = (root / "src" / "command1.c").read_text(encoding="utf-8")
io = (root / "src" / "io.c").read_text(encoding="utf-8")
gate = (root / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")

def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)

require("onboarding_activation_gate_advance(fd, control.command_id)" in command,
        "both accepted ACTIVATED call sites must enter the gate")
require(command.count("onboarding_activation_gate_advance(fd, control.command_id)") == 2,
        "provision and claim must be the only direct ACTIVATED gate callers")
require("onboarding_activation_gate_idle_retry();" in io,
        "retained activation must retry at the serialized host idle boundary")
require("onboarding_activation_pending) return 1;" in command,
        "retained activation must not accept socket input")
require("ONBOARDING_ACTIVATION_GATE_RETAINED" in command and
        "onboarding_activation_wait" in command,
        "PREPARED must retain its descriptor state")
require("onboarding_send_active(fd," in command and
        command.index("onboarding_activation_complete") < command.index(
            "onboarding_activation_gate_advance(fd, control.command_id)"),
        "ACTIVE must be emitted only by post-consumption completion")
require("libpq" not in command and "PQconnect" not in command and
        "runtime_native" not in command,
        "command1 must not own native or libpq resources")
require("character_save_journal_v2_player_store_set_candidate_resolver" not in command,
        "ordinary command handling must not install the V4 resolver")
require("onboarding_activation_save_runtime_helper_attempt" in gate and
        "activation_gate_owner" in gate,
        "gate must dispatch only through the active process owner")
print("onboarding_activation_gate_static_test: ok")
