#!/usr/bin/env python3
"""Keep the real C/Gateway/browser acceptance DB on the current game schema."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
runner = (root / "scripts/run-stack-e2e.sh").read_text()
# Realtime is intentionally absent from the disposable PostgreSQL contract.
excluded = {"20260901000000_profiles_and_lobby_presence.sql"}
expected = sorted(p.name for p in (root / "supabase/migrations").glob("*.sql")
                  if p.name not in excluded)
actual = re.findall(r'^apply_sql "\$repo_root/supabase/migrations/([^"/]+\.sql)"$',
                    runner, re.MULTILINE)
assert actual == expected, (
    "stack E2E must apply every game migration once in chronological order; "
    f"missing={sorted(set(expected) - set(actual))}, "
    f"unexpected={sorted(set(actual) - set(expected))}"
)
assert 'if [[ "${CI:-}" != "true" ]]' in runner, "CI-only gate must remain"
print(f"stack_e2e_migration_coverage_test: {len(actual)} game migrations in order")
