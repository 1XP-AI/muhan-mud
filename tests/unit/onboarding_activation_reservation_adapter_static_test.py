#!/usr/bin/env python3
"""Keep the reservation adapter explicitly caller-owned and uncomposed."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "src" / "onboarding_activation_reservation_adapter.c").read_text()
HEADER = (ROOT / "src" / "onboarding_activation_reservation_adapter.h").read_text()
MAKEFILE = (ROOT / "src" / "Makefile").read_text()
OWNER = (ROOT / "src" / "character_save_journal_v2_process_owner.c").read_text()
OWNER_HEADER = (ROOT / "src" / "character_save_journal_v2_process_owner.h").read_text()
NATIVE = (ROOT / "src" / "character_save_journal_v2_runtime_native.c").read_text()
COMMAND = (ROOT / "src" / "command1.c").read_text()
OWNER_BOUNDARY = (ROOT / "src" / "onboarding_activation_reservation_owner.c").read_text()
OWNER_BOUNDARY_HEADER = (ROOT / "src" / "onboarding_activation_reservation_owner.h").read_text()

def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)

require("int reservation_directory_fd" in HEADER and
        "const onboarding_activation_binding *expected" in HEADER,
        "the adapter must require the caller FD and complete expected binding")
for call in ("onboarding_snapshot_command_consumer_reserve(",
             "onboarding_snapshot_command_consumer_read(",
             "onboarding_activation_binding_read(",
             "onboarding_activation_save_bridge_begin("):
    require(call in SOURCE, f"adapter must use explicit boundary: {call}")
require(SOURCE.index("onboarding_snapshot_command_consumer_read(") <
        SOURCE.index("onboarding_activation_binding_read(") <
        SOURCE.index("onboarding_activation_save_bridge_begin("),
        "retained reservation and source identity must be revalidated before bridge install")
for forbidden in ("getenv", "open(", "openat(", "close(", "fcntl(", "dup(",
                  "player_store", "process_owner", "runtime_native", "command1"):
    require(forbidden not in SOURCE + HEADER,
            f"adapter must not own generic runtime behavior: {forbidden}")
require("character_save_journal_v2_process_owner *owner" in OWNER_BOUNDARY_HEADER and
        "const onboarding_activation_binding *expected" in OWNER_BOUNDARY_HEADER,
        "owner boundary must take the real owner and complete binding tuple")
require("onboarding_activation_reservation_adapter_install(" in OWNER_BOUNDARY and
        "owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY" in OWNER_BOUNDARY and
        "!owner->writer_held" in OWNER_BOUNDARY and
        "owner->player_store.state !=\n       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE" in OWNER_BOUNDARY and
        "if(owner->operation_active" in OWNER_BOUNDARY,
        "owner boundary must enforce actual READY/writer/idle/inactive gates")
require(OWNER_BOUNDARY.index("owner->operation_active = 1;") <
        OWNER_BOUNDARY.index("onboarding_activation_reservation_adapter_install(") <
        OWNER_BOUNDARY.index("owner->operation_active = 0;"),
        "owner active state must cover only the adapter attempt")
for forbidden in ("getenv", "open(", "openat(", "close(", "fcntl(", "dup(",
                  "command1", "runtime_native", "snapshot_reservation_enabled"):
    require(forbidden not in OWNER_BOUNDARY + OWNER_BOUNDARY_HEADER,
            f"owner boundary must not take resource ownership or lifecycle flags: {forbidden}")
for source, label in ((OWNER, "process owner"), (OWNER_HEADER, "process owner header"),
                      (NATIVE, "native runtime"), (COMMAND, "command handler")):
    require("onboarding_activation_reservation_adapter" not in source and
            "onboarding_activation_reservation_owner" not in source,
            f"{label} must not implicitly compose the reservation boundary")
require("snapshot_reservation_directory_fd" not in OWNER + OWNER_HEADER,
        "generic process owner must not retain a caller reservation descriptor")
for variable in ("OBJECTS", "M3_RUNTIME_OBJECTS"):
    block = re.search(rf"^{variable}\s*=.*?(?=^\S|\Z)", MAKEFILE,
                      re.MULTILINE | re.DOTALL)
    require(block and "onboarding_activation_reservation_adapter" not in block.group(0) and
            "onboarding_activation_reservation_owner" not in block.group(0),
            f"{variable} must not link the explicit reservation boundary")
print("onboarding_activation_reservation_adapter_static_test: ok")
