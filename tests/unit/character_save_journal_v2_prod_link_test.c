#include "character_save_journal_v2.h"

/* Link-only probe.  The Make target deliberately compiles the v2 object
 * without CHARACTER_SAVE_JOURNAL_V2_TESTING, links that exact object here,
 * and inspects both the object symbols and this binary's fresh link map. */
int main(void)
{
    return 0;
}
