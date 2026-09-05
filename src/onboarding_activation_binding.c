/* Durable MUD1O ACTIVATED -> ACTIVE binding; no M3 command consumption. */
#include "onboarding_activation_binding.h"
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
#ifndef O_CLOEXEC
#define O_CLOEXEC 0
#endif

#define OAB_DIR_LEGACY MUDHOME "/onboarding-activation-bindings"
#define OAB_PATH_MAX 1024U
#define OAB_TEXT_MAX 256U
#define OAB_NAME_MAX 64U

static unsigned long oab_bounded(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1U;
    for(length=0; length<=limit; length++) if(!value[length]) return length;
    return limit + 1U;
}

static int oab_hex(value)
char value;
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int oab_uuid(value)
const char *value;
{
    unsigned long i;
    if(oab_bounded(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(i=0; i<ONBOARDING_ADMISSION_UUID_LEN; i++)
        if(i == 8U || i == 13U || i == 18U || i == 23U) {
            if(value[i] != '-') return 0;
        } else if(!oab_hex(value[i])) return 0;
    return 1;
}

static int oab_copy(destination, capacity, source)
char *destination;
unsigned long capacity;
const char *source;
{
    unsigned long length;
    if(!destination || !capacity || !source) return -1;
    length=oab_bounded(source, capacity-1U);
    if(length >= capacity) return -1;
    memcpy(destination, source, length); destination[length]=0;
    return 0;
}

static const char *oab_mode_name(mode)
onboarding_activation_binding_mode mode;
{
    if(mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) return "provision";
    if(mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) return "claim";
    return 0;
}

static onboarding_activation_binding_mode oab_mode(value)
const char *value;
{
    if(value && !strcmp(value, "provision")) return ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    if(value && !strcmp(value, "claim")) return ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM;
    return ONBOARDING_ACTIVATION_BINDING_MODE_INVALID;
}

static int oab_valid(binding)
const onboarding_activation_binding *binding;
{
    return binding && oab_uuid(binding->actor_user_id) &&
        oab_uuid(binding->correlation_id) && oab_uuid(binding->character_id) &&
        oab_mode_name(binding->mode) && oab_uuid(binding->command_id);
}

static int oab_prepare(binding, actor_user_id, correlation_id, character_id, mode,
                       command_id)
onboarding_activation_binding *binding;
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *command_id;
{
    if(!binding || !oab_uuid(actor_user_id) || !oab_uuid(correlation_id) ||
       !oab_uuid(character_id) || !oab_mode_name(mode) || !oab_uuid(command_id))
        return -1;
    memset(binding, 0, sizeof(*binding));
    if(oab_copy(binding->actor_user_id, sizeof(binding->actor_user_id), actor_user_id) ||
       oab_copy(binding->correlation_id, sizeof(binding->correlation_id), correlation_id) ||
       oab_copy(binding->character_id, sizeof(binding->character_id), character_id) ||
       oab_copy(binding->command_id, sizeof(binding->command_id), command_id)) {
        memset(binding, 0, sizeof(*binding)); return -1;
    }
    binding->mode=mode;
    return oab_valid(binding) ? 0:-1;
}

static int oab_equal(left, right)
const onboarding_activation_binding *left;
const onboarding_activation_binding *right;
{
    return oab_valid(left) && oab_valid(right) &&
        !strcmp(left->actor_user_id, right->actor_user_id) &&
        !strcmp(left->correlation_id, right->correlation_id) &&
        !strcmp(left->character_id, right->character_id) &&
        left->mode == right->mode && !strcmp(left->command_id, right->command_id);
}

static int oab_dir_path(out, out_size)
char *out;
unsigned long out_size;
{ return resolve_runtime_path(OAB_DIR_LEGACY, out, out_size); }

static int oab_filename(command_id, out, out_size)
const char *command_id;
char *out;
unsigned long out_size;
{
    int length;
    if(!oab_uuid(command_id) || !out || !out_size) return -1;
    length=snprintf(out, out_size, "%s.binding", command_id);
    return length == 44 ? 0:-1;
}

int onboarding_activation_binding_path(command_id, out, out_size)
const char *command_id;
char *out;
unsigned long out_size;
{
    char dir[OAB_PATH_MAX], name[OAB_NAME_MAX];
    int length;
    if(!out || !out_size || oab_dir_path(dir, sizeof(dir)) ||
       oab_filename(command_id, name, sizeof(name))) return -1;
    length=snprintf(out, out_size, "%s/%s", dir, name);
    if(length < 0 || (unsigned long)length >= out_size) { out[0]=0; return -1; }
    return 0;
}

static int oab_ensure_dir(dir)
const char *dir;
{
    struct stat status;
    if(!dir || !dir[0]) return -1;
    if(lstat(dir, &status)) {
        if(errno != ENOENT || mkdir(dir, 0700)) return -1;
        if(lstat(dir, &status)) return -1;
    }
    if(!S_ISDIR(status.st_mode) || S_ISLNK(status.st_mode) || chmod(dir, 0700) ||
       lstat(dir, &status) || !S_ISDIR(status.st_mode) || S_ISLNK(status.st_mode) ||
       (status.st_mode & 0777) != 0700) return -1;
    return 0;
}

static int oab_write_all(fd, bytes, length)
int fd;
const char *bytes;
unsigned long length;
{
    int count;
    while(length) {
        count=write(fd, bytes, length);
        if(count < 0 && errno == EINTR) continue;
        if(count <= 0) return -1;
        bytes += count; length -= (unsigned long)count;
    }
    return 0;
}

static int oab_format(binding, out, out_size)
const onboarding_activation_binding *binding;
char *out;
unsigned long out_size;
{
    int length;
    if(!oab_valid(binding) || !out || !out_size) return -1;
    length=snprintf(out, out_size,
        "actor_user_id=%s\ncorrelation_id=%s\ncharacter_id=%s\nmode=%s\ncommand_id=%s\n",
        binding->actor_user_id, binding->correlation_id, binding->character_id,
        oab_mode_name(binding->mode), binding->command_id);
    return length < 0 || (unsigned long)length >= out_size ? -1:length;
}

static int oab_line(line, prefix, out, out_size)
char *line;
const char *prefix;
char *out;
unsigned long out_size;
{
    unsigned long prefix_size;
    if(!line || !prefix || !out) return -1;
    prefix_size=(unsigned long)strlen(prefix);
    if(strncmp(line, prefix, prefix_size)) return -1;
    return oab_copy(out, out_size, line + prefix_size);
}

static int oab_parse(text, binding)
char *text;
onboarding_activation_binding *binding;
{
    static const char *prefixes[5] = {
        "actor_user_id=", "correlation_id=", "character_id=", "mode=", "command_id="
    };
    char *line, *next, mode[16];
    int index;
    if(!text || !binding) return -1;
    memset(binding, 0, sizeof(*binding)); memset(mode, 0, sizeof(mode)); line=text;
    for(index=0; index<5; index++) {
        next=strchr(line, '\n'); if(!next) goto bad;
        *next=0;
        if(index == 0 && oab_line(line,prefixes[index],binding->actor_user_id,
                                  sizeof(binding->actor_user_id))) goto bad;
        if(index == 1 && oab_line(line,prefixes[index],binding->correlation_id,
                                  sizeof(binding->correlation_id))) goto bad;
        if(index == 2 && oab_line(line,prefixes[index],binding->character_id,
                                  sizeof(binding->character_id))) goto bad;
        if(index == 3 && oab_line(line,prefixes[index],mode,sizeof(mode))) goto bad;
        if(index == 4 && oab_line(line,prefixes[index],binding->command_id,
                                  sizeof(binding->command_id))) goto bad;
        line=next+1;
    }
    if(*line) goto bad;
    binding->mode=oab_mode(mode);
    if(!oab_valid(binding)) goto bad;
    memset(mode, 0, sizeof(mode)); return 0;
bad:
    memset(binding, 0, sizeof(*binding)); memset(mode, 0, sizeof(mode)); return -1;
}

static int oab_read_at(directory, name, binding)
int directory;
const char *name;
onboarding_activation_binding *binding;
{
    char text[OAB_TEXT_MAX];
    struct stat status;
    int fd, count, extra, result;
    fd=-1; result=-1; memset(text, 0, sizeof(text));
    if(directory < 0 || !name || !binding) goto out;
    fd=openat(directory, name, O_RDONLY|O_BINARY|O_NOFOLLOW|O_CLOEXEC);
    if(fd < 0 || fstat(fd, &status) || !S_ISREG(status.st_mode) ||
       (status.st_mode & 0777) != 0600 || status.st_nlink != 1) goto out;
    count=read(fd, text, sizeof(text)-1U);
    if(count < 0) goto out;
    text[count]=0; extra=read(fd, text+count, 1);
    if(extra != 0 || close(fd)) goto out;
    fd=-1; result=oab_parse(text, binding);
out:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    return result;
}

int onboarding_activation_binding_read(command_id, binding)
const char *command_id;
onboarding_activation_binding *binding;
{
    char dir[OAB_PATH_MAX], name[OAB_NAME_MAX];
    int directory, result;
    if(binding) memset(binding, 0, sizeof(*binding));
    if(!binding || oab_dir_path(dir, sizeof(dir)) || oab_filename(command_id, name,
        sizeof(name)) || oab_ensure_dir(dir)) return -1;
    directory=open(dir, O_RDONLY|O_BINARY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory < 0) return -1;
    result=oab_read_at(directory, name, binding);
    if(result == 0 && strcmp(command_id, binding->command_id) != 0) {
        memset(binding, 0, sizeof(*binding));
        result=-1;
    }
    if(close(directory)) result=-1;
    return result;
}

int onboarding_activation_binding_write(actor_user_id, correlation_id, character_id,
                                        mode, command_id)
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *command_id;
{
    onboarding_activation_binding next, prior;
    char dir[OAB_PATH_MAX], name[OAB_NAME_MAX], temporary[OAB_NAME_MAX], text[OAB_TEXT_MAX];
    int directory, fd, text_length, result;
    memset(&next, 0, sizeof(next)); memset(&prior, 0, sizeof(prior));
    memset(temporary, 0, sizeof(temporary)); memset(text, 0, sizeof(text));
    directory=-1; fd=-1; result=-1;
    if(oab_prepare(&next, actor_user_id, correlation_id, character_id, mode, command_id) ||
       oab_dir_path(dir, sizeof(dir)) || oab_filename(command_id, name, sizeof(name)) ||
       oab_ensure_dir(dir) || (text_length=oab_format(&next, text, sizeof(text))) < 0)
        goto out;
    directory=open(dir, O_RDONLY|O_BINARY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory < 0) goto out;
    if(oab_read_at(directory, name, &prior) == 0) {
        result=oab_equal(&next, &prior) ? 0:-1; goto out;
    }
    if(errno != ENOENT && faccessat(directory, name, F_OK, AT_SYMLINK_NOFOLLOW) == 0)
        goto out;
    if(snprintf(temporary, sizeof(temporary), ".%s.tmp", command_id) != 41) goto out;
    fd=openat(directory, temporary, O_WRONLY|O_CREAT|O_EXCL|O_BINARY|O_NOFOLLOW|O_CLOEXEC,
              0600);
    if(fd < 0 || fchmod(fd, 0600) || oab_write_all(fd, text, (unsigned long)text_length) ||
       fsync(fd) || close(fd)) goto out;
    fd=-1;
    if(linkat(directory, temporary, directory, name, 0)) {
        if(errno != EEXIST || oab_read_at(directory, name, &prior) || !oab_equal(&next, &prior))
            goto out;
    }
    if(unlinkat(directory, temporary, 0) && errno != ENOENT) goto out;
    temporary[0]=0;
    if(fsync(directory)) goto out;
    result=0;
out:
    if(fd >= 0) close(fd);
    if(directory >= 0 && temporary[0]) unlinkat(directory, temporary, 0);
    if(directory >= 0) close(directory);
    memset(&next, 0, sizeof(next)); memset(&prior, 0, sizeof(prior));
    memset(text, 0, sizeof(text));
    return result;
}
