# Legacy identity evidence Rust handoff

`legacy_identity_evidence` remains a versioned C in-memory report, not a Rust
ABI. Its C enum representation, field padding, and `unsigned int` width are
intentionally not an interoperability contract, so Rust must not use `repr(C)`
or decode a dumped struct.

The stable field-level handoff used by the Rust DTO is:

- `version`: unsigned integer, currently `1`.
- `result`: one of `ok`, `not_found`, `corrupt`, `io_error`, or
  `invalid_input`.  `invalid_input` means the requested name could not be
  canonicalized; no player file was opened, and it must not be interpreted as
  a new-character result.
- `canonicalization`: one of `canonical`, `normalized`, or `invalid`.
- `canonical_name`: UTF-8 legacy name, omitted when canonicalization is
  `invalid`.
- `legacy_shard`: exactly two lowercase hexadecimal ASCII characters, omitted
  when canonicalization is `invalid`.
- `player_file_sha256`: exactly 64 lowercase hexadecimal ASCII characters,
  present only when result is `ok`.
- `storage_format`: the ASCII identifier `player-v1`.

A Rust DTO is introduced only after the required canonical byte envelope and
paired C/Rust golden vectors exist. The closed `LegacyIdentityEvidenceV1`
codec and exact rejection rules are documented in
`docs/legacy_identity_evidence_wire_v1.md`; it preserves the C invariant that
a digest is emitted only after the exact opened file passes the legacy decoder
unchanged.
