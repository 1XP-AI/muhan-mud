# Local DB replay check — incomplete

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
