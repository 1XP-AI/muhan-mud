/* Test-only S1 oracle: legacy player bytes -> bounded decoder -> CDTO v1.
 *
 * It is deliberately outside src/ and creates its legacy bytes with the
 * bounded serializer.  This keeps the ABI-bound fixture construction local
 * while the resulting PlayerSnapshotV1 fixture is portable.
 */
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "cdto_v1.h"
#include "player_record_serializer.h"
#include "player_snapshot_v1.h"

extern int read_crt_player(int fd, creature *player);

#define FIXTURE_CAPACITY 16384UL

/* This test-only raw-file grammar is intentionally accepted for one audited
 * native layout only.  The exported CDTO fixtures remain portable; this gate
 * applies only before the oracle serializes or reads legacy struct bytes. */
#define LEGACY_PLAYER_SNAPSHOT_V1_RAW_ABI_CONTRACT \
    "legacy-player-snapshot-v1/raw-v1;endian=little;char=8;short=16;int=32;" \
    "long=64;ptr=64;creature=1952;object=376;creature.level=318;" \
    "creature.type=319;creature.hpmax=332;creature.hpcur=334;" \
    "creature.mpmax=336;creature.mpcur=338;creature.gold=352;" \
    "creature.first_obj=1920;object.value=304;object.shotsmax=316;" \
    "object.shotscur=318;object.first_obj=344"

typedef enum fixture_profile {
    FIXTURE_PROFILE_RICH,
    FIXTURE_PROFILE_MINIMAL,
    FIXTURE_PROFILE_PERSISTED_GRAPH
} fixture_profile;

typedef enum legacy_negative_mutation {
    LEGACY_NEGATIVE_CREATURE_PREFIX_TRUNCATED,
    LEGACY_NEGATIVE_ROOT_COUNT_NEGATIVE,
    LEGACY_NEGATIVE_TRAILING_OCTET
} legacy_negative_mutation;

typedef struct legacy_negative_case {
    const char *name;
    legacy_negative_mutation mutation;
} legacy_negative_case;

/* Every corpus member begins as the serializer's valid rich record.  The
 * decoder must fail twice with no returned player, proving rejection is both
 * deterministic and free of a publishable partial result. */
static const legacy_negative_case LEGACY_NEGATIVE_CORPUS[] = {
    { "creature-prefix-truncated", LEGACY_NEGATIVE_CREATURE_PREFIX_TRUNCATED },
    { "root-count-negative", LEGACY_NEGATIVE_ROOT_COUNT_NEGATIVE },
    { "trailing-octet-at-eof", LEGACY_NEGATIVE_TRAILING_OCTET }
};

/* Link-only hooks for dead legacy sections retained in files1.c. */
void merror(char *message, char kind)
{ (void)message; (void)kind; }
void del_active(creature *player)
{ (void)player; }

static int legacy_raw_abi_contract(output, capacity)
char *output;
size_t capacity;
{
    unsigned int endian_probe;
    const char *endian;
    int written;

    if (!output || !capacity)
        return -1;
    endian_probe = 1U;
    endian = *(const unsigned char *)&endian_probe == 1U ? "little" : "other";
    written = snprintf(output, capacity,
        "legacy-player-snapshot-v1/raw-v1;endian=%s;char=%lu;short=%lu;int=%lu;"
        "long=%lu;ptr=%lu;creature=%lu;object=%lu;creature.level=%lu;"
        "creature.type=%lu;creature.hpmax=%lu;creature.hpcur=%lu;"
        "creature.mpmax=%lu;creature.mpcur=%lu;creature.gold=%lu;"
        "creature.first_obj=%lu;object.value=%lu;object.shotsmax=%lu;"
        "object.shotscur=%lu;object.first_obj=%lu",
        endian,
        (unsigned long)CHAR_BIT,
        (unsigned long)(sizeof(short) * CHAR_BIT),
        (unsigned long)(sizeof(int) * CHAR_BIT),
        (unsigned long)(sizeof(long) * CHAR_BIT),
        (unsigned long)(sizeof(void *) * CHAR_BIT),
        (unsigned long)sizeof(creature),
        (unsigned long)sizeof(object),
        (unsigned long)offsetof(creature, level),
        (unsigned long)offsetof(creature, type),
        (unsigned long)offsetof(creature, hpmax),
        (unsigned long)offsetof(creature, hpcur),
        (unsigned long)offsetof(creature, mpmax),
        (unsigned long)offsetof(creature, mpcur),
        (unsigned long)offsetof(creature, gold),
        (unsigned long)offsetof(creature, first_obj),
        (unsigned long)offsetof(object, value),
        (unsigned long)offsetof(object, shotsmax),
        (unsigned long)offsetof(object, shotscur),
        (unsigned long)offsetof(object, first_obj));
    return written >= 0 && (size_t)written < capacity ? 0 : -1;
}

static int raw_legacy_abi_supported(void)
{
    char actual[512];

    return legacy_raw_abi_contract(actual, sizeof(actual)) == 0 &&
        !strcmp(actual, LEGACY_PLAYER_SNAPSHOT_V1_RAW_ABI_CONTRACT);
}

static int raw_legacy_abi_matches(expected)
const char *expected;
{
    char actual[512];

    return expected && raw_legacy_abi_supported() &&
        legacy_raw_abi_contract(actual, sizeof(actual)) == 0 &&
        !strcmp(actual, expected);
}

static void set_text(value, length, text)
char *value;
size_t length;
const char *text;
{
    memset(value, 0, length);
    memcpy(value, text, strlen(text));
}

static int write_all(fd, value, length)
int fd;
const unsigned char *value;
unsigned long length;
{
    ssize_t written;
    while (length) {
        written = write(fd, value, (size_t)length);
        if (written < 0 && errno == EINTR)
            continue;
        if (written <= 0)
            return -1;
        value += written;
        length -= (unsigned long)written;
    }
    return 0;
}

static void free_object_tree(value)
object *value;
{
    otag *tag;
    otag *next;
    if (!value)
        return;
    for (tag = value->first_obj; tag; tag = next) {
        next = tag->next_tag;
        free_object_tree(tag->obj);
        free(tag);
    }
    free(value);
}

static void free_decoded_player(value)
creature *value;
{
    otag *tag;
    otag *next;
    if (!value)
        return;
    for (tag = value->first_obj; tag; tag = next) {
        next = tag->next_tag;
        free_object_tree(tag->obj);
        free(tag);
    }
    free(value);
}

static void make_rich_source(player, objects, tags)
creature *player;
object objects[4];
otag tags[4];
{
    size_t i;

    memset(player, 0, sizeof(*player));
    memset(objects, 0, 4U * sizeof(*objects));
    memset(tags, 0, 4U * sizeof(*tags));
    set_text(player->name, sizeof(player->name), "s1-fixture");
    set_text(player->description, sizeof(player->description), "S1 legacy decoder fixture.");
    set_text(player->talk, sizeof(player->talk), "fixture talk");
    set_text(player->password, sizeof(player->password), "not-snapshot");
    set_text(player->key[0], sizeof(player->key[0]), "s1");
    set_text(player->key[1], sizeof(player->key[1]), "fixture");
    set_text(player->key[2], sizeof(player->key[2]), "decoder");
    player->level = 42;
    player->type = PLAYER;
    player->class = -3;
    player->race = 4;
    player->numwander = -5;
    player->alignment = -123;
    player->strength = 18;
    player->dexterity = 17;
    player->constitution = 16;
    player->intelligence = 15;
    player->piety = 14;
    player->hpmax = 20;
    player->hpcur = 23;                 /* decoder clamps this to hpmax */
    player->mpmax = 10;
    player->mpcur = 12;                 /* decoder clamps this to mpmax */
    player->armor = -4;
    player->thaco = 9;
    player->experience = 123456L;
    player->gold = -789L;
    player->ndice = 2;
    player->sdice = 3;
    player->pdice = -1;
    player->special = 7;
    player->questnum = 8;
    player->rom_num = -9;
    player->fd = 99;                    /* decoder must detach it */
    player->following = (creature *)1;  /* serialized legacy pointer junk */
    player->ready[0] = (object *)1;
    for (i = 0U; i < 5U; ++i)
        player->proficiency[i] = (long)(i * 17U) - 40L;
    for (i = 0U; i < 4U; ++i)
        player->realm[i] = (long)(i * 19U) - 30L;
    for (i = 0U; i < sizeof(player->spells); ++i)
        player->spells[i] = (char)(i + 1U);
    for (i = 0U; i < sizeof(player->flags); ++i)
        player->flags[i] = (char)(0x40U + i);
    for (i = 0U; i < sizeof(player->quests); ++i)
        player->quests[i] = (char)(0x20U + i);
    for (i = 0U; i < 10U; ++i) {
        player->carry[i] = (short)((int)i - 4);
        player->daily[i].max = (char)(i + 2U);
        player->daily[i].cur = (char)(i + 1U);
        player->daily[i].ltime = (long)(1000U + i);
    }
    for (i = 0U; i < 45U; ++i) {
        player->lasttime[i].interval = (long)(i * 11U) - 200L;
        player->lasttime[i].ltime = (long)(300U - i * 7U);
        player->lasttime[i].misc = (short)((int)i - 20);
    }

    set_text(objects[0].name, sizeof(objects[0].name), "s1-root");
    set_text(objects[0].description, sizeof(objects[0].description), "root item");
    set_text(objects[0].key[0], sizeof(objects[0].key[0]), "root");
    set_text(objects[0].use_output, sizeof(objects[0].use_output), "root output");
    objects[0].value = 111;
    objects[0].shotsmax = 2;
    objects[0].shotscur = 5;            /* decoder clamps this to shotsmax */
    set_text(objects[1].name, sizeof(objects[1].name), "s1-child");
    set_text(objects[1].description, sizeof(objects[1].description), "child item");
    set_text(objects[1].key[0], sizeof(objects[1].key[0]), "child");
    set_text(objects[1].use_output, sizeof(objects[1].use_output), "child output");
    objects[1].value = 222;
    objects[1].shotsmax = 1;
    objects[1].shotscur = 1;
    tags[0].obj = &objects[0];
    tags[0].next_tag = 0;
    tags[1].obj = &objects[1];
    tags[1].next_tag = 0;
    player->first_obj = &tags[0];
    objects[0].first_obj = &tags[1];
    objects[0].parent_crt = player;
    objects[1].parent_obj = &objects[0];
}

/* A legal player record with no persisted inventory.  Only type, bounded
 * scalar state, and a name distinguish this from a zero-filled native image. */
static void make_minimal_source(player, objects, tags)
creature *player;
object objects[4];
otag tags[4];
{
    memset(player, 0, sizeof(*player));
    memset(objects, 0, 4U * sizeof(*objects));
    memset(tags, 0, 4U * sizeof(*tags));
    set_text(player->name, sizeof(player->name), "s1-minimal");
    player->type = PLAYER;
    player->hpmax = 1;
    player->hpcur = 1;
    player->mpmax = 1;
    player->mpcur = 1;
    player->fd = 51;                    /* must not enter the CDTO fixture */
    player->following = (creature *)1;  /* must be detached by the decoder */
    player->ready[0] = (object *)1;
}

/* This profile represents persisted scalar state plus a multi-root object
 * forest.  The non-zero bytes after the first NUL are legal legacy payload,
 * but ObjectGraphV1 must erase them before publishing the portable fixture. */
static void make_persisted_graph_source(player, objects, tags)
creature *player;
object objects[4];
otag tags[4];
{
    memset(player, 0, sizeof(*player));
    memset(objects, 0, 4U * sizeof(*objects));
    memset(tags, 0, 4U * sizeof(*tags));
    set_text(player->name, sizeof(player->name), "s1-persisted");
    set_text(player->description, sizeof(player->description),
        "persisted graph fixture");
    set_text(player->talk, sizeof(player->talk), "portable graph");
    set_text(player->key[0], sizeof(player->key[0]), "persisted");
    set_text(player->key[1], sizeof(player->key[1]), "normalization");
    player->level = 17;
    player->type = PLAYER;
    player->class = 2;
    player->race = -2;
    player->alignment = 321;
    player->strength = 13;
    player->dexterity = 12;
    player->constitution = 11;
    player->intelligence = 10;
    player->piety = 9;
    player->hpmax = 50;
    player->hpcur = 88;                 /* decoder clamps this to hpmax */
    player->mpmax = 21;
    player->mpcur = 34;                 /* decoder clamps this to mpmax */
    player->experience = 987654321L;
    player->gold = 7654321L;
    player->fd = 73;                    /* runtime-only, must be detached */
    player->following = (creature *)1;
    player->ready[3] = (object *)1;

    set_text(objects[0].name, sizeof(objects[0].name), "s1-satchel");
    objects[0].name[11] = (char)0xa5;   /* tail canonicalized by ObjectGraphV1 */
    set_text(objects[0].description, sizeof(objects[0].description), "root bag");
    objects[0].description[9] = (char)0x5a;
    set_text(objects[0].key[0], sizeof(objects[0].key[0]), "bag");
    objects[0].key[0][4] = (char)0x3c;
    set_text(objects[0].use_output, sizeof(objects[0].use_output), "open bag");
    objects[0].use_output[9] = (char)0x7e;
    objects[0].value = 501;
    objects[0].weight = 7;
    objects[0].type = 3;
    objects[0].shotsmax = 2;
    objects[0].shotscur = 9;            /* decoder clamps this to shotsmax */
    objects[0].flags[0] = 0x31;

    set_text(objects[1].name, sizeof(objects[1].name), "s1-lantern");
    set_text(objects[1].description, sizeof(objects[1].description), "nested lamp");
    set_text(objects[1].key[0], sizeof(objects[1].key[0]), "lamp");
    set_text(objects[1].use_output, sizeof(objects[1].use_output), "light");
    objects[1].value = 502;
    objects[1].shotsmax = 1;
    objects[1].shotscur = 1;

    set_text(objects[2].name, sizeof(objects[2].name), "s1-map");
    set_text(objects[2].description, sizeof(objects[2].description), "nested map");
    set_text(objects[2].key[0], sizeof(objects[2].key[0]), "map");
    set_text(objects[2].use_output, sizeof(objects[2].use_output), "read map");
    objects[2].value = 503;
    objects[2].shotsmax = 3;
    objects[2].shotscur = 7;            /* decoder clamps this to shotsmax */

    set_text(objects[3].name, sizeof(objects[3].name), "s1-token");
    set_text(objects[3].description, sizeof(objects[3].description), "second root");
    set_text(objects[3].key[0], sizeof(objects[3].key[0]), "token");
    set_text(objects[3].use_output, sizeof(objects[3].use_output), "spend");
    objects[3].value = 504;
    objects[3].shotsmax = 4;
    objects[3].shotscur = 4;

    tags[0].obj = &objects[0];
    tags[0].next_tag = &tags[3];
    tags[1].obj = &objects[1];
    tags[1].next_tag = &tags[2];
    tags[2].obj = &objects[2];
    tags[2].next_tag = 0;
    tags[3].obj = &objects[3];
    tags[3].next_tag = 0;
    player->first_obj = &tags[0];
    objects[0].first_obj = &tags[1];
    objects[0].parent_crt = player;
    objects[1].parent_obj = &objects[0];
    objects[2].parent_obj = &objects[0];
    objects[3].parent_crt = player;
}

static int make_source(profile, player, objects, tags)
fixture_profile profile;
creature *player;
object objects[4];
otag tags[4];
{
    if (profile == FIXTURE_PROFILE_RICH)
        make_rich_source(player, objects, tags);
    else if (profile == FIXTURE_PROFILE_MINIMAL)
        make_minimal_source(player, objects, tags);
    else if (profile == FIXTURE_PROFILE_PERSISTED_GRAPH)
        make_persisted_graph_source(player, objects, tags);
    else
        return -1;
    return 0;
}

static int legacy_fixture(profile, bytes, length)
fixture_profile profile;
unsigned char **bytes;
unsigned long *length;
{
    creature player;
    object objects[4];
    otag tags[4];
    player_record_serializer_limits limits;
    unsigned char *buffer;

    if (!raw_legacy_abi_supported())
        return -1;
    *bytes = 0;
    *length = 0;
    buffer = (unsigned char *)malloc(FIXTURE_CAPACITY);
    if (!buffer)
        return -1;
    if (make_source(profile, &player, objects, tags)) {
        free(buffer);
        return -1;
    }
    limits.max_depth = 64UL;
    limits.max_objects = 8192UL;
    if (player_record_serialize_bounded(&player, 0, (char *)buffer,
        FIXTURE_CAPACITY, length, &limits) != PLAYER_RECORD_SERIALIZER_OK) {
        free(buffer);
        return -1;
    }
    *bytes = buffer;
    return 0;
}

static int decode_legacy(bytes, length, player_out)
const unsigned char *bytes;
unsigned long length;
creature **player_out;
{
    char path[] = "/tmp/muhan-s1-player-snapshot.XXXXXX";
    int fd;
    creature *player;
    int result;

    if (!raw_legacy_abi_supported())
        return -1;
    *player_out = 0;
    fd = mkstemp(path);
    if (fd < 0)
        return -1;
    unlink(path);
    if (write_all(fd, bytes, length) < 0 || lseek(fd, 0L, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    player = (creature *)calloc(1U, sizeof(*player));
    if (!player) {
        close(fd);
        return -1;
    }
    result = read_crt_player(fd, player);
    if (close(fd) < 0)
        result = -1;
    if (result < 0) {
        free_decoded_player(player);
        return -1;
    }
    *player_out = player;
    return 0;
}

static void print_hex(bytes, length)
const unsigned char *bytes;
size_t length;
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    for (i = 0U; i < length; ++i) {
        putchar(digits[bytes[i] >> 4]);
        putchar(digits[bytes[i] & 15U]);
    }
    putchar('\n');
}

static int decoded_profile_valid(profile, decoded)
fixture_profile profile;
const creature *decoded;
{
    const object *root;

    if (!decoded || decoded->fd != -1 || decoded->hpcur != decoded->hpmax ||
        decoded->mpcur != decoded->mpmax)
        return 0;
    if (profile == FIXTURE_PROFILE_RICH)
        return decoded->first_obj && decoded->first_obj->obj->shotscur ==
            decoded->first_obj->obj->shotsmax;
    if (profile == FIXTURE_PROFILE_MINIMAL)
        return decoded->first_obj == 0 && !strcmp(decoded->name, "s1-minimal");
    if (profile != FIXTURE_PROFILE_PERSISTED_GRAPH || !decoded->first_obj ||
        !decoded->first_obj->next_tag)
        return 0;
    root = decoded->first_obj->obj;
    return root && root->shotscur == root->shotsmax && root->name[11] == (char)0xa5 &&
        root->first_obj && root->first_obj->next_tag &&
        root->first_obj->obj->parent_obj == root &&
        root->first_obj->next_tag->obj->shotscur == 3 &&
        decoded->first_obj->next_tag->obj->parent_crt == decoded;
}

static int clone_profile_valid(profile, clone)
fixture_profile profile;
const creature *clone;
{
    const object *root;

    if (!clone || clone->fd != -1)
        return 0;
    if (profile == FIXTURE_PROFILE_RICH)
        return clone->first_obj && clone->first_obj->obj->shotscur == 2;
    if (profile == FIXTURE_PROFILE_MINIMAL)
        return clone->first_obj == 0 && !strcmp(clone->name, "s1-minimal");
    if (profile != FIXTURE_PROFILE_PERSISTED_GRAPH || !clone->first_obj ||
        !clone->first_obj->next_tag)
        return 0;
    root = clone->first_obj->obj;
    return root && root->name[11] == 0 && root->description[9] == 0 &&
        root->key[0][4] == 0 && root->use_output[9] == 0 &&
        root->shotscur == 2 && root->first_obj && root->first_obj->next_tag &&
        root->first_obj->obj->parent_obj == root &&
        root->first_obj->next_tag->obj->shotscur == 3 &&
        clone->first_obj->next_tag->obj->parent_crt == clone;
}

static int snapshot_fixture(profile, wire, wire_length)
fixture_profile profile;
unsigned char **wire;
size_t *wire_length;
{
    unsigned char *legacy;
    unsigned long legacy_length;
    creature *decoded;
    int result;

    *wire = 0;
    *wire_length = 0U;
    if (legacy_fixture(profile, &legacy, &legacy_length)) {
        return -1;
    }
    result = decode_legacy(legacy, legacy_length, &decoded);
    free(legacy);
    if (result)
        return -1;
    if (!decoded_profile_valid(profile, decoded)) {
        free_decoded_player(decoded);
        return -1;
    }
    result = player_snapshot_v1_encode_loaded(decoded, wire, wire_length);
    free_decoded_player(decoded);
    return result == CDTO_V1_OK ? 0 : -1;
}

static int parse_hex_file(path, bytes, length)
const char *path;
unsigned char **bytes;
size_t *length;
{
    FILE *file;
    int ch;
    int high;
    int digit;
    size_t capacity;

    *bytes = 0;
    *length = 0U;
    capacity = 1024U;
    *bytes = (unsigned char *)malloc(capacity);
    if (!*bytes)
        return -1;
    file = fopen(path, "r");
    if (!file)
        goto fail;
    high = -1;
    while ((ch = fgetc(file)) != EOF) {
        if (ch >= '0' && ch <= '9') digit = ch - '0';
        else if (ch >= 'a' && ch <= 'f') digit = ch - 'a' + 10;
        else if (ch >= 'A' && ch <= 'F') digit = ch - 'A' + 10;
        else if (ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t') continue;
        else goto close_fail;
        if (high < 0) high = digit;
        else {
            if (*length == capacity) {
                unsigned char *grown = (unsigned char *)realloc(*bytes, capacity * 2U);
                if (!grown) goto close_fail;
                *bytes = grown;
                capacity *= 2U;
            }
            (*bytes)[(*length)++] = (unsigned char)((high << 4) | digit);
            high = -1;
        }
    }
    if (fclose(file) || high >= 0) goto fail;
    return 0;
close_fail:
    fclose(file);
fail:
    free(*bytes);
    *bytes = 0;
    *length = 0U;
    return -1;
}

static int make_negative_legacy_case(case_info, legacy, legacy_length, changed,
    changed_length)
const legacy_negative_case *case_info;
const unsigned char *legacy;
unsigned long legacy_length;
unsigned char **changed;
unsigned long *changed_length;
{
    int invalid_count;

    *changed = 0;
    *changed_length = 0UL;
    if (case_info->mutation == LEGACY_NEGATIVE_CREATURE_PREFIX_TRUNCATED) {
        if (legacy_length <= (unsigned long)sizeof(creature))
            return -1;
        *changed_length = (unsigned long)sizeof(creature) - 1UL;
    }
    else if (case_info->mutation == LEGACY_NEGATIVE_ROOT_COUNT_NEGATIVE) {
        if (legacy_length < (unsigned long)sizeof(creature) + (unsigned long)sizeof(int))
            return -1;
        *changed_length = legacy_length;
    }
    else if (case_info->mutation == LEGACY_NEGATIVE_TRAILING_OCTET)
        *changed_length = legacy_length + 1UL;
    else
        return -1;
    *changed = (unsigned char *)malloc(*changed_length);
    if (!*changed)
        return -1;
    if (case_info->mutation == LEGACY_NEGATIVE_CREATURE_PREFIX_TRUNCATED)
        memcpy(*changed, legacy, (size_t)*changed_length);
    else {
        memcpy(*changed, legacy, (size_t)legacy_length);
        if (case_info->mutation == LEGACY_NEGATIVE_ROOT_COUNT_NEGATIVE) {
            invalid_count = -1;
            memcpy(*changed + sizeof(creature), &invalid_count, sizeof(invalid_count));
        }
        else
            (*changed)[legacy_length] = 0x7f;
    }
    return 0;
}

static int legacy_case_rejected_without_player(bytes, length)
const unsigned char *bytes;
unsigned long length;
{
    creature *player;
    int attempt;

    for (attempt = 0; attempt < 2; ++attempt) {
        player = 0;
        if (decode_legacy(bytes, length, &player) == 0) {
            free_decoded_player(player);
            return -1;
        }
        if (player)
            return -1;
    }
    return 0;
}

static int reject_cases(legacy, legacy_length)
const unsigned char *legacy;
unsigned long legacy_length;
{
    unsigned char *changed;
    unsigned long changed_length;
    size_t index;

    for (index = 0U; index < sizeof(LEGACY_NEGATIVE_CORPUS) /
        sizeof(LEGACY_NEGATIVE_CORPUS[0]); ++index) {
        if (make_negative_legacy_case(&LEGACY_NEGATIVE_CORPUS[index], legacy,
            legacy_length, &changed, &changed_length))
            return -1;
        if (legacy_case_rejected_without_player(changed, changed_length)) {
            free(changed);
            return -1;
        }
        free(changed);
    }
    return 0;
}

static int verify(profile, path)
fixture_profile profile;
const char *path;
{
    unsigned char *wire;
    unsigned char *expected;
    unsigned char *legacy;
    creature *clone;
    unsigned long legacy_length;
    size_t wire_length;
    size_t expected_length;
    int result;

    if (snapshot_fixture(profile, &wire, &wire_length) || parse_hex_file(path, &expected,
        &expected_length))
        return -1;
    result = wire_length == expected_length && !memcmp(wire, expected, wire_length);
    free(expected);
    if (!result) {
        cdto_v1_free_wire(wire);
        return -1;
    }
    clone = 0;
    if (player_snapshot_v1_decode_clone(wire, wire_length, &clone) != CDTO_V1_OK ||
        !clone_profile_valid(profile, clone)) {
        player_snapshot_v1_free_clone(clone);
        cdto_v1_free_wire(wire);
        return -1;
    }
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
    if (legacy_fixture(profile, &legacy, &legacy_length))
        return -1;
    result = profile == FIXTURE_PROFILE_RICH ? reject_cases(legacy, legacy_length) : 0;
    free(legacy);
    return result;
}

/* Classify one portable PlayerSnapshotV1 fixture through the same C decoder
 * used by the legacy-source oracle.  The input is intentionally a checked-in
 * hex fixture, never a production save: this is the offline boundary shared
 * with the Rust DTO corpus. */
static int project_portable_fixture(path)
const char *path;
{
    unsigned char *wire;
    unsigned char *canonical;
    size_t wire_length;
    size_t canonical_length;
    creature *clone;
    int result;

    wire = 0;
    canonical = 0;
    clone = 0;
    if (parse_hex_file(path, &wire, &wire_length)) {
        puts("reject");
        return 0;
    }
    result = player_snapshot_v1_decode_clone(wire, wire_length, &clone);
    free(wire);
    if (result != CDTO_V1_OK || !clone) {
        player_snapshot_v1_free_clone(clone);
        puts("reject");
        return 0;
    }
    result = player_snapshot_v1_encode_loaded(clone, &canonical, &canonical_length);
    player_snapshot_v1_free_clone(clone);
    if (result != CDTO_V1_OK || !canonical) {
        cdto_v1_free_wire(canonical);
        return -1;
    }
    fputs("accept ", stdout);
    print_hex(canonical, canonical_length);
    cdto_v1_free_wire(canonical);
    return 0;
}

int main(argc, argv)
int argc;
char **argv;
{
    unsigned char *wire;
    size_t wire_length;

    fixture_profile profile;

    if (argc == 2 && !strcmp(argv[1], "abi-fingerprint")) {
        char abi_contract[512];

        if (legacy_raw_abi_contract(abi_contract, sizeof(abi_contract)))
            return 2;
        puts(abi_contract);
        return 0;
    }
    if (argc == 3 && !strcmp(argv[1], "abi-check"))
        return raw_legacy_abi_matches(argv[2]) ? 0 : 1;

    if (argc == 2 && !strcmp(argv[1], "fixture")) {
        if (snapshot_fixture(FIXTURE_PROFILE_RICH, &wire, &wire_length))
            return 2;
        print_hex(wire, wire_length);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (argc == 3 && !strcmp(argv[1], "fixture")) {
        if (!strcmp(argv[2], "rich")) profile = FIXTURE_PROFILE_RICH;
        else if (!strcmp(argv[2], "minimal")) profile = FIXTURE_PROFILE_MINIMAL;
        else if (!strcmp(argv[2], "persisted-graph")) profile = FIXTURE_PROFILE_PERSISTED_GRAPH;
        else return 2;
        if (snapshot_fixture(profile, &wire, &wire_length))
            return 2;
        print_hex(wire, wire_length);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (argc == 3 && !strcmp(argv[1], "verify")) {
        if (verify(FIXTURE_PROFILE_RICH, argv[2])) {
            fprintf(stderr, "legacy_player_snapshot_v1_oracle: verification failed\n");
            return 1;
        }
        puts("legacy_player_snapshot_v1_oracle: ok");
        return 0;
    }
    if (argc == 4 && !strcmp(argv[1], "verify")) {
        if (!strcmp(argv[2], "rich")) profile = FIXTURE_PROFILE_RICH;
        else if (!strcmp(argv[2], "minimal")) profile = FIXTURE_PROFILE_MINIMAL;
        else if (!strcmp(argv[2], "persisted-graph")) profile = FIXTURE_PROFILE_PERSISTED_GRAPH;
        else return 2;
        if (verify(profile, argv[3])) {
            fprintf(stderr, "legacy_player_snapshot_v1_oracle: verification failed\n");
            return 1;
        }
        puts("legacy_player_snapshot_v1_oracle: ok");
        return 0;
    }
    if (argc == 3 && !strcmp(argv[1], "project-portable"))
        return project_portable_fixture(argv[2]) ? 2 : 0;
    fprintf(stderr, "usage: %s abi-fingerprint | abi-check CONTRACT | fixture [rich|minimal|persisted-graph] | verify [PROFILE] FIXTURE.hex | project-portable FIXTURE.hex\n", argv[0]);
    return 2;
}
