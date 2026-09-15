#!/usr/bin/env python3
"""Prove the 091a production object has no live-writer edge or test hook."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


FORBIDDEN_EXACT = {
    "save_ply",
    "load_ply",
    "read_crt_player",
    "gateway",
}
FORBIDDEN_PREFIXES = (
    "player_store_",
    "file_player_store_",
    "write_crt",
    "player_path_",
    "rp_",
    "savegame",
    "gateway_",
    "db_",
    "supabase_",
    "postgres_",
    "pq",  # libpq exports are PQ*; symbols are normalized to lowercase.
    "bank_",
)
FORBIDDEN_OBJECTS = {
    "bank.o",
    "file_player_store.o",
    "onboarding_admission.o",
    "onboarding_receipt.o",
    "onboarding_recovery.o",
    "onboarding_session.o",
    "player_path.o",
    "player_store.o",
    "resource_path.o",
    "trusted_admission.o",
}
CRASH_TEST_RUNTIME = {"getenv", "kill", "strtoul", "exit"}


def normalize_symbol(raw: str) -> str:
    symbol = raw.strip()
    if symbol.startswith("_"):
        symbol = symbol[1:]
    return symbol.lower()


def nm_symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", *flags, str(path)], text=True, stderr=subprocess.STDOUT
    )
    result: set[str] = set()
    for line in output.splitlines():
        fields = line.split()
        if fields:
            result.add(normalize_symbol(fields[0]))
    return result


def forbidden_symbols(symbols: set[str]) -> list[str]:
    return sorted(
        symbol
        for symbol in symbols
        if symbol in FORBIDDEN_EXACT
        or any(symbol.startswith(prefix) for prefix in FORBIDDEN_PREFIXES)
    )


def make_objects(makefile: Path) -> set[str]:
    lines = makefile.read_text(encoding="utf-8").splitlines()
    value: list[str] = []
    collecting = False
    for line in lines:
        if not collecting:
            match = re.match(r"^OBJECTS\s*=\s*(.*)$", line)
            if not match:
                continue
            part = match.group(1)
            collecting = True
        else:
            part = line.strip()
        continued = part.endswith("\\")
        value.extend(part.rstrip("\\").split())
        if not continued:
            break
    if not collecting:
        raise SystemExit("Makefile OBJECTS assignment was not found")
    return set(value)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--map", dest="link_map", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()

    for path in (args.binary, args.object, args.link_map, args.makefile):
        if not path.is_file():
            raise SystemExit(f"v2 static fixture is missing: {path}")

    undefined = nm_symbols(args.object, "-u") | nm_symbols(args.binary, "-u")
    forbidden = forbidden_symbols(undefined)
    if forbidden:
        raise SystemExit(
            "v2 production object has forbidden live symbols: "
            + ", ".join(forbidden)
        )
    crash_runtime = sorted(undefined & CRASH_TEST_RUNTIME)
    if crash_runtime:
        raise SystemExit(
            "v2 production object references crash-test runtime: "
            + ", ".join(crash_runtime)
        )

    globals_ = nm_symbols(args.object, "-g")
    if "character_save_journal_v2_prepare" not in globals_:
        raise SystemExit("v2 production probe did not expose the expected module")
    test_hooks = sorted(symbol for symbol in globals_ if symbol.endswith("_for_test"))
    if test_hooks:
        raise SystemExit(
            "v2 production object leaked test hooks: " + ", ".join(test_hooks)
        )

    link_text = args.link_map.read_text(errors="replace").lower()
    if args.object.name.lower() not in link_text:
        raise SystemExit("fresh production link map does not contain the audited v2 object")
    linked_forbidden = sorted(
        name for name in FORBIDDEN_OBJECTS if name.lower() in link_text
    )
    if linked_forbidden:
        raise SystemExit(
            "v2 production probe linked forbidden live objects: "
            + ", ".join(linked_forbidden)
        )

    objects = make_objects(args.makefile)
    if "character_save_journal_v2.o" in objects:
        raise SystemExit("live MUD OBJECTS unexpectedly contains the v2 test-only module")

    print("character_save_journal_v2_no_live_link_test: ok")


if __name__ == "__main__":
    main()
