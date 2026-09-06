/* Pure caller-owned facts for a detached AliasTitleSnapshotManifestV1 build.
 * This boundary has no legacy-runtime, filesystem, socket, receipt, or outbox
 * capability.  It accepts facts exactly as supplied; it never derives them. */
#ifndef TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_H
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_H

#include "alias_title_snapshot_manifest_v1.h"

#include <stdint.h>

#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WORLD_ID 0x001U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_LEGACY_NAME_KEY 0x002U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CHARACTER_ID 0x004U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_INSTANCE_ID 0x008U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH 0x010U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION 0x020U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_COMMAND_ID 0x040U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CORRELATION_ID 0x080U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_EVENT_ID 0x100U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT 0x200U
#define TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_REQUIRED \
    (TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WORLD_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_LEGACY_NAME_KEY | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CHARACTER_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_INSTANCE_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_COMMAND_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CORRELATION_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_EVENT_ID | \
     TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT)

typedef struct trusted_alias_identity_context_v1_builder {
    uint32_t supplied;
    alias_title_snapshot_manifest_v1 value;
} trusted_alias_identity_context_v1_builder;

void trusted_alias_identity_context_v1_builder_init(
    trusted_alias_identity_context_v1_builder *);
int trusted_alias_identity_context_v1_set_world_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_canonical_legacy_name_key(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_character_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_writer_instance_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_writer_epoch(
    trusted_alias_identity_context_v1_builder *, uint64_t);
int trusted_alias_identity_context_v1_set_writer_revision(
    trusted_alias_identity_context_v1_builder *, uint64_t);
int trusted_alias_identity_context_v1_set_command_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_correlation_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_event_id(
    trusted_alias_identity_context_v1_builder *, const char *);
int trusted_alias_identity_context_v1_set_snapshot(
    trusted_alias_identity_context_v1_builder *, const uint8_t *, size_t,
    const uint8_t *, uint64_t);

/* Copies only explicitly supplied facts into output and validates them using
 * the detached manifest codec.  On failure, output is cleared. */
int trusted_alias_identity_context_v1_build(
    const trusted_alias_identity_context_v1_builder *,
    alias_title_snapshot_manifest_v1 *);

#endif
