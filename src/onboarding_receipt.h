#ifndef ONBOARDING_RECEIPT_H
#define ONBOARDING_RECEIPT_H

#include "onboarding_admission.h"

/* Durable, non-secret provenance for a feature-on MUD1O provision.  These
 * files are deliberately correlation-addressed so an external reconciler can
 * find exactly one candidate without searching a player-controlled name. */
#define ONBOARDING_RECEIPT_VERSION 1
#define ONBOARDING_RECEIPT_NAME_HEX_MAX ONBOARDING_CONTROL_NAME_HEX_MAX
#define ONBOARDING_RECEIPT_STORAGE_FORMAT_MAX ONBOARDING_CONTROL_STORAGE_FORMAT_MAX

typedef enum onboarding_receipt_state {
    ONBOARDING_RECEIPT_INVALID = 0,
    ONBOARDING_RECEIPT_PENDING,
    ONBOARDING_RECEIPT_SAVED,
    ONBOARDING_RECEIPT_COMMITTED
} onboarding_receipt_state;

typedef struct onboarding_receipt {
    onboarding_receipt_state state;
    char actor_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char correlation_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char character_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char canonical_name_hex[ONBOARDING_RECEIPT_NAME_HEX_MAX + 1];
    char storage_format[ONBOARDING_RECEIPT_STORAGE_FORMAT_MAX + 1];
    char file_sha256[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
} onboarding_receipt;

/* This is the only filename construction API.  It accepts canonical UUIDs
 * only and never incorporates a name, ticket, password, or other input. */
int onboarding_receipt_path(const char *correlation_id, char *out,
                            unsigned long out_size);

/* `pending` creates rather than overwrites.  Later transitions require an
 * exact matching prior record, then use a fsynced temporary-file replacement.
 * No call in this API accepts an admission ticket, HMAC, game password, or
 * wizard input. */
int onboarding_receipt_write_pending(const char *actor_id,
                                     const char *correlation_id,
                                     const char *character_id,
                                     const char *canonical_name,
                                     const char *storage_format);
int onboarding_receipt_mark_saved(const char *actor_id,
                                  const char *correlation_id,
                                  const char *character_id,
                                  const char *canonical_name,
                                  const char *storage_format,
                                  const char *file_sha256);
int onboarding_receipt_mark_committed(const char *actor_id,
                                      const char *correlation_id,
                                      const char *character_id,
                                      const char *canonical_name,
                                      const char *storage_format,
                                      const char *file_sha256);

/* Read-only helper for an out-of-band reconciler or hermetic tests. */
int onboarding_receipt_read(const char *correlation_id,
                            onboarding_receipt *receipt);

#endif
