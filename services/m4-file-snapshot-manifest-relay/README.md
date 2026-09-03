# M4 file snapshot manifest relay

This one-shot relay reads the immutable C `legacy-file-manifest-v1` outbox and
records only its hash/identity manifest through
`private.record_m4_file_snapshot_manifest_for_receipt`. It never reads player
payloads and never deletes, renames, rewrites, or acknowledges an outbox file.

The scanner requires a process-owned `0700` directory and process-owned,
single-link regular `0600` files. Symlinks, FIFOs, hard links, races, malformed
names, non-canonical manifests, and files at or above 2048 bytes are rejected.
At most 256 candidate files are considered, in bytewise lexical filename order.

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
