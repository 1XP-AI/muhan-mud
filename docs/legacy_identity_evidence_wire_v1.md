# LegacyIdentityEvidenceV1 canonical wire

`LegacyIdentityEvidenceV1` is a closed, metadata-only C/Rust boundary for the
existing `legacy_identity_evidence` report. It is not a C ABI: no struct image,
padding, enum width, pointer, descriptor, password, player bytes, token, or
opaque extension data is encoded.

The bytes are `MUDLIE\0\0`, big-endian schema `u16` (`1`), big-endian version
`u16` (`1`), big-endian payload length `u32`, then exactly: outcome `u8`,
canonicalization `u8`, name length and UTF-8 canonical-name bytes, shard length
and two lowercase-hex bytes, SHA-256 length and 64 lowercase-hex bytes, and
storage-format length and ASCII `player-v1`. The maximum wire is 111 bytes.

Only `ok` carries a SHA-256. `not_found`, `corrupt`, and `io_error` carry a
canonical name and shard but no digest; `invalid_input` carries canonicalization
`invalid` and empty name, shard, and digest. Both implementations reject unknown
schema/version, wrong lengths, invalid UTF-8, non-lowercase hex, storage changes,
contradictory outcomes, trailing bytes, and other noncanonical input.

Every non-empty length-delimited text field is NUL-free: byte `0x00` is rejected
before decode copies any field into a C string buffer, and Rust rejects it when
encoding a `String`. The C encoder accepts only zero-filled fixed string buffers;
bytes after a C terminator are rejected rather than silently shortened, so no
embedded NUL can be encoded. This rule applies equally to canonical name, shard,
digest, and storage format, preserving byte-for-byte C and Rust round trips.

The codec consumes the report after existing inspection canonicalizes the name
and derives the shard; it performs neither operation. Shared goldens are in
`tests/fixtures/legacy_identity_evidence_wire_v1_*.hex`; run the C/Rust proof
with `scripts/run-legacy-identity-evidence-wire-differential.sh`.
