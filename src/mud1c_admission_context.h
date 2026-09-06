/* MUD1C is a detached, caller-owned admission context codec.  It has no
 * socket, login, filesystem, replay-cache, database, or legacy-runtime
 * capability, and it is disabled unless explicitly compiled on. */
#ifndef MUD1C_ADMISSION_CONTEXT_H
#define MUD1C_ADMISSION_CONTEXT_H

#include <stddef.h>
#include <stdint.h>

#ifndef MUD1C_ADMISSION_CONTEXT_ENABLED
#define MUD1C_ADMISSION_CONTEXT_ENABLED 0
#endif

#define MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX 64U
#define MUD1C_ADMISSION_CONTEXT_UUID_LEN 36U
#define MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX 28U
#define MUD1C_ADMISSION_CONTEXT_NONCE_BYTES 32U
#define MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN 64U
#define MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN 64U
#define MUD1C_ADMISSION_CONTEXT_MAX_WIRE 384U
#define MUD1C_ADMISSION_CONTEXT_MAX_TTL_SECONDS 30L

#define MUD1C_ADMISSION_CONTEXT_OK 0
#define MUD1C_ADMISSION_CONTEXT_INVALID -1
#define MUD1C_ADMISSION_CONTEXT_DISABLED -2

/* The MAC is SHA-256 over this exact ASCII sequence, with each placeholder
 * replaced by its canonical field value and no LF:
 * MUD1C|world_id|actor_id|character_id|legacy_name_key_hex|expires_at|nonce_hex
 */
#define MUD1C_ADMISSION_CONTEXT_SIGNED_PREFIX "MUD1C|"

typedef struct mud1c_admission_context {
    char world_id[MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX + 1U];
    char actor_id[MUD1C_ADMISSION_CONTEXT_UUID_LEN + 1U];
    char character_id[MUD1C_ADMISSION_CONTEXT_UUID_LEN + 1U];
    char canonical_legacy_name_key[
        MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX + 1U];
    long expires_at;
    uint8_t nonce[MUD1C_ADMISSION_CONTEXT_NONCE_BYTES];
} mud1c_admission_context;

/* The default is feature-off.  Every codec operation returns DISABLED until
 * built with -DMUD1C_ADMISSION_CONTEXT_ENABLED=1. */
int mud1c_admission_context_enabled(void);

/* Compute lower-case HMAC-SHA256 for exact bytes.  This is exposed only for
 * deterministic vectors and interoperable formatting; it never reads state. */
int mud1c_admission_context_hmac_sha256_hex(const char *secret,
    const unsigned char *signed_bytes, size_t signed_length,
    char output[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U]);

/* Parse exactly one non-LF wire record.  `mac_hex` receives the supplied
 * lower-hex MAC; parse does not authenticate it or compare expiry. */
int mud1c_admission_context_parse(const unsigned char *wire, size_t wire_length,
    mud1c_admission_context *context,
    char mac_hex[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U]);

/* Format a canonical record, including its lower-hex HMAC.  On failure the
 * output buffer is cleared and `written` is zeroed. */
int mud1c_admission_context_format(const mud1c_admission_context *context,
    const char *secret, unsigned char *output, size_t output_size,
    size_t *written);

/* Parse, require now <= expires_at <= now + 30, and compare the supplied MAC
 * in constant time.  The output context is cleared on every failure. */
int mud1c_admission_context_validate(const unsigned char *wire,
    size_t wire_length, const char *secret, long now,
    mud1c_admission_context *context);

#endif
