/* TDD boundary: an artifact gets its legacy manifest only after exact local
 * DB_ACKED evidence.  This remains an explicit test-only consumer. */
#include "character_player_snapshot_v1_receipt_pair.h"

#include "character_save_journal_v2.h"
#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_publish.h"
#include "character_snapshot_shadow_outbox.h"
#include "cdto_v1.h"
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

static const char WORLD[]="m3-handoff";
static const char INSTANCE[]="11111111-1111-4111-8111-111111111111";
static const char CHARACTER[]="90000000-0000-4000-8000-000000000006";
static const char COMMAND[]="10000000-0000-4000-8000-000000000001";
static const unsigned char NAME[]="M3alpha";
static const unsigned char STAGE[]="legacy-player-stage";

static int expect(ok, text)
int ok; const char *text;
{ if(ok) return 0; fprintf(stderr,"character_player_snapshot_v1_receipt_pair_test: %s\n",text); return 1; }

static int write_all(fd, bytes, length)
int fd; const void *bytes; size_t length;
{ const unsigned char *cursor=(const unsigned char *)bytes; ssize_t count; while(length) { count=write(fd,cursor,length); if(count<0&&errno==EINTR) continue; if(count<=0) return -1; cursor+=count; length-=(size_t)count; } return 0; }

static int join(output, capacity, root, relative)
char *output; size_t capacity; const char *root,*relative;
{ int count=snprintf(output,capacity,"%s/%s",root,relative); return count<0||(size_t)count>=capacity?-1:0; }

static int make_dir(root, relative)
const char *root,*relative;
{ char path[PATH_MAX]; return join(path,sizeof(path),root,relative)||mkdir(path,0700)?-1:0; }

static int make_root(root)
char root[PATH_MAX];
{ char temporary[PATH_MAX];int count;if(!realpath("/tmp",temporary))return-1;count=snprintf(root,PATH_MAX,"%s/muhan-player-snapshot-receipt-pair-XXXXXX",temporary);return count<0||count>=PATH_MAX||!mkdtemp(root)?-1:0; }

static int leaf(root, relative, bytes, length)
const char *root,*relative; const void *bytes; size_t length;
{ char path[PATH_MAX];int fd,result=0;if(join(path,sizeof(path),root,relative))return-1;fd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);if(fd<0)return-1;if(write_all(fd,bytes,length)||fsync(fd))result=-1;if(close(fd))result=-1;return result; }

static int remove_tree(path)
const char *path;
{ DIR *directory;struct dirent *entry;struct stat status;char child[PATH_MAX];if(lstat(path,&status))return errno==ENOENT?0:-1;if(!S_ISDIR(status.st_mode))return unlink(path);directory=opendir(path);if(!directory)return-1;while((entry=readdir(directory))){if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,".."))continue;if(snprintf(child,sizeof(child),"%s/%s",path,entry->d_name)<0||remove_tree(child)){closedir(directory);return-1;}}return closedir(directory)||rmdir(path)?-1:0; }

static int seed(root)
const char *root;
{
    static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
    static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-handoff\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
    return make_dir(root,"player")||make_dir(root,"player/66")||
        make_dir(root,"character-save-stage")||make_dir(root,"character-save-journal")||
        leaf(root,"character-save-journal/writer-instance.v2",instance,sizeof(instance)-1U)||
        leaf(root,"character-save-journal/writer-epoch.v2",epoch,sizeof(epoch)-1U)?-1:0;
}

static int hash_bytes(root, bytes, length, output)
const char *root; const void *bytes; size_t length; char output[65];
{ char path[PATH_MAX];int fd,result;if(join(path,sizeof(path),root,"hash.tmp"))return-1;fd=open(path,O_RDWR|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);if(fd<0||write_all(fd,bytes,length)){if(fd>=0)close(fd);return-1;}if(lseek(fd,0,SEEK_SET)<0)result=-1;else result=character_save_journal_v2_hash_fd(fd,output);if(close(fd))result=-1;if(unlink(path))result=-1;return result; }

static int prepare(root, wire)
const char *root; character_save_journal_v2_wire *wire;
{ memset(wire,0,sizeof(*wire));wire->state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;strcpy(wire->writer_instance_id,INSTANCE);strcpy(wire->character_id,CHARACTER);strcpy(wire->world_id,WORLD);strcpy(wire->legacy_name_key_hex,"4d33616c706861");strcpy(wire->legacy_shard,"66");strcpy(wire->command_uuid,COMMAND);wire->writer_epoch=7;wire->writer_revision=1;wire->expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;wire->storage_format=1;if(hash_bytes(root,STAGE,sizeof(STAGE)-1U,wire->post_sha256)||character_save_journal_v2_request_sha256(wire,wire->request_sha256))return-1;return character_save_journal_v2_prepare(root,wire,STAGE,sizeof(STAGE)-1U); }

static character_save_journal_v2_route_lookup_result route(opaque,world,name,length,reply)
void *opaque;const char *world;const unsigned char *name;size_t length;character_save_journal_v2_route_reply *reply;
{ (void)opaque;memset(reply,0,sizeof(*reply));if(strcmp(world,WORLD)||length!=sizeof(NAME)-1U||memcmp(name,NAME,length))return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;reply->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;reply->row_count=1;strcpy(reply->world_id,WORLD);strcpy(reply->character_id,CHARACTER);memcpy(reply->legacy_name,NAME,length);reply->legacy_name_length=length;strcpy(reply->legacy_shard,"66");reply->storage_format=1;reply->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK; }

static character_save_journal_v2_receipt_result receipt(opaque, value)
void *opaque; const character_save_journal_v2_receipt *value;
{ character_save_journal_v2_receipt_result *result=(character_save_journal_v2_receipt_result *)opaque;return !value||strcmp(value->command_id,COMMAND)?CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE:*result; }

static int snapshot_wire(bytes, length)
uint8_t **bytes; size_t *length;
{ creature player;memset(&player,0,sizeof(player));player.type=PLAYER;player.fd=-1;strcpy(player.name,"M3alpha");memset(player.password,0x5a,sizeof(player.password));return player_snapshot_v1_encode_loaded(&player,bytes,length); }

static void artifact_fixture(artifact, wire)
character_player_snapshot_v1_artifact_metadata *artifact;const character_save_journal_v2_wire *wire;
{ memset(artifact,0,sizeof(*artifact));strcpy(artifact->world_id,wire->world_id);strcpy(artifact->character_id,wire->character_id);strcpy(artifact->command_id,wire->command_uuid);strcpy(artifact->canonical_name_hex,wire->legacy_name_key_hex);strcpy(artifact->request_sha256,wire->request_sha256);strcpy(artifact->source_post_sha256,wire->post_sha256);strcpy(artifact->writer_instance_id,wire->writer_instance_id);strcpy(artifact->snapshot_format,CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT);artifact->writer_epoch=wire->writer_epoch;artifact->writer_revision=wire->writer_revision;artifact->storage_format=(int16_t)wire->storage_format;artifact->source_octets=sizeof(STAGE)-1U; }

static int same_file_at(fd, name, before)
int fd;const char *name;const struct stat *before;
{ struct stat after;return before&&fstatat(fd,name,&after,AT_SYMLINK_NOFOLLOW)==0&&before->st_dev==after.st_dev&&before->st_ino==after.st_ino&&before->st_size==after.st_size&&before->st_mode==after.st_mode&&before->st_nlink==after.st_nlink; }

static void manifest_fixture(manifest, artifact)
character_snapshot_shadow_outbox_manifest *manifest;const character_player_snapshot_v1_artifact_metadata *artifact;
{ memset(manifest,0,sizeof(*manifest));strcpy(manifest->world_id,artifact->world_id);strcpy(manifest->character_id,artifact->character_id);strcpy(manifest->command_id,artifact->command_id);strcpy(manifest->canonical_name_hex,artifact->canonical_name_hex);strcpy(manifest->request_sha256,artifact->request_sha256);strcpy(manifest->post_sha256,artifact->source_post_sha256);strcpy(manifest->writer_instance_id,artifact->writer_instance_id);strcpy(manifest->snapshot_format,CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT);manifest->writer_epoch=artifact->writer_epoch;manifest->writer_revision=artifact->writer_revision;manifest->storage_format=artifact->storage_format;manifest->snapshot_octets=artifact->source_octets; }

int main(void)
{
    char root[PATH_MAX], directory[PATH_MAX];
    char artifact_name[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE], manifest_name[64];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_wire wire;
    character_player_snapshot_v1_artifact_metadata artifact;
    character_snapshot_shadow_outbox_manifest conflicting;
    character_snapshot_shadow_outbox_manifest generated, reread;
    character_save_journal_v2_receipt_result receipt_result;
    uint8_t *snapshot=0;
    size_t snapshot_length=0;
    int directory_fd=-1,failed=0;
    struct stat artifact_before,manifest_before;

    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    if(make_root(root)||chmod(root,0700)) { fprintf(stderr,"receipt-pair setup: root\n"); return 2; }
    if(seed(root)) { fprintf(stderr,"receipt-pair setup: seed\n"); return 2; }
    if(character_save_journal_v2_writer_open(root,WORLD,&writer)) { fprintf(stderr,"receipt-pair setup: writer before prepare errno=%d\n",errno); return 2; }
    if(prepare(root,&wire)) { fprintf(stderr,"receipt-pair setup: prepare\n"); return 2; }
    if(make_dir(root,"snapshots")||join(directory,sizeof(directory),root,"snapshots")) { fprintf(stderr,"receipt-pair setup: snapshots\n"); return 2; }
    directory_fd=open(directory,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory_fd<0||snapshot_wire(&snapshot,&snapshot_length)!=CDTO_V1_OK) {
        fprintf(stderr,"receipt-pair setup: directory or snapshot\n"); return 2;
    }
    artifact_fixture(&artifact,&wire);
    if(character_player_snapshot_v1_artifact_store(directory_fd,&artifact,snapshot,snapshot_length)!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK) { fprintf(stderr,"receipt-pair setup: artifact\n"); return 2; }
    if(character_player_snapshot_v1_artifact_filename(&artifact,artifact_name,sizeof(artifact_name))||snprintf(manifest_name,sizeof(manifest_name),"%s.manifest",COMMAND)!=45) { fprintf(stderr,"receipt-pair setup: names\n"); return 2; }

    failed+=expect(character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_NOT_ACKED&&
        fstatat(directory_fd,manifest_name,&manifest_before,AT_SYMLINK_NOFOLLOW)<0&&errno==ENOENT,
        "PREPARED cannot create a manifest");
    failed+=expect(character_save_journal_v2_publish(&writer,NAME,sizeof(NAME)-1U,route,0,COMMAND)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_NOT_ACKED,
        "PUBLISHED cannot create a manifest");
    receipt_result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    failed+=expect(character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_result)==CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED&&
        character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_NOT_ACKED,
        "DEFERRED cannot create a manifest");
    receipt_result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    character_save_journal_v2_ack_faults_for_test(0,0,0,1,0,0,0);
    failed+=expect(character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_result)==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&
        character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_LOCAL_INCOMPLETE,
        "DB_ACKED_LOCAL_INCOMPLETE cannot create a manifest");
    failed+=expect(character_save_journal_v2_ack(&writer,COMMAND,receipt,&receipt_result)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED,
        "exact DB retry must finish the local marker");

    character_snapshot_shadow_outbox_test_fail_next(CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_FSYNC);
    failed+=expect(character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR&&
        fstatat(directory_fd,artifact_name,&artifact_before,AT_SYMLINK_NOFOLLOW)==0&&
        fstatat(directory_fd,manifest_name,&manifest_before,AT_SYMLINK_NOFOLLOW)==0,
        "manifest fsync fault retains retryable immutable evidence");
    manifest_fixture(&generated,&artifact);
    failed+=expect(character_snapshot_shadow_outbox_retry(directory_fd,&generated,&reread)==CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY&&
        reread.snapshot_octets==artifact.source_octets&&
        reread.snapshot_octets!=artifact.snapshot_octets,
        "generated manifest rereads the relay artifact source_octets");
    failed+=expect(character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_EXACT_RETRY&&
        same_file_at(directory_fd,artifact_name,&artifact_before)&&same_file_at(directory_fd,manifest_name,&manifest_before),
        "exact retry must not replace artifact or manifest");

    manifest_fixture(&conflicting,&artifact);
    conflicting.writer_revision++;
    if(unlinkat(directory_fd,manifest_name,0)||fsync(directory_fd)||
       character_snapshot_shadow_outbox_write(directory_fd,&conflicting)!=CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK||
       fstatat(directory_fd,artifact_name,&artifact_before,AT_SYMLINK_NOFOLLOW)||
       fstatat(directory_fd,manifest_name,&manifest_before,AT_SYMLINK_NOFOLLOW)) failed++;
    failed+=expect(character_player_snapshot_v1_receipt_pair_commit(&writer,directory_fd,&artifact)==CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_FROZEN&&
        same_file_at(directory_fd,artifact_name,&artifact_before)&&same_file_at(directory_fd,manifest_name,&manifest_before),
        "mismatched artifact and manifest evidence must freeze without replacement");

    character_player_snapshot_v1_artifact_free(snapshot);
    if(directory_fd>=0&&close(directory_fd)) failed++;
    if(character_save_journal_v2_writer_close(&writer)||remove_tree(root)) failed++;
    return failed?1:0;
}
