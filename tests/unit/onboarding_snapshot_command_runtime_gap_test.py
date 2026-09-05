#!/usr/bin/env python3
"""Keep the reservation-to-candidate proof explicitly out of live ownership."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = (ROOT / "src" / "Makefile").read_text(encoding="utf-8")
CONSUMER = (ROOT / "src" / "onboarding_snapshot_command_consumer.c").read_text(encoding="utf-8")
GATE = (ROOT / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")
NATIVE = (ROOT / "src" / "character_save_journal_v2_runtime_native.c").read_text(encoding="utf-8")
COMMAND = (ROOT / "src" / "command1.c").read_text(encoding="utf-8")
BRIDGE = (ROOT / "src" / "onboarding_activation_save_bridge.c").read_text(encoding="utf-8")
PROTOCOL = (ROOT / "src" / "character_save_journal_v2_protocol.c").read_text(encoding="utf-8")
CAPTURE = (ROOT / "src" / "character_player_snapshot_v1_capture.c").read_text(encoding="utf-8")

def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)

require("onboarding_snapshot_command_consumer_reserve" in CONSUMER and
        "int reservation_directory_fd" in CONSUMER,
        "the consumer must remain descriptor-rooted")
require("onboarding_snapshot_command_consumer.o" not in MAKEFILE,
        "the local reservation consumer must not silently join production objects")
for source, label in ((GATE, "activation gate"), (NATIVE, "native runtime"),
                      (COMMAND, "command handler")):
    require("onboarding_snapshot_command_consumer" not in source,
            f"{label} must not claim an unowned reservation descriptor")
require("onboarding_activation_save_runtime_helper_attempt" in GATE and
        "onboarding_activation_gate_bind_owner" in GATE,
        "the existing bounded live path must remain owner/gate based")
require("memcpy(candidate_out->command_uuid, bridge->selected.command_id" in BRIDGE and
        "snprintf(command_uuid, sizeof(command_uuid), \"%s\", candidate.command_uuid)" in PROTOCOL and
        "request->command_uuid" in PROTOCOL[PROTOCOL.index("observe_prepared_stage"):],
        "the existing V4 candidate path must carry its command into PREPARED observation")
require("capture_text_copy(metadata.command_id,sizeof(metadata.command_id),wire.command_uuid)" in CAPTURE,
        "snapshot artifacts must retain the prepared command identity")
print("onboarding_snapshot_command_runtime_gap_test: ok")
