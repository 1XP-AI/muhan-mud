#!/usr/bin/env python3
"""Verify the injectable wake supervisor is absent from the default link."""
import argparse
import os
import re
import subprocess
from pathlib import Path


def default_inputs(makefile):
    environment = os.environ.copy()
    environment.pop("USE_M3_RUNTIME", None)
    environment.pop("USE_RUST_RESOLVER", None)
    environment.pop("MAKEFLAGS", None)
    environment.pop("MFLAGS", None)
    environment.pop("MAKEOVERRIDES", None)
    return subprocess.check_output([
        "make", "-s", "--no-print-directory", "-f", makefile.name,
        "m3-wake-supervisor-default-link-inputs"], cwd=makefile.parent,
        env=environment, text=True)


def undefined_symbols(path):
    output = subprocess.check_output(["nm", "-P", "-u", str(path)], text=True)
    return {line.split()[0].lstrip("_").lower()
            for line in output.splitlines() if line.split()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--header", type=Path, required=True)
    args = parser.parse_args()

    inputs = default_inputs(args.makefile)
    if re.search(r"m3_wake_supervisor|m3_wake_v1|socket|libpq|postgres|"
                 r"use_rust_resolver|muhan_resource_ffi",
                 inputs, re.I):
        raise SystemExit("default link inputs gained wake supervision or live dependencies")
    source = args.source.read_text(encoding="utf-8") + args.header.read_text(encoding="utf-8")
    if re.search(r"\b(socket|connect|bind|listen|accept|fork|vfork|exec\w*|posix_spawn|waitpid|kill)\s*\(",
                 source):
        raise SystemExit("supervisor must stay behind injected operations")
    if re.search(r"character_save_journal|player_store|rpc|queue|libpq|\bPQ\w*", source, re.I):
        raise SystemExit("supervisor must not couple to save or RPC state")
    symbols = undefined_symbols(args.object)
    allowed = {"m3_wake_v1_encode", "memset", "memset_chk",
               "stack_chk_fail", "stack_chk_guard"}
    if not symbols.issubset(allowed):
        raise SystemExit("supervisor object reached beyond codec and C memory support")
    print("m3_wake_supervisor_default_link_test: ok")


if __name__ == "__main__":
    main()
