#!/usr/bin/env python3
"""Keep the disposable receipt transport out of the live MUD link."""

from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path


def symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(["nm", "-P", *flags, str(path)], text=True)
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def make_assignments(text: str) -> dict[str, set[str]]:
    assignments: dict[str, set[str]] = {}
    logical = ""
    for raw in text.splitlines():
        if logical:
            piece = raw.strip()
            logical += " " + piece
        else:
            if raw.startswith("\t"):
                continue
            logical = raw
        if logical.rstrip().endswith("\\"):
            logical = logical.rstrip()[:-1]
            continue
        match = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)\s*(?:\+|\?|:)?=\s*(.*)$", logical)
        if match:
            assignments.setdefault(match.group(1), set()).update(match.group(2).split())
        logical = ""
    if logical:
        raise SystemExit("unterminated Makefile continuation")
    return assignments


def parser_self_test() -> None:
    fixture = (
        "OBJECTS := global.o \\\n"
        " main.o\n"
        "OBJECTS += receipt.o\n"
        "LIBS ?= -lm \\\n"
        " -lpq\n"
        "EXTRA_LIBS += -pthread\n"
    )
    parsed = make_assignments(fixture)
    if parsed.get("OBJECTS") != {"global.o", "main.o", "receipt.o"}:
        raise SystemExit("Makefile assignment parser lost an OBJECTS operator or continuation")
    if "-lpq" not in parsed.get("LIBS", set()):
        raise SystemExit("Makefile assignment parser lost a continued library value")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--adapter-object", type=Path)
    parser.add_argument("--native-object", type=Path)
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--adapter-source", type=Path, required=True)
    parser.add_argument("--adapter-header", type=Path, required=True)
    parser.add_argument("--native-source", type=Path, required=True)
    parser.add_argument("--native-header", type=Path, required=True)
    args = parser.parse_args()
    parser_self_test()
    assignments = make_assignments(args.makefile.read_text(encoding="utf-8"))
    objects = assignments.get("OBJECTS", set())
    for forbidden in ("character_save_journal_v2_receipt_transport.o", "character_save_journal_v2_receipt_transport_native.o"):
        if forbidden in objects:
            raise SystemExit("receipt transport entered live MUD OBJECTS")
    library_values = assignments.get("LIBS", set()) | assignments.get("EXTRA_LIBS", set())
    if any("pq" in value.lower() or "postgres" in value.lower() for value in library_values):
        raise SystemExit("receipt transport leaked libpq into generic LIBS or EXTRA_LIBS")
    source = args.adapter_source.read_text(encoding="utf-8")
    forbidden_text = ("getenv", "fopen", "open(", "read(", "fprintf", "perror", "syslog", "PQ")
    if any(token in source for token in forbidden_text):
        raise SystemExit("generic adapter reads, logs, or exposes transport details")
    headers = args.adapter_header.read_text(encoding="utf-8") + args.native_header.read_text(encoding="utf-8")
    if "for_test" in source or "for_test" in args.native_source.read_text(encoding="utf-8") or "for_test" in headers:
        raise SystemExit("receipt transport test hook leaked")
    if args.adapter_object:
        undefined = symbols(args.adapter_object, "-u")
        if any(symbol.startswith("pq") for symbol in undefined):
            raise SystemExit("generic adapter has libpq undefined symbols")
        globals_ = symbols(args.adapter_object, "-g")
        if any(symbol.endswith("_for_test") for symbol in globals_):
            raise SystemExit("generic adapter object leaked a test hook")
    if args.native_object:
        undefined = symbols(args.native_object, "-u")
        if not any(symbol.startswith("pq") for symbol in undefined):
            raise SystemExit("isolated native object did not contain libpq binding")
    print("character_save_journal_v2_receipt_transport_no_live_link_test: ok")


if __name__ == "__main__":
    main()
