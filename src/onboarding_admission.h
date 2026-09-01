#ifndef ONBOARDING_ADMISSION_H
#define ONBOARDING_ADMISSION_H

#include "player_path.h"

/* MUD1O is intentionally independent of legacy MUD1 admission.  These
 * helpers accept one complete, bounded LF-terminated protocol line only. */
#define ONBOARDING_ADMISSION_UUID_LEN 36
#define ONBOARDING_ADMISSION_NONCE_LEN 32
#define ONBOARDING_ADMISSION_HMAC_HEX_LEN 64
#define ONBOARDING_ADMISSION_SHA256_HEX_LEN 64
#define ONBOARDING_ADMISSION_MAX_LINE 256
#define ONBOARDING_CONTROL_NAME_HEX_MAX (PLAYER_NAME_MAX_BYTES * 2)
#define ONBOARDING_CONTROL_STORAGE_FORMAT_MAX 32

#define ONBOARDING_ADMISSION_MODE_PROVISION 'P'
#define ONBOARDING_ADMISSION_MODE_CLAIM 'C'

typedef struct onboarding_admission_ticket {
    char mode;
    char user_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char correlation_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char nonce[ONBOARDING_ADMISSION_NONCE_LEN + 1];
    long expires_at;
} onboarding_admission_ticket;

/* Return 0 only after atomically recording nonce consumption.  The callback
 * receives no secret, raw ticket, HMAC, or game password. */
typedef int (*onboarding_admission_replay_fn)(void *context, const char *nonce,
                                               long expires_at);

/* Validate a MUD1O ticket against a caller-owned secret.  `line` must have
 * exactly one terminal LF; ticket metadata is copied to `ticket` only on
 * success.  The replay callback is mandatory and runs only after signature
 * and syntax validation. */
int onboarding_admission_validate_ticket(const char *line, const char *secret,
                                         long now,
                                         onboarding_admission_replay_fn replay,
                                         void *replay_context,
                                         onboarding_admission_ticket *ticket);

typedef enum onboarding_control_kind {
    ONBOARDING_CONTROL_INVALID = 0,
    ONBOARDING_CONTROL_OK,
    ONBOARDING_CONTROL_RESERVE,
    ONBOARDING_CONTROL_VERIFIED,
    ONBOARDING_CONTROL_SAVED,
    ONBOARDING_CONTROL_ERR,
    ONBOARDING_CONTROL_RESERVED,
    ONBOARDING_CONTROL_CLAIMED,
    ONBOARDING_CONTROL_COMMIT,
    ONBOARDING_CONTROL_ABORT
} onboarding_control_kind;

/* One typed control value.  It deliberately carries only non-secret protocol
 * metadata; callers must not use it as an evidence/log record. */
typedef struct onboarding_control {
    onboarding_control_kind kind;
    char name_hex[ONBOARDING_CONTROL_NAME_HEX_MAX + 1];
    char character_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char file_sha256[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
    char storage_format[ONBOARDING_CONTROL_STORAGE_FORMAT_MAX + 1];
} onboarding_control;

int onboarding_parse_c_control(const char *line, onboarding_control *control);
int onboarding_format_c_control(char *out, unsigned long out_size,
                                const onboarding_control *control);
int onboarding_parse_gateway_control(const char *line,
                                     onboarding_control *control);
int onboarding_format_gateway_control(char *out, unsigned long out_size,
                                      const onboarding_control *control);

typedef enum onboarding_state {
    ONBOARDING_STATE_NEW = 0,
    ONBOARDING_STATE_PROVISION_READY,
    ONBOARDING_STATE_PROVISION_AWAIT_RESERVED,
    ONBOARDING_STATE_PROVISION_RESERVED,
    ONBOARDING_STATE_PROVISION_AWAIT_COMMIT,
    ONBOARDING_STATE_CLAIM_READY,
    ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED,
    ONBOARDING_STATE_READY,
    ONBOARDING_STATE_FAILED
} onboarding_state;

/* State transitions are an explicit guard only; they do not perform network,
 * storage, password, or player-file work.  Failed calls leave `state`
 * unchanged. */
int onboarding_state_accept_ticket(onboarding_state *state,
                                   const onboarding_admission_ticket *ticket);
int onboarding_state_apply_c_control(onboarding_state *state,
                                     const onboarding_control *control);
int onboarding_state_apply_gateway_control(onboarding_state *state,
                                           const onboarding_control *control);

#endif
