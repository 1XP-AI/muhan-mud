#!/usr/bin/env python3
"""Prove the opt-in RPC transport cannot enter the default MUD link."""
import argparse
import os
import re
import subprocess
from pathlib import Path

def symbols(path, flag):
    text=subprocess.check_output(["nm","-P",flag,str(path)],text=True)
    return {line.split()[0].lstrip("_").lower() for line in text.splitlines() if line.split()}

def default_make_values(makefile):
    environment=os.environ.copy()
    environment.pop("USE_M3_RUNTIME",None)
    environment.pop("USE_RUST_RESOLVER",None)
    environment.pop("MAKEFLAGS",None)
    environment.pop("MFLAGS",None)
    environment.pop("MAKEOVERRIDES",None)
    output=subprocess.check_output([
        "make","-s","--no-print-directory","-f",makefile.name,
        "character-save-journal-v2-rpc-transport-default-link-inputs"],
        cwd=makefile.parent,env=environment,text=True)
    values={}
    for line in output.splitlines():
        name,separator,value=line.partition("=")
        if not separator or name not in {"OBJECTS","LIBS","EXTRA_LIBS","M3_RUNTIME_LIBS"}:
            raise SystemExit("default link audit emitted an invalid variable")
        values[name]=value
    if set(values)!={"OBJECTS","LIBS","EXTRA_LIBS","M3_RUNTIME_LIBS"}:
        raise SystemExit("default link audit omitted an expanded link variable")
    return " ".join(values.values())

def main():
    p=argparse.ArgumentParser()
    p.add_argument("--object",type=Path,required=True); p.add_argument("--native",type=Path)
    p.add_argument("--makefile",type=Path,required=True); p.add_argument("--source",type=Path,required=True)
    p.add_argument("--header",type=Path,required=True); p.add_argument("--native-source",type=Path,required=True)
    p.add_argument("--native-header",type=Path,required=True); a=p.parse_args()
    default=default_make_values(a.makefile)
    if re.search(r"character_save_journal_v2_rpc_transport|-l(?:pq|postgres)|\bpq[a-z_]",default,re.I):
        raise SystemExit("default OBJECTS/LIBS/EXTRA_LIBS gained RPC transport or libpq")
    if any(symbol.startswith("pq") for symbol in symbols(a.object,"-u")):
        raise SystemExit("generic transport has a PQ symbol")
    source=a.source.read_text()+"\n"+a.header.read_text()
    if re.search(r"\bPQ[A-Za-z0-9_]*\b|\b(getenv|fopen|open)\s*\(",source):
        raise SystemExit("generic transport binds libpq or discovers credentials")
    native_source=a.native_source.read_text()+"\n"+a.native_header.read_text()
    if "PQtransactionStatus" not in native_source or "PQTRANS_IDLE" not in native_source:
        raise SystemExit("native transport does not enforce PQtransactionStatus idle")
    if any(token not in native_source for token in ("PQerrorMessage", "PQresultStatus", "PG_DIAG_SQLSTATE")):
        raise SystemExit("native transport failure diagnostics omit required libpq context")
    if a.native:
        native=symbols(a.native,"-u")
        if not {"pqstatus","pqexecparams","pqtransactionstatus","pqfinish"}.issubset(native):
            raise SystemExit("native binding lacks required PQ lifecycle symbols")
    print("character_save_journal_v2_rpc_transport_no_live_link_test: ok")

if __name__=="__main__": main()
