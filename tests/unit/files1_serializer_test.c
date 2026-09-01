#include <errno.h>
#include <stdio.h>
#include <string.h>

#include "mstruct.h"

extern int write_crt(int fd, creature *crt_ptr, char perm_only);

enum write_mode {
    WRITE_OK,
    WRITE_SHORT_FIRST,
    WRITE_EINTR_FIRST,
    WRITE_ENOSPC_AT
};

static enum write_mode mode;
static int write_calls;
static int fail_at;
static int fatal_calls;

int serializer_test_write(int fd, char *buf, unsigned long size)
{
    (void)fd;
    (void)buf;
    write_calls++;
    if(mode == WRITE_SHORT_FIRST && write_calls == 1)
        return size > 1 ? (int)(size / 2) : 1;
    if(mode == WRITE_EINTR_FIRST && write_calls == 1) {
        errno = EINTR;
        return -1;
    }
    if(mode == WRITE_ENOSPC_AT && write_calls == fail_at) {
        errno = ENOSPC;
        return -1;
    }
    return (int)size;
}

void merror(char *message, char kind)
{
    (void)message;
    (void)kind;
    fatal_calls++;
}

void del_active(creature *crt_ptr)
{
    (void)crt_ptr;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "files1_serializer_test: %s\n", message);
    return 1;
}

static void reset_script(enum write_mode next_mode, int next_fail_at)
{
    mode = next_mode;
    write_calls = 0;
    fail_at = next_fail_at;
    fatal_calls = 0;
}

int main(void)
{
    creature player;
    object container, nested;
    otag player_tag, nested_tag;
    int failed = 0;

    memset(&player, 0, sizeof(player));
    reset_script(WRITE_SHORT_FIRST, 0);
    failed += expect(write_crt(7, &player, 0) == 0,
                     "short writes must be completed by the serializer");
    failed += expect(fatal_calls == 0,
                     "short writes must not call merror(FATAL)");

    reset_script(WRITE_EINTR_FIRST, 0);
    failed += expect(write_crt(7, &player, 0) == 0,
                     "EINTR must be retried by the serializer");
    failed += expect(fatal_calls == 0,
                     "EINTR must not call merror(FATAL)");

    reset_script(WRITE_ENOSPC_AT, 1);
    failed += expect(write_crt(7, &player, 0) == -1,
                     "ENOSPC must be returned from write_crt");
    failed += expect(fatal_calls == 0,
                     "ENOSPC must not terminate the process");

    memset(&container, 0, sizeof(container));
    memset(&nested, 0, sizeof(nested));
    nested_tag.obj = &nested;
    nested_tag.next_tag = 0;
    container.first_obj = &nested_tag;
    player_tag.obj = &container;
    player_tag.next_tag = 0;
    player.first_obj = &player_tag;
    reset_script(WRITE_ENOSPC_AT, 5);
    failed += expect(write_crt(7, &player, 0) == -1,
                     "nested object write errors must reach write_crt");
    failed += expect(fatal_calls == 0,
                     "nested object errors must not call merror(FATAL)");

    if(failed)
        return 1;
    puts("files1_serializer_test: ok");
    return 0;
}
