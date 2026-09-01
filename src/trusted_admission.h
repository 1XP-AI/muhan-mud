#ifndef TRUSTED_ADMISSION_H
#define TRUSTED_ADMISSION_H

#include "mtype.h"
#include "player_path.h"

#define TRUSTED_ADMISSION_UUID_LEN 36
#define TRUSTED_ADMISSION_NONCE_LEN 32
#define TRUSTED_ADMISSION_HMAC_HEX_LEN 64
#define TRUSTED_ADMISSION_MAX_LINE 256
#define TRUSTED_ADMISSION_REPLAY_LIMIT (2 * PMAX)

typedef struct trusted_admission_ticket {
    char user_id[TRUSTED_ADMISSION_UUID_LEN + 1];
    char character_id[TRUSTED_ADMISSION_UUID_LEN + 1];
    char nonce[TRUSTED_ADMISSION_NONCE_LEN + 1];
    char name[PLAYER_NAME_MAX_BYTES + 1];
    long expires_at;
} trusted_admission_ticket;

/* 0 means legacy mode, 1 means ticket mode, and -1 is an invalid
 * MUD_ADMISSION_SECRET configuration.  Invalid configuration is fail-closed. */
int trusted_admission_mode(void);

/* Validate and consume a single ticket.  The line may include one terminal
 * LF for unit-test convenience; the socket command dispatcher passes it
 * without the LF.  A successful validation consumes its nonce. */
int trusted_admission_validate(const char *line, long now,
                               trusted_admission_ticket *ticket);

/* Exposed for deterministic vector tests and gateway interoperability. */
int trusted_admission_hmac_hex(const char *secret, const char *signed_part,
                               char out[TRUSTED_ADMISSION_HMAC_HEX_LEN + 1]);

/* Test-only configuration/reset hooks.  Production uses MUD_ADMISSION_SECRET. */
int trusted_admission_set_secret_for_test(const char *secret);
void trusted_admission_reset_for_test(void);

#endif
