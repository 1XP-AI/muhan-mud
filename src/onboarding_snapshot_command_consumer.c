/* Local-only activation-command reservation evidence; no M3/runtime wiring. */
#include "onboarding_snapshot_command_consumer.h"

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
#ifndef O_CLOEXEC
#define O_CLOEXEC 0
#endif

#define OSCC_TEXT_MAX 320U
#define OSCC_NAME_MAX 80U

static unsigned long oscc_bounded(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1U;
    for(length=0; length<=limit; length++) if(!value[length]) return length;
    return limit + 1U;
}

static int oscc_hex(value)
char value;
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int oscc_uuid(value)
const char *value;
{
    unsigned long index;
    if(oscc_bounded(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(index=0; index<ONBOARDING_ADMISSION_UUID_LEN; index++)
        if(index == 8U || index == 13U || index == 18U || index == 23U) {
            if(value[index] != '-') return 0;
        } else if(!oscc_hex(value[index])) return 0;
    return 1;
}

static int oscc_copy(destination, capacity, source)
char *destination;
unsigned long capacity;
const char *source;
{
    unsigned long length;
    if(!destination || !capacity || !source) return -1;
    length=oscc_bounded(source,capacity-1U);
    if(length >= capacity) return -1;
    memcpy(destination,source,length); destination[length]=0;
    return 0;
}

static const char *oscc_mode_name(mode)
onboarding_activation_binding_mode mode;
{
    if(mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) return "provision";
    if(mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) return "claim";
    return 0;
}

static onboarding_activation_binding_mode oscc_mode(value)
const char *value;
{
    if(value && !strcmp(value,"provision")) return ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    if(value && !strcmp(value,"claim")) return ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM;
    return ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
}

static int oscc_activation_valid(activation)
const onboarding_activation_binding *activation;
{
    return activation && oscc_uuid(activation->actor_user_id) &&
        oscc_uuid(activation->correlation_id) && oscc_uuid(activation->character_id) &&
        oscc_mode_name(activation->mode) && oscc_uuid(activation->command_id);
}

static int oscc_reservation_valid(reservation)
const onboarding_snapshot_command_reservation *reservation;
{
    return reservation &&
        reservation->state == ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED &&
        oscc_activation_valid(&reservation->activation);
}

static int oscc_name(command_id, out, out_size)
const char *command_id;
char *out;
unsigned long out_size;
{
    int length;
    if(!oscc_uuid(command_id) || !out || !out_size) return -1;
    length=snprintf(out,out_size,"%s.reservation",command_id);
    return length == 48 ? 0:-1;
}

static int oscc_temp_name(command_id, out, out_size)
const char *command_id;
char *out;
unsigned long out_size;
{
    int length;
    if(!oscc_uuid(command_id) || !out || !out_size) return -1;
    length=snprintf(out,out_size,".%s.reservation.tmp",command_id);
    return length == 53 ? 0:-1;
}

static int oscc_dup_directory(directory_fd)
int directory_fd;
{
    struct stat status;
    int duplicate;
    if(directory_fd < 0) return -1;
    duplicate=fcntl(directory_fd,F_DUPFD_CLOEXEC,3);
    if(duplicate < 0) return -1;
    if(fstat(duplicate,&status) || !S_ISDIR(status.st_mode) ||
       status.st_uid != geteuid() || (status.st_mode & 0777) != 0700) {
        close(duplicate); return -1;
    }
    return duplicate;
}

static int oscc_file_safe(fd, directory_status, record_status)
int fd;
const struct stat *directory_status;
struct stat *record_status;
{
    struct stat status;
    if(!directory_status || fstat(fd,&status) || !S_ISREG(status.st_mode) ||
       status.st_uid != directory_status->st_uid || (status.st_mode & 0777) != 0600 ||
       status.st_size < 0 || (unsigned long)status.st_size >= OSCC_TEXT_MAX) return 0;
    if(record_status) *record_status=status;
    return 1;
}

static int oscc_write_all(fd, bytes, length)
int fd;
const char *bytes;
unsigned long length;
{
    int count;
    while(length) {
        count=write(fd,bytes,length);
        if(count < 0 && errno == EINTR) continue;
        if(count <= 0) return -1;
        bytes += count; length -= (unsigned long)count;
    }
    return 0;
}

static int oscc_format(reservation, out, out_size)
const onboarding_snapshot_command_reservation *reservation;
char *out;
unsigned long out_size;
{
    int length;
    if(!oscc_reservation_valid(reservation) || !out || !out_size) return -1;
    length=snprintf(out,out_size,
        "version=1\nstate=reserved\nactor_user_id=%s\ncorrelation_id=%s\n"
        "character_id=%s\nmode=%s\ncommand_id=%s\n",
        reservation->activation.actor_user_id,reservation->activation.correlation_id,
        reservation->activation.character_id,oscc_mode_name(reservation->activation.mode),
        reservation->activation.command_id);
    return length < 0 || (unsigned long)length >= out_size ? -1:length;
}

static int oscc_line(line, prefix, out, out_size)
char *line;
const char *prefix;
char *out;
unsigned long out_size;
{
    unsigned long prefix_size;
    if(!line || !prefix || !out) return -1;
    prefix_size=(unsigned long)strlen(prefix);
    if(strncmp(line,prefix,prefix_size)) return -1;
    return oscc_copy(out,out_size,line+prefix_size);
}

static int oscc_parse(text, reservation)
char *text;
onboarding_snapshot_command_reservation *reservation;
{
    static const char *prefixes[7] = {
        "version=", "state=", "actor_user_id=", "correlation_id=",
        "character_id=", "mode=", "command_id="
    };
    char version[4], state[16], mode[16], *line, *next;
    int index;
    if(!text || !reservation) return -1;
    memset(reservation,0,sizeof(*reservation)); memset(version,0,sizeof(version));
    memset(state,0,sizeof(state)); memset(mode,0,sizeof(mode)); line=text;
    for(index=0; index<7; index++) {
        next=strchr(line,'\n'); if(!next) goto bad;
        *next=0;
        if(index == 0 && oscc_line(line,prefixes[index],version,sizeof(version))) goto bad;
        if(index == 1 && oscc_line(line,prefixes[index],state,sizeof(state))) goto bad;
        if(index == 2 && oscc_line(line,prefixes[index],reservation->activation.actor_user_id,
            sizeof(reservation->activation.actor_user_id))) goto bad;
        if(index == 3 && oscc_line(line,prefixes[index],reservation->activation.correlation_id,
            sizeof(reservation->activation.correlation_id))) goto bad;
        if(index == 4 && oscc_line(line,prefixes[index],reservation->activation.character_id,
            sizeof(reservation->activation.character_id))) goto bad;
        if(index == 5 && oscc_line(line,prefixes[index],mode,sizeof(mode))) goto bad;
        if(index == 6 && oscc_line(line,prefixes[index],reservation->activation.command_id,
            sizeof(reservation->activation.command_id))) goto bad;
        line=next+1;
    }
    if(*line || strcmp(version,"1") || strcmp(state,"reserved")) goto bad;
    reservation->state=ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    reservation->activation.mode=oscc_mode(mode);
    if(!oscc_reservation_valid(reservation)) goto bad;
    memset(version,0,sizeof(version)); memset(state,0,sizeof(state)); memset(mode,0,sizeof(mode));
    return 0;
bad:
    memset(reservation,0,sizeof(*reservation)); memset(version,0,sizeof(version));
    memset(state,0,sizeof(state)); memset(mode,0,sizeof(mode)); return -1;
}

static onboarding_snapshot_command_consumer_result oscc_read_at(directory, name,
    reservation, record_status)
int directory;
const char *name;
onboarding_snapshot_command_reservation *reservation;
struct stat *record_status;
{
    char text[OSCC_TEXT_MAX];
    struct stat directory_status;
    int fd, count, extra, result;
    fd=-1; result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
    memset(text,0,sizeof(text));
    if(record_status) memset(record_status,0,sizeof(*record_status));
    if(directory < 0 || !name || !reservation || fstat(directory,&directory_status)) goto out;
    fd=openat(directory,name,O_RDONLY|O_BINARY|O_NOFOLLOW|O_CLOEXEC);
    if(fd < 0) {
        result=errno == ENOENT ? ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE:
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        goto out;
    }
    if(!oscc_file_safe(fd,&directory_status,record_status)) {
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT; goto out;
    }
    count=read(fd,text,sizeof(text)-1U);
    if(count < 0) goto out;
    text[count]=0; extra=read(fd,text+count,1);
    if(extra != 0 || close(fd)) goto out;
    fd=-1;
    result=oscc_parse(text,reservation) ? ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT:
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED;
out:
    if(fd >= 0) close(fd);
    memset(text,0,sizeof(text));
    return result;
}

onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_read(reservation_directory_fd, command_id, reservation)
int reservation_directory_fd;
const char *command_id;
onboarding_snapshot_command_reservation *reservation;
{
    char name[OSCC_NAME_MAX];
    int directory;
    onboarding_snapshot_command_consumer_result result;
    if(reservation) memset(reservation,0,sizeof(*reservation));
    if(!reservation || oscc_name(command_id,name,sizeof(name)))
        return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID;
    directory=oscc_dup_directory(reservation_directory_fd);
    if(directory < 0) return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
    {
        struct stat status;
        result=oscc_read_at(directory,name,reservation,&status);
        if(result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && status.st_nlink != 1) {
            memset(reservation,0,sizeof(*reservation));
            result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT;
        }
    }
    if(close(directory) && result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED)
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
    if(result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
       strcmp(command_id,reservation->activation.command_id)) {
        memset(reservation,0,sizeof(*reservation));
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT;
    }
    return result;
}

static int oscc_same(left, right)
const onboarding_snapshot_command_reservation *left;
const onboarding_snapshot_command_reservation *right;
{
    return oscc_reservation_valid(left) && oscc_reservation_valid(right) &&
        !strcmp(left->activation.actor_user_id,right->activation.actor_user_id) &&
        !strcmp(left->activation.correlation_id,right->activation.correlation_id) &&
        !strcmp(left->activation.character_id,right->activation.character_id) &&
        left->activation.mode == right->activation.mode &&
        !strcmp(left->activation.command_id,right->activation.command_id);
}

/* The temporary name is a deterministic PENDING state.  It may be removed
 * only after proving it is either the one safe singleton to publish, or the
 * second name of the same safe inode which linkat already published. */
static onboarding_snapshot_command_consumer_result oscc_resume(directory, name,
    temporary, expected, pending_result)
int directory;
const char *name;
const char *temporary;
const onboarding_snapshot_command_reservation *expected;
onboarding_snapshot_command_consumer_result pending_result;
{
    onboarding_snapshot_command_reservation final, pending;
    onboarding_snapshot_command_consumer_result final_result, pending_result_read;
    struct stat final_status, pending_status;
    int same_inode;
    memset(&final,0,sizeof(final)); memset(&pending,0,sizeof(pending));
    memset(&final_status,0,sizeof(final_status)); memset(&pending_status,0,sizeof(pending_status));
    final_result=oscc_read_at(directory,name,&final,&final_status);
    pending_result_read=oscc_read_at(directory,temporary,&pending,&pending_status);
    if(final_result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE) {
        if(pending_result_read == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE)
            return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE;
        if(pending_result_read != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED ||
           pending_status.st_nlink != 1) return pending_result_read ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED ?
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT:pending_result_read;
        if(!oscc_same(expected,&pending)) return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CONFLICT;
        if(linkat(directory,temporary,directory,name,0))
            return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        /* Preserve a durable final before removing the only other name. */
        if(fsync(directory)) return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        if(unlinkat(directory,temporary,0) || fsync(directory))
            return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        return pending_result;
    }
    if(final_result != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED) return final_result;
    if(final_status.st_nlink == 1) {
        if(pending_result_read != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE)
            return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT;
        return oscc_same(expected,&final) ? ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY:
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CONFLICT;
    }
    same_inode=final_status.st_dev == pending_status.st_dev &&
        final_status.st_ino == pending_status.st_ino;
    if(final_status.st_nlink != 2 || pending_result_read !=
       ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED || pending_status.st_nlink != 2 ||
       !same_inode || !oscc_same(&final,&pending))
        return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT;
    if(!oscc_same(expected,&final)) return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CONFLICT;
    if(unlinkat(directory,temporary,0) || fsync(directory))
        return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
    return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY;
}

onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_reserve(reservation_directory_fd, command_id,
    expected_character_id, expected_mode, expected_correlation_id)
int reservation_directory_fd;
const char *command_id;
const char *expected_character_id;
onboarding_activation_binding_mode expected_mode;
const char *expected_correlation_id;
{
    onboarding_activation_binding source;
    onboarding_snapshot_command_reservation next;
    onboarding_snapshot_command_consumer_result result;
    char name[OSCC_NAME_MAX], temporary[OSCC_NAME_MAX], text[OSCC_TEXT_MAX];
    int directory, fd, text_length;
    memset(&source,0,sizeof(source)); memset(&next,0,sizeof(next));
    memset(temporary,0,sizeof(temporary)); memset(text,0,sizeof(text)); directory=-1; fd=-1;
    if(!oscc_uuid(command_id) || !oscc_uuid(expected_character_id) ||
       !oscc_uuid(expected_correlation_id) || !oscc_mode_name(expected_mode)) {
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID; goto out;
    }
    errno=0;
    if(onboarding_activation_binding_read(command_id,&source)) {
        result=errno == ENOENT ? ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE:
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        goto out;
    }
    if(!oscc_activation_valid(&source) || strcmp(source.command_id,command_id)) {
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT; goto out;
    }
    if(strcmp(source.character_id,expected_character_id) ||
       strcmp(source.correlation_id,expected_correlation_id) || source.mode != expected_mode) {
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH; goto out;
    }
    next.state=ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    memcpy(&next.activation,&source,sizeof(source));
    if(oscc_name(command_id,name,sizeof(name)) ||
       oscc_temp_name(command_id,temporary,sizeof(temporary)) ||
       (text_length=oscc_format(&next,text,sizeof(text))) < 0) {
        result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID; goto out;
    }
    directory=oscc_dup_directory(reservation_directory_fd);
    if(directory < 0) { result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR; goto out; }
    result=oscc_resume(directory,name,temporary,&next,
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY);
    if(result != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE) goto out;
    fd=openat(directory,temporary,O_WRONLY|O_CREAT|O_EXCL|O_BINARY|O_NOFOLLOW|O_CLOEXEC,0600);
    if(fd < 0) {
        if(errno == EEXIST) result=oscc_resume(directory,name,temporary,&next,
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY);
        else result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
        goto out;
    }
    if(fchmod(fd,0600) || oscc_write_all(fd,text,(unsigned long)text_length) || fsync(fd) ||
       close(fd)) { result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR; goto out; }
    fd=-1;
    /* Make the PENDING directory entry durable before publishing it. */
    if(fsync(directory)) { result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR; goto out; }
    result=oscc_resume(directory,name,temporary,&next,
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED);
out:
    if(fd >= 0) close(fd);
    if(directory >= 0) close(directory);
    memset(&source,0,sizeof(source)); memset(&next,0,sizeof(next));
    memset(text,0,sizeof(text));
    return result;
}
