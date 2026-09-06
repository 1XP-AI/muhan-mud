#!/usr/bin/env python3
"""Static and fixture checks for the detached alias-title shadow intake.

The repository deliberately has no disposable PostgreSQL runner. These checks
parse the narrow SQL DDL/RPC shape and exercise its deterministic retry model;
they never connect to a database or enable this default-off contract.
"""

from __future__ import annotations

import hashlib
import json
import pathlib
import re
import sys


ROOT = pathlib.Path(__file__).resolve().parents[2]
SQL_PATH = ROOT / "shadow_intake" / "alias_title_snapshot_manifest_v1.sql"
FIXTURE_PATH = ROOT / "shadow_intake" / "alias_title_snapshot_manifest_v1.fixture.json"

METADATA_TABLE = "shadow_intake.alias_title_snapshot_manifest_v1"
WIRE_TABLE = "shadow_intake.alias_title_snapshot_manifest_v1_snapshot_wire"
RPC = "shadow_intake.record_alias_title_snapshot_manifest_v1"

METADATA_COLUMNS = (
    "event_id", "world_id", "canonical_legacy_name_key", "character_id",
    "writer_instance_id", "writer_epoch", "writer_revision", "command_id",
    "correlation_id", "companion_manifest_schema", "recorded_at",
)
WIRE_COLUMNS = (
    "event_id", "canonical_alias_title_snapshot_v1_wire",
    "canonical_alias_title_snapshot_v1_digest_sha256",
    "canonical_alias_title_snapshot_v1_length",
)
# event_id selects the existing record. Every other stored value must match
# for that event ID to be an idempotent retry.
IMMUTABLE_FIELDS = METADATA_COLUMNS[1:] + WIRE_COLUMNS[1:]
FIXTURE_IMMUTABLE_FIELDS = IMMUTABLE_FIELDS[:-3] + (
    "canonical_alias_title_snapshot_v1_wire_hex",
    "canonical_alias_title_snapshot_v1_digest_sha256",
    "canonical_alias_title_snapshot_v1_length",
)


def fail(message: str) -> None:
    raise AssertionError(message)


def strip_line_comments(sql: str) -> str:
    return "\n".join(line.split("--", 1)[0] for line in sql.splitlines())


def balanced_parenthesized(text: str, opening: int) -> tuple[str, int]:
    if text[opening] != "(":
        fail("expected parenthesized SQL expression")
    depth = 0
    quote: str | None = None
    for index in range(opening, len(text)):
        char = text[index]
        if quote:
            if char == quote:
                quote = None
            continue
        if char in "'\"":
            quote = char
        elif char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if depth == 0:
                return text[opening + 1:index], index + 1
    fail("unclosed SQL parenthesis")


def split_top_level(text: str) -> list[str]:
    items: list[str] = []
    start = depth = 0
    quote: str | None = None
    for index, char in enumerate(text):
        if quote:
            if char == quote:
                quote = None
            continue
        if char in "'\"":
            quote = char
        elif char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
        elif char == "," and depth == 0:
            items.append(text[start:index].strip())
            start = index + 1
    items.append(text[start:].strip())
    return [item for item in items if item]


def table_body(sql: str, name: str) -> str:
    match = re.search(rf"\bCREATE\s+TABLE\s+{re.escape(name)}\s*\(", sql, re.I)
    if not match:
        fail(f"missing table {name}")
    return balanced_parenthesized(sql, match.end() - 1)[0]


def table_definition(sql: str, name: str) -> tuple[list[str], dict[str, str]]:
    body = table_body(sql, name)
    columns: dict[str, str] = {}
    order: list[str] = []
    for item in split_top_level(body):
        column = re.match(r"([a-z_][a-z0-9_]*)\s+(.+)", item, re.I | re.S)
        if column and column.group(1).lower() not in {"check", "constraint", "primary", "foreign"}:
            key = column.group(1).lower()
            order.append(key)
            columns[key] = " ".join(column.group(2).split())
    return order, columns


def function_definition(sql: str, name: str) -> tuple[list[tuple[str, str]], str, str]:
    match = re.search(rf"\bCREATE\s+OR\s+REPLACE\s+FUNCTION\s+{re.escape(name)}\s*\(", sql, re.I)
    if not match:
        fail(f"missing RPC {name}")
    parameters, params_end = balanced_parenthesized(sql, match.end() - 1)
    prefix_end = sql.find("AS $$", params_end)
    if prefix_end == -1:
        fail("RPC lacks dollar-quoted body")
    body_start = prefix_end + len("AS $$")
    body_end = sql.find("$$;", body_start)
    if body_end == -1:
        fail("RPC has unterminated body")
    parsed: list[tuple[str, str]] = []
    for parameter in split_top_level(parameters):
        item = " ".join(parameter.split())
        parameter_match = re.fullmatch(r"(p_[a-z0-9_]+)\s+(.+)", item, re.I)
        if not parameter_match:
            fail(f"invalid RPC parameter: {parameter!r}")
        parsed.append((parameter_match.group(1), parameter_match.group(2).lower()))
    return parsed, " ".join(sql[params_end:prefix_end].split()), sql[body_start:body_end]


def assert_column_contract(order: list[str], columns: dict[str, str], expected: tuple[str, ...], table: str) -> None:
    if tuple(order) != expected:
        fail(f"{table} columns changed: expected {expected}, got {tuple(order)}")
    for column in expected:
        if "NOT NULL" not in columns[column].upper() and "PRIMARY KEY" not in columns[column].upper():
            fail(f"{table}.{column} must be NOT NULL")


def test_static_schema_contract(sql: str) -> None:
    sql = strip_line_comments(sql)
    metadata_order, metadata = table_definition(sql, METADATA_TABLE)
    wire_order, wire = table_definition(sql, WIRE_TABLE)
    assert_column_contract(metadata_order, metadata, METADATA_COLUMNS, METADATA_TABLE)
    assert_column_contract(wire_order, wire, WIRE_COLUMNS, WIRE_TABLE)
    if "uuid primary key" not in metadata["event_id"].lower():
        fail("metadata event_id must be the primary key")
    if "references shadow_intake.alias_title_snapshot_manifest_v1 (event_id)" not in wire["event_id"].lower():
        fail("wire event_id must reference companion metadata")
    if "AliasTitleSnapshotManifestV1" not in metadata["companion_manifest_schema"]:
        fail("companion metadata must name its manifest schema")
    if "canonical_cdto" in sql or "manifest_kind" in sql or "manifest_format" in sql:
        fail("old ambiguous manifest/wire terminology remains")

    wire_constraints = " ".join(wire.values()) + " " + " ".join(table_body(sql, WIRE_TABLE).split())
    for expression in (
        "BETWEEN 48 AND 65584",
        "octet_length(canonical_alias_title_snapshot_v1_wire)",
        "substring(canonical_alias_title_snapshot_v1_wire FROM 1 FOR 8)",
        "decode('4d55484344544f00', 'hex')",
        "substring(canonical_alias_title_snapshot_v1_wire FROM 9 FOR 2)",
        "decode('0001', 'hex')",
        "substring(canonical_alias_title_snapshot_v1_wire FROM 11 FOR 2)",
        "decode('0009', 'hex')",
        "get_byte(canonical_alias_title_snapshot_v1_wire, 12)::bigint",
        "substring( canonical_alias_title_snapshot_v1_wire FROM octet_length(canonical_alias_title_snapshot_v1_wire) - 31 FOR 32 )",
        "encode(",
        "'^[0-9a-f]{64}$'",
    ):
        if expression not in wire_constraints:
            fail(f"wire envelope check missing {expression!r}")

    parameters, rpc_declaration, rpc_body = function_definition(sql, RPC)
    expected_parameters = tuple((f"p_{field}", field_type) for field, field_type in (
        ("event_id", "uuid"), ("world_id", "text"),
        ("canonical_legacy_name_key", "text"), ("character_id", "uuid"),
        ("writer_instance_id", "uuid"), ("writer_epoch", "bigint"),
        ("writer_revision", "bigint"), ("command_id", "uuid"),
        ("correlation_id", "uuid"), ("companion_manifest_schema", "text"),
        ("recorded_at", "timestamptz"),
        ("canonical_alias_title_snapshot_v1_wire", "bytea"),
        ("canonical_alias_title_snapshot_v1_digest_sha256", "text"),
        ("canonical_alias_title_snapshot_v1_length", "integer"),
    ))
    if tuple(parameters) != expected_parameters:
        fail(f"RPC parameter contract changed: {parameters}")
    if "RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, shadow_intake" not in rpc_declaration:
        fail("RPC must be a schema-pinned SECURITY DEFINER text function")
    if not re.search(r"INSERT\s+INTO\s+shadow_intake\.alias_title_snapshot_manifest_v1\b.*?ON\s+CONFLICT\s*\(event_id\)\s+DO\s+NOTHING", rpc_body, re.I | re.S):
        fail("RPC must use event_id insert-or-retry selection")
    if not re.search(rf"INSERT\s+INTO\s+{re.escape(WIRE_TABLE)}\b", rpc_body, re.I):
        fail("RPC must store the snapshot wire in its separate relation")
    comparisons = re.findall(
        r"v_existing\.([a-z0-9_]+)\s+IS\s+NOT\s+DISTINCT\s+FROM\s+p_([a-z0-9_]+)",
        rpc_body,
        re.I,
    )
    if tuple((left, right) for left, right in comparisons) != tuple((field, field) for field in IMMUTABLE_FIELDS):
        fail("RPC must compare every and only each non-key immutable field")
    for outcome in ("first_recorded", "exact_retry_idempotent"):
        if len(re.findall(rf"RETURN\s+'{outcome}'", rpc_body, re.I)) != 1:
            fail(f"RPC must return {outcome} exactly once")
    if not re.search(r"ERRCODE\s*=\s*'23505'", rpc_body, re.I):
        fail("RPC must reject non-identical retries with SQLSTATE 23505")

    triggers = re.findall(
        r"CREATE\s+TRIGGER\s+([a-z0-9_]+)\s+BEFORE\s+UPDATE\s+OR\s+DELETE\s+ON\s+([a-z0-9_.]+)\s+FOR\s+EACH\s+ROW\s+EXECUTE\s+FUNCTION\s+shadow_intake\.reject_mutation\s*\(\s*\)",
        sql,
        re.I | re.S,
    )
    expected_trigger_tables = {METADATA_TABLE, WIRE_TABLE}
    if {table for _, table in triggers} != expected_trigger_tables or len(triggers) != 2:
        fail("both intake relations must reject UPDATE and DELETE through the trigger")
    if not re.search(r"CREATE\s+OR\s+REPLACE\s+FUNCTION\s+shadow_intake\.reject_mutation\s*\(\s*\).*?RAISE\s+EXCEPTION\s+'shadow intake is append-only'", sql, re.I | re.S):
        fail("append-only trigger function is missing")

    revoked_tables = set(re.findall(r"REVOKE\s+ALL\s+ON\s+TABLE\s+([a-z0-9_.]+)\s+FROM\s+PUBLIC\s*;", sql, re.I))
    if revoked_tables != expected_trigger_tables:
        fail("default-deny must revoke both tables from PUBLIC")
    if not re.search(r"REVOKE\s+ALL\s+ON\s+SCHEMA\s+shadow_intake\s+FROM\s+PUBLIC\s*;", sql, re.I):
        fail("default-deny schema revoke is missing")
    if not re.search(rf"REVOKE\s+ALL\s+ON\s+FUNCTION\s+{re.escape(RPC)}\s*\(", sql, re.I):
        fail("default-deny RPC revoke is missing")
    if re.search(r"^\s*GRANT\b", sql, re.I | re.M):
        fail("the detached schema must not grant privileges")
    if re.search(r"CREATE\s+EXTENSION\b", sql, re.I):
        fail("the detached schema must not add extension dependencies")


def parse_snapshot_fields(payload: bytes) -> list[tuple[int, int, bytes]]:
    fields: list[tuple[int, int, bytes]] = []
    cursor = 0
    while cursor < len(payload):
        if len(payload) - cursor < 7:
            fail("truncated CDTO field header")
        field_id = int.from_bytes(payload[cursor:cursor + 2], "big")
        field_type = payload[cursor + 2]
        length = int.from_bytes(payload[cursor + 3:cursor + 7], "big")
        cursor += 7
        if length > len(payload) - cursor:
            fail("truncated CDTO field value")
        fields.append((field_id, field_type, payload[cursor:cursor + length]))
        cursor += length
    return fields


def test_fixture_snapshot_wire(fixture: dict) -> None:
    if fixture.get("contract") != "AliasTitleSnapshotManifestV1":
        fail("fixture declares the wrong companion manifest contract")
    if fixture.get("snapshot_wire_contract") != {
        "format": "CDTO", "version": 1, "kind": 9, "schema": "AliasTitleSnapshotV1",
    }:
        fail("fixture must declare the nested AliasTitleSnapshotV1 CDTO v1 kind-9 wire")
    first = fixture["first_record"]
    wire = bytes.fromhex(first["canonical_alias_title_snapshot_v1_wire_hex"])
    if wire[:8] != b"MUHCDTO\x00" or int.from_bytes(wire[8:10], "big") != 1 or int.from_bytes(wire[10:12], "big") != 9:
        fail("fixture is not a CDTO v1 kind-9 envelope")
    payload_length = int.from_bytes(wire[12:16], "big")
    if len(wire) != first["canonical_alias_title_snapshot_v1_length"] or len(wire) != 16 + payload_length + 32:
        fail("fixture CDTO length does not bind its envelope payload length")
    expected_digest = hashlib.sha256(wire[16:16 + payload_length]).digest()
    if wire[-32:] != expected_digest:
        fail("fixture CDTO digest trailer is not SHA-256(payload)")
    if first["canonical_alias_title_snapshot_v1_digest_sha256"] != expected_digest.hex():
        fail("fixture digest text does not match the verified CDTO trailer")
    expected_fields = [
        (1, 2, b"\x00\x01"),
        (2, 9, b"\x01n\x00\x05north\x01a\x00\rattack target"),
        (3, 11, b"\x01"),
        (4, 9, b"Ruler"),
    ]
    if parse_snapshot_fields(wire[16:16 + payload_length]) != expected_fields:
        fail("fixture payload is not the canonical AliasTitleSnapshotV1 field sequence")


def dedupe_outcome(existing: dict[str, object] | None, candidate: dict[str, object]) -> str:
    if existing is None:
        return "first_recorded"
    return "exact_retry_idempotent" if all(
        existing[field] == candidate[field] for field in FIXTURE_IMMUTABLE_FIELDS
    ) else "immutable_conflict"


def test_fixture_dedupe_model(fixture: dict) -> None:
    first = fixture["first_record"]
    existing: dict[str, object] | None = None
    exercised: set[str] = set()
    for attempt in fixture["attempts"]:
        candidate = dict(first)
        candidate.update(attempt["changes"])
        actual = dedupe_outcome(existing, candidate)
        if actual != attempt["expected"]:
            fail(f"{attempt['name']}: expected {attempt['expected']}, got {actual}")
        if existing is None:
            existing = dict(candidate)
            continue
        changes = set(attempt["changes"])
        if attempt["expected"] == "exact_retry_idempotent":
            if changes:
                fail("exact retry must have no changed immutable value")
        else:
            if len(changes) != 1 or not changes <= set(FIXTURE_IMMUTABLE_FIELDS):
                fail(f"{attempt['name']} must change exactly one immutable field")
            exercised.update(changes)
    if exercised != set(FIXTURE_IMMUTABLE_FIELDS):
        fail(f"fixture must exercise each immutable conflict; missing {set(FIXTURE_IMMUTABLE_FIELDS) - exercised}")


def main() -> int:
    test_static_schema_contract(SQL_PATH.read_text(encoding="utf-8"))
    fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))
    test_fixture_snapshot_wire(fixture)
    test_fixture_dedupe_model(fixture)
    print("AliasTitleSnapshotManifestV1 static contract: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
