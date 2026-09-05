# Legacy identity evidence Rust handoff

`legacy_identity_evidence` is a versioned C in-memory report, not a byte
format.  Its C enum representation, field padding, and `unsigned int` width
are intentionally not an interoperability contract, so Rust must not use
`repr(C)` or decode a dumped struct.

The stable field-level handoff for a future Rust DTO is:

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

Before a Rust DTO is added, define a canonical byte envelope and add paired C
and Rust golden vectors.  The envelope must reject unknown mandatory fields,
must not contain C padding, pointers, descriptors, passwords, or raw player
bytes, and must preserve the C invariant that a digest is emitted only after
the exact opened file passes the legacy decoder unchanged.
