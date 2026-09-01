#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <stddef.h>
#include <string.h>
#include <unistd.h>

#include "mstruct.h"

extern int read_crt_player(int fd, creature *player);
extern void free_crt(creature *player);

static long allocation_budget = -1;
static int fatal_calls;
static int force_eintr;
static unsigned long decoder_read_calls;
static unsigned long decoder_read_bytes;

void *player_decoder_malloc(unsigned long size)
{
    if(allocation_budget == 0)
        return 0;
    if(allocation_budget > 0)
        allocation_budget--;
    return malloc((size_t)size);
}

int decoder_test_read(int fd, void *buf, unsigned long size)
{
    int n;
    if(force_eintr) {
        force_eintr = 0;
        errno = EINTR;
        return -1;
    }
    if(size > 7)
        size = 7;
    n = (int)read(fd, buf, (size_t)size);
    if(n > 0) {
        decoder_read_calls++;
        decoder_read_bytes += (unsigned long)n;
    }
    return n;
}

void merror(char *message, char kind)
{
    (void)message;
    (void)kind;
    fatal_calls++;
}

void del_active(creature *player)
{
    (void)player;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "files1_decoder_test: %s\n", message);
    return 1;
}

static int write_full(int fd, const void *buf, size_t size)
{
    const char *cursor = (const char *)buf;
    ssize_t n;
    while(size) {
        n = write(fd, cursor, size);
        if(n <= 0)
            return -1;
        cursor += n;
        size -= (size_t)n;
    }
    return 0;
}

static int write_fixture(int fd, int object_depth, int root_count)
{
    creature player;
    object item;
    int count;
    int i;

    memset(&player, 0, sizeof(player));
    player.type = PLAYER;
    player.fd = 17;
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &root_count, sizeof(root_count)) < 0)
        return -1;
    memset(&item, 0, sizeof(item));
    item.shotsmax = 1;
    for(i = 0; i < object_depth; ++i) {
        count = (i + 1 < object_depth) ? 1 : 0;
        if(write_full(fd, &item, sizeof(item)) < 0 ||
           write_full(fd, &count, sizeof(count)) < 0)
            return -1;
    }
    return 0;
}

static int open_fixture(int object_depth, int root_count)
{
    char path[] = "/tmp/muhan-files1-decoder.XXXXXX";
    int fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    if(write_fixture(fd, object_depth, root_count) < 0 || lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static void poison_creature_field(creature *player, int field)
{
    switch(field) {
    case 0: memset(player->name, 'N', sizeof(player->name)); break;
    case 1: memset(player->description, 'D', sizeof(player->description)); break;
    case 2: memset(player->talk, 'T', sizeof(player->talk)); break;
    case 3: memset(player->password, 'P', sizeof(player->password)); break;
    case 4: memset(player->key[0], '0', sizeof(player->key[0])); break;
    case 5: memset(player->key[1], '1', sizeof(player->key[1])); break;
    case 6: memset(player->key[2], '2', sizeof(player->key[2])); break;
    }
}

static void poison_object_field(object *item, int field)
{
    switch(field) {
    case 0: memset(item->name, 'N', sizeof(item->name)); break;
    case 1: memset(item->description, 'D', sizeof(item->description)); break;
    case 2: memset(item->key[0], '0', sizeof(item->key[0])); break;
    case 3: memset(item->key[1], '1', sizeof(item->key[1])); break;
    case 4: memset(item->key[2], '2', sizeof(item->key[2])); break;
    case 5: memset(item->use_output, 'U', sizeof(item->use_output)); break;
    }
}

static int open_bad_string_fixture(int bad_object, int field)
{
    char path[] = "/tmp/muhan-files1-decoder-strings.XXXXXX";
    creature player;
    object item;
    int root_count = bad_object ? 1 : 0;
    int no_children = 0;
    int fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    memset(&player, 0, sizeof(player));
    memset(&item, 0, sizeof(item));
    player.type = PLAYER;
    player.fd = 17;
    if(!bad_object)
        poison_creature_field(&player, field);
    else
        poison_object_field(&item, field);
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &root_count, sizeof(root_count)) < 0 ||
       (bad_object && (write_full(fd, &item, sizeof(item)) < 0 ||
                       write_full(fd, &no_children, sizeof(no_children)) < 0)) ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_nested_partial_object_fixture(void)
{
    char path[] = "/tmp/muhan-files1-decoder-nested-partial.XXXXXX";
    creature player;
    object root;
    unsigned char bytes[sizeof(object)];
    int root_count = 1;
    int child_count = 1;
    size_t prefix = offsetof(object, first_obj) + sizeof(void *);
    int fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    memset(&player, 0, sizeof(player));
    memset(&root, 0, sizeof(root));
    memset(bytes, 0xa5, sizeof(bytes));
    player.type = PLAYER;
    player.fd = 17;
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &root_count, sizeof(root_count)) < 0 ||
       write_full(fd, &root, sizeof(root)) < 0 ||
       write_full(fd, &child_count, sizeof(child_count)) < 0 ||
       write_full(fd, bytes, prefix) < 0 ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int write_budget_fixture(int fd, int second_child_count)
{
    object root, item;
    int root_count = 2;
    int child_count;
    int no_children = 0;
    int root_index;
    int child_index;

    memset(&root, 0, sizeof(root));
    memset(&item, 0, sizeof(item));
    for(root_index = 0; root_index < root_count; ++root_index) {
        child_count = root_index == 0 ? 4095 : second_child_count;
        if(write_full(fd, &root, sizeof(root)) < 0 ||
           write_full(fd, &child_count, sizeof(child_count)) < 0)
            return -1;
        for(child_index = 0; child_index < child_count; ++child_index) {
            if(write_full(fd, &item, sizeof(item)) < 0 ||
               write_full(fd, &no_children, sizeof(no_children)) < 0)
                return -1;
        }
    }
    return 0;
}

static int open_budget_fixture(int second_child_count)
{
    char path[] = "/tmp/muhan-files1-decoder-budget.XXXXXX";
    creature player;
    int root_count = 2;
    int fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    memset(&player, 0, sizeof(player));
    player.type = PLAYER;
    player.fd = 17;
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &root_count, sizeof(root_count)) < 0 ||
       write_budget_fixture(fd, second_child_count) < 0 ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_sibling_fixture(void)
{
    char path[] = "/tmp/muhan-files1-decoder-siblings.XXXXXX";
    creature player;
    object root, item;
    int root_count = 1;
    int child_count = 2;
    int no_children = 0;
    int fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    memset(&player, 0, sizeof(player));
    memset(&root, 0, sizeof(root));
    memset(&item, 0, sizeof(item));
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &root_count, sizeof(root_count)) < 0 ||
       write_full(fd, &root, sizeof(root)) < 0 ||
       write_full(fd, &child_count, sizeof(child_count)) < 0 ||
       write_full(fd, &item, sizeof(item)) < 0 ||
       write_full(fd, &no_children, sizeof(no_children)) < 0 ||
       write_full(fd, &item, sizeof(item)) < 0 ||
       write_full(fd, &no_children, sizeof(no_children)) < 0 ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_partial_creature_fixture(void)
{
    char path[] = "/tmp/muhan-files1-decoder-partial.XXXXXX";
    unsigned char bytes[sizeof(creature)];
    size_t prefix = offsetof(creature, first_obj) + sizeof(void *);
    int fd;

    memset(bytes, 0xa5, sizeof(bytes));
    fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    if(write_full(fd, bytes, prefix) < 0 || lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_partial_object_fixture(void)
{
    char path[] = "/tmp/muhan-files1-decoder-object-partial.XXXXXX";
    unsigned char bytes[sizeof(object)];
    creature player;
    int count = 1;
    size_t prefix = offsetof(object, first_obj) + sizeof(void *);
    int fd;

    memset(&player, 0, sizeof(player));
    memset(bytes, 0xa5, sizeof(bytes));
    fd = mkstemp(path);
    if(fd < 0)
        return -1;
    unlink(path);
    if(write_full(fd, &player, sizeof(player)) < 0 ||
       write_full(fd, &count, sizeof(count)) < 0 ||
       write_full(fd, bytes, prefix) < 0 || lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_monster_fixture(void)
{
    int fd;
    char monster = MONSTER;
    fd = open_fixture(0, 0);
    if(fd < 0)
        return -1;
    if(lseek(fd, offsetof(creature, type), SEEK_SET) < 0 ||
       write_full(fd, &monster, sizeof(monster)) < 0 ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int open_trailing_fixture(void)
{
    int fd;
    char trailing = 'x';
    fd = open_fixture(1, 1);
    if(fd < 0)
        return -1;
    if(lseek(fd, 0, SEEK_END) < 0 ||
       write_full(fd, &trailing, sizeof(trailing)) < 0 ||
       lseek(fd, 0, SEEK_SET) < 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static int test_depth_budget(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;
    fd = open_fixture(80, 1);
    failed += expect(fd >= 0, "deep fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "over-depth player tree must fail closed");
        failed += expect(player->first_obj == 0,
                         "over-depth rejection must not return a partial tree");
        close(fd);
    }
    free(player);
    return failed;
}

static int test_count_and_allocation_budget(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;
    fd = open_fixture(0, INT_MAX);
    failed += expect(fd >= 0, "invalid-count fixture must be created");
    if(fd >= 0) {
        fatal_calls = 0;
        allocation_budget = -1;
        failed += expect(read_crt_player(fd, player) < 0,
                         "invalid root count must fail before allocation");
        failed += expect(fatal_calls == 0, "invalid count must not call merror(FATAL)");
        close(fd);
    }
    fd = open_fixture(2, 1);
    failed += expect(fd >= 0, "allocation fixture must be created");
    if(fd >= 0) {
        allocation_budget = 3;
        fatal_calls = 0;
        failed += expect(read_crt_player(fd, player) < 0,
                         "allocation failure must return a decoder error");
        failed += expect(fatal_calls == 0, "allocation failure must not call merror(FATAL)");
        failed += expect(player->first_obj == 0,
                         "allocation failure must not return a partial tree");
        close(fd);
    }
    fd = open_sibling_fixture();
    failed += expect(fd >= 0, "sibling allocation fixture must be created");
    if(fd >= 0) {
        allocation_budget = 5;
        fatal_calls = 0;
        failed += expect(read_crt_player(fd, player) < 0,
                         "partial sibling allocation must return a decoder error");
        failed += expect(fatal_calls == 0,
                         "partial sibling allocation must not call merror(FATAL)");
        failed += expect(player->first_obj == 0,
                         "partial sibling allocation must free already attached children");
        close(fd);
    }
    allocation_budget = -1;
    free(player);
    return failed;
}

static int test_player_record_contracts(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd, field;
    int failed = 0;
    if(!player)
        return 1;

    fd = open_monster_fixture();
    failed += expect(fd >= 0, "monster fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "non-player creature type must fail closed");
        failed += expect(player->first_obj == 0,
                         "non-player rejection must not return a tree");
        close(fd);
    }

    fd = open_fixture(0, 0);
    failed += expect(fd >= 0, "fd scrub fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) == 0,
                         "valid player record must load");
        failed += expect(player->fd == -1,
                         "loaded player must not retain a persisted socket");
        close(fd);
    }

    for(field = 0; field < 7; ++field) {
        fd = open_bad_string_fixture(0, field);
        failed += expect(fd >= 0, "creature string fixture must be created");
        if(fd >= 0) {
            decoder_read_bytes = 0;
            failed += expect(read_crt_player(fd, player) < 0,
                             "unterminated creature strings must fail closed");
            failed += expect(decoder_read_bytes == sizeof(creature),
                             "creature string check must follow the full creature read");
            failed += expect(player->first_obj == 0,
                             "bad creature strings must not return a tree");
            close(fd);
        }
    }

    for(field = 0; field < 6; ++field) {
        fd = open_bad_string_fixture(1, field);
        failed += expect(fd >= 0, "object string fixture must be created");
        if(fd >= 0) {
            decoder_read_bytes = 0;
            failed += expect(read_crt_player(fd, player) < 0,
                             "unterminated object strings must fail closed");
            failed += expect(decoder_read_bytes >=
                             sizeof(creature) + sizeof(int) + sizeof(object),
                             "object string check must follow the object header read");
            failed += expect(player->first_obj == 0,
                             "bad object strings must clean the root tree");
            close(fd);
        }
    }

    fd = open_trailing_fixture();
    failed += expect(fd >= 0, "trailing-byte fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "trailing player bytes must fail exact-record validation");
        failed += expect(player->first_obj == 0,
                         "trailing-byte rejection must clean the loaded tree");
        close(fd);
    }
    free(player);
    return failed;
}

static int test_nested_partial_and_total_budget(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;

    fd = open_nested_partial_object_fixture();
    failed += expect(fd >= 0, "nested partial object fixture must be created");
    if(fd >= 0) {
        decoder_read_bytes = 0;
        failed += expect(read_crt_player(fd, player) < 0,
                         "nested partial object header must fail closed");
        failed += expect(decoder_read_bytes >=
                         sizeof(creature) + sizeof(int) + sizeof(object) + sizeof(int) +
                         offsetof(object, first_obj) + sizeof(void *),
                         "nested partial fixture must reach the child object header");
        failed += expect(player->first_obj == 0,
                         "nested partial object must clean attached roots");
        close(fd);
    }

    fd = open_budget_fixture(4095);
    failed += expect(fd >= 0, "8192-object fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) == 0,
                         "exactly 8192 objects must fit the player budget");
        close(fd);
        free_crt(player);
        player = (creature *)calloc(1, sizeof(creature));
        if(!player)
            return failed + 1;
    }

    fd = open_budget_fixture(4096);
    failed += expect(fd >= 0, "8193-object fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "8193 objects must exceed the player budget");
        failed += expect(player->first_obj == 0,
                         "object budget rejection must clean attached roots");
        close(fd);
    }
    free(player);
    return failed;
}

static int test_short_reads_and_eintr(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;
    fd = open_fixture(2, 1);
    failed += expect(fd >= 0, "short-read fixture must be created");
    if(fd >= 0) {
        force_eintr = 1;
        failed += expect(read_crt_player(fd, player) == 0,
                         "short reads and EINTR must be completed");
        failed += expect(player->first_obj && player->first_obj->obj &&
                         player->first_obj->obj->first_obj,
                         "valid legacy player object nesting must be retained");
        failed += expect(player->first_obj->obj->parent_crt == player,
                         "top-level player object must retain its parent creature");
        failed += expect(player->first_obj->obj->first_obj->obj->parent_obj ==
                         player->first_obj->obj,
                         "nested player object must retain its parent object");
        close(fd);
    }
    if(player->first_obj)
        free_crt(player);
    else
        free(player);
    return failed;
}

static int test_partial_header_sanitizes_links(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;
    fd = open_partial_creature_fixture();
    failed += expect(fd >= 0, "partial creature fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "partial creature header must fail closed");
        failed += expect(player->first_obj == 0 && player->first_fol == 0 &&
                         player->first_enm == 0 && player->first_tlk == 0 &&
                         player->following == 0 && player->ready[0] == 0 &&
                         player->ready[MAXWEAR - 1] == 0,
                         "partial header must not leave hostile links");
        close(fd);
    }
    free(player);
    return failed;
}

static int test_partial_object_sanitizes_links(void)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    int fd;
    int failed = 0;
    if(!player)
        return 1;
    fd = open_partial_object_fixture();
    failed += expect(fd >= 0, "partial object fixture must be created");
    if(fd >= 0) {
        failed += expect(read_crt_player(fd, player) < 0,
                         "partial object header must fail closed");
        failed += expect(player->first_obj == 0,
                         "partial object header must not leave a hostile root link");
        close(fd);
    }
    free(player);
    return failed;
}

int main(void)
{
    int failed = 0;
    failed += test_depth_budget();
    failed += test_count_and_allocation_budget();
    failed += test_player_record_contracts();
    failed += test_nested_partial_and_total_budget();
    failed += test_short_reads_and_eintr();
    failed += test_partial_header_sanitizes_links();
    failed += test_partial_object_sanitizes_links();
    if(failed)
        return 1;
    puts("files1_decoder_test: ok");
    return 0;
}
