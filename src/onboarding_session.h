#ifndef ONBOARDING_SESSION_H
#define ONBOARDING_SESSION_H

#include "onboarding_admission.h"

/* Environment gate for the real legacy-C lane.  0 keeps MUD1 behavior,
 * 1 enables MUD1O, and -1 is a fail-closed deployment configuration. */
int onboarding_session_mode(void);

/* Runtime wrapper around the protocol parser with the mandatory bounded,
 * in-process single-use nonce cache.  This deliberately has no socket or
 * player-file dependency so the consumption invariant is unit-testable. */
int onboarding_session_validate_ticket(const char *line, const char *secret,
                                       long now,
                                       onboarding_admission_ticket *ticket);
void onboarding_session_reset_for_test(void);

/* True only for the reserved protocol prefix.  This lets the legacy welcome
 * path fail closed instead of treating a disabled MUD1O ticket as a name. */
int onboarding_session_is_protocol_line(const unsigned char *line);

/* Digest exactly the final on-disk player representation, never the heap
 * creature. */
int onboarding_session_file_sha256(const char *name,
                                   char out[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1]);

/* A challenge is usable only during the same fixed 90-second window used by
 * the database ledger.  The C side compares time(0) wall-clock seconds while
 * avoiding a signed addition overflow. */
int onboarding_session_claim_allow_live(long challenged_at, long now);

/* MUD1O claim-only credential hook.  It must be called before a loaded
 * creature or its input buffer is released; the volatile writes prevent the
 * compiler from eliding the wipe.  It accepts no logging/evidence payload. */
void onboarding_session_zeroize_claim_memory(void *password,
                                             unsigned long password_size,
                                             void *input,
                                             unsigned long input_size);

#endif
