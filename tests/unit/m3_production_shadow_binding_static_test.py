#!/usr/bin/env python3
"""Prove the opt-in production M3 shadow binding without starting the MUD."""

from __future__ import annotations

import argparse
import subprocess
from pathlib import Path


REQUIRED_M3_OBJECTS = {
    "character_save_journal_v2_runtime.o",
    "character_save_journal_v2_runtime_native.o",
    "character_save_journal_v2_process_owner.o",
    "character_save_journal_v2_player_store.o",
    "character_save_journal_v2_rpc_transport_native.o",
    "character_save_journal_v2_deadline_native.o",
    "character_save_journal_v2_uuid_native.o",
}


def nm_symbols(path: Path) -> set[str]:
    output = subprocess.check_output(["nm", "-P", str(path)], text=True)
    return {line.split()[0].lstrip("_").lower()
            for line in output.splitlines() if line.split()}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--main-source", type=Path, required=True)
    parser.add_argument("--runtime-source", type=Path, required=True)
    parser.add_argument("--native-source", type=Path, required=True)
    parser.add_argument("--process-owner-source", type=Path, required=True)
    parser.add_argument("--files2-source", type=Path, required=True)
    parser.add_argument("--command8-source", type=Path, required=True)
    parser.add_argument("--default-object", type=Path, action="append", required=True)
    args = parser.parse_args()

    make_text = args.makefile.read_text(encoding="utf-8")
    compile_block_start = make_text.index("ifeq ($(USE_M3_RUNTIME),1)")
    compile_block_end = make_text.index("ifeq ($(USE_RUST_RESOLVER),1)",
                                       compile_block_start)
    compile_block = make_text[compile_block_start:compile_block_end]
    if "CFLAGS += -DUSE_M3_RUNTIME" not in compile_block:
        raise SystemExit("opt-in build does not define USE_M3_RUNTIME")
    object_definition_start = make_text.index("M3_RUNTIME_OBJECTS =")
    runtime_bind_start = make_text.index("ifeq ($(USE_M3_RUNTIME),1)",
                                         object_definition_start)
    m3_object_definition = make_text[object_definition_start:runtime_bind_start]
    m3_objects = {token for token in m3_object_definition.replace("\\", " ").split()
                  if token.endswith(".o")}
    if not REQUIRED_M3_OBJECTS.issubset(m3_objects):
        raise SystemExit("opt-in build omits a required M3 runtime object")
    object_add = make_text.index("OBJECTS += $(M3_RUNTIME_OBJECTS)",
                                 runtime_bind_start)
    nearest_m3_gate = make_text.rfind("ifeq ($(USE_M3_RUNTIME),1)", 0,
                                      object_add)
    if nearest_m3_gate != runtime_bind_start:
        raise SystemExit("M3 runtime object set is not behind the opt-in gate")
    default_object_start = make_text.index("OBJECTS =")
    default_text = make_text[default_object_start:object_definition_start]
    if "$(M3_RUNTIME_OBJECTS)" in default_text or any(
            name in default_text for name in REQUIRED_M3_OBJECTS):
        raise SystemExit("M3 runtime object leaked into default OBJECTS")

    main = args.main_source.read_text(encoding="utf-8")
    runtime = args.runtime_source.read_text(encoding="utf-8")
    native = args.native_source.read_text(encoding="utf-8")
    owner = args.process_owner_source.read_text(encoding="utf-8")
    files2 = args.files2_source.read_text(encoding="utf-8")
    command8 = args.command8_source.read_text(encoding="utf-8")

    if "#ifdef USE_M3_RUNTIME" not in main:
        raise SystemExit("main runtime lifecycle is not opt-in")
    init = main.index("character_save_journal_v2_runtime_native_init(&m3_native);")
    start = main.index("character_save_journal_v2_runtime_start(&m3_runtime)")
    socket = main.index("sock_init(")
    if not init < start < socket:
        raise SystemExit("native M3 runtime must start before socket setup")
    if 'mode_length==6&&!memcmp(mode,"shadow",6)' not in runtime:
        raise SystemExit("runtime does not require exact MUD_M3_MODE=shadow")
    for required in (
        "character_save_journal_v2_player_store_build",
        "player_store_bind(&store_ops",
        "owner->player_store_installed = 1",
    ):
        if required not in owner:
            raise SystemExit("native shadow owner is missing PlayerStore binding")
    if "character_save_journal_v2_process_owner_start(&native->process_owner)" not in native:
        raise SystemExit("native runtime does not start the process-owned shadow")
    if "savegame_nomsg" not in command8 or "save_ply(" not in command8:
        raise SystemExit("savegame_nomsg -> save_ply caller linkage is missing")
    if "savegame(" not in files2 or "save_ply" not in command8:
        raise SystemExit("files2.c -> savegame -> save_ply linkage is missing")

    for object_path in args.default_object:
        symbols = nm_symbols(object_path)
        if any(symbol.startswith("m3_") or
               symbol.startswith("character_save_journal_v2") or
               symbol.startswith("onboarding_activation_gate")
               for symbol in symbols):
            raise SystemExit(f"default object leaked M3 runtime symbols: {object_path}")
    print("m3_production_shadow_binding_static_test: ok")


if __name__ == "__main__":
    main()
