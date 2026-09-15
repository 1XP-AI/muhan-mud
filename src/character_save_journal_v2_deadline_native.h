#ifndef CHARACTER_SAVE_JOURNAL_V2_DEADLINE_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_DEADLINE_NATIVE_H

#include "character_save_journal_v2_deadline.h"

#if !defined(__linux__)
#error "character_save_journal_v2_deadline_native requires Linux CLOCK_REALTIME"
#endif

typedef struct character_save_journal_v2_deadline_native {
    character_save_journal_v2_deadline_provider provider;
} character_save_journal_v2_deadline_native;

/* Caller owns native and retains it for every callback invocation. */
void character_save_journal_v2_deadline_native_init(
    character_save_journal_v2_deadline_native *native,
    int64_t extension_seconds);

int character_save_journal_v2_deadline_native_callback(
    void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE]);

#endif
