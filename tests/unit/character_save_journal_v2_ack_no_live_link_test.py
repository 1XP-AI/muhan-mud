#!/usr/bin/env python3
import argparse, re, subprocess
from pathlib import Path
P=argparse.ArgumentParser();P.add_argument("--object",type=Path,required=True);P.add_argument("--makefile",type=Path,required=True);A=P.parse_args()
bad=("save_ply","load_ply","gateway","bank_","player_store_","file_player_store_","player_path_","onboarding_","db_","postgres_","supabase_","pq","savegame")
crash_test_runtime={"getenv","kill","strtoul","exit"}
syms={x.split()[0].lstrip("_").lower() for x in subprocess.check_output(["nm","-P","-u",str(A.object)],text=True).splitlines() if x.split()}
hit=sorted(x for x in syms if any(x==y or x.startswith(y) for y in bad))
if hit: raise SystemExit("ack object has live dependency: "+", ".join(hit))
hit=sorted(syms & crash_test_runtime)
if hit: raise SystemExit("ack production object references crash-test runtime: "+", ".join(hit))
lines=A.makefile.read_text().splitlines();objs=[];on=False
for line in lines:
 m=re.match(r"^OBJECTS\s*=\s*(.*)$",line) if not on else None
 if m:on=True;part=m.group(1)
 elif on:part=line.strip()
 else:continue
 objs+=part.rstrip("\\").split()
 if not part.endswith("\\"):break
if "character_save_journal_v2_ack.o" in objs:raise SystemExit("ack object entered live MUD OBJECTS")
print("character_save_journal_v2_ack_no_live_link_test: ok")
