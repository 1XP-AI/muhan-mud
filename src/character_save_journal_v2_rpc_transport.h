#ifndef CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_H
#define CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_H

#include "character_save_journal_v2_receipt_transport.h"

/* Opt-in/test-only owner of an already-authenticated writer session.  It never
 * accepts, copies, or retains a conninfo/password string. */
#define CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_TUPLES_OK 1

typedef enum character_save_journal_v2_rpc_transport_outcome {
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK=0,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED=1,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID=2,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED=3,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE=4
} character_save_journal_v2_rpc_transport_outcome;

typedef enum character_save_journal_v2_rpc_transport_state {
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NEW=0,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY=1,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE=2,
    CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED=3
} character_save_journal_v2_rpc_transport_state;

typedef struct character_save_journal_v2_rpc_transport_operations {
    int (*connection_ok)(void *connection);
    /* Nonzero only when no transaction is active or failed. */
    int (*transaction_status)(void *connection);
    void *(*exec_params)(void *connection, const char *sql, int parameter_count,
                         const unsigned int *parameter_types,
                         const char *const *parameter_values,
                         const int *parameter_lengths,
                         const int *parameter_formats, int result_format);
    int (*result_status)(void *result);
    int (*result_rows)(void *result);
    int (*result_columns)(void *result);
    const char *(*result_value)(void *result, int row, int column);
    int (*result_value_length)(void *result, int row, int column);
    const char *(*result_sqlstate)(void *result);
    void (*result_clear)(void *result);
    void (*connection_finish)(void *connection);
} character_save_journal_v2_rpc_transport_operations;

typedef struct character_save_journal_v2_rpc_route {
    char world_id[65], character_id[37], legacy_name_key[15], legacy_shard[3];
    char lifecycle[32], imported_file_sha256[65];
    unsigned int storage_format;
} character_save_journal_v2_rpc_route;

/* The additive v3 lookup deliberately has its own result shape/API: callers
 * that use v2 retain its exact identity-only contract.  An empty head_sha256
 * means the database NULL required by absent and uninitialized head states. */
typedef struct character_save_journal_v2_rpc_route_v3 {
    char world_id[65], character_id[37], legacy_name_key[15], legacy_shard[3];
    char lifecycle[32], imported_file_sha256[65];
    char head_state[14], head_sha256[65];
    unsigned int storage_format;
    unsigned long long head_revision;
} character_save_journal_v2_rpc_route_v3;

typedef struct character_save_journal_v2_rpc_transport {
    const character_save_journal_v2_rpc_transport_operations *operations;
    void *operations_opaque;
    void *connection;
    character_save_journal_v2_rpc_transport_state state;
    unsigned int initialized;
} character_save_journal_v2_rpc_transport;

/* Call exactly once before start.  close is idempotent.  A start while READY
 * preserves the already-owned pointer when it is presented again, and closes
 * a distinct newly transferred connection. */
void character_save_journal_v2_rpc_transport_init(
    character_save_journal_v2_rpc_transport *transport);
/* Ownership of connection transfers even on failed/rejected startup; it is
 * closed once whenever the supplied operations can finish it.  A deferred
 * outcome is deliberately not retried by this module. */
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_start(
    character_save_journal_v2_rpc_transport *transport,
    const character_save_journal_v2_rpc_transport_operations *operations,
    void *operations_opaque, void *connection);
void character_save_journal_v2_rpc_transport_close(
    character_save_journal_v2_rpc_transport *transport);
character_save_journal_v2_rpc_transport_state
character_save_journal_v2_rpc_transport_get_state(
    const character_save_journal_v2_rpc_transport *transport);

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_lookup_route(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *legacy_name_key, character_save_journal_v2_rpc_route *route);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_lookup_route_v3(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *legacy_name_key, character_save_journal_v2_rpc_route_v3 *route);
/* Seeds only the revision-zero absent baseline after the caller has proved
 * the canonical legacy file absent beneath a held trusted root. */
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_seed_absent_head(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *legacy_name_key, const char *character_id,
    const char *writer_instance_id, unsigned long long writer_epoch,
    unsigned int storage_format);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_acquire(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *writer_instance_id, const char *lease_expires_at,
    unsigned long long *writer_epoch, char expires_at[64]);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_renew(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *writer_instance_id, unsigned long long writer_epoch,
    const char *lease_expires_at, char expires_at[64]);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_seal(
    character_save_journal_v2_rpc_transport *transport, const char *world_id,
    const char *writer_instance_id, unsigned long long writer_epoch);
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_receipt(
    character_save_journal_v2_rpc_transport *transport,
    const character_save_journal_v2_receipt *receipt);

#endif
