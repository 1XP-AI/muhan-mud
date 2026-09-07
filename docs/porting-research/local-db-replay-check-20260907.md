# Local DB replay check — incomplete

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
