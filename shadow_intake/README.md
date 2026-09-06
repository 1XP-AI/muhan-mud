# Alias title snapshot shadow intake

`alias_title_snapshot_manifest_v1.sql` is a detached Postgres/Supabase-compatible
schema contract for `AliasTitleSnapshotManifestV1`. It is not a migration and
is default-off: nothing in this repository loads it, configures it, calls it,
or grants access to it.

The metadata relation is keyed by `event_id` and records the routing,
provenance, and manifest identity. The separate wire relation preserves the
unchanged canonical kind-9 CDTO bytes with their supplied SHA-256 digest and
byte length; it does not parse or reserialize the wire payload.

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

The test checks the schema shape, wire/metadata separation, append-only and
default-deny/RPC clauses, then evaluates a deterministic fixture covering the
first write, exact retry, and immutable-field conflicts.

## Future disposable PostgreSQL 17 contract test

When a disposable PG17 service is intentionally available, the future harness
will create an empty database, apply only this SQL file, create a NOLOGIN test
writer role, grant that role schema `USAGE` and RPC `EXECUTE`, and run SQL cases
for the fixture. For example, with a throwaway database URL:

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
