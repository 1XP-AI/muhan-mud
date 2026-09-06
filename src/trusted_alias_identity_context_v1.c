/* Pure TrustedAliasIdentityContextV1 provider.  Deliberately detached from
 * live save/load, legacy data structures, paths, and transports. */
#include "trusted_alias_identity_context_v1.h"

#include "cdto_v1.h"

#include <string.h>

static size_t taic_bounded(const char *value, size_t maximum)
{
    size_t index;
    if(!value) return maximum + 1U;
    for(index = 0U; index <= maximum; ++index)
        if(!value[index]) return index;
    return maximum + 1U;
}

static int taic_text(trusted_alias_identity_context_v1_builder *builder,
    uint32_t fact, char *destination, size_t maximum, const char *source)
{
    size_t length;
    if(!builder) return CDTO_V1_INVALID_ARGUMENT;
    builder->supplied &= ~fact;
    memset(destination, 0, maximum + 1U);
    length = taic_bounded(source, maximum);
    if(!length || length > maximum) return CDTO_V1_INVALID_ARGUMENT;
    memcpy(destination, source, length);
    builder->supplied |= fact;
    return CDTO_V1_OK;
}

void trusted_alias_identity_context_v1_builder_init(builder)
trusted_alias_identity_context_v1_builder *builder;
{
    if(builder) memset(builder, 0, sizeof(*builder));
}

int trusted_alias_identity_context_v1_set_world_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WORLD_ID,
    builder ? builder->value.world_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX, value); }

int trusted_alias_identity_context_v1_set_canonical_legacy_name_key(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_LEGACY_NAME_KEY,
    builder ? builder->value.canonical_legacy_name_key : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX, value); }

int trusted_alias_identity_context_v1_set_character_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CHARACTER_ID,
    builder ? builder->value.character_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH, value); }

int trusted_alias_identity_context_v1_set_writer_instance_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_INSTANCE_ID,
    builder ? builder->value.writer_instance_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH, value); }

int trusted_alias_identity_context_v1_set_writer_epoch(builder, value)
trusted_alias_identity_context_v1_builder *builder;
uint64_t value;
{
    if(!builder) return CDTO_V1_INVALID_ARGUMENT;
    builder->supplied &= ~TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH;
    builder->value.writer_epoch = value;
    builder->supplied |= TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH;
    return CDTO_V1_OK;
}

int trusted_alias_identity_context_v1_set_writer_revision(builder, value)
trusted_alias_identity_context_v1_builder *builder;
uint64_t value;
{
    if(!builder) return CDTO_V1_INVALID_ARGUMENT;
    builder->supplied &= ~TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION;
    builder->value.writer_revision = value;
    builder->supplied |= TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION;
    return CDTO_V1_OK;
}

int trusted_alias_identity_context_v1_set_command_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_COMMAND_ID,
    builder ? builder->value.command_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH, value); }

int trusted_alias_identity_context_v1_set_correlation_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CORRELATION_ID,
    builder ? builder->value.correlation_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH, value); }

int trusted_alias_identity_context_v1_set_event_id(builder, value)
trusted_alias_identity_context_v1_builder *builder;
const char *value;
{ return taic_text(builder, TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_EVENT_ID,
    builder ? builder->value.event_id : NULL,
    ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH, value); }

int trusted_alias_identity_context_v1_set_snapshot(builder, wire, wire_length,
    digest, octets)
trusted_alias_identity_context_v1_builder *builder;
const uint8_t *wire;
size_t wire_length;
const uint8_t *digest;
uint64_t octets;
{
    if(!builder) return CDTO_V1_INVALID_ARGUMENT;
    builder->supplied &= ~TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT;
    memset(builder->value.snapshot_wire, 0, sizeof(builder->value.snapshot_wire));
    memset(builder->value.snapshot_digest, 0, sizeof(builder->value.snapshot_digest));
    builder->value.snapshot_wire_length = 0U;
    builder->value.snapshot_octets = 0U;
    if(!wire || !digest || !wire_length ||
       wire_length > sizeof(builder->value.snapshot_wire)) return CDTO_V1_INVALID_ARGUMENT;
    memcpy(builder->value.snapshot_wire, wire, wire_length);
    memcpy(builder->value.snapshot_digest, digest, CDTO_V1_DIGEST_LENGTH);
    builder->value.snapshot_wire_length = wire_length;
    builder->value.snapshot_octets = octets;
    builder->supplied |= TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT;
    return CDTO_V1_OK;
}

int trusted_alias_identity_context_v1_build(builder, output)
const trusted_alias_identity_context_v1_builder *builder;
alias_title_snapshot_manifest_v1 *output;
{
    uint8_t *validation_wire;
    size_t validation_wire_length;
    int status;

    if(!output) return CDTO_V1_INVALID_ARGUMENT;
    if(builder && output == &builder->value) return CDTO_V1_INVALID_ARGUMENT;
    memset(output, 0, sizeof(*output));
    if(!builder || builder->supplied != TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_REQUIRED)
        return CDTO_V1_INVALID_ARGUMENT;
    *output = builder->value;
    validation_wire = NULL;
    validation_wire_length = 0U;
    status = alias_title_snapshot_manifest_v1_encode(output, &validation_wire,
        &validation_wire_length);
    cdto_v1_free_wire(validation_wire);
    if(status != CDTO_V1_OK) memset(output, 0, sizeof(*output));
    return status;
}
