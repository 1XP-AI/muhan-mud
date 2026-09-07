# Live bank capture and transaction gap

## Confirmed current behavior

All ordinary `load_bank` / `save_bank` calls in `src/bank.c` route through
BankStore. However, no live caller invokes `bank_snapshot_v1_artifact_store`.
`bank_evidence.c` creates canonical bytes for validation and immediately frees
them; it does not publish a bank artifact. Passing synthetic relay/restore
tests therefore does not establish live bank capture coverage.

| Mutation | Current durable-call order | Problem |
| --- | --- | --- |
| Single / bulk item deposit | player save, bank save | player receipt can precede bank publication |
| Single / bulk item withdrawal | bank save, player save | bank publication can precede player receipt |
| Money deposit / withdrawal | bank save, player save | bank save result is ignored |
| Missing bank initialization / editor | bank save only | no accompanying player command receipt |

The legacy file writer uses in-place `write_obj` with no transaction joining
the player file. Reordering two independent file writes is not an atomic fix.
The existing payload RPC binds a bank digest to a player receipt/manifest, but
does not prove both reflect one successful transfer. Never infer that binding
from a name, newest receipt, timestamp proximity or successful bank save alone.

## Executed failure evidence

`tests/unit/bank_transfer_legacy_characterization.c` links the actual C
`deposit` and `withdraw` commands, replacing only storage/output boundaries.
It runs all-money transfers with initial player 100 and bank 50:

- Successful deposit and withdrawal preserve total 150.
- Failed bank save during deposit still saves player 0, bank remains 50.
- Failed bank save during withdrawal still saves player 150, bank remains 50.
- Both failures call bank save followed by player save and do not abort.

Executed successfully in isolated Linux ARM64 Docker with read-only source,
no network and tmpfs binaries. This test **documents a known defect**; a passing
characterization is not a successful failure-safety or cutover acceptance gate.
The local runner now includes it so the gap remains visible.

## Required next implementation

1. Introduce a command-scoped transfer intent holding exact character identity,
   command UUID, expected player/bank revisions and both proposed snapshots.
   Validate funds, limits and object ownership before mutating live state.
2. Commit player and bank state together in one PostgreSQL transaction with
   compare-and-swap revisions and immutable command-result replay. A retry must
   return the original result; a changed payload for the same command fails.
3. Add crash/failure tests before commit, after commit before acknowledgement,
   concurrent writers, stale revisions and retry. Assert conservation of money
   and unique object ownership rather than merely two successful file writes.
4. Connect all six gameplay mutation paths plus initialization explicitly.
   Legacy file mirrors require a durable outbox and recovery, not success
   messages before both authoritative changes are committed.
5. Compare legacy successful-operation results with the new transaction in
   shadow mode. Failure behavior must deliberately differ from the defect
   characterized above. Only then enable a versioned DB-authority gate.

No live capture hook, authority switch or deployment was added by this audit.

## Detached transfer planner implemented

Rust `bank_transfer_v1::plan_money_transfer` now accepts full player and bank
snapshots and an explicit positive amount. It validates both schemas, checks
funds, nonnegative balances, the existing 300-million deposit ceiling and i64
overflow before returning two owned proposed snapshots. Only player gold and
bank root value change; nested objects and other fields remain identical.
The input snapshots are never mutated. Zero or negative amounts are rejected;
this intentionally does not preserve the legacy empty "all" transfer behavior.

TDD: tests first failed with the missing module, then all three new integration
tests passed. They cover both directions, rejected conditions, exact other-field
preservation and a deterministic balance/amount matrix of deposit/withdraw
roundtrips. Full default Rust crate tests passed on Linux ARM64 in local Docker:
`/tmp/muhan-bank-transfer-rust.log` (exit 0).

This is the calculation stage, not an atomic persistence implementation. It
does not authenticate the actor or bind revisions, world/character identity,
command replay or writer epoch. The future transaction must perform those
checks and compare both input versions before publishing either output.
Live command integration and C/Rust command-level differential tests remain.

## Actual C / Rust command differential verified

Source `7db0146` extends the C characterization harness with a bounded numeric
command interface. Rust launches the binary linked with actual `src/bank.c`
deposit/withdraw code for 686 deterministic combinations (two directions,
seven wallet balances, seven bank balances, seven amounts). Compared results
include accepted final balances and rejected unchanged durable balances, with
zero amounts, insufficient funds and values around the 300-million deposit cap.
All 686 agree with the Rust planner. This replaces the command-differential
gap above for the tested explicit-amount money-transfer domain only.

The test requires an explicit C oracle and is ignored by default cargo runs;
the local Linux runner compiles the oracle and explicitly runs `--ignored`.
Full local runner passed exit 0, including bank file/PG E2E and both player/bank
restore profiles: `/tmp/muhan-bank-command-differential.log`.

Not covered by this comparison: item transfers, "all" parsing, i64 overflow
behavior in legacy C, storage failure recovery or a DB transaction. The known
legacy failed-save defect is still separately characterized and not reproduced
as desired Rust behavior. Next persistence work must bind both snapshot input
revisions and command identity, atomically commit both outputs, and prove retry
and crash behavior before any live switch.
