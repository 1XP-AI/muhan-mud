#!/usr/bin/env python3
"""Prove the trusted-alias context stays a pure, detached provider."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


DETACHED_PROVIDER_OBJECT = "trusted_alias_identity_context_v1.o"
ALLOWED_UNDEFINED = {
    "alias_title_snapshot_manifest_v1_encode",
    "cdto_v1_free_wire",
    "memcpy", "memcpy_chk",
    "memset", "memset_chk",
}
ALLOWED_HEADER_INCLUDES = {"alias_title_snapshot_manifest_v1.h"}
ALLOWED_SOURCE_INCLUDES = {
    "trusted_alias_identity_context_v1.h",
    "cdto_v1.h",
}
FORBIDDEN_CAPABILITIES = (
    "alias.c", "alias_title_snapshot_v1_outbox", "fopen", "open(",
    "socket", "connect(", "getenv", "mstruct.h", "player_store",
    "save_ply", "load_ply", "pq", "postgres", "supabase",
)


def symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", *flags, str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {line.split()[0].lstrip("_") for line in output.splitlines()
            if line.split()}


def assignments(makefile: Path, variable: str) -> list[str]:
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
    return values


def default_objects(makefile: Path) -> set[str]:
    text = makefile.read_text(encoding="utf-8")
    try:
        default = text[text.index("OBJECTS ="):text.index("M3_RUNTIME_OBJECTS =")]
    except ValueError as error:
        raise SystemExit("Makefile default/M3 object boundaries were not found") from error
    return set(re.findall(r"\b[A-Za-z0-9_]+\.o\b", default))


def direct_includes(text: str) -> set[str]:
    return set(re.findall(r'^\s*#\s*include\s+"([^"]+)"', text, re.MULTILINE))


def code_without_comments(text: str) -> str:
    return re.sub(r"/\*.*?\*/|//[^\n]*", "", text, flags=re.DOTALL)


def require_detached_composition(makefile: Path) -> None:
    compositions = {
        "default OBJECTS": default_objects(makefile),
        "M3_RUNTIME_OBJECTS": set(assignments(makefile, "M3_RUNTIME_OBJECTS")),
        "all OBJECTS assignments": set(assignments(makefile, "OBJECTS")),
    }
    for name, objects in compositions.items():
        if DETACHED_PROVIDER_OBJECT in objects:
            raise SystemExit(
                f"detached provider leaked into {name}: {DETACHED_PROVIDER_OBJECT}"
            )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--header", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    args = parser.parse_args()

    for path in (args.object, args.source, args.header, args.makefile):
        if not path.is_file():
            raise SystemExit("trusted alias context static fixture is missing: " + str(path))

    source = args.source.read_text(encoding="utf-8")
    header = args.header.read_text(encoding="utf-8")
    if direct_includes(header) != ALLOWED_HEADER_INCLUDES:
        raise SystemExit("trusted context header gained an unapproved dependency")
    if direct_includes(source) != ALLOWED_SOURCE_INCLUDES:
        raise SystemExit("trusted context implementation gained an unapproved dependency")
    for path, text in ((args.header, header), (args.source, source)):
        code = code_without_comments(text)
        for forbidden in FORBIDDEN_CAPABILITIES:
            if forbidden in code:
                raise SystemExit(f"forbidden detached-boundary capability in {path.name}: {forbidden}")

    require_detached_composition(args.makefile)
    undefined = symbols(args.object, "-u")
    unexpected = sorted(undefined - ALLOWED_UNDEFINED)
    if unexpected:
        raise SystemExit("trusted context object has unapproved external dependency: "
                         + ", ".join(unexpected))
    print("trusted_alias_identity_context_v1_static_test: ok")


if __name__ == "__main__":
    main()
