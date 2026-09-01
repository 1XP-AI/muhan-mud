#include "character_save_journal.h"
#include "mtype.h"
#include "resource_path.h"
#include "utf8_text.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "character save journal requires O_NOFOLLOW; refusing insecure fallback"
#endif
#ifndef O_DIRECTORY
#error "character save journal requires O_DIRECTORY"
#endif

#define CHARACTER_SAVE_JOURNAL_DIR_LEGACY MUDHOME "/character-save-journal"
#define CHARACTER_SAVE_JOURNAL_PATH_MAX 1024
#define CHARACTER_SAVE_JOURNAL_TEXT_MAX 896
#define CHARACTER_SAVE_JOURNAL_TEMP_MAX 128

static unsigned long journal_temp_counter;
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
static int journal_fail_parent_fsync;
static int journal_write_eintr_once;
static int journal_write_short_once;
static int journal_read_eintr_once;
static int journal_read_short_once;
static int journal_fail_temp_write;
static int journal_fail_temp_fsync;
static int journal_fail_temp_close_once;
static int journal_fail_temp_unlink_once;
#endif

#ifdef CHARACTER_SAVE_JOURNAL_TESTING
static ssize_t journal_test_read(fd, buffer, length)
int fd;
void *buffer;
size_t length;
{
    if(journal_read_eintr_once) {
        journal_read_eintr_once = 0;
        errno = EINTR;
        return -1;
    }
    if(journal_read_short_once && length > 1) {
        journal_read_short_once = 0;
        return read(fd, buffer, 1);
    }
    return read(fd, buffer, length);
}

static ssize_t journal_test_write(fd, buffer, length)
int fd;
const void *buffer;
size_t length;
{
    if(journal_fail_temp_write) {
        errno = EIO;
        return -1;
    }
    if(journal_write_eintr_once) {
        journal_write_eintr_once = 0;
        errno = EINTR;
        return -1;
    }
    if(journal_write_short_once && length > 1) {
        journal_write_short_once = 0;
        return write(fd, buffer, 1);
    }
    return write(fd, buffer, length);
}
#else
#define journal_test_read read
#define journal_test_write write
#endif

static unsigned long journal_bounded_strlen(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1;
    for(length = 0; length <= limit; length++)
        if(value[length] == 0) return length;
    return limit + 1;
}

static int journal_uuid(value)
const char *value;
{
    int i;
    if(!value || journal_bounded_strlen(value, CHARACTER_SAVE_JOURNAL_UUID_LEN) !=
       CHARACTER_SAVE_JOURNAL_UUID_LEN) return 0;
    for(i = 0; i < CHARACTER_SAVE_JOURNAL_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(value[i] != '-') return 0;
        }
        else if(!((value[i] >= '0' && value[i] <= '9') ||
                  (value[i] >= 'a' && value[i] <= 'f'))) return 0;
    }
    return 1;
}

static int journal_hex(value, expected_length, optional)
const char *value;
unsigned long expected_length;
int optional;
{
    unsigned long length, i;
    if(!value) return optional;
    length = journal_bounded_strlen(value, expected_length);
    if(optional && !length) return 1;
    if(length != expected_length) return 0;
    for(i = 0; i < length; i++)
        if(!((value[i] >= '0' && value[i] <= '9') ||
             (value[i] >= 'a' && value[i] <= 'f'))) return 0;
    return 1;
}

static int journal_name(const char *value);

static int journal_decode_name_hex(value, decoded, decoded_size)
const char *value;
unsigned char *decoded;
unsigned long decoded_size;
{
    unsigned long length, i;
    if(!value || !decoded || !decoded_size) return 0;
    length = journal_bounded_strlen(value, CHARACTER_SAVE_JOURNAL_NAME_HEX_MAX);
    if(length < 2 || length > CHARACTER_SAVE_JOURNAL_NAME_HEX_MAX || (length & 1))
        return 0;
    if(decoded_size < length / 2 + 1) return 0;
    for(i = 0; i < length; i++)
        if(!((value[i] >= '0' && value[i] <= '9') ||
             (value[i] >= 'a' && value[i] <= 'f'))) return 0;
    for(i = 0; i < length / 2; i++) {
        unsigned char high = (unsigned char)value[2 * i];
        unsigned char low = (unsigned char)value[2 * i + 1];
        high = (unsigned char)(high <= '9' ? high - '0' : high - 'a' + 10);
        low = (unsigned char)(low <= '9' ? low - '0' : low - 'a' + 10);
        decoded[i] = (unsigned char)((high << 4) | low);
    }
    decoded[length / 2] = 0;
    return journal_name((const char *)decoded);
}

static int journal_canonical_name_hex(value)
const char *value;
{
    unsigned char decoded[PLAYER_NAME_MAX_BYTES + 1];
    return journal_decode_name_hex(value, decoded, sizeof(decoded));
}

static int journal_name(value)
const char *value;
{
    unsigned long length, i;
    if(!value) return 0;
    length = journal_bounded_strlen(value, PLAYER_NAME_MAX_BYTES);
    if(!length || length > PLAYER_NAME_MAX_BYTES ||
       !utf8_validate((const unsigned char *)value, length) ||
       utf8_codepoint_len((const unsigned char *)value) < PLAYER_NAME_MIN_CODEPOINTS ||
       utf8_codepoint_len((const unsigned char *)value) > PLAYER_NAME_MAX_CODEPOINTS)
        return 0;
    for(i = 0; i < length; i++)
        if((unsigned char)value[i] < 0x20 || (unsigned char)value[i] == 127 ||
           value[i] == '/' || value[i] == '\\' || value[i] == ':')
            return 0;
    if(!strcmp(value, ".") || !strcmp(value, "..")) return 0;
    return 1;
}

static int journal_name_is_canonical(value)
const char *value;
{
    char canonical[PLAYER_NAME_MAX_BYTES + 1];
    unsigned long length, i;
    if(!journal_name(value)) return 0;
    length = strlen(value);
    memcpy(canonical, value, length + 1);
    for(i = 0; i < length; i++)
        if(canonical[i] >= 'A' && canonical[i] <= 'Z')
            canonical[i] = (char)(canonical[i] + ('a' - 'A'));
    if(canonical[0] >= 'a' && canonical[0] <= 'z')
        canonical[0] = (char)(canonical[0] - ('a' - 'A'));
    return !strcmp(value, canonical);
}

static int journal_storage_format(value)
const char *value;
{
    unsigned long length, i;
    if(!value) return 0;
    length = journal_bounded_strlen(value, CHARACTER_SAVE_JOURNAL_STORAGE_FORMAT_MAX);
    if(!length || length > CHARACTER_SAVE_JOURNAL_STORAGE_FORMAT_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(i = 1; i < length; i++)
        if(!((value[i] >= 'a' && value[i] <= 'z') ||
             (value[i] >= '0' && value[i] <= '9') || value[i] == '-' ||
             value[i] == '_' || value[i] == '.')) return 0;
    return 1;
}

static int journal_world_id(value)
const char *value;
{
    unsigned long length, i;
    if(!value) return 0;
    length = journal_bounded_strlen(value, CHARACTER_SAVE_JOURNAL_WORLD_ID_MAX);
    if(!length || length > CHARACTER_SAVE_JOURNAL_WORLD_ID_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(i = 1; i < length; i++)
        if(!((value[i] >= 'a' && value[i] <= 'z') ||
             (value[i] >= '0' && value[i] <= '9') || value[i] == '-' ||
             value[i] == '_')) return 0;
    return 1;
}

static int journal_legacy_shard(value)
const char *value;
{
    return value && journal_bounded_strlen(value,
           CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN) ==
           CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN &&
           ((value[0] >= '0' && value[0] <= '9') ||
            (value[0] >= 'a' && value[0] <= 'f')) &&
           ((value[1] >= '0' && value[1] <= '9') ||
            (value[1] >= 'a' && value[1] <= 'f'));
}

static int journal_uint64(value, out)
const char *value;
uint64_t *out;
{
    unsigned long length, i;
    uint64_t parsed, digit;
    if(!value || !out) return 0;
    length = journal_bounded_strlen(value, 20);
    if(!length || length > 19) return 0;
    if(length > 1 && value[0] == '0') return 0;
    parsed = 0;
    for(i = 0; i < length; i++) {
        if(value[i] < '0' || value[i] > '9') return 0;
        digit = (uint64_t)(value[i] - '0');
        if(parsed > (UINT64_MAX - digit) / UINT64_C(10)) return 0;
        parsed = parsed * UINT64_C(10) + digit;
    }
    if(parsed == 0 || parsed > (uint64_t)INT64_MAX) return 0;
    *out = parsed;
    return 1;
}

static const char *journal_precondition_name(precondition)
character_save_journal_precondition precondition;
{
    if(precondition == CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING)
        return "existing";
    if(precondition == CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT)
        return "absent";
    return 0;
}

static character_save_journal_precondition journal_parse_precondition(value)
const char *value;
{
    if(!value) return 0;
    if(!strcmp(value, "existing")) return CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING;
    if(!strcmp(value, "absent")) return CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT;
    return 0;
}

static const char *journal_state_name(state)
character_save_journal_state state;
{
    switch(state) {
    case CHARACTER_SAVE_JOURNAL_PREPARED: return "prepared";
    case CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED: return "legacy_published";
    case CHARACTER_SAVE_JOURNAL_DB_ACKED: return "db_acked";
    default: return 0;
    }
}

static character_save_journal_state journal_parse_state(value)
const char *value;
{
    if(!value) return CHARACTER_SAVE_JOURNAL_INVALID;
    if(!strcmp(value, "prepared")) return CHARACTER_SAVE_JOURNAL_PREPARED;
    if(!strcmp(value, "legacy_published"))
        return CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED;
    if(!strcmp(value, "db_acked")) return CHARACTER_SAVE_JOURNAL_DB_ACKED;
    return CHARACTER_SAVE_JOURNAL_INVALID;
}

static int journal_copy(destination, destination_size, value)
char *destination;
unsigned long destination_size;
const char *value;
{
    unsigned long length;
    if(!destination || !destination_size || !value) return -1;
    length = journal_bounded_strlen(value, destination_size - 1);
    if(length >= destination_size) return -1;
    memcpy(destination, value, length);
    destination[length] = 0;
    return 0;
}

static int journal_encode_name(name, out, out_size)
const char *name;
char *out;
unsigned long out_size;
{
    static const char hex[] = "0123456789abcdef";
    unsigned long length, i;
    if(!journal_name(name) || !out) return -1;
    length = strlen(name);
    if(out_size < length * 2 + 1) return -1;
    for(i = 0; i < length; i++) {
        unsigned char byte = (unsigned char)name[i];
        out[2 * i] = hex[byte >> 4];
        out[2 * i + 1] = hex[byte & 15];
    }
    out[2 * length] = 0;
    return 0;
}

static int journal_valid(record)
const character_save_journal *record;
{
    unsigned long name_length;
    unsigned char decoded_name[PLAYER_NAME_MAX_BYTES + 1];
    char derived_shard[CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN + 1];
    if(!record || !journal_state_name(record->state) ||
       !journal_uuid(record->command_uuid) ||
       !journal_canonical_name_hex(record->canonical_name_hex) ||
       !journal_precondition_name(record->precondition) ||
       !journal_storage_format(record->storage_format) ||
       !journal_world_id(record->world_id) ||
       !journal_legacy_shard(record->legacy_shard) ||
       !record->writer_epoch || record->writer_epoch > (uint64_t)INT64_MAX ||
       !record->writer_revision || record->writer_revision > (uint64_t)INT64_MAX)
        return 0;
    if(!journal_decode_name_hex(record->canonical_name_hex, decoded_name,
                                sizeof(decoded_name)) ||
       !journal_name_is_canonical((const char *)decoded_name) ||
       player_path_shard_from_name((const char *)decoded_name, derived_shard) != 0 ||
       strcmp(record->legacy_shard, derived_shard) != 0)
        return 0;
    name_length = strlen(record->canonical_name_hex);
    if(name_length < 2 || (name_length & 1)) return 0;
    if(record->precondition == CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING &&
       !journal_hex(record->expected_pre_hash,
                    CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN, 0)) return 0;
    if(record->precondition == CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT &&
       record->expected_pre_hash[0]) return 0;
    return journal_hex(record->post_hash, CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN, 0);
}

static int journal_build(record, state, command_uuid, canonical_name,
                          expected_pre_hash, precondition, post_hash, storage_format,
                          world_id, legacy_shard,
                          writer_epoch, writer_revision)
character_save_journal *record;
character_save_journal_state state;
const char *command_uuid;
const char *canonical_name;
const char *expected_pre_hash;
character_save_journal_precondition precondition;
const char *post_hash;
const char *storage_format;
const char *world_id;
const char *legacy_shard;
uint64_t writer_epoch;
uint64_t writer_revision;
{
    if(!record || !journal_uuid(command_uuid) || !journal_storage_format(storage_format) ||
       !journal_world_id(world_id) || !journal_legacy_shard(legacy_shard) ||
       !journal_name_is_canonical(canonical_name) ||
       strcmp(legacy_shard, "") == 0 ||
       !writer_epoch || writer_epoch > (uint64_t)INT64_MAX ||
       !writer_revision || writer_revision > (uint64_t)INT64_MAX ||
       !journal_precondition_name(precondition) ||
       !journal_hex(post_hash, CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN, 0) ||
       (precondition == CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING &&
        !journal_hex(expected_pre_hash, CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN, 0)) ||
       (precondition == CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT &&
        expected_pre_hash && expected_pre_hash[0]))
        return -1;
    memset(record, 0, sizeof(*record));
    record->state = state;
    record->precondition = precondition;
    if(journal_copy(record->command_uuid, sizeof(record->command_uuid), command_uuid) != 0 ||
       journal_encode_name(canonical_name, record->canonical_name_hex,
                           sizeof(record->canonical_name_hex)) != 0 ||
       (expected_pre_hash && journal_copy(record->expected_pre_hash,
                                          sizeof(record->expected_pre_hash),
                                          expected_pre_hash) != 0) ||
       journal_copy(record->storage_format, sizeof(record->storage_format),
                    storage_format) != 0 ||
       journal_copy(record->world_id, sizeof(record->world_id), world_id) != 0 ||
       journal_copy(record->legacy_shard, sizeof(record->legacy_shard), legacy_shard) != 0) {
        memset(record, 0, sizeof(*record));
        return -1;
    }
    if(post_hash && journal_copy(record->post_hash, sizeof(record->post_hash),
                                 post_hash) != 0) {
        memset(record, 0, sizeof(*record));
        return -1;
    }
    record->writer_epoch = writer_epoch;
    record->writer_revision = writer_revision;
    {
        char derived_shard[CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN + 1];
        if(player_path_shard_from_name(canonical_name, derived_shard) != 0 ||
           strcmp(legacy_shard, derived_shard) != 0) {
            memset(record, 0, sizeof(*record));
            return -1;
        }
    }
    return journal_valid(record) ? 0 : -1;
}

static int journal_metadata_equal(left, right)
const character_save_journal *left;
const character_save_journal *right;
{
    return left && right && !strcmp(left->command_uuid, right->command_uuid) &&
           !strcmp(left->canonical_name_hex, right->canonical_name_hex) &&
           left->precondition == right->precondition &&
           !strcmp(left->expected_pre_hash, right->expected_pre_hash) &&
           !strcmp(left->storage_format, right->storage_format) &&
           !strcmp(left->world_id, right->world_id) &&
           !strcmp(left->legacy_shard, right->legacy_shard) &&
           left->writer_epoch == right->writer_epoch &&
           left->writer_revision == right->writer_revision;
}

static int journal_payload_equal(left, right)
const character_save_journal *left;
const character_save_journal *right;
{
    return journal_metadata_equal(left, right) &&
           !strcmp(left->post_hash, right->post_hash);
}

static int journal_dir_path(out, out_size)
char *out;
unsigned long out_size;
{
    return resolve_runtime_path(CHARACTER_SAVE_JOURNAL_DIR_LEGACY, out, out_size);
}

static int journal_ensure_dir(dir)
const char *dir;
{
    struct stat st;
    if(!dir || !dir[0]) return -1;
    if(lstat(dir, &st) < 0) {
        if(errno != ENOENT || mkdir(dir, 0700) < 0) return -1;
    }
    if(lstat(dir, &st) < 0 || !S_ISDIR(st.st_mode) || S_ISLNK(st.st_mode) ||
       (st.st_mode & 0777) != 0700)
        return -1;
    return 0;
}

static int journal_open_dir(dir)
const char *dir;
{
    struct stat st;
    int fd;
    fd = open(dir, O_RDONLY | O_DIRECTORY | O_NOFOLLOW, 0);
    if(fd < 0 || fstat(fd, &st) < 0 || !S_ISDIR(st.st_mode) ||
       S_ISLNK(st.st_mode) || (st.st_mode & 0777) != 0700) {
        if(fd >= 0) close(fd);
        return -1;
    }
    return fd;
}

static int journal_leaf(command_uuid, out, out_size)
const char *command_uuid;
char *out;
unsigned long out_size;
{
    int written;
    if(!journal_uuid(command_uuid) || !out || !out_size) return -1;
    written = snprintf(out, out_size, "%s.journal", command_uuid);
    return written < 0 || (unsigned long)written >= out_size ? -1 : 0;
}

int character_save_journal_path(command_uuid, out, out_size)
const char *command_uuid;
char *out;
unsigned long out_size;
{
    char dir[CHARACTER_SAVE_JOURNAL_PATH_MAX], leaf[CHARACTER_SAVE_JOURNAL_TEMP_MAX];
    if(!out || !out_size || journal_dir_path(dir, sizeof(dir)) != 0 ||
       journal_leaf(command_uuid, leaf, sizeof(leaf)) != 0) return -1;
    if(snprintf(out, out_size, "%s/%s", dir, leaf) < 0 ||
       strlen(dir) + strlen(leaf) + 1 >= out_size) {
        out[0] = 0;
        return -1;
    }
    return 0;
}

static int journal_format(record, out, out_size)
const character_save_journal *record;
char *out;
unsigned long out_size;
{
    int written;
    if(!journal_valid(record) || !out || !out_size) return -1;
    written = snprintf(out, out_size,
        "version=1\n"
        "state=%s\n"
        "command_uuid=%s\n"
        "canonical_name_hex=%s\n"
        "expected_precondition=%s\n"
        "expected_pre_hash=%s\n"
        "post_hash=%s\n"
        "storage_format=%s\n"
        "world_id=%s\n"
        "legacy_shard=%s\n"
        "writer_epoch=%" PRIu64 "\n"
        "writer_revision=%" PRIu64 "\n",
        journal_state_name(record->state), record->command_uuid,
        record->canonical_name_hex, journal_precondition_name(record->precondition),
        record->expected_pre_hash, record->post_hash, record->storage_format,
        record->world_id, record->legacy_shard,
        record->writer_epoch,
        record->writer_revision);
    return written < 0 || (unsigned long)written >= out_size ? -1 : written;
}

static int journal_parse(text, record)
char *text;
character_save_journal *record;
{
    static const char *prefixes[] = {
        "version=", "state=", "command_uuid=", "canonical_name_hex=",
        "expected_precondition=", "expected_pre_hash=", "post_hash=", "storage_format=",
        "world_id=", "legacy_shard=", "writer_epoch=", "writer_revision="
    };
    char *line, *next;
    char version[4], state[24], precondition[10], epoch[24], revision[24];
    int i;
    if(!text || !record) return -1;
    memset(record, 0, sizeof(*record));
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    memset(precondition, 0, sizeof(precondition));
    memset(epoch, 0, sizeof(epoch));
    memset(revision, 0, sizeof(revision));
    line = text;
    for(i = 0; i < 12; i++) {
        next = strchr(line, '\n');
        if(!next) goto bad;
        *next = 0;
        if(strncmp(line, prefixes[i], strlen(prefixes[i])) != 0) goto bad;
        if(i == 0 && journal_copy(version, sizeof(version),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 1 && journal_copy(state, sizeof(state),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 2 && journal_copy(record->command_uuid,
                                  sizeof(record->command_uuid),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 3 && journal_copy(record->canonical_name_hex,
                                  sizeof(record->canonical_name_hex),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 4 && journal_copy(precondition, sizeof(precondition),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 5 && journal_copy(record->expected_pre_hash,
                                  sizeof(record->expected_pre_hash),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 6 && journal_copy(record->post_hash, sizeof(record->post_hash),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 7 && journal_copy(record->storage_format,
                                  sizeof(record->storage_format),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 8 && journal_copy(record->world_id, sizeof(record->world_id),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 9 && journal_copy(record->legacy_shard, sizeof(record->legacy_shard),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 10 && journal_copy(epoch, sizeof(epoch),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        if(i == 11 && journal_copy(revision, sizeof(revision),
                                  line + strlen(prefixes[i])) != 0) goto bad;
        line = next + 1;
    }
    if(*line || strcmp(version, "1") != 0 ||
       (record->state = journal_parse_state(state)) ==
           CHARACTER_SAVE_JOURNAL_INVALID ||
       (record->precondition = journal_parse_precondition(precondition)) == 0 ||
       !journal_uint64(epoch, &record->writer_epoch) ||
       !journal_uint64(revision, &record->writer_revision) ||
       !journal_valid(record)) goto bad;
    return 0;
bad:
    memset(record, 0, sizeof(*record));
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    memset(precondition, 0, sizeof(precondition));
    memset(epoch, 0, sizeof(epoch));
    memset(revision, 0, sizeof(revision));
    return -1;
}

static int journal_read_fd(fd, record)
int fd;
character_save_journal *record;
{
    char text[CHARACTER_SAVE_JOURNAL_TEXT_MAX];
    struct stat st;
    unsigned long count;
    int n;
    if(!record || fstat(fd, &st) < 0 || !S_ISREG(st.st_mode) ||
       (st.st_mode & 0777) != 0600) return -1;
    count = 0;
    while(count < sizeof(text) - 1) {
        n = (int)journal_test_read(fd, text + count, sizeof(text) - 1 - count);
        if(n < 0 && errno == EINTR) continue;
        if(n < 0) return -1;
        if(!n) break;
        count += (unsigned long)n;
    }
    do n = (int)journal_test_read(fd, text + count, 1); while(n < 0 && errno == EINTR);
    if(n != 0) return -1;
    text[count] = 0;
    n = journal_parse(text, record);
    memset(text, 0, sizeof(text));
    return n;
}

#define JOURNAL_READ_MISSING 1

static int journal_read_internal(command_uuid, record)
const char *command_uuid;
character_save_journal *record;
{
    char dir[CHARACTER_SAVE_JOURNAL_PATH_MAX], leaf[CHARACTER_SAVE_JOURNAL_TEMP_MAX];
    int dir_fd, fd, result;
    if(record) memset(record, 0, sizeof(*record));
    if(!record || journal_dir_path(dir, sizeof(dir)) != 0 ||
       journal_leaf(command_uuid, leaf, sizeof(leaf)) != 0) return -1;
    dir_fd = journal_open_dir(dir);
    if(dir_fd < 0) return errno == ENOENT ? JOURNAL_READ_MISSING : -1;
    fd = openat(dir_fd, leaf, O_RDONLY | O_NONBLOCK | O_NOFOLLOW, 0);
    if(fd < 0) {
        result = errno == ENOENT ? JOURNAL_READ_MISSING : -1;
        close(dir_fd);
        return result;
    }
    result = journal_read_fd(fd, record);
    if(close(fd) < 0) result = -1;
    close(dir_fd);
    return result;
}

int character_save_journal_read(command_uuid, record)
const char *command_uuid;
character_save_journal *record;
{
    return journal_read_internal(command_uuid, record) == 0 ?
           CHARACTER_SAVE_JOURNAL_OK : CHARACTER_SAVE_JOURNAL_REJECTED;
}

static int journal_write_all(fd, text, length)
int fd;
const char *text;
unsigned long length;
{
    int n;
    while(length) {
        n = (int)journal_test_write(fd, text, length);
        if(n < 0 && errno == EINTR) continue;
        if(n <= 0) return -1;
        text += n;
        length -= (unsigned long)n;
    }
    return 0;
}

static int journal_fsync_retry(fd)
int fd;
{
    int result;
    do result = fsync(fd); while(result < 0 && errno == EINTR);
    return result;
}

static int journal_parent_fsync(fd)
int fd;
{
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
    if(journal_fail_parent_fsync) {
        errno = EIO;
        return -1;
    }
#endif
    return journal_fsync_retry(fd);
}

static int journal_temp_fsync(fd)
int fd;
{
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
    if(journal_fail_temp_fsync) {
        errno = EIO;
        return -1;
    }
#endif
    return journal_fsync_retry(fd);
}

static int journal_temp_close(fd)
int fd;
{
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
    if(journal_fail_temp_close_once) {
        journal_fail_temp_close_once = 0;
        /* Exercise the POSIX close-error ownership boundary: the descriptor
         * has been released, but the caller sees an error and must not retry. */
        (void)close(fd);
        errno = EIO;
        return -1;
    }
#endif
    return close(fd);
}

static int journal_unlink_after_link(dir_fd, temp)
int dir_fd;
const char *temp;
{
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
    if(journal_fail_temp_unlink_once) {
        journal_fail_temp_unlink_once = 0;
        errno = EIO;
        return -1;
    }
#endif
    return unlinkat(dir_fd, temp, 0);
}

static int journal_write_record(record, replace)
const character_save_journal *record;
int replace;
{
    char dir[CHARACTER_SAVE_JOURNAL_PATH_MAX], leaf[CHARACTER_SAVE_JOURNAL_TEMP_MAX];
    char temp[CHARACTER_SAVE_JOURNAL_TEMP_MAX], text[CHARACTER_SAVE_JOURNAL_TEXT_MAX];
    int dir_fd, temp_fd, temp_exists, retain_temp, published, result, attempt;
    if(!record || journal_dir_path(dir, sizeof(dir)) != 0 ||
       journal_leaf(record->command_uuid, leaf, sizeof(leaf)) != 0 ||
       journal_ensure_dir(dir) != 0 || journal_format(record, text, sizeof(text)) < 0)
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    dir_fd = journal_open_dir(dir);
    if(dir_fd < 0) return CHARACTER_SAVE_JOURNAL_REJECTED;
    temp_fd = -1;
    temp_exists = 0;
    retain_temp = 0;
    published = 0;
    result = CHARACTER_SAVE_JOURNAL_REJECTED;
    for(attempt = 0; attempt < 8; attempt++) {
        if(snprintf(temp, sizeof(temp), ".save-journal.tmp.%ld.%lu",
                    (long)getpid(), journal_temp_counter++) < 0)
            break;
        temp_fd = openat(dir_fd, temp, O_WRONLY | O_CREAT | O_EXCL, 0600);
        if(temp_fd >= 0) {
            temp_exists = 1;
            break;
        }
        if(errno != EEXIST) break;
    }
    if(temp_fd < 0 || fchmod(temp_fd, 0600) < 0 ||
       journal_write_all(temp_fd, text, (unsigned long)strlen(text)) != 0 ||
       journal_temp_fsync(temp_fd) < 0) goto out;
    {
        int close_result = journal_temp_close(temp_fd);
        /* POSIX does not make retrying close(2) safe after an error: the
         * descriptor may already have been released and reused.  Relinquish
         * ownership exactly once, then reject and remove the staging name. */
        temp_fd = -1;
        if(close_result < 0) goto out;
    }
    if(replace) {
        if(renameat(dir_fd, temp, dir_fd, leaf) < 0) goto out;
        temp_exists = 0;
        published = 1;
    }
    else {
        if(linkat(dir_fd, temp, dir_fd, leaf, 0) < 0) goto out;
        published = 1;
        if(journal_unlink_after_link(dir_fd, temp) < 0) {
            /* The canonical link is visible but the staging name remains.
             * Treat this as reconciliation-required even if the directory
             * fsync below succeeds: the duplicate must be inspected. */
            retain_temp = 1;
            result = CHARACTER_SAVE_JOURNAL_RECONCILE_REQUIRED;
            (void)journal_parent_fsync(dir_fd);
            goto out;
        }
        temp_exists = 0;
    }
    if(journal_parent_fsync(dir_fd) < 0) {
        result = CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN;
        goto out;
    }
    result = CHARACTER_SAVE_JOURNAL_OK;
out:
    if(temp_fd >= 0) close(temp_fd);
    if(temp_exists && !retain_temp) unlinkat(dir_fd, temp, 0);
    if(close(dir_fd) < 0 && published && result == CHARACTER_SAVE_JOURNAL_OK)
        result = CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN;
    memset(text, 0, sizeof(text));
    return result;
}

static int journal_sync_existing(command_uuid)
const char *command_uuid;
{
    char dir[CHARACTER_SAVE_JOURNAL_PATH_MAX], leaf[CHARACTER_SAVE_JOURNAL_TEMP_MAX];
    int dir_fd, fd, result;
    struct stat st;
    if(journal_dir_path(dir, sizeof(dir)) != 0 ||
       journal_leaf(command_uuid, leaf, sizeof(leaf)) != 0)
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    dir_fd = journal_open_dir(dir);
    if(dir_fd < 0) return CHARACTER_SAVE_JOURNAL_REJECTED;
    fd = openat(dir_fd, leaf, O_RDONLY | O_NONBLOCK | O_NOFOLLOW, 0);
    if(fd < 0) {
        close(dir_fd);
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    }
    if(fstat(fd, &st) < 0 || !S_ISREG(st.st_mode) ||
       (st.st_mode & 0777) != 0600) {
        close(fd);
        close(dir_fd);
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    }
    result = journal_fsync_retry(fd);
    if(close(fd) < 0) result = -1;
    if(result == 0 && journal_parent_fsync(dir_fd) < 0)
        result = CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN;
    if(close(dir_fd) < 0 && result == 0)
        result = CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN;
    return result == 0 ? CHARACTER_SAVE_JOURNAL_OK :
           (result == CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN ?
            result : CHARACTER_SAVE_JOURNAL_REJECTED);
}

#ifdef CHARACTER_SAVE_JOURNAL_TESTING
void character_save_journal_fail_parent_fsync_for_test(enabled)
int enabled;
{
    journal_fail_parent_fsync = enabled != 0;
}

void character_save_journal_io_faults_for_test(write_eintr_once,
                                               write_short_once,
                                               read_eintr_once,
                                               read_short_once)
int write_eintr_once;
int write_short_once;
int read_eintr_once;
int read_short_once;
{
    journal_write_eintr_once = write_eintr_once != 0;
    journal_write_short_once = write_short_once != 0;
    journal_read_eintr_once = read_eintr_once != 0;
    journal_read_short_once = read_short_once != 0;
}

void character_save_journal_fail_temp_write_for_test(enabled)
int enabled;
{
    journal_fail_temp_write = enabled != 0;
}

void character_save_journal_fail_temp_fsync_for_test(enabled)
int enabled;
{
    journal_fail_temp_fsync = enabled != 0;
}

void character_save_journal_fail_temp_close_once_for_test(enabled)
int enabled;
{
    journal_fail_temp_close_once = enabled != 0;
}

void character_save_journal_fail_temp_unlink_once_for_test(enabled)
int enabled;
{
    journal_fail_temp_unlink_once = enabled != 0;
}
#endif

int character_save_journal_prepare(command_uuid, canonical_name,
                                   expected_pre_hash, precondition, post_hash,
                                   storage_format, world_id, legacy_shard,
                                   writer_epoch, writer_revision)
const char *command_uuid;
const char *canonical_name;
const char *expected_pre_hash;
character_save_journal_precondition precondition;
const char *post_hash;
const char *storage_format;
const char *world_id;
const char *legacy_shard;
uint64_t writer_epoch;
uint64_t writer_revision;
{
    character_save_journal next, prior;
    int result;
    if(journal_build(&next, CHARACTER_SAVE_JOURNAL_PREPARED, command_uuid,
                     canonical_name, expected_pre_hash, precondition, post_hash,
                     storage_format, world_id, legacy_shard,
                     writer_epoch, writer_revision) != 0)
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    result = journal_read_internal(command_uuid, &prior);
    if(result == 0) {
        if(!journal_payload_equal(&prior, &next))
            return CHARACTER_SAVE_JOURNAL_REJECTED;
        return journal_sync_existing(command_uuid);
    }
    if(result != JOURNAL_READ_MISSING) return CHARACTER_SAVE_JOURNAL_REJECTED;
    return journal_write_record(&next, 0);
}

static int journal_transition(target, prior_state, command_uuid, canonical_name,
                               expected_pre_hash, precondition, post_hash, storage_format,
                               world_id, legacy_shard,
                               writer_epoch, writer_revision)
character_save_journal_state target;
character_save_journal_state prior_state;
const char *command_uuid;
const char *canonical_name;
const char *expected_pre_hash;
character_save_journal_precondition precondition;
const char *post_hash;
const char *storage_format;
const char *world_id;
const char *legacy_shard;
uint64_t writer_epoch;
uint64_t writer_revision;
{
    character_save_journal next, prior;
    int result;
    if(journal_build(&next, target, command_uuid, canonical_name,
                     expected_pre_hash, precondition, post_hash, storage_format,
                     world_id, legacy_shard,
                     writer_epoch, writer_revision) != 0)
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    result = journal_read_internal(command_uuid, &prior);
    if(result != 0 || !journal_payload_equal(&prior, &next))
        return CHARACTER_SAVE_JOURNAL_REJECTED;
    if(prior.state == target)
        return journal_sync_existing(command_uuid);
    if(prior.state > target)
        return journal_sync_existing(command_uuid);
    if(prior.state != prior_state) return CHARACTER_SAVE_JOURNAL_REJECTED;
    return journal_write_record(&next, 1);
}

int character_save_journal_mark_legacy_published(
    command_uuid, canonical_name, expected_pre_hash, precondition, post_hash,
    storage_format, world_id, legacy_shard, writer_epoch, writer_revision)
const char *command_uuid;
const char *canonical_name;
const char *expected_pre_hash;
character_save_journal_precondition precondition;
const char *post_hash;
const char *storage_format;
const char *world_id;
const char *legacy_shard;
uint64_t writer_epoch;
uint64_t writer_revision;
{
    return journal_transition(CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED,
                               CHARACTER_SAVE_JOURNAL_PREPARED, command_uuid,
                               canonical_name, expected_pre_hash, precondition, post_hash,
                               storage_format, world_id, legacy_shard,
                               writer_epoch, writer_revision);
}

int character_save_journal_mark_db_acked(
    command_uuid, canonical_name, expected_pre_hash, precondition, post_hash,
    storage_format, world_id, legacy_shard, writer_epoch, writer_revision)
const char *command_uuid;
const char *canonical_name;
const char *expected_pre_hash;
character_save_journal_precondition precondition;
const char *post_hash;
const char *storage_format;
const char *world_id;
const char *legacy_shard;
uint64_t writer_epoch;
uint64_t writer_revision;
{
    return journal_transition(CHARACTER_SAVE_JOURNAL_DB_ACKED,
                               CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED,
                               command_uuid, canonical_name, expected_pre_hash, precondition,
                               post_hash, storage_format, world_id, legacy_shard, writer_epoch,
                               writer_revision);
}
