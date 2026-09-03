#include "character_player_snapshot_v1_capture.h"

#include "cdto_v1.h"
#include "character_save_journal_v2.h"
#include "player_snapshot_v1.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#ifndef O_BINARY
#define O_BINARY 0
#endif
#ifndef O_CLOEXEC
#error "PlayerSnapshotV1 capture requires O_CLOEXEC"
#endif
#ifndef O_DIRECTORY
#error "PlayerSnapshotV1 capture requires O_DIRECTORY"
#endif
#ifndef O_NOFOLLOW
#error "PlayerSnapshotV1 capture requires O_NOFOLLOW"
#endif
#ifndef AT_SYMLINK_NOFOLLOW
#error "PlayerSnapshotV1 capture requires AT_SYMLINK_NOFOLLOW"
#endif

static void capture_increment(value)
uint64_t *value;
{ if(value&&*value<UINT64_MAX)(*value)++; }

static int capture_close(fd)
int fd;
{ return close(fd); }

static int capture_sync(fd)
int fd;
{
    int result;
    do result=fsync(fd);while(result<0&&errno==EINTR);
    return result;
}

static int capture_directory_safe(fd,uid_out)
int fd;
uid_t *uid_out;
{
    struct stat status;
    if(fd<0||fstat(fd,&status)||!S_ISDIR(status.st_mode)||
       (status.st_mode&07777)!=0700) return 0;
    if(uid_out)*uid_out=status.st_uid;
    return 1;
}

static int capture_child_directory_safe(fd,uid)
int fd;
uid_t uid;
{
    struct stat status;
    return fd>=0&&!fstat(fd,&status)&&S_ISDIR(status.st_mode)&&
        status.st_uid==uid&&(status.st_mode&07777)==0700;
}

static int capture_stage_safe(fd,uid,status)
int fd;
uid_t uid;
struct stat *status;
{
    return status&&fd>=0&&!fstat(fd,status)&&S_ISREG(status->st_mode)&&
        status->st_uid==uid&&(status->st_mode&07777)==0600&&
        status->st_nlink==1&&status->st_size>0&&
        (uint64_t)status->st_size<=CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES;
}

static int capture_same_stage(left,right)
const struct stat *left,*right;
{
    if(!left||!right||left->st_dev!=right->st_dev||
       left->st_ino!=right->st_ino||left->st_mode!=right->st_mode||
       left->st_uid!=right->st_uid||left->st_gid!=right->st_gid||
       left->st_nlink!=right->st_nlink||left->st_size!=right->st_size||
       left->st_mtime!=right->st_mtime||left->st_ctime!=right->st_ctime)
        return 0;
#if defined(__APPLE__)
    return left->st_mtimespec.tv_nsec==right->st_mtimespec.tv_nsec&&
        left->st_ctimespec.tv_nsec==right->st_ctimespec.tv_nsec;
#elif defined(__linux__) || defined(__FreeBSD__) || defined(__NetBSD__) || \
      defined(__OpenBSD__)
    return left->st_mtim.tv_nsec==right->st_mtim.tv_nsec&&
        left->st_ctim.tv_nsec==right->st_ctim.tv_nsec;
#else
    return 1;
#endif
}

static int capture_text_copy(destination,capacity,source)
char *destination;
size_t capacity;
const char *source;
{
    size_t length;
    if(!destination||!capacity||!source)return -1;
    length=strlen(source);
    if(length>=capacity)return -1;
    memcpy(destination,source,length+1);
    return 0;
}

static int capture_hex_digit(value)
unsigned char value;
{
    if(value>='0'&&value<='9')return value-'0';
    if(value>='a'&&value<='f')return value-'a'+10;
    return -1;
}

static int capture_player_name_matches(player,name_hex)
const creature *player;
const char *name_hex;
{
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX+1];
    size_t index,length,player_length;
    int high,low;
    if(!player||!name_hex)return 0;
    length=strlen(name_hex);
    if(length<2||length>CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX||
       (length&1))return 0;
    for(index=0;index<length;index+=2) {
        high=capture_hex_digit((unsigned char)name_hex[index]);
        low=capture_hex_digit((unsigned char)name_hex[index+1]);
        if(high<0||low<0)return 0;
        name[index/2]=(unsigned char)((high<<4)|low);
    }
    name[length/2]=0;
    for(player_length=0;player_length<sizeof(player->name);player_length++)
        if(!player->name[player_length])break;
    if(player_length==sizeof(player->name))return 0;
    return player_length==length/2&&!memcmp(player->name,name,player_length);
}

static int capture_open_child(root,name,uid)
int root;
const char *name;
uid_t uid;
{
    int fd=openat(root,name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC|O_BINARY);
    if(fd<0)return -1;
    if(!capture_child_directory_safe(fd,uid)) {
        capture_close(fd);
        return -1;
    }
    return fd;
}

static int capture_open_artifact_directory(root,uid)
int root;
uid_t uid;
{
    int created=0,fd;
    if(mkdirat(root,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,0700)==0)
        created=1;
    else if(errno!=EEXIST)
        return -1;
    fd=capture_open_child(root,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,uid);
    if(fd<0)return -1;
    if(created&&capture_sync(root)) {
        capture_close(fd);
        return -1;
    }
    return fd;
}

static void capture_set_last(capture,result,artifact_result,source_hash)
character_player_snapshot_v1_capture *capture;
character_player_snapshot_v1_capture_result result;
int artifact_result;
const char *source_hash;
{
    capture->report.last_result=result;
    capture->report.last_artifact_result=artifact_result;
    memset(capture->report.last_source_post_sha256,0,
        sizeof(capture->report.last_source_post_sha256));
    if(source_hash)capture_text_copy(capture->report.last_source_post_sha256,
        sizeof(capture->report.last_source_post_sha256),source_hash);
    if(result!=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK)
        capture_increment(&capture->report.failed);
    else if(artifact_result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK)
        capture_increment(&capture->report.recorded);
    else if(artifact_result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY)
        capture_increment(&capture->report.exact_retries);
}

void character_player_snapshot_v1_capture_init(capture,decode,decode_opaque,
    release,release_opaque)
character_player_snapshot_v1_capture *capture;
character_player_snapshot_v1_capture_decode decode;
void *decode_opaque;
character_player_snapshot_v1_capture_release release;
void *release_opaque;
{
    if(!capture)return;
    memset(capture,0,sizeof(*capture));
    capture->decode=decode;
    capture->decode_opaque=decode_opaque;
    capture->release=release;
    capture->release_opaque=release_opaque;
    capture->report.last_artifact_result=
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;
}

int character_player_snapshot_v1_capture_observe(opaque,writer,command_uuid)
void *opaque;
const character_save_journal_v2_writer_context *writer;
const char *command_uuid;
{
    character_player_snapshot_v1_capture *capture=
        (character_player_snapshot_v1_capture *)opaque;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    character_player_snapshot_v1_artifact_metadata metadata;
    creature *player=0;
    uint8_t *snapshot=0;
    size_t snapshot_length=0;
    struct stat stage_before,stage_after;
    uid_t trusted_uid=0;
    char stage_leaf[CHARACTER_SAVE_JOURNAL_V2_STAGE_LEAF_MAX+1];
    char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN+1];
    int root=-1,stage_directory=-1,stage=-1,artifact_directory=-1;
    int artifact_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;
    character_player_snapshot_v1_capture_result result=
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID;

    if(!capture)return CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID;
    capture_increment(&capture->report.attempted);
    capture->report.last_result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID;
    capture->report.last_artifact_result=
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;
    capture->report.last_source_post_sha256[0]=0;
    if(!capture->decode||!capture->release||!writer||!command_uuid)goto done;
    memset(&tuple,0,sizeof(tuple));
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||
       character_save_journal_v2_writer_dup_held_root_fd(writer,&root)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_CONTEXT;
        goto done;
    }
    if(!capture_directory_safe(root,&trusted_uid)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_CONTEXT;
        goto done;
    }
    memset(&wire,0,sizeof(wire));
    if(character_save_journal_v2_read_prepared_at(root,command_uuid,&wire)||
       strcmp(wire.command_uuid,command_uuid)||
       strcmp(wire.world_id,tuple.world_id)||
       strcmp(wire.writer_instance_id,tuple.writer_instance_id)||
       wire.writer_epoch!=tuple.writer_epoch) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_PREPARED;
        goto done;
    }
    capture_text_copy(capture->report.last_source_post_sha256,
        sizeof(capture->report.last_source_post_sha256),wire.post_sha256);
    stage_directory=capture_open_child(root,"character-save-stage",trusted_uid);
    if(stage_directory<0||character_save_journal_v2_stage_leaf(
       command_uuid,stage_leaf,sizeof(stage_leaf))) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE;
        goto done;
    }
    stage=openat(stage_directory,stage_leaf,
        O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC|O_BINARY);
    if(stage<0||!capture_stage_safe(stage,trusted_uid,&stage_before)||
       character_save_journal_v2_hash_fd(stage,digest)||
       strcmp(digest,wire.post_sha256)||lseek(stage,0,SEEK_SET)<0) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE;
        goto done;
    }
    if(capture->decode(capture->decode_opaque,stage,&player)||!player||
       player->type!=PLAYER||
       !capture_player_name_matches(player,wire.legacy_name_key_hex)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DECODE;
        goto done;
    }
    if(player_snapshot_v1_encode_loaded(player,&snapshot,&snapshot_length)!=
       CDTO_V1_OK||!snapshot||!snapshot_length) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_ENCODE;
        goto done;
    }
    if(character_save_journal_v2_hash_fd(stage,digest)||
       strcmp(digest,wire.post_sha256)||
       !capture_stage_safe(stage,trusted_uid,&stage_after)||
       !capture_same_stage(&stage_before,&stage_after)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE;
        goto done;
    }
    if(capture_close(stage)) {
        stage=-1;
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
        goto done;
    }
    stage=-1;
    if(capture_close(stage_directory)) {
        stage_directory=-1;
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
        goto done;
    }
    stage_directory=-1;
    artifact_directory=capture_open_artifact_directory(root,trusted_uid);
    if(artifact_directory<0) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
        goto done;
    }
    memset(&metadata,0,sizeof(metadata));
    if(capture_text_copy(metadata.world_id,sizeof(metadata.world_id),wire.world_id)||
       capture_text_copy(metadata.character_id,sizeof(metadata.character_id),wire.character_id)||
       capture_text_copy(metadata.command_id,sizeof(metadata.command_id),wire.command_uuid)||
       capture_text_copy(metadata.canonical_name_hex,sizeof(metadata.canonical_name_hex),wire.legacy_name_key_hex)||
       capture_text_copy(metadata.request_sha256,sizeof(metadata.request_sha256),wire.request_sha256)||
       capture_text_copy(metadata.source_post_sha256,sizeof(metadata.source_post_sha256),wire.post_sha256)||
       capture_text_copy(metadata.writer_instance_id,sizeof(metadata.writer_instance_id),wire.writer_instance_id)||
       capture_text_copy(metadata.snapshot_format,sizeof(metadata.snapshot_format),CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID;
        goto done;
    }
    metadata.writer_epoch=wire.writer_epoch;
    metadata.writer_revision=wire.writer_revision;
    metadata.source_octets=(uint64_t)stage_before.st_size;
    metadata.storage_format=(int16_t)wire.storage_format;
    artifact_result=character_player_snapshot_v1_artifact_store(
        artifact_directory,&metadata,snapshot,snapshot_length);
    if(artifact_result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK&&
       artifact_result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_ARTIFACT;
        goto done;
    }
    result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK;
done:
    if(snapshot)cdto_v1_free_wire(snapshot);
    if(player)capture->release(capture->release_opaque,player);
    if(stage>=0&&capture_close(stage)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
    if(stage_directory>=0&&capture_close(stage_directory)&&
       result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
    if(artifact_directory>=0&&capture_close(artifact_directory)&&
       result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
    if(root>=0&&capture_close(root)&&
       result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR;
    capture_set_last(capture,result,artifact_result,
        wire.post_sha256[0]?wire.post_sha256:0);
    memset(&tuple,0,sizeof(tuple));
    memset(&wire,0,sizeof(wire));
    memset(&metadata,0,sizeof(metadata));
    memset(digest,0,sizeof(digest));
    memset(stage_leaf,0,sizeof(stage_leaf));
    return result;
}
