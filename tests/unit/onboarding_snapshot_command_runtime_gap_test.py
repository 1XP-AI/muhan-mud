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
STARTUP = OWNER[OWNER.index(
    "character_save_journal_v2_process_owner_start(\n"):
    OWNER.index("\ncharacter_save_journal_v2_process_owner_shutdown(\n")]
SHUTDOWN = OWNER[OWNER.index(
    "character_save_journal_v2_process_owner_shutdown(\n"):
    OWNER.index("\ncharacter_save_journal_v2_process_owner_snapshot_tick(\n")]
CANCEL_START = OWNER[OWNER.index("process_owner_cancel_start("):
                     OWNER.index("\nvoid character_save_journal_v2_process_owner_init(")]
UNBIND = OWNER[OWNER.index("process_owner_unbind_store("):
               OWNER.index("\nstatic character_save_journal_v2_process_owner_startup_result\nprocess_owner_stop_start(")]
UNWIND = OWNER[OWNER.index("process_owner_unwind("):
               OWNER.index("\nstatic void process_owner_unbind_store(")]

def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)

def require_ordered(source: str, terms: tuple[str, ...], message: str) -> None:
    positions = []
    for term in terms:
        require(term in source, f"{message}: missing {term}")
        positions.append(source.index(term))
    require(positions == sorted(positions), message)

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
require(not re.search(r"\b(?:reservation_directory_fd|reservation_descriptor|reservation_fd)\b",
                      OWNER_HEADER + OWNER),
        "the process owner must not add or manage a reservation descriptor")
require_ordered(STARTUP, (
    "owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING;",
    "configuration->acquire_deadline(",
    "configuration->candidate_uuid(",
    "character_save_journal_v2_live_ops_init(",
    "character_save_journal_v2_writer_bootstrap(",
    "character_save_journal_v2_recovery_run_with_stage_observer(",
    "character_save_journal_v2_player_store_init(",
    "character_save_journal_v2_player_store_set_stage_observer(",
    "character_save_journal_v2_player_store_build(",
    "player_store_bind(",
    "owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;"),
    "startup must acquire and install the existing owner lifecycle before READY")
require("character_player_snapshot_v1_handoff_drain(" not in STARTUP and
        not re.search(r"\b(?:character_save_journal_v2_publish|"
                      r"character_save_journal_v2_ack|player_store_save|"
                      r"onboarding_snapshot_command_consumer)\s*\(", OWNER),
        "owner startup must not implicitly save, publish, ACK, or dispatch a reservation consumer")
require("owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY" in SNAPSHOT_TICK and
        "owner->player_store.state !=\n       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE" in SNAPSHOT_TICK and
        "character_player_snapshot_v1_handoff_drain(" in SNAPSHOT_TICK and
        "onboarding_snapshot_command_consumer" not in SNAPSHOT_TICK,
        "the safe READY/idle owner boundary drains only the configured handoff")
require_ordered(SNAPSHOT_TICK, (
    "if(!owner->configuration.snapshot_handoff)",
    "owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY",
    "owner->player_store.state !=\n       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE",
    "owner->operation_active = 1;",
    "character_player_snapshot_v1_handoff_drain("),
    "snapshot drain must remain opt-in and occur only after READY/idle guards")
require(not re.search(r"\b(?:character_save_journal_v2_publish|"
                      r"character_save_journal_v2_ack|player_store_save|"
                      r"onboarding_snapshot_command_consumer|player_store_bind|"
                      r"player_store_unbind)\s*\(", SNAPSHOT_TICK),
        "the explicit snapshot tick must not save, publish, ACK, bind, or dispatch a consumer")
require("player_store_unbind(&owner->player_store_binding);" in UNBIND and
        "owner->player_store_installed = 0;" in UNBIND,
        "owner shutdown must remove its PlayerStore binding before writer release")
require_ordered(SHUTDOWN, (
    "owner->operation_active = 1;",
    "process_owner_unbind_store(owner);",
    "character_save_journal_v2_writer_close(&owner->held_writer)",
    "owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;",
    "owner->operation_active = 0;"),
    "shutdown must unbind before closing the writer and then stop the owner")
require("if(owner->operation_active)" in SHUTDOWN and
        "owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING" in SHUTDOWN and
        "owner->shutdown_requested = 1;" in SHUTDOWN and
        SHUTDOWN.index("if(owner->operation_active)") <
        SHUTDOWN.index("process_owner_unbind_store(owner);") and
        "owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED" in SHUTDOWN and
        SHUTDOWN.count("character_save_journal_v2_writer_close(") == 1,
        "active startup shutdown must defer cancellation and stopped shutdown must be idempotent")
require_ordered(CANCEL_START, (
    "process_owner_unbind_store(owner);",
    "process_owner_stop_start(owner,",
    "CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANCELLED, 1)"),
    "deferred startup cancellation must unbind before unwinding the writer")
require("character_save_journal_v2_writer_close(&owner->held_writer)" in UNWIND,
        "cancelled startup must release only the held writer during unwind")
require("memcpy(candidate_out->command_uuid, bridge->selected.command_id" in BRIDGE and
        "snprintf(command_uuid, sizeof(command_uuid), \"%s\", candidate.command_uuid)" in PROTOCOL and
        "request->command_uuid" in PROTOCOL[PROTOCOL.index("observe_prepared_stage"):],
        "the existing V4 candidate path must carry its command into PREPARED observation")
require("capture_text_copy(metadata.command_id,sizeof(metadata.command_id),wire.command_uuid)" in CAPTURE,
        "snapshot artifacts must retain the prepared command identity")
print("onboarding_snapshot_command_runtime_gap_test: ok")
