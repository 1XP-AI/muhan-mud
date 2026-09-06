#!/usr/bin/env python3
"""Static safety checks for the explicitly opt-in PG17 shadow-intake harness.

This test does not execute the shell harness or invoke psql.  It only checks
that the future disposable-database path remains detached and guarded.
"""

from __future__ import annotations

import json
import pathlib
import re
import sys


ROOT = pathlib.Path(__file__).resolve().parents[2]
HARNESS = ROOT / "shadow_intake" / "run_pg17_alias_title_snapshot_manifest_v1_contract.sh"
ASSERTIONS = ROOT / "shadow_intake" / "pg17_alias_title_snapshot_manifest_v1_contract.sql"
README = ROOT / "shadow_intake" / "README.md"
FIXTURE = ROOT / "shadow_intake" / "alias_title_snapshot_manifest_v1.fixture.json"


def fail(message: str) -> None:
    raise AssertionError(message)


def test_explicit_opt_in_and_no_default_url(harness: str) -> None:
    opt_in = 'if [ "${PG17_CONTRACT_RUN:-}" != "1" ]; then'
    supplied_url = 'if [ -z "${PG17_CONTRACT_URL:-}" ]; then'
    if opt_in not in harness or supplied_url not in harness:
        fail("harness must require both an explicit run opt-in and a supplied URL")
    if re.search(r"PG17_CONTRACT_URL\s*[:?+]?=\s*['\"]?(?:postgres|\$\{)", harness):
        fail("harness must not define a default PostgreSQL URL")
    first_psql = harness.find("psql ")
    if first_psql == -1 or harness.find(opt_in) > first_psql or harness.find(supplied_url) > first_psql:
        fail("both opt-in guards must run before any psql invocation")
    if 'PG17_CONTRACT_URL"' not in harness:
        fail("psql must use the caller-provided URL")


def test_no_broad_database_lifecycle_commands() -> None:
    assertions = ASSERTIONS.read_text(encoding="utf-8")
    combined = "\n".join((HARNESS.read_text(encoding="utf-8"), assertions))
    forbidden = (
        r"\bcreatedb\b", r"\bdropdb\b", r"\binitdb\b",
        r"\bCREATE\s+DATABASE\b", r"\bDROP\s+DATABASE\b",
        r"\bALTER\s+DATABASE\b", r"\bTRUNCATE\s+(?:TABLE\s+)?(?:ALL|DATABASE)\b",
    )
    for pattern in forbidden:
        if re.search(pattern, combined, re.I):
            fail(f"forbidden broad database lifecycle command: {pattern}")
    if "ROLLBACK;" not in assertions:
        fail("contract assertions must roll back their role, grant, and data setup")


def test_contract_cases_and_narrow_writer_surface(assertions: str) -> None:
    for required in (
        "current_setting('server_version_num')", "CREATE ROLE shadow_intake_contract_writer NOLOGIN",
        "GRANT USAGE ON SCHEMA shadow_intake", "GRANT EXECUTE ON FUNCTION shadow_intake.record_alias_title_snapshot_manifest_v1",
        "SET LOCAL ROLE shadow_intake_contract_writer", "first_recorded", "exact_retry_idempotent",
        "WHEN unique_violation", "writer direct metadata SELECT unexpectedly succeeded",
        "writer direct wire SELECT unexpectedly succeeded",
        "writer direct metadata INSERT unexpectedly succeeded", "shadow intake is append-only",
        "RESET ROLE", "ROLLBACK;",
    ):
        if required not in assertions:
            fail(f"PG17 assertions missing {required!r}")
    expected_conflicts = (
        "world_id", "canonical_legacy_name_key", "character_id", "writer_instance_id",
        "writer_epoch", "writer_revision", "command_id", "correlation_id",
        "companion_manifest_schema", "recorded_at", "wire", "digest", "length",
    )
    for field in expected_conflicts:
        if f"v_field = '{field}'" not in assertions:
            fail(f"immutable conflict coverage missing {field}")
    for table in (
        "shadow_intake.alias_title_snapshot_manifest_v1",
        "shadow_intake.alias_title_snapshot_manifest_v1_snapshot_wire",
    ):
        if table not in assertions:
            fail(f"append-only assertions missing {table}")


def test_pg17_assertions_use_the_static_canonical_fixture(assertions: str) -> None:
    first = json.loads(FIXTURE.read_text(encoding="utf-8"))["first_record"]
    wire = first["canonical_alias_title_snapshot_v1_wire_hex"]
    digest = first["canonical_alias_title_snapshot_v1_digest_sha256"]
    length = first["canonical_alias_title_snapshot_v1_length"]
    if f"'{wire}'" not in assertions:
        fail("PG17 assertions must use the static canonical snapshot wire")
    if f"'{digest}'" not in assertions:
        fail("PG17 assertions must use the static canonical snapshot digest")
    if f"v_length integer := {length};" not in assertions:
        fail("PG17 assertions must use the static canonical snapshot length")


def test_harness_is_detached_from_normal_static_test() -> None:
    allowed = {HARNESS.resolve(), ASSERTIONS.resolve(), README.resolve(), pathlib.Path(__file__).resolve()}
    source_suffixes = {".py", ".sh", ".yml", ".yaml", ".toml", ".ini", ".json"}
    for path in ROOT.rglob("*"):
        if not path.is_file() or path.resolve() in allowed or path.suffix not in source_suffixes:
            continue
        text = path.read_text(encoding="utf-8", errors="ignore")
        if "PG17_CONTRACT_RUN" in text or "run_pg17_alias_title_snapshot_manifest_v1_contract" in text:
            fail(f"normal source unexpectedly references the opt-in harness: {path.relative_to(ROOT)}")


def main() -> int:
    harness = HARNESS.read_text(encoding="utf-8")
    assertions = ASSERTIONS.read_text(encoding="utf-8")
    test_explicit_opt_in_and_no_default_url(harness)
    test_no_broad_database_lifecycle_commands()
    test_contract_cases_and_narrow_writer_surface(assertions)
    test_pg17_assertions_use_the_static_canonical_fixture(assertions)
    test_harness_is_detached_from_normal_static_test()
    print("PG17 AliasTitleSnapshotManifestV1 harness static safety: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
