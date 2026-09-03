#include "character_save_journal_v2_recovery.h"
#include "character_save_journal_v2.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char INSTANCE[] = "11111111-1111-4111-8111-111111111111";
static const char WORLD[] = "m3-recovery";
static const char COMMAND_A[] = "10000000-0000-0000-0000-000000000001";
static const char COMMAND_B[] = "20000000-0000-0000-0000-000000000002";
static const char COMMAND_C[] = "30000000-0000-0000-0000-000000000003";
static const char COMMAND_REVISION_2[] = "00000000-0000-0000-0000-000000000004";
static const char COMMAND_REVISION_1[] = "f0000000-0000-0000-0000-000000000005";
static const char CHARACTER_REVISION[] = "90000000-0000-0000-0000-000000000006";
static const char COMMAND_INDEPENDENT[] = "40000000-0000-0000-0000-000000000007";
static const char CHARACTER_INDEPENDENT[] = "a0000000-0000-0000-0000-000000000008";
static const char COMMAND_UPPER[] = "A0000000-0000-0000-0000-000000000005";
static const char NAME_A[] = "M3alpha";
static const char NAME_B[] = "M3beta";
static const char NAME_C[] = "M3gamma";

typedef struct mock {
    character_save_journal_v2_receipt_result results[8];
    int calls, add_prepared, reopen_writer, close_writer;
    const char *root;
    character_save_journal_v2_writer_context *writer;
    char order[8][37];
} mock;

typedef struct evidence_snapshot {
    struct stat st;
    unsigned char bytes[2048];
    size_t length;
} evidence_snapshot;

typedef struct observed_recovery {
    mock receipts;
    const char *root;
    int observer_result;
    int observer_calls;
    int observer_saw_stage;
    unsigned int event_count;
    char events[8];
} observed_recovery;

static int bad(condition, message)
int condition;
const char *message;
{ if(condition) return 0; fprintf(stderr, "recovery: %s\n", message); return 1; }

static int path_join(out, out_size, root, relative)
char *out; size_t out_size; const char *root, *relative;
{ int n=snprintf(out,out_size,"%s/%s",root,relative); return n<0||(size_t)n>=out_size?-1:0; }

static int journal_relative(out, out_size, command, suffix)
char *out; size_t out_size; const char *command, *suffix;
{ int n=snprintf(out,out_size,"character-save-journal/%s.%s",command,suffix); return n<0||(size_t)n>=out_size?-1:0; }

static int write_all(fd, bytes, length)
int fd; const void *bytes; size_t length;
{ const char *cursor=bytes; ssize_t written; while(length){written=write(fd,cursor,length);if(written<0&&errno==EINTR)continue;if(written<=0)return-1;cursor+=written;length-=(size_t)written;}return 0; }

static int leaf(root, relative, bytes, length)
const char *root,*relative,*bytes; size_t length;
{ char full[PATH_MAX];int fd,result=0;if(path_join(full,sizeof(full),root,relative))return-1;fd=open(full,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0)return-1;if(fchmod(fd,0600)||write_all(fd,bytes,length)||fsync(fd))result=-1;if(close(fd))result=-1;return result; }

static int make_directory(root, relative)
const char *root,*relative;
{ char full[PATH_MAX];return path_join(full,sizeof(full),root,relative)||mkdir(full,0700)?-1:0; }

static int remove_tree(path)
const char *path;
{ DIR *directory;struct dirent *entry;struct stat st;char child[PATH_MAX];if(lstat(path,&st))return errno==ENOENT?0:-1;if(!S_ISDIR(st.st_mode))return unlink(path);if(!(directory=opendir(path)))return-1;while((entry=readdir(directory))){if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,".."))continue;if(snprintf(child,sizeof(child),"%s/%s",path,entry->d_name)<0||remove_tree(child)){closedir(directory);return-1;}}return closedir(directory)||rmdir(path)?-1:0; }

static int teardown(writer, root)
character_save_journal_v2_writer_context *writer;
const char *root;
{ int close_result=character_save_journal_v2_writer_close(writer);int tree_result=remove_tree(root);if(close_result||tree_result)fprintf(stderr,"recovery teardown: close=%d tree=%d errno=%d\n",close_result,tree_result,errno);return close_result||tree_result; }

/* writer_open intentionally receives realpath(/tmp), not the macOS symlink. */
static int make_root(root, label)
char root[PATH_MAX]; const char *label;
{ char temporary[PATH_MAX];int n;if(!realpath("/tmp",temporary))return-1;n=snprintf(root,PATH_MAX,"%s/muhan-v2-recovery-%s-XXXXXX",temporary,label);return n<0||n>=PATH_MAX||!mkdtemp(root)?-1:0; }

static int seed(root)
const char *root;
{ static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n",epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-recovery\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";return make_directory(root,"player")||make_directory(root,"player/11")||make_directory(root,"player/66")||make_directory(root,"player/b2")||make_directory(root,"player/a1")||make_directory(root,"character-save-stage")||make_directory(root,"character-save-journal")||leaf(root,"character-save-journal/writer-instance.v2",instance,sizeof(instance)-1)||leaf(root,"character-save-journal/writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0; }

static int hash_bytes(root, bytes, length, output)
const char *root,*bytes; size_t length; char output[65];
{ char full[PATH_MAX];int fd,result;if(path_join(full,sizeof(full),root,"hash"))return-1;fd=open(full,O_RDWR|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0||write_all(fd,bytes,length)||lseek(fd,0,SEEK_SET)<0){if(fd>=0)close(fd);return-1;}result=character_save_journal_v2_hash_fd(fd,output);if(close(fd))result=-1;unlink(full);return result; }

static const char *legacy_shard(legacy_name)
const char *legacy_name;
{
    if(!strcmp(legacy_name, "M3alpha")) return "66";
    if(!strcmp(legacy_name, "M3beta")) return "b2";
    if(!strcmp(legacy_name, "M3gamma")) return "a1";
    return "11";
}

static int prepare_revision_wire(root, command, character, revision, instance, epoch, legacy_name, bytes, length, expected_state, expected_sha256, post_sha256)
const char *root,*command,*character,*instance,*legacy_name,*bytes,*expected_sha256; unsigned long long revision,epoch; size_t length; character_save_journal_v2_expected_state expected_state; char post_sha256[65];
{ character_save_journal_v2_wire wire;char hex[129];size_t i;static const char digits[]="0123456789abcdef";memset(&wire,0,sizeof(wire));for(i=0;legacy_name[i];i++){hex[i*2]=digits[((unsigned char)legacy_name[i])>>4];hex[i*2+1]=digits[(unsigned char)legacy_name[i]&15];}hex[i*2]=0;wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;strcpy(wire.writer_instance_id,instance);strcpy(wire.character_id,character);strcpy(wire.world_id,WORLD);strcpy(wire.legacy_name_key_hex,hex);strcpy(wire.legacy_shard,legacy_shard(legacy_name));strcpy(wire.command_uuid,command);wire.writer_epoch=epoch;wire.writer_revision=revision;wire.expected_state=expected_state;if(expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING&&expected_sha256)strcpy(wire.expected_sha256,expected_sha256);wire.storage_format=1;if(hash_bytes(root,bytes,length,wire.post_sha256)||character_save_journal_v2_request_sha256(&wire,wire.request_sha256)||character_save_journal_v2_prepare(root,&wire,bytes,length)){memset(&wire,0,sizeof(wire));return-1;}if(post_sha256)strcpy(post_sha256,wire.post_sha256);memset(&wire,0,sizeof(wire));return 0; }

static int prepare_revision_tuple(root, command, character, revision, instance, epoch, legacy_name, bytes, length)
const char *root,*command,*character,*instance,*legacy_name,*bytes; unsigned long long revision,epoch; size_t length;
{ return prepare_revision_wire(root,command,character,revision,instance,epoch,legacy_name,bytes,length,CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT,0,0); }

static int prepare_revision(root, command, character, revision, legacy_name, bytes, length)
const char *root,*command,*character,*legacy_name,*bytes; unsigned long long revision; size_t length;
{ return prepare_revision_tuple(root,command,character,revision,INSTANCE,7,legacy_name,bytes,length); }

static int prepare_revision_existing(root, command, character, revision, legacy_name, bytes, length, expected_sha256, post_sha256)
const char *root,*command,*character,*legacy_name,*bytes,*expected_sha256; unsigned long long revision; size_t length; char post_sha256[65];
{ return prepare_revision_wire(root,command,character,revision,INSTANCE,7,legacy_name,bytes,length,CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING,expected_sha256,post_sha256); }

static int prepare(root, command, legacy_name, bytes, length)
const char *root,*command,*legacy_name,*bytes; size_t length;
{ return prepare_revision(root,command,command,1,legacy_name,bytes,length); }

static int prepare_published_first(root, writer, command, character, legacy_name, bytes, length, post_sha256)
const char *root,*command,*character,*legacy_name,*bytes; character_save_journal_v2_writer_context *writer; size_t length; char post_sha256[65];
{ int prepared=prepare_revision_wire(root,command,character,1,INSTANCE,7,legacy_name,bytes,length,CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT,0,post_sha256);int published=prepared?CHARACTER_SAVE_JOURNAL_V2_PUBLISH_INVALID_ARGUMENT:character_save_journal_v2_publish_recover(writer,command);return prepared||published!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK?-1:0; }

static int setup(root, label, writer)
char root[PATH_MAX]; const char *label; character_save_journal_v2_writer_context *writer;
{ if(make_root(root,label)||seed(root))return-1;memset(writer,0,sizeof(*writer));return character_save_journal_v2_writer_open(root,WORLD,writer); }

static int exists(root, relative)
const char *root,*relative;
{ char full[PATH_MAX];struct stat st;return !path_join(full,sizeof(full),root,relative)&&!lstat(full,&st); }

static int command_exists(root, command, suffix)
const char *root,*command,*suffix;
{ char relative[128];return !journal_relative(relative,sizeof(relative),command,suffix)&&exists(root,relative); }

static int snapshot(root, relative, output)
const char *root,*relative; evidence_snapshot *output;
{ char full[PATH_MAX];int fd;ssize_t read_count,extra;if(!output||path_join(full,sizeof(full),root,relative)||lstat(full,&output->st)||!S_ISREG(output->st.st_mode)||output->st.st_size<0||output->st.st_size>=(off_t)sizeof(output->bytes))return-1;fd=open(full,O_RDONLY|O_NOFOLLOW);if(fd<0)return-1;do read_count=read(fd,output->bytes,sizeof(output->bytes));while(read_count<0&&errno==EINTR);do extra=read(fd,output->bytes,1);while(extra<0&&errno==EINTR);if(close(fd)||read_count<0||extra!=0||read_count!=output->st.st_size)return-1;output->length=(size_t)read_count;return 0; }

static int same_snapshot(left, right)
const evidence_snapshot *left,*right;
{ return left&&right&&left->st.st_dev==right->st.st_dev&&left->st.st_ino==right->st.st_ino&&left->st.st_nlink==right->st.st_nlink&&left->st.st_mode==right->st.st_mode&&left->st.st_uid==right->st.st_uid&&left->st.st_size==right->st.st_size&&left->length==right->length&&!memcmp(left->bytes,right->bytes,left->length); }

static int same_stat(left, right)
const struct stat *left,*right;
{ return left&&right&&left->st_dev==right->st_dev&&left->st_ino==right->st_ino&&left->st_mode==right->st_mode&&left->st_nlink==right->st_nlink&&left->st_uid==right->st_uid&&left->st_size==right->st_size; }

static int report_zero(report)
const character_save_journal_v2_recovery_report *report;
{ const unsigned char *bytes=(const unsigned char *)report;size_t i;for(i=0;i<sizeof(*report);i++)if(bytes[i])return 0;return 1; }

static int report_totals_match(report)
const character_save_journal_v2_recovery_report *report;
{ unsigned int publish_total=0,ack_total=0,i;for(i=0;i<=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;i++)publish_total+=report->publish_results[i];for(i=0;i<=CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;i++)ack_total+=report->ack_results[i];return publish_total==report->publish_attempted&&ack_total==report->ack_attempted; }

static character_save_journal_v2_receipt_result receipt(opaque, value)
void *opaque; const character_save_journal_v2_receipt *value;
{ mock *state=opaque;int index=state->calls;if(index<(int)(sizeof(state->order)/sizeof(state->order[0])))strcpy(state->order[index],value->command_id);state->calls++;if(state->add_prepared){state->add_prepared=0;if(!state->root||prepare(state->root,COMMAND_C,NAME_C,"C",1))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;}if(state->reopen_writer){state->reopen_writer=0;if(!state->root||!state->writer||character_save_journal_v2_writer_close(state->writer)||character_save_journal_v2_writer_open(state->root,WORLD,state->writer))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;}if(state->close_writer){state->close_writer=0;if(!state->writer||character_save_journal_v2_writer_close(state->writer))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;}return index>=(int)(sizeof(state->results)/sizeof(state->results[0]))?CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE:state->results[index]; }

static character_save_journal_v2_receipt_result observed_receipt(opaque, value)
void *opaque;
const character_save_journal_v2_receipt *value;
{
    observed_recovery *state = opaque;
    if(state->event_count < sizeof(state->events))
        state->events[state->event_count++] = 'R';
    return receipt(&state->receipts, value);
}

static int observe_stage(opaque, writer, command_uuid)
void *opaque;
const character_save_journal_v2_writer_context *writer;
const char *command_uuid;
{
    observed_recovery *state = opaque;
    character_save_journal_v2_writer_tuple tuple;
    char relative[128];
    int visible;

    memset(&tuple, 0, sizeof(tuple));
    if(state->event_count < sizeof(state->events))
        state->events[state->event_count++] = 'O';
    state->observer_calls++;
    visible = state->root &&
        character_save_journal_v2_writer_validate_held(writer, &tuple) ==
            CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK &&
        !journal_relative(relative, sizeof(relative), command_uuid, "prepared") &&
        exists(state->root, relative) &&
        snprintf(relative, sizeof(relative), "character-save-stage/%s.stage",
                 command_uuid) > 0 && exists(state->root, relative);
    if(visible) state->observer_saw_stage++;
    memset(&tuple, 0, sizeof(tuple));
    return visible ? state->observer_result : -99;
}

static int test_stage_observer_is_pre_publish_and_non_authoritative(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_recovery_report report;
    observed_recovery state;
    int failed = 0;

    if(setup(root, "stage-observer", &writer) ||
       prepare(root, COMMAND_A, NAME_A, "A", 1) ||
       prepare(root, COMMAND_B, NAME_B, "B", 1)) return 1;
    memset(&state, 0, sizeof(state));
    state.root = root;
    state.observer_result = -73;
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run_with_stage_observer(
        &writer, observed_receipt, &state, observe_stage, &state, &report) ==
            CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK &&
        state.observer_calls == 2 && state.observer_saw_stage == 2 &&
        state.receipts.calls == 2 && state.event_count == 4 &&
        state.events[0] == 'O' && state.events[1] == 'R' &&
        state.events[2] == 'O' && state.events[3] == 'R' &&
        report.snapshot_attempted == 2 &&
        report.snapshot_succeeded == 0 && report.snapshot_failed == 2 &&
        report.publish_attempted == 2 && report.ack_attempted == 2 &&
        command_exists(root, COMMAND_A, "acked") &&
        command_exists(root, COMMAND_B, "acked") &&
        report_totals_match(&report),
        "observer failures must be reported before each publish without changing recovery");
    if(teardown(&writer, root)) return failed + 1;
    return failed;
}

static int test_lexical_retry_and_totals(void)
{ char root[PATH_MAX];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;mock state;int failed=0;if(setup(root,"lexical",&writer)||prepare(root,COMMAND_B,NAME_B,"B",1)||prepare(root,COMMAND_A,NAME_A,"A",1))return 1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==2&&!strcmp(state.order[0],COMMAND_A)&&!strcmp(state.order[1],COMMAND_B)&&report.discovered==2&&report.visited==2&&report.publish_attempted==2&&report.ack_attempted==2&&report.publish_results[CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK]==2&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED]==2&&report_totals_match(&report),"scrambled creation is visited lexically with exact enum totals");memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==2&&!strcmp(state.order[0],COMMAND_A)&&!strcmp(state.order[1],COMMAND_B)&&report_totals_match(&report),"already published and acked commands retry exactly");if(teardown(&writer,root))return failed+1;return failed; }

static int test_same_character_revision_order(void)
{ char root[PATH_MAX],post_sha256[65];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;mock state;int failed=0;if(setup(root,"revision-order",&writer)||prepare_published_first(root,&writer,COMMAND_REVISION_1,CHARACTER_REVISION,NAME_B,"1",1,post_sha256)||prepare_revision_existing(root,COMMAND_REVISION_2,CHARACTER_REVISION,2,NAME_B,"2",1,post_sha256,0))return 1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==2&&!strcmp(state.order[0],COMMAND_REVISION_1)&&!strcmp(state.order[1],COMMAND_REVISION_2)&&report.discovered==2&&report.visited==2&&report.publish_attempted==2&&report.ack_attempted==2&&report_totals_match(&report),"a physically possible reverse-lexical backlog ACKs in writer revision order");if(teardown(&writer,root))return failed+1;return failed; }

static int test_snapshot_chain_rejections(void)
{
    char root[PATH_MAX], first[128], second[128], post_sha256[65], wrong_hash[65];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_recovery_report report;
    evidence_snapshot first_before, first_after, second_before, second_after;
    mock state;
    int failed = 0;

    if(setup(root, "route-drift", &writer)) return 1;
    if(prepare_revision_wire(root, COMMAND_REVISION_1, CHARACTER_REVISION, 1,
                             INSTANCE, 7, NAME_A, "1", 1,
                             CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT, 0, post_sha256) ||
       character_save_journal_v2_publish_recover(&writer, COMMAND_REVISION_1) !=
           CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK ||
       leaf(root, "player/b2/M3beta", "1", 1) ||
       prepare_revision_existing(root, COMMAND_REVISION_2, CHARACTER_REVISION, 2,
                                 NAME_B, "2", 1, post_sha256, 0) ||
       journal_relative(first, sizeof(first), COMMAND_REVISION_1, "prepared") ||
       journal_relative(second, sizeof(second), COMMAND_REVISION_2, "prepared") ||
       snapshot(root, first, &first_before) || snapshot(root, second, &second_before)) {
        bad(0, "route drift fixture setup");
        teardown(&writer, root);
        return 1;
    }
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE && !state.calls &&
                  report_zero(&report) && snapshot(root, first, &first_after) == 0 &&
                  snapshot(root, second, &second_after) == 0 &&
                  same_snapshot(&first_before, &first_after) &&
                  same_snapshot(&second_before, &second_after) &&
                  !command_exists(root, COMMAND_REVISION_2, "published"),
                  "same-character route drift rejects the complete snapshot before mutation");
    if(teardown(&writer, root)) return failed + 1;

    if(setup(root, "broken-chain", &writer) ||
       prepare_published_first(root, &writer, COMMAND_REVISION_1,
                              CHARACTER_REVISION, NAME_A, "1", 1, post_sha256) ||
       leaf(root, "player/66/M3alpha", "x", 1) ||
       hash_bytes(root, "x", 1, wrong_hash) ||
       prepare_revision_existing(root, COMMAND_REVISION_2, CHARACTER_REVISION, 2,
                                 NAME_A, "2", 1, wrong_hash, 0) ||
       leaf(root, "player/66/M3alpha", "1", 1) ||
       journal_relative(first, sizeof(first), COMMAND_REVISION_1, "prepared") ||
       journal_relative(second, sizeof(second), COMMAND_REVISION_2, "prepared") ||
       snapshot(root, first, &first_before) || snapshot(root, second, &second_before))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE && !state.calls &&
                  report_zero(&report) && snapshot(root, first, &first_after) == 0 &&
                  snapshot(root, second, &second_after) == 0 &&
                  same_snapshot(&first_before, &first_after) &&
                  same_snapshot(&second_before, &second_after) &&
                  !command_exists(root, COMMAND_REVISION_2, "published"),
                  "a broken same-character hash chain rejects before mutation");
    if(teardown(&writer, root)) return failed + 1;

    if(setup(root, "revision-gap", &writer) ||
       prepare_published_first(root, &writer, COMMAND_REVISION_1,
                              CHARACTER_REVISION, NAME_A, "1", 1, post_sha256) ||
       prepare_revision_existing(root, COMMAND_REVISION_2, CHARACTER_REVISION, 3,
                                 NAME_A, "2", 1, post_sha256, 0) ||
       journal_relative(first, sizeof(first), COMMAND_REVISION_1, "prepared") ||
       journal_relative(second, sizeof(second), COMMAND_REVISION_2, "prepared") ||
       snapshot(root, first, &first_before) || snapshot(root, second, &second_before))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE && !state.calls &&
                  report_zero(&report) && snapshot(root, first, &first_after) == 0 &&
                  snapshot(root, second, &second_after) == 0 &&
                  same_snapshot(&first_before, &first_after) &&
                  same_snapshot(&second_before, &second_after) &&
                  !command_exists(root, COMMAND_REVISION_2, "published"),
                  "a same-character revision gap rejects before mutation");
    if(teardown(&writer, root)) return failed + 1;

    if(setup(root, "path-collision", &writer) ||
       prepare_revision(root, COMMAND_A, COMMAND_A, 1, NAME_A, "A", 1) ||
       prepare_revision(root, COMMAND_B, COMMAND_B, 1, NAME_A, "B", 1) ||
       journal_relative(first, sizeof(first), COMMAND_A, "prepared") ||
       journal_relative(second, sizeof(second), COMMAND_B, "prepared") ||
       snapshot(root, first, &first_before) || snapshot(root, second, &second_before))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE && !state.calls &&
                  report_zero(&report) && snapshot(root, first, &first_after) == 0 &&
                  snapshot(root, second, &second_after) == 0 &&
                  same_snapshot(&first_before, &first_after) &&
                  same_snapshot(&second_before, &second_after) &&
                  !command_exists(root, COMMAND_A, "published") &&
                  !command_exists(root, COMMAND_B, "published"),
                  "different characters sharing a canonical legacy path reject before mutation");
    if(teardown(&writer, root)) return failed + 1;
    return failed;
}


static int test_revision_snapshot_rejections_and_fence(void)
{
    char root[PATH_MAX], post_sha256[65];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_recovery_report report;
    mock state;
    int failed = 0;

    if(setup(root, "revision-duplicate", &writer) ||
       prepare_revision(root, COMMAND_REVISION_1, CHARACTER_REVISION, 1,
                        NAME_A, "1", 1) ||
       prepare_revision(root, COMMAND_REVISION_2, CHARACTER_REVISION, 1,
                        NAME_B, "2", 1)) return 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE && !state.calls &&
                  report_zero(&report),
                  "duplicate same-character revisions reject before mutation");
    if(teardown(&writer, root)) return failed + 1;

    if(setup(root, "revision-publish-fence", &writer) ||
       prepare_published_first(root, &writer, COMMAND_REVISION_1,
                              CHARACTER_REVISION, NAME_A, "1", 1, post_sha256) ||
       prepare_revision_existing(root, COMMAND_REVISION_2, CHARACTER_REVISION, 2,
                                 NAME_A, "2", 1, post_sha256, 0) ||
       prepare_revision(root, COMMAND_INDEPENDENT, CHARACTER_INDEPENDENT, 1,
                        NAME_C, "i", 1) ||
       leaf(root, "player/66/M3alpha", "changed", 7)) return failed + 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE && state.calls == 1 &&
                  !strcmp(state.order[0], COMMAND_INDEPENDENT) &&
                  command_exists(root, COMMAND_REVISION_2, "prepared") &&
                  !command_exists(root, COMMAND_REVISION_2, "published") &&
                  command_exists(root, COMMAND_INDEPENDENT, "acked") &&
                  report_totals_match(&report),
                  "a nonterminal publish fences later revisions while independent characters continue");
    if(teardown(&writer, root)) return failed + 1;

    if(setup(root, "revision-fence", &writer) ||
       prepare_published_first(root, &writer, COMMAND_REVISION_1,
                              CHARACTER_REVISION, NAME_A, "1", 1, post_sha256) ||
       prepare_revision_existing(root, COMMAND_REVISION_2, CHARACTER_REVISION, 2,
                                 NAME_A, "2", 1, post_sha256, 0) ||
       prepare_revision(root, COMMAND_INDEPENDENT, CHARACTER_INDEPENDENT, 1,
                        NAME_C, "i", 1)) return failed + 1;
    memset(&state, 0, sizeof(state));
    state.results[0] = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE && state.calls == 2 &&
                  !strcmp(state.order[0], COMMAND_REVISION_1) &&
                  !strcmp(state.order[1], COMMAND_INDEPENDENT) &&
                  command_exists(root, COMMAND_REVISION_2, "prepared") &&
                  !command_exists(root, COMMAND_REVISION_2, "published") &&
                  command_exists(root, COMMAND_INDEPENDENT, "acked") &&
                  report_totals_match(&report),
                  "a deferred revision fences later revisions while independent characters continue");
    if(teardown(&writer, root)) return failed + 1;
    return failed;
}

static int test_deferred_and_freeze(void)
{ char root[PATH_MAX];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;mock state;int failed=0;if(setup(root,"deferred",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1))return 1;memset(&state,0,sizeof(state));state.results[0]=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE&&state.calls==1&&command_exists(root,COMMAND_A,"published")&&!command_exists(root,COMMAND_A,"acked")&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED]==1&&report_totals_match(&report),"deferred DB receipt retains exact evidence for retry");memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==1&&command_exists(root,COMMAND_A,"acked")&&report_totals_match(&report),"deferred receipt converges on its exact later retry");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"freeze",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||prepare(root,COMMAND_B,"M3beta","B",1))return failed+1;memset(&state,0,sizeof(state));state.results[0]=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;state.results[1]=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE&&state.calls==2&&!strcmp(state.order[0],COMMAND_A)&&!strcmp(state.order[1],COMMAND_B)&&!command_exists(root,COMMAND_A,"acked")&&!command_exists(root,COMMAND_B,"acked")&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE]==1&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE]==1&&report_totals_match(&report),"invalid and rejected freezes both retain evidence and do not stop traversal");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;return failed; }

static int test_live_and_snapshot_boundaries(void)
{ char root[PATH_MAX];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;mock state;int failed=0;if(setup(root,"live",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||prepare(root,COMMAND_B,"M3beta","B",1)||leaf(root,"player/66/M3alpha","changed",7))return 1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE&&state.calls==1&&!strcmp(state.order[0],COMMAND_B)&&report.visited==2&&report.publish_results[CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE]==1&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED]==1&&command_exists(root,COMMAND_B,"acked")&&report_totals_match(&report),"a changed first live leaf does not prevent the next lexical command from ACKing");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"snapshot",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||prepare(root,COMMAND_B,"M3beta","B",1))return failed+1;memset(&state,0,sizeof(state));state.root=root;state.add_prepared=1;memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==2&&report.discovered==2&&command_exists(root,COMMAND_C,"prepared")&&report_totals_match(&report),"callback-created prepared evidence is outside the current snapshot");memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==3&&!strcmp(state.order[2],COMMAND_C)&&report.discovered==3&&report_totals_match(&report),"callback-created prepared evidence is first visited on the next run");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;return failed; }

static int test_structure_and_non_authority(void)
{ char root[PATH_MAX],relative[128],full[PATH_MAX],target[PATH_MAX];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;evidence_snapshot before,after;struct stat node_before,node_after;mock state;int failed=0;if(setup(root,"malformed",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||journal_relative(relative,sizeof(relative),COMMAND_UPPER,"prepared")||leaf(root,relative,"bad",3)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared")||snapshot(root,relative,&before))return 1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after),"uppercase prepared identifier rejects the whole snapshot without mutation");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"noise",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1))return failed+1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));if(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared.extra")||leaf(root,relative,"noise",5)||journal_relative(relative,sizeof(relative),COMMAND_B,"published.tmp")||leaf(root,relative,"noise",5)||journal_relative(relative,sizeof(relative),COMMAND_C,"acked.tmp")||leaf(root,relative,"noise",5))return failed+1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK&&state.calls==1&&report.discovered==1&&command_exists(root,COMMAND_A,"published")&&command_exists(root,COMMAND_A,"acked")&&command_exists(root,COMMAND_B,"published.tmp")&&command_exists(root,COMMAND_C,"acked.tmp")&&command_exists(root,COMMAND_A,"prepared.extra")&&exists(root,"character-save-journal/writer-instance.v2")&&exists(root,"character-save-journal/writer-epoch.v2")&&exists(root,"character-save-journal/.m3-writer.lock")&&report_totals_match(&report),"published, acked, unrelated temp, tuple and lock leaves neither duplicate visits nor auto-cleanup");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"symlink",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared")||path_join(target,sizeof(target),root,relative)||journal_relative(relative,sizeof(relative),COMMAND_B,"prepared")||path_join(full,sizeof(full),root,relative)||symlink(target,full)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared")||snapshot(root,relative,&before)||lstat(full,&node_before))return failed+1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after)&&lstat(full,&node_after)==0&&same_stat(&node_before,&node_after),"symlink prepared candidate fails before callback with evidence retained");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"fifo",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||journal_relative(relative,sizeof(relative),COMMAND_B,"prepared")||path_join(full,sizeof(full),root,relative)||mkfifo(full,0600)||lstat(full,&node_before))return failed+1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&lstat(full,&node_after)==0&&same_stat(&node_before,&node_after),"FIFO prepared candidate fails before callback with evidence retained");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"hardlink",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared")||path_join(target,sizeof(target),root,relative)||journal_relative(relative,sizeof(relative),COMMAND_B,"prepared")||path_join(full,sizeof(full),root,relative)||link(target,full)||lstat(target,&node_before)||lstat(full,&node_after))return failed+1;memset(&state,0,sizeof(state));memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&lstat(target,&after.st)==0&&lstat(full,&before.st)==0&&same_stat(&node_before,&after.st)&&same_stat(&node_after,&before.st),"hard-linked prepared candidate fails before callback with evidence retained");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;return failed; }

static int test_scan_failures_and_writer_reopen(void)
{ char root[PATH_MAX],relative[128];character_save_journal_v2_writer_context writer;character_save_journal_v2_recovery_report report;evidence_snapshot before,after;mock state;int failed=0;if(setup(root,"scan-failures",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||journal_relative(relative,sizeof(relative),COMMAND_A,"prepared")||snapshot(root,relative,&before))return 1;memset(&state,0,sizeof(state));character_save_journal_v2_recovery_fail_root_close_for_test(1);memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_JOURNAL&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after),"root descriptor close failure leaves report and evidence unchanged");character_save_journal_v2_recovery_fail_allocation_for_test(1);memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_NOMEM&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after),"OOM leaves report and evidence unchanged");character_save_journal_v2_recovery_set_entry_cap_for_test(0);memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after),"entry cap leaves report and evidence unchanged");character_save_journal_v2_recovery_set_entry_cap_for_test(CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES);character_save_journal_v2_recovery_fail_closedir_for_test(1);memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE&&!state.calls&&report_zero(&report)&&snapshot(root,relative,&after)==0&&same_snapshot(&before,&after)&&character_save_journal_v2_recovery_scan_fd_cloexec_for_test(),"closedir failure leaves report and evidence unchanged, scan fd is CLOEXEC");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;if(setup(root,"context-stop",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||prepare(root,COMMAND_B,"M3beta","B",1))return failed+1;memset(&state,0,sizeof(state));state.writer=&writer;state.close_writer=1;state.results[0]=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID&&state.calls==1&&report.visited==2&&report.publish_attempted==2&&report.publish_results[CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_INVALID]==1&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED]==1&&report_totals_match(&report),"a later publish context failure stops traversal with the exact mapped result");if(remove_tree(root))return failed+1;if(setup(root,"reopen",&writer)||prepare(root,COMMAND_A,"M3alpha","A",1)||prepare(root,COMMAND_B,"M3beta","B",1))return failed+1;memset(&state,0,sizeof(state));state.root=root;state.writer=&writer;state.reopen_writer=1;memset(&report,0,sizeof(report));failed+=bad(character_save_journal_v2_recovery_run(&writer,receipt,&state,&report)==CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE&&state.calls==2&&!strcmp(state.order[0],COMMAND_A)&&!strcmp(state.order[1],COMMAND_B)&&command_exists(root,COMMAND_B,"acked")&&report.visited==2&&report.publish_attempted==2&&report.ack_attempted==2&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE]==1&&report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED]==1&&report_totals_match(&report),"local-incomplete ACK fences only its character and continues independent work");if(character_save_journal_v2_writer_close(&writer)||remove_tree(root))return failed+1;return failed; }

static int test_malformed_marker_callback_replay(void)
{
    char root[PATH_MAX], relative[128];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_recovery_report report;
    evidence_snapshot before, after;
    mock state;
    int failed = 0;

    if(setup(root, "malformed-marker-replay", &writer) ||
       prepare(root, COMMAND_A, NAME_A, "A", 1) ||
       journal_relative(relative, sizeof(relative), COMMAND_A, "acked.tmp") ||
       leaf(root, relative, "malformed", 9) || snapshot(root, relative, &before))
        return 1;
    memset(&state, 0, sizeof(state));
    memset(&report, 0, sizeof(report));
    failed += bad(character_save_journal_v2_recovery_run(&writer, receipt, &state,
                                                          &report) ==
                  CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE &&
                  state.calls == 1 && !strcmp(state.order[0], COMMAND_A) &&
                  report.discovered == 1 && report.ack_attempted == 1 &&
                  report.ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE] == 1 &&
                  snapshot(root, relative, &after) == 0 &&
                  same_snapshot(&before, &after) &&
                  command_exists(root, COMMAND_A, "published") &&
                  !command_exists(root, COMMAND_A, "acked") &&
                  report_totals_match(&report),
                  "malformed local marker replays the exact receipt and preserves evidence");
    if(teardown(&writer, root)) return failed + 1;
    return failed;
}

int main(void)
{ int failed;character_save_journal_v2_set_trusted_uid_for_test(getuid());character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());failed=test_lexical_retry_and_totals();failed+=test_same_character_revision_order();failed+=test_snapshot_chain_rejections();failed+=test_revision_snapshot_rejections_and_fence();failed+=test_deferred_and_freeze();failed+=test_live_and_snapshot_boundaries();failed+=test_structure_and_non_authority();failed+=test_scan_failures_and_writer_reopen();failed+=test_malformed_marker_callback_replay();failed+=test_stage_observer_is_pre_publish_and_non_authoritative();if(failed)fprintf(stderr,"recovery failures: %d\n",failed);return failed?1:0; }
