#!/usr/bin/env python3
"""Keep the local receipt proof out of every live object graph."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
makefile = (root / "src" / "Makefile").read_text(encoding="utf-8")
source = (root / "src" / "onboarding_activation_snapshot_receipt.c").read_text(
    encoding="utf-8")

if "#error \"activation snapshot receipt requires O_NOFOLLOW\"" not in source:
    raise SystemExit("receipt proof must fail closed when O_NOFOLLOW is unavailable")

for variable in ("OBJECTS", "M3_RUNTIME_OBJECTS"):
    match = re.search(rf"^{variable}\s*=.*?(?=^\S|\Z)", makefile,
                      re.MULTILINE | re.DOTALL)
    if not match:
        raise SystemExit(f"{variable} assignment was not found")
    if "onboarding_activation_snapshot_receipt.o" in match.group(0):
        raise SystemExit(f"receipt proof unexpectedly entered {variable}")

if "onboarding-activation-snapshot-receipt-test: m3-feature-off-legacy-authority-test" not in makefile:
    raise SystemExit("receipt proof test must retain the FileStore authority proof")

print("onboarding_activation_snapshot_receipt_static_test: ok")
