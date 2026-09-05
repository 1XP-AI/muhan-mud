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
for source, label in ((OWNER, "process owner"), (OWNER_HEADER, "process owner header"),
                      (NATIVE, "native runtime"), (COMMAND, "command handler")):
    require("onboarding_activation_reservation_adapter" not in source,
            f"{label} must not implicitly compose the adapter")
for variable in ("OBJECTS", "M3_RUNTIME_OBJECTS"):
    block = re.search(rf"^{variable}\s*=.*?(?=^\S|\Z)", MAKEFILE,
                      re.MULTILINE | re.DOTALL)
    require(block and "onboarding_activation_reservation_adapter" not in block.group(0),
            f"{variable} must not link the caller-owned adapter")
print("onboarding_activation_reservation_adapter_static_test: ok")
