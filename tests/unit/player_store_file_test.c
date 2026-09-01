#include <fcntl.h>
#include <errno.h>
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

int read_crt_player(int fd, creature *player)
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

static int path_mode_is(const char *path, mode_t mode)
{
    struct stat st;

    return stat(path, &st) == 0 && (st.st_mode & 0777) == mode;
}

int main(void)
{
    char root[] = "/tmp/muhan-player-store.XXXXXX";
    char legacy[512], canonical[512], shard_dir[512], contents[128];
    char fresh_file[512], fresh_shard[512], oversized_file[512], oversized_shard[512];
    char link_file[512], link_shard[512], target_dir[512], target_file[512];
    char parent_target[512];
    char *slash;
    creature input;
    creature *output = (creature *)1;
    int fd, failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    if(setenv("MUHAN_HOME", root, 1) < 0) {
        perror("setenv");
        return 1;
    }
    snprintf(canonical, sizeof(canonical), "%s/player", root);
    snprintf(parent_target, sizeof(parent_target), "%s/player-target", root);
    if(mkdir(parent_target, 0700) < 0 || symlink(parent_target, canonical) < 0) {
        perror("parent player symlink");
        return 1;
    }
    failed += expect(file_player_store_load("ParentLink", &output) == PLAYER_STORE_IO_ERROR &&
                     output == 0,
                     "load must reject a MUHAN_HOME/player intermediate symlink");
    if(unlink(canonical) < 0) {
        perror("unlink parent player symlink");
        return 1;
    }
    if(mkdir(canonical, 0770) < 0) {
        perror("mkdir player");
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
    if(mkdir(shard_dir, 0777) < 0) {
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
    failed += expect(path_mode_is(shard_dir, 0700),
                     "existing shard directory must be normalized to 0700 before save");
    failed += expect(path_mode_is(canonical, 0600),
                     "atomically saved player file must be 0600");

    failed += expect(player_path_from_name("FreshTester", fresh_file, sizeof(fresh_file)) == 0,
                     "new shard fixture path creation");
    snprintf(fresh_shard, sizeof(fresh_shard), "%s", fresh_file);
    slash = strrchr(fresh_shard, '/');
    if(!slash) return 1;
    *slash = 0;
    failed += expect(file_player_store_save("FreshTester", &input) == PLAYER_STORE_OK,
                     "save must create a missing shard");
    failed += expect(path_mode_is(fresh_shard, 0700),
                     "new shard directory must be exactly 0700");
    failed += expect(path_mode_is(fresh_file, 0600),
                     "new shard player file must be exactly 0600");

    failed += expect(player_path_from_name("Oversized", oversized_file, sizeof(oversized_file)) == 0,
                     "oversized read fixture path creation");
    snprintf(oversized_shard, sizeof(oversized_shard), "%s", oversized_file);
    slash = strrchr(oversized_shard, '/');
    if(!slash) return 1;
    *slash = 0;
    if(mkdir(oversized_shard, 0700) < 0) {
        perror("mkdir oversized shard");
        return 1;
    }
    fd = open(oversized_file, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || ftruncate(fd, (off_t)PLAYER_PATH_READ_MAX_BYTES + 1) < 0 || close(fd) < 0) {
        perror("oversized player fixture");
        return 1;
    }
    errno = ENOENT;
    failed += expect(player_path_open_readonly("Oversized") < 0 && errno == EFBIG,
                     "oversized regular player files must fail with explicit EFBIG");
    errno = ENOENT;
    output = (creature *)1;
    failed += expect(file_player_store_load("Oversized", &output) == PLAYER_STORE_IO_ERROR &&
                     output == 0,
                     "oversized player rejection must not be misclassified as not found");

    snprintf(target_dir, sizeof(target_dir), "%s/target", root);
    if(mkdir(target_dir, 0700) < 0) {
        perror("mkdir target");
        return 1;
    }
    failed += expect(player_path_from_name("LinkTester", link_file, sizeof(link_file)) == 0,
                     "symlink save fixture path creation");
    snprintf(link_shard, sizeof(link_shard), "%s", link_file);
    slash = strrchr(link_shard, '/');
    if(!slash) return 1;
    *slash = 0;
    failed += expect(symlink(target_dir, link_shard) == 0,
                     "shard symlink fixture creation");
    failed += expect(file_player_store_save("LinkTester", &input) == PLAYER_STORE_IO_ERROR,
                     "save must fail closed for a shard symlink");
    output = (creature *)1;
    failed += expect(file_player_store_load("LinkTester", &output) == PLAYER_STORE_IO_ERROR &&
                     output == 0,
                     "load must reject an intermediate shard symlink");

    snprintf(target_file, sizeof(target_file), "%s/legacy-player", target_dir);
    failed += expect(write_text(target_file, "legacy-player-record") == 0,
                     "final symlink target fixture creation");
    unlink(canonical);
    failed += expect(symlink(target_file, canonical) == 0,
                     "final player symlink fixture creation");
    output = (creature *)1;
    failed += expect(file_player_store_load("Tester", &output) == PLAYER_STORE_IO_ERROR,
                     "load must fail closed for a final player symlink");
    failed += expect(output == 0, "symlinked player must not allocate an object");

    unlink(canonical);
    unlink(fresh_file);
    unlink(oversized_file);
    unlink(link_shard);
    unlink(target_file);
    rmdir(target_dir);
    rmdir(shard_dir);
    rmdir(fresh_shard);
    rmdir(oversized_shard);
    snprintf(canonical, sizeof(canonical), "%s/player", root);
    rmdir(canonical);
    rmdir(parent_target);
    rmdir(root);
    if(failed) return 1;
    puts("player_store_file_test: ok");
    return 0;
}
