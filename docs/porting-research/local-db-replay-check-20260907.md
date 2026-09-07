# Local DB replay check — passed on Linux

## Backup/restore gate — valid fixture passed

Empty and nested inventory restore at `65daff0` passes (combined runner exit 0).
Log: `/tmp/muhan-nested-inventory-restore.log`. The original canonical profile
(zero items) is retained, and the checked-in tree_inventory fixture is separately
written through the same receipt/manifest/artifact/projection RPC path into a
different database. Both are independently dumped and restored with constraints,
checked for exact retries and six-relation full-row equality, then decoded into
detached C clones and re-encoded byte-for-byte. Rust verifies the DB-bound digest
and explicitly reports the expected inventory count: zero and five respectively.
Thus the tree fixture's encoded parent/sibling topology and item fields survive
this restore, not merely the item count. These remain synthetic bounded profiles,
not bank/world/PVC or live player installation coverage.

Restored C/Rust decoding at `abcb3c1` also passes (combined runner exit 0).
Log: `/tmp/muhan-restored-c-rust.log`. A fresh Linux container, sharing only
the disposable PG namespace, reads the exact known character/command payload
and stored digest from the restored database with read-only session settings.
The test uses the disposable administrator login for this extraction; it is not
a new least-privilege gameplay reader. It compiles the current native C oracle,
decodes a detached clone, re-encodes it and compares every payload byte. Current
Rust replay then accepts those restored bytes against the database digest.
No live player, socket, file projection or gameplay authority is changed.
This fixture has the canonical small inventory; broader persisted-world restore
and value-bearing nested inventory coverage remain separate requirements.

Restored operation checks at `026b8d3` pass as well (full runner exit 0).
Log: `/tmp/muhan-restored-runtime-contract.log`. Under the restored database's
reader session identities, full-payload SELECT remains available without DML,
while metadata-only reader cannot SELECT payload. These checks use session
authorization in the isolated fixture, not new external authentication proof.
Under writer session identity and writer role, manifest/artifact/projection RPCs
all return EXACT_RETRY for the original immutable command. Complete JSON-row
fingerprints remain identical after those retries across six private relations:
artifacts, receipts, level projections, manifests, legacy heads, writer epochs.
All six contain the expected nonempty fixture; no empty-table success is allowed.

Source `a93180b` passes the full Linux conformance/replay/backup runner (exit 0).
Log: `/tmp/muhan-valid-backup-writer.log`. The negative comparator database is
left unchanged. A separate empty database receives its schema, then a valid
fixture: setup inserts a character/absent head, acquires writer epoch and records
a receipt; the M4 manifest, snapshot artifact and level projection are written
through their RPCs under mud_writer_login session identity plus mud_writer role.
Triggers and foreign keys stay enabled.

Using the PG17 server image's pg_dump and pg_restore, the valid database is
restored to a third database with --exit-on-error. Nonempty full-row evidence
fingerprints match for artifacts, receipts and projections. This proves a
same-cluster schema/data restore of synthetic evidence with pre-existing roles,
not cross-cluster role/secret recovery, legacy PVC restoration, gameplay loading,
or operational backup acceptance. Both earlier negative controls remain recorded
below; no constraints were disabled to obtain this result.

### Initial failing attempt

Source `1fbe640` adds an actual server-version pg_dump/pg_restore into a second
database, with full-row fingerprints for artifacts, receipts and projections.
The helper accepts only the exact container ID marked by the disposable runner;
no arbitrary DB URL is accepted. PG tmpfs increased to 384 MB for two databases.

Actual result: replay/conformance still passes, but the added restore exits 1
when PostgreSQL validates `game_character_player_snapshot_v1_artifacts_receipt_fk`.
The pre-existing negative comparator fixture intentionally seeds artifact rows
without receipts using session_replication_role=replica, so it is not a valid
restorable game-state dataset. Log: `/tmp/muhan-replay-backup-restore.log`.
This is not evidence of production corruption. Do not suppress constraints or
count this dump as a successful backup proof. Next supply a separate valid
dataset through normal writes for restoration, keeping negative comparator
coverage independent. The combined runner currently returns failure at this
new gate, accurately reflecting incomplete backup acceptance.

## Verified result

Extended Linux run at `df59c7b` also exits 0. It checks the reused image lockfile
matches current sources, compiles the native C oracle and Rust projector, runs
the full relay suite (163 pass, 0 fail, 1 deferred conformance case), then runs
that remaining case with its actual C producer/Rust artifact verifier (1 pass,
0 skipped), before the PG17 replay contracts. Thus all 164 relay cases ran
successfully across the two required harnesses, including Linux-only descriptor
and file replacement cases. Log: `/tmp/muhan-linux-all-conformance-replay.log`.

The dedicated C unit gate initially failed GCC's fixed-array overread warning
because its malformed UUID test supplied a 19-byte literal to a 37-byte API
parameter. The malformed fixture now has the required storage length and still
fails UUID validation; production parsing was not changed. The C artifact unit,
C-to-Rust artifact verifier test, Node conformance, and DB rehearsal all pass.

Independent confirmation at `c077cf9`: another fresh isolated Linux/PG17 run
exited 0, now without the noisy ERR trap. Log:
`/tmp/muhan-linux-replay-confirmation.log`.

The relay's supported `pnpm --filter @muhan/m4-file-snapshot-manifest-relay test`
entrypoint also passed: 159 passed, 0 failed, 5 skipped (164 total), with native
C oracle and real Rust normalized projector prepared by the bridge harness.
Log: `/tmp/muhan-relay-supported-regression.log`. A preliminary direct tsx
invocation omitted this setup and failed three bridge tests; that invocation is
not the supported full-suite result. The five platform-specific skips are not
counted as passes. Typecheck and focused digest tests remain green as below.

Source `c6bf3e9` completed the full isolated Linux/PG17 harness with exit 0.
Log: `/tmp/muhan-linux-replay-digest-fix.log`. Current frozen TypeScript and
Rust sources were compiled inside the reused Linux test image; PostgreSQL used
tmpfs and no published ports or Docker socket. Owned containers were cleaned up.

Root cause of the final DECODE_MISMATCH: the rehearsal CLI omitted mandatory
`snapshotSha256` when invoking the Rust process adapter. A new regression first
failed, then passed after binding both local and database verifier calls to the
immutable artifact digest. Related unit suites pass 14/14; typecheck passes.
No digest, receipt, filesystem or database-read checks were relaxed.

Actual coverage includes pre-migration DB_READ_ERROR, post-migration full-payload
MATCH using real Rust, actual reader login/role/read-only assertions, forbidden
reads/writes, comparator match/mismatch/missing/duplicate/read-error/sanitization
cases, and unchanged artifact/projection fingerprints. This is seeded immutable
replay evidence, not a live gameplay loader, backup restore, production image,
or DB-authoritative gameplay cutover.

Removed the temporary general ERR trap after diagnosis: expected negative cases
also triggered it and printed misleading failure lines in the successful log.
The specific aggregate full-payload diagnostic remains for real failures.

## Earlier failure history

## Linux execution now available

`scripts/run-replay-reader-local-docker.sh --allow-disposable` reuses an existing
`REPLAY_RUNNER_IMAGE` without a new image build. It freezes current source,
compiles TypeScript and Rust on Linux in tmpfs, and shares only a fresh PG17
container's network namespace. No host port, Docker socket, production database,
or writable host checkout is provided; cleanup removes only created IDs.
The integration harness has an explicitly opted-in containerless psql mode for
this setup, fixed to loopback and the disposable fixture password.

Actual Linux execution passed the pre-migration DB_READ_ERROR negative control.
It then exposed two latent SQL test errors, fixed from failing runs: SQL quoted
identifier escaping used backslashes instead of doubled quotes; the password
baseline scalar subquery returned two rows because two relations are recorded.
The assertion now rejects any baseline row whose hash changed, preserving its
intended strength. Source changes: `688a37e`, `c831294`.

Latest full attempt at `c831294` still exited 1 after SQL assertions, without a
clear final diagnostic. It is not GREEN. Log:
`/tmp/muhan-linux-db-replay-baseline-fix.log`. Earlier failure logs:
`/tmp/muhan-linux-db-replay-retry.log`, `/tmp/muhan-linux-db-replay-sql-fix.log`.
Next preserve the CLI failure evidence before cleanup and locate the remaining
failure; do not suppress the failing return code or relax read-only contracts.

## Root cause established

The remaining INVALID_INPUT is an unsupported host, not demonstrated malformed
fixture data. `scanImmutableOutboxFilesWithPolicies` in `relay.ts` rejects both
an actual non-Linux process and a non-Linux platform parameter before opening
any file. The rehearsal's artifact loader invokes that filesystem and maps its
rejection to INVALID_INPUT. Both failed reruns used macOS Node 24.

The PG17 harness now verifies actual Node process.platform is linux before
creating a database. The new platform regression failed with the old harness
and passes with the preflight. Production descriptor-bound Linux checks remain
unchanged. The next full rehearsal must run its Node/Rust processes on Linux;
moving only PostgreSQL into Docker does not satisfy this prerequisite.

At source `9e6029f`, froze the repository under
`/tmp/muhan-db-replay-check.Tjnmru`, built the Rust release verifier offline,
and supplied the previously verified relay deployment package. Ran the existing
PG17 replay-reader integration harness with disposable consent, assigned
loopback port and 192 MB tmpfs database. No production DB was used and every
owned test container was removed by the harness.

First run failed because `/tmp` is a symlink on macOS: the CLI's ESM direct-run
comparison does not match its real `/private/tmp` module path. Direct invocation
through `/tmp` silently returned zero with no output; the physical path emitted
the expected diagnostic and nonzero status for missing configuration. Updated
the harness root resolution to `pwd -P` so this test executes the CLI physically.

The physical-path rerun advanced to a different failure: pre-migration rehearsal
returned `INVALID_INPUT` with null identities instead of expected `DB_READ_ERROR`.
Repeating with physical TMPDIR produced the same result. This is unresolved;
do not count either run as a passing replay/restore test. The production strict
input checks have not been relaxed. Next investigate artifact loading versus
configuration validation with the exact generated fixture, preserving diagnostics
without exposing payloads or credentials.

Logs: `/tmp/muhan-db-replay-check.log`,
`/tmp/muhan-db-replay-canonical-check.log`,
`/tmp/muhan-db-replay-realpath-check.log`.
The harness removes temporary artifact directories on exit; its current cleanup
therefore limits post-failure inspection. Frozen sources and compile outputs remain.
