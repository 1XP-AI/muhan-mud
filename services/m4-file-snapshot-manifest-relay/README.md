# M4 file snapshot manifest relay

This one-shot relay reads the immutable C `legacy-file-manifest-v1` outbox and
records only its hash/identity manifest through
`private.record_m4_file_snapshot_manifest_for_receipt`. It never reads player
payloads and never deletes, renames, rewrites, or acknowledges an outbox file.

The scanner requires a process-owned `0700` directory and process-owned,
single-link regular `0600` files. Symlinks, FIFOs, hard links, races, malformed
names, non-canonical manifests, and files at or above 2048 bytes are rejected.
At most 256 candidate files are considered, in bytewise lexical filename order.
The relay is Linux-only: on non-Linux platforms it fails closed because Node
does not provide the descriptor-relative `openat` operation needed to prevent a
root rename/replacement race.

Run once with:

```sh
M4_FILE_SNAPSHOT_OUTBOX_DIR=/path/to/outbox \
DATABASE_URL="$MUD_WRITER_DATABASE_URL" \
  pnpm --filter @muhan/m4-file-snapshot-manifest-relay start -- --once
```

`DATABASE_URL` must be a direct PostgreSQL URL for `mud_writer_login`; the
`MUD_WRITER_DATABASE_URL` example above is expected to be injected by the
runtime secret provider rather than written into source or shell history. The
adapter issues `SET ROLE mud_writer` and then only the parameterized M4
function call. SQLSTATE `22023` is counted as invalid, `P0001` as conflict,
and class `08`/transport failures as retryable. The JSON output is aggregate
only and contains no paths, names, hashes, payloads, or credentials.

The dedicated `player-snapshot-v1-artifact-cli` image builds the pinned Rust
`player_snapshot_v1_replay_verify` binary and injects its absolute path through
`M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH`. The artifact relay sends only the
already-parsed CDTO payload to that binary; `replayObserved` and
`replayDisabled` are aggregate observation counters only, and neither result
changes database recording or legacy M4 headers. A missing runner, an invalid
path, execution failure, or invalid report is counted as `replayDisabled`.

Onboarding-eligibility fulfillment is a separate, default-OFF authority step.
Only `M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED=true` supplies that
side effect after the immutable artifact has recorded or exactly retried; an
absent, false, or other value never invokes the fulfillment database function.
The relay never deletes or acknowledges C-side artifact, receipt, or source
evidence, including when fulfillment is retryable.

## Replay shadow-journal differential

The separate replay differential command reads only journal JSON metadata and
the immutable PlayerSnapshotV1 artifact relation. It is explicitly opt-in and
requires independent absolute-path and read-only connection settings; it never
uses `DATABASE_URL` or any relay writer configuration.

```sh
M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_JOURNAL_PATH=/absolute/journal/path \
M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL="$MUD_REPLAY_READER_DATABASE_URL" \
  pnpm --filter @muhan/m4-file-snapshot-manifest-relay replay-differential -- --once
```

The supplied connection must not use `mud_writer_login`. Entries are processed
in bytewise lexical filename order, with a hard limit of 256 JSON journal
entries per invocation; a set above that bound is reported as `INCONSISTENT`
without reading an entry or querying the database. The stable top-level
`classification` is `EXACT`, `MISSING`, or `INCONSISTENT`; detailed record
reasons remain metadata-only evidence, and the CLI exits zero only for
`EXACT`.

The command performs only one parameterized `SELECT` per valid journal record
and emits fixed metadata evidence only: it never emits payloads, game state,
paths, parse errors, or credentials. It performs no database mutation, repair,
authority change, or authority cutover; its output is observational evidence
only.

## V2 journal level shadow comparator

This independent default-OFF command compares only closed V2 journal level
metadata with the read-only level-projection relation. It does not use relay
writer configuration or `DATABASE_URL`.

```sh
M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH=/absolute/journal/path \
M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL="$MUD_REPLAY_READER_DATABASE_URL" \
  pnpm --filter @muhan/m4-file-snapshot-manifest-relay v2-journal-level-shadow-comparator -- --once
```

The supplied URL must be the dedicated read-only replay-reader URL and must
not equal `DATABASE_URL`. The command emits one metadata-only JSON line, closes
its reader before returning, and exits zero only when every journal record is
an eligible `MATCH`; invalid or bounded journal input and every comparison
failure are fail-closed and exit nonzero.
