#ifndef LEGACY_IDENTITY_EVIDENCE_WIRE_H
#define LEGACY_IDENTITY_EVIDENCE_WIRE_H

#include <stddef.h>

#include "legacy_identity_evidence.h"

/* A closed, deterministic metadata envelope.  This is intentionally separate
 * from the in-memory report and from the live PlayerStore interfaces. */
#define LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC "MUDLIE\0\0"
#define LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC_LENGTH 8U
#define LEGACY_IDENTITY_EVIDENCE_WIRE_SCHEMA 1U
#define LEGACY_IDENTITY_EVIDENCE_WIRE_VERSION 1U
#define LEGACY_IDENTITY_EVIDENCE_WIRE_HEADER_LENGTH 16U
#define LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_PAYLOAD \
    (1U + 1U + 1U + PLAYER_NAME_MAX_BYTES + 1U + 2U + 1U + \
     LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN + 1U + \
     LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN)
#define LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_LENGTH \
    (LEGACY_IDENTITY_EVIDENCE_WIRE_HEADER_LENGTH + \
     LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_PAYLOAD)

enum legacy_identity_evidence_wire_status {
    LEGACY_IDENTITY_EVIDENCE_WIRE_OK = 0,
    LEGACY_IDENTITY_EVIDENCE_WIRE_INVALID_ARGUMENT = -1,
    LEGACY_IDENTITY_EVIDENCE_WIRE_SIZE_LIMIT = -2,
    LEGACY_IDENTITY_EVIDENCE_WIRE_ALLOCATION_FAILED = -3,
    LEGACY_IDENTITY_EVIDENCE_WIRE_INVALID_MAGIC = -4,
    LEGACY_IDENTITY_EVIDENCE_WIRE_UNSUPPORTED_SCHEMA = -5,
    LEGACY_IDENTITY_EVIDENCE_WIRE_UNSUPPORTED_VERSION = -6,
    LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED = -7,
    LEGACY_IDENTITY_EVIDENCE_WIRE_TRAILING_BYTES = -8,
    LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL = -9
};

/* Field order is fixed: outcome, canonicalization, canonical-name length and
 * bytes, shard length and bytes, SHA-256 length and bytes, storage-format
 * length and bytes.  No field admits opaque extension data. */
int legacy_identity_evidence_wire_encode(
    const legacy_identity_evidence *evidence, unsigned char **wire,
    size_t *wire_length);
int legacy_identity_evidence_wire_decode(const unsigned char *wire,
    size_t wire_length, legacy_identity_evidence *evidence);
void legacy_identity_evidence_wire_free(unsigned char *wire);

#endif
