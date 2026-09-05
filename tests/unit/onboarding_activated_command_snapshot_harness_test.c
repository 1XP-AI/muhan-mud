/*
 * Evidence-only composition for the still-local reservation consumer.
 *
 * This deliberately is not production wiring: no runtime-owned reservation
 * descriptor exists today.  It proves the exact adapter contract a future
 * owner must preserve: a reservation may project only its own full tuple into
 * the existing descriptor-local V4 candidate bridge.
 */
#include "onboarding_snapshot_command_consumer.h"
#include "onboarding_activation_save_bridge.h"

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
#define NONMATCH_COMMAND "55555555-5555-4555-8555-555555555555"
#define STALE_COMMAND "66666666-6666-4666-8666-666666666666"

static int candidate_dispatches;

int player_name_is_valid(name, minimum, maximum)
const unsigned char *name;
unsigned long minimum;
unsigned long maximum;
{
    (void)minimum;
    return name && name[0] && strlen((const char *)name) <= maximum;
}

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activated_command_snapshot_harness_test: %s\n",
        message);
    return 1;
}

static int reservation_path(directory, command, output, output_size)
const char *directory;
const char *command;
char *output;
unsigned long output_size;
{
    int length;
    length=snprintf(output,output_size,"%s/%s.reservation",directory,command);
    return length<0 || (unsigned long)length>=output_size ? -1:0;
}

static int source_path(command, output, output_size)
const char *command;
char *output;
unsigned long output_size;
{ return onboarding_activation_binding_path(command,output,output_size); }

static int capture_reservation(capability, reservation, name)
onboarding_activation_save_capability *capability;
const onboarding_snapshot_command_reservation *reservation;
const char *name;
{
    return onboarding_activation_save_capability_capture(capability,
        reservation->activation.actor_user_id,
        reservation->activation.correlation_id,
        reservation->activation.character_id,reservation->activation.mode,
        reservation->activation.command_id,name);
}

/* A held reservation is only a dispatch candidate while its durable ACTIVATED
 * source still names the same full tuple.  This is deliberately local to the
 * evidence harness: production has no owner for the reservation descriptor. */
static int reservation_source_current(reservation)
const onboarding_snapshot_command_reservation *reservation;
{
    onboarding_activation_binding source;

    memset(&source,0,sizeof(source));
    return reservation && onboarding_activation_binding_read(
        reservation->activation.command_id,&source)==0 &&
        !strcmp(source.actor_user_id,reservation->activation.actor_user_id) &&
        !strcmp(source.correlation_id,reservation->activation.correlation_id) &&
        !strcmp(source.character_id,reservation->activation.character_id) &&
        source.mode==reservation->activation.mode &&
        !strcmp(source.command_id,reservation->activation.command_id);
}

/* This is the missing adapter's narrow output boundary.  It uses the actual
 * V4 candidate type and bridge, but does not pretend that the local-only
 * consumer has a production owner or may install a PlayerStore resolver. */
static int dispatch_candidate(capability, reservation, candidate)
onboarding_activation_save_capability *capability;
const onboarding_snapshot_command_reservation *reservation;
character_save_journal_v2_protocol_candidate_v4 *candidate;
{
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_writer_tuple writer;
    character_save_journal_v2_bound_route_v3 route;
    character_save_journal_v2_protocol_report report;
    unsigned long length;

    if(candidate) memset(candidate,0,sizeof(*candidate));
    if(!capability || !reservation || !candidate) return -1;
    if(!reservation_source_current(reservation)) return 0;
    memset(&bridge,0,sizeof(bridge)); memset(&writer,0,sizeof(writer));
    memset(&route,0,sizeof(route)); memset(&report,0,sizeof(report));
    if(onboarding_activation_save_bridge_begin(&bridge,capability,
       reservation->activation.command_id,reservation->activation.actor_user_id,
       reservation->activation.correlation_id,reservation->activation.character_id,
       reservation->activation.mode,"Alice") != ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY)
        return 0;
    strcpy(writer.world_id,"m3-a");
    strcpy(writer.writer_instance_id,"77777777-7777-4777-8777-777777777777");
    writer.writer_epoch=7;
    strcpy(route.character_id,reservation->activation.character_id);
    memcpy(route.legacy_name,"Alice",5); route.legacy_name_length=5;
    length=5;
    if(onboarding_activation_save_bridge_resolve(&bridge,&writer,&route,
       (const unsigned char *)"Alice",length,candidate)!=1) return -1;
    candidate_dispatches++;
    report.reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;
    return onboarding_activation_save_bridge_finish(&bridge,&report) ==
        ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED ? 1:-1;
}

static int publish_retry(capability, reservation)
onboarding_activation_save_capability *capability;
const onboarding_snapshot_command_reservation *reservation;
{
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_protocol_report report;

    memset(&bridge,0,sizeof(bridge)); memset(&report,0,sizeof(report));
    if(onboarding_activation_save_bridge_begin(&bridge,capability,
       reservation->activation.command_id,reservation->activation.actor_user_id,
       reservation->activation.correlation_id,reservation->activation.character_id,
       reservation->activation.mode,"Alice") != ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY)
        return -1;
    report.reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    return onboarding_activation_save_bridge_finish(&bridge,&report)==
        ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED ? 0:-1;
}

int main(void)
{
    char root[]="/tmp/muhan-activated-command-snapshot.XXXXXX";
    char directory[512], path[512];
    onboarding_snapshot_command_reservation reservation, stale;
    onboarding_activation_binding source;
    onboarding_activation_save_capability capability;
    character_save_journal_v2_protocol_candidate_v4 candidate;
    struct stat status;
    int directory_fd, failed;

    directory_fd=-1; failed=0; memset(&reservation,0,sizeof(reservation));
    memset(&stale,0,sizeof(stale)); memset(&source,0,sizeof(source));
    memset(&capability,0,sizeof(capability));
    memset(&candidate,0,sizeof(candidate));
    if(!mkdtemp(root) || setenv("MUHAN_HOME",root,1) ||
       snprintf(directory,sizeof(directory),"%s/reservations",root)>=(int)sizeof(directory) ||
       mkdir(directory,0700) || (directory_fd=open(directory,O_RDONLY|O_DIRECTORY))<0)
        return 1;

    /* Feature-off has no reservation-consumer call, no capability, and no
     * candidate dispatch; ordinary legacy completion remains outside this
     * harness and is covered by m3-feature-off-legacy-authority-test. */
    unsetenv("MUD_M3_MODE"); unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    failed+=expect(onboarding_activation_save_capability_capture(&capability,
        ACTOR,CORRELATION,CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        COMMAND,"Alice")==ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED &&
        !capability.armed && !candidate_dispatches &&
        reservation_path(directory,COMMAND,path,sizeof(path))==0 && lstat(path,&status)<0,
        "feature-off creates no reservation capability or snapshot candidate");

    setenv("MUD_M3_MODE","shadow",1);
    setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1);

    /* Before an accepted ACTIVATED there is no durable source binding, so an
     * exact-looking command remains unreserved and cannot dispatch. */
    failed+=expect(onboarding_snapshot_command_consumer_reserve(directory_fd,COMMAND,
        CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        onboarding_snapshot_command_consumer_read(directory_fd,COMMAND,&reservation)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE && !candidate_dispatches,
        "pre-ACTIVATED command retains without a snapshot dispatch");

    failed+=expect(onboarding_activation_binding_write(ACTOR,CORRELATION,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,NONMATCH_COMMAND)==0 &&
        onboarding_snapshot_command_consumer_reserve(directory_fd,NONMATCH_COMMAND,
        CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_read(directory_fd,NONMATCH_COMMAND,
        &reservation)==ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        !candidate_dispatches,
        "nonmatching mode retains the command without a snapshot dispatch");

    failed+=expect(onboarding_activation_binding_write(ACTOR,CORRELATION,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,COMMAND)==0 &&
        onboarding_snapshot_command_consumer_reserve(directory_fd,COMMAND,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        onboarding_snapshot_command_consumer_reserve(directory_fd,COMMAND,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY &&
        onboarding_snapshot_command_consumer_read(directory_fd,COMMAND,&reservation)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        !strcmp(reservation.activation.command_id,COMMAND) &&
        !strcmp(reservation.activation.character_id,CHARACTER) &&
        !strcmp(reservation.activation.correlation_id,CORRELATION) &&
        reservation.activation.mode==ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        capture_reservation(&capability,&reservation,"Alice")==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        dispatch_candidate(&capability,&reservation,&candidate)==1 &&
        !strcmp(candidate.command_uuid,COMMAND) &&
        !strcmp(candidate.character_id,CHARACTER) && candidate_dispatches==1 &&
        capability.armed && publish_retry(&capability,&reservation)==0 && !capability.armed,
        "one exact ACTIVATED reservation projects its identity into the V4 candidate and retries idempotently");

    candidate_dispatches=0;
    failed+=expect(onboarding_activation_binding_write(ACTOR,CORRELATION,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,STALE_COMMAND)==0 &&
        onboarding_snapshot_command_consumer_reserve(directory_fd,STALE_COMMAND,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        onboarding_snapshot_command_consumer_read(directory_fd,STALE_COMMAND,&stale)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        dispatch_candidate(&capability,&stale,&candidate)==0 && !candidate_dispatches &&
        capture_reservation(&capability,&stale,"Alice")==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        source_path(STALE_COMMAND,path,sizeof(path))==0 && unlink(path)==0 &&
        onboarding_activation_binding_read(STALE_COMMAND,&source)!=0 &&
        onboarding_snapshot_command_consumer_read(directory_fd,STALE_COMMAND,&stale)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        onboarding_snapshot_command_consumer_reserve(directory_fd,STALE_COMMAND,CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,CORRELATION)==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        dispatch_candidate(&capability,&stale,&candidate)==0 && capability.armed &&
        !candidate_dispatches && !candidate.command_uuid[0],
        "removed ACTIVATED source after reservation retains without V4 dispatch or consumption");

    if(directory_fd>=0) close(directory_fd);
    if(reservation_path(directory,COMMAND,path,sizeof(path))==0) unlink(path);
    if(reservation_path(directory,NONMATCH_COMMAND,path,sizeof(path))==0) unlink(path);
    if(reservation_path(directory,STALE_COMMAND,path,sizeof(path))==0) unlink(path);
    if(source_path(COMMAND,path,sizeof(path))==0) unlink(path);
    if(source_path(NONMATCH_COMMAND,path,sizeof(path))==0) unlink(path);
    if(source_path(STALE_COMMAND,path,sizeof(path))==0) unlink(path);
    snprintf(path,sizeof(path),"%s/onboarding-activation-bindings",root); rmdir(path);
    rmdir(directory); unsetenv("MUHAN_HOME"); rmdir(root);
    unsetenv("MUD_M3_MODE"); unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed ? 1:0;
}
