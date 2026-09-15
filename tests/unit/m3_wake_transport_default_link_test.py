#!/usr/bin/env python3
"""Verify the bounded wake transport stays outside the default link."""
import argparse
import os
import re
import subprocess
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--object", type=Path, required=True)
    parser.add_argument("--makefile", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--header", type=Path, required=True)
    args = parser.parse_args()
    environment = os.environ.copy()
    for name in ("USE_M3_RUNTIME", "USE_RUST_RESOLVER", "MAKEFLAGS", "MFLAGS", "MAKEOVERRIDES"):
        environment.pop(name, None)
    inputs = subprocess.check_output([
        "make", "-s", "--no-print-directory", "-f", args.makefile.name,
        "m3-wake-transport-default-link-inputs"], cwd=args.makefile.parent,
        env=environment, text=True)
    if re.search(r"m3_wake_transport|m3_wake_v1|socket|libpq|postgres|use_rust_resolver",
                 inputs, re.I):
        raise SystemExit("default link inputs gained wake transport or live dependencies")
    source = args.source.read_text(encoding="utf-8") + args.header.read_text(encoding="utf-8")
    if re.search(r"\b(socket|connect|bind|listen|accept|fork|vfork|exec\w*|posix_spawn|waitpid|kill)\s*\(", source):
        raise SystemExit("transport must stay behind injected effects")
    if re.search(r"character_save_journal|player_store|rpc|libpq|\bPQ\w*", source, re.I):
        raise SystemExit("transport must not couple to save or RPC state")
    symbols = {line.split()[0].lstrip("_").lower()
               for line in subprocess.check_output(["nm", "-P", "-u", str(args.object)], text=True).splitlines()
               if line.split()}
    allowed = {"m3_wake_v1_encode", "m3_wake_v1_decode", "memcpy", "memmove",
               "memcpy_chk", "memmove_chk", "memset", "memset_chk",
               "stack_chk_fail", "stack_chk_guard"}
    if not symbols.issubset(allowed):
        raise SystemExit("transport object reached beyond codec and C memory support")
    print("m3_wake_transport_default_link_test: ok")


if __name__ == "__main__":
    main()
