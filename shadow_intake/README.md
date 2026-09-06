# Alias title snapshot shadow intake

`alias_title_snapshot_manifest_v1.sql` is a detached Postgres/Supabase-compatible
schema contract for `AliasTitleSnapshotManifestV1` companion metadata. It is
not a migration and is default-off: nothing in this repository loads it,
configures it, calls it, or grants access to it.

The metadata relation is keyed by `event_id` and records routing, provenance,
and the relational companion-manifest schema identity. It is not a CDTO wire.
The separate wire relation preserves a nested canonical CDTO v1 **kind-9
`AliasTitleSnapshotV1`** envelope, its byte length, and its SHA-256 digest
trailer. It is not an `AliasTitleSnapshotManifestV1` CDTO envelope, and the
intake does not parse or reserialize its snapshot payload.

SQL binds the wire length, CDTO magic/version/kind, declared payload length,
and supplied lowercase digest text to the trailing 32 CDTO bytes using only
built-in PostgreSQL functions. It deliberately does not recompute SHA-256 or
fully decode/re-canonicalize snapshot fields: that validation remains a future
disposable-PG plus application-intake gate, where the canonical codec can
verify the digest over the encoded payload without adding an extension here.

The RPC establishes the only intended write semantics:

- A new event UUID returns `first_recorded`.
- A retry returns `exact_retry_idempotent` only when every stored immutable
  value, including `recorded_at` and all canonical wire values, is identical.
- Reusing an event UUID with any changed immutable value raises SQLSTATE
  `23505`; direct updates and deletes are rejected by triggers.

All privileges are revoked from `PUBLIC`, including RPC execution. A future
integrator must explicitly create a dedicated writer role and grant it only
`USAGE` on `shadow_intake` and `EXECUTE` on the named RPC; the tables themselves
remain ungranted.

## Default static test

Run without a database:

```sh
python3 tests/contract/test_alias_title_snapshot_manifest_v1.py
```

The test structurally checks both table definitions, the RPC parameter and
immutable-comparison set, append-only triggers, and default-deny revocations.
It also verifies that the fixture's kind-9 CDTO digest is SHA-256 of its
encoded payload, then exercises first write, exact retry, and one conflict for
each non-key immutable field.

## Future disposable PostgreSQL 17 contract test

When a disposable PG17 service is intentionally available, the future harness
will create an empty database, apply only this SQL file, create a NOLOGIN test
writer role, grant that role schema `USAGE` and RPC `EXECUTE`, and run SQL cases
for the fixture. That gate must also invoke the application CDTO intake/canonical
codec to recompute the payload SHA-256 and fully validate the kind-9 snapshot
fields before acceptance. For example, with a throwaway database URL:

```sh
export PG17_CONTRACT_URL='postgresql:///alias_shadow_contract'
createdb alias_shadow_contract
psql "$PG17_CONTRACT_URL" -v ON_ERROR_STOP=1 \
  -f shadow_intake/alias_title_snapshot_manifest_v1.sql
# Harness then grants only the explicit test role and invokes the RPC cases.
dropdb alias_shadow_contract
```

This repository intentionally does not add that database, container, service,
deployment configuration, or runtime invocation.
