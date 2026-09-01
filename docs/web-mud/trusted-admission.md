# Trusted admission: private MUD TCP contract

`MUD_ADMISSION_SECRET` is optional only for explicit legacy compatibility and
tests. Production sets `MUD_REQUIRE_TRUSTED_ADMISSION=1`; with that invariant a
missing secret is invalid instead of enabling the legacy login path. If the
secret is present (including an empty value), it must be 32 through 512 printable
ASCII bytes. An invalid or required-but-missing configured value is fail-closed:
the server exits before binding the listener,
so readiness fails and no connection can fall back to a legacy prompt.

In ticket mode the MUD TCP listener (`:4000`) accepts one line only. It does
not print the legacy banner, name prompt, site-password prompt, or password
prompt, and it does not start the legacy ident child. The private gateway is
the only intended peer for this port.

## Wire format

The gateway sends exactly this ASCII line, terminated with `\n`:

```text
MUD1|<exp_unix>|<nonce32_lowerhex>|<user_uuid_lower>|<character_uuid_lower>|<name_utf8_lowerhex>|<hmac_sha256_64_lowerhex>\n
```

The signed portion is exactly the bytes before the final HMAC separator:

```text
MUD1|<exp_unix>|<nonce32_lowerhex>|<user_uuid_lower>|<character_uuid_lower>|<name_utf8_lowerhex>
```

`hmac_sha256_64_lowerhex` is `HMAC-SHA256(MUD_ADMISSION_SECRET, signed bytes)`
encoded as 64 lowercase hexadecimal characters. UUIDs are strictly lowercase
`8-4-4-4-12` hexadecimal strings. `nonce` is 32 lowercase hexadecimal
characters. The name field is 2 through 28 lowercase hexadecimal characters,
which decode to UTF-8 and must already equal the legacy canonical form: every
ASCII uppercase byte is folded, then byte zero is capitalized only when it is
an ASCII `a` through `z` (matching C `lowercize(name, 1)`).

The Gateway issues a ticket for at most 15 seconds, and the C server accepts
timestamps from `now` through `now + 30 seconds`. It
keeps at least `2 * PMAX` nonce entries, evicts expired entries, and consumes a
nonce only after the complete syntactic, timestamp, and HMAC validation has
succeeded. A consumed nonce cannot be reused before its expiry.

For every malformed, expired, future, replayed, unsigned, wrong-name, missing,
corrupt, or recovery-blocked admission, the sole protocol response is:

```text
MUD1 ERR\n
```

and the socket is closed. A successful admission loads the canonical legacy
player, disconnects an existing same-name session safely, initializes the
player, records the user/character/nonce IDs only in non-persisted `extra`,
and first sends:

```text
MUD1 OK\n
```

before entering the normal command state.

## Operational limit

This is a hop-authentication mechanism, not a public-client protocol. Its
security relies on a strong shared secret and a private, access-controlled TCP
path between gateway and MUD. The replay cache is process-local; multiple MUD
processes require sticky routing or a shared replay store before they can safely
accept the same ticket stream. The identity fields are session metadata only;
the legacy player file remains name-keyed until the database ownership migration
is completed. The Helm deployment therefore hard-codes one MUD replica with a
`Recreate` strategy and injects both `MUD_REQUIRE_TRUSTED_ADMISSION=1` and the
shared secret. The replay cache is reset by a process restart; the short TTL,
private NetworkPolicy, and prohibition on ticket logging bound that residual
risk but do not make multi-replica admission safe.
