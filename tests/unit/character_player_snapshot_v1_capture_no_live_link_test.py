#!/usr/bin/env python3
"""Keep PlayerSnapshot capture detached from the default legacy executable."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess


PLAYER_SNAPSHOT_OBJECT_PREFIXES = (
    "character_player_snapshot_v1_",
    "character_snapshot_shadow_outbox.o",
    "player_snapshot_v1.o",
    "object_graph_v1.o",
    "cdto_v1.o",
)
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


def _logical_lines(text: str) -> list[str]:
    lines: list[str] = []
    logical = ""
    for raw in text.splitlines():
        if logical:
            logical += " " + raw.strip()
        else:
            logical = raw
        if logical.rstrip().endswith("\\"):
            logical = logical.rstrip()[:-1]
            continue
        lines.append(logical)
        logical = ""
    if logical:
        raise SystemExit("unterminated Makefile continuation")
    return lines


def _make_variables(makefile: Path, m3_runtime: bool) -> dict[str, set[str]]:
    """Evaluate the object-variable subset of make's conditionals.

    In particular, do not stop at the first OBJECTS assignment: += inside the
    USE_M3_RUNTIME branch is part of the link input when that feature is on.
    """
    variables: dict[str, set[str]] = {}
    active = [True]
    for line in _logical_lines(makefile.read_text(encoding="utf-8")):
        condition = re.match(r"^ifeq\s*\(\$\(USE_M3_RUNTIME\),\s*1\)$", line)
        if condition:
            active.append(active[-1] and m3_runtime)
            continue
        if re.match(r"^(?:ifeq|ifneq|ifdef|ifndef)\b", line):
            # Other Makefile conditionals are irrelevant to object membership,
            # but still need a stack entry so their else/endif pairs parse.
            active.append(active[-1])
            continue
        if line.strip() == "else":
            if len(active) == 1:
                raise SystemExit("unexpected Makefile else")
            active[-1] = active[-2] and not active[-1]
            continue
        if line.strip() == "endif":
            if len(active) == 1:
                raise SystemExit("unexpected Makefile endif")
            active.pop()
            continue
        if not active[-1]:
            continue
        match = re.match(
            r"^([A-Za-z_][A-Za-z0-9_]*)\s*(\+?=|\?=|:=)\s*(.*)$", line
        )
        if not match:
            continue
        name, operator, value = match.groups()
        words = set(value.split())
        if operator == "+=":
            variables.setdefault(name, set()).update(words)
        elif operator == "?=" and name in variables:
            continue
        else:
            variables[name] = words
    if len(active) != 1:
        raise SystemExit("unterminated Makefile conditional")
    return variables


def _expand_words(words: set[str], variables: dict[str, set[str]]) -> set[str]:
    expanded: set[str] = set()
    pending = list(words)
    while pending:
        word = pending.pop()
        match = re.fullmatch(r"\$\(([A-Za-z_][A-Za-z0-9_]*)\)", word)
        if match:
            pending.extend(variables.get(match.group(1), set()))
        elif word.endswith(".o"):
            expanded.add(word)
    return expanded


def linked_objects(makefile: Path, m3_runtime: bool) -> set[str]:
    variables = _make_variables(makefile, m3_runtime)
    return _expand_words(variables.get("OBJECTS", set()), variables)


def assert_link_composition(makefile: Path, audited_objects: set[str]) -> None:
    text = makefile.read_text(encoding="utf-8")
    default = linked_objects(makefile, False)
    enabled = linked_objects(makefile, True)
    relevant_m3 = {
        object_name
        for object_name in enabled
        if object_name.startswith(PLAYER_SNAPSHOT_OBJECT_PREFIXES)
    }
    leaked = sorted(default & relevant_m3)
    if leaked:
        raise SystemExit(
            "PlayerSnapshot capture leaked into feature-off legacy link: "
            + ", ".join(leaked)
        )
    if audited_objects != relevant_m3:
        missing = sorted(relevant_m3 - audited_objects)
        extra = sorted(audited_objects - relevant_m3)
        raise SystemExit(
            "no-live-link audit object set differs from M3 PlayerSnapshot composition; "
            "missing: " + (", ".join(missing) or "none")
            + "; extra: " + (", ".join(extra) or "none")
        )
    if "$(OUTFILE): $(OBJECTS)" not in text or "$(CC) $(CFLAGS) $(OBJECTS)" not in text:
        raise SystemExit("legacy link recipe does not use its complete OBJECTS variable")
    if not re.search(
        r"ifeq \(\$\(USE_M3_RUNTIME\),1\).*?OBJECTS \+= \$\(M3_RUNTIME_OBJECTS\)",
        text,
        re.DOTALL,
    ):
        raise SystemExit("snapshot composition is not gated by USE_M3_RUNTIME")


def _braced_body(text: str, signature: str) -> str:
    start = text.find(signature)
    if start < 0:
        raise SystemExit(f"PlayerSnapshot native caller is missing: {signature}")
    opening = text.find("{", start)
    if opening < 0:
        raise SystemExit(f"PlayerSnapshot native caller has no body: {signature}")
    depth = 0
    for index in range(opening, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                return text[opening + 1 : index]
    raise SystemExit(f"PlayerSnapshot native caller is unterminated: {signature}")


def assert_native_snapshot_handoff_gate(native_source: Path) -> None:
    """Prove the real M3 owner has one exact PlayerSnapshot entry point."""
    text = native_source.read_text(encoding="utf-8")
    if not re.search(
        r'#define RUNTIME_NATIVE_SNAPSHOT_HANDOFF_ENV "MUD_M3_PLAYER_SNAPSHOT_V1"',
        text,
    ) or not re.search(
        r'#define RUNTIME_NATIVE_SNAPSHOT_HANDOFF_VALUE "handoff"', text
    ):
        raise SystemExit("PlayerSnapshot opt-in must retain its exact environment contract")

    gate = _braced_body(text, "static int runtime_native_snapshot_handoff_enabled(void)")
    if not re.search(
        r"return value && !strcmp\(value,RUNTIME_NATIVE_SNAPSHOT_HANDOFF_VALUE\) &&\s*"
        r"player_snapshot_v1_native_abi_supported\(",
        gate,
        re.DOTALL,
    ):
        raise SystemExit(
            "PlayerSnapshot opt-in must require the exact handoff value and ABI gate"
        )
    if text.count("runtime_native_snapshot_handoff_enabled()") != 1:
        raise SystemExit("PlayerSnapshot handoff gate must have exactly one native caller")

    start = _braced_body(text, "static int runtime_native_shadow_start(void *opaque,")
    branch = _braced_body(
        start, "if(runtime_native_snapshot_handoff_enabled())"
    )
    required = (
        "character_player_snapshot_v1_capture_native_init(&native->snapshot_capture);",
        "character_player_snapshot_v1_handoff_init(&native->snapshot_handoff,",
        "character_player_snapshot_v1_handoff_enable_receipt_pair(",
        "configuration.snapshot_handoff=&native->snapshot_handoff;",
        "native->snapshot_handoff_enabled=1;",
    )
    for statement in required:
        if statement not in branch:
            raise SystemExit(
                "PlayerSnapshot capture/outbox setup escaped its exact opt-in branch: "
                + statement
            )
    branch_start = start.find("if(runtime_native_snapshot_handoff_enabled())")
    before_branch = start[:branch_start]
    if "native->snapshot_handoff_enabled=0;" not in before_branch:
        raise SystemExit("PlayerSnapshot default path must explicitly remain disabled")
    for call in required[:-1]:
        if call in before_branch or start[branch_start + len(branch) :].count(call):
            raise SystemExit(
                "PlayerSnapshot capture/outbox setup has a second native entry: " + call
            )


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
    parser.add_argument("--native-source", type=Path, required=True)
    args = parser.parse_args()

    for path in [args.makefile, args.native_source, *args.object, *args.source]:
        if not path.is_file():
            raise SystemExit(f"PlayerSnapshot capture static fixture is missing: {path}")

    audited_objects = {
        path.name.replace("_no_live_link", "") for path in args.object
    }
    assert_link_composition(args.makefile, audited_objects)
    assert_native_snapshot_handoff_gate(args.native_source)

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
