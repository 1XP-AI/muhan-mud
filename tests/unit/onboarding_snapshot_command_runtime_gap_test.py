#!/usr/bin/env python3
"""Audit native ownership of the M3 onboarding reservation descriptor."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = (ROOT / "src" / "Makefile").read_text(encoding="utf-8")
NATIVE = (ROOT / "src" / "character_save_journal_v2_runtime_native.c").read_text(
    encoding="utf-8")
NATIVE_HEADER = (ROOT / "src" / "character_save_journal_v2_runtime_native.h").read_text(
    encoding="utf-8")
OWNER = (ROOT / "src" / "character_save_journal_v2_process_owner.c").read_text(
    encoding="utf-8")
OWNER_HEADER = (ROOT / "src" / "character_save_journal_v2_process_owner.h").read_text(
    encoding="utf-8")
GATE = (ROOT / "src" / "onboarding_activation_gate.c").read_text(encoding="utf-8")
CONSUMER = (ROOT / "src" / "onboarding_snapshot_command_consumer.c").read_text(
    encoding="utf-8")
CONSUMER_HEADER = (ROOT / "src" / "onboarding_snapshot_command_consumer.h").read_text(
    encoding="utf-8")

DEFAULT_OBJECTS = MAKEFILE[
    MAKEFILE.index("OBJECTS ="):MAKEFILE.index("M3_RUNTIME_OBJECTS =")
]
M3_LINK_BLOCK = MAKEFILE[
    MAKEFILE.index("ifeq ($(USE_M3_RUNTIME),1)", MAKEFILE.index("M3_RUNTIME_OBJECTS =")):
    MAKEFILE.index("$(OUTFILE): $(OBJECTS)")
]
NATIVE_OPEN = NATIVE[NATIVE.index(
    "static int runtime_native_activation_reservation_directory_open("):
    NATIVE.index("/* This owns exactly the resources")]
NATIVE_SHUTDOWN = NATIVE[NATIVE.index(
    "static void runtime_native_shadow_shutdown("):
    NATIVE.index("static int runtime_native_shadow_start(")]


def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)


# The reservation consumer is a deliberate part of the optional M3 runtime,
# never of the normal object graph.
for object_name in (
    "onboarding_snapshot_command_consumer.o",
    "onboarding_activation_reservation_adapter.o",
    "onboarding_activation_reservation_owner.o",
):
    require(object_name in M3_LINK_BLOCK,
            f"{object_name} must link only with USE_M3_RUNTIME=1")
    require(object_name not in DEFAULT_OBJECTS,
            f"the default non-M3 object graph must exclude {object_name}")

# Only the native runtime retains the protected directory descriptor: it opens
# a private root, stores the descriptor, and closes that same stored value.
require("int activation_reservation_directory_fd;" in NATIVE_HEADER and
        "Native runtime owns this one private descriptor" in NATIVE_HEADER,
        "the native runtime must declare the protected descriptor as its storage")
require("open(native->muhan_home" in NATIVE_OPEN and
        "openat(root,RUNTIME_NATIVE_ACTIVATION_RESERVATION_DIRECTORY" in NATIVE_OPEN and
        "native->activation_reservation_directory_fd=directory;" in NATIVE_OPEN,
        "native runtime startup must open and retain the private reservation directory")
require("close(native->activation_reservation_directory_fd);" in NATIVE_SHUTDOWN and
        "native->activation_reservation_directory_fd=-1;" in NATIVE_SHUTDOWN,
        "native runtime shutdown must release its retained descriptor")
require("Returns the native caller-owned reservation directory" in NATIVE_HEADER and
        "return native->activation_reservation_directory_fd;" in NATIVE,
        "the native accessor must borrow the descriptor without transfer or duplication")

# The generic process owner remains lifecycle-only.  Reservation participants
# receive a descriptor from their caller and must not open or close that root.
require(not re.search(r"\b(?:reservation_directory_fd|activation_reservation_directory_fd)\b",
                      OWNER + OWNER_HEADER),
        "the generic process owner must not store or manage the protected descriptor")
require(not re.search(r"\b(?:open|openat|dup|dup2|fcntl|close)\s*\(", OWNER),
        "the generic process owner must not acquire or release reservation descriptors")
require("activation_gate_reservation_directory_fd" in GATE and
        "onboarding_activation_reservation_owner_attempt(" in GATE and
        not re.search(r"\b(?:open|openat|dup|dup2|fcntl|close)\s*\(", GATE),
        "the activation gate may borrow and forward, but not own, the descriptor")
require("int reservation_directory_fd" in CONSUMER_HEADER and
        "onboarding_snapshot_command_consumer_reserve(reservation_directory_fd" in CONSUMER and
        not re.search(r"\bopen\s*\(", CONSUMER),
        "the reservation consumer must require a supplied directory descriptor")

print("onboarding_snapshot_command_runtime_gap_test: ok")
