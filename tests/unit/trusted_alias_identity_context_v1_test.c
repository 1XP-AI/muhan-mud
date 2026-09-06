#include "trusted_alias_identity_context_v1.h"

#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#include <assert.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>

typedef int (*text_setter)(trusted_alias_identity_context_v1_builder *,
    const char *);

typedef struct text_setter_case {
    const char *name;
    text_setter setter;
    size_t maximum;
    size_t value_offset;
    uint32_t fact;
} text_setter_case;

static int zeroed(const void *value, size_t length)
{
    const unsigned char *bytes = value;
    size_t index;
    for(index = 0U; index < length; ++index) if(bytes[index]) return 0;
    return 1;
}

static void make_snapshot(uint8_t **wire, size_t *wire_length,
    uint8_t digest[CDTO_V1_DIGEST_LENGTH])
{
    alias_title_snapshot_v1 snapshot;
    memset(&snapshot, 0, sizeof(snapshot));
    snapshot.alias_count = 1U;
    snapshot.aliases[0].alias_length = 1U;
    memcpy(snapshot.aliases[0].alias, "n", 1U);
    snapshot.aliases[0].process_length = 5U;
    memcpy(snapshot.aliases[0].process, "north", 5U);
    assert(alias_title_snapshot_v1_encode(&snapshot, wire, wire_length) == CDTO_V1_OK);
    memcpy(digest, *wire + *wire_length - CDTO_V1_DIGEST_LENGTH,
        CDTO_V1_DIGEST_LENGTH);
}

static void populate(trusted_alias_identity_context_v1_builder *builder,
    const uint8_t *wire, size_t wire_length, const uint8_t *digest)
{
    trusted_alias_identity_context_v1_builder_init(builder);
    assert(trusted_alias_identity_context_v1_set_world_id(builder, "muhan") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_canonical_legacy_name_key(builder, "616c696365") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_character_id(builder, "11111111-1111-1111-1111-111111111111") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_writer_instance_id(builder, "22222222-2222-2222-2222-222222222222") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_writer_epoch(builder, 7U) == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_writer_revision(builder, 19U) == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_command_id(builder, "33333333-3333-3333-3333-333333333333") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_correlation_id(builder, "44444444-4444-4444-4444-444444444444") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_event_id(builder, "55555555-5555-5555-5555-555555555555") == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_snapshot(builder, wire, wire_length,
        digest, (uint64_t)wire_length) == CDTO_V1_OK);
}

static void test_text_setters_reject_and_copy(void)
{
    const text_setter_case cases[] = {
        { "world", trusted_alias_identity_context_v1_set_world_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX,
          offsetof(alias_title_snapshot_manifest_v1, world_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WORLD_ID },
        { "legacy key", trusted_alias_identity_context_v1_set_canonical_legacy_name_key,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX,
          offsetof(alias_title_snapshot_manifest_v1, canonical_legacy_name_key),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_LEGACY_NAME_KEY },
        { "character id", trusted_alias_identity_context_v1_set_character_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH,
          offsetof(alias_title_snapshot_manifest_v1, character_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CHARACTER_ID },
        { "writer instance id", trusted_alias_identity_context_v1_set_writer_instance_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH,
          offsetof(alias_title_snapshot_manifest_v1, writer_instance_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_INSTANCE_ID },
        { "command id", trusted_alias_identity_context_v1_set_command_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH,
          offsetof(alias_title_snapshot_manifest_v1, command_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_COMMAND_ID },
        { "correlation id", trusted_alias_identity_context_v1_set_correlation_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH,
          offsetof(alias_title_snapshot_manifest_v1, correlation_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CORRELATION_ID },
        { "event id", trusted_alias_identity_context_v1_set_event_id,
          ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH,
          offsetof(alias_title_snapshot_manifest_v1, event_id),
          TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_EVENT_ID }
    };
    trusted_alias_identity_context_v1_builder builder;
    char source[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX + 2U];
    size_t index;

    for(index = 0U; index < sizeof(cases) / sizeof(cases[0]); ++index) {
        const text_setter_case *test = &cases[index];
        char *stored;

        assert(test->name[0]);
        assert(test->setter(NULL, "x") == CDTO_V1_INVALID_ARGUMENT);
        memset(source, 'a', test->maximum);
        source[test->maximum] = '\0';
        trusted_alias_identity_context_v1_builder_init(&builder);
        assert(test->setter(&builder, source) == CDTO_V1_OK);
        stored = (char *)&builder.value + test->value_offset;
        assert(!memcmp(stored, source, test->maximum + 1U));
        source[0] = 'z';
        assert(stored[0] == 'a');
        assert(builder.supplied & test->fact);

        assert(test->setter(&builder, NULL) == CDTO_V1_INVALID_ARGUMENT);
        assert(!(builder.supplied & test->fact));
        assert(zeroed(stored, test->maximum + 1U));
        assert(test->setter(&builder, "") == CDTO_V1_INVALID_ARGUMENT);
        assert(!(builder.supplied & test->fact));
        assert(zeroed(stored, test->maximum + 1U));
        memset(source, 'b', test->maximum + 1U);
        source[test->maximum + 1U] = '\0';
        assert(test->setter(&builder, source) == CDTO_V1_INVALID_ARGUMENT);
        assert(!(builder.supplied & test->fact));
        assert(zeroed(stored, test->maximum + 1U));
    }
}

static void test_scalar_setters_and_init(void)
{
    trusted_alias_identity_context_v1_builder builder;

    trusted_alias_identity_context_v1_builder_init(NULL);
    assert(trusted_alias_identity_context_v1_set_writer_epoch(NULL, 1U) == CDTO_V1_INVALID_ARGUMENT);
    assert(trusted_alias_identity_context_v1_set_writer_revision(NULL, 1U) == CDTO_V1_INVALID_ARGUMENT);
    trusted_alias_identity_context_v1_builder_init(&builder);
    assert(trusted_alias_identity_context_v1_set_writer_epoch(&builder, 0U) == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_set_writer_revision(&builder, 0U) == CDTO_V1_OK);
    assert(builder.value.writer_epoch == 0U && builder.value.writer_revision == 0U);
    assert((builder.supplied & (TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH |
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION)) ==
        (TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH |
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION));
}

static void assert_snapshot_absent(
    const trusted_alias_identity_context_v1_builder *builder)
{
    assert(!(builder->supplied & TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT));
    assert(!builder->value.snapshot_wire_length);
    assert(!builder->value.snapshot_octets);
    assert(zeroed(builder->value.snapshot_wire, sizeof(builder->value.snapshot_wire)));
    assert(zeroed(builder->value.snapshot_digest, sizeof(builder->value.snapshot_digest)));
}

static void test_snapshot_setter_rejects_and_copies(void)
{
    trusted_alias_identity_context_v1_builder builder;
    uint8_t *wire, digest[CDTO_V1_DIGEST_LENGTH];
    uint8_t oversized[ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SNAPSHOT_WIRE_MAX + 1U];
    size_t wire_length;

    wire = NULL; wire_length = 0U;
    make_snapshot(&wire, &wire_length, digest);
    memset(oversized, 0, sizeof(oversized));
    assert(trusted_alias_identity_context_v1_set_snapshot(NULL, wire, wire_length,
        digest, (uint64_t)wire_length) == CDTO_V1_INVALID_ARGUMENT);
    trusted_alias_identity_context_v1_builder_init(&builder);
    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, NULL, wire_length,
        digest, (uint64_t)wire_length) == CDTO_V1_INVALID_ARGUMENT);
    assert_snapshot_absent(&builder);
    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, wire, 0U,
        digest, 0U) == CDTO_V1_INVALID_ARGUMENT);
    assert_snapshot_absent(&builder);
    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, wire, wire_length,
        NULL, (uint64_t)wire_length) == CDTO_V1_INVALID_ARGUMENT);
    assert_snapshot_absent(&builder);
    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, oversized,
        sizeof(oversized), digest, (uint64_t)sizeof(oversized)) == CDTO_V1_INVALID_ARGUMENT);
    assert_snapshot_absent(&builder);

    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, wire, wire_length,
        digest, (uint64_t)wire_length) == CDTO_V1_OK);
    assert(!memcmp(builder.value.snapshot_wire, wire, wire_length));
    assert(!memcmp(builder.value.snapshot_digest, digest, sizeof(digest)));
    wire[0] ^= 1U;
    digest[0] ^= 1U;
    assert(builder.value.snapshot_wire[0] != wire[0]);
    assert(builder.value.snapshot_digest[0] != digest[0]);
    cdto_v1_free_wire(wire);
}

static void assert_build_rejected(
    const trusted_alias_identity_context_v1_builder *builder,
    alias_title_snapshot_manifest_v1 *output)
{
    memset(output, 0xa5, sizeof(*output));
    assert(trusted_alias_identity_context_v1_build(builder, output) == CDTO_V1_INVALID_ARGUMENT);
    assert(zeroed(output, sizeof(*output)));
}

static void test_build_rejects_every_invalid_supplied_fact(void)
{
    trusted_alias_identity_context_v1_builder builder;
    alias_title_snapshot_manifest_v1 output;
    uint8_t *wire, digest[CDTO_V1_DIGEST_LENGTH];
    size_t wire_length;

    wire = NULL; wire_length = 0U;
    make_snapshot(&wire, &wire_length, digest);
    populate(&builder, wire, wire_length, digest);
    builder.value.world_id[0] = 'M';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.canonical_legacy_name_key[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.character_id[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.writer_instance_id[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    assert(trusted_alias_identity_context_v1_set_writer_epoch(&builder, 0U) == CDTO_V1_OK);
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    assert(trusted_alias_identity_context_v1_set_writer_revision(&builder, UINT64_MAX) == CDTO_V1_OK);
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.command_id[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.correlation_id[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.event_id[0] = 'A';
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.snapshot_digest[0] ^= 1U;
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    assert(trusted_alias_identity_context_v1_set_snapshot(&builder, wire, wire_length,
        digest, (uint64_t)wire_length - 1U) == CDTO_V1_OK);
    assert_build_rejected(&builder, &output);
    populate(&builder, wire, wire_length, digest);
    builder.value.snapshot_wire[wire_length - 1U] ^= 1U;
    assert_build_rejected(&builder, &output);
    cdto_v1_free_wire(wire);
}

static void test_exact_detached_construction_and_output_clearing(void)
{
    trusted_alias_identity_context_v1_builder builder, missing;
    alias_title_snapshot_manifest_v1 output;
    uint8_t *wire, digest[CDTO_V1_DIGEST_LENGTH];
    const uint32_t facts[] = {
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WORLD_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_LEGACY_NAME_KEY,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CHARACTER_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_INSTANCE_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_EPOCH,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_WRITER_REVISION,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_COMMAND_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_CORRELATION_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_EVENT_ID,
        TRUSTED_ALIAS_IDENTITY_CONTEXT_V1_SNAPSHOT
    };
    size_t wire_length, index;

    wire = NULL; wire_length = 0U;
    make_snapshot(&wire, &wire_length, digest);
    memset(&output, 0xa5, sizeof(output));
    assert(trusted_alias_identity_context_v1_build(NULL, &output) == CDTO_V1_INVALID_ARGUMENT);
    assert(zeroed(&output, sizeof(output)));
    assert(trusted_alias_identity_context_v1_build(NULL, NULL) == CDTO_V1_INVALID_ARGUMENT);
    populate(&builder, wire, wire_length, digest);
    for(index = 0U; index < sizeof(facts) / sizeof(facts[0]); ++index) {
        missing = builder;
        missing.supplied &= ~facts[index];
        memset(&output, 0xa5, sizeof(output));
        assert(trusted_alias_identity_context_v1_build(&missing, &output) == CDTO_V1_INVALID_ARGUMENT);
        assert(zeroed(&output, sizeof(output)));
    }

    populate(&builder, wire, wire_length, digest);
    assert(trusted_alias_identity_context_v1_build(&builder, &output) == CDTO_V1_OK);
    assert(!strcmp(output.world_id, "muhan"));
    assert(!strcmp(output.canonical_legacy_name_key, "616c696365"));
    assert(!strcmp(output.character_id, "11111111-1111-1111-1111-111111111111"));
    assert(!strcmp(output.writer_instance_id, "22222222-2222-2222-2222-222222222222"));
    assert(output.writer_epoch == 7U && output.writer_revision == 19U);
    assert(!strcmp(output.command_id, "33333333-3333-3333-3333-333333333333"));
    assert(!strcmp(output.correlation_id, "44444444-4444-4444-4444-444444444444"));
    assert(!strcmp(output.event_id, "55555555-5555-5555-5555-555555555555"));
    assert(output.snapshot_octets == wire_length && output.snapshot_wire_length == wire_length);
    assert(!memcmp(output.snapshot_wire, wire, wire_length));
    assert(!memcmp(output.snapshot_digest, digest, sizeof(digest)));
    cdto_v1_free_wire(wire);
}

static void test_build_preserves_canonical_kind_9_and_rejects_aliases(void)
{
    trusted_alias_identity_context_v1_builder builder;
    alias_title_snapshot_manifest_v1 output, again, decoded, before_alias;
    cdto_v1_decoded_record record, manifest_record;
    uint8_t *snapshot_wire, digest[CDTO_V1_DIGEST_LENGTH];
    uint8_t *manifest_wire, *manifest_again;
    const uint16_t expected_field_ids[] = {
        1U, 2U, 3U, 4U, 5U, 6U, 7U, 8U, 9U, 10U, 11U, 12U, 13U
    };
    const uint8_t expected_field_tags[] = {
        CDTO_V1_TYPE_U16, CDTO_V1_TYPE_TEXT, CDTO_V1_TYPE_TEXT,
        CDTO_V1_TYPE_TEXT, CDTO_V1_TYPE_TEXT, CDTO_V1_TYPE_U64,
        CDTO_V1_TYPE_U64, CDTO_V1_TYPE_TEXT, CDTO_V1_TYPE_TEXT,
        CDTO_V1_TYPE_TEXT, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_U64
    };
    size_t snapshot_length, manifest_length, manifest_again_length;
    size_t index;

    snapshot_wire = NULL; snapshot_length = 0U;
    manifest_wire = NULL; manifest_length = 0U;
    manifest_again = NULL; manifest_again_length = 0U;
    make_snapshot(&snapshot_wire, &snapshot_length, digest);
    populate(&builder, snapshot_wire, snapshot_length, digest);

    assert(trusted_alias_identity_context_v1_build(&builder, &output) == CDTO_V1_OK);
    assert(trusted_alias_identity_context_v1_build(&builder, &again) == CDTO_V1_OK);
    assert(!memcmp(&output, &again, sizeof(output)));
    assert(output.snapshot_wire_length == snapshot_length);
    assert(output.snapshot_octets == snapshot_length);
    assert(!memcmp(output.snapshot_wire, snapshot_wire, snapshot_length));
    assert(!memcmp(output.snapshot_digest, digest, sizeof(digest)));
    assert(!memcmp(output.snapshot_digest,
        output.snapshot_wire + output.snapshot_wire_length -
        CDTO_V1_DIGEST_LENGTH, CDTO_V1_DIGEST_LENGTH));
    memset(&record, 0, sizeof(record));
    assert(cdto_v1_decode(output.snapshot_wire, output.snapshot_wire_length,
        &record) == CDTO_V1_OK);
    assert(record.kind == CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT);
    cdto_v1_free_decoded(&record);
    assert(alias_title_snapshot_manifest_v1_encode(&output, &manifest_wire,
        &manifest_length) == CDTO_V1_OK);
    assert(alias_title_snapshot_manifest_v1_encode(&again, &manifest_again,
        &manifest_again_length) == CDTO_V1_OK);
    assert(manifest_length == manifest_again_length);
    assert(!memcmp(manifest_wire, manifest_again, manifest_length));
    cdto_v1_free_wire(manifest_again);

    memset(&decoded, 0, sizeof(decoded));
    assert(alias_title_snapshot_manifest_v1_decode(manifest_wire,
        manifest_length, &decoded) == CDTO_V1_OK);
    assert(!strcmp(decoded.world_id, "muhan"));
    assert(!strcmp(decoded.canonical_legacy_name_key, "616c696365"));
    assert(!strcmp(decoded.character_id, "11111111-1111-1111-1111-111111111111"));
    assert(!strcmp(decoded.writer_instance_id, "22222222-2222-2222-2222-222222222222"));
    assert(decoded.writer_epoch == 7U && decoded.writer_revision == 19U);
    assert(!strcmp(decoded.command_id, "33333333-3333-3333-3333-333333333333"));
    assert(!strcmp(decoded.correlation_id, "44444444-4444-4444-4444-444444444444"));
    assert(!strcmp(decoded.event_id, "55555555-5555-5555-5555-555555555555"));
    assert(decoded.snapshot_octets == snapshot_length);
    assert(decoded.snapshot_wire_length == snapshot_length);
    assert(!memcmp(decoded.snapshot_wire, snapshot_wire, snapshot_length));
    assert(!memcmp(decoded.snapshot_digest, digest, sizeof(digest)));

    memset(&manifest_record, 0, sizeof(manifest_record));
    assert(cdto_v1_decode(manifest_wire, manifest_length, &manifest_record) == CDTO_V1_OK);
    assert(manifest_record.kind == CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT_MANIFEST);
    assert(manifest_record.field_count == sizeof(expected_field_ids) /
        sizeof(expected_field_ids[0]));
    assert(manifest_record.fields[0].length == 2U);
    assert(manifest_record.fields[0].value[0] == 0U);
    assert(manifest_record.fields[0].value[1] == ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SCHEMA);
    for(index = 0U; index < manifest_record.field_count; ++index) {
        assert(manifest_record.fields[index].id == expected_field_ids[index]);
        assert(manifest_record.fields[index].type_tag == expected_field_tags[index]);
    }
    cdto_v1_free_decoded(&manifest_record);
    cdto_v1_free_wire(manifest_wire);

    populate(&builder, snapshot_wire, snapshot_length, digest);
    before_alias = builder.value;
    assert(trusted_alias_identity_context_v1_build(&builder,
        &builder.value) == CDTO_V1_INVALID_ARGUMENT);
    assert(!memcmp(&builder.value, &before_alias, sizeof(builder.value)));
    assert(trusted_alias_identity_context_v1_build(&builder, &output) == CDTO_V1_OK);
    assert(!memcmp(&output, &before_alias, sizeof(output)));
    cdto_v1_free_wire(snapshot_wire);
}

int main(void)
{
    test_text_setters_reject_and_copy();
    test_scalar_setters_and_init();
    test_snapshot_setter_rejects_and_copies();
    test_build_rejects_every_invalid_supplied_fact();
    test_exact_detached_construction_and_output_clearing();
    test_build_preserves_canonical_kind_9_and_rejects_aliases();
    puts("trusted_alias_identity_context_v1_test: ok");
    return 0;
}
