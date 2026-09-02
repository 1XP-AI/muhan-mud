#include "character_save_journal_v2_uuid.h"

#include <string.h>

static const char uuid_hex[] = "0123456789abcdef";

character_save_journal_v2_uuid_result character_save_journal_v2_uuid_generate(
    character_save_journal_v2_uuid_entropy_fill fill, void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1])
{
    unsigned char bytes[CHARACTER_SAVE_JOURNAL_V2_UUID_BINARY_LENGTH];
    size_t total, received, i, position;
    int attempt, result;

    if (output) memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1);
    if (!fill || !output) return CHARACTER_SAVE_JOURNAL_V2_UUID_INVALID_ARGUMENT;

    memset(bytes, 0, sizeof(bytes));
    total = 0;
    for (attempt = 0; attempt < CHARACTER_SAVE_JOURNAL_V2_UUID_MAX_ATTEMPTS &&
         total < sizeof(bytes); attempt++) {
        received = 0;
        result = fill(opaque, bytes + total, sizeof(bytes) - total, &received);
        if (result) {
            memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1);
            return CHARACTER_SAVE_JOURNAL_V2_UUID_ENTROPY_FAILURE;
        }
        if (received == 0) {
            memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1);
            return CHARACTER_SAVE_JOURNAL_V2_UUID_ZERO_PROGRESS;
        }
        if (received > sizeof(bytes) - total) {
            memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1);
            return CHARACTER_SAVE_JOURNAL_V2_UUID_OVERSIZED_PROGRESS;
        }
        total += received;
    }
    if (total != sizeof(bytes)) {
        memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1);
        return CHARACTER_SAVE_JOURNAL_V2_UUID_ATTEMPTS_EXHAUSTED;
    }

    bytes[6] = (unsigned char)((bytes[6] & 0x0fU) | 0x40U);
    bytes[8] = (unsigned char)((bytes[8] & 0x3fU) | 0x80U);
    position = 0;
    for (i = 0; i < sizeof(bytes); i++) {
        if (i == 4 || i == 6 || i == 8 || i == 10) output[position++] = '-';
        output[position++] = uuid_hex[bytes[i] >> 4];
        output[position++] = uuid_hex[bytes[i] & 15];
    }
    output[position] = '\0';
    return CHARACTER_SAVE_JOURNAL_V2_UUID_OK;
}
