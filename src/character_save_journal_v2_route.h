#ifndef CHARACTER_SAVE_JOURNAL_V2_ROUTE_H
#define CHARACTER_SAVE_JOURNAL_V2_ROUTE_H

#include "character_save_journal_v2_writer.h"

#include <stddef.h>

#define CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX 14
#define CHARACTER_SAVE_JOURNAL_V2_ROUTE_HASH_HEX_LEN 64
#define CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1 1

typedef enum character_save_journal_v2_route_lifecycle {
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED = 1,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING = 2,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE = 3
} character_save_journal_v2_route_lifecycle;

typedef enum character_save_journal_v2_route_lookup_result {
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE = 1
} character_save_journal_v2_route_lookup_result;

typedef enum character_save_journal_v2_route_callback_status {
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_REJECTED = 1
} character_save_journal_v2_route_callback_status;

typedef enum character_save_journal_v2_route_error {
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID = 2,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_STALE = 3,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_LOCK = 4,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_FAILURE = 5,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS = 6,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_CARDINALITY = 7,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_WORLD = 8,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_CHARACTER_ID = 9,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_NAME = 10,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_SHARD = 11,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_FORMAT = 12,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_LIFECYCLE = 13,
    CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH = 14
} character_save_journal_v2_route_error;

/* The callback is a test-only transport seam.  It receives the exact held
 * persisted world and the caller's original canonical bytes; it has no DB,
 * cache, player-file, or publication side effect in this slice. */
typedef struct character_save_journal_v2_route_reply {
    character_save_journal_v2_route_callback_status status;
    unsigned int row_count;
    char world_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX + 1];
    char character_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN + 1];
    unsigned char legacy_name[CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX + 1];
    size_t legacy_name_length;
    char legacy_shard[3];
    unsigned int storage_format;
    character_save_journal_v2_route_lifecycle lifecycle;
    int has_imported_file_sha256;
    char imported_file_sha256[CHARACTER_SAVE_JOURNAL_V2_ROUTE_HASH_HEX_LEN + 1];
} character_save_journal_v2_route_reply;

typedef character_save_journal_v2_route_lookup_result
    (*character_save_journal_v2_route_lookup)(
    void *opaque, const char *persisted_world_id,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length,
    character_save_journal_v2_route_reply *reply);

typedef struct character_save_journal_v2_bound_route {
    char world_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX + 1];
    char character_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN + 1];
    unsigned char legacy_name[CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX + 1];
    size_t legacy_name_length;
    char legacy_shard[3];
    unsigned int storage_format;
    character_save_journal_v2_route_lifecycle lifecycle;
    int has_imported_file_sha256;
    char imported_file_sha256[CHARACTER_SAVE_JOURNAL_V2_ROUTE_HASH_HEX_LEN + 1];
} character_save_journal_v2_bound_route;

/* Revalidates the held descriptor tuple before making exactly one callback.
 * On every non-OK return `out` is byte-for-byte unchanged. */
character_save_journal_v2_route_error character_save_journal_v2_route_bind(
    const character_save_journal_v2_writer_context *context,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length,
    character_save_journal_v2_route_lookup lookup, void *lookup_opaque,
    character_save_journal_v2_bound_route *out);

#endif
