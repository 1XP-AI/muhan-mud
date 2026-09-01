#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "mstruct.h"
#include "player_path.h"
#include "player_store.h"

static int fail_write;

void merror(char *message, char kind)
{
    (void)message;
    (void)kind;
    abort();
}

void zero(void *memory, int size)
{
    memset(memory, 0, (size_t)size);
}

int utf8_validate(const unsigned char *s, unsigned long n)
{
    (void)s;
    (void)n;
    return 1;
}

unsigned long utf8_codepoint_len(const unsigned char *s)
{
    return s ? (unsigned long)strlen((const char *)s) : 0;
}

int write_crt(int fd, creature *player, char perm_only)
{
    const char payload[] = "new-player-record";
    (void)player;
    (void)perm_only;
    if(fail_write)
        return -1;
    return write(fd, payload, sizeof(payload)) == sizeof(payload) ? 0 : -1;
}

int read_crt(int fd, creature *player)
{
    (void)fd;
    (void)player;
    return -1;
}

void free_crt(creature *player)
{
    free(player);
}

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "player_store_file_test: %s\n", message);
    return 1;
}

static int write_text(const char *path, const char *text)
{
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0660);
    int n;
    if(fd < 0) return -1;
    n = write(fd, text, strlen(text));
    close(fd);
    return n == (int)strlen(text) ? 0 : -1;
}

static int read_text(const char *path, char *out, unsigned long out_sz)
{
    int fd, n;
    if(!out_sz) return -1;
    fd = open(path, O_RDONLY);
    if(fd < 0) return -1;
    n = read(fd, out, out_sz - 1);
    close(fd);
    if(n < 0) return -1;
    out[n] = 0;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-player-store.XXXXXX";
    char legacy[512], canonical[512], shard_dir[512], contents[128];
    char *slash;
    creature input;
    creature *output = (creature *)1;
    int failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    snprintf(canonical, sizeof(canonical), "%s/player", root);
    if(mkdir(canonical, 0770) < 0) {
        perror("mkdir player");
        return 1;
    }
    if(setenv("MUHAN_HOME", root, 1) < 0) {
        perror("setenv");
        return 1;
    }

    failed += expect(file_player_store_load("Tester", &output) == PLAYER_STORE_NOT_FOUND,
                     "missing file must be distinct from corrupt data");
    failed += expect(output == 0, "missing player must not allocate an object");
    failed += expect(player_path_from_name("Tester", legacy, sizeof(legacy)) == 0,
                     "player path creation must succeed");
    snprintf(canonical, sizeof(canonical), "%s", legacy);
    snprintf(shard_dir, sizeof(shard_dir), "%s", canonical);
    slash = strrchr(shard_dir, '/');
    if(!slash) {
        fprintf(stderr, "player_store_file_test: missing shard separator\n");
        return 1;
    }
    *slash = 0;
    if(mkdir(shard_dir, 0770) < 0) {
        perror("mkdir shard");
        return 1;
    }

    failed += expect(write_text(canonical, "bad") == 0, "truncated fixture creation");
    output = (creature *)1;
    failed += expect(file_player_store_load("Tester", &output) == PLAYER_STORE_CORRUPT,
                     "truncated file must fail closed as corrupt");
    failed += expect(output == 0, "corrupt player must not return partial state");

    failed += expect(write_text(canonical, "old-player-record") == 0, "canonical fixture creation");
    memset(&input, 0, sizeof(input));
    fail_write = 1;
    failed += expect(file_player_store_save("Tester", &input) == PLAYER_STORE_IO_ERROR,
                     "temp write failure must be an IO error");
    failed += expect(read_text(canonical, contents, sizeof(contents)) == 0 && !strcmp(contents, "old-player-record"),
                     "failed save must leave canonical file unchanged");
    fail_write = 0;
    failed += expect(file_player_store_save("Tester", &input) == PLAYER_STORE_OK,
                     "flushed temp write must save successfully");
    failed += expect(read_text(canonical, contents, sizeof(contents)) == 0 && !strcmp(contents, "new-player-record"),
                     "atomic rename must replace canonical file");

    unlink(canonical);
    rmdir(shard_dir);
    snprintf(canonical, sizeof(canonical), "%s/player", root);
    rmdir(canonical);
    rmdir(root);
    if(failed) return 1;
    puts("player_store_file_test: ok");
    return 0;
}
