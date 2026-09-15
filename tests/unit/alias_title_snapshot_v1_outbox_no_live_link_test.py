#!/usr/bin/env python3
"""Prove the detached AliasTitleSnapshotV1 outbox cannot become live by accident."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


PRODUCTION_LISTS = ("OBJECTS", "M3_RUNTIME_OBJECTS")
FORBIDDEN_OBJECT = "alias_title_snapshot_v1_outbox.o"
TEST_HOOK_PREFIX = "alias_title_snapshot_v1_outbox_test_"


def global_symbols(path: Path) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", "-g", str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines()
            if line.split()}


def make_objects(makefile: Path, variable: str) -> set[str]:
    values: list[str] = []
    found = False
    lines = makefile.read_text(encoding="utf-8").splitlines()
    pattern = re.compile(rf"^{re.escape(variable)}\s*(?:\+?=|:=|\?=)\s*(.*)$")
    index = 0
    while index < len(lines):
        match = pattern.match(lines[index])
        if not match:
            index += 1
            continue
        found = True
        part = match.group(1)
        while True:
            values.extend(part.rstrip("\\").split())
            if not part.endswith("\\"):
                break
            index += 1
            if index == len(lines):
                raise SystemExit(f"unterminated Makefile {variable} assignment")
            part = lines[index].strip()
        index += 1
    if not found:
        raise SystemExit(f"Makefile {variable} assignment was not found")
    return set(values)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--header", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()

    for path in (args.object, args.source, args.header, args.makefile):
        if not path.is_file():
            raise SystemExit(f"alias title outbox static fixture is missing: {path}")

    listed_in = [
        variable for variable in PRODUCTION_LISTS
        if FORBIDDEN_OBJECT in make_objects(args.makefile, variable)
    ]
    if listed_in:
        raise SystemExit(
            "detached outbox leaked into production object lists: "
            + ", ".join(listed_in)
        )

    exported = sorted(
        symbol for symbol in global_symbols(args.object)
        if symbol.startswith(TEST_HOOK_PREFIX)
    )
    if exported:
        raise SystemExit(
            "production outbox object exports test fault hooks: "
            + ", ".join(exported)
        )

    source = args.source.read_text(encoding="utf-8")
    header = args.header.read_text(encoding="utf-8")
    guard = "#ifdef ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING"
    if guard not in source or guard not in header:
        raise SystemExit("outbox test fault hooks must have explicit test-only guards")
    print("alias_title_snapshot_v1_outbox_no_live_link_test: ok")


if __name__ == "__main__":
    main()
