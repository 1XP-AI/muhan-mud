#include "character_player_snapshot_v1_capture.h"

#include "cdto_v1.h"
#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2.h"
#include "player_snapshot_v1.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char WORLD[]="m3-recovery";
static const char INSTANCE[]="11111111-1111-4111-8111-111111111111";
static const char CHARACTER[]="90000000-0000-4000-8000-000000000006";
static const char COMMAND[]="10000000-0000-4000-8000-000000000001";
static const unsigned char RAW[]="legacy-player-stage";

typedef struct decode_fixture {
    int calls, releases, fail, wrong_name;
} decode_fixture;

static int expect(value,message)
int value;
const char *message;
{ if(value)return 0;fprintf(stderr,"character_player_snapshot_v1_capture_test: %s\n",message);return 1; }

static int write_all(fd,bytes,length)
int fd;const void *bytes;size_t length;
{ const unsigned char *cursor=(const unsigned char *)bytes;ssize_t count;while(length){count=write(fd,cursor,length);if(count<0&&errno==EINTR)continue;if(count<=0)return-1;cursor+=count;length-=(size_t)count;}return 0; }

static int path_join(output,capacity,root,relative)
char *output;size_t capacity;const char *root,*relative;
{ int count=snprintf(output,capacity,"%s/%s",root,relative);return count<0||(size_t)count>=capacity?-1:0; }

static int make_dir(root,relative)
const char *root,*relative;
{ char path[PATH_MAX];return path_join(path,sizeof(path),root,relative)||mkdir(path,0700)?-1:0; }

static int leaf(root,relative,bytes,length)
const char *root,*relative;const void *bytes;size_t length;
{ char path[PATH_MAX];int fd,result=0;if(path_join(path,sizeof(path),root,relative))return-1;fd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);if(fd<0)return-1;if(fchmod(fd,0600)||write_all(fd,bytes,length)||fsync(fd))result=-1;if(close(fd))result=-1;return result; }

static int remove_tree(path)
const char *path;
{ DIR *directory;struct dirent *entry;struct stat status;char child[PATH_MAX];if(lstat(path,&status))return errno==ENOENT?0:-1;if(!S_ISDIR(status.st_mode))return unlink(path);directory=opendir(path);if(!directory)return-1;while((entry=readdir(directory))){if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,".."))continue;if(snprintf(child,sizeof(child),"%s/%s",path,entry->d_name)<0||remove_tree(child)){closedir(directory);return-1;}}return closedir(directory)||rmdir(path)?-1:0; }

static int make_root(root,label)
char root[PATH_MAX];const char *label;
{ char temporary[PATH_MAX];int count;if(!realpath("/tmp",temporary))return-1;count=snprintf(root,PATH_MAX,"%s/muhan-player-snapshot-capture-%s-XXXXXX",temporary,label);return count<0||count>=PATH_MAX||!mkdtemp(root)?-1:0; }

static int seed(root)
const char *root;
{
    static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
    static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-recovery\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
    return make_dir(root,"player")||make_dir(root,"player/66")||
        make_dir(root,"character-save-stage")||
        make_dir(root,"character-save-journal")||
        leaf(root,"character-save-journal/writer-instance.v2",instance,sizeof(instance)-1)||
        leaf(root,"character-save-journal/writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0;
}

static int hash_bytes(root,bytes,length,output)
const char *root;const void *bytes;size_t length;char output[65];
{ char path[PATH_MAX];int fd,result;if(path_join(path,sizeof(path),root,"hash.tmp"))return-1;fd=open(path,O_RDWR|O_CREAT|O_EXCL|O_NOFOLLOW,0600);if(fd<0||write_all(fd,bytes,length)){if(fd>=0)close(fd);return-1;}result=character_save_journal_v2_hash_fd(fd,output);if(close(fd))result=-1;if(unlink(path))result=-1;return result; }

static int prepare(root)
const char *root;
{
    character_save_journal_v2_wire wire;
    memset(&wire,0,sizeof(wire));
    wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire.writer_instance_id,INSTANCE);
    strcpy(wire.character_id,CHARACTER);
    strcpy(wire.world_id,WORLD);
    strcpy(wire.legacy_name_key_hex,"4d33616c706861");
    strcpy(wire.legacy_shard,"66");
    strcpy(wire.command_uuid,COMMAND);
    wire.writer_epoch=7;
    wire.writer_revision=1;
    wire.expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    wire.storage_format=1;
    if(hash_bytes(root,RAW,sizeof(RAW)-1,wire.post_sha256)||
       character_save_journal_v2_request_sha256(&wire,wire.request_sha256))
        return -1;
    return character_save_journal_v2_prepare(root,&wire,RAW,sizeof(RAW)-1);
}

static int decode(void *opaque,int fd,creature **output)
{
    decode_fixture *fixture=(decode_fixture *)opaque;
    unsigned char bytes[sizeof(RAW)];
    creature *player;
    ssize_t count,extra;
    fixture->calls++;
    *output=0;
    if(fixture->fail)return -1;
    do count=read(fd,bytes,sizeof(RAW)-1);while(count<0&&errno==EINTR);
    do extra=read(fd,bytes+sizeof(RAW)-1,1);while(extra<0&&errno==EINTR);
    if(count!=(ssize_t)(sizeof(RAW)-1)||extra!=0||memcmp(bytes,RAW,sizeof(RAW)-1))
        return -1;
    player=(creature *)calloc(1,sizeof(*player));
    if(!player)return -1;
    player->type=PLAYER;
    player->fd=-1;
    strcpy(player->name,fixture->wrong_name?"M3beta":"M3alpha");
    memset(player->password,0x5a,sizeof(player->password));
    *output=player;
    return 0;
}

static void release(void *opaque,creature *player)
{ decode_fixture *fixture=(decode_fixture *)opaque;fixture->releases++;free(player); }

static int setup(root,label,writer,capture,decoder)
char root[PATH_MAX];const char *label;
character_save_journal_v2_writer_context *writer;
character_player_snapshot_v1_capture *capture;
decode_fixture *decoder;
{
    if(make_root(root,label)||seed(root)||prepare(root))return -1;
    memset(writer,0,sizeof(*writer));
    if(character_save_journal_v2_writer_open(root,WORLD,writer))return -1;
    memset(decoder,0,sizeof(*decoder));
    character_player_snapshot_v1_capture_init(capture,decode,decoder,release,decoder);
    return 0;
}

static int teardown(root,writer)
const char *root;character_save_journal_v2_writer_context *writer;
{ int first=character_save_journal_v2_writer_close(writer);int second=remove_tree(root);return first||second; }

static int test_capture_and_exact_retry(void)
{
    char root[PATH_MAX],path[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_artifact_metadata key,metadata;
    decode_fixture decoder;
    creature *clone=0;
    uint8_t *snapshot=0;
    size_t snapshot_length=0;
    struct stat status;
    int directory_fd=-1,failed=0;

    if(setup(root,"success",&writer,&capture,&decoder))return 1;
    failed+=expect(character_player_snapshot_v1_capture_observe(&capture,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK&&decoder.calls==1&&decoder.releases==1&&
        capture.report.attempted==1&&capture.report.recorded==1&&
        capture.report.last_artifact_result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK,
        "durable PREPARED stage must become one canonical artifact");
    if(path_join(path,sizeof(path),root,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY)||
       lstat(path,&status))return failed+1;
    failed+=expect(S_ISDIR(status.st_mode)&&(status.st_mode&07777)==0700,
        "capture directory must be a private regular directory");
    directory_fd=open(path,O_RDONLY|O_DIRECTORY|O_NOFOLLOW);
    memset(&key,0,sizeof(key));strcpy(key.command_id,COMMAND);
    memset(&metadata,0,sizeof(metadata));
    failed+=expect(directory_fd>=0&&character_player_snapshot_v1_artifact_load(
        directory_fd,&key,&metadata,&snapshot,&snapshot_length)==
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK&&
        !strcmp(metadata.character_id,CHARACTER)&&
        !strcmp(metadata.source_post_sha256,capture.report.last_source_post_sha256)&&
        metadata.source_octets==sizeof(RAW)-1&&
        player_snapshot_v1_decode_clone(snapshot,snapshot_length,&clone)==CDTO_V1_OK&&
        clone&&!strcmp(clone->name,"M3alpha")&&clone->fd==-1&&clone->password[0]==0,
        "loaded artifact must preserve only the canonical player projection");
    if(clone)player_snapshot_v1_free_clone(clone);
    if(snapshot)character_player_snapshot_v1_artifact_free(snapshot);
    if(directory_fd>=0&&close(directory_fd))failed++;
    failed+=expect(character_player_snapshot_v1_capture_observe(&capture,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK&&decoder.calls==2&&decoder.releases==2&&
        capture.report.attempted==2&&capture.report.recorded==1&&
        capture.report.exact_retries==1&&capture.report.failed==0&&
        capture.report.last_artifact_result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY,
        "same immutable stage must converge as an exact retry");
    if(teardown(root,&writer))failed++;
    return failed;
}

static int test_source_and_decode_rejections(void)
{
    char root[PATH_MAX],stage[PATH_MAX],alias[PATH_MAX],artifact[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    decode_fixture decoder;
    int fd,failed=0;

    if(setup(root,"wrong-name",&writer,&capture,&decoder))return 1;
    decoder.wrong_name=1;
    failed+=expect(character_player_snapshot_v1_capture_observe(&capture,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DECODE&&decoder.calls==1&&decoder.releases==1&&
        path_join(artifact,sizeof(artifact),root,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY)==0&&
        access(artifact,F_OK)!=0,
        "a decoded player whose name differs from PREPARED identity must create no artifact");
    if(teardown(root,&writer))failed++;

    if(setup(root,"changed-stage",&writer,&capture,&decoder))return failed+1;
    if(path_join(stage,sizeof(stage),root,"character-save-stage/10000000-0000-4000-8000-000000000001.stage"))return failed+1;
    fd=open(stage,O_WRONLY|O_NOFOLLOW);if(fd<0)return failed+1;
    if(write_all(fd,"X",1)||close(fd))return failed+1;
    failed+=expect(character_player_snapshot_v1_capture_observe(&capture,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE&&decoder.calls==0,
        "a stage whose bytes no longer match PREPARED must be rejected before decode");
    if(teardown(root,&writer))failed++;

    if(setup(root,"hardlink-stage",&writer,&capture,&decoder))return failed+1;
    if(path_join(stage,sizeof(stage),root,"character-save-stage/10000000-0000-4000-8000-000000000001.stage")||
       path_join(alias,sizeof(alias),root,"stage-alias")||link(stage,alias))return failed+1;
    failed+=expect(character_player_snapshot_v1_capture_observe(&capture,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE&&decoder.calls==0,
        "a hard-linked stage must be rejected without deleting either evidence name");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* The handoff consumer receives an already-open private source.  It must
 * still enforce one-link ownership itself, because a caller can race an
 * external hardlink in after queue validation. */
static int test_hardlinked_private_consume_source_is_rejected(void)
{
    char root[PATH_MAX],source[PATH_MAX],alias[PATH_MAX],artifact[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_save_journal_v2_wire wire;
    decode_fixture decoder;
    struct stat source_status,alias_status;
    int fd=-1,failed=0,result;

    if(setup(root,"hardlink-private-consume",&writer,&capture,&decoder))return 1;
    if(path_join(source,sizeof(source),root,"private-source")||
       path_join(alias,sizeof(alias),root,"private-source-alias")||
       leaf(root,"private-source",RAW,sizeof(RAW)-1)||link(source,alias)||
       character_save_journal_v2_read_prepared(root,COMMAND,&wire))
        return teardown(root,&writer)?2:1;
    fd=open(source,O_RDONLY|O_NOFOLLOW);
    result=fd<0 ? CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID :
        character_player_snapshot_v1_capture_consume(&capture,&writer,COMMAND,
        wire.request_sha256,wire.writer_instance_id,wire.writer_epoch,fd);
    if(fd>=0&&close(fd))failed++;
    failed+=expect(result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE&&
        decoder.calls==0&&decoder.releases==0&&
        path_join(artifact,sizeof(artifact),root,
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY)==0&&access(artifact,F_OK)!=0&&
        lstat(source,&source_status)==0&&lstat(alias,&alias_status)==0&&
        source_status.st_dev==alias_status.st_dev&&source_status.st_ino==alias_status.st_ino&&
        source_status.st_nlink==2,
        "capture consume must reject an externally hardlinked private source without touching either name");
    memset(&wire,0,sizeof(wire));
    if(teardown(root,&writer))failed++;
    return failed;
}

int main(void)
{
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    return test_capture_and_exact_retry()|test_source_and_decode_rejections()|
        test_hardlinked_private_consume_source_is_rejected();
}
