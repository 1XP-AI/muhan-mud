#ifndef CHARACTER_SAVE_JOURNAL_V2_UUID_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_UUID_NATIVE_H

#include "character_save_journal_v2_uuid.h"

#if !defined(__linux__)
#error "character_save_journal_v2_uuid_native requires Linux getrandom"
#endif

character_save_journal_v2_uuid_result character_save_journal_v2_uuid_generate_native(
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1]);

#endif
