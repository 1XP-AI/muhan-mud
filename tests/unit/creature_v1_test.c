#include <stdio.h>
#include <stdlib.h>
#include <stddef.h>
#include <string.h>

#include "cdto_v1.h"
#include "creature_v1.h"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "creature_v1_test: %s\n", message);
    return 1;
}

static void fixture(value)
creature *value;
{
    int i;
    memset(value, 0, sizeof(*value));
    memcpy(value->name, "synthetic-ranger", 17);
    memcpy(value->description, "Synthetic clone fixture.", 25);
    memcpy(value->key[0], "ranger", 7);
    memcpy(value->key[1], "synthetic", 10);
    memcpy(value->key[2], "test", 5);
    memset(value->password, 0xa5, sizeof(value->password));
    value->fd = 57; value->level = 255; value->type = -2; value->class = 3;
    value->race = 4; value->numwander = -1; value->alignment = -123;
    value->strength = 18; value->dexterity = 17; value->constitution = 16;
    value->intelligence = 15; value->piety = 14;
    value->hpmax = 123; value->hpcur = 99; value->mpmax = 77; value->mpcur = 66;
    value->armor = -4; value->thaco = 12; value->experience = 0x102030405L;
    value->gold = -0x1020304L; value->ndice = 2; value->sdice = 7;
    value->pdice = -1; value->special = 44; value->questnum = 9; value->rom_num = 31;
    for(i = 0; i < 5; ++i) value->proficiency[i] = (long)(i * 101 - 200);
    for(i = 0; i < 4; ++i) value->realm[i] = (long)(i * 89 - 110);
    for(i = 0; i < 16; ++i) { value->spells[i] = (char)(i - 8); value->quests[i] = (char)(0x30 + i); }
    for(i = 0; i < 8; ++i) value->flags[i] = (char)(0xa0 + i);
    for(i = 0; i < 10; ++i) { value->carry[i] = (short)(i - 5); value->daily[i].max = (char)(i + 3); value->daily[i].cur = (char)(i + 1); value->daily[i].ltime = (long)(1000 + i); }
}

static int zero_bytes(value, length)
const char *value;
size_t length;
{
    size_t i;
    for(i = 0; i < length; ++i) if(value[i]) return 0;
    return 1;
}

static void poison_daily_padding(value)
creature *value;
{
    unsigned int i;
    unsigned char *bytes;
    for(i = 0; i < 10; ++i) {
        bytes = (unsigned char *)&value->daily[i];
        memset(bytes + 2, 0xa5, offsetof(daily, ltime) - 2);
        memset(bytes + offsetof(daily, ltime) + sizeof(value->daily[i].ltime), 0xa5,
               sizeof(value->daily[i]) - offsetof(daily, ltime) - sizeof(value->daily[i].ltime));
    }
}

static int mutate_field(canonical, canonical_length, field_index, first, second, wire, wire_length)
const unsigned char *canonical;
size_t canonical_length;
unsigned int field_index;
unsigned char first, second;
unsigned char **wire;
size_t *wire_length;
{
    cdto_v1_decoded_record decoded;
    cdto_v1_field fields[CREATURE_V1_FIELD_COUNT];
    cdto_v1_record record;
    unsigned int i;
    int status;
    memset(&decoded, 0, sizeof(decoded));
    *wire = 0; *wire_length = 0;
    status = cdto_v1_decode(canonical, canonical_length, &decoded);
    if(status != CDTO_V1_OK) return status;
    for(i = 0; i < CREATURE_V1_FIELD_COUNT; ++i) {
        fields[i].id = decoded.fields[i].id; fields[i].type_tag = decoded.fields[i].type_tag;
        fields[i].value = decoded.fields[i].value; fields[i].length = decoded.fields[i].length;
    }
    decoded.fields[field_index].value[0] = first;
    if(decoded.fields[field_index].length > 1) decoded.fields[field_index].value[1] = second;
    record.kind = decoded.kind; record.fields = fields; record.field_count = CREATURE_V1_FIELD_COUNT;
    status = cdto_v1_encode(&record, wire, wire_length);
    cdto_v1_free_decoded(&decoded);
    return status;
}

int main(void)
{
    creature input, output, unsupported;
    unsigned char *wire, *password_changed, *bad;
    unsigned char *stale;
    size_t wire_length, password_changed_length, bad_length, stale_length;
    int failed = 0;

    fixture(&input); wire = 0; wire_length = 0;
    failed += expect(creature_v1_encode_flat(&input, &wire, &wire_length) == CDTO_V1_OK,
                     "safe synthetic creature must encode");
    failed += expect(wire && wire_length > 32 && wire[10] == 0 && wire[11] == CDTO_V1_KIND_CREATURE,
                     "CreatureV1 must use the CDTO creature kind");
    memset(&output, 0xa5, sizeof(output));
    failed += expect(creature_v1_decode_flat(wire, wire_length, &output) == CDTO_V1_OK &&
                     creature_v1_equal_safe(&input, &output),
                     "C CreatureV1 encode/decode must retain safe semantics");
    failed += expect(output.fd == -1 && zero_bytes(output.password, sizeof(output.password)) && zero_bytes(output.talk, sizeof(output.talk)) &&
                     !output.first_obj && !output.first_tlk && !output.parent_rom,
                     "decoder must scrub password, talk, transport, and pointers");
    password_changed = 0; password_changed_length = 0;
    fixture(&unsupported); memset(unsupported.password, 0x5a, sizeof(unsupported.password));
    failed += expect(creature_v1_encode_flat(&unsupported, &password_changed, &password_changed_length) == CDTO_V1_OK &&
                     password_changed_length == wire_length && !memcmp(password_changed, wire, wire_length),
                     "password bytes must not affect CreatureV1 output");
    cdto_v1_free_wire(password_changed);
    unsupported = input; poison_daily_padding(&unsupported);
    failed += expect(creature_v1_equal_safe(&unsupported, &output),
                     "daily native padding must not affect safe semantic equality");
    stale = wire; stale_length = wire_length;
    unsupported = input; unsupported.first_obj = (otag *)1;
    failed += expect(creature_v1_encode_flat(&unsupported, &stale, &stale_length) == CREATURE_V1_UNSUPPORTED_STATE &&
                     !stale && stale_length == 0,
                     "unsupported inventory must fail closed and clear stale output");
    unsupported = input; unsupported.ready[0] = (object *)1; stale = (unsigned char *)1; stale_length = 1;
    failed += expect(creature_v1_encode_flat(&unsupported, &stale, &stale_length) == CREATURE_V1_UNSUPPORTED_STATE &&
                     !stale && stale_length == 0,
                     "ready attachment must fail closed");
    unsupported = input; unsupported.talk[0] = 'x'; stale = (unsigned char *)1; stale_length = 1;
    failed += expect(creature_v1_encode_flat(&unsupported, &stale, &stale_length) == CREATURE_V1_UNSUPPORTED_STATE &&
                     !stale && stale_length == 0,
                     "talk state must fail closed");
    bad = 0; bad_length = 0;
    failed += expect(mutate_field(wire, wire_length, 17, 0, 124, &bad, &bad_length) == CDTO_V1_OK,
                     "test must create valid-digest hp noncanonical wire");
    failed += expect(creature_v1_decode_flat(bad, bad_length, &output) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "hp current above max must be rejected, not normalized");
    cdto_v1_free_wire(bad);
    bad = 0; bad_length = 0;
    failed += expect(mutate_field(wire, wire_length, 0, 's', 0, &bad, &bad_length) == CDTO_V1_OK,
                     "test must create valid-digest padding wire");
    failed += expect(creature_v1_decode_flat(bad, bad_length, &output) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "nonzero data after a fixed-string NUL must be rejected");
    cdto_v1_free_wire(bad);
    cdto_v1_free_wire(wire);
    return failed ? 1 : 0;
}
