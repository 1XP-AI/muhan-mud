#include "onboarding_receipt.h"
#include "mtype.h"
#include "resource_path.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
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

#define ONBOARDING_RECEIPT_DIR_LEGACY MUDHOME "/onboarding-receipts"
#define ONBOARDING_RECEIPT_PATH_MAX 1024
#define ONBOARDING_RECEIPT_TEXT_MAX 640

static unsigned long or_bounded_strlen(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1;
    for(length=0; length<=limit; length++)
        if(value[length] == 0) return length;
    return limit + 1;
}

static int or_lower_hex(c)
char c;
{
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
}

static int or_hex(value, expected_length)
const char *value;
unsigned long expected_length;
{
    unsigned long i;
    if(!value || or_bounded_strlen(value, expected_length) != expected_length)
        return 0;
    for(i=0; i<expected_length; i++)
        if(!or_lower_hex(value[i])) return 0;
    return 1;
}

static int or_uuid(value)
const char *value;
{
    int i;
    if(!value || or_bounded_strlen(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(i=0; i<ONBOARDING_ADMISSION_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(value[i] != '-') return 0;
        }
        else if(!or_lower_hex(value[i])) return 0;
    }
    return 1;
}

static int or_copy(destination, destination_size, value)
char *destination;
unsigned long destination_size;
const char *value;
{
    unsigned long length;
    if(!destination || !destination_size || !value) return -1;
    length = or_bounded_strlen(value, destination_size - 1);
    if(length >= destination_size) return -1;
    memcpy(destination, value, length);
    destination[length] = 0;
    return 0;
}

static int or_storage_format(value)
const char *value;
{
    unsigned long i, length;
    if(!value) return 0;
    length = or_bounded_strlen(value, ONBOARDING_RECEIPT_STORAGE_FORMAT_MAX);
    if(!length || length > ONBOARDING_RECEIPT_STORAGE_FORMAT_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(i=1; i<length; i++)
        if(!((value[i] >= 'a' && value[i] <= 'z') ||
             (value[i] >= '0' && value[i] <= '9') || value[i] == '-' ||
             value[i] == '_' || value[i] == '.')) return 0;
    return 1;
}

static const char *or_state_name(state)
onboarding_receipt_state state;
{
    switch(state) {
    case ONBOARDING_RECEIPT_PENDING: return "pending";
    case ONBOARDING_RECEIPT_SAVED: return "saved";
    case ONBOARDING_RECEIPT_COMMITTED: return "committed";
    default: return 0;
    }
}

static onboarding_receipt_state or_parse_state(value)
const char *value;
{
    if(!value) return ONBOARDING_RECEIPT_INVALID;
    if(!strcmp(value, "pending")) return ONBOARDING_RECEIPT_PENDING;
    if(!strcmp(value, "saved")) return ONBOARDING_RECEIPT_SAVED;
    if(!strcmp(value, "committed")) return ONBOARDING_RECEIPT_COMMITTED;
    return ONBOARDING_RECEIPT_INVALID;
}

static int or_name_hex(name, out, out_size)
const char *name;
char *out;
unsigned long out_size;
{
    static const char hex[] = "0123456789abcdef";
    unsigned long i, length;

    if(!name || !out) return -1;
    length = or_bounded_strlen(name, ONBOARDING_RECEIPT_NAME_HEX_MAX / 2);
    if(!length || length > ONBOARDING_RECEIPT_NAME_HEX_MAX / 2 ||
       out_size < length * 2 + 1) return -1;
    for(i=0; i<length; i++) {
        unsigned char byte = (unsigned char)name[i];
        if(!byte) return -1;
        out[2*i] = hex[byte >> 4];
        out[2*i + 1] = hex[byte & 15];
    }
    out[length * 2] = 0;
    return 0;
}

static int or_receipt_valid(receipt)
const onboarding_receipt *receipt;
{
    unsigned long name_length;
    if(!receipt || !or_state_name(receipt->state) ||
       !or_uuid(receipt->actor_id) || !or_uuid(receipt->correlation_id) ||
       !or_uuid(receipt->character_id) || !or_storage_format(receipt->storage_format))
        return 0;
    name_length = or_bounded_strlen(receipt->canonical_name_hex,
                                    ONBOARDING_RECEIPT_NAME_HEX_MAX);
    if(name_length < 2 || name_length > ONBOARDING_RECEIPT_NAME_HEX_MAX ||
       (name_length & 1) || !or_hex(receipt->canonical_name_hex, name_length))
        return 0;
    if(receipt->state == ONBOARDING_RECEIPT_PENDING)
        return receipt->file_sha256[0] == 0;
    return or_hex(receipt->file_sha256, ONBOARDING_ADMISSION_SHA256_HEX_LEN);
}

static int or_prepare(receipt, state, actor_id, correlation_id, character_id,
                      canonical_name, storage_format, file_sha256)
onboarding_receipt *receipt;
onboarding_receipt_state state;
const char *actor_id;
const char *correlation_id;
const char *character_id;
const char *canonical_name;
const char *storage_format;
const char *file_sha256;
{
    if(!receipt || !or_uuid(actor_id) || !or_uuid(correlation_id) ||
       !or_uuid(character_id) || !or_storage_format(storage_format)) return -1;
    memset(receipt, 0, sizeof(*receipt));
    receipt->state = state;
    if(or_copy(receipt->actor_id, sizeof(receipt->actor_id), actor_id) != 0 ||
       or_copy(receipt->correlation_id, sizeof(receipt->correlation_id), correlation_id) != 0 ||
       or_copy(receipt->character_id, sizeof(receipt->character_id), character_id) != 0 ||
       or_name_hex(canonical_name, receipt->canonical_name_hex,
                   sizeof(receipt->canonical_name_hex)) != 0 ||
       or_copy(receipt->storage_format, sizeof(receipt->storage_format),
               storage_format) != 0) {
        memset(receipt, 0, sizeof(*receipt));
        return -1;
    }
    if(state != ONBOARDING_RECEIPT_PENDING &&
       (!file_sha256 || !or_hex(file_sha256,
                                ONBOARDING_ADMISSION_SHA256_HEX_LEN) ||
        or_copy(receipt->file_sha256, sizeof(receipt->file_sha256),
                file_sha256) != 0)) {
        memset(receipt, 0, sizeof(*receipt));
        return -1;
    }
    if(!or_receipt_valid(receipt)) {
        memset(receipt, 0, sizeof(*receipt));
        return -1;
    }
    return 0;
}

static int or_dir_path(out, out_size)
char *out;
unsigned long out_size;
{
    return resolve_runtime_path(ONBOARDING_RECEIPT_DIR_LEGACY, out, out_size);
}

static int or_ensure_dir(dir)
const char *dir;
{
    struct stat st;
    if(!dir || !dir[0]) return -1;
    if(lstat(dir, &st) < 0) {
        if(errno != ENOENT || mkdir(dir, 0700) < 0) return -1;
        if(lstat(dir, &st) < 0) return -1;
    }
    if(!S_ISDIR(st.st_mode) || S_ISLNK(st.st_mode) || chmod(dir, 0700) < 0)
        return -1;
    if(lstat(dir, &st) < 0 || !S_ISDIR(st.st_mode) || S_ISLNK(st.st_mode) ||
       (st.st_mode & 0777) != 0700) return -1;
    return 0;
}

int onboarding_receipt_path(correlation_id, out, out_size)
const char *correlation_id;
char *out;
unsigned long out_size;
{
    char dir[ONBOARDING_RECEIPT_PATH_MAX];
    int written;
    if(!or_uuid(correlation_id) || !out || !out_size ||
       or_dir_path(dir, sizeof(dir)) != 0) return -1;
    written = snprintf(out, out_size, "%s/%s.receipt", dir, correlation_id);
    if(written < 0 || (unsigned long)written >= out_size) {
        out[0] = 0;
        return -1;
    }
    return 0;
}

static int or_open_directory(dir)
const char *dir;
{
    return open(dir, O_RDONLY | O_BINARY | O_DIRECTORY | O_NOFOLLOW, 0);
}

static int or_write_all(fd, data, length)
int fd;
const char *data;
unsigned long length;
{
    int written;
    while(length) {
        written = write(fd, data, length);
        if(written <= 0) return -1;
        data += written;
        length -= (unsigned long)written;
    }
    return 0;
}

static int or_format(receipt, out, out_size)
const onboarding_receipt *receipt;
char *out;
unsigned long out_size;
{
    int written;
    const char *state;
    if(!or_receipt_valid(receipt) || !out || !out_size) return -1;
    state = or_state_name(receipt->state);
    written = snprintf(out, out_size,
        "version=1\n"
        "state=%s\n"
        "actor_uuid=%s\n"
        "correlation_uuid=%s\n"
        "character_uuid=%s\n"
        "canonical_name_hex=%s\n"
        "storage_format=%s\n"
        "saved_file_sha256=%s\n",
        state, receipt->actor_id, receipt->correlation_id, receipt->character_id,
        receipt->canonical_name_hex, receipt->storage_format, receipt->file_sha256);
    return written < 0 || (unsigned long)written >= out_size ? -1 : written;
}

/* A create uses temp+fsync+hard-link+directory-fsync so it cannot replace a
 * stale receipt.  State transitions use temp+fsync+rename+directory-fsync.
 * Both make a crash expose either the old complete record or the new complete
 * record, never a partially written one. */
static int or_write_record(receipt, replace)
const onboarding_receipt *receipt;
int replace;
{
    char dir[ONBOARDING_RECEIPT_PATH_MAX];
    char path[ONBOARDING_RECEIPT_PATH_MAX];
    char temp[ONBOARDING_RECEIPT_PATH_MAX];
    char text[ONBOARDING_RECEIPT_TEXT_MAX];
    int dir_fd, temp_fd, text_length, result;

    result = -1;
    dir_fd = -1;
    temp_fd = -1;
    memset(temp, 0, sizeof(temp));
    if(!or_receipt_valid(receipt) ||
       or_dir_path(dir, sizeof(dir)) != 0 || or_ensure_dir(dir) != 0 ||
       onboarding_receipt_path(receipt->correlation_id, path, sizeof(path)) != 0 ||
       snprintf(temp, sizeof(temp), "%s/.receipt.tmp.XXXXXX", dir) >=
           (int)sizeof(temp) ||
       (text_length = or_format(receipt, text, sizeof(text))) < 0) goto out;
    dir_fd = or_open_directory(dir);
    if(dir_fd < 0) goto out;
    temp_fd = mkstemp(temp);
    if(temp_fd < 0 || fchmod(temp_fd, 0600) < 0 ||
       or_write_all(temp_fd, text, (unsigned long)text_length) != 0 ||
       fsync(temp_fd) < 0 || close(temp_fd) < 0) goto out;
    temp_fd = -1;
    if(replace) {
        if(rename(temp, path) < 0) goto out;
    }
    else {
        if(link(temp, path) < 0) goto out;
        if(unlink(temp) < 0) goto out;
    }
    temp[0] = 0;
    if(fsync(dir_fd) < 0) goto out;
    result = 0;
out:
    if(temp_fd >= 0) close(temp_fd);
    if(temp[0]) unlink(temp);
    if(dir_fd >= 0) close(dir_fd);
    memset(text, 0, sizeof(text));
    return result;
}

static int or_parse_line(line, prefix, destination, destination_size)
char *line;
const char *prefix;
char *destination;
unsigned long destination_size;
{
    unsigned long prefix_length;
    if(!line || !prefix || !destination) return -1;
    prefix_length = (unsigned long)strlen(prefix);
    if(strncmp(line, prefix, prefix_length) != 0) return -1;
    return or_copy(destination, destination_size, line + prefix_length);
}

static int or_parse(text, receipt)
char *text;
onboarding_receipt *receipt;
{
    static const char *prefixes[8] = {
        "version=", "state=", "actor_uuid=", "correlation_uuid=",
        "character_uuid=", "canonical_name_hex=", "storage_format=",
        "saved_file_sha256="
    };
    char *line, *next;
    char version[4], state[16];
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
        case 0: if(or_parse_line(line, prefixes[i], version, sizeof(version)) != 0) goto bad; break;
        case 1: if(or_parse_line(line, prefixes[i], state, sizeof(state)) != 0) goto bad; break;
        case 2: if(or_parse_line(line, prefixes[i], receipt->actor_id, sizeof(receipt->actor_id)) != 0) goto bad; break;
        case 3: if(or_parse_line(line, prefixes[i], receipt->correlation_id, sizeof(receipt->correlation_id)) != 0) goto bad; break;
        case 4: if(or_parse_line(line, prefixes[i], receipt->character_id, sizeof(receipt->character_id)) != 0) goto bad; break;
        case 5: if(or_parse_line(line, prefixes[i], receipt->canonical_name_hex, sizeof(receipt->canonical_name_hex)) != 0) goto bad; break;
        case 6: if(or_parse_line(line, prefixes[i], receipt->storage_format, sizeof(receipt->storage_format)) != 0) goto bad; break;
        case 7: if(or_parse_line(line, prefixes[i], receipt->file_sha256, sizeof(receipt->file_sha256)) != 0) goto bad; break;
        }
        line = next + 1;
    }
    if(*line || strcmp(version, "1") != 0) goto bad;
    receipt->state = or_parse_state(state);
    if(!or_receipt_valid(receipt)) goto bad;
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    return 0;
bad:
    memset(receipt, 0, sizeof(*receipt));
    memset(version, 0, sizeof(version));
    memset(state, 0, sizeof(state));
    return -1;
}

static int or_read_path(path, receipt)
const char *path;
onboarding_receipt *receipt;
{
    char text[ONBOARDING_RECEIPT_TEXT_MAX];
    struct stat st;
    int fd, count, extra, result;

    result = -1;
    fd = -1;
    memset(text, 0, sizeof(text));
    if(!path || !receipt) goto out;
    fd = open(path, O_RDONLY | O_BINARY | O_NOFOLLOW, 0);
    if(fd < 0 || fstat(fd, &st) < 0 || !S_ISREG(st.st_mode) ||
       (st.st_mode & 0077) != 0) goto out;
    count = read(fd, text, sizeof(text) - 1);
    if(count < 0) goto out;
    text[count] = 0;
    extra = read(fd, text + count, 1);
    if(extra != 0 || close(fd) < 0) goto out;
    fd = -1;
    result = or_parse(text, receipt);
out:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    return result;
}

int onboarding_receipt_read(correlation_id, receipt)
const char *correlation_id;
onboarding_receipt *receipt;
{
    char path[ONBOARDING_RECEIPT_PATH_MAX];
    if(receipt) memset(receipt, 0, sizeof(*receipt));
    if(!receipt || onboarding_receipt_path(correlation_id, path, sizeof(path)) != 0)
        return -1;
    if(or_read_path(path, receipt) != 0 || strcmp(receipt->correlation_id,
                                                   correlation_id) != 0) {
        memset(receipt, 0, sizeof(*receipt));
        return -1;
    }
    return 0;
}

static int or_same_metadata(left, right)
const onboarding_receipt *left;
const onboarding_receipt *right;
{
    return left && right && !strcmp(left->actor_id, right->actor_id) &&
           !strcmp(left->correlation_id, right->correlation_id) &&
           !strcmp(left->character_id, right->character_id) &&
           !strcmp(left->canonical_name_hex, right->canonical_name_hex) &&
           !strcmp(left->storage_format, right->storage_format);
}

int onboarding_receipt_write_pending(actor_id, correlation_id, character_id,
                                     canonical_name, storage_format)
const char *actor_id;
const char *correlation_id;
const char *character_id;
const char *canonical_name;
const char *storage_format;
{
    onboarding_receipt receipt, prior;
    int result;
    memset(&receipt, 0, sizeof(receipt));
    memset(&prior, 0, sizeof(prior));
    result = or_prepare(&receipt, ONBOARDING_RECEIPT_PENDING, actor_id,
                        correlation_id, character_id, canonical_name,
                        storage_format, 0);
    if(result == 0) result = or_write_record(&receipt, 0);
    /* A dropped Gateway/C connection may restart the same wizard after the
     * database has already reserved its exact correlation. Reuse only an
     * byte-equivalent pending provenance; any different or later state stays
     * fail-closed and can be handled only by reconciliation. */
    if(result != 0 &&
       onboarding_receipt_read(correlation_id, &prior) == 0 &&
       prior.state == ONBOARDING_RECEIPT_PENDING &&
       or_same_metadata(&prior, &receipt))
        result = 0;
    memset(&receipt, 0, sizeof(receipt));
    memset(&prior, 0, sizeof(prior));
    return result;
}

static int or_transition(target_state, prior_state, actor_id, correlation_id,
                         character_id, canonical_name, storage_format,
                         file_sha256)
onboarding_receipt_state target_state;
onboarding_receipt_state prior_state;
const char *actor_id;
const char *correlation_id;
const char *character_id;
const char *canonical_name;
const char *storage_format;
const char *file_sha256;
{
    onboarding_receipt next, prior;
    int result;
    memset(&next, 0, sizeof(next));
    memset(&prior, 0, sizeof(prior));
    result = or_prepare(&next, target_state, actor_id, correlation_id,
                        character_id, canonical_name, storage_format,
                        file_sha256);
    if(result == 0 && onboarding_receipt_read(correlation_id, &prior) != 0)
        result = -1;
    if(result == 0 && (prior.state != prior_state || !or_same_metadata(&prior, &next) ||
                       (prior_state != ONBOARDING_RECEIPT_PENDING &&
                        strcmp(prior.file_sha256, next.file_sha256) != 0)))
        result = -1;
    if(result == 0) result = or_write_record(&next, 1);
    memset(&next, 0, sizeof(next));
    memset(&prior, 0, sizeof(prior));
    return result;
}

int onboarding_receipt_mark_saved(actor_id, correlation_id, character_id,
                                   canonical_name, storage_format, file_sha256)
const char *actor_id;
const char *correlation_id;
const char *character_id;
const char *canonical_name;
const char *storage_format;
const char *file_sha256;
{
    return or_transition(ONBOARDING_RECEIPT_SAVED, ONBOARDING_RECEIPT_PENDING,
                         actor_id, correlation_id, character_id, canonical_name,
                         storage_format, file_sha256);
}

int onboarding_receipt_mark_committed(actor_id, correlation_id, character_id,
                                      canonical_name, storage_format, file_sha256)
const char *actor_id;
const char *correlation_id;
const char *character_id;
const char *canonical_name;
const char *storage_format;
const char *file_sha256;
{
    return or_transition(ONBOARDING_RECEIPT_COMMITTED, ONBOARDING_RECEIPT_SAVED,
                         actor_id, correlation_id, character_id, canonical_name,
                         storage_format, file_sha256);
}
