#!/usr/bin/env python3
"""Keep the 092 composition object out of live MUD linkage."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


FORBIDDEN_PREFIXES = (
    "save_ply",
    "load_ply",
    "write_crt",
    "player_store_",
    "file_player_store_",
    "player_path_",
    "gateway",
    "bank_",
    "pq",
    "postgres_",
    "supabase_",
    "db_",
)
FORBIDDEN_OBJECTS = {
    "bank.o",
    "file_player_store.o",
    "player_path.o",
    "player_store.o",
    "resource_path.o",
}
TEST_ONLY_TOKENS = (
    "_for_test",
    "character_save_journal_v2_testing",
    "character_save_journal_v2_writer_testing",
    "character_save_journal_v2_publish_testing",
    "character_save_journal_v2_ack_testing",
    "character_save_journal_v2_recovery_testing",
)


def symbols(path: Path) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", "-u", str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def global_symbols(path: Path) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", "-g", str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def make_objects(makefile: Path) -> set[str]:
    values: list[str] = []
    collecting = False
    for line in makefile.read_text(encoding="utf-8").splitlines():
        if not collecting:
            match = re.match(r"^OBJECTS\s*=\s*(.*)$", line)
            if not match:
                continue
            part = match.group(1)
            collecting = True
        else:
            part = line.strip()
        values.extend(part.rstrip("\\").split())
        if not part.endswith("\\"):
            break
    if not collecting:
        raise SystemExit("Makefile OBJECTS assignment was not found")
    return set(values)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--map", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--header", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()
    for path in (args.object, args.binary, args.map, args.source, args.header, args.makefile):
        if not path.is_file():
            raise SystemExit(f"092 static fixture is missing: {path}")
    blocked = sorted(
        symbol for symbol in symbols(args.object)
        if any(symbol.startswith(prefix) for prefix in FORBIDDEN_PREFIXES)
    )
    if blocked:
        raise SystemExit("092 object has forbidden live symbols: " + ", ".join(blocked))
    binary_blocked = sorted(
        symbol for symbol in global_symbols(args.binary)
        if any(symbol.startswith(prefix) for prefix in FORBIDDEN_PREFIXES)
    )
    if binary_blocked:
        raise SystemExit("092 production probe exports forbidden live symbols: " + ", ".join(binary_blocked))
    text = args.source.read_text(encoding="utf-8") + args.header.read_text(encoding="utf-8")
    if any(token in text.lower() for token in ("save_ply", "bank_", "gateway", "libpq", "pq")):
        raise SystemExit("092 source/header names a forbidden live integration")
    if any(token in text.lower() for token in TEST_ONLY_TOKENS):
        raise SystemExit("092 production boundary must not expose test hooks")
    if "protocol_durable_acked" in text or "strstr(" in args.source.read_text(encoding="utf-8"):
        raise SystemExit("092 must not suppress exact receipt retries from marker substrings")
    objects = make_objects(args.makefile)
    if "character_save_journal_v2_protocol.o" in objects:
        raise SystemExit("092 protocol object must not be linked into live MUD OBJECTS")
    map_text = args.map.read_text(encoding="utf-8").lower()
    if any(object_name in map_text for object_name in FORBIDDEN_OBJECTS):
        raise SystemExit("092 production link map includes a forbidden live object")
    print("character_save_journal_v2_protocol_no_live_link_test: ok")


if __name__ == "__main__":
    main()
