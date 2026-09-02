#!/usr/bin/env python3
"""CI-only ELF audit for the already-built default MUD executable."""
import argparse
import platform
import re
import subprocess
from pathlib import Path


FORBIDDEN = re.compile(r"(?:^|[^a-z0-9_])(pq[a-z0-9_]*|libpq)(?:$|[^a-z0-9_])", re.I)


def output(command, binary):
    return subprocess.check_output(command + [str(binary)], text=True,
                                   stderr=subprocess.STDOUT)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    if platform.system() != "Linux":
        raise SystemExit("live MUD audit is Linux-only")
    if not args.binary.is_file():
        raise SystemExit("live MUD audit binary is missing")
    scans = {
        "full symbol table": output(["nm", "-a", "-P"], args.binary),
        "ELF symbol table": output(["readelf", "--wide", "--symbols"], args.binary),
        "ELF dynamic dependencies": output(["readelf", "--dynamic", "--wide"], args.binary),
        "resolved dynamic dependencies": output(["ldd"], args.binary),
    }
    for name, text in scans.items():
        if FORBIDDEN.search(text):
            raise SystemExit("default live MUD %s contains PQ/libpq material" % name)
    print("character_save_journal_v2_rpc_transport_live_mud_audit: ok")


if __name__ == "__main__":
    main()
