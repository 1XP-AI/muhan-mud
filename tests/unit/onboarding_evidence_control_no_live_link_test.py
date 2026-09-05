#!/usr/bin/env python3
"""The evidence envelope must remain a parser/formatter-only seam."""

from pathlib import Path
import re


ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "src" / "onboarding_evidence_control.c"
HEADER = ROOT / "src" / "onboarding_evidence_control.h"
MAKEFILE = ROOT / "src" / "Makefile"
FORBIDDEN = ("save_ply", "load_ply", "write_crt", "player_store_", "file_player_store_",
             "player_path_", "onboarding_state", "onboarding_session", "gateway", "socket",
             "descriptor", "relay")


def objects(text: str) -> set[str]:
    match = re.search(r"^OBJECTS\s*=\s*(.*?)(?=^[^\t ].*?=|^\S[^:]*:)", text,
                      re.MULTILINE | re.DOTALL)
    if not match:
        raise SystemExit("OBJECTS assignment was not found")
    return set(match.group(1).replace("\\\n", " ").split())


def main() -> None:
    text = SOURCE.read_text(encoding="utf-8") + HEADER.read_text(encoding="utf-8")
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.DOTALL).lower()
    blocked = [token for token in FORBIDDEN if token in text]
    if blocked:
        raise SystemExit("evidence boundary names game integration: " + ", ".join(blocked))
    if "onboarding_evidence_control.o" in objects(MAKEFILE.read_text(encoding="utf-8")):
        raise SystemExit("evidence boundary is linked into live OBJECTS")
    print("onboarding_evidence_control_no_live_link_test: ok")


if __name__ == "__main__":
    main()
