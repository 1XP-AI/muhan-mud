#!/usr/bin/env python3
"""Audit the narrow M3 onboarding gate without native resources."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
command = (root / "src" / "command1.c").read_text(encoding="utf-8")
io = (root / "src" / "io.c").read_text(encoding="utf-8")
gate = (root / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")

def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)

def function_body(name: str) -> str:
    start = command.index(name)
    opening = command.index("{", start)
    depth = 0
    for position in range(opening, len(command)):
        if command[position] == "{":
            depth += 1
        elif command[position] == "}":
            depth -= 1
            if depth == 0:
                return command[opening + 1:position]
    raise SystemExit(f"unterminated function: {name}")

def case_body(body: str, number: int) -> str:
    match = re.search(rf"^\s*case {number}:", body, re.MULTILINE)
    if not match:
        raise SystemExit(f"missing case {number}")
    following = re.search(r"^\s*(?:case \d+|default):", body[match.end():],
                          re.MULTILINE)
    end = match.end() + following.start() if following else len(body)
    return body[match.start():end]

provision_activated = case_body(function_body("void onboarding_provision"), 5)
claim_activated = case_body(function_body("void onboarding_claim"), 6)

for label, activated in (("provision", provision_activated),
                         ("claim", claim_activated)):
    require("ONBOARDING_CONTROL_ACTIVATED" in activated,
            f"{label} must validate its ACTIVATED case")
    require(activated.count("onboarding_activation_lifecycle_advance(fd, control.command_id)") == 1,
            f"{label} must enter the shared lifecycle exactly once")
    lifecycle_call = activated.index(
        "onboarding_activation_lifecycle_advance(fd, control.command_id)")
    require("onboarding_send_active(fd," not in activated[:lifecycle_call],
            f"{label} must not emit ACTIVE before the lifecycle boundary")

require(command.count("onboarding_activation_lifecycle_advance(fd, control.command_id)") == 2,
        "provision and claim must be the only direct ACTIVATED lifecycle callers")
require("onboarding_activation_gate_idle_retry();" in io,
        "retained activation must retry at the serialized host idle boundary")
require("onboarding_activation_lifecycle_advance(fd," in command and
        "onboarding_activation_gate_idle_retry" in command,
        "the idle retry must use the same lifecycle boundary as commands")
require("onboarding_activation_pending) return 1;" in command,
        "retained activation must not accept socket input")
require("ONBOARDING_ACTIVATION_GATE_RETAINED" in command and
        "onboarding_activation_wait" in command,
        "PREPARED must retain its descriptor state")
require("onboarding_send_active(fd," in command and
        command.index("onboarding_activation_complete") < command.index(
            "onboarding_activation_gate_advance(fd, command_id)"),
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
