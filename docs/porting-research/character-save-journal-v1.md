# Character save journal v1

This is a deliberately separate type from the onboarding receipt. The
onboarding receipt records the MUD1O admission/claim lifecycle and its actor
and correlation data. A save journal records a writer command, legacy-file
publication, and database acknowledgement. Reusing the receipt would make a
save command depend on onboarding-only fields and would invite credentials or
ticket material into a writer record. The journal therefore has its own file
namespace, parser, and state machine.

The v1 record is metadata only. It never accepts a password, raw creature,
ticket, JWT, or player payload. Its exact line order is:

```text
version=1
state=prepared|legacy_published|db_acked
command_uuid=<lowercase UUID>
canonical_name_hex=<even lowercase hex, 1..14 decoded UTF-8 bytes>
expected_precondition=existing|absent
expected_pre_hash=<64 lowercase hex, or empty only for expected_absent>
post_hash=<64 lowercase hex intended new bytes>
storage_format=<bounded lowercase identifier>
world_id=<strict lowercase ASCII routing id, up to 64 bytes>
legacy_shard=<two lowercase hex digits, SHA-1(name)'s first byte>
writer_epoch=<decimal 1..INT64_MAX>
writer_revision=<decimal 1..INT64_MAX>
```

`existing` is mandatory for an update and requires the exact 64-character
pre-image digest. `absent` is an explicit new-file precondition; an empty
pre-hash never means “unknown”. `world_id` and the exact shard hint derived by
the shared `player_path_shard_from_name()` helper are part of the command
payload, so a shadow writer cannot silently route a name to the wrong legacy
shard. Epoch and revision are stored as portable `uint64_t` values but their
wire/API domain is the positive signed-`int64_t` range
`1..9223372036854775807`; zero, overflow, and `18446744073709551615` are
rejected.

Names must already equal the legacy `lowercize(name, 1)` result (initial ASCII
letter capitalized, remaining ASCII letters lowercased), in addition to the
shared UTF-8/name bounds. Thus `Terra` is canonical while `tERRA` and `TErra`
are rejected.

## Crash order and reconciliation

The caller stages the intended legacy bytes, fsyncs that temporary file, and
computes `post_hash`. It then durably writes `prepared` with both the expected
pre-hash and intended post-hash. Only after that record is durable does the
caller rename the staged legacy file and fsync its parent directory, then mark
`legacy_published`. Finally the database acknowledgement marks `db_acked`.

Journal files are written through a private 0600 temporary file, followed by
file fsync, rename/no-replace publication, and parent-directory fsync. The
journal directory is created and verified as 0700. Descriptor-relative
`O_NONBLOCK|O_NOFOLLOW` opens reject FIFOs, symlinks, and non-regular records;
sync retries also require a regular 0600 record. The implementation
fails to compile if the platform does not provide `O_NOFOLLOW` or
`O_DIRECTORY`, rather than silently weakening the check. Short writes and
`EINTR` reads/writes/fsyncs are retried within the bounded operation.

If a parent fsync fails after publication, the result is
`RENAME_DURABILITY_UNCERTAIN`; the caller must reconcile the exact command
record before retrying. If the no-replace staging link succeeds but removing
the staging name fails, the result is `RECONCILE_REQUIRED` (and a parent fsync
is attempted), because the canonical record is visible but duplicate staging
state remains. That duplicate hard link is deliberately retained for explicit
inspection/reconciliation; generic failure cleanup must not silently remove it.
An exact command+payload retry is byte-idempotent and
re-establishes file and parent fsync; a different payload, state skip/backward
transition, malformed/unknown field, or routing identity is rejected.

This v1 slice is intentionally not connected to `save_ply`, the live player
writer, bank, SQL migrations, or dual-write. It is synthetic-only until the
M3 writer integration has an independently reviewed reconcile policy.

## Gates before live wiring

The current component proves only the bounded metadata wire and local journal
state machine. Live M3 integration remains blocked until a later slice:

- descriptor-walks the trusted `MUHAN_HOME` ancestry and verifies directory
  ownership instead of protecting only the final journal component;
- persists `writer_instance_id`, `character_id`, the canonical receipt request
  digest, and an exact command-UUID-derived staged-artifact binding;
- opens and hashes the descriptor-safe staged/live player bytes before allowing
  `LEGACY_PUBLISHED`, and freezes rather than guessing on a mismatch;
- adds concurrent-process/crash recovery and PostgreSQL receipt/fencing tests,
  including exact same-writer renewal after restart; and
- enforces a cumulative read cap even if an already-open regular player file
  grows after its initial `fstat`.

The fault suite currently covers short/EINTR I/O, write/fsync/close failures,
parent-fsync uncertainty, retained duplicate staging after an unlink failure,
malformed records, FIFO/symlink/non-regular rejection, and sanitizer execution.
These tests do not satisfy the live-wiring gates above.

The hermetic unit test is run with:

```sh
cc -std=gnu89 -fcommon -DCHARACTER_SAVE_JOURNAL_TESTING -Isrc \
  tests/unit/character_save_journal_test.c src/character_save_journal.c \
  src/player_path.c src/resource_path.c src/utf8_text.c \
  -o /tmp/character-save-journal-test
/tmp/character-save-journal-test
```

The same source is also run under AddressSanitizer and UndefinedBehaviorSanitizer.
