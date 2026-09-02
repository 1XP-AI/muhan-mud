#ifndef CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_H
#define CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_H

/*
 * Test-only composition boundary for the v2 save journal.  It deliberately
 * remains outside the live MUD build: callers provide serializer, route, and
 * receipt seams, while this module only composes the local 091 evidence APIs.
 */
#include "character_save_journal_v2_recovery.h"

#include <stddef.h>

typedef enum character_save_journal_v2_protocol_result {
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER = 2,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE = 3,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER = 4,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE = 5,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH = 6,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED = 7,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_FROZEN = 8,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK = 9,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_RECOVERY = 10,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SEAL = 11,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INSTALL = 12
} character_save_journal_v2_protocol_result;

typedef enum character_save_journal_v2_protocol_cutpoint {
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NONE = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_WRITER_LOCKED = 1,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH = 2,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED = 3,
    /* stage_at returned only after its file and directory fsyncs succeed. */
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED = 4,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED = 5,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED = 6,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_RECEIPT = 7,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED = 8,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DRAINED = 9,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SEALED = 10,
    CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NEXT_WRITER = 11
} character_save_journal_v2_protocol_cutpoint;

typedef struct character_save_journal_v2_protocol_report {
    character_save_journal_v2_protocol_cutpoint reached;
    character_save_journal_v2_publish_result publish_result;
    character_save_journal_v2_ack_result ack_result;
    character_save_journal_v2_recovery_result recovery_result;
} character_save_journal_v2_protocol_report;

typedef int (*character_save_journal_v2_protocol_serialize)(
    void *opaque, const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route *route,
    const char *command_uuid, const unsigned char **bytes_out,
    size_t *length_out);

typedef struct character_save_journal_v2_protocol_operations {
    character_save_journal_v2_route_lookup route_lookup;
    void *route_opaque;
    character_save_journal_v2_protocol_serialize serialize;
    void *serialize_opaque;
    character_save_journal_v2_receipt_callback receipt;
    void *receipt_opaque;
} character_save_journal_v2_protocol_operations;

typedef struct character_save_journal_v2_protocol_request {
    const char *root;
    const char *world_id;
    const unsigned char *canonical_legacy_name;
    size_t canonical_legacy_name_length;
    const char *command_uuid;
    unsigned long long writer_revision;
} character_save_journal_v2_protocol_request;

typedef int (*character_save_journal_v2_protocol_serialize_v3)(
    void *opaque, const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route,
    const char *command_uuid, const unsigned char **bytes_out,
    size_t *length_out);

typedef struct character_save_journal_v2_protocol_operations_v3 {
    character_save_journal_v2_route_lookup_v3 route_lookup;
    void *route_opaque;
    character_save_journal_v2_protocol_serialize_v3 serialize;
    void *serialize_opaque;
    character_save_journal_v2_receipt_callback receipt;
    void *receipt_opaque;
} character_save_journal_v2_protocol_operations_v3;

/* There is intentionally no root, world, writer revision, or writer tuple in
 * this request.  A caller that already holds the writer cannot manufacture
 * any of those capabilities or revision authority. */
typedef struct character_save_journal_v2_protocol_held_request_v3 {
    const unsigned char *canonical_legacy_name;
    size_t canonical_legacy_name_length;
    const char *command_uuid;
} character_save_journal_v2_protocol_held_request_v3;

/* The ordered success path is held writer validation, route/epoch, serializer,
 * writer revalidation, stage/fsync/hash, live precondition through the same
 * held-root descriptor, tuple revalidation, PREPARED, publish, then ack. */
character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save(
    const character_save_journal_v2_protocol_request *request,
    const character_save_journal_v2_protocol_operations *operations,
    character_save_journal_v2_protocol_report *report_out);

/* Head-aware composition for an already-held writer.  It neither opens nor
 * closes, rotates, seals, or installs that writer.  The v3 route supplies the
 * only head revision and this primitive derives PREPARED revision as +1. */
character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save_held_v3(
    const character_save_journal_v2_writer_context *writer,
    const character_save_journal_v2_protocol_held_request_v3 *request,
    const character_save_journal_v2_protocol_operations_v3 *operations,
    character_save_journal_v2_protocol_report *report_out);

/* Runs the real recovery engine under a writer lease.  The receipt callback
 * remains the 091 exact-idempotent retry boundary, including DB_ACKED and
 * local-incomplete restart cases. */
character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_recover(
    const char *root, const char *world_id,
    character_save_journal_v2_receipt_callback receipt, void *receipt_opaque,
    character_save_journal_v2_protocol_report *report_out);

typedef int (*character_save_journal_v2_protocol_attest_drained)(
    void *opaque, const character_save_journal_v2_writer_tuple *drained);
typedef int (*character_save_journal_v2_protocol_seal)(
    void *opaque, const character_save_journal_v2_writer_tuple *drained);
typedef int (*character_save_journal_v2_protocol_install_next_writer)(
    void *opaque, const character_save_journal_v2_writer_tuple *drained,
    character_save_journal_v2_writer_tuple *expected_next);

typedef struct character_save_journal_v2_protocol_cutover_operations {
    character_save_journal_v2_receipt_callback receipt;
    void *receipt_opaque;
    character_save_journal_v2_protocol_attest_drained attest_drained;
    character_save_journal_v2_protocol_seal seal;
    character_save_journal_v2_protocol_install_next_writer install_next_writer;
    void *lifecycle_opaque;
} character_save_journal_v2_protocol_cutover_operations;

/* Test seam for a local handoff sequence: recovery, an explicit drain
 * attestation, exact-A sealing, close, install, and B tuple verification.
 * It makes no claim about ordering beyond this process-local mock boundary. */
character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_drain_and_install(
    const char *root, const char *world_id,
    const character_save_journal_v2_protocol_cutover_operations *operations,
    character_save_journal_v2_protocol_report *report_out);

#endif
