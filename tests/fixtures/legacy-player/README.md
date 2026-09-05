# Legacy player projection fixture corpus

`tests/harness/legacy_player_oracle.c` generates this small corpus from the
audited native C layout. Its compile-time offset/size assertions and runtime
little-endian check pin the fixtures to the current `mstruct.h` ABI; a changed
ABI intentionally fails the harness build instead of silently broadening
coverage.

The corpus consists of `empty`, `mixed-case`, `nested`, `truncated`, and
`invalid-count`. `mixed-case` stores raw `aLiCe` and proves that the C oracle
and Rust projection both follow the repository contract: ASCII lowercase every
byte, then capitalize the first byte only when it is an ASCII letter, before
DTO naming, SHA-1 sharding, and source-path construction.
Each test generates it in a temporary directory, so pointer-bearing records
are never checked in as portable-looking production fixtures. This is neither
an authority nor a decoder for arbitrary production saves, unsupported
compression, CP949 text, or other encodings.
