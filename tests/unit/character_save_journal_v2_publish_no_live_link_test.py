#!/usr/bin/env python3
"""Prove the 091b-2 publish boundary remains outside live linkage."""

import argparse
import re
import subprocess
from pathlib import Path


FORBIDDEN = (
    "save_ply", "load_ply", "read_crt_player", "gateway", "bank_",
    "player_store_", "file_player_store_", "player_path_", "onboarding_",
    "db_", "postgres_", "supabase_", "pq", "savegame",
)


def symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(["nm", "-P", *flags, str(path)], text=True)
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def objects(makefile: Path) -> set[str]:
    result: list[str] = []
    active = False
    for line in makefile.read_text(encoding="utf-8").splitlines():
        match = re.match(r"^OBJECTS\s*=\s*(.*)$", line) if not active else None
        if match:
            active = True
            piece = match.group(1)
        elif active:
            piece = line.strip()
        else:
            continue
        result.extend(piece.rstrip("\\").split())
        if not piece.endswith("\\"):
            return set(result)
    raise SystemExit("Makefile OBJECTS assignment was not found")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()
    if not args.object.is_file() or not args.makefile.is_file():
        raise SystemExit("publish static fixture is missing")
    undefined = symbols(args.object, "-u")
    forbidden = sorted(symbol for symbol in undefined if any(symbol == item or symbol.startswith(item) for item in FORBIDDEN))
    if forbidden:
        raise SystemExit("publish object has live dependency: " + ", ".join(forbidden))
    globals_ = symbols(args.object, "-g")
    if "character_save_journal_v2_publish" not in globals_:
        raise SystemExit("publish production object did not expose publish API")
    if any(symbol.endswith("_for_test") for symbol in globals_):
        raise SystemExit("publish production object leaked a test hook")
    if "character_save_journal_v2_publish.o" in objects(args.makefile):
        raise SystemExit("publish object unexpectedly entered live MUD OBJECTS")
    print("character_save_journal_v2_publish_no_live_link_test: ok")


if __name__ == "__main__":
    main()
