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
python3 tests/contract/test_pg17_alias_title_snapshot_manifest_v1_harness.py
```

The test structurally checks both table definitions, the RPC parameter and
immutable-comparison set, append-only triggers, and default-deny revocations.
It also verifies that the fixture's kind-9 CDTO digest is SHA-256 of its
encoded payload, then exercises first write, exact retry, and one conflict for
each non-key immutable field. The second test reads only the harness source to
prove it stays opt-in, has no default database URL or broad database lifecycle
command, and is not referenced by normal application/test sources.

## Explicit disposable PostgreSQL 17 contract harness

`run_pg17_alias_title_snapshot_manifest_v1_contract.sh` is deliberately outside
normal testing and will exit before invoking `psql` unless both conditions are
met: `PG17_CONTRACT_RUN=1` and a caller-provided `PG17_CONTRACT_URL`. It has no
default URL, does not create or drop databases, and must only receive an empty,
disposable PostgreSQL 17 database whose owner can create a temporary NOLOGIN
test role. The harness applies this detached schema, then runs all assertions in
one rolled-back transaction: first record, exact retry, every immutable-field
conflict, denied direct table access, append-only update/delete rejection, and
the writer role's narrow schema-usage/RPC-execute surface.

Run it only after you have independently provisioned the disposable database:

```sh
PG17_CONTRACT_RUN=1 \
PG17_CONTRACT_URL='postgresql://contract_owner@127.0.0.1:5432/alias_shadow_contract' \
  sh shadow_intake/run_pg17_alias_title_snapshot_manifest_v1_contract.sh
```

The role/grant/data test setup rolls back; the schema remains in the supplied
disposable database. This repository intentionally does not add a database,
container, service, deployment configuration, runtime invocation, or normal-test
hook. The harness also does not replace the future application CDTO intake gate:
that gate must recompute the payload SHA-256 and fully validate the kind-9
snapshot fields before acceptance.
