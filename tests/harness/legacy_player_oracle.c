/*
 * Test-only oracle for the audited legacy player fixture grammar:
 * creature bytes, int inventory_count, then recursive object bytes, int
 * child_count records.  This does not decode arbitrary production saves.
 */
#include <stddef.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mstruct.h"

#define MAX_ROOT_ITEMS 200
#define MAX_NESTED_ITEMS 4096
#define MAX_TOTAL_ITEMS 4096
#define MAX_DEPTH 64

#define STATIC_ASSERT(name, condition) typedef char static_assert_##name[(condition) ? 1 : -1]
STATIC_ASSERT(byte_is_eight_bits, CHAR_BIT == 8);
STATIC_ASSERT(char_is_one_byte, sizeof(char) == 1);
STATIC_ASSERT(char_is_signed, CHAR_MIN < 0);
STATIC_ASSERT(short_is_two_bytes, sizeof(short) == 2);
STATIC_ASSERT(short_is_signed, SHRT_MIN < 0);
STATIC_ASSERT(int_is_four_bytes, sizeof(int) == 4);
STATIC_ASSERT(int_is_signed, INT_MIN < 0);
STATIC_ASSERT(long_is_eight_bytes, sizeof(long) == 8);
STATIC_ASSERT(long_is_signed, LONG_MIN < 0);
STATIC_ASSERT(creature_size, sizeof(creature) == 1952);
STATIC_ASSERT(object_size, sizeof(object) == 376);
STATIC_ASSERT(creature_name, offsetof(creature, name) == 0);
STATIC_ASSERT(creature_level, offsetof(creature, level) == 318);
STATIC_ASSERT(creature_level_is_unsigned_char,
              _Generic(((creature *)0)->level, unsigned char: 1, default: 0));
STATIC_ASSERT(creature_gold, offsetof(creature, gold) == 352);
STATIC_ASSERT(creature_hpmax, offsetof(creature, hpmax) == 332);
STATIC_ASSERT(creature_hpcur, offsetof(creature, hpcur) == 334);
STATIC_ASSERT(creature_mpmax, offsetof(creature, mpmax) == 336);
STATIC_ASSERT(creature_mpcur, offsetof(creature, mpcur) == 338);
STATIC_ASSERT(creature_level_width, sizeof(((creature *)0)->level) == 1);
STATIC_ASSERT(creature_gold_width, sizeof(((creature *)0)->gold) == 8);
STATIC_ASSERT(creature_hpmax_width, sizeof(((creature *)0)->hpmax) == 2);
STATIC_ASSERT(creature_hpcur_width, sizeof(((creature *)0)->hpcur) == 2);
STATIC_ASSERT(creature_mpmax_width, sizeof(((creature *)0)->mpmax) == 2);
STATIC_ASSERT(creature_mpcur_width, sizeof(((creature *)0)->mpcur) == 2);
STATIC_ASSERT(object_name, offsetof(object, name) == 0);
STATIC_ASSERT(object_description, offsetof(object, description) == 80);
STATIC_ASSERT(object_value, offsetof(object, value) == 304);
STATIC_ASSERT(object_weight, offsetof(object, weight) == 312);
STATIC_ASSERT(object_type, offsetof(object, type) == 314);
STATIC_ASSERT(object_shotsmax, offsetof(object, shotsmax) == 316);
STATIC_ASSERT(object_shotscur, offsetof(object, shotscur) == 318);
STATIC_ASSERT(object_value_width, sizeof(((object *)0)->value) == 8);
STATIC_ASSERT(object_weight_width, sizeof(((object *)0)->weight) == 2);
STATIC_ASSERT(object_type_width, sizeof(((object *)0)->type) == 1);
STATIC_ASSERT(object_shotsmax_width, sizeof(((object *)0)->shotsmax) == 2);
STATIC_ASSERT(object_shotscur_width, sizeof(((object *)0)->shotscur) == 2);

typedef struct item_node {
    object value;
    int count;
    struct item_node **children;
} item_node;

static int write_exact(FILE *file, const void *buf, size_t size)
{
    return fwrite(buf, 1, size, file) == size ? 0 : -1;
}

static int read_exact(FILE *file, void *buf, size_t size)
{
    return fread(buf, 1, size, file) == size ? 0 : -1;
}

static void set_player(creature *player, const char *name, unsigned char level)
{
    memset(player, 0, sizeof(*player));
    strncpy(player->name, name, sizeof(player->name) - 1);
    player->level = level;
    player->gold = 4242;
    player->hpmax = 40;
    player->hpcur = 31;
    player->mpmax = 22;
    player->mpcur = 14;
}

static void set_item(object *item, const char *name, const char *description,
                     long value, short weight, char type, short shotsmax,
                     short shotscur)
{
    memset(item, 0, sizeof(*item));
    strncpy(item->name, name, sizeof(item->name) - 1);
    strncpy(item->description, description, sizeof(item->description) - 1);
    item->value = value;
    item->weight = weight;
    item->type = type;
    item->shotsmax = shotsmax;
    item->shotscur = shotscur;
}

/* Match scripts/export-player-inventory.py:canonical_name_key exactly. */
static void canonical_name(const char *raw, char output[sizeof(((creature *)0)->name)])
{
    size_t index;

    for (index = 0; raw[index] != '\0' && index + 1 < sizeof(((creature *)0)->name); index++) {
        unsigned char byte = (unsigned char)raw[index];
        if (byte >= 'A' && byte <= 'Z')
            byte = (unsigned char)(byte + ('a' - 'A'));
        output[index] = (char)byte;
    }
    output[index] = '\0';
    if (output[0] >= 'a' && output[0] <= 'z')
        output[0] = (char)(output[0] - ('a' - 'A'));
}

static int emit_fixture(const char *fixture, const char *path)
{
    FILE *file;
    creature player;
    object outer;
    object inner;
    int count;
    const char *name;
    unsigned char level;

    file = fopen(path, "wb");
    if (!file)
        return -1;
    name = strcmp(fixture, "nested") == 0 ? "Beatrice" :
           strcmp(fixture, "mixed-case") == 0 ? "aLiCe" : "Alice";
    level = strcmp(fixture, "high-level") == 0 ? UCHAR_MAX : 17;
    set_player(&player, name, level);

    if (strcmp(fixture, "truncated") == 0) {
        if (write_exact(file, &player, 64) < 0) {
            fclose(file);
            return -1;
        }
    } else if (strcmp(fixture, "invalid-count") == 0) {
        count = 201;
        if (write_exact(file, &player, sizeof(player)) < 0 ||
            write_exact(file, &count, sizeof(count)) < 0) {
            fclose(file);
            return -1;
        }
    } else if (strcmp(fixture, "empty") == 0 || strcmp(fixture, "mixed-case") == 0 ||
               strcmp(fixture, "high-level") == 0) {
        count = 0;
        if (write_exact(file, &player, sizeof(player)) < 0 ||
            write_exact(file, &count, sizeof(count)) < 0) {
            fclose(file);
            return -1;
        }
    } else if (strcmp(fixture, "nested") == 0) {
        set_item(&outer, "Satchel", "a canvas satchel", 75, 3, 9, 10, 8);
        set_item(&inner, "Coin", "a silver coin", 1, 0, 2, 0, 0);
        count = 1;
        if (write_exact(file, &player, sizeof(player)) < 0 ||
            write_exact(file, &count, sizeof(count)) < 0 ||
            write_exact(file, &outer, sizeof(outer)) < 0 ||
            write_exact(file, &count, sizeof(count)) < 0 ||
            write_exact(file, &inner, sizeof(inner)) < 0) {
            fclose(file);
            return -1;
        }
        count = 0;
        if (write_exact(file, &count, sizeof(count)) < 0) {
            fclose(file);
            return -1;
        }
    } else {
        fclose(file);
        return -1;
    }
    return fclose(file);
}

static void free_item(item_node *node)
{
    int index;
    if (!node)
        return;
    for (index = 0; index < node->count; index++)
        free_item(node->children[index]);
    free(node->children);
    free(node);
}

static int parse_item(FILE *file, item_node **out, int depth, int *total,
                      const char **failure)
{
    item_node *node;
    int index;

    if (depth > MAX_DEPTH) {
        *failure = "depth-limit";
        return -1;
    }
    *total += 1;
    if (*total > MAX_TOTAL_ITEMS) {
        *failure = "item-limit";
        return -1;
    }
    node = (item_node *)calloc(1, sizeof(*node));
    if (!node) {
        *failure = "allocation";
        return -1;
    }
    if (read_exact(file, &node->value, sizeof(node->value)) < 0 ||
        read_exact(file, &node->count, sizeof(node->count)) < 0) {
        free(node);
        *failure = "truncated";
        return -1;
    }
    if (!memchr(node->value.name, 0, sizeof(node->value.name)) ||
        !memchr(node->value.description, 0, sizeof(node->value.description))) {
        free(node);
        *failure = "invalid-text";
        return -1;
    }
    if (node->count < 0 || node->count > MAX_NESTED_ITEMS) {
        free(node);
        *failure = "invalid-count";
        return -1;
    }
    if (node->count > 0) {
        node->children = (item_node **)calloc((size_t)node->count, sizeof(*node->children));
        if (!node->children) {
            free(node);
            *failure = "allocation";
            return -1;
        }
    }
    for (index = 0; index < node->count; index++) {
        if (parse_item(file, &node->children[index], depth + 1, total, failure) < 0) {
            free_item(node);
            return -1;
        }
    }
    *out = node;
    return 0;
}

static unsigned int rol32(unsigned int value, int bits)
{
    return (value << bits) | (value >> (32 - bits));
}

static unsigned int load_u32_be(const unsigned char *bytes)
{
    return ((unsigned int)bytes[0] << 24) | ((unsigned int)bytes[1] << 16) |
           ((unsigned int)bytes[2] << 8) | (unsigned int)bytes[3];
}

static void store_u32_be(unsigned int value, unsigned char *bytes)
{
    bytes[0] = (unsigned char)(value >> 24);
    bytes[1] = (unsigned char)(value >> 16);
    bytes[2] = (unsigned char)(value >> 8);
    bytes[3] = (unsigned char)value;
}

/* Fixture names are below the legacy 20-byte buffer and one SHA-1 block. */
static void sha1_digest(const unsigned char *data, unsigned long length,
                        unsigned char output[20])
{
    unsigned int h0 = 0x67452301U, h1 = 0xEFCDAB89U, h2 = 0x98BADCFEU;
    unsigned int h3 = 0x10325476U, h4 = 0xC3D2E1F0U;
    unsigned char block[64];
    unsigned int words[80];
    unsigned int a, b, c, d, e, f, k, next;
    int index;

    memset(block, 0, sizeof(block));
    memcpy(block, data, length);
    block[length] = 0x80;
    store_u32_be((unsigned int)(length >> 29), &block[56]);
    store_u32_be((unsigned int)(length << 3), &block[60]);
    for (index = 0; index < 16; index++)
        words[index] = load_u32_be(&block[index * 4]);
    for (index = 16; index < 80; index++)
        words[index] = rol32(words[index - 3] ^ words[index - 8] ^ words[index - 14] ^ words[index - 16], 1);
    a = h0; b = h1; c = h2; d = h3; e = h4;
    for (index = 0; index < 80; index++) {
        if (index < 20) { f = (b & c) | ((~b) & d); k = 0x5A827999U; }
        else if (index < 40) { f = b ^ c ^ d; k = 0x6ED9EBA1U; }
        else if (index < 60) { f = (b & c) | (b & d) | (c & d); k = 0x8F1BBCDCU; }
        else { f = b ^ c ^ d; k = 0xCA62C1D6U; }
        next = rol32(a, 5) + f + e + k + words[index];
        e = d; d = c; c = rol32(b, 30); b = a; a = next;
    }
    store_u32_be(h0 + a, &output[0]);
    store_u32_be(h1 + b, &output[4]);
    store_u32_be(h2 + c, &output[8]);
    store_u32_be(h3 + d, &output[12]);
    store_u32_be(h4 + e, &output[16]);
}

static void print_items(const item_node *const *items, int count)
{
    int index;
    for (index = 0; index < count; index++) {
        if (index)
            putchar(',');
        printf("%s(value=%ld,weight=%d,type=%d,shots=%d/%d,description=%s)",
               items[index]->value.name, items[index]->value.value,
               (int)items[index]->value.weight, (int)items[index]->value.type,
               (int)items[index]->value.shotscur, (int)items[index]->value.shotsmax,
               items[index]->value.description);
        if (items[index]->count) {
            putchar('{');
            print_items((const item_node *const *)items[index]->children, items[index]->count);
            putchar('}');
        }
    }
}

enum projection_mode {
    PROJECTION_MODE_FULL,
    PROJECTION_MODE_IDENTITY,
    PROJECTION_MODE_LOCATOR
};

static int project_fixture(const char *path, enum projection_mode mode)
{
    FILE *file;
    creature player;
    int count, index, total = 0;
    item_node **items = 0;
    const char *failure = 0;
    unsigned char digest[20];
    char player_name[sizeof(player.name)];

    file = fopen(path, "rb");
    if (!file || read_exact(file, &player, sizeof(player)) < 0) {
        if (file) fclose(file);
        puts("ERR|truncated");
        return 0;
    }
    if (!memchr(player.name, 0, sizeof(player.name))) {
        fclose(file);
        puts("ERR|invalid-text");
        return 0;
    }
    canonical_name(player.name, player_name);
    if (read_exact(file, &count, sizeof(count)) < 0) {
        fclose(file);
        puts("ERR|truncated");
        return 0;
    }
    if (count < 0 || count > MAX_ROOT_ITEMS) {
        fclose(file);
        puts("ERR|invalid-count");
        return 0;
    }
    if (count) {
        items = (item_node **)calloc((size_t)count, sizeof(*items));
        if (!items) {
            fclose(file);
            puts("ERR|allocation");
            return 0;
        }
    }
    for (index = 0; index < count; index++) {
        if (parse_item(file, &items[index], 1, &total, &failure) < 0) {
            int prior;
            for (prior = 0; prior <= index; prior++) free_item(items[prior]);
            free(items);
            fclose(file);
            printf("ERR|%s\n", failure);
            return 0;
        }
    }
    if (fgetc(file) != EOF) {
        for (index = 0; index < count; index++) free_item(items[index]);
        free(items);
        fclose(file);
        puts("ERR|trailing-bytes");
        return 0;
    }
    fclose(file);
    sha1_digest((const unsigned char *)player_name, (unsigned long)strlen(player_name), digest);
    printf("OK|name=%s|sha1=", player_name);
    for (index = 0; index < 20; index++) printf("%02x", digest[index]);
    if (mode == PROJECTION_MODE_LOCATOR) {
        printf("|shard=%02x\n", digest[0]);
        for (index = 0; index < count; index++) free_item(items[index]);
        free(items);
        return 0;
    }
    if (mode == PROJECTION_MODE_IDENTITY) {
        printf("|shard=%02x|level=%d", digest[0], (int)player.level);
        putchar('\n');
        for (index = 0; index < count; index++) free_item(items[index]);
        free(items);
        return 0;
    }
    printf("|shard=%02x|source=player/%02x/%s|level=%d|gold=%ld|hp=%d/%d|mp=%d/%d|items=",
           digest[0], digest[0], player_name, (int)player.level, player.gold,
           (int)player.hpcur, (int)player.hpmax, (int)player.mpcur, (int)player.mpmax);
    print_items((const item_node *const *)items, count);
    putchar('\n');
    for (index = 0; index < count; index++) free_item(items[index]);
    free(items);
    return 0;
}

int main(int argc, char **argv)
{
    unsigned int endian_probe = 1;
    if (*(unsigned char *)&endian_probe != 1) {
        fputs("legacy_player_oracle requires the audited little-endian fixture ABI\n", stderr);
        return 3;
    }
    if (argc == 4 && strcmp(argv[1], "emit") == 0)
        return emit_fixture(argv[2], argv[3]) == 0 ? 0 : 1;
    if (argc == 3 && strcmp(argv[1], "project") == 0)
        return project_fixture(argv[2], PROJECTION_MODE_FULL);
    if (argc == 3 && strcmp(argv[1], "identity") == 0)
        return project_fixture(argv[2], PROJECTION_MODE_IDENTITY);
    if (argc == 3 && strcmp(argv[1], "locator") == 0)
        return project_fixture(argv[2], PROJECTION_MODE_LOCATOR);
    fprintf(stderr, "usage: %s emit <empty|mixed-case|high-level|nested|truncated|invalid-count> <path> | project <path> | identity <path> | locator <path>\n", argv[0]);
    return 2;
}
