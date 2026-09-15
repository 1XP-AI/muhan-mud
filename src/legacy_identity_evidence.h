#ifndef LEGACY_IDENTITY_EVIDENCE_H
#define LEGACY_IDENTITY_EVIDENCE_H

#include "player_path.h"

#define LEGACY_IDENTITY_EVIDENCE_VERSION 1U
#define LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN 64U
#define LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT "player-v1"
#define LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN 9U

/* This in-memory report is deliberately metadata-only.  It is not a wire
 * format or a C ABI promise: callers must not serialize its padding or infer
 * a Rust layout from it.  It has no password, creature image, descriptor, or
 * runtime-pointer field. */
typedef enum legacy_identity_evidence_result {
    LEGACY_IDENTITY_EVIDENCE_OK = 0,
    LEGACY_IDENTITY_EVIDENCE_NOT_FOUND = -1,
    LEGACY_IDENTITY_EVIDENCE_CORRUPT = -2,
    LEGACY_IDENTITY_EVIDENCE_IO_ERROR = -3,
    /* The requested legacy name cannot be canonicalized.  No player file is
     * opened for this result, so callers must not treat it as a new player. */
    LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT = -4
} legacy_identity_evidence_result;

typedef enum legacy_identity_evidence_canonicalization {
    LEGACY_IDENTITY_EVIDENCE_CANONICAL = 0,
    LEGACY_IDENTITY_EVIDENCE_NORMALIZED = 1,
    LEGACY_IDENTITY_EVIDENCE_INVALID = 2
} legacy_identity_evidence_canonicalization;

typedef struct legacy_identity_evidence {
    unsigned int version;
    legacy_identity_evidence_result result;
    legacy_identity_evidence_canonicalization canonicalization;
    char canonical_name[PLAYER_NAME_MAX_BYTES + 1];
    char legacy_shard[3];
    char player_file_sha256[LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN + 1];
    char storage_format[LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN + 1];
} legacy_identity_evidence;

/* Read-only inspection of the immutable legacy FileStore.  It canonicalizes
 * with the existing game canonicalizer, derives the existing player shard,
 * then accepts a digest only when that same opened file passes the legacy
 * decoder.  It never dispatches through the mutable active PlayerStore. */
legacy_identity_evidence_result legacy_identity_evidence_inspect(
    const char *legacy_name, legacy_identity_evidence *out);

#endif
