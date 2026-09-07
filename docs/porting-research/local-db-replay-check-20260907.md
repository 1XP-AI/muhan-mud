# Local DB replay check — passed on Linux

## Verified result

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
