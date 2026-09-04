/*
 * TDD record:
 * RED: `make -C src character-player-snapshot-v1-handoff-test` failed before
 * the durable handoff boundary existed.
 * GREEN: the observer writes a bounded durable token; drain is explicitly
 * separate and is the only path that may call the heavy PlayerSnapshotV1
 * decoder.
 */
#include "character_player_snapshot_v1_handoff.h"

#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2.h"
#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_publish.h"
#include "character_snapshot_shadow_outbox.h"
#include "player_snapshot_v1.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char WORLD[]="m3-handoff";
static const char INSTANCE[]="11111111-1111-4111-8111-111111111111";
static const char CHARACTER[]="90000000-0000-4000-8000-000000000006";
static const char CHARACTER_B[]="90000000-0000-4000-8000-000000000007";
static const char COMMAND[]="10000000-0000-4000-8000-000000000001";
static const char COMMAND_B[]="20000000-0000-4000-8000-000000000002";
static const unsigned char RAW[]="legacy-player-stage";
static const unsigned char RAW_B[]="legacy-player-stage-successor";

typedef struct decode_fixture { int calls,releases,block,name_b; } decode_fixture;

static int expect(value,message)
int value; const char *message;
{ if(value)return 0;fprintf(stderr,"character_player_snapshot_v1_handoff_test: %s\n",message);return 1; }

static int write_all(fd,bytes,length)
int fd; const void *bytes; size_t length;
{ const unsigned char *cursor=(const unsigned char *)bytes;ssize_t count;while(length){count=write(fd,cursor,length);if(count<0&&errno==EINTR)continue;if(count<=0)return-1;cursor+=count;length-=(size_t)count;}return 0; }

static int path_join(output,capacity,root,relative)
char *output; size_t capacity; const char *root,*relative;
{ int count=snprintf(output,capacity,"%s/%s",root,relative);return count<0||(size_t)count>=capacity?-1:0; }

static int make_dir(root,relative)
const char *root,*relative;
{ char path[PATH_MAX];return path_join(path,sizeof(path),root,relative)||mkdir(path,0700)?-1:0; }

static int leaf(root,relative,bytes,length)
const char *root,*relative; const void *bytes; size_t length;
{ char path[PATH_MAX];int fd,result=0;if(path_join(path,sizeof(path),root,relative))return-1;fd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);if(fd<0)return-1;if(fchmod(fd,0600)||write_all(fd,bytes,length)||fsync(fd))result=-1;if(close(fd))result=-1;return result; }

static int remove_tree(path)
const char *path;
{ DIR *directory;struct dirent *entry;struct stat status;char child[PATH_MAX];if(lstat(path,&status))return errno==ENOENT?0:-1;if(!S_ISDIR(status.st_mode))return unlink(path);directory=opendir(path);if(!directory)return-1;while((entry=readdir(directory))){if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,".."))continue;if(snprintf(child,sizeof(child),"%s/%s",path,entry->d_name)<0||remove_tree(child)){closedir(directory);return-1;}}return closedir(directory)||rmdir(path)?-1:0; }

static int make_root(root,label)
char root[PATH_MAX]; const char *label;
{ char temporary[PATH_MAX];int count;if(!realpath("/tmp",temporary))return-1;count=snprintf(root,PATH_MAX,"%s/muhan-player-snapshot-handoff-%s-XXXXXX",temporary,label);return count<0||count>=PATH_MAX||!mkdtemp(root)?-1:0; }

static int seed(root)
const char *root;
{
    static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
    static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-handoff\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
    return make_dir(root,"player")||make_dir(root,"player/66")||make_dir(root,"player/b2")||
        make_dir(root,"character-save-stage")||make_dir(root,"character-save-journal")||
        leaf(root,"character-save-journal/writer-instance.v2",instance,sizeof(instance)-1)||
        leaf(root,"character-save-journal/writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0;
}

static int hash_bytes(root,bytes,length,output)
const char *root; const void *bytes; size_t length; char output[65];
{ char path[PATH_MAX];int fd,result;if(path_join(path,sizeof(path),root,"hash.tmp"))return-1;fd=open(path,O_RDWR|O_CREAT|O_EXCL|O_NOFOLLOW,0600);if(fd<0||write_all(fd,bytes,length)){if(fd>=0)close(fd);return-1;}result=character_save_journal_v2_hash_fd(fd,output);if(close(fd))result=-1;if(unlink(path))result=-1;return result; }

static int prepare_command(root,command,bytes,length,expected,expected_bytes,
    expected_length,revision)
const char *root,*command; const void *bytes,*expected_bytes; size_t length,
    expected_length; character_save_journal_v2_expected_state expected;
uint64_t revision;
{
    character_save_journal_v2_wire wire;
    memset(&wire,0,sizeof(wire));
    wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire.writer_instance_id,INSTANCE);strcpy(wire.character_id,CHARACTER);
    strcpy(wire.world_id,WORLD);strcpy(wire.legacy_name_key_hex,"4d33616c706861");
    strcpy(wire.legacy_shard,"66");strcpy(wire.command_uuid,command);
    wire.writer_epoch=7;wire.writer_revision=revision;
    wire.expected_state=expected;wire.storage_format=1;
    if(expected==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING&&
       (!expected_bytes||hash_bytes(root,expected_bytes,expected_length,
       wire.expected_sha256)))return-1;
    if(hash_bytes(root,bytes,length,wire.post_sha256)||
       character_save_journal_v2_request_sha256(&wire,wire.request_sha256))return-1;
    return character_save_journal_v2_prepare(root,&wire,bytes,length);
}

static int prepare(root)
const char *root;
{ return prepare_command(root,COMMAND,RAW,sizeof(RAW)-1,
    CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT,0,0,1); }

static int prepare_command_other(root,command,bytes,length,revision)
const char *root,*command;const void *bytes;size_t length;uint64_t revision;
{
    character_save_journal_v2_wire wire;
    memset(&wire,0,sizeof(wire));
    wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire.writer_instance_id,INSTANCE);strcpy(wire.character_id,CHARACTER_B);
    strcpy(wire.world_id,WORLD);strcpy(wire.legacy_name_key_hex,"4d3362657461");
    strcpy(wire.legacy_shard,"b2");strcpy(wire.command_uuid,command);
    wire.writer_epoch=7;wire.writer_revision=revision;
    wire.expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;wire.storage_format=1;
    if(hash_bytes(root,bytes,length,wire.post_sha256)||
       character_save_journal_v2_request_sha256(&wire,wire.request_sha256))return-1;
    return character_save_journal_v2_prepare(root,&wire,bytes,length);
}

static int decode(opaque,fd,output)
void *opaque; int fd; creature **output;
{
    decode_fixture *fixture=(decode_fixture *)opaque;
    creature *player;
    (void)fd;
    fixture->calls++;*output=0;
    if(fixture->block) pause();
    player=(creature *)calloc(1,sizeof(*player));
    if(!player)return-1;
    player->type=PLAYER;player->fd=-1;strcpy(player->name,
        fixture->name_b?"M3beta":"M3alpha");
    memset(player->password,0x5a,sizeof(player->password));*output=player;
    return 0;
}

static void release(opaque,player)
void *opaque; creature *player;
{ decode_fixture *fixture=(decode_fixture *)opaque;fixture->releases++;free(player); }

static character_save_journal_v2_receipt_result receipt(opaque,value)
void *opaque; const character_save_journal_v2_receipt *value;
{ int *calls=(int *)opaque;if(!value||!value->command_id||(strcmp(value->command_id,COMMAND)&&strcmp(value->command_id,COMMAND_B)))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;(*calls)++;return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED; }

static int exists(root,relative)
const char *root,*relative;
{ char path[PATH_MAX];struct stat status;return !path_join(path,sizeof(path),root,relative)&&!lstat(path,&status); }

static int artifact_exists_command(root,command)
const char *root,*command;
{ char relative[128];int count=snprintf(relative,sizeof(relative),"%s/%s.player-snapshot-v1",CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,command);return count>0&&(size_t)count<sizeof(relative)&&exists(root,relative); }

static int artifact_exists(root)
const char *root;
{ return artifact_exists_command(root,COMMAND); }

static int manifest_exists_command(root,command)
const char *root,*command;
{
    char relative[128];int count=snprintf(relative,sizeof(relative),"%s/%s.manifest",
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,command);
    return count>0&&(size_t)count<sizeof(relative)&&exists(root,relative);
}

static int snapshot_path(root,relative,output)
const char *root,*relative;struct stat *output;
{ char path[PATH_MAX];return path_join(path,sizeof(path),root,relative)||lstat(path,output)?-1:0; }

static int same_path(root,relative,before)
const char *root,*relative;const struct stat *before;
{
    struct stat after;
    return before&&snapshot_path(root,relative,&after)==0&&
        before->st_dev==after.st_dev&&before->st_ino==after.st_ino&&
        before->st_mode==after.st_mode&&before->st_nlink==after.st_nlink&&
        before->st_size==after.st_size;
}

static int write_conflicting_manifest(root,command)
const char *root,*command;
{
    char directory[PATH_MAX];int fd,result;
    uint8_t *snapshot=0;size_t length=0;
    character_player_snapshot_v1_artifact_metadata key,artifact;
    character_snapshot_shadow_outbox_manifest manifest;
    if(path_join(directory,sizeof(directory),root,
       CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY))return-1;
    fd=open(directory,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(fd<0)return-1;
    memset(&key,0,sizeof(key));memset(&artifact,0,sizeof(artifact));
    memset(&manifest,0,sizeof(manifest));strcpy(key.command_id,command);
    result=character_player_snapshot_v1_artifact_load(fd,&key,&artifact,
        &snapshot,&length);
    character_player_snapshot_v1_artifact_free(snapshot);
    if(result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK) { close(fd);return-1; }
    strcpy(manifest.world_id,artifact.world_id);
    strcpy(manifest.character_id,artifact.character_id);
    strcpy(manifest.command_id,artifact.command_id);
    strcpy(manifest.canonical_name_hex,artifact.canonical_name_hex);
    strcpy(manifest.request_sha256,artifact.request_sha256);
    strcpy(manifest.post_sha256,artifact.source_post_sha256);
    strcpy(manifest.writer_instance_id,artifact.writer_instance_id);
    strcpy(manifest.snapshot_format,CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT);
    manifest.writer_epoch=artifact.writer_epoch;
    manifest.writer_revision=artifact.writer_revision+1U;
    manifest.storage_format=artifact.storage_format;
    manifest.snapshot_octets=artifact.source_octets;
    result=character_snapshot_shadow_outbox_write(fd,&manifest);
    if(close(fd))return-1;
    return result==CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK?0:-1;
}

static int handoff_exists(root,command,suffix)
const char *root,*command,*suffix;
{
    char relative[160];int count=snprintf(relative,sizeof(relative),"%s/%s%s",
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,command,suffix);
    return count>0&&(size_t)count<sizeof(relative)&&exists(root,relative);
}

static int handoff_unlink_and_sync(root,command,suffix)
const char *root,*command,*suffix;
{
    char directory[PATH_MAX],leaf_name[128];
    int directory_fd,length;
    if(path_join(directory,sizeof(directory),root,
       CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY))return -1;
    length=snprintf(leaf_name,sizeof(leaf_name),"%s%s",command,suffix);
    if(length<0||(size_t)length>=sizeof(leaf_name))return -1;
    directory_fd=open(directory,O_RDONLY|O_DIRECTORY|O_NOFOLLOW);
    if(directory_fd<0)return -1;
    if(unlinkat(directory_fd,leaf_name,0)) { close(directory_fd);return -1; }
    if(fsync(directory_fd)) { close(directory_fd);return -1; }
    if(close(directory_fd))return -1;
    return 0;
}

static int handoff_leaf_and_sync(root,command,suffix,bytes,length)
const char *root,*command,*suffix;const void *bytes;size_t length;
{
    char relative[160],directory[PATH_MAX];int directory_fd,count;
    count=snprintf(relative,sizeof(relative),"%s/%s%s",
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,command,suffix);
    if(count<0||(size_t)count>=sizeof(relative)||leaf(root,relative,bytes,length)||
       path_join(directory,sizeof(directory),root,
       CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY))return -1;
    directory_fd=open(directory,O_RDONLY|O_DIRECTORY|O_NOFOLLOW);
    if(directory_fd<0)return -1;
    if(fsync(directory_fd)||close(directory_fd))return -1;
    return 0;
}

static int handoff_source_is_private_copy(root,command)
const char *root,*command;
{
    char stage[128],source[160],stage_path[PATH_MAX],source_path[PATH_MAX];
    struct stat stage_status,source_status;
    int stage_length,source_length;
    stage_length=snprintf(stage,sizeof(stage),"character-save-stage/%s.stage",command);
    source_length=snprintf(source,sizeof(source),"%s/%s.source",
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,command);
    if(stage_length<0||(size_t)stage_length>=sizeof(stage)||source_length<0||
       (size_t)source_length>=sizeof(source)||
       path_join(stage_path,sizeof(stage_path),root,stage)||
       path_join(source_path,sizeof(source_path),root,source)||
       lstat(stage_path,&stage_status)||lstat(source_path,&source_status))return 0;
    return (S_ISREG(stage_status.st_mode)&&S_ISREG(source_status.st_mode)&&
        stage_status.st_nlink==1&&source_status.st_nlink==1)&&
        (stage_status.st_dev!=source_status.st_dev||
         stage_status.st_ino!=source_status.st_ino);
}

static int artifact_source_hash(root,command,hash)
const char *root,*command; char hash[65];
{
    char directory[PATH_MAX];int fd,result;uint8_t *snapshot=0;size_t length=0;
    character_player_snapshot_v1_artifact_metadata key,metadata;
    if(path_join(directory,sizeof(directory),root,
       CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY))return-1;
    fd=open(directory,O_RDONLY|O_DIRECTORY|O_NOFOLLOW);
    if(fd<0)return-1;
    memset(&key,0,sizeof(key));memset(&metadata,0,sizeof(metadata));
    strcpy(key.command_id,command);
    result=character_player_snapshot_v1_artifact_load(fd,&key,&metadata,
        &snapshot,&length);
    if(result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK)
        memcpy(hash,metadata.source_post_sha256,sizeof(metadata.source_post_sha256));
    character_player_snapshot_v1_artifact_free(snapshot);
    if(close(fd))return-1;
    return result==CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK?0:-1;
}

static int fill_handoff_capacity(root)
const char *root;
{
    char relative[128],command[37];
    unsigned int index;
    if(make_dir(root,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY))return -1;
    for(index=0;index<CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING;index++) {
        if(snprintf(command,sizeof(command),"30000000-0000-4000-8000-%012u",index)!=36||
           snprintf(relative,sizeof(relative),"%s/%s.handoff",
           CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,command)<0||
           leaf(root,relative,"x",1))return -1;
    }
    return 0;
}

/* Poison evidence is durable queue state too.  Exercise every fixed poison
 * spelling so a quarantined identity cannot silently make room for another
 * 64MiB private source. */
static int fill_handoff_poison_capacity(root)
const char *root;
{
    static const char *suffixes[]={
        ".handoff.poison", ".handoff.tmp.poison", ".source.poison",
        ".source.tmp.poison", ".source.consumed.poison",
        ".source.consumed.tmp.poison", ".poison"
    };
    char relative[160],command[37];
    unsigned int index;
    if(make_dir(root,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY))return -1;
    for(index=0;index<CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING;index++) {
        if(snprintf(command,sizeof(command),"40000000-0000-4000-8000-%012u",index)!=36||
           snprintf(relative,sizeof(relative),"%s/%s%s",
           CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,command,
           suffixes[index%(sizeof(suffixes)/sizeof(suffixes[0]))])<0||
           leaf(root,relative,"poison=1\n",9))return -1;
    }
    return 0;
}

static int setup(root,writer,capture,handoff,decoder,label)
char root[PATH_MAX]; character_save_journal_v2_writer_context *writer;
character_player_snapshot_v1_capture *capture;
character_player_snapshot_v1_handoff *handoff; decode_fixture *decoder;
const char *label;
{
    if(make_root(root,label)||seed(root)||prepare(root))return-1;
    memset(writer,0,sizeof(*writer));if(character_save_journal_v2_writer_open(root,WORLD,writer))return-1;
    memset(decoder,0,sizeof(*decoder));
    character_player_snapshot_v1_capture_init(capture,decode,decoder,release,decoder);
    character_player_snapshot_v1_handoff_init(handoff,capture);
    return 0;
}

static int teardown(root,writer)
const char *root; character_save_journal_v2_writer_context *writer;
{ return character_save_journal_v2_writer_close(writer)||remove_tree(root); }

static int test_stopped_consumer_does_not_gate_publish_or_ack(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,observed,published,acked,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"stopped"))return 1;
    decoder.block=1;
    alarm(2);
    observed=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND);
    published=character_save_journal_v2_publish_recover(&writer,COMMAND);
    acked=character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls);
    failed+=expect(observed==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==0&&
        published==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1,
        "a stopped heavy consumer must not block the durable handoff, legacy publish, or ACK");
    if(observed!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK||
       published!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK||
       acked!=CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED)
        fprintf(stderr,"handoff trace: observe=%d publish=%d ack=%d decoder=%d receipt=%d\n",
            observed,published,acked,decoder.calls,receipt_calls);
    alarm(0);
    failed+=expect(exists(root,"player/66/M3alpha")&&
        exists(root,"character-save-journal/10000000-0000-4000-8000-000000000001.published")&&
        exists(root,"character-save-journal/10000000-0000-4000-8000-000000000001.acked")&&
        !artifact_exists(root),
        "publish and ACK must finish before a separately scheduled consumer captures anything");
    decoder.block=0;
    failed+=expect(character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "a durable handoff must survive reopening the held writer after the publisher exits");
    observed=character_player_snapshot_v1_handoff_drain(&handoff,&writer,1);
    failed+=expect(observed==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&decoder.releases==1&&
        capture.report.recorded==1&&artifact_exists(root)&&
        handoff.report.consumed==1,
        "the explicit consumer must later create the one detached artifact");
    if(observed!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        fprintf(stderr,"handoff drain trace: result=%d capture=%d report=%d calls=%d consumed=%llu\n",
            observed,capture.report.last_result,handoff.report.last_result,decoder.calls,
            (unsigned long long)handoff.report.consumed);
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: the ordinary handoff consumer predates receipt pairing.  It must
 * capture and clean its durable reservation without waiting for an ACK marker
 * or creating a relay manifest. */
static int test_default_consumer_captures_and_cleans_up_without_ack(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"default-cleanup"))return 1;
    failed+=expect(!handoff.receipt_pair&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        decoder.releases==1&&artifact_exists_command(root,COMMAND)&&
        !manifest_exists_command(root,COMMAND)&&
        !handoff_exists(root,COMMAND,".handoff")&&
        !handoff_exists(root,COMMAND,".source")&&
        !exists(root,"character-save-journal/10000000-0000-4000-8000-000000000001.acked"),
        "the default consumer must preserve capture-to-cleanup without ACK or relay pairing");
    if(teardown(root,&writer))failed++;
    return failed;
}

static int test_crash_cutpoints_retry_without_wrong_duplicate(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"cutpoints"))return 1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_LINK);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        !exists(root,"character-player-snapshot-v1-handoff/10000000-0000-4000-8000-000000000001.handoff")&&
        handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".handoff.tmp")&&decoder.calls==0,
        "a token-final failure must retain a source-only private reservation and no visible token");
    failed+=expect(character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists(root)&&
        !handoff_exists(root,COMMAND,".source")&&
        !handoff_exists(root,COMMAND,".handoff")&&
        !handoff_exists(root,COMMAND,".handoff.tmp"),
        "restart must reconstruct a source-only reservation from durable PREPARED metadata without head-of-line corruption");
    if(teardown(root,&writer))return failed+1;

    if(setup(root,&writer,&capture,&handoff,&decoder,"source-temp-replay"))return failed+1;
    receipt_calls=0;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_RENAME);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        !handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".source.tmp")&&
        !handoff_exists(root,COMMAND,".handoff"),
        "a source-rename failure must leave only a bounded self-identifying private source temporary");
    failed+=expect(character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists(root)&&
        !handoff_exists(root,COMMAND,".source")&&
        !handoff_exists(root,COMMAND,".source.tmp"),
        "a source temporary must be rename-promoted from prepared metadata and consumed after restart, never block the queue");
    if(teardown(root,&writer))return failed+1;

    if(setup(root,&writer,&capture,&handoff,&decoder,"pre-publication-retry"))return failed+1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&capture.report.recorded==1&&
        artifact_exists(root),
        "re-observing immutable PREPARED evidence after a pre-publication crash must not lose capture work");
    if(teardown(root,&writer))return failed+1;

    if(setup(root,&writer,&capture,&handoff,&decoder,"duplicate"))return failed+1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_DIRECTORY_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "a post-publication crash cutpoint must converge to the same durable handoff record");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_UNLINK);
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&capture.report.recorded==1&&
        artifact_exists(root)&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&capture.report.recorded==1&&
        capture.report.exact_retries==0&&decoder.calls==1&&decoder.releases==1&&
        handoff.report.consumed==1,
        "a consumer crash after artifact durability must resume durable consumed cleanup without decoding or duplicating the artifact");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED before immutable source copies: after A has published, B can replace the
 * same live leaf before the idle consumer runs.  A's token must still produce
 * A's own artifact, and it must not strand B behind a SOURCE failure. */
static int test_same_character_successors_keep_distinct_immutable_sources(void)
{
    char root[PATH_MAX],hash_a[65],hash_b[65],artifact_a[65],artifact_b[65];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,observed_a,private_a,published_a,acked_a,prepared_b,
        observed_b,private_b,
        published_b,acked_b,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"successors"))return 1;
    if(hash_bytes(root,RAW,sizeof(RAW)-1,hash_a)||
       hash_bytes(root,RAW_B,sizeof(RAW_B)-1,hash_b))return teardown(root,&writer)?2:1;
    observed_a=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND);
    private_a=handoff_source_is_private_copy(root,COMMAND);
    published_a=character_save_journal_v2_publish_recover(&writer,COMMAND);
    acked_a=character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls);
    prepared_b=prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2);
    observed_b=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B);
    private_b=handoff_source_is_private_copy(root,COMMAND_B);
    published_b=character_save_journal_v2_publish_recover(&writer,COMMAND_B);
    acked_b=character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls);
    failed+=expect(observed_a==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&private_a&&
        published_a==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked_a==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&prepared_b==0&&
        observed_b==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&private_b&&
        published_b==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked_b==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2,
        "two same-character saves must retain distinct private copies while each legacy stage remains one-linked, then publish and ACK without drain");
    if(observed_a!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK||
       published_a!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK||
       acked_a!=CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED||prepared_b||
       observed_b!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK||
       published_b!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK||
       acked_b!=CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED)
        fprintf(stderr,"successor trace: A=%d/%d/%d B=%d/%d/%d/%d receipts=%d\n",
            observed_a,published_a,acked_a,prepared_b,observed_b,published_b,
            acked_b,receipt_calls);
    failed+=expect(character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,2)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==2&&
        handoff.report.consumed==2&&
        artifact_source_hash(root,COMMAND,artifact_a)==0&&
        artifact_source_hash(root,COMMAND_B,artifact_b)==0&&
        !strcmp(artifact_a,hash_a)&&!strcmp(artifact_b,hash_b)&&
        strcmp(artifact_a,artifact_b)&&
        !handoff_exists(root,COMMAND,".handoff")&&
        !handoff_exists(root,COMMAND_B,".handoff"),
        "restart must drain both successor tokens into distinct source-verified artifacts without head-of-line blocking");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* A final name visible after its parent fsync failure is not durable evidence.
 * The retry must retry that fsync before it can report an exact handoff retry. */
static int test_final_token_fsync_exact_retry_repairs_parent_durability(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"final-fsync-retry"))return 1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_DIRECTORY_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        handoff_exists(root,COMMAND,".handoff"),
        "a failed final-token directory fsync leaves a retryable visible token");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_DIRECTORY_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY,
        "an exact retry must repair the final-token parent fsync before reporting success");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* Source and queue creation carry their own parent-directory durability.  A
 * source-only crash must finish the same immutable reservation on replay. */
static int test_source_and_queue_creation_fsync_retries(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"source-fsync-retry"))return 1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_DIRECTORY_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        handoff_exists(root,COMMAND,".source")&&
        !handoff_exists(root,COMMAND,".handoff")&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK,
        "a source-rename fsync failure must replay the same source reservation before publishing its token");
    if(teardown(root,&writer))return failed+1;

    if(setup(root,&writer,&capture,&handoff,&decoder,"queue-create-fsync"))return failed+1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_QUEUE_CREATE_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK,
        "a queue-creation parent fsync failure must be retried before a handoff is accepted");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_QUEUE_CREATE_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY,
        "an exact token retry must revalidate the queue creation parent durability");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* Once the artifact is durable, cleanup is driven by a separately durable
 * consumed record.  This specifically exercises token-absent/source-absent
 * replay after the source unlink reached the namespace but its parent fsync
 * did not. */
static int test_consumed_cleanup_fsync_retry(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"cleanup-fsync"))return 1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "cleanup replay fixture must durably enqueue and publish its source");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_DIRECTORY_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&capture.report.recorded==1&&
        artifact_exists(root)&&!handoff_exists(root,COMMAND,".handoff")&&
        !handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".source.consumed"),
        "a source cleanup fsync failure must retain a durable consumed record after token/source removal");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&capture.report.recorded==1&&
        capture.report.exact_retries==0&&decoder.calls==1&&decoder.releases==1&&
        handoff.report.consumed==1&&
        !handoff_exists(root,COMMAND,".source.consumed"),
        "a restartable cleanup pass must re-fsync absent token/source state and remove its consumed record without recapturing");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* This is the otherwise dangerous order: the artifact and consumed record
 * are durable, but a crash has left the source absent while the token still
 * exists.  The consumed record must make it ordinary idempotent cleanup,
 * rather than a corrupt head-of-line capture failure. */
static int test_source_absent_token_present_consumed_replay(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"source-absent-token"))return 1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "source-absent replay fixture must enqueue before legacy publication");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_UNLINK);
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&capture.report.recorded==1&&
        artifact_exists(root)&&handoff_exists(root,COMMAND,".handoff")&&
        handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".source.consumed"),
        "a durable artifact must publish its consumed record before cleanup starts");
    failed+=expect(handoff_unlink_and_sync(root,COMMAND,".source")==0&&
        !handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".handoff")&&
        handoff_exists(root,COMMAND,".source.consumed"),
        "test cutpoint must leave source absent and token present after durable artifact");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&capture.report.recorded==1&&
        capture.report.exact_retries==0&&decoder.calls==1&&decoder.releases==1&&
        handoff.report.consumed==1&&!handoff_exists(root,COMMAND,".handoff")&&
        !handoff_exists(root,COMMAND,".source.consumed"),
        "source-absent/token-present replay must consume durable cleanup state without a second capture");
    if(teardown(root,&writer))failed++;
    return failed;
}

static int test_full_handoff_remains_diagnostic(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,observed,published,acked,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"full")||
       fill_handoff_capacity(root))return 1;
    decoder.block=1;
    alarm(2);
    observed=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND);
    published=character_save_journal_v2_publish_recover(&writer,COMMAND);
    acked=character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls);
    alarm(0);
    failed+=expect(observed==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_FULL&&
        handoff.report.full==1&&decoder.calls==0&&
        published==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1,
        "a bounded full handoff must be diagnostic while legacy publish and ACK remain live");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: poisoned leaves were excluded from the identity count, so 128 durable
 * quarantine records no longer exerted backpressure.  GREEN: poison consumes
 * the same bounded slot, yet a FULL observer remains only a legacy-independent
 * diagnostic. */
static int test_poison_capacity_remains_full_without_gating_publish_or_ack(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,observed,published,acked,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"poison-full")||
       fill_handoff_poison_capacity(root))return 1;
    observed=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND);
    published=character_save_journal_v2_publish_recover(&writer,COMMAND);
    acked=character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls);
    failed+=expect(observed==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_FULL&&
        handoff.report.full==1&&decoder.calls==0&&
        published==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1&&
        !artifact_exists(root),
        "128 durable poison identities must keep handoff capacity full without gating legacy publish or ACK");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: an I/O interruption can leave a short .source.tmp.  After legacy
 * publish/ACK removes stage, drain used to call it CORRUPT forever and block
 * B.  GREEN: the first bounded tick drops only A without an artifact, and B
 * captures on the next limit=1 tick after restart. */
static int test_partial_source_temp_after_publish_drops_only_that_snapshot(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"partial-source-drop"))return 1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_RENAME);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        handoff_exists(root,COMMAND,".source.tmp")&&
        handoff_unlink_and_sync(root,COMMAND,".source.tmp")==0&&
        handoff_leaf_and_sync(root,COMMAND,".source.tmp","partial",7)==0&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==1,
        "a partial private source temporary must coexist with independent legacy publish and ACK");
    failed+=expect(prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        handoff_source_is_private_copy(root,COMMAND_B)&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "B must retain its own one-linked-stage private source before the restart drain");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DROPPED&&handoff.report.dropped==1&&
        handoff.report.failed>=2&&decoder.calls==0&&capture.report.recorded==0&&
        !artifact_exists_command(root,COMMAND)&&
        !handoff_exists(root,COMMAND,".source.tmp")&&
        !handoff_exists(root,COMMAND,".handoff"),
        "stage-absent partial A must be diagnosed and dropped, never reported as a captured artifact");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND)&&
        !handoff_exists(root,COMMAND_B,".handoff")&&
        !handoff_exists(root,COMMAND_B,".source"),
        "the later B identity must capture exactly once on its next bounded tick");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: a malformed A.handoff.tmp made consumed cleanup fail after removing
 * A's final token, leaving the same local poison ahead of every later token.
 * GREEN: cleanup durably reclaims that fixed private temp, reports the
 * diagnostic, and B advances on the following limit=1 tick. */
static int test_malformed_token_temp_cleanup_does_not_head_of_line_block(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"malformed-temp-cleanup"))return 1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "cleanup poison fixture must enqueue and publish A");
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_UNLINK);
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&capture.report.recorded==1&&
        handoff_exists(root,COMMAND,".source.consumed")&&
        handoff_leaf_and_sync(root,COMMAND,".handoff.tmp","malformed\n",10)==0&&
        prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        handoff_source_is_private_copy(root,COMMAND_B)&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "a consumed-cleanup replay plus B must preserve B's private non-hardlinked source");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&handoff.report.reclaimed_poison==1&&
        handoff.report.failed>=2&&handoff.report.consumed==1&&
        !handoff_exists(root,COMMAND,".handoff.tmp")&&
        !handoff_exists(root,COMMAND,".source.consumed")&&
        artifact_exists_command(root,COMMAND),
        "malformed local token temp must be durably reclaimed while preserving a failure diagnostic");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==2&&
        capture.report.recorded==2&&artifact_exists_command(root,COMMAND)&&
        artifact_exists_command(root,COMMAND_B)&&
        !handoff_exists(root,COMMAND_B,".handoff")&&
        !handoff_exists(root,COMMAND_B,".source"),
        "valid B must pass the reclaimed local poison on the next bounded tick without duplicate artifacts");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: a safe malformed A token could survive a later scan and create a
 * duplicate.  GREEN: it consumes one bounded cleanup slot, is quarantined
 * without capture, and B is the sole artifact on the following idle tick. */
static int test_safe_poison_skips_to_later_valid_snapshot_without_duplicate(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"safe-poison-skip"))return 1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        handoff_unlink_and_sync(root,COMMAND,".handoff")==0&&
        handoff_leaf_and_sync(root,COMMAND,".handoff","malformed\n",10)==0&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "safe poison fixture must leave legacy publish and ACK independent before the idle consumer tick");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&
        handoff.report.quarantined_poison==1&&handoff.report.reclaimed_poison==1&&
        decoder.calls==0&&
        capture.report.recorded==0&&!artifact_exists_command(root,COMMAND)&&
        !artifact_exists_command(root,COMMAND_B)&&
        handoff_exists(root,COMMAND,".handoff.poison")&&
        handoff_exists(root,COMMAND,".source.poison"),
        "a reclaimable A poison must be durably quarantined without creating an artifact");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND),
        "a quarantined safe poison must stay out of later scans so B captures exactly once");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND),
        "a safe poison and completed B must not re-enter scanning or duplicate B's artifact");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: an externally linked A source could be accepted as an ordinary private
 * source, or could block B forever because it was unsafe to reclaim.  GREEN:
 * the unsafe name remains untouched with durable poison evidence, while B
 * advances during the same explicit limit=1 consumer tick. */
static int test_external_hardlinked_source_skips_to_later_valid_snapshot(void)
{
    char root[PATH_MAX],source[PATH_MAX],alias[PATH_MAX];
    struct stat source_status,alias_status;
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"unsafe-source-skip"))return 1;
    if(path_join(source,sizeof(source),root,
       "character-player-snapshot-v1-handoff/10000000-0000-4000-8000-000000000001.source")||
       path_join(alias,sizeof(alias),root,"external-a-source-link"))return teardown(root,&writer)?2:1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        link(source,alias)==0&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "externally linked A must coexist with independent legacy publication and a valid B handoff");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&
        handoff.report.quarantined_poison==1&&decoder.calls==1&&
        capture.report.recorded==1&&!artifact_exists_command(root,COMMAND)&&
        artifact_exists_command(root,COMMAND_B)&&handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".handoff.poison")&&
        lstat(source,&source_status)==0&&lstat(alias,&alias_status)==0&&
        source_status.st_dev==alias_status.st_dev&&source_status.st_ino==alias_status.st_ino&&
        source_status.st_nlink==2,
        "unsafe A must remain unlinked from capture and B must progress without deleting external evidence");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND),
        "durably marked unsafe A must not re-enter scanning or duplicate B's artifact");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* The only legal two-name interval is this queue's own promotion.  An
 * externally linked source temporary must be poison, not a recoverable short
 * copy, and it must not consume B's limit=1 consumer turn. */
static int test_external_hardlinked_source_temp_skips_to_later_valid_snapshot(void)
{
    char root[PATH_MAX],temp[PATH_MAX],alias[PATH_MAX];
    struct stat temp_status,alias_status;
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"unsafe-temp-skip"))return 1;
    if(path_join(temp,sizeof(temp),root,
       "character-player-snapshot-v1-handoff/10000000-0000-4000-8000-000000000001.source.tmp")||
       path_join(alias,sizeof(alias),root,"external-a-source-temp-link"))return teardown(root,&writer)?2:1;
    character_player_snapshot_v1_handoff_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_RENAME);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&
        handoff_exists(root,COMMAND,".source.tmp")&&link(temp,alias)==0&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "an externally linked private temp must remain independent of legacy publication and B enqueue");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&
        handoff.report.quarantined_poison==1&&decoder.calls==1&&
        capture.report.recorded==1&&!artifact_exists_command(root,COMMAND)&&
        artifact_exists_command(root,COMMAND_B)&&handoff_exists(root,COMMAND,".source.tmp")&&
        handoff_exists(root,COMMAND,".poison")&&lstat(temp,&temp_status)==0&&
        lstat(alias,&alias_status)==0&&temp_status.st_dev==alias_status.st_dev&&
        temp_status.st_ino==alias_status.st_ino&&temp_status.st_nlink==2,
        "an externally hardlinked temp must never be promoted or captured, while B progresses in one bounded scan");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND),
        "a poison-marked source temp must remain out of future scans and cannot duplicate B");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: source and its fixed temporary can be made into an untrusted two-link
 * pair after a normal reservation.  That exact shape used to look like this
 * queue's interrupted link/unlink promotion, so drain silently captured A.
 * GREEN: neither name belongs to recovery; both remain inspectable behind one
 * generic poison marker while valid B uses the same limit=1 consumer turn. */
static int test_untrusted_source_and_temp_pair_skips_to_later_valid_snapshot(void)
{
    char root[PATH_MAX],source[PATH_MAX],temp[PATH_MAX];
    struct stat source_status,temp_status;
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"unsafe-source-pair"))return 1;
    if(path_join(source,sizeof(source),root,
       "character-player-snapshot-v1-handoff/10000000-0000-4000-8000-000000000001.source")||
       path_join(temp,sizeof(temp),root,
       "character-player-snapshot-v1-handoff/10000000-0000-4000-8000-000000000001.source.tmp"))
        return teardown(root,&writer)?2:1;
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        link(source,temp)==0&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        prepare_command(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,RAW,sizeof(RAW)-1,2)==0&&
        character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND_B)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&receipt_calls==2&&
        character_save_journal_v2_writer_close(&writer)==0&&
        character_save_journal_v2_writer_open(root,WORLD,&writer)==0,
        "an untrusted source/temp pair must not affect independent legacy publish, ACK, or B enqueue");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&
        handoff.report.quarantined_poison==1&&decoder.calls==1&&
        capture.report.recorded==1&&!artifact_exists_command(root,COMMAND)&&
        artifact_exists_command(root,COMMAND_B)&&handoff_exists(root,COMMAND,".source")&&
        handoff_exists(root,COMMAND,".source.tmp")&&handoff_exists(root,COMMAND,".poison")&&
        lstat(source,&source_status)==0&&lstat(temp,&temp_status)==0&&
        source_status.st_dev==temp_status.st_dev&&source_status.st_ino==temp_status.st_ino&&
        source_status.st_nlink==2&&temp_status.st_nlink==2,
        "the two unsafe source names must remain untouched, be generically marked, and let B capture exactly once in one bounded tick");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        capture.report.recorded==1&&artifact_exists_command(root,COMMAND_B)&&
        !artifact_exists_command(root,COMMAND),
        "the generic poison marker must keep the untrusted pair out of later scans");
    if(teardown(root,&writer))failed++;
    return failed;
}

/* RED: an already durable artifact used to be consumed before its matching
 * DB_ACKED marker could publish its same-directory relay manifest.  A pending
 * A must remain retryable while an ACKED B advances in the same bounded scan;
 * after A becomes ACKED the exact artifact is paired without re-decoding. */
static int test_ack_verified_receipt_pair_retries_without_head_of_line_blocking(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    character_save_journal_v2_receipt_result receipt_result;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"ack-pair"))return 1;
    character_player_snapshot_v1_handoff_enable_receipt_pair(&handoff,
        character_player_snapshot_v1_receipt_pair_commit);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        artifact_exists_command(root,COMMAND)&&!manifest_exists_command(root,COMMAND)&&
        handoff_exists(root,COMMAND,".handoff")&&handoff_exists(root,COMMAND,".source"),
        "an artifact without DB_ACKED must retain its token and source without a manifest");
    { int prepared_b=prepare_command_other(root,COMMAND_B,RAW_B,sizeof(RAW_B)-1,2);
    int observed_b=character_player_snapshot_v1_handoff_observe(&handoff,&writer,COMMAND_B);
    int published_b=character_save_journal_v2_publish_recover(&writer,COMMAND_B);
    int acked_b=character_save_journal_v2_ack(&writer,COMMAND_B,receipt,&receipt_calls);
    decoder.name_b=1;
    int drained_b=character_player_snapshot_v1_handoff_drain(&handoff,&writer,1);
    failed+=expect(prepared_b==0&&observed_b==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        published_b==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        acked_b==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&drained_b==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==2&&
        handoff_exists(root,COMMAND,".handoff")&&handoff_exists(root,COMMAND,".source")&&
        !manifest_exists_command(root,COMMAND)&&artifact_exists_command(root,COMMAND_B)&&
        manifest_exists_command(root,COMMAND_B)&&!handoff_exists(root,COMMAND_B,".handoff")&&
        !handoff_exists(root,COMMAND_B,".source"),
        "a pending A must not spend B's bounded drain slot once B is ACKED"); }
    receipt_result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    character_save_journal_v2_ack_faults_for_test(0,0,0,1,0,0,0);
    { int acked_a=character_save_journal_v2_ack(&writer,COMMAND,receipt,
        &receipt_result);int drained_a=character_player_snapshot_v1_handoff_drain(
        &handoff,&writer,1);
    failed+=expect(acked_a==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&
        drained_a==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==2&&
        !manifest_exists_command(root,COMMAND)&&handoff_exists(root,COMMAND,".handoff")&&
        handoff_exists(root,COMMAND,".source"),
        "local-incomplete ACK evidence must remain pending without cleanup"); }
    failed+=expect(character_save_journal_v2_ack(&writer,COMMAND,receipt,
        &receipt_result)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED,
        "exact DB retry must complete A's local ACK marker");
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_FSYNC);
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR&&decoder.calls==2&&
        artifact_exists_command(root,COMMAND)&&manifest_exists_command(root,COMMAND)&&
        handoff_exists(root,COMMAND,".handoff")&&handoff_exists(root,COMMAND,".source"),
        "pair fsync failure must retain artifact, manifest, token, and source for retry");
    failed+=expect(character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==2&&
        artifact_exists_command(root,COMMAND)&&manifest_exists_command(root,COMMAND)&&
        !handoff_exists(root,COMMAND,".handoff")&&!handoff_exists(root,COMMAND,".source"),
        "ACKED A must pair its exact artifact and clean up without another decode");
    if(teardown(root,&writer))failed++;
    return failed;
}

static int test_ack_pair_conflict_and_corruption_preserve_immutable_evidence(void)
{
    char root[PATH_MAX],relative[128],artifact_relative[128];
    struct stat artifact_before,manifest_before;
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_capture capture;
    character_player_snapshot_v1_handoff handoff;
    decode_fixture decoder;
    int receipt_calls=0,failed=0;

    if(setup(root,&writer,&capture,&handoff,&decoder,"ack-pair-conflict"))return 1;
    character_player_snapshot_v1_handoff_enable_receipt_pair(&handoff,
        character_player_snapshot_v1_receipt_pair_commit);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        write_conflicting_manifest(root,COMMAND)==0&&
        character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_calls)==
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        snprintf(relative,sizeof(relative),"%s/%s.player-snapshot-v1",
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,COMMAND)>0&&
        snapshot_path(root,relative,&artifact_before)==0&&
        snprintf(artifact_relative,sizeof(artifact_relative),"%s",relative)>0&&
        snprintf(relative,sizeof(relative),"%s/%s.manifest",
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,COMMAND)>0&&
        snapshot_path(root,relative,&manifest_before)==0&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&decoder.calls==1&&
        same_path(root,artifact_relative,&artifact_before)&&
        same_path(root,relative,&manifest_before)&&artifact_exists_command(root,COMMAND)&&
        handoff_exists(root,COMMAND,".handoff")&&handoff_exists(root,COMMAND,".source"),
        "conflicting receipt-pair evidence must freeze without cleanup or replacement");
    if(teardown(root,&writer))return failed+1;

    if(setup(root,&writer,&capture,&handoff,&decoder,"ack-pair-corrupt"))return failed+1;
    character_player_snapshot_v1_handoff_enable_receipt_pair(&handoff,
        character_player_snapshot_v1_receipt_pair_commit);
    failed+=expect(character_player_snapshot_v1_handoff_observe(&handoff,&writer,
        COMMAND)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
        character_save_journal_v2_publish_recover(&writer,COMMAND)==
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_handoff_drain(&handoff,&writer,1)==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&decoder.calls==1&&
        leaf(root,"character-player-snapshot-v1-outbox/10000000-0000-4000-8000-000000000001.manifest",
        "corrupt\n",8)==0&&character_save_journal_v2_ack(&writer,COMMAND,
        receipt,&receipt_calls)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&
        snapshot_path(root,"character-player-snapshot-v1-outbox/10000000-0000-4000-8000-000000000001.manifest",
        &manifest_before)==0&&character_player_snapshot_v1_handoff_drain(&handoff,
        &writer,1)==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT&&decoder.calls==1&&
        same_path(root,"character-player-snapshot-v1-outbox/10000000-0000-4000-8000-000000000001.manifest",
        &manifest_before)&&artifact_exists_command(root,COMMAND)&&handoff_exists(root,
        COMMAND,".handoff")&&handoff_exists(root,COMMAND,".source"),
        "corrupt receipt-pair evidence must remain immutable and unconsumed");
    if(teardown(root,&writer))failed++;
    return failed;
}

int main(void)
{
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    return test_stopped_consumer_does_not_gate_publish_or_ack()|
        test_default_consumer_captures_and_cleans_up_without_ack()|
        test_crash_cutpoints_retry_without_wrong_duplicate()|
        test_same_character_successors_keep_distinct_immutable_sources()|
        test_final_token_fsync_exact_retry_repairs_parent_durability()|
        test_source_and_queue_creation_fsync_retries()|
        test_consumed_cleanup_fsync_retry()|
        test_source_absent_token_present_consumed_replay()|
        test_full_handoff_remains_diagnostic()|
        test_poison_capacity_remains_full_without_gating_publish_or_ack()|
        test_partial_source_temp_after_publish_drops_only_that_snapshot()|
        test_malformed_token_temp_cleanup_does_not_head_of_line_block()|
        test_safe_poison_skips_to_later_valid_snapshot_without_duplicate()|
        test_external_hardlinked_source_skips_to_later_valid_snapshot()|
        test_external_hardlinked_source_temp_skips_to_later_valid_snapshot()|
        test_untrusted_source_and_temp_pair_skips_to_later_valid_snapshot()|
        test_ack_verified_receipt_pair_retries_without_head_of_line_blocking()|
        test_ack_pair_conflict_and_corruption_preserve_immutable_evidence();
}
