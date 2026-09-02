#!/usr/bin/env python3
import argparse
import re
import subprocess
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument("--object", type=Path, required=True)
p.add_argument("--makefile", type=Path, required=True)
a = p.parse_args()
bad = ("save_ply", "load_ply", "gateway", "bank_", "player_store_",
       "file_player_store_", "player_path_", "onboarding_", "db_",
       "postgres_", "supabase_", "pq", "savegame")
symbols = {line.split()[0].lstrip("_").lower()
           for line in subprocess.check_output(["nm", "-P", "-u", str(a.object)],
                                                text=True).splitlines() if line.split()}
hits = sorted(s for s in symbols if any(s == x or s.startswith(x) for x in bad))
if hits:
    raise SystemExit("recovery object has live dependency: " + ", ".join(hits))
globals_out = subprocess.check_output(["nm", "-g", "-P", str(a.object)], text=True)
exported = {line.split()[0].lstrip("_") for line in globals_out.splitlines()
            if line.split()}
if "character_save_journal_v2_recovery_run" not in exported:
    raise SystemExit("recovery_run is not exported by the recovery object")
hooks = sorted(name for name in exported if "_for_test" in name)
if hooks:
    raise SystemExit("test hooks leaked from production recovery object: " + ", ".join(hooks))
objs = []
active = False
for line in a.makefile.read_text().splitlines():
    m = re.match(r"^OBJECTS\s*=\s*(.*)$", line) if not active else None
    if m:
        active = True
        part = m.group(1)
    elif active:
        part = line.strip()
    else:
        continue
    objs += part.rstrip("\\").split()
    if not part.endswith("\\"):
        break
if "character_save_journal_v2_recovery.o" in objs:
    raise SystemExit("recovery object entered live MUD OBJECTS")
print("character_save_journal_v2_recovery_no_live_link_test: ok")
