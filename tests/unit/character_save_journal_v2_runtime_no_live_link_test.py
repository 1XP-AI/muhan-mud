#!/usr/bin/env python3
"""Keep M3 runtime ownership and its libpq adapter out of the legacy link."""

from __future__ import annotations

import argparse
import re
import subprocess
import tempfile
from pathlib import Path


def symbols(path: Path, *flags: str) -> set[str]:
    output = subprocess.check_output(["nm", "-P", *flags, str(path)], text=True)
    return {line.split()[0].lstrip("_").lower() for line in output.splitlines() if line.split()}


def assignments(text: str) -> dict[str, set[str]]:
    found: dict[str, set[str]] = {}
    logical = ""
    for raw in text.splitlines():
        if logical:
            logical += " " + raw.strip()
        elif not raw.startswith("\t"):
            logical = raw
        if not logical:
            continue
        if logical.rstrip().endswith("\\"):
            logical = logical.rstrip()[:-1]
            continue
        match = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)\s*(?:\+|\?|:)?=\s*(.*)$", logical)
        if match:
            found.setdefault(match.group(1), set()).update(match.group(2).split())
        logical = ""
    if logical:
        raise SystemExit("unterminated Makefile continuation")
    return found


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--runtime-object", type=Path)
    parser.add_argument("--native-object", type=Path)
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--runtime-source", type=Path, required=True)
    parser.add_argument("--runtime-header", type=Path, required=True)
    parser.add_argument("--native-source", type=Path, required=True)
    parser.add_argument("--native-header", type=Path, required=True)
    parser.add_argument("--main-source", type=Path, required=True)
    args = parser.parse_args()

    make_text = args.makefile.read_text(encoding="utf-8")
    enabled_block = re.compile(
        r"^ifeq \(\$\(USE_M3_RUNTIME\),1\)\n.*?^endif\n", re.MULTILINE | re.DOTALL
    )
    default_make_text = enabled_block.sub("", make_text)
    if default_make_text == make_text:
        raise SystemExit("M3 runtime opt-in Makefile block is missing")
    if "character_save_journal_v2_runtime.o character_save_journal_v2_runtime_native.o" not in make_text:
        raise SystemExit("M3 runtime opt-in objects are missing")
    for required in {
        "character_save_journal_v2_process_owner.o",
        "character_save_journal_v2_rpc_transport_native.o",
        "character_save_journal_v2_deadline_native.o",
        "character_save_journal_v2_uuid_native.o",
        "character_save_journal_v2_player_store.o",
    }:
        if required not in make_text:
            raise SystemExit(f"M3 shadow runtime omits required object {required}")
    if "M3_RUNTIME_LIBS" not in make_text:
        raise SystemExit("M3 runtime opt-in libpq link flags are missing")
    make = assignments(default_make_text)
    objects = make.get("OBJECTS", set())
    forbidden = {"character_save_journal_v2_runtime.o", "character_save_journal_v2_runtime_native.o"}
    if objects & forbidden:
        raise SystemExit("M3 runtime entered default live MUD OBJECTS")
    libraries = make.get("LIBS", set()) | make.get("EXTRA_LIBS", set())
    if any("pq" in value.lower() or "postgres" in value.lower() for value in libraries):
        raise SystemExit("M3 runtime leaked libpq into generic LIBS or EXTRA_LIBS")
    clean_match = re.search(r"^clean:\s*\n((?:\t.*\n)+)", make_text, re.MULTILINE)
    if not clean_match or "$(M3_RUNTIME_OBJECTS)" not in clean_match.group(1):
        raise SystemExit("default clean does not remove stale M3 runtime objects")
    with tempfile.TemporaryDirectory(prefix="m3-runtime-clean-") as temporary:
        stale = Path(temporary) / "character_save_journal_v2_runtime.o"
        stale_native = Path(temporary) / "character_save_journal_v2_runtime_native.o"
        stale.write_bytes(b"stale")
        stale_native.write_bytes(b"stale")
        subprocess.run(
            ["make", "-s", "-C", temporary, "-f", str(args.makefile.resolve()), "clean"],
            check=True,
        )
        if stale.exists() or stale_native.exists():
            raise SystemExit("default clean left stale M3 runtime objects")

    runtime_text = args.runtime_source.read_text(encoding="utf-8")
    runtime_header = args.runtime_header.read_text(encoding="utf-8")
    native_text = args.native_source.read_text(encoding="utf-8")
    native_header = args.native_header.read_text(encoding="utf-8")
    if "PQ" in runtime_text or "libpq" in runtime_text or "libpq" in runtime_header:
        raise SystemExit("generic M3 runtime exposes libpq")
    if "MUD_M3_MODE" not in runtime_text or '"shadow"' not in runtime_text:
        raise SystemExit("generic M3 runtime lacks opt-in shadow mode")
    if "MUHAN_HOME" not in runtime_text or "shadow_operations" not in runtime_text:
        raise SystemExit("shadow runtime lacks explicit root validation or lifecycle boundary")
    if "char muhan_home[" not in native_header or "char world_id[" not in native_header:
        raise SystemExit("native shadow runtime lacks process-lifetime routing storage")
    if (
        "configuration.root=native->muhan_home" not in native_text
        or "configuration.world_id=native->world_id" not in native_text
        or "configuration.root=muhan_home" in native_text
        or "configuration.world_id=world_id" in native_text
    ):
        raise SystemExit("native shadow owner retains borrowed routing pointers")
    if "for_test" in runtime_text + runtime_header + native_text + native_header:
        raise SystemExit("M3 runtime test hook leaked")
    main_text = args.main_source.read_text(encoding="utf-8")
    if "character_save_journal_v2_runtime_start(&m3_runtime)!=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY" in main_text:
        raise SystemExit("M3 opt-in startup incorrectly exits for disabled mode")
    if "character_save_journal_v2_runtime_start(&m3_runtime)" not in main_text or "==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED" not in main_text:
        raise SystemExit("M3 opt-in startup must exit only for failed state")
    if "atexit(m3_runtime_shutdown_at_exit)" not in main_text:
        raise SystemExit("M3 runtime must register its normal-exit shutdown hook")
    if args.runtime_object:
        undefined = symbols(args.runtime_object, "-u")
        if any(symbol.startswith("pq") for symbol in undefined):
            raise SystemExit("generic M3 runtime has libpq undefined symbols")
    if args.native_object:
        undefined = symbols(args.native_object, "-u")
        if not any(symbol.startswith("pq") for symbol in undefined):
            raise SystemExit("isolated M3 runtime native object lacks libpq binding")
    print("character_save_journal_v2_runtime_no_live_link_test: ok")


if __name__ == "__main__":
    main()
