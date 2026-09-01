#include "mstruct.h"
#include "mextern.h"
#include "mtype.h"
#include "onboarding_receipt.h"
#include "onboarding_recovery.h"
#include "onboarding_session.h"
#include "player_path.h"
#include "player_store.h"
#include "resource_path.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_BINARY
#define O_BINARY 0
#endif
#ifndef O_DIRECTORY
#define O_DIRECTORY 0
#endif
#ifndef O_NOFOLLOW
#define O_NOFOLLOW 0
#endif
#ifndef O_CLOEXEC
#define O_CLOEXEC 0
#endif

#define ONBOARDING_RECOVERY_RECEIPT_DIR MUDHOME "/onboarding-receipts"
#define ONBOARDING_RECOVERY_PATH_MAX 1024
#define ONBOARDING_RECOVERY_TEXT_MAX 640

static unsigned long ore_bounded_strlen(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;

    if(!value) return limit + 1;
    for(length=0; length<=limit; length++)
        if(value[length] == 0) return length;
    return limit + 1;
}

static int ore_lower_hex(value)
char value;
{
    return (value >= '0' && value <= '9') ||
           (value >= 'a' && value <= 'f');
}

static int ore_exact_hex(value, length)
const char *value;
unsigned long length;
{
    unsigned long i;

    if(!value || ore_bounded_strlen(value, length) != length) return 0;
    for(i=0; i<length; i++)
        if(!ore_lower_hex(value[i])) return 0;
    return 1;
}

static int ore_uuid(value)
const char *value;
{
    unsigned long i;

    if(!value || ore_bounded_strlen(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(i=0; i<ONBOARDING_ADMISSION_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(value[i] != '-') return 0;
        }
        else if(!ore_lower_hex(value[i])) return 0;
    }
    return 1;
}

static int ore_copy(destination, destination_size, source)
char *destination;
unsigned long destination_size;
const char *source;
{
    unsigned long length;

    if(!destination || !destination_size || !source) return -1;
    length = ore_bounded_strlen(source, destination_size - 1);
    if(length >= destination_size) return -1;
    memcpy(destination, source, length);
    destination[length] = 0;
    return 0;
}

static int ore_storage_format(value)
const char *value;
{
    /* Recovery deliberately supports only the byte representation written by
     * this binary.  A future format must add a new recovery implementation,
     * rather than guessing how to validate a foreign player structure. */
    return value && !strcmp(value, "player-v1");
}

static onboarding_receipt_state ore_state(value)
const char *value;
{
    if(!value) return ONBOARDING_RECEIPT_INVALID;
    if(!strcmp(value, "pending")) return ONBOARDING_RECEIPT_PENDING;
    if(!strcmp(value, "saved")) return ONBOARDING_RECEIPT_SAVED;
    if(!strcmp(value, "committed")) return ONBOARDING_RECEIPT_COMMITTED;
    return ONBOARDING_RECEIPT_INVALID;
}

static int ore_parse_line(line, prefix, value, value_size)
char *line;
const char *prefix;
char *value;
unsigned long value_size;
{
    unsigned long prefix_size;

    if(!line || !prefix || !value) return -1;
    prefix_size = (unsigned long)strlen(prefix);
    if(strncmp(line, prefix, prefix_size) != 0) return -1;
    return ore_copy(value, value_size, line + prefix_size);
}

static int ore_receipt_valid(receipt)
const onboarding_receipt *receipt;
{
    unsigned long name_length;

    if(!receipt || !ore_uuid(receipt->actor_id) ||
       !ore_uuid(receipt->correlation_id) || !ore_uuid(receipt->character_id) ||
       !ore_storage_format(receipt->storage_format)) return 0;
    name_length = ore_bounded_strlen(receipt->canonical_name_hex,
                                     ONBOARDING_RECEIPT_NAME_HEX_MAX);
    if(name_length < 2 || name_length > ONBOARDING_RECEIPT_NAME_HEX_MAX ||
       (name_length & 1) || !ore_exact_hex(receipt->canonical_name_hex,
                                            name_length)) return 0;
    if(receipt->state == ONBOARDING_RECEIPT_PENDING)
        return receipt->file_sha256[0] == 0;
    if(receipt->state == ONBOARDING_RECEIPT_SAVED ||
       receipt->state == ONBOARDING_RECEIPT_COMMITTED)
        return ore_exact_hex(receipt->file_sha256,
                             ONBOARDING_ADMISSION_SHA256_HEX_LEN);
    return 0;
}

/* Receipt parsing is intentionally duplicated from the writer's private
 * parser.  Startup needs an already-open no-follow file descriptor, while
 * the public reader accepts a correlation and necessarily reopens a path. */
static int ore_parse_receipt(text, receipt)
char *text;
onboarding_receipt *receipt;
{
    static const char *prefixes[8] = {
        "version=", "state=", "actor_uuid=", "correlation_uuid=",
        "character_uuid=", "canonical_name_hex=", "storage_format=",
        "saved_file_sha256="
    };
    char version[4], state[16];
    char *line, *next;
    int i;

    if(!text || !receipt) return -1;
    memset(receipt, 0, sizeof(*receipt));
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    line = text;
    for(i=0; i<8; i++) {
        next = strchr(line, '\n');
        if(!next) goto bad;
        *next = 0;
        switch(i) {
        case 0:
            if(ore_parse_line(line, prefixes[i], version, sizeof(version)) != 0)
                goto bad;
            break;
        case 1:
            if(ore_parse_line(line, prefixes[i], state, sizeof(state)) != 0)
                goto bad;
            break;
        case 2:
            if(ore_parse_line(line, prefixes[i], receipt->actor_id,
                              sizeof(receipt->actor_id)) != 0) goto bad;
            break;
        case 3:
            if(ore_parse_line(line, prefixes[i], receipt->correlation_id,
                              sizeof(receipt->correlation_id)) != 0) goto bad;
            break;
        case 4:
            if(ore_parse_line(line, prefixes[i], receipt->character_id,
                              sizeof(receipt->character_id)) != 0) goto bad;
            break;
        case 5:
            if(ore_parse_line(line, prefixes[i], receipt->canonical_name_hex,
                              sizeof(receipt->canonical_name_hex)) != 0) goto bad;
            break;
        case 6:
            if(ore_parse_line(line, prefixes[i], receipt->storage_format,
                              sizeof(receipt->storage_format)) != 0) goto bad;
            break;
        case 7:
            if(ore_parse_line(line, prefixes[i], receipt->file_sha256,
                              sizeof(receipt->file_sha256)) != 0) goto bad;
            break;
        }
        line = next + 1;
    }
    if(*line || strcmp(version, "1") != 0) goto bad;
    receipt->state = ore_state(state);
    if(!ore_receipt_valid(receipt)) goto bad;
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    return 0;
bad:
    memset(receipt, 0, sizeof(*receipt));
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    return -1;
}

static int ore_receipt_filename(entry, correlation)
const char *entry;
char correlation[ONBOARDING_ADMISSION_UUID_LEN + 1];
{
    static const char suffix[] = ".receipt";
    unsigned long length;

    if(!entry || !correlation) return -1;
    length = ore_bounded_strlen(entry, 255);
    if(length != ONBOARDING_ADMISSION_UUID_LEN + sizeof(suffix) - 1 ||
       strcmp(entry + ONBOARDING_ADMISSION_UUID_LEN, suffix) != 0) return -1;
    memcpy(correlation, entry, ONBOARDING_ADMISSION_UUID_LEN);
    correlation[ONBOARDING_ADMISSION_UUID_LEN] = 0;
    return ore_uuid(correlation) ? 0 : -1;
}

static int ore_read_receipt(dir_fd, entry, receipt)
int dir_fd;
const char *entry;
onboarding_receipt *receipt;
{
    char text[ONBOARDING_RECOVERY_TEXT_MAX];
    struct stat st;
    unsigned long used;
    int fd, count, result;

    result = -1;
    fd = -1;
    used = 0;
    memset(text, 0, sizeof(text));
    if(dir_fd < 0 || !entry || !receipt) goto out;
    fd = openat(dir_fd, entry, O_RDONLY | O_BINARY | O_NOFOLLOW | O_CLOEXEC);
    if(fd < 0 || fstat(fd, &st) < 0 || !S_ISREG(st.st_mode) ||
       (st.st_mode & 0777) != 0600 || st.st_nlink != 1) goto out;
    while(used < sizeof(text) - 1) {
        count = read(fd, text + used, sizeof(text) - 1 - used);
        if(count > 0) {
            used += (unsigned long)count;
            continue;
        }
        if(count == 0) break;
        if(errno != EINTR) goto out;
    }
    if(used == sizeof(text) - 1) {
        count = read(fd, text + used, 1);
        if(count != 0) goto out;
    }
    if(close(fd) < 0) goto out;
    fd = -1;
    text[used] = 0;
    result = ore_parse_receipt(text, receipt);
out:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    return result;
}

static int ore_hex_value(value)
char value;
{
    if(value >= '0' && value <= '9') return value - '0';
    if(value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static int ore_canonical_name(receipt, name, name_size)
const onboarding_receipt *receipt;
char *name;
unsigned long name_size;
{
    char canonical[PLAYER_NAME_MAX_BYTES + 1];
    unsigned long i, length;
    int high, low;

    if(!receipt || !name || name_size < PLAYER_NAME_MAX_BYTES + 1) return -1;
    memset(name, 0, name_size);
    memset(canonical, 0, sizeof(canonical));
    length = ore_bounded_strlen(receipt->canonical_name_hex,
                                ONBOARDING_RECEIPT_NAME_HEX_MAX);
    if(!length || (length & 1) || length / 2 > PLAYER_NAME_MAX_BYTES) goto bad;
    for(i=0; i<length; i+=2) {
        high = ore_hex_value(receipt->canonical_name_hex[i]);
        low = ore_hex_value(receipt->canonical_name_hex[i + 1]);
        if(high < 0 || low < 0) goto bad;
        name[i / 2] = (char)((high << 4) | low);
        if(name[i / 2] == 0) goto bad;
    }
    name[length / 2] = 0;
    if(!utf8_validate((const unsigned char *)name, length / 2) ||
       !player_name_is_valid((const unsigned char *)name,
                             PLAYER_NAME_MIN_CODEPOINTS,
                             PLAYER_NAME_MAX_CODEPOINTS)) goto bad;
    if(ore_copy(canonical, sizeof(canonical), name) != 0) goto bad;
    lowercize(canonical, 1);
    if(strcmp(canonical, name) != 0 || !strcmp(name, DMNAME) ||
       !strcmp(name, DMNAME2) || !strcmp(name, DMNAME3) ||
       !strcmp(name, DMNAME4) || !strcmp(name, DMNAME5) ||
       !strcmp(name, DMNAME6) || !strcmp(name, DMNAME7)) goto bad;
    memset(canonical, 0, sizeof(canonical));
    return 0;
bad:
    memset(name, 0, name_size);
    memset(canonical, 0, sizeof(canonical));
    return -1;
}

/* Returns 0 for the one harmless case: a valid pending receipt whose player
 * file was never published.  Any other file-system anomaly is an error. */
static int ore_player_exists(name, path, path_size, before)
const char *name;
char *path;
unsigned long path_size;
struct stat *before;
{
    struct stat opened;
    int fd;

    if(!name || !path || !before ||
       player_path_from_name(name, path, path_size) != 0) return -1;
    if(lstat(path, before) < 0) return errno == ENOENT ? 0 : -1;
    if(!S_ISREG(before->st_mode) || S_ISLNK(before->st_mode) ||
       before->st_nlink != 1) return -1;
    fd = open(path, O_RDONLY | O_BINARY | O_NOFOLLOW | O_CLOEXEC);
    if(fd < 0) return -1;
    if(fstat(fd, &opened) < 0 || !S_ISREG(opened.st_mode) ||
       opened.st_nlink != 1 || opened.st_dev != before->st_dev ||
       opened.st_ino != before->st_ino) {
        close(fd);
        return -1;
    }
    if(close(fd) < 0)
        return -1;
    return 1;
}

static int ore_same_player(path, before)
const char *path;
const struct stat *before;
{
    struct stat after;

    return path && before && lstat(path, &after) == 0 &&
           S_ISREG(after.st_mode) && !S_ISLNK(after.st_mode) &&
           after.st_nlink == 1 && after.st_dev == before->st_dev &&
           after.st_ino == before->st_ino;
}

static int ore_recover_pending(receipt)
const onboarding_receipt *receipt;
{
    char name[PLAYER_NAME_MAX_BYTES + 1];
    char path[ONBOARDING_RECOVERY_PATH_MAX];
    char digest[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
    creature *player;
    struct stat before;
    int exists, result;

    result = -1;
    player = 0;
    memset(name, 0, sizeof(name));
    memset(path, 0, sizeof(path));
    memset(digest, 0, sizeof(digest));
    memset(&before, 0, sizeof(before));
    if(ore_canonical_name(receipt, name, sizeof(name)) != 0) goto out;
    exists = ore_player_exists(name, path, sizeof(path), &before);
    if(exists == 0) {
        result = 0;
        goto out;
    }
    if(exists < 0 || load_ply(name, &player) != PLAYER_STORE_OK || !player ||
       ore_bounded_strlen(player->name, sizeof(player->name) - 1) >=
           sizeof(player->name) || strcmp(player->name, name) != 0 ||
       !ore_same_player(path, &before) ||
       onboarding_session_file_sha256(name, digest) != 0 ||
       !ore_same_player(path, &before)) goto out;
    if(onboarding_receipt_mark_saved(receipt->actor_id, receipt->correlation_id,
                                     receipt->character_id, name,
                                     receipt->storage_format, digest) != 0) goto out;
    result = 0;
out:
    /* A corrupt on-disk type must not make cleanup manipulate live monster
     * state.  load_ply has already validated the byte structure; this only
     * makes its detached allocation safe for the legacy free routine. */
    if(player) {
        player->type = PLAYER;
        free_crt(player);
    }
    memset(name, 0, sizeof(name));
    memset(path, 0, sizeof(path));
    memset(digest, 0, sizeof(digest));
    memset(&before, 0, sizeof(before));
    return result;
}

static int ore_is_interrupted_temp(entry)
const char *entry;
{
    return entry && !strncmp(entry, ".receipt.tmp.", 13);
}

int onboarding_recovery_startup(void)
{
    char directory[ONBOARDING_RECOVERY_PATH_MAX];
    char correlation[ONBOARDING_ADMISSION_UUID_LEN + 1];
    onboarding_receipt receipt;
    struct stat st;
    struct dirent *entry;
    DIR *stream;
    int dir_fd, mode, result;

    mode = onboarding_session_mode();
    if(mode == 0) return 0;
    if(mode != 1 ||
       resolve_runtime_path(ONBOARDING_RECOVERY_RECEIPT_DIR, directory,
                            sizeof(directory)) != 0) return -1;
    dir_fd = open(directory, O_RDONLY | O_BINARY | O_DIRECTORY | O_NOFOLLOW |
                  O_CLOEXEC);
    if(dir_fd < 0) return errno == ENOENT ? 0 : -1;
    if(fstat(dir_fd, &st) < 0 || !S_ISDIR(st.st_mode) ||
       (st.st_mode & 0777) != 0700) {
        close(dir_fd);
        return -1;
    }
    stream = fdopendir(dir_fd);
    if(!stream) {
        close(dir_fd);
        return -1;
    }
    result = -1;
    for(;;) {
        errno = 0;
        entry = readdir(stream);
        if(!entry) {
            if(errno != 0) goto out;
            break;
        }
        memset(correlation, 0, sizeof(correlation));
        memset(&receipt, 0, sizeof(receipt));
        if(!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..") ||
           ore_is_interrupted_temp(entry->d_name)) continue;
        if(ore_receipt_filename(entry->d_name, correlation) != 0 ||
           ore_read_receipt(dirfd(stream), entry->d_name, &receipt) != 0 ||
           strcmp(correlation, receipt.correlation_id) != 0) goto out;
        if(receipt.state == ONBOARDING_RECEIPT_PENDING &&
           ore_recover_pending(&receipt) != 0) goto out;
    }
    result = 0;
out:
    memset(correlation, 0, sizeof(correlation));
    memset(&receipt, 0, sizeof(receipt));
    memset(directory, 0, sizeof(directory));
    if(closedir(stream) < 0) result = -1;
    return result;
}
