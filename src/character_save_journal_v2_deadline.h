#ifndef CHARACTER_SAVE_JOURNAL_V2_DEADLINE_H
#define CHARACTER_SAVE_JOURNAL_V2_DEADLINE_H

#include <stdint.h>

/* The PlayerStore callback contract reserves 64 bytes.  A canonical UTC
 * timestamp occupies 20 bytes including neither its trailing NUL nor the
 * unused portion of the caller's buffer. */
#define CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE 64
#define CHARACTER_SAVE_JOURNAL_V2_DEADLINE_TEXT_LENGTH 20

/* The database lease contract accepts a deadline no more than five minutes
 * from the authority's current wall clock. */
#define CHARACTER_SAVE_JOURNAL_V2_DEADLINE_EXTENSION_MAX_SECONDS 300

typedef int (*character_save_journal_v2_deadline_clock)(
    void *opaque, int64_t *unix_seconds);

typedef struct character_save_journal_v2_deadline_provider {
    character_save_journal_v2_deadline_clock clock;
    void *clock_opaque;
    int64_t extension_seconds;
} character_save_journal_v2_deadline_provider;

/* Produces now + extension_seconds using only the supplied clock.  The
 * output is cleared before every failure return. */
int character_save_journal_v2_deadline_generate(
    character_save_journal_v2_deadline_clock clock,
    void *clock_opaque, int64_t extension_seconds,
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE]);

/* Initializes caller-owned state; no registration or ownership transfer is
 * performed.  Invalid configuration is retained and fails closed at call
 * time, which keeps initialization allocation-free and deterministic. */
void character_save_journal_v2_deadline_provider_init(
    character_save_journal_v2_deadline_provider *provider,
    character_save_journal_v2_deadline_clock clock,
    void *clock_opaque, int64_t extension_seconds);

/* Compatible with int(void *, char output[64]) PlayerStore callbacks. */
int character_save_journal_v2_deadline_callback(
    void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE]);

/* Short aliases make the provider usable by integrations that call it a
 * lease rather than a deadline, without introducing a second implementation. */
typedef character_save_journal_v2_deadline_provider
    character_save_journal_v2_lease_deadline_provider;
typedef character_save_journal_v2_deadline_clock
    character_save_journal_v2_lease_deadline_clock;

#define character_save_journal_v2_lease_deadline_generate \
    character_save_journal_v2_deadline_generate
#define character_save_journal_v2_lease_deadline_provider_init \
    character_save_journal_v2_deadline_provider_init
#define character_save_journal_v2_lease_deadline_callback \
    character_save_journal_v2_deadline_callback

#endif
