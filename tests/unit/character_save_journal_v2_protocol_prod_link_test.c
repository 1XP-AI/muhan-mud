#include "character_save_journal_v2_protocol.h"

/* A production-flags link probe.  It must stay free of test hooks and live
 * MUD wiring; linking the complete private v2 composition closure proves the
 * exported protocol boundary has no hidden test-only dependency. */
int main(void)
{
    return sizeof(character_save_journal_v2_protocol_report) ? 0 : 1;
}
