#!/usr/bin/env python3
"""Audit the narrow M3 onboarding gate without native resources."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
command = (root / "src" / "command1.c").read_text(encoding="utf-8")
io = (root / "src" / "io.c").read_text(encoding="utf-8")
gate = (root / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")
native = (root / "src" / "character_save_journal_v2_runtime_native.c").read_text(encoding="utf-8")
makefile = (root / "src" / "Makefile").read_text(encoding="utf-8")

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

provision_activated = case_body(function_body("void onboarding_provision"), 7)
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
test_seam = function_body("void onboarding_activation_command_test_deliver_activated")
require("Ply[fd].io->fn(fd, Ply[fd].io->fnparam" in test_seam and
        "MUD1O ACTIVATED|%s" in test_seam,
        "the test seam must dispatch a serialized ACTIVATED record")
require("onboarding_activation_lifecycle_advance" not in test_seam and
        "onboarding_provision(fd, 5" not in test_seam and
        "onboarding_claim(fd, 6" not in test_seam,
        "the test seam must not bypass the descriptor-selected command handler")
require("#ifdef ONBOARDING_ACTIVATION_COMMAND_TESTING\n/* The dynamic harness" in command and
        command.index("#ifdef ONBOARDING_ACTIVATION_COMMAND_TESTING\n/* The dynamic harness") <
        command.index("void onboarding_activation_command_test_deliver_activated"),
        "the command test seam must remain compiled out of production objects")
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
require("onboarding_activation_reservation_owner_attempt(" in gate and
        "onboarding_activation_save_runtime_helper_attempt_bridge(" in gate and
        gate.index("onboarding_activation_reservation_owner_attempt(") <
        gate.index("onboarding_activation_save_runtime_helper_attempt_bridge("),
        "an armed activation must reserve through the owner before bridge dispatch")
require("if(!capability->armed) return ONBOARDING_ACTIVATION_GATE_BYPASS;" in gate and
        gate.index("if(!capability->armed)") <
        gate.index("onboarding_activation_reservation_owner_attempt("),
        "the feature-off gate must bypass before reservation or bridge wiring")
require("activation_gate_reservation_directory_fd" in gate and
        "activation_gate_reservation_directory_fd<0" in gate,
        "the gate must reject armed activation without a caller-owned descriptor")
require("onboarding_snapshot_command_consumer" not in command and
        "onboarding_activation_reservation_owner" not in command,
        "ordinary command, web, login, and name flows must not own reservation wiring")
require("runtime_native_activation_reservation_directory_open" in native and
        "native->activation_reservation_directory_fd=directory;" in native and
        "close(native->activation_reservation_directory_fd);" in native,
        "the explicit native runtime must retain and release its own descriptor")
m3_gate_objects = "OBJECTS += onboarding_snapshot_command_consumer.o onboarding_activation_reservation_adapter.o"
require(m3_gate_objects in makefile and
        makefile.index(m3_gate_objects) > makefile.index("ifeq ($(USE_M3_RUNTIME),1)"),
        "reservation wiring must link only from the explicit M3 runtime graph")
default_objects = makefile[makefile.index("OBJECTS ="):makefile.index("M3_RUNTIME_OBJECTS =")]
require("onboarding_snapshot_command_consumer.o" not in default_objects and
        "onboarding_activation_reservation_owner.o" not in default_objects,
        "the default build must not link reservation or owner runtime wiring")
print("onboarding_activation_gate_static_test: ok")
