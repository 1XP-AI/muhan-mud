#!/usr/bin/env python3
"""Keep the reservation-to-candidate proof explicitly out of live ownership."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = (ROOT / "src" / "Makefile").read_text(encoding="utf-8")
CONSUMER = (ROOT / "src" / "onboarding_snapshot_command_consumer.c").read_text(encoding="utf-8")
CONSUMER_HEADER = (ROOT / "src" / "onboarding_snapshot_command_consumer.h").read_text(encoding="utf-8")
GATE = (ROOT / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")
NATIVE = (ROOT / "src" / "character_save_journal_v2_runtime_native.c").read_text(encoding="utf-8")
COMMAND = (ROOT / "src" / "command1.c").read_text(encoding="utf-8")
BRIDGE = (ROOT / "src" / "onboarding_activation_save_bridge.c").read_text(encoding="utf-8")
PROTOCOL = (ROOT / "src" / "character_save_journal_v2_protocol.c").read_text(encoding="utf-8")
CAPTURE = (ROOT / "src" / "character_player_snapshot_v1_capture.c").read_text(encoding="utf-8")
OWNER_HEADER = (ROOT / "src" / "character_save_journal_v2_process_owner.h").read_text(encoding="utf-8")
OWNER = (ROOT / "src" / "character_save_journal_v2_process_owner.c").read_text(encoding="utf-8")

CONFIGURATION = OWNER_HEADER[
    OWNER_HEADER.index("typedef struct character_save_journal_v2_process_owner_configuration {"):
    OWNER_HEADER.index("} character_save_journal_v2_process_owner_configuration;")
]
SNAPSHOT_TICK = OWNER[OWNER.index(
    "character_save_journal_v2_process_owner_snapshot_tick("):]

def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)

require("onboarding_snapshot_command_consumer_reserve" in CONSUMER and
        "int reservation_directory_fd" in CONSUMER,
        "the consumer must remain descriptor-rooted")
require("already-open 0700 private reservation directory" in CONSUMER_HEADER and
        "int reservation_directory_fd" in CONSUMER_HEADER and
        "const char *expected_character_id" in CONSUMER_HEADER and
        "onboarding_activation_binding_mode expected_mode" in CONSUMER_HEADER and
        "const char *expected_correlation_id" in CONSUMER_HEADER,
        "the consumer must require an already-open reservation root and its full activation tuple")
require("onboarding_snapshot_command_consumer.o" not in MAKEFILE,
        "the local reservation consumer must not silently join production objects")
for source, label in ((GATE, "activation gate"), (NATIVE, "native runtime"),
                      (COMMAND, "command handler")):
    require("onboarding_snapshot_command_consumer" not in source,
            f"{label} must not claim an unowned reservation descriptor")
require("onboarding_activation_save_runtime_helper_attempt" in GATE and
        "onboarding_activation_gate_bind_owner" in GATE,
        "the existing bounded live path must remain owner/gate based")
require("character_player_snapshot_v1_handoff *snapshot_handoff;" in CONFIGURATION,
        "the process-owner configuration must expose only the existing snapshot handoff seam")
for missing_input in ("reservation_directory_fd", "command_id",
                      "expected_character_id", "expected_mode",
                      "expected_correlation_id"):
    require(missing_input not in CONFIGURATION,
            f"the process-owner configuration must not claim unowned {missing_input}")
for source, label in ((OWNER_HEADER, "process-owner configuration"),
                      (OWNER, "process-owner lifecycle")):
    require("onboarding_snapshot_command_consumer" not in source and
            "reservation_directory_fd" not in source,
            f"{label} must not claim the local reservation consumer or descriptor")
require(not re.search(r"\b(?:open|openat|dup|dup2|fcntl|close)\s*\(", OWNER),
        "the process owner must not open, duplicate, or close a reservation descriptor")
require("owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY" in SNAPSHOT_TICK and
        "owner->player_store.state !=\n       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE" in SNAPSHOT_TICK and
        "character_player_snapshot_v1_handoff_drain(" in SNAPSHOT_TICK and
        "onboarding_snapshot_command_consumer" not in SNAPSHOT_TICK,
        "the safe READY/idle owner boundary drains only the configured handoff")
require("memcpy(candidate_out->command_uuid, bridge->selected.command_id" in BRIDGE and
        "snprintf(command_uuid, sizeof(command_uuid), \"%s\", candidate.command_uuid)" in PROTOCOL and
        "request->command_uuid" in PROTOCOL[PROTOCOL.index("observe_prepared_stage"):],
        "the existing V4 candidate path must carry its command into PREPARED observation")
require("capture_text_copy(metadata.command_id,sizeof(metadata.command_id),wire.command_uuid)" in CAPTURE,
        "snapshot artifacts must retain the prepared command identity")
print("onboarding_snapshot_command_runtime_gap_test: ok")
