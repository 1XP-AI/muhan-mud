/* Test-only S1 oracle: legacy player bytes -> bounded decoder -> CDTO v1.
 *
 * It is deliberately outside src/ and creates its legacy bytes with the
 * bounded serializer.  This keeps the ABI-bound fixture construction local
 * while the resulting PlayerSnapshotV1 fixture is portable.
 */
#include <errno.h>
#include <fcntl.h>
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

/* Link-only hooks for dead legacy sections retained in files1.c. */
void merror(char *message, char kind)
{ (void)message; (void)kind; }
void del_active(creature *player)
{ (void)player; }

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

static void make_source(player, objects, tags)
creature *player;
object objects[2];
otag tags[2];
{
    size_t i;

    memset(player, 0, sizeof(*player));
    memset(objects, 0, 2U * sizeof(*objects));
    memset(tags, 0, 2U * sizeof(*tags));
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

static int legacy_fixture(bytes, length)
unsigned char **bytes;
unsigned long *length;
{
    creature player;
    object objects[2];
    otag tags[2];
    player_record_serializer_limits limits;
    unsigned char *buffer;

    *bytes = 0;
    *length = 0;
    buffer = (unsigned char *)malloc(FIXTURE_CAPACITY);
    if (!buffer)
        return -1;
    make_source(&player, objects, tags);
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

static int snapshot_fixture(wire, wire_length)
unsigned char **wire;
size_t *wire_length;
{
    unsigned char *legacy;
    unsigned long legacy_length;
    creature *decoded;
    int result;

    *wire = 0;
    *wire_length = 0U;
    if (legacy_fixture(&legacy, &legacy_length)) {
        return -1;
    }
    result = decode_legacy(legacy, legacy_length, &decoded);
    free(legacy);
    if (result)
        return -1;
    if (decoded->fd != -1 || decoded->hpcur != decoded->hpmax ||
        decoded->mpcur != decoded->mpmax || !decoded->first_obj ||
        decoded->first_obj->obj->shotscur != decoded->first_obj->obj->shotsmax) {
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

static int reject_cases(legacy, legacy_length)
const unsigned char *legacy;
unsigned long legacy_length;
{
    unsigned char *changed;
    creature *player;
    int result;

    if (legacy_length < 2UL || decode_legacy(legacy, legacy_length - 1UL, &player) == 0) {
        free_decoded_player(player);
        return -1;
    }
    changed = (unsigned char *)malloc(legacy_length + 1UL);
    if (!changed)
        return -1;
    memcpy(changed, legacy, legacy_length);
    changed[offsetof(creature, type)] = MONSTER;
    result = decode_legacy(changed, legacy_length, &player);
    if (result == 0)
        free_decoded_player(player);
    if (result == 0) {
        free(changed);
        return -1;
    }
    memcpy(changed, legacy, legacy_length);
    changed[legacy_length] = 0x7f;
    result = decode_legacy(changed, legacy_length + 1UL, &player);
    if (result == 0)
        free_decoded_player(player);
    free(changed);
    return result == 0 ? -1 : 0;
}

static int verify(path)
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

    if (snapshot_fixture(&wire, &wire_length) || parse_hex_file(path, &expected,
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
        !clone || clone->fd != -1 || !clone->first_obj ||
        clone->first_obj->obj->shotscur != 2) {
        player_snapshot_v1_free_clone(clone);
        cdto_v1_free_wire(wire);
        return -1;
    }
    player_snapshot_v1_free_clone(clone);
    cdto_v1_free_wire(wire);
    if (legacy_fixture(&legacy, &legacy_length))
        return -1;
    result = reject_cases(legacy, legacy_length);
    free(legacy);
    return result;
}

int main(argc, argv)
int argc;
char **argv;
{
    unsigned char *wire;
    size_t wire_length;

    if (argc == 2 && !strcmp(argv[1], "fixture")) {
        if (snapshot_fixture(&wire, &wire_length))
            return 2;
        print_hex(wire, wire_length);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (argc == 3 && !strcmp(argv[1], "verify")) {
        if (verify(argv[2])) {
            fprintf(stderr, "legacy_player_snapshot_v1_oracle: verification failed\n");
            return 1;
        }
        puts("legacy_player_snapshot_v1_oracle: ok");
        return 0;
    }
    fprintf(stderr, "usage: %s fixture | verify FIXTURE.hex\n", argv[0]);
    return 2;
}
