/* A disposable actual-login harness.  It only starts and shuts down the M3
 * readiness probe; it never creates a writer or enters any save path. */
#include "character_save_journal_v2_runtime.h"
#include "character_save_journal_v2_runtime_native.h"

int main(void)
{
    character_save_journal_v2_runtime runtime;
    character_save_journal_v2_runtime_native native;
    character_save_journal_v2_runtime_native_init(&native);
    character_save_journal_v2_runtime_init(&runtime,&native.dependencies);
    if(character_save_journal_v2_runtime_start(&runtime)!=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY)
        return 1;
    character_save_journal_v2_runtime_shutdown(&runtime);
    return 0;
}
