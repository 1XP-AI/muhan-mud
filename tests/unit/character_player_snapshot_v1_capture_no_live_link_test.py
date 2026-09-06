#!/usr/bin/env python3
"""Keep PlayerSnapshot capture detached from the default legacy executable."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


DETACHED_OBJECTS = {
    "character_player_snapshot_v1_capture.o",
    "character_player_snapshot_v1_capture_native.o",
    "character_player_snapshot_v1_artifact.o",
    "character_player_snapshot_v1_handoff.o",
    "character_player_snapshot_v1_receipt_pair.o",
    "character_snapshot_shadow_outbox.o",
    "player_snapshot_v1.o",
    "object_graph_v1.o",
    "cdto_v1.o",
}
FORBIDDEN_UNDEFINED = {
    "save_ply",
    "save_all_ply",
    "load_ply",
    "gateway",
    "pqconnectdb",
}
FORBIDDEN_SOURCE_TOKENS = (
    "save_ply",
    "save_all_ply",
    "load_ply",
    "gateway",
    "supabase",
    "kubernetes",
)


def default_objects(makefile: Path) -> set[str]:
    lines = makefile.read_text(encoding="utf-8").splitlines()
    values: list[str] = []
    for index, line in enumerate(lines):
        match = re.match(r"^OBJECTS\s*=\s*(.*)$", line)
        if not match:
            continue
        part = match.group(1)
        while True:
            values.extend(part.rstrip("\\").split())
            if not part.endswith("\\"):
                return set(values)
            index += 1
            if index == len(lines):
                raise SystemExit("unterminated Makefile OBJECTS assignment")
            part = lines[index].strip()
    raise SystemExit("Makefile OBJECTS assignment was not found")


def undefined_symbols(path: Path) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", "-u", str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {
        line.split()[0].lstrip("_").lower()
        for line in output.splitlines()
        if line.split()
    }


def global_symbols(path: Path) -> set[str]:
    output = subprocess.check_output(
        ["nm", "-P", "-g", str(path)], text=True, stderr=subprocess.STDOUT
    )
    return {
        line.split()[0].lstrip("_").lower()
        for line in output.splitlines()
        if line.split()
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--object", type=Path, action="append", required=True)
    parser.add_argument("--source", type=Path, action="append", required=True)
    args = parser.parse_args()

    for path in [args.makefile, *args.object, *args.source]:
        if not path.is_file():
            raise SystemExit(f"PlayerSnapshot capture static fixture is missing: {path}")

    leaked = sorted(default_objects(args.makefile) & DETACHED_OBJECTS)
    if leaked:
        raise SystemExit(
            "PlayerSnapshot capture leaked into default legacy OBJECTS: "
            + ", ".join(leaked)
        )

    for path in args.object:
        forbidden = sorted(undefined_symbols(path) & FORBIDDEN_UNDEFINED)
        if forbidden:
            raise SystemExit(
                f"{path.name} references a live save/network boundary: "
                + ", ".join(forbidden)
            )
        hooks = sorted(symbol for symbol in global_symbols(path)
                       if "_test_" in symbol)
        if hooks:
            raise SystemExit(
                f"{path.name} production object exports test hooks: "
                + ", ".join(hooks)
            )

    for path in args.source:
        text = path.read_text(encoding="utf-8").lower()
        leaked = [token for token in FORBIDDEN_SOURCE_TOKENS
                  if re.search(rf"\b{re.escape(token)}\b", text)]
        if leaked:
            raise SystemExit(
                f"{path.name} directly names a live integration boundary: "
                + ", ".join(leaked)
            )

    print("character_player_snapshot_v1_capture_no_live_link_test: ok")


if __name__ == "__main__":
    main()
