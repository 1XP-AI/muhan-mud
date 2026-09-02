#ifndef CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NATIVE_H

#include "character_save_journal_v2_receipt_transport.h"

/* The conninfo storage belongs to the caller.  This wrapper performs no
 * explicit environment/file reads or logging; libpq connection resolution
 * remains the caller's responsibility.  There is no production caller. */
typedef struct character_save_journal_v2_receipt_transport_native {
    const char *conninfo;
    character_save_journal_v2_receipt_transport transport;
} character_save_journal_v2_receipt_transport_native;

void character_save_journal_v2_receipt_transport_native_init(
    character_save_journal_v2_receipt_transport_native *native,
    const char *conninfo);

#endif
