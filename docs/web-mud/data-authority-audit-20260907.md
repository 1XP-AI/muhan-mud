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

## Flow and remaining gap

Gateway onboarding is: browser JWT → `begin` intent → provision `reserve` or claim `challenge` → C private admission and save/password proof → `finalize`/`claim` evidence → `activateHandoff` → active lease. Activation atomically creates one pending snapshot-eligibility outbox row. The normal reconciler validates an exact receipt and player path/hash, then calls only `reconcile_game_character_provisioning` and expects `handoff_pending`; committed receipts make no RPC call. Its opt-in recovery calls the evidence finalizer followed by activation as one exact ordered pair. It has no implementation for polling the pending eligibility outbox and calling `fulfill_game_character_onboarding_snapshot_eligibility`.

Thus active identity is DB-authorized, but the first durable snapshot is not yet acknowledged as a complete DB-side baseline. Migrations 15/16/19/30/40/50/60/70 already provide immutable artifact, receipt, level/inventory/topology/bank/normalized-projection evidence, and migration 24 provides the fulfillment relation/RPC; the missing live-data-authority seam is the read-only eligibility consumer plus exact fulfillment call, not a gameplay read cutover.

## Recommended TDD-safe candidate

Add a dedicated private, service-only `list_pending_game_character_onboarding_snapshot_eligibility` RPC (bounded page, deterministic order, exact columns: correlation, actor, character, mode) and extend the reconciler with a finite `fulfillPendingSnapshotEligibility` pass. For each row, use only the exact command ID already bound by `register_game_character_onboarding_snapshot_command_binding`; call the existing fulfillment RPC, which must remain the sole writer. Do not scan for “latest” artifacts, derive correlation from names/hashes/timestamps, read payloads in the reconciler, or change the C PlayerStore/gameplay loader.

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
| missing artifact/receipt/manifest or any tuple mismatch | fulfill RPC | transaction abort; remains pending; rejected/fail closed |
| active character but wrong/stale command binding | worker | no artifact lookup by guess; rejected |
| RPC timeout/5xx | bounded exact retry | pending unless a later exact retry proves fulfillment; retry-exhausted aggregate |
| committed onboarding receipt | existing receipt path | no normal reconcile mutation; only pending-outbox consumer may fulfill |

### Code/migration boundaries

Implement only in `services/onboarding-reconciler/src/reconciler.ts`, its tests, and a new additive migration/RPC plus contract tests. Reuse the existing `OnboardingReconciler` filesystem safety and bounded HTTP parser, but do not broaden its file scan beyond receipt/player validation. Keep `services/gateway/src/onboarding-authorizer.ts` unchanged except for any separately justified RPC contract type; keep `src/player_store.h`, legacy identity inspection, and C snapshot capture out of the authority cutover. A later, separately gated milestone may make a versioned snapshot the gameplay read source after dual-write/replay/restore evidence; this candidate is only the durable baseline handoff.

## Existing test evidence

Gateway tests cover exact auth/lease/challenge/claim/activation ordering and private C evidence controls (`services/gateway/test/gateway-handoff-gate.integration.test.ts`). Reconciler tests cover strict receipt parsing, no-follow file/hash checks, default reconcile-only behavior, opt-in finalizer→activation ordering, retries, and committed no-call behavior (`services/onboarding-reconciler/test/reconciler.test.ts`). Importer tests cover serializable batch idempotency and immutable per-member identity tuples (`services/character-inventory-importer/test/batch-import.test.ts`). C/Rust differential tests cover the metadata-only identity wire (`rust/muhan-core-dto/tests/legacy_identity_evidence_wire_differential.rs`); none of these prove pending snapshot eligibility is consumed, which is the candidate's key coverage gap.
