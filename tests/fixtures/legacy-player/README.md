# Legacy player projection fixture corpus

`tests/harness/legacy_player_oracle.c` generates this small corpus from the
audited native C layout. Its compile-time offset/size assertions and runtime
little-endian check pin the fixtures to the current `mstruct.h` ABI; a changed
ABI intentionally fails the harness build instead of silently broadening
coverage.

The corpus consists of `empty`, `nested`, `truncated`, and `invalid-count`.
Each test generates it in a temporary directory, so pointer-bearing records
are never checked in as portable-looking production fixtures. This is neither
an authority nor a decoder for arbitrary saves, compression, or CP949 text.
