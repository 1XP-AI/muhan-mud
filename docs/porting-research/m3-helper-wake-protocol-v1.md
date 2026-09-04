# M3 helper wake protocol v1

## Purpose and delivery semantics

The M3 wake is a best-effort, lossy optimization signal from C to a future Rust helper. A received wake asks the helper to re-scan its durable artifact and outbox evidence; it neither identifies a player nor says which artifact changed. A lost, duplicated, delayed, or reordered wake is harmless: periodic/startup recovery and every normal scan remain responsible for discovering durable evidence, while legacy save behavior and database authority remain unchanged.

No production transport is part of v1. In particular, this repository adds no Unix socket I/O, helper process, database work, deployment, chart, or MUD runtime linkage.

## Framing

Every v1 frame is exactly 16 octets and is big-endian:

| Bytes | Field | Required value |
| --- | --- | --- |
| 0..7 | magic | ASCII plus NUL `MUHM3WK\0` (`4d55484d33574b00`) |
| 8..9 | version | unsigned 16-bit `1` |
| 10..11 | kind | unsigned 16-bit `1` (`WAKE`) |
| 12..15 | payload length | unsigned 32-bit `0` |

The sole canonical encoding is `4d55484d33574b000001000100000000`. A v1 decoder accepts only those exact 16 bytes; null, short, long, or any different magic, version, kind, or payload length is rejected.

## Privacy and authority boundary

The frame has no payload and must never contain credentials, player bytes, a pathname, character name, UUID, hash, or any other identity or durable-state reference. It confers no capability and has no acknowledgement, retry, durability, ordering, or authority meaning. Durable artifacts/outbox records, legacy save behavior, and the database continue to be the evidence and authority; the future helper must re-scan them independently of wake delivery.

## Contract tests

`tests/unit/m3_wake_v1_test.c` pins C encoding, exact decode, null/short/long rejection, and each variable field rejection under normal and ASan/UBSan builds. `rust/muhan-m3-wake-protocol/src/lib.rs` repeats the byte and malformed-input contract independently, while `rust/muhan-m3-wake-protocol/tests/differential.rs` compares canonical encoding and a deterministic malformed corpus with a separately compiled C oracle. `scripts/run-m3-wake-differential.sh` builds that disposable oracle with ASan/UBSan, runs the C sanitizer target, and executes the Rust differential test.

The supported differential command is `./scripts/run-m3-wake-differential.sh`; it builds a disposable C oracle, runs the C sanitizer/unit target, and runs all tests for `muhan-m3-wake-protocol` with `M3_WAKE_V1_C_ORACLE` set. Direct runs of the differential test require that variable to point to a compatible oracle binary.
