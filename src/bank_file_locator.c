/* Fixed FileStore source locator for non-authoritative observers.  It is
 * deliberately separate from bank.c: no gameplay operation or mutable store
 * binding is entered merely to inspect legacy evidence. */
#include "mtype.h"
#include "player_path.h"
#include "resource_path.h"
#include "bank_store.h"

#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#ifndef O_CLOEXEC
#error "bank evidence requires O_CLOEXEC"
#endif
#ifndef O_DIRECTORY
#error "bank evidence requires O_DIRECTORY"
#endif
#ifndef O_NOFOLLOW
#error "bank evidence requires O_NOFOLLOW"
#endif
#ifndef AT_SYMLINK_NOFOLLOW
#error "bank evidence requires AT_SYMLINK_NOFOLLOW"
#endif

#ifdef BANK_EVIDENCE_TESTING
static uid_t bfl_test_expected_uid;
static int bfl_test_expected_uid_set;
void file_bank_store_test_set_expected_uid(uid)
uid_t uid;
{ bfl_test_expected_uid=uid;bfl_test_expected_uid_set=1; }
void file_bank_store_test_reset_expected_uid(void)
{ bfl_test_expected_uid_set=0; }
#endif

static uid_t bfl_expected_uid(void)
{
#ifdef BANK_EVIDENCE_TESTING
    if(bfl_test_expected_uid_set)return bfl_test_expected_uid;
#endif
    return geteuid();
}

static int bfl_same_entry(left,right)
const struct stat *left;
const struct stat *right;
{
    return left&&right&&left->st_dev==right->st_dev&&
        left->st_ino==right->st_ino&&left->st_mode==right->st_mode&&
        left->st_uid==right->st_uid&&left->st_gid==right->st_gid&&
        left->st_nlink==right->st_nlink;
}

static int bfl_directory_safe(value)
const struct stat *value;
{
    return value&&S_ISDIR(value->st_mode)&&value->st_uid==bfl_expected_uid()&&
        (value->st_mode&07777)==0700;
}

static int bfl_file_safe(value)
const struct stat *value;
{
    return value&&S_ISREG(value->st_mode)&&value->st_nlink==1&&
        value->st_uid==bfl_expected_uid()&&(value->st_mode&07777)==0600&&
        value->st_size>=0;
}

/* Open one named child only after observing its directory entry without
 * following a link.  Checking the entry both before and after open catches a
 * rename/interchange during this step; the descriptor then anchors all later
 * descendants. */
static int bfl_open_directory(parent,name,before)
int parent;
const char *name;
struct stat *before;
{
    int child;
    struct stat entry,opened,after;

    if(fstatat(parent,name,&entry,AT_SYMLINK_NOFOLLOW)<0)return -1;
    if(!bfl_directory_safe(&entry)) { errno=EIO;return -1; }
    child=openat(parent,name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC|O_BINARY,0);
    if(child<0)return -1;
    if(fstat(child,&opened)<0||!bfl_directory_safe(&opened)||
       !bfl_same_entry(&entry,&opened)||
       fstatat(parent,name,&after,AT_SYMLINK_NOFOLLOW)<0||
       !bfl_directory_safe(&after)||!bfl_same_entry(&opened,&after)) {
        close(child);
        errno=EIO;
        return -1;
    }
    if(before)*before=opened;
    return child;
}

static int bfl_open_file(parent,name,before)
int parent;
const char *name;
struct stat *before;
{
    int file;
    struct stat entry,opened,after;

    if(fstatat(parent,name,&entry,AT_SYMLINK_NOFOLLOW)<0)return -1;
    if(!bfl_file_safe(&entry)) { errno=EIO;return -1; }
    file=openat(parent,name,O_RDONLY|O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC|O_BINARY,0);
    if(file<0)return -1;
    if(fstat(file,&opened)<0||!bfl_file_safe(&opened)||
       !bfl_same_entry(&entry,&opened)||
       fstatat(parent,name,&after,AT_SYMLINK_NOFOLLOW)<0||!bfl_file_safe(&after)||
       !bfl_same_entry(&opened,&after)) {
        close(file);
        errno=EIO;
        return -1;
    }
    if(before)*before=opened;
    return file;
}

int file_bank_store_open_readonly(str)
const char *str;
{
    char root[1024];
    int root_fd,player_fd,bank_fd,file_fd,saved_errno;
    struct stat root_status;

    if(!str||!player_name_is_valid((const unsigned char *)str,
       PLAYER_NAME_MIN_CODEPOINTS,PLAYER_NAME_MAX_CODEPOINTS)||
       resolve_runtime_path(MUDHOME,root,sizeof(root))<0) {
        errno=EINVAL;
        return -1;
    }
    root_fd=player_fd=bank_fd=file_fd=-1;
    root_fd=open(root,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC|O_BINARY,0);
    if(root_fd<0)goto failed;
    if(fstat(root_fd,&root_status)<0||!bfl_directory_safe(&root_status)) {
        errno=EIO;
        goto failed;
    }
    player_fd=bfl_open_directory(root_fd,"player",0);
    if(player_fd<0)goto failed;
    bank_fd=bfl_open_directory(player_fd,"bank",0);
    if(bank_fd<0)goto failed;
    file_fd=bfl_open_file(bank_fd,str,0);
    if(file_fd<0)goto failed;
    if(close(bank_fd)<0) { bank_fd=-1;goto failed; }
    bank_fd=-1;
    if(close(player_fd)<0) { player_fd=-1;goto failed; }
    player_fd=-1;
    if(close(root_fd)<0) { root_fd=-1;goto failed; }
    return file_fd;
failed:
    saved_errno=errno;
    if(file_fd>=0)close(file_fd);
    if(bank_fd>=0)close(bank_fd);
    if(player_fd>=0)close(player_fd);
    if(root_fd>=0)close(root_fd);
    errno=saved_errno;
    return -1;
}

/* Bind an already-read descriptor back to the current fixed FileStore path.
 * This is intentionally a fresh descriptor-rooted walk rather than a path
 * stat: a same-size rename after open must not yield evidence for its old
 * occupant.  A process that can mutate between this final check and return is
 * still outside what the native FileStore ABI can lock atomically. */
int file_bank_store_validate_open_readonly(str,file)
const char *str;
int file;
{
    int current,result,saved_errno;
    struct stat opened,observed;

    if(file<0) { errno=EINVAL;return -1; }
    if(fstat(file,&opened)<0)return -1;
    if(!bfl_file_safe(&opened)) { errno=EIO;return -1; }
    current=file_bank_store_open_readonly(str);
    if(current<0)return -1;
    result=fstat(current,&observed)<0||!bfl_file_safe(&observed)||
        !bfl_same_entry(&opened,&observed);
    saved_errno=result?EIO:0;
    if(close(current)<0) { result=1;saved_errno=errno; }
    if(result) {
        errno=saved_errno?saved_errno:EIO;
        return -1;
    }
    return 0;
}
