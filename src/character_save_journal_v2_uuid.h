#ifndef CHARACTER_SAVE_JOURNAL_V2_UUID_H
#define CHARACTER_SAVE_JOURNAL_V2_UUID_H

#include <stddef.h>

#define CHARACTER_SAVE_JOURNAL_V2_UUID_BINARY_LENGTH 16
#define CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH 36
#define CHARACTER_SAVE_JOURNAL_V2_UUID_MAX_ATTEMPTS 8

typedef int (*character_save_journal_v2_uuid_entropy_fill)(
    void *opaque, unsigned char *buffer, size_t capacity, size_t *filled);

typedef enum character_save_journal_v2_uuid_result {
    CHARACTER_SAVE_JOURNAL_V2_UUID_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_UUID_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_UUID_ENTROPY_FAILURE = 2,
    CHARACTER_SAVE_JOURNAL_V2_UUID_ZERO_PROGRESS = 3,
    CHARACTER_SAVE_JOURNAL_V2_UUID_OVERSIZED_PROGRESS = 4,
    CHARACTER_SAVE_JOURNAL_V2_UUID_ATTEMPTS_EXHAUSTED = 5
} character_save_journal_v2_uuid_result;

/* Fills exactly one canonical lowercase RFC 4122 version-4 UUID. */
character_save_journal_v2_uuid_result character_save_journal_v2_uuid_generate(
    character_save_journal_v2_uuid_entropy_fill fill, void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1]);

#endif
