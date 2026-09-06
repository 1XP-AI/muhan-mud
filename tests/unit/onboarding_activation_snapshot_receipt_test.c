/* RED: a feature-off receipt proof must carry only one exact reserved
 * activation tuple into the existing snapshot/artifact receipt boundary. */
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
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_activation_snapshot_receipt_test: %s\n", message);
    return 1;
}

static void reservation(onboarding_snapshot_command_reservation *value)
{
    memset(value, 0, sizeof(*value));
    value->state = ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    strcpy(value->activation.actor_user_id, ACTOR);
    strcpy(value->activation.correlation_id, CORRELATION);
    strcpy(value->activation.character_id, CHARACTER);
    value->activation.mode = ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    strcpy(value->activation.command_id, COMMAND);
}

static void boundary(character_player_snapshot_v1_artifact_metadata *artifact,
                     character_save_journal_v2_receipt *receipt)
{
    memset(artifact, 0, sizeof(*artifact));
    memset(receipt, 0, sizeof(*receipt));
    strcpy(artifact->character_id, CHARACTER);
    strcpy(artifact->command_id, COMMAND);
    receipt->character_id = CHARACTER;
    receipt->command_id = COMMAND;
}

static int proof_path(char *output, size_t output_size)
{
    int count = snprintf(output, output_size, "%s.activation-receipt", COMMAND);
    return count < 0 || (size_t)count >= output_size ? -1 : 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-activation-snapshot-receipt.XXXXXX";
    char name[96];
    onboarding_snapshot_command_reservation accepted, stored;
    character_player_snapshot_v1_artifact_metadata artifact;
    character_save_journal_v2_receipt receipt;
    struct stat status;
    int directory, file, failed = 0;

    directory = -1;
    if(!mkdtemp(root) || (directory = open(root, O_RDONLY | O_DIRECTORY)) < 0)
        return 1;
    reservation(&accepted);
    boundary(&artifact, &receipt);

    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED &&
        onboarding_activation_snapshot_receipt_read(directory, COMMAND, &stored) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED &&
        stored.state == ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED &&
        !strcmp(stored.activation.actor_user_id, ACTOR) &&
        !strcmp(stored.activation.correlation_id, CORRELATION) &&
        !strcmp(stored.activation.character_id, CHARACTER) &&
        stored.activation.mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        !strcmp(stored.activation.command_id, COMMAND),
        "the exact actor/correlation/character/mode/command tuple reaches the receipt boundary");

    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY,
        "an exact boundary retry is idempotent");

    strcpy(accepted.activation.actor_user_id,
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CONFLICT,
        "conflicting reuse of the command is rejected without replacing the proof");
    reservation(&accepted);

    strcpy(artifact.character_id, "55555555-5555-4555-8555-555555555555");
    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_BOUNDARY_MISMATCH,
        "artifact identity substitution is rejected before local proof acceptance");
    strcpy(artifact.character_id, CHARACTER);
    receipt.command_id = "55555555-5555-4555-8555-555555555555";
    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_BOUNDARY_MISMATCH,
        "journal receipt command substitution is rejected before local proof acceptance");
    receipt.command_id = COMMAND;

    memset(accepted.activation.correlation_id, 'x',
        sizeof(accepted.activation.correlation_id));
    failed += expect(onboarding_activation_snapshot_receipt_record(directory,
        &accepted, &artifact, &receipt) ==
        ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_INVALID,
        "a malformed or truncated accepted binding is rejected before boundary use");
    reservation(&accepted);

    failed += expect(proof_path(name, sizeof(name)) == 0 &&
        unlinkat(directory, name, 0) == 0 &&
        (file = openat(directory, name, O_WRONLY | O_CREAT | O_EXCL, 0600)) >= 0 &&
        write(file, "actor_user_id=truncated\n", 24) == 24 && close(file) == 0 &&
        onboarding_activation_snapshot_receipt_record(directory, &accepted, &artifact,
            &receipt) == ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT &&
        fstatat(directory, name, &status, AT_SYMLINK_NOFOLLOW) == 0 &&
        status.st_size == 24,
        "a malformed or truncated local tuple proof remains rejected and untouched");

    if(directory >= 0) {
        unlinkat(directory, name, 0);
        close(directory);
    }
    rmdir(root);
    return failed ? 1 : 0;
}
