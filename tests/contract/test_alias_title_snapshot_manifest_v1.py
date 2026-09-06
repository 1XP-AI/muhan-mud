#!/usr/bin/env python3
"""Static and fixture contract checks for the detached shadow-intake schema.

This deliberately opens no network connection and starts no database.  It is
the default test for a repository that has no database-test runner yet.
"""

from __future__ import annotations

import json
import pathlib
import sys


ROOT = pathlib.Path(__file__).resolve().parents[2]
SQL_PATH = ROOT / "shadow_intake" / "alias_title_snapshot_manifest_v1.sql"
FIXTURE_PATH = ROOT / "shadow_intake" / "alias_title_snapshot_manifest_v1.fixture.json"


def require(text: str, fragment: str) -> None:
    if fragment not in text:
        raise AssertionError(f"missing contract fragment: {fragment!r}")


def test_static_schema_shape(sql: str) -> None:
    required_columns = (
        "event_id uuid PRIMARY KEY",
        "world_id text NOT NULL",
        "canonical_legacy_name_key text NOT NULL",
        "character_id uuid NOT NULL",
        "writer_instance_id uuid NOT NULL",
        "writer_epoch bigint NOT NULL",
        "writer_revision bigint NOT NULL",
        "command_id uuid NOT NULL",
        "correlation_id uuid NOT NULL",
        "manifest_format text NOT NULL",
        "manifest_kind smallint NOT NULL",
        "manifest_schema text NOT NULL",
        "recorded_at timestamptz NOT NULL",
        "canonical_cdto_wire bytea NOT NULL",
        "canonical_cdto_digest_sha256 text NOT NULL",
        "canonical_cdto_length integer NOT NULL",
    )
    for column in required_columns:
        require(sql, column)

    require(sql, "CREATE TABLE shadow_intake.alias_title_snapshot_manifest_v1 (")
    require(sql, "CREATE TABLE shadow_intake.alias_title_snapshot_manifest_v1_wire (")
    metadata_body = sql.split("CREATE TABLE shadow_intake.alias_title_snapshot_manifest_v1 (", 1)[1].split(");", 1)[0]
    if "canonical_cdto_wire" in metadata_body:
        raise AssertionError("canonical CDTO bytes must remain outside manifest metadata")

    for fragment in (
        "manifest_format = 'CDTO'",
        "manifest_kind = 9",
        "manifest_schema = 'AliasTitleSnapshotManifestV1'",
        "canonical_cdto_length = octet_length(canonical_cdto_wire)",
        "ON CONFLICT (event_id) DO NOTHING",
        "RETURN 'first_recorded'",
        "RETURN 'exact_retry_idempotent'",
        "ERRCODE = '23505'",
    ):
        require(sql, fragment)

    # Default deny plus an explicit narrow RPC is the required Supabase/Postgres
    # exposure posture; the schema intentionally adds no grant.
    for fragment in (
        "SECURITY DEFINER",
        "REVOKE ALL ON SCHEMA shadow_intake FROM PUBLIC;",
        "REVOKE ALL ON TABLE shadow_intake.alias_title_snapshot_manifest_v1 FROM PUBLIC;",
        "REVOKE ALL ON TABLE shadow_intake.alias_title_snapshot_manifest_v1_wire FROM PUBLIC;",
        "REVOKE ALL ON FUNCTION shadow_intake.record_alias_title_snapshot_manifest_v1(",
        "BEFORE UPDATE OR DELETE ON shadow_intake.alias_title_snapshot_manifest_v1",
        "BEFORE UPDATE OR DELETE ON shadow_intake.alias_title_snapshot_manifest_v1_wire",
    ):
        require(sql, fragment)
    if any(line.lstrip().upper().startswith("GRANT ") for line in sql.splitlines()):
        raise AssertionError("shadow schema must remain default-deny; add no grants")
    if "CREATE EXTENSION" in sql.upper():
        raise AssertionError("shadow schema must not add deployment dependencies")


def test_fixture_dedupe_contract(fixture: dict) -> None:
    if fixture["contract"] != "AliasTitleSnapshotManifestV1":
        raise AssertionError("fixture declares the wrong contract")
    first = fixture["first_record"]
    if first["manifest_format"] != "CDTO" or first["manifest_kind"] != 9:
        raise AssertionError("fixture is not a canonical kind-9 CDTO record")
    if first["manifest_schema"] != "AliasTitleSnapshotManifestV1":
        raise AssertionError("fixture has the wrong manifest schema")
    if len(bytes.fromhex(first["canonical_cdto_wire_hex"])) != first["canonical_cdto_length"]:
        raise AssertionError("fixture wire length is not canonical")
    digest = first["canonical_cdto_digest_sha256"]
    if len(digest) != 64 or any(char not in "0123456789abcdef" for char in digest):
        raise AssertionError("fixture digest is not a lowercase SHA-256 hex value")

    immutable = dict(first)
    seen = False
    for attempt in fixture["attempts"]:
        candidate = dict(first)
        candidate.update(attempt["changes"])
        actual = "first_recorded" if not seen else (
            "exact_retry_idempotent" if candidate == immutable else "immutable_conflict"
        )
        if actual != attempt["expected"]:
            raise AssertionError(f"{attempt['name']}: expected {attempt['expected']}, got {actual}")
        seen = True


def main() -> int:
    sql = SQL_PATH.read_text(encoding="utf-8")
    fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))
    test_static_schema_shape(sql)
    test_fixture_dedupe_contract(fixture)
    print("AliasTitleSnapshotManifestV1 static contract: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
