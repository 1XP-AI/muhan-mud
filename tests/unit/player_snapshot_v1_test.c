#include "player_snapshot_v1.h"

#include "cdto_v1.h"
#include "object_graph_v1.h"

#include <assert.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void
set_string(char *value, size_t length, const char *text)
{
    memset(value, 0, length);
    memcpy(value, text, strlen(text));
}

static void
fill_player(creature *value)
{
    size_t index;

    memset(value, 0, sizeof(*value));
    set_string(value->name, sizeof(value->name), "snapshot-player");
    set_string(value->description, sizeof(value->description), "description");
    set_string(value->talk, sizeof(value->talk), "talk");
    set_string(value->key[0], sizeof(value->key[0]), "key-zero");
    set_string(value->key[1], sizeof(value->key[1]), "key-one");
    set_string(value->key[2], sizeof(value->key[2]), "key-two");
    memset(value->password, 0x5a, sizeof(value->password));
    value->fd = 77;
    value->level = 42U;
    value->type = PLAYER;
    value->class = -3;
    value->race = 4;
    value->numwander = -5;
    value->alignment = -123;
    value->strength = -2;
    value->dexterity = 3;
    value->constitution = -4;
    value->intelligence = 5;
    value->piety = -6;
    value->hpmax = 100;
    value->hpcur = 90;
    value->mpmax = 50;
    value->mpcur = 40;
    value->armor = -7;
    value->thaco = 8;
    value->experience = -9000001L;
    value->gold = 9000002L;
    value->ndice = -9;
    value->sdice = 10;
    value->pdice = -11;
    value->special = 12;
    value->questnum = -13;
    value->rom_num = -14;
    for (index = 0U; index < 5U; ++index)
        value->proficiency[index] = (long)(index * 101U) - 200L;
    for (index = 0U; index < 4U; ++index)
        value->realm[index] = (long)(index * 101U) - 300L;
    for (index = 0U; index < sizeof(value->spells); ++index)
        value->spells[index] = (char)(index + 1U);
    for (index = 0U; index < sizeof(value->flags); ++index)
        value->flags[index] = (char)(index + 20U);
    for (index = 0U; index < sizeof(value->quests); ++index)
        value->quests[index] = (char)(index + 30U);
    for (index = 0U; index < 10U; ++index) {
        value->carry[index] = (short)((int)index - 5);
        value->daily[index].max = (char)index;
        value->daily[index].cur = (char)(index + 10U);
        value->daily[index].ltime = (long)(index * 17U) - 80L;
    }
    for (index = 0U; index < 45U; ++index) {
        value->lasttime[index].interval = (long)(index * 19U) - 90L;
        value->lasttime[index].ltime = (long)(index * 23U) - 100L;
        value->lasttime[index].misc = (short)((int)index - 22);
    }
}

static void
test_round_trip(void)
{
    creature source;
    creature equivalent;
    creature *clone;
    uint8_t *wire;
    uint8_t *equivalent_wire;
    size_t wire_length;
    size_t equivalent_length;

    fill_player(&source);
    source.name[20] = 'x'; /* Encoder must remove stale fixed-string tail bytes. */
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) == 0);
    assert(wire != NULL && wire_length > 0U);
    clone = (creature *)1;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) == 0);
    assert(clone != NULL);
    assert(clone->fd == -1);
    assert(memcmp(clone->password, "\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0", 15U) == 0);
    assert(clone->daily[0].cur > clone->daily[0].max);
    assert(clone->lasttime[44].misc == source.lasttime[44].misc);
    assert(clone->name[20] == '\0');
    assert(player_snapshot_v1_equal_persisted(&source, clone));
    equivalent = source;
    memset(equivalent.password, 0xa7, sizeof(equivalent.password));
    equivalent.fd = -300;
    equivalent.following = (creature *)1;
    equivalent.first_fol = (ctag *)1;
    equivalent.first_enm = (etag *)1;
    equivalent.first_tlk = (ttag *)1;
    equivalent.parent_rom = (room *)1;
    equivalent_wire = NULL;
    equivalent_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&equivalent, &equivalent_wire,
        &equivalent_length) == CDTO_V1_OK);
    assert(equivalent_length == wire_length);
    assert(memcmp(equivalent_wire, wire, wire_length) == 0);
    cdto_v1_free_wire(equivalent_wire);
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
}

/* Field 7 is a raw wire U8, not a gameplay-level policy.  Keep its complete
 * byte range stable before a later projection consumes it. */
static void
test_level_raw_u8_canonicalization(void)
{
    const uint8_t levels[] = { 0U, 42U, 255U };
    creature source;
    creature *clone;
    cdto_v1_decoded_record decoded;
    uint8_t *wire;
    uint8_t *reread;
    size_t wire_length;
    size_t reread_length;
    size_t index;

    for (index = 0U; index < sizeof(levels) / sizeof(levels[0]); ++index) {
        fill_player(&source);
        source.level = levels[index];
        wire = NULL;
        wire_length = 0U;
        assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
            CDTO_V1_OK);
        memset(&decoded, 0, sizeof(decoded));
        assert(cdto_v1_decode(wire, wire_length, &decoded) == CDTO_V1_OK);
        assert(decoded.fields[6].id == 7U);
        assert(decoded.fields[6].type_tag == CDTO_V1_TYPE_U8);
        assert(decoded.fields[6].length == 1U);
        assert(decoded.fields[6].value[0] == levels[index]);
        cdto_v1_free_decoded(&decoded);

        clone = NULL;
        assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
            CDTO_V1_OK);
        assert(clone->level == levels[index]);
        reread = NULL;
        reread_length = 0U;
        assert(player_snapshot_v1_encode_loaded(clone, &reread, &reread_length) ==
            CDTO_V1_OK);
        assert(reread_length == wire_length);
        assert(memcmp(reread, wire, wire_length) == 0);
        cdto_v1_free_wire(reread);
        player_snapshot_v1_free_clone(clone);
        cdto_v1_free_wire(wire);
    }
}

static void
test_native_abi_capability(void)
{
    assert(player_snapshot_v1_native_abi_supported(8U, 16U, 64U, 1, 0));
    assert(!player_snapshot_v1_native_abi_supported(7U, 16U, 64U, 1, 0));
    assert(!player_snapshot_v1_native_abi_supported(8U, 32U, 64U, 1, 0));
    assert(!player_snapshot_v1_native_abi_supported(8U, 16U, 32U, 1, 0));
    assert(!player_snapshot_v1_native_abi_supported(8U, 16U, 64U, 0, 0));
    assert(!player_snapshot_v1_native_abi_supported(8U, 16U, 64U, 1, 1));
}

static void
test_rejections(void)
{
    creature source;
    otag tag;
    uint8_t *wire;
    size_t wire_length;

    fill_player(&source);
    source.hpcur = (short)(source.hpmax + 1);
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) != 0);
    assert(wire == NULL);
    fill_player(&source);
    source.ready[0] = (object *)1;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) != 0);
    assert(wire == NULL);
    fill_player(&source);
    memset(source.name, 'x', sizeof(source.name));
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) != 0);
    assert(wire == NULL);
    fill_player(&source);
    tag.obj = NULL;
    tag.next_tag = NULL;
    source.first_obj = &tag;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        OBJECT_GRAPH_V1_INVALID_GRAPH);
    assert(wire == NULL);
}

static void
test_inventory_and_faults(void)
{
    creature source;
    creature *clone;
    object root_one;
    object root_two;
    object child;
    object grandchild;
    otag roots[2];
    otag child_tag;
    otag grandchild_tag;
    uint8_t *wire;
    size_t wire_length;
    object before;

    fill_player(&source);
    memset(&root_one, 0, sizeof(root_one));
    memset(&root_two, 0, sizeof(root_two));
    memset(&child, 0, sizeof(child));
    memset(&grandchild, 0, sizeof(grandchild));
    set_string(root_one.name, sizeof(root_one.name), "root-one");
    set_string(root_two.name, sizeof(root_two.name), "root-two");
    set_string(child.name, sizeof(child.name), "child");
    set_string(grandchild.name, sizeof(grandchild.name), "grandchild");
    root_one.name[20] = 'x';
    root_one.parent_crt = &source;
    root_two.parent_crt = &source;
    child.parent_obj = &root_one;
    grandchild.parent_obj = &child;
    roots[0].obj = &root_one;
    roots[0].next_tag = &roots[1];
    roots[1].obj = &root_two;
    roots[1].next_tag = NULL;
    child_tag.obj = &child;
    child_tag.next_tag = NULL;
    grandchild_tag.obj = &grandchild;
    grandchild_tag.next_tag = NULL;
    root_one.first_obj = &child_tag;
    child.first_obj = &grandchild_tag;
    source.first_obj = roots;
    before = root_one;
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) == 0);
    assert(memcmp(&before, &root_one, sizeof(root_one)) == 0);
    clone = NULL;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) == 0);
    assert(clone->first_obj != NULL && clone->first_obj->next_tag != NULL);
    assert(clone->first_obj->obj->parent_crt == clone);
    assert(clone->first_obj->obj->first_obj->obj->parent_obj == clone->first_obj->obj);
    assert(clone->first_obj->obj->first_obj->obj->first_obj->obj->parent_obj ==
        clone->first_obj->obj->first_obj->obj);
    player_snapshot_v1_free_clone(clone);

#ifdef PLAYER_SNAPSHOT_V1_TESTING
    clone = (creature *)1;
    player_snapshot_v1_test_fail_after(0L);
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
        CDTO_V1_ALLOCATION_FAILED);
    assert(clone == NULL);
    player_snapshot_v1_test_reset_allocator();
#endif
#ifdef OBJECT_GRAPH_V1_TESTING
    clone = (creature *)1;
    object_graph_v1_test_fail_after(0L);
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
        CDTO_V1_ALLOCATION_FAILED);
    assert(clone == NULL);
    object_graph_v1_test_reset_allocator();
    cdto_v1_free_wire(wire);
    wire = NULL;
    wire_length = 0U;
    object_graph_v1_test_fail_after(0L);
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) != 0);
    assert(wire == NULL);
    object_graph_v1_test_reset_allocator();
#else
    cdto_v1_free_wire(wire);
#endif
    memset(child.name, 'x', sizeof(child.name));
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        OBJECT_GRAPH_V1_INVALID_GRAPH);
    assert(wire == NULL);
}

static void
test_valid_envelope_schema_rejections(void)
{
    creature source;
    creature *clone;
    cdto_v1_decoded_record decoded;
    cdto_v1_field fields[40];
    cdto_v1_field extra_fields[41];
    cdto_v1_record record;
    uint8_t *wire;
    uint8_t *changed;
    uint8_t *session_wire;
    size_t wire_length;
    size_t changed_length;
    size_t index;

    fill_player(&source);
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) == 0);
    memset(&decoded, 0, sizeof(decoded));
    assert(cdto_v1_decode(wire, wire_length, &decoded) == 0);
    for (index = 0U; index < 40U; ++index) {
        fields[index].id = decoded.fields[index].id;
        fields[index].type_tag = decoded.fields[index].type_tag;
        fields[index].value = decoded.fields[index].value;
        fields[index].length = decoded.fields[index].length;
    }
    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    record.fields = fields;
    record.field_count = 40U;
    fields[6].type_tag = CDTO_V1_TYPE_I8;
    changed = NULL;
    changed_length = 0U;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    fields[6].type_tag = CDTO_V1_TYPE_U8;
    fields[6].length = 0U;
    changed = NULL;
    changed_length = 0U;
    assert(cdto_v1_encode(&record, &changed, &changed_length) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(changed == NULL);
    fields[6].length = 1U;
    record.kind = CDTO_V1_KIND_SESSION;
    changed = NULL;
    changed_length = 0U;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    clone = (creature *)1;
    ((uint8_t *)fields[7].value)[0] = 1U;
    changed = NULL;
    changed_length = 0U;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    ((uint8_t *)fields[7].value)[0] = 0U;
    {
        uint8_t saved_experience[8];
        const uint8_t maximum_i64[8] = {
            0x7fU, 0xffU, 0xffU, 0xffU, 0xffU, 0xffU, 0xffU, 0xffU
        };

        memcpy(saved_experience, fields[23].value, 8U);
        memcpy((uint8_t *)fields[23].value, maximum_i64, 8U);
        changed = NULL;
        changed_length = 0U;
        clone = (creature *)1;
        assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
#if LONG_MAX < 0x7fffffffffffffffL
        assert(player_snapshot_v1_decode_clone(changed, changed_length,
            &clone) == CDTO_V1_INVALID_FIELD_LENGTH);
        assert(clone == NULL);
#else
        assert(player_snapshot_v1_decode_clone(changed, changed_length,
            &clone) == CDTO_V1_OK);
        assert(clone->experience == LONG_MAX);
        player_snapshot_v1_free_clone(clone);
#endif
        cdto_v1_free_wire(changed);
        memcpy((uint8_t *)fields[23].value, saved_experience, 8U);
    }
    fields[0].length = 79U;
    changed = NULL;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    fields[0].length = 80U;
    fields[38].length = 809U;
    changed = NULL;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) ==
        CDTO_V1_INVALID_FIELD_LENGTH);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    fields[38].length = 810U;
    session_wire = NULL;
    record.kind = CDTO_V1_KIND_SESSION;
    record.fields = NULL;
    record.field_count = 0U;
    assert(cdto_v1_encode(&record, &session_wire, &changed_length) == 0);
    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    record.fields = fields;
    record.field_count = 40U;
    fields[39].value = session_wire;
    fields[39].length = (uint32_t)changed_length;
    changed = NULL;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) != 0);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    cdto_v1_free_wire(session_wire);
    fields[39].value = decoded.fields[39].value;
    fields[39].length = decoded.fields[39].length;
    for (index = 0U; index < 40U; ++index)
        extra_fields[index] = fields[index];
    extra_fields[40].id = 41U;
    extra_fields[40].type_tag = CDTO_V1_TYPE_BYTES;
    extra_fields[40].value = (const uint8_t *)"x";
    extra_fields[40].length = 1U;
    record.fields = extra_fields;
    record.field_count = 41U;
    changed = NULL;
    clone = (creature *)1;
    assert(cdto_v1_encode(&record, &changed, &changed_length) == 0);
    assert(player_snapshot_v1_decode_clone(changed, changed_length, &clone) != 0);
    assert(clone == NULL);
    cdto_v1_free_wire(changed);
    cdto_v1_free_decoded(&decoded);
    cdto_v1_free_wire(wire);
}

static void
test_root_boundaries(void)
{
    const size_t legacy_max_roots = 4096U;
    creature source;
    creature *clone;
    object *objects;
    otag *tags;
    uint8_t *wire;
    size_t wire_length;
    size_t index;
    size_t count;
    otag *tag;

    fill_player(&source);
    objects = (object *)calloc(legacy_max_roots + 1U, sizeof(*objects));
    tags = (otag *)calloc(legacy_max_roots + 1U, sizeof(*tags));
    assert(objects != NULL);
    assert(tags != NULL);
    for (index = 0U; index < legacy_max_roots; ++index) {
        objects[index].parent_crt = &source;
        tags[index].obj = &objects[index];
        tags[index].next_tag = index + 1U < legacy_max_roots ?
            &tags[index + 1U] : NULL;
    }
    source.first_obj = tags;
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) == 0);
    clone = NULL;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) == 0);
    assert(clone->first_obj != NULL);
    count = 0U;
    for (tag = clone->first_obj; tag != NULL; tag = tag->next_tag)
        ++count;
    assert(count == legacy_max_roots);
    assert(player_snapshot_v1_equal_persisted(&source, clone));
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
    objects[legacy_max_roots].parent_crt = &source;
    tags[legacy_max_roots].obj = &objects[legacy_max_roots];
    tags[legacy_max_roots - 1U].next_tag = &tags[legacy_max_roots];
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        CDTO_V1_SIZE_LIMIT_EXCEEDED);
    assert(wire == NULL);
    free(tags);
    free(objects);
}

static void
test_child_list_boundaries(void)
{
    const size_t legacy_max_children = 4096U;
    creature source;
    object root;
    object *children;
    otag root_tag;
    otag *child_tags;
    uint8_t *wire;
    size_t wire_length;
    size_t index;

    fill_player(&source);
    memset(&root, 0, sizeof(root));
    memset(&root_tag, 0, sizeof(root_tag));
    children = (object *)calloc(legacy_max_children + 1U, sizeof(*children));
    child_tags = (otag *)calloc(legacy_max_children + 1U,
        sizeof(*child_tags));
    assert(children != NULL);
    assert(child_tags != NULL);
    root.parent_crt = &source;
    root_tag.obj = &root;
    source.first_obj = &root_tag;
    root.first_obj = child_tags;
    for (index = 0U; index < legacy_max_children + 1U; ++index) {
        children[index].parent_obj = &root;
        child_tags[index].obj = &children[index];
        child_tags[index].next_tag = index + 1U < legacy_max_children ?
            &child_tags[index + 1U] : NULL;
    }
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) == 0);
    cdto_v1_free_wire(wire);
    child_tags[legacy_max_children - 1U].next_tag =
        &child_tags[legacy_max_children];
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        CDTO_V1_SIZE_LIMIT_EXCEEDED);
    assert(wire == NULL);
    free(child_tags);
    free(children);
}

static int
fixture_hex_value(int value)
{
    if (value >= '0' && value <= '9') return value - '0';
    if (value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static int
fixture_hex_stream_matches(FILE *fixture, const uint8_t *wire,
    size_t wire_length)
{
    size_t index;
    int high;
    int low;
    int trailing;

    for (index = 0U; index < wire_length; ++index) {
        high = fixture_hex_value(fgetc(fixture));
        low = fixture_hex_value(fgetc(fixture));
        if (high < 0 || low < 0) return 0;
        if (wire[index] != (uint8_t)((high << 4) | low)) return 0;
    }
    trailing = fgetc(fixture);
    if (trailing == EOF) return ferror(fixture) == 0;
    if (trailing == '\n') return fgetc(fixture) == EOF && ferror(fixture) == 0;
    if (trailing != '\r') return 0;
    return fgetc(fixture) == '\n' && fgetc(fixture) == EOF
        && ferror(fixture) == 0;
}

static void
assert_fixture_hex_stream(FILE *fixture, const uint8_t *wire, size_t wire_length)
{
    assert(fixture_hex_stream_matches(fixture, wire, wire_length));
}

static void
assert_canonical_fixture(const char *path, const uint8_t *wire, size_t wire_length)
{
    FILE *fixture;

    fixture = fopen(path, "rb");
    assert(fixture != NULL);
    assert_fixture_hex_stream(fixture, wire, wire_length);
    assert(fclose(fixture) == 0);
}

static int
fixture_hex_stream_matches_text(const char *text, size_t text_length,
    const uint8_t *wire, size_t wire_length)
{
    FILE *fixture;
    int matches;

    /* tmpfile() opens a binary update stream, avoiding line-ending conversion. */
    fixture = tmpfile();
    assert(fixture != NULL);
    assert(fwrite(text, 1U, text_length, fixture) == text_length);
    assert(fseek(fixture, 0L, SEEK_SET) == 0);
    matches = fixture_hex_stream_matches(fixture, wire, wire_length);
    assert(fclose(fixture) == 0);
    return matches;
}

static void
test_fixture_reader_trailer_contract(void)
{
    const uint8_t wire[] = { 0U };
    struct fixture_hex_trailer_case {
        const char *text;
        size_t text_length;
        int expected;
    } cases[] = {
        { "00", sizeof("00") - 1U, 1 },
        { "00\n", sizeof("00\n") - 1U, 1 },
        { "00\r\n", sizeof("00\r\n") - 1U, 1 },
        { "00\r", sizeof("00\r") - 1U, 0 },
        { "00\rx", sizeof("00\rx") - 1U, 0 },
        { "00\nx", sizeof("00\nx") - 1U, 0 },
        { "00x", sizeof("00x") - 1U, 0 }
    };
    size_t index;

    for (index = 0U; index < sizeof(cases) / sizeof(cases[0]); ++index) {
        assert(fixture_hex_stream_matches_text(cases[index].text,
            cases[index].text_length, wire, sizeof(wire)) ==
            cases[index].expected);
    }
}

/* This literal fixture was emitted by player_snapshot_v1_encode_loaded() for
 * a zeroed PLAYER named Pvahero.  It is also the PG contract fixture, so this
 * check prevents the database test from silently drifting away from C bytes. */
static void
test_canonical_fixture_exact_reread(void)
{
    creature source;
    creature *clone;
    uint8_t *wire;
    uint8_t *reread;
    size_t wire_length;
    size_t reread_length;

    memset(&source, 0, sizeof(source));
    source.type = PLAYER;
    source.fd = -1;
    set_string(source.name, sizeof(source.name), "Pvahero");
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        CDTO_V1_OK);
    assert_canonical_fixture("../tests/fixtures/player_snapshot_v1_canonical.hex",
        wire, wire_length);

    clone = NULL;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
        CDTO_V1_OK);
    reread = NULL;
    reread_length = 0U;
    assert(player_snapshot_v1_encode_loaded(clone, &reread, &reread_length) ==
        CDTO_V1_OK);
    assert(reread_length == wire_length);
    assert(memcmp(reread, wire, wire_length) == 0);
    cdto_v1_free_wire(reread);
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
}

static void
test_canonical_inventory_fixture_exact_reread(void)
{
    creature source;
    creature *clone;
    object item;
    otag tag;
    uint8_t *wire;
    uint8_t *reread;
    size_t wire_length;
    size_t reread_length;

    memset(&source, 0, sizeof(source));
    memset(&item, 0, sizeof(item));
    memset(&tag, 0, sizeof(tag));
    source.type = PLAYER;
    source.fd = -1;
    set_string(source.name, sizeof(source.name), "Pvahero");
    item.parent_crt = &source;
    tag.obj = &item;
    source.first_obj = &tag;
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        CDTO_V1_OK);
    assert_canonical_fixture("../tests/fixtures/player_snapshot_v1_one_inventory_item.hex",
        wire, wire_length);

    clone = NULL;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
        CDTO_V1_OK);
    assert(clone->first_obj != NULL && clone->first_obj->obj != NULL);
    reread = NULL;
    reread_length = 0U;
    assert(player_snapshot_v1_encode_loaded(clone, &reread, &reread_length) ==
        CDTO_V1_OK);
    assert(reread_length == wire_length);
    assert(memcmp(reread, wire, wire_length) == 0);
    cdto_v1_free_wire(reread);
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
}

/* Preorder: root-a, child-a-1, grandchild-a-1, child-a-2, root-b.  This
 * covers root siblings, child siblings, and a grandchild parent path. */
static void
test_canonical_tree_inventory_fixture_exact_reread(void)
{
    creature source;
    creature *clone;
    object root_a, root_b, child_a1, child_a2, grandchild;
    otag root_a_tag, root_b_tag, child_a1_tag, child_a2_tag, grandchild_tag;
    uint8_t *wire;
    uint8_t *reread;
    size_t wire_length;
    size_t reread_length;

    memset(&source, 0, sizeof(source));
    memset(&root_a, 0, sizeof(root_a));
    memset(&root_b, 0, sizeof(root_b));
    memset(&child_a1, 0, sizeof(child_a1));
    memset(&child_a2, 0, sizeof(child_a2));
    memset(&grandchild, 0, sizeof(grandchild));
    memset(&root_a_tag, 0, sizeof(root_a_tag));
    memset(&root_b_tag, 0, sizeof(root_b_tag));
    memset(&child_a1_tag, 0, sizeof(child_a1_tag));
    memset(&child_a2_tag, 0, sizeof(child_a2_tag));
    memset(&grandchild_tag, 0, sizeof(grandchild_tag));
    source.type = PLAYER;
    source.fd = -1;
    set_string(source.name, sizeof(source.name), "Pvahero");
    root_a.parent_crt = &source;
    root_b.parent_crt = &source;
    child_a1.parent_obj = &root_a;
    child_a2.parent_obj = &root_a;
    grandchild.parent_obj = &child_a1;
    set_string(root_a.name, sizeof(root_a.name), "root-a");
    set_string(root_b.name, sizeof(root_b.name), "root-b");
    set_string(child_a1.name, sizeof(child_a1.name), "child-a-1");
    set_string(child_a2.name, sizeof(child_a2.name), "child-a-2");
    set_string(grandchild.name, sizeof(grandchild.name), "grandchild-a-1");
    root_a_tag.obj = &root_a;
    root_b_tag.obj = &root_b;
    root_a_tag.next_tag = &root_b_tag;
    child_a1_tag.obj = &child_a1;
    child_a2_tag.obj = &child_a2;
    child_a1_tag.next_tag = &child_a2_tag;
    grandchild_tag.obj = &grandchild;
    root_a.first_obj = &child_a1_tag;
    child_a1.first_obj = &grandchild_tag;
    source.first_obj = &root_a_tag;
    wire = NULL;
    wire_length = 0U;
    assert(player_snapshot_v1_encode_loaded(&source, &wire, &wire_length) ==
        CDTO_V1_OK);
    assert_canonical_fixture("../tests/fixtures/player_snapshot_v1_tree_inventory.hex",
        wire, wire_length);

    clone = NULL;
    assert(player_snapshot_v1_decode_clone(wire, wire_length, &clone) ==
        CDTO_V1_OK);
    assert(clone->first_obj != NULL && clone->first_obj->next_tag != NULL
        && clone->first_obj->obj->first_obj != NULL
        && clone->first_obj->obj->first_obj->next_tag != NULL
        && clone->first_obj->obj->first_obj->obj->first_obj != NULL);
    reread = NULL;
    reread_length = 0U;
    assert(player_snapshot_v1_encode_loaded(clone, &reread, &reread_length) ==
        CDTO_V1_OK);
    assert(reread_length == wire_length);
    assert(memcmp(reread, wire, wire_length) == 0);
    cdto_v1_free_wire(reread);
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
}

int
main(void)
{
    test_native_abi_capability();
    test_round_trip();
    test_level_raw_u8_canonicalization();
    test_rejections();
    test_inventory_and_faults();
    test_valid_envelope_schema_rejections();
    test_root_boundaries();
    test_child_list_boundaries();
    test_fixture_reader_trailer_contract();
    test_canonical_fixture_exact_reread();
    test_canonical_inventory_fixture_exact_reread();
    test_canonical_tree_inventory_fixture_exact_reread();
    return 0;
}
