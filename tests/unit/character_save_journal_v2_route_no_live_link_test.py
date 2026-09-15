#!/usr/bin/env python3
"""Prove the test-only route binder is absent from live MUD linkage."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


FORBIDDEN_EXACT = {"save_ply", "load_ply", "read_crt_player", "gateway"}
FORBIDDEN_PREFIXES = (
    "player_store_", "file_player_store_", "write_crt", "player_path_",
    "rp_", "savegame", "gateway_", "db_", "supabase_", "postgres_", "pq",
    "bank_", "onboarding_",
)


def symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", *flags, str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def make_objects(makefile: Path) -> set[str]:
    collected: list[str] = []
    active = False
    for line in makefile.read_text(encoding="utf-8").splitlines():
        if not active:
            match = re.match(r"^OBJECTS\s*=\s*(.*)$", line)
            if not match:
                continue
            active = True
            piece = match.group(1)
        else:
            piece = line.strip()
        collected.extend(piece.rstrip("\\").split())
        if not piece.endswith("\\"):
            return set(collected)
    raise SystemExit("Makefile OBJECTS assignment was not found")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--writer-header", type=Path, required=True)
    args = parser.parse_args()
    if not args.object.is_file() or not args.makefile.is_file() or not args.writer_header.is_file():
        raise SystemExit("route static fixture is missing")
    writer_header = args.writer_header.read_text(encoding="utf-8")
    if "character_save_journal_v2_writer_state" in writer_header:
        raise SystemExit("writer public context leaked a private state type")
    context_match = re.search(
        r"typedef\s+struct\s+character_save_journal_v2_writer_context\s*\{(.*?)\}\s*character_save_journal_v2_writer_context;",
        writer_header,
        re.DOTALL,
    )
    if not context_match or "*" in context_match.group(1) or not re.search(
        r"unsigned\s+char\s+opaque\s*\[", context_match.group(1)
    ):
        raise SystemExit("writer public context is not an opaque stack handle")
    undefined = symbols(args.object, "-u")
    forbidden = sorted(symbol for symbol in undefined if symbol in FORBIDDEN_EXACT or any(symbol.startswith(prefix) for prefix in FORBIDDEN_PREFIXES))
    if forbidden:
        raise SystemExit("route object has live dependency: " + ", ".join(forbidden))
    globals_ = symbols(args.object, "-g")
    if "character_save_journal_v2_route_bind" not in globals_:
        raise SystemExit("route production object did not expose route_bind")
    if any(symbol.endswith("_for_test") for symbol in globals_):
        raise SystemExit("route production object leaked a test hook")
    objects = make_objects(args.makefile)
    if "character_save_journal_v2_route.o" in objects or "character_save_journal_v2_writer.o" in objects:
        raise SystemExit("route or writer object unexpectedly entered live MUD OBJECTS")
    print("character_save_journal_v2_route_no_live_link_test: ok")


if __name__ == "__main__":
    main()
