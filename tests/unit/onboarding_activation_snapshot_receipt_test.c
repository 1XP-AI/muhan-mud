/* Descriptor-local crash/retry proof; this test binary is not runtime wiring. */
#include "onboarding_activation_snapshot_receipt.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_DIRECTORY
#define O_DIRECTORY 0
#endif

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define OTHER "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"

static const char *fault_point;
static int fault_directory=-1;
static char fault_name[96];
static const char *fault_bytes;

int onboarding_activation_snapshot_receipt_test_fault(const char *point)
{
    int fd;
    if(!fault_point || strcmp(fault_point,point)) return 0;
    if(!strcmp(point,"read-before-recheck")) {
        if(unlinkat(fault_directory,fault_name,0)) return 0;
        fd=openat(fault_directory,fault_name,O_WRONLY|O_CREAT|O_EXCL,0600);
        if(fd<0) return 0;
        if(write(fd,fault_bytes,strlen(fault_bytes))!=(ssize_t)strlen(fault_bytes)) {
            close(fd); return 0;
        }
        close(fd);
    }
    return 1;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr,"onboarding_activation_snapshot_receipt_test: %s\n",message);
    return 1;
}

static void reservation(onboarding_snapshot_command_reservation *value, const char *actor)
{
    memset(value,0,sizeof(*value));
    value->state=ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    strcpy(value->activation.actor_user_id,actor);
    strcpy(value->activation.correlation_id,CORRELATION);
    strcpy(value->activation.character_id,CHARACTER);
    value->activation.mode=ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    strcpy(value->activation.command_id,COMMAND);
}

static void boundary(character_player_snapshot_v1_artifact_metadata *artifact,
                     character_save_journal_v2_receipt *receipt)
{
    memset(artifact,0,sizeof(*artifact)); memset(receipt,0,sizeof(*receipt));
    strcpy(artifact->character_id,CHARACTER); strcpy(artifact->command_id,COMMAND);
    receipt->character_id=CHARACTER; receipt->command_id=COMMAND;
}

static int names(char *proof, size_t proof_size, char *temp, size_t temp_size)
{
    int left,right;
    left=snprintf(proof,proof_size,"%s.activation-receipt",COMMAND);
    right=snprintf(temp,temp_size,".%s.activation-receipt.tmp",COMMAND);
    return left==55&&right==60 ? 0:-1;
}

static int put(int directory, const char *name, const char *bytes, mode_t mode)
{
    int fd=openat(directory,name,O_WRONLY|O_CREAT|O_EXCL,mode);
    if(fd<0) return -1;
    if(write(fd,bytes,strlen(bytes))!=(ssize_t)strlen(bytes)||close(fd)) return -1;
    return 0;
}

static void clean(int directory, const char *proof, const char *temp)
{
    (void)unlinkat(directory,proof,0); (void)unlinkat(directory,temp,0);
}

static const char *valid_text =
    "actor_user_id=" ACTOR "\ncorrelation_id=" CORRELATION "\ncharacter_id=" CHARACTER
    "\nmode=provision\ncommand_id=" COMMAND "\n";
static const char *conflict_text =
    "actor_user_id=" OTHER "\ncorrelation_id=" CORRELATION "\ncharacter_id=" CHARACTER
    "\nmode=provision\ncommand_id=" COMMAND "\n";

int main(void)
{
    char root[]="/tmp/muhan-activation-snapshot-receipt.XXXXXX";
    char proof[96],temp[96],overlong[300];
    onboarding_snapshot_command_reservation accepted,stored;
    character_player_snapshot_v1_artifact_metadata artifact;
    character_save_journal_v2_receipt receipt;
    struct stat status;
    int directory=-1,failed=0;

    if(!mkdtemp(root)||(directory=open(root,O_RDONLY|O_DIRECTORY))<0||
       names(proof,sizeof(proof),temp,sizeof(temp))) return 1;
    reservation(&accepted,ACTOR); boundary(&artifact,&receipt);

    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED &&
        onboarding_activation_snapshot_receipt_read(directory,COMMAND,&stored)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED &&
        !strcmp(stored.activation.actor_user_id,ACTOR) &&
        !strcmp(stored.activation.correlation_id,CORRELATION) &&
        !strcmp(stored.activation.character_id,CHARACTER) &&
        stored.activation.mode==ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        !strcmp(stored.activation.command_id,COMMAND),
        "the exact actor/correlation/character/mode/command tuple reaches the receipt boundary");
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY,
        "an exact published retry is idempotent");
    clean(directory,proof,temp);

    fault_point="after-temp-fsync";
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0&&status.st_nlink==1,
        "a crash after temp fsync retains the durable pending temp");
    fault_point=0;
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)<0 &&
        onboarding_activation_snapshot_receipt_read(directory,COMMAND,&stored)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED,
        "a retry validates and recovers the fsynced pending temp");
    clean(directory,proof,temp);

    fault_point="after-temp-close";
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0,
        "a crash after close retains the pending temp");
    fault_point=0;
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY,
        "a retry recovers a closed pending temp");
    clean(directory,proof,temp);

    fault_point="after-link";
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR &&
        fstatat(directory,proof,&status,AT_SYMLINK_NOFOLLOW)==0&&status.st_nlink==2,
        "a post-link crash leaves a verifiable two-name state");
    fault_point=0;
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)<0,
        "a retry resolves the post-link durable pair");
    clean(directory,proof,temp);

    fault_point="after-directory-fsync";
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR,
        "a crash after directory fsync is observable at the final boundary");
    fault_point=0;
    failed+=expect(onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,
        &receipt)==ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY,
        "the final durable boundary has an exact retry");
    clean(directory,proof,temp);

    failed+=expect(put(directory,temp,conflict_text,0600)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CONFLICT &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0,
        "a conflicting pending temp is rejected and retained");
    clean(directory,proof,temp);
    failed+=expect(put(directory,temp,"actor_user_id=truncated\n",0600)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0,
        "a malformed pending temp is rejected and retained");
    clean(directory,proof,temp);
    memset(overlong,'x',sizeof(overlong)-1U); overlong[sizeof(overlong)-1U]=0;
    failed+=expect(put(directory,temp,overlong,0600)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0,
        "an overlong pending temp is rejected and retained");
    clean(directory,proof,temp);
    failed+=expect(symlinkat("missing",directory,temp)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0&&S_ISLNK(status.st_mode),
        "a symlink pending temp is rejected and retained");
    clean(directory,proof,temp);
    failed+=expect(put(directory,temp,valid_text,0600)==0&&fchmodat(directory,temp,0644,0)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT &&
        fstatat(directory,temp,&status,AT_SYMLINK_NOFOLLOW)==0&&(status.st_mode&0777)==0644,
        "a wrong-mode pending temp is rejected and retained");
    clean(directory,proof,temp);

    failed+=expect(put(directory,proof,valid_text,0600)==0,
        "replacement race fixture is installed");
    fault_point="read-before-recheck"; fault_directory=directory;
    strcpy(fault_name,proof); fault_bytes=conflict_text;
    failed+=expect(onboarding_activation_snapshot_receipt_read(directory,COMMAND,&stored)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT,
        "read-after-replacement is detected by descriptor/name identity recheck");
    fault_point=0; clean(directory,proof,temp);

    failed+=expect(fchmod(directory,0755)==0 &&
        onboarding_activation_snapshot_receipt_record(directory,&accepted,&artifact,&receipt)==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_INVALID && fchmod(directory,0700)==0,
        "the supplied directory descriptor must retain private mode");

    close(directory); rmdir(root);
    return failed ? 1:0;
}
