#ifndef MUHAN_ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_H
#define MUHAN_ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_H

#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#include <stddef.h>
#include <stdint.h>

/* Closed, metadata-only boundary for a future local AliasTitleSnapshotV1
 * outbox-to-intake shadow flow.  This header deliberately provides neither a
 * pathname nor a callback/runtime/database capability. */
#define ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SCHEMA 1U
#define ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX 64U
#define ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX 28U
#define ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH 36U
#define ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SNAPSHOT_WIRE_MAX \
    (CDTO_V1_ALIAS_TITLE_SNAPSHOT_PAYLOAD_LIMIT + CDTO_V1_PREFIX_LENGTH + \
     CDTO_V1_DIGEST_LENGTH)

typedef struct alias_title_snapshot_manifest_v1 {
    char world_id[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX + 1U];
    char canonical_legacy_name_key[
        ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX + 1U];
    char character_id[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH + 1U];
    char writer_instance_id[
        ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH + 1U];
    uint64_t writer_epoch;
    uint64_t writer_revision;
    char command_id[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH + 1U];
    char correlation_id[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH + 1U];
    char event_id[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH + 1U];
    /* All three snapshot facts are caller-supplied and cross-checked. */
    uint64_t snapshot_octets;
    uint8_t snapshot_digest[CDTO_V1_DIGEST_LENGTH];
    size_t snapshot_wire_length;
    uint8_t snapshot_wire[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SNAPSHOT_WIRE_MAX];
} alias_title_snapshot_manifest_v1;

/* Encode/decode exactly thirteen ordered fields.  The manifest accepts only a
 * canonical AliasTitleSnapshotV1 wire whose provided digest and length match
 * it byte-for-byte; no metadata is inferred from snapshot contents. */
int alias_title_snapshot_manifest_v1_encode(
    const alias_title_snapshot_manifest_v1 *, uint8_t **, size_t *);
int alias_title_snapshot_manifest_v1_decode(const uint8_t *, size_t,
    alias_title_snapshot_manifest_v1 *);

#endif
