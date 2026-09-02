#ifndef CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_H
#define CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_H

#include "character_save_journal_v2_ack.h"

/* This test-only adapter owns only the scalar libpq call shape.  Receipt
 * payloads contain no player or credential bytes; connection configuration
 * and ownership remain inside the caller-supplied operations context. */
#define CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NAME_MAX 14
#define CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_TUPLES_OK 1

typedef struct character_save_journal_v2_receipt_transport_operations {
    void *(*connect)(void *opaque);
    int (*connection_ok)(void *connection);
    void *(*exec_params)(void *connection, const char *sql, int parameter_count,
                         const unsigned int *parameter_types,
                         const char *const *parameter_values,
                         const int *parameter_lengths,
                         const int *parameter_formats, int result_format);
    int (*result_status)(void *result);
    const char *(*result_sqlstate)(void *result);
    void (*result_clear)(void *result);
    void (*connection_finish)(void *connection);
} character_save_journal_v2_receipt_transport_operations;

typedef struct character_save_journal_v2_receipt_transport {
    const character_save_journal_v2_receipt_transport_operations *operations;
    void *operations_opaque;
} character_save_journal_v2_receipt_transport;

/* Implements character_save_journal_v2_receipt_callback with one execution
 * at most.  Invalid scalar evidence and unknown transport outcomes defer. */
character_save_journal_v2_receipt_result
character_save_journal_v2_receipt_transport_callback(
    void *opaque, const character_save_journal_v2_receipt *receipt);

#endif
