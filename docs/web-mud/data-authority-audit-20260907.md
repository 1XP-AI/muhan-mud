# Data-authority audit — 2026-09-07

## Current authority map

| Concern | Current authority | Evidence/boundary |
| --- | --- | --- |
| Human account/JWT/profile | Supabase Auth + `public.profiles` | `20260901000000_profiles_and_lobby_presence.sql` |
| Account ↔ character ownership, lifecycle, canonical name, shard, imported file digest | `public.game_characters` | `20260902000000_game_identity.sql`; service-only RPCs and RLS |
| Online lease and onboarding intent/handoff state | Private Postgres relations | `character-authorizer.ts`, `onboarding-authorizer.ts`, migrations 03/09/22 |
| Admission decision | Gateway DB lease + C-verified ticket | `services/gateway/src/gateway.ts:434-468`; C trusted-admission controls |
| Live name/password/player bytes, level, HP/MP, inventory, location, timers, world state | Legacy files + C process memory | `docs/web-mud/game-identity-refactor.md`; `src/player_store.h` |
| Snapshot/artifact/projection rows | Immutable Postgres evidence only | migrations 15/19/30/40/50/60/70; no gameplay loader or cutover |

The DB does not currently become gameplay authority. `game_characters.imported_file_sha256` is import/claim evidence, not a current-save digest: normal C saves can change the file. The C `legacy_identity_evidence_inspect()` path reads and validates the legacy file, then emits metadata only; `onboarding_evidence_emission.c` rejects any name or digest mismatch. The C snapshot artifact/capture comments explicitly keep this as a test/evidence boundary, not a production live-object loader.

## Flow and implemented bounded fulfillment

Gateway onboarding is: browser JWT → `begin` intent → provision `reserve` or claim `challenge` → C private admission and save/password proof → `finalize`/`claim` evidence → `activateHandoff` → active lease. Activation atomically creates one pending snapshot-eligibility outbox row. The ordinary reconciler still validates exact receipts and player path/hash, then calls only `reconcile_game_character_provisioning`; committed receipts make no ordinary reconcile mutation. Its separate `--fulfill-pending-snapshot-eligibility` entrypoint now reads the bounded private list using the dedicated `onboarding_snapshot_eligibility_login` and sends only each listed character/immutable command pair to the two-argument writer RPC as `mud_writer_login` with `SET ROLE mud_writer`.

The M4 artifact relay can also invoke that same RPC after recording or exactly retrying the immutable artifact, but only when `M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED=true`; absent, false, or any other value leaves fulfillment off. M3 is likewise default-off: an absent or `off` `MUD_M3_MODE` performs no M3 runtime I/O, and shadow capture requires the M3-enabled build plus literal `MUD_M3_MODE=shadow` and `MUD_M3_PLAYER_SNAPSHOT_V1=handoff`. These are implemented, opt-in evidence paths, not evidence of deployment or of a database gameplay-read cutover.

## Implemented contract

`list_pending_game_character_onboarding_snapshot_eligibility` returns a bounded, deterministic page with the exact immutable correlation/actor/character/mode/command binding. The finite reconciler pass rejects a malformed, duplicated, or unbounded batch before any writer call; it neither discovers a command from artifact data nor reads payloads. `fulfill_game_character_onboarding_snapshot_eligibility(character_id, command_id)` remains the sole fulfillment writer: it accepts only the pre-bound command and exact artifact/M3 receipt/M4 manifest evidence, returns `FULFILLED`, `EXACT_RETRY`, `ALREADY_FULFILLED`, or `NOT_ELIGIBLE`, and does not scan for “latest” artifacts or derive identity from names, hashes, or timestamps.

### Required invariants

1. Every returned row is `status=pending`, has one exact active handoff, finalized intent, owner/lifecycle consistency, and a command binding owned by the same correlation/character/mode.
2. Fulfillment succeeds only when the artifact, M3 receipt, and M4 manifest all match character/world/name/storage format, receipt request/post hashes, writer instance/epoch/revision, acknowledgement time, and snapshot hash/octet count; existing SQL checks this and must remain immutable.
3. One `(correlation_id)` yields at most one fulfillment; exact retries return `EXACT_RETRY` (or equivalent), while a substituted character/command/mode/hash fails closed.
4. Pending, fulfilled, rejected, and retry-exhausted outcomes are aggregate-only; no names, paths, hashes, payloads, credentials, or per-row durable checkpoint is logged by the worker.
5. A transient/indeterminate HTTP failure is retried with the same exact tuple and bounded attempts; no activation, claim, unclaim, artifact mutation, or file mutation is performed by this pass.
6. Fulfillment does not alter `public.game_characters.lifecycle` or make the projection a gameplay read source. The authority transition is only `pending eligibility → immutable fulfilled baseline evidence`; live gameplay remains legacy-file authoritative.

### TDD/state-transition matrix

| Input state | Action | Expected state/result |
| --- | --- | --- |
| pending eligibility + exact bound artifact/evidence | fulfill RPC | one immutable fulfillment row; outbox `fulfilled`; `FULFILLED` |
| same tuple retried | same fulfill RPC | unchanged rows; `EXACT_RETRY` |
| missing, stale, or mismatched artifact/receipt/manifest/binding | fulfill RPC | `NOT_ELIGIBLE`; no candidate lookup or lifecycle mutation |
| malformed, duplicate, or unbounded pending-list batch | finite worker | no writer call; aggregate rejection |
| RPC timeout/5xx | bounded exact retry | pending unless a later exact retry proves fulfillment; retry-exhausted aggregate |
| committed onboarding receipt | existing receipt path | no normal reconcile mutation; only pending-outbox consumer may fulfill |

### Code/migration boundaries

The implemented boundary is `services/onboarding-reconciler/src/reconciler.ts` plus migrations 23–27 and 20261010–13; the normal receipt reconciler retains its filesystem safety boundary and is not broadened into a snapshot scanner. Keep `services/gateway/src/onboarding-authorizer.ts`, `src/player_store.h`, legacy identity inspection, and C snapshot capture out of any authority cutover. A later, separately gated milestone would still need dual-write/replay/restore evidence before a versioned snapshot could become a gameplay read source.

## Existing test evidence

Gateway tests cover exact auth/lease/challenge/claim/activation ordering and private C evidence controls (`services/gateway/test/gateway-handoff-gate.integration.test.ts`). Reconciler tests cover strict receipt parsing, no-follow file/hash checks, default reconcile-only behavior, opt-in finalizer→activation ordering, and the bounded eligibility-list/exact-writer pass, including malformed batches, retries, terminal outcomes, and direct-login role boundaries (`services/onboarding-reconciler/test/reconciler.test.ts`). The disposable real-stack test exercises the explicit M3/M4 fulfillment opt-in and exact retry; it is CI-only evidence, not deployment evidence (`tests/stack-e2e/stack-e2e.test.ts`).
