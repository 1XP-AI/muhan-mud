#ifndef CHARACTER_SAVE_JOURNAL_V2_RUNTIME_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_NATIVE_H

#include "character_save_journal_v2_runtime.h"

/* This is the only M3 runtime unit that includes or calls libpq.  The live
 * executable includes it only when USE_M3_RUNTIME=1; no save path owns it. */
typedef struct character_save_journal_v2_runtime_native {
    character_save_journal_v2_runtime_dependencies dependencies;
} character_save_journal_v2_runtime_native;

void character_save_journal_v2_runtime_native_init(
    character_save_journal_v2_runtime_native *native);

#endif
