#!/usr/bin/env python3
"""Prove MUD1C stays feature-off and detached from legacy admission."""

from __future__ import annotations

from pathlib import Path
import re
import sys


def default_objects(makefile: str) -> set[str]:
    try:
        text = makefile[makefile.index("OBJECTS ="):makefile.index("M3_RUNTIME_OBJECTS =")]
    except ValueError as error:
        raise SystemExit("Makefile object boundary was not found") from error
    return set(re.findall(r"\b[A-Za-z0-9_]+\.o\b", text))


def includes(text: str) -> set[str]:
    return set(re.findall(r'^\s*#\s*include\s+"([^"]+)"', text, re.MULTILINE))


def uncommented(text: str) -> str:
    return re.sub(r"/\*.*?\*/|//[^\n]*", "", text, flags=re.DOTALL)


def main() -> None:
    root = Path(__file__).resolve().parents[2]
    source = root / "src/mud1c_admission_context.c"
    header = root / "src/mud1c_admission_context.h"
    makefile = root / "src/Makefile"
    for path in (source, header, makefile):
        if not path.is_file():
            raise SystemExit(f"MUD1C fixture missing: {path}")
    source_text = source.read_text(encoding="utf-8")
    header_text = header.read_text(encoding="utf-8")
    if includes(source_text) != {"mud1c_admission_context.h"}:
        raise SystemExit("MUD1C source gained a non-standard dependency")
    if includes(header_text):
        raise SystemExit("MUD1C header gained a project dependency")
    if "#if MUD1C_ADMISSION_CONTEXT_ENABLED" not in source_text:
        raise SystemExit("MUD1C codec lost its compile-time feature gate")
    if "mud1c_admission_context.o" in default_objects(makefile.read_text(encoding="utf-8")):
        raise SystemExit("MUD1C codec leaked into the legacy default object graph")
    forbidden = ("trusted_admission", "onboarding_admission", "socket", "connect(",
                 "fopen", "open(", "getenv", "player_store", "save_ply",
                 "load_ply", "postgres", "supabase")
    for needle in forbidden:
        if needle in uncommented(source_text) or needle in uncommented(header_text):
            raise SystemExit(f"forbidden MUD1C capability: {needle}")
    print("mud1c_admission_context_static_test: ok")


if __name__ == "__main__":
    main()
