# Onboarding receipt reconciler

This service performs finite, read-only scans of `MUHAN_HOME/onboarding-receipts/*.receipt`. It accepts only the exact eight-line C MUD receipt v1 record (strict UTF-8, canonical player name, lowercase UUIDs and SHA-256), verifies the no-follow regular player file at the C SHA-1 shard path, then invokes only `reconcile_game_character_provisioning` using service-role PostgREST credentials for a `saved` receipt. A `committed` receipt is still fully validated, but never makes a PostgREST call: it is durable proof that C already sent its COMMIT after database finalization.

The reconciler fails closed unless `MUHAN_HOME`, `onboarding-receipts`, `player`, and the selected player shard have no group or other permission bits, and the receipt/player files likewise have no group or other permission bits. Linux opens every path component from a retained directory descriptor with `O_NOFOLLOW`; all platforms also check regular-file metadata before and after reads. PostgREST responses must be a bounded JSON response with exactly the expected one-row RPC shape, and redirects are rejected so service credentials cannot be forwarded to another origin.

Build and execute one scan:

```sh
pnpm --filter @muhan/onboarding-reconciler build
MUHAN_HOME=/srv/muhan \
SUPABASE_INTERNAL_REST_URL=http://postgrest.internal:3000 \
SUPABASE_SERVICE_ROLE_KEY="$SUPABASE_SERVICE_ROLE_KEY" \
pnpm --filter @muhan/onboarding-reconciler start -- --once
```

Without `--once`, `ONBOARDING_RECONCILER_POLLS` bounds the number of scans (default `1`). `ONBOARDING_RECONCILER_POLL_INTERVAL_MS`, `ONBOARDING_RECONCILER_RPC_ATTEMPTS` (default `3`), `ONBOARDING_RECONCILER_RETRY_DELAY_MS`, and `ONBOARDING_RECONCILER_RPC_TIMEOUT_MS` (default `10000`) are bounded integer controls. The CLI writes only aggregate counts (`pending`, `reconciled`, `committed`, `rejected`, `retry_exhausted`) and never modifies receipts. It retains no per-scan observations: a successful `saved` receipt skips a repeat RPC only while its receipt inode/content/timestamps and player inode/content/hash are unchanged in the same process.

`--fulfill-pending-snapshot-eligibility` is a separate, finite command with no receipt or player-file scan. It requires `SUPABASE_SERVICE_DATABASE_URL` authenticated as `service_role` and `MUD_WRITER_DATABASE_URL` authenticated as `mud_writer_login`; it lists at most `ONBOARDING_SNAPSHOT_ELIGIBILITY_LIMIT` rows (default `100`, maximum `1000`) through the private service-only list RPC, validates every returned actor/correlation/character/mode/command tuple as one bounded batch, and invokes only `private.fulfill_game_character_onboarding_snapshot_eligibility(character_id, command_id)` under `SET ROLE mud_writer`. `FULFILLED`, `EXACT_RETRY`, and `NOT_ELIGIBLE` are the only accepted fulfillment results; malformed list rows fail closed before any fulfillment call, and transient adapter failures retry the unchanged pair only up to `ONBOARDING_RECONCILER_RPC_ATTEMPTS`.
