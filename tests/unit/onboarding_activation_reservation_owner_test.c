/*
 * RED: this contract is intentionally compiled before the owner boundary
 * exists.  It proves the only permitted composition: a real, already-ready
 * process owner lends its idle boundary to one caller-owned adapter attempt.
 */
#include "onboarding_activation_reservation_owner.h"

#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"

typedef struct fixture {
    character_save_journal_v2_process_owner owner;
    onboarding_activation_reservation_adapter_result adapter_result;
    int adapter_calls, recursive_result;
    int directory_fd;
} fixture;

static fixture current;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_activation_reservation_owner_test: %s\n", message);
    return 1;
}

static void binding(onboarding_activation_binding *value)
{
    memset(value,0,sizeof(*value));
    strcpy(value->actor_user_id,ACTOR);
    strcpy(value->correlation_id,CORRELATION);
    strcpy(value->character_id,CHARACTER);
    value->mode=ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    strcpy(value->command_id,COMMAND);
}

onboarding_activation_reservation_adapter_result
onboarding_activation_reservation_adapter_install(int directory_fd,
    const onboarding_activation_binding *expected,
    onboarding_activation_save_capability *capability,
    const char *canonical_name, onboarding_activation_save_bridge *bridge_out)
{
    current.adapter_calls++;
    if(directory_fd != current.directory_fd || fcntl(directory_fd,F_GETFD)<0 ||
       !expected || strcmp(expected->actor_user_id,ACTOR) ||
       strcmp(expected->correlation_id,CORRELATION) ||
       strcmp(expected->character_id,CHARACTER) ||
       expected->mode != ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION ||
       strcmp(expected->command_id,COMMAND) || !capability || !bridge_out ||
       strcmp(canonical_name,"Alice") || !current.owner.operation_active)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_INVALID;
    if(current.adapter_result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY)
        bridge_out->active=1;
    if(current.adapter_result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_CONSUMER_FAILED) {
        current.recursive_result=onboarding_activation_reservation_owner_attempt(
            &current.owner,directory_fd,expected,capability,canonical_name,bridge_out);
    }
    return current.adapter_result;
}

static void setup(int directory_fd)
{
    memset(&current,0,sizeof(current));
    current.directory_fd=directory_fd;
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY;
    current.owner.state=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;
    current.owner.writer_held=1;
    current.owner.player_store_installed=1;
    current.owner.player_store.state=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE;
}

static int owner_is_unchanged(
    const character_save_journal_v2_process_owner *before)
{
    return !memcmp(&current.owner,before,sizeof(current.owner));
}

int main(void)
{
    onboarding_activation_binding expected;
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_process_owner owner_before;
    int directory_fd, failed;

    directory_fd=open(".",O_RDONLY);
    if(directory_fd<0) return 1;
    failed=0; binding(&expected); memset(&capability,0,sizeof(capability));

    setup(directory_fd); owner_before=current.owner; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(0,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "null owner is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); owner_before=current.owner; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        -1,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "invalid directory FD is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); owner_before=current.owner; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,0,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "null expected binding is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); owner_before=current.owner; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,0,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "null capability is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); owner_before=current.owner; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,0,&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "null canonical name is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); owner_before=current.owner;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",0)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT &&
        !current.adapter_calls && owner_is_unchanged(&owner_before),
        "null bridge output is rejected without adapter use or owner lifecycle mutation");

    setup(directory_fd); current.owner.state=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
    memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_NOT_READY && !current.adapter_calls &&
        !current.owner.operation_active && fcntl(directory_fd,F_GETFD)>=0,
        "non-READY owner is denied without adapter or descriptor mutation");

    setup(directory_fd); current.owner.writer_held=0; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_NOT_READY && !current.adapter_calls &&
        !current.owner.operation_active,
        "owner without held writer is denied before adapter use");

    setup(directory_fd); current.owner.player_store_installed=0;
    memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_NOT_READY && !current.adapter_calls &&
        !current.owner.operation_active && fcntl(directory_fd,F_GETFD)>=0,
        "owner without an installed V4 PlayerStore is denied before reservation");

    setup(directory_fd);
    current.owner.player_store.state=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_SAVING;
    memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_BUSY && !current.adapter_calls &&
        !current.owner.operation_active,
        "non-idle PlayerStore is denied before adapter use");

    setup(directory_fd); current.owner.operation_active=1; memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_BUSY && !current.adapter_calls &&
        current.owner.operation_active,
        "active owner is denied without clearing the caller-owned lifecycle state");

    setup(directory_fd); memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY && current.adapter_calls==1 &&
        bridge.active && !current.owner.operation_active && fcntl(directory_fd,F_GETFD)>=0,
        "the exact complete tuple reaches one adapter attempt and leaves caller FD open");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_NO_CANDIDATE;
    memset(&bridge,0,sizeof(bridge));
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_NO_CANDIDATE && current.adapter_calls==1 &&
        !current.owner.operation_active && fcntl(directory_fd,F_GETFD)>=0,
        "no-candidate adapter outcome is preserved after one guarded attempt");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_TUPLE_MISMATCH && current.adapter_calls==1 &&
        !current.owner.operation_active,
        "tuple mismatch adapter outcome is preserved");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_SOURCE_BINDING_MISMATCH;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_SOURCE_BINDING_MISMATCH &&
        current.adapter_calls==1 && !current.owner.operation_active,
        "source mismatch adapter outcome is preserved");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_BRIDGE_REJECTED;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_BRIDGE_REJECTED && current.adapter_calls==1 &&
        !current.owner.operation_active,
        "bridge rejection adapter outcome is preserved");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_INVALID;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_ADAPTER_FAILED && current.adapter_calls==1 &&
        !current.owner.operation_active,
        "invalid adapter outcome fails closed after clearing the outer operation");

    setup(directory_fd);
    current.adapter_result=ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_CONSUMER_FAILED;
    failed+=expect(onboarding_activation_reservation_owner_attempt(&current.owner,
        directory_fd,&expected,&capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_ADAPTER_FAILED && current.adapter_calls==1 &&
        current.recursive_result==ONBOARDING_ACTIVATION_RESERVATION_OWNER_BUSY &&
        !current.owner.operation_active,
        "a reentrant adapter attempt is denied and the outer operation is cleared");

    close(directory_fd);
    return failed ? 1:0;
}
