#ifndef MUHAN_CDTO_V1_H
#define MUHAN_CDTO_V1_H

#include <stddef.h>
#include <stdint.h>

#define CDTO_V1_MAGIC "MUHCDTO\0"
#define CDTO_V1_MAGIC_LENGTH 8
#define CDTO_V1_WIRE_VERSION 1
#define CDTO_V1_DIGEST_LENGTH 32
#define CDTO_V1_PREFIX_LENGTH 16
#define CDTO_V1_FIELD_HEADER_LENGTH 7

#define CDTO_V1_KIND_CREATURE 1
#define CDTO_V1_KIND_OBJECT 2
#define CDTO_V1_KIND_ROOM 3
#define CDTO_V1_KIND_SESSION 4
/* Test-only execution-image metadata has its own explicit wire kind. */
#define CDTO_V1_KIND_ABI_FINGERPRINT 5
#define CDTO_V1_ABI_FINGERPRINT_KIND CDTO_V1_KIND_ABI_FINGERPRINT
/* Detached, bounded object forest.  This is deliberately distinct from the
 * flat ObjectV1 record so graph identity cannot be smuggled through pointers. */
#define CDTO_V1_KIND_OBJECT_GRAPH 6
/* Complete, pointer-free projection of a successfully loaded legacy player,
 * including its canonical detached inventory graph. */
#define CDTO_V1_KIND_PLAYER_SNAPSHOT 7
/* Non-live bank artifact: exactly one detached ObjectGraphV1 root. */
#define CDTO_V1_KIND_BANK_SNAPSHOT 8
/* Offline, detached characterization of the legacy alias/title sidecar.
 * This has no runtime save/load route. */
#define CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT 9
/* Detached metadata boundary for an AliasTitleSnapshotV1 shadow handoff.
 * It owns no directory, outbox, intake, database, or runtime capability. */
#define CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT_MANIFEST 10

#define CDTO_V1_TYPE_U8 1
#define CDTO_V1_TYPE_U16 2
#define CDTO_V1_TYPE_U32 3
#define CDTO_V1_TYPE_U64 4
#define CDTO_V1_TYPE_I8 5
#define CDTO_V1_TYPE_I16 6
#define CDTO_V1_TYPE_I32 7
#define CDTO_V1_TYPE_I64 8
#define CDTO_V1_TYPE_BYTES 9
#define CDTO_V1_TYPE_TEXT 10
#define CDTO_V1_TYPE_BOOL 11
#define CDTO_V1_OPTIONAL_TYPE_BIT 0x80

#define CDTO_V1_CREATURE_PAYLOAD_LIMIT (4U * 1024U * 1024U)
#define CDTO_V1_OBJECT_PAYLOAD_LIMIT (2U * 1024U * 1024U)
#define CDTO_V1_ROOM_PAYLOAD_LIMIT (8U * 1024U * 1024U)
#define CDTO_V1_SESSION_PAYLOAD_LIMIT (1024U * 1024U)
#define CDTO_V1_OBJECT_GRAPH_PAYLOAD_LIMIT (4U * 1024U * 1024U)
#define CDTO_V1_PLAYER_SNAPSHOT_PAYLOAD_LIMIT (4U * 1024U * 1024U)
#define CDTO_V1_BANK_SNAPSHOT_PAYLOAD_LIMIT (4U * 1024U * 1024U)
/* The bounded legacy sidecar grammar permits at most 50 entries of
 * 13-byte alias + 253-byte command plus a 78-byte title. */
#define CDTO_V1_ALIAS_TITLE_SNAPSHOT_PAYLOAD_LIMIT (64U * 1024U)
/* The embedded bounded snapshot plus closed, independently supplied metadata. */
#define CDTO_V1_ALIAS_TITLE_SNAPSHOT_MANIFEST_PAYLOAD_LIMIT (128U * 1024U)
#define CDTO_V1_MAX_ENVELOPE_SIZE \
    (CDTO_V1_ROOM_PAYLOAD_LIMIT + CDTO_V1_PREFIX_LENGTH + CDTO_V1_DIGEST_LENGTH)
#define CDTO_V1_MAX_FIELDS 65536U

enum cdto_v1_status {
    CDTO_V1_OK = 0,
    CDTO_V1_INVALID_ARGUMENT = -1,
    CDTO_V1_ALLOCATION_FAILED = -2,
    CDTO_V1_INVALID_MAGIC = -3,
    CDTO_V1_UNSUPPORTED_VERSION = -4,
    CDTO_V1_UNKNOWN_KIND = -5,
    CDTO_V1_SIZE_LIMIT_EXCEEDED = -6,
    CDTO_V1_TRUNCATED = -7,
    CDTO_V1_TRAILING_BYTES = -8,
    CDTO_V1_DIGEST_MISMATCH = -9,
    CDTO_V1_DUPLICATE_FIELD = -10,
    CDTO_V1_OUT_OF_ORDER_FIELD = -11,
    CDTO_V1_UNKNOWN_MANDATORY_TYPE = -12,
    CDTO_V1_INVALID_FIELD_LENGTH = -13,
    CDTO_V1_INVALID_UTF8 = -14,
    CDTO_V1_INVALID_BOOLEAN = -15,
    CDTO_V1_LENGTH_OVERFLOW = -16
};

typedef struct cdto_v1_field {
    uint16_t id;
    uint8_t type_tag;
    const uint8_t *value;
    uint32_t length;
} cdto_v1_field;

typedef struct cdto_v1_record {
    uint16_t kind;
    const cdto_v1_field *fields;
    size_t field_count;
} cdto_v1_record;

typedef struct cdto_v1_decoded_field {
    uint16_t id;
    uint8_t type_tag;
    uint8_t *value;
    uint32_t length;
} cdto_v1_decoded_field;

typedef struct cdto_v1_decoded_record {
    uint16_t kind;
    cdto_v1_decoded_field *fields;
    size_t field_count;
} cdto_v1_decoded_record;

int cdto_v1_encode(const cdto_v1_record *record, uint8_t **wire, size_t *wire_length);
int cdto_v1_decode(const uint8_t *wire, size_t wire_length,
                   cdto_v1_decoded_record *record);
void cdto_v1_free_wire(uint8_t *wire);
void cdto_v1_free_decoded(cdto_v1_decoded_record *record);

/* Encodes only logical build-layout facts; it never serializes a C address. */
int cdto_v1_encode_abi_fingerprint(uint8_t **wire, size_t *wire_length);

#endif
