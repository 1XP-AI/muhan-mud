#ifndef CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NATIVE_H

#include "character_save_journal_v2_rpc_transport.h"

/* libpq is isolated here.  The caller creates the PGconn and transfers it to
 * start; neither this type nor the generic transport has credential storage. */
typedef struct character_save_journal_v2_rpc_transport_native {
    character_save_journal_v2_rpc_transport transport;
} character_save_journal_v2_rpc_transport_native;

void character_save_journal_v2_rpc_transport_native_init(
    character_save_journal_v2_rpc_transport_native *native);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_native_start(
    character_save_journal_v2_rpc_transport_native *native, void *pg_connection);

#endif
