# Persistence porting vertical-slice backlog

Inventory scope: committed legacy C player save/load paths, PlayerSnapshotV1 and
M3 character-journal paths, and committed Supabase migrations/contracts. This is
an evidence-backed planning artifact; no implementation or broad test was run.

## Current authority and boundaries

### Facts

- The legacy player file is the gameplay source today. `save_ply`/`load_ply`
  dispatch through the replaceable `PlayerStore` seam
  (`src/player_store.c:104-116`), whose default implementation is the file
  store (`src/player_store.c:3-25`).
- A player save serializes the creature graph to a temporary file, fsyncs it,
  renames it into place, then fsyncs the parent directory
  (`src/file_player_store.c:258-318`). The path is SHA-1-sharded by the name
  (`src/player_path.c:115-149`; summarized in
  `docs/porting-research/persistence-supabase.md:44`).
- The bounded player decoder validates fixed strings, bounds nested object
  count/depth, clears runtime links, and requires EOF
  (`src/files1.c:546-735`, `src/files1.c:608-678`). It is therefore the safe
  legacy read boundary for import/differential testing; raw struct bytes are not
  a DB contract.
- Ordinary login still owns side effects after load: `init_ply` can load rooms,
  add permanent world objects, log/broadcast, update timers, and consume files
  (`src/player.c:273-299`, `src/player.c:239-254`). Staging a character and
  activation must remain separate from persistence conversion.
- Periodic save is invoked from the live tick (`src/player.c:781-784`), and
  disconnect/quit performs `uninit_ply` immediately before save
  (`src/player.c:306-310`). These are the principal write trigger classes.
- Legacy player state includes the creature plus a recursive object tree; the
  intended DB model is character state plus item instances/locations
  (`docs/porting-research/persistence-supabase.md:41-46,87-100`). Ready items
  are normalized back into inventory on save (`persistence-supabase.md:44`).
- Bank persistence is a separate direct/truncate path and bank operations call
  player and bank saves separately (`persistence-supabase.md:46`). This is a
  known split-wealth risk, not safe to solve by dual-writing only PlayerStore.
- `public.game_characters` is an ownership/index surface. Existing
  `private.game_character_snapshots` is explicitly optional/read-model, not
  gameplay authority (`persistence-supabase.md:5-14`; migration
  `supabase/migrations/20260902000000_game_identity.sql:331-356`).
- M3 migrations provide writer epochs/routes, legacy head compare-and-swap, and
  idempotent shadow receipts keyed by `(character_id, command_id)`
  (`20260909000000_m3_shadow_receipts.sql:40-79,326-451`). The live route is
  read-only route resolution (`20260912000000_m3_live_save_route.sql:6-135`).
- PlayerSnapshotV1 is an immutable, receipt-anchored CDTO artifact, not
  gameplay/file authority. Its migration validates payload structure/hash,
  binds the artifact to the M3 receipt, and grants a narrow writer function
  (`20260915000000_player_snapshot_v1_artifacts.sql:194-250,254-320,323-414`).
  The level projection derives one validated raw-U8 field and is likewise
  evidence/read-model only (`20260919000000_player_snapshot_v1_level_projection.sql:7-46,105-159`).

### Inferences (must be confirmed during implementation)

- The safest first DB slice is character aggregate read/import plus shadow
  artifact publication, not DB gameplay reads: existing contracts deliberately
  stop short of a normalized `private.mud_character_state` writer.
- A `PlayerStore` adapter can provide a character-scoped persistence seam, but
  it cannot make character+bank+item movement atomic; that requires changing
  the command/bank boundary, not just replacing file I/O.
- Snapshot artifacts are suitable as deterministic migration evidence and
  differential-test input, but should not be treated as a replacement for the
  normalized item graph or as a cutover signal by themselves.

## Proposed vertical slice: character save/load shadow path

### Slice S1 — freeze and fixture the legacy contract (independent)

1. Capture a deterministic fixture set through the bounded player decoder:
   empty/new character; scalar-rich character; equipped/ready items; nested
   containers at depth 0/1/max; timers and quests; malformed/truncated input;
   invalid UTF-8/NUL and over-limit counts. Store expected canonical DTO,
   object ordering, source byte length, and SHA-256. Never include password
   material in expected DB fields.
2. Define the canonical mapping from exact legacy name/world/shard to
   `game_characters.id`; unmatched or conflicting names quarantine rather than
   auto-claim. Preserve the legacy file hash as evidence only.
3. Differential oracle: decode legacy file, encode PlayerSnapshotV1, decode the
   snapshot, and compare persisted fields plus recursive item graph. Ignore
   runtime pointers/descriptors/ready pointers and document every ignored field.

Evidence: `src/files1.c:546-735`, `src/player_snapshot_v1.c:206-498`,
`supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql:194-250`.

### Slice S2 — resumable read-only importer (independent after S1)

1. Read player files only; record `(source_path, source_sha256, size, parser
   version, ABI, start/end marker, result, quarantine reason)` in an import
   ledger. Re-running identical bytes must be idempotent; changed bytes create
   a new revision.
2. Materialize proposed normalized rows for character scalar state, timers,
   item instances, and item locations. Enforce one owner per item, preserved
   list position, no cycles, and no runtime links. Do not change any legacy
   file or make the game read shadow rows.
3. Bind every imported character to the existing ownership index only after
   exact canonical name/world/shard matching. A provisioning/claim flow must
   remain distinct from ordinary save import.

Evidence: `persistence-supabase.md:139-149`, target invariants at
`persistence-supabase.md:87-102`, identity migration at
`20260902000000_game_identity.sql:55-75`.

### Slice S3 — receipt-bound PlayerSnapshotV1 publication (parallel with S2)

1. At the prepared M3 save observation boundary, capture a detached canonical
   PlayerSnapshotV1 artifact. Keep legacy file and M3 journal as save authority.
2. Publish only after the legacy receipt is DB-acknowledged, using the existing
   receipt-bound artifact function; retry with the same command ID and payload
   must be exact/idempotent, while a changed payload must reject.
3. Add reconciliation reporting for missing/mismatched artifact, source hash,
   octets, writer epoch/revision, or snapshot digest. A missing artifact must
   not fail the legacy save or alter gameplay authority.

Evidence: `src/character_player_snapshot_v1_handoff.h:7-12,23-26`,
`src/character_player_snapshot_v1_artifact.h:25-55`, and migration
`20260915000000_player_snapshot_v1_artifacts.sql:367-414`.

### Slice S4 — legacy-authoritative dual-write facade (after S2/S3)

1. Extend the persistence facade around every character write trigger: periodic
   save, disconnect/quit save, explicit save, and recovery save. The facade
   records command ID, pre/post file hashes, writer epoch/revision, and desired
   shadow operation before legacy replacement; it marks completion only after
   the file is durable.
2. A relay applies the shadow operation idempotently through narrow DB RPCs.
   File success remains authoritative when DB shadow publication fails; retain
   repair records until receipt and reconciliation watermark.
3. Differentially compare decoded post-save file with the shadow normalized
   state and PlayerSnapshotV1 artifact. Exercise crash points around journal
   intent, file replacement, receipt publication, and relay retry.

Evidence: `persistence-supabase.md:151-165`; legacy atomic save
`src/file_player_store.c:272-317`; M3 receipt CAS/idempotency
`20260909000000_m3_shadow_receipts.sql:394-451`.

### Slice S5 — DB-canonical character read/write (blocked on schema/RPC review)

1. Add reviewed normalized character/item tables and operation-specific RPCs
   with command idempotency, expected revision, lease/writer fencing, and
   atomic character+item graph updates. Existing migrations do not provide
   this gameplay writer, so this is a new contract rather than an extension of
   the artifact reader.
2. During cutover, stop admissions, drain commands, reconcile a final
   watermark, and flip an audited feature flag. C loads DB state into the
   legacy runtime; file output becomes a deterministic derived projection.
3. Do not cut over bank/economy until character+bank+item movement commits in
   one transaction. The separate legacy `savegame`/`save_bank` calls are the
   acceptance test for this gate.

Evidence: `persistence-supabase.md:111-135,167-178,238-253`.

## First differential-test boundary

Use the post-save file boundary, before `init_ply` activation side effects:

`legacy save trigger → durable legacy file → bounded decode → PlayerSnapshotV1 → normalized shadow projection → compare`

This boundary is deterministic and excludes room loading, broadcasts, socket
state, process-local timers, pointers, and other runtime-only data. The test
must compare scalar fields, timer slots, object graph parent/order/equipped
semantics, source/post hashes, and revision/command identity. It must explicitly
assert that credentials are absent from DB payloads and that replaying the same
command ID is stable.

## Parallel work map

| Workstream | Can start independently? | Dependency / handoff |
| --- | --- | --- |
| Fixture corpus + decoder oracle | Yes | Defines S1 DTO and expected hashes |
| Import ledger + read-only importer | After fixture schema | Produces quarantines and proposed normalized rows |
| Snapshot artifact capture/publication | Yes, against existing M3 contracts | Hands receipt-bound artifacts to reconciliation |
| M3 relay/reconciliation | Yes | Consumes writer routes/receipts; no gameplay reads |
| PlayerStore dual-write facade | After command/hash contract | Must cover all character save triggers, not only `save_ply` |
| Bank/item transaction design | Yes, but separate critical path | Blocks economy cutover; must replace split player/bank writes |
| DB-canonical loader/projector | No | Requires normalized schema, RPCs, final reconciliation gates |
| Credential migration | Yes, isolated | Must not leak/import plaintext legacy password |

## Exit criteria for this lane

- Every fixture has stable DTO/checksum and explicit fact-vs-inference notes.
- Import is resumable/idempotent and quarantines malformed or unmatched files.
- Same command ID/hash retries exactly; different hash rejects.
- Crash/retry reconciliation has no silent divergence at the file/artifact
  boundary.
- No gameplay path reads shadow DB or PlayerSnapshotV1 before the separately
  reviewed DB-canonical gate.
- Bank/economy remains legacy-authoritative until one-transaction character,
  bank, and item semantics are proven.
