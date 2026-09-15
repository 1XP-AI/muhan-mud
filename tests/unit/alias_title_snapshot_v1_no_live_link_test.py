#!/usr/bin/env python3
"""Verify the AliasTitleSnapshotV1 observer remains a test-only alias seam."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


FORBIDDEN_PREFIXES = (
    "alias_title_snapshot_v1_",
    "cdto_v1_",
)
FORBIDDEN_OBJECTS = {
    "alias_title_snapshot_v1.o",
    "cdto_v1.o",
}


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
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()
    for path in (args.object, args.source, args.makefile):
        if not path.is_file():
            raise SystemExit(f"alias observer static fixture is missing: {path}")
    blocked = sorted(
        symbol for symbol in symbols(args.object)
        if any(symbol.startswith(prefix) for prefix in FORBIDDEN_PREFIXES)
    )
    if blocked:
        raise SystemExit("production alias object has test-seam symbols: " + ", ".join(blocked))
    exported = sorted(
        symbol for symbol in global_symbols(args.object)
        if symbol.startswith("alias_title_snapshot_v1_observer_")
    )
    if exported:
        raise SystemExit("production alias object exports test observer API: " + ", ".join(exported))
    source = args.source.read_text(encoding="utf-8")
    if "#ifdef ALIAS_TITLE_SNAPSHOT_V1_TEST_SEAM" not in source:
        raise SystemExit("alias observer seam must have an explicit test-only compile guard")
    objects = make_objects(args.makefile)
    present = sorted(FORBIDDEN_OBJECTS & objects)
    if present:
        raise SystemExit("production OBJECTS links observer codec objects: " + ", ".join(present))
    print("alias_title_snapshot_v1_no_live_link_test: ok")


if __name__ == "__main__":
    main()
