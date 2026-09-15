/* Caller-owned ACTIVATED reservation -> bridge installation contract. */
#include "onboarding_activation_reservation_adapter.h"

#include <fcntl.h>
#include <stdio.h>
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

typedef struct fixture {
    onboarding_snapshot_command_consumer_result reserve_result;
    onboarding_snapshot_command_reservation retained;
    onboarding_activation_binding source;
    int reserve_calls, read_calls, source_calls, bridge_calls;
    int directory_fd;
} fixture;

static fixture current;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_activation_reservation_adapter_test: %s\n", message);
    return 1;
}

static void binding(onboarding_activation_binding *value)
{
    memset(value, 0, sizeof(*value));
    strcpy(value->actor_user_id, ACTOR);
    strcpy(value->correlation_id, CORRELATION);
    strcpy(value->character_id, CHARACTER);
    value->mode=ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    strcpy(value->command_id, COMMAND);
}

onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_reserve(int directory_fd,
    const char *command_id, const char *expected_actor_user_id,
    const char *expected_character_id, onboarding_activation_binding_mode expected_mode,
    const char *expected_correlation_id)
{
    current.reserve_calls++;
    if(directory_fd != current.directory_fd || fcntl(directory_fd,F_GETFD)<0 ||
       strcmp(command_id,COMMAND) || strcmp(expected_actor_user_id,ACTOR) ||
       strcmp(expected_character_id,CHARACTER) ||
       strcmp(expected_correlation_id,CORRELATION) ||
       expected_mode != ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION)
        return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID;
    return current.reserve_result;
}

onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_read(int directory_fd, const char *command_id,
    onboarding_snapshot_command_reservation *reservation)
{
    current.read_calls++;
    if(directory_fd != current.directory_fd || fcntl(directory_fd,F_GETFD)<0 ||
       strcmp(command_id,COMMAND) || !reservation)
        return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR;
    *reservation=current.retained;
    return ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED;
}

int onboarding_activation_binding_read(const char *command_id,
                                       onboarding_activation_binding *binding_out)
{
    current.source_calls++;
    if(strcmp(command_id,COMMAND) || !binding_out) return -1;
    *binding_out=current.source;
    return 0;
}

onboarding_activation_save_bridge_result
onboarding_activation_save_bridge_begin(onboarding_activation_save_bridge *bridge,
    onboarding_activation_save_capability *capability, const char *command_id,
    const char *actor_user_id, const char *correlation_id, const char *character_id,
    onboarding_activation_binding_mode mode, const char *canonical_name)
{
    current.bridge_calls++;
    if(!bridge || !capability || strcmp(command_id,COMMAND) ||
       strcmp(actor_user_id,ACTOR) || strcmp(correlation_id,CORRELATION) ||
       strcmp(character_id,CHARACTER) ||
       mode != ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION ||
       strcmp(canonical_name,"Alice"))
        return ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED;
    bridge->active=1;
    return ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY;
}

static void setup(int directory_fd)
{
    memset(&current,0,sizeof(current));
    current.directory_fd=directory_fd;
    current.reserve_result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED;
    current.retained.state=ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED;
    binding(&current.retained.activation);
    binding(&current.source);
}

int main(void)
{
    char root[]="/tmp/muhan-activation-reservation-adapter.XXXXXX";
    char directory[256];
    onboarding_activation_binding expected;
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    int directory_fd, failed;

    directory_fd=-1; failed=0;
    if(!mkdtemp(root) || snprintf(directory,sizeof(directory),"%s/private",root) >=
       (int)sizeof(directory) || mkdir(directory,0700) ||
       (directory_fd=open(directory,O_RDONLY|O_DIRECTORY))<0) return 1;
    binding(&expected); memset(&capability,0,sizeof(capability)); memset(&bridge,0,sizeof(bridge));

    setup(directory_fd);
    failed+=expect(onboarding_activation_reservation_adapter_install(directory_fd,&expected,
        &capability,"Alice",&bridge)==ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY &&
        current.reserve_calls==1 && current.read_calls==1 && current.source_calls==1 &&
        current.bridge_calls==1 && bridge.active && fcntl(directory_fd,F_GETFD)>=0,
        "an exact caller-owned descriptor and tuple install exactly one bridge without closing the caller FD");

    setup(directory_fd); memset(&bridge,0,sizeof(bridge));
    strcpy(current.source.actor_user_id,"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    failed+=expect(onboarding_activation_reservation_adapter_install(directory_fd,&expected,
        &capability,"Alice",&bridge)==
        ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_SOURCE_BINDING_MISMATCH &&
        current.reserve_calls==1 && current.read_calls==1 && current.source_calls==1 &&
        !current.bridge_calls && !bridge.active && fcntl(directory_fd,F_GETFD)>=0,
        "a retained reservation with a changed source binding must not install a bridge");

    setup(directory_fd); memset(&bridge,0,sizeof(bridge));
    strcpy(current.retained.activation.character_id,"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    failed+=expect(onboarding_activation_reservation_adapter_install(directory_fd,&expected,
        &capability,"Alice",&bridge)==ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH &&
        current.reserve_calls==1 && current.read_calls==1 && !current.source_calls &&
        !current.bridge_calls && !bridge.active && fcntl(directory_fd,F_GETFD)>=0,
        "a retained tuple substitution must stop before source or bridge use");

    setup(directory_fd); memset(&bridge,0,sizeof(bridge));
    current.reserve_result=ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE;
    failed+=expect(onboarding_activation_reservation_adapter_install(directory_fd,&expected,
        &capability,"Alice",&bridge)==ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_NO_CANDIDATE &&
        current.reserve_calls==1 && !current.read_calls && !current.source_calls &&
        !current.bridge_calls && !bridge.active && fcntl(directory_fd,F_GETFD)>=0,
        "no candidate must not read, revalidate, install, or close the caller descriptor");

    if(directory_fd>=0) close(directory_fd);
    rmdir(directory); rmdir(root);
    return failed ? 1:0;
}
