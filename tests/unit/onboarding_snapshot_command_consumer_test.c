/* Descriptor-rooted, local-only activation-command reservation contract. */
#include "onboarding_activation_binding.h"
#include "onboarding_snapshot_command_consumer.h"

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

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_snapshot_command_consumer_test: %s\n", message);
    return 1;
}

static int reservation_matches(reservation)
const onboarding_snapshot_command_reservation *reservation;
{
    return reservation &&
        reservation->state == ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED &&
        !strcmp(reservation->activation.actor_user_id, ACTOR) &&
        !strcmp(reservation->activation.correlation_id, CORRELATION) &&
        !strcmp(reservation->activation.character_id, CHARACTER) &&
        reservation->activation.mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        !strcmp(reservation->activation.command_id, COMMAND);
}

int main(void)
{
    char root[] = "/tmp/muhan-snapshot-command-reservation.XXXXXX";
    char directory[1024], source_path[1024], reservation_path[1024];
    struct stat status;
    onboarding_snapshot_command_reservation reservation;
    onboarding_activation_binding source;
    int fd, failed;

    fd = -1;
    failed = 0;
    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) != 0 ||
       snprintf(directory, sizeof(directory), "%s/private-reservations", root) >=
           (int)sizeof(directory) || mkdir(directory, 0700) != 0 ||
       (fd = open(directory, O_RDONLY | O_DIRECTORY)) < 0) return 1;

    /* A missing activation binding is not a candidate and must not create a
     * reservation that an ordinary save could later reuse. */
    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE,
        "no activation candidate must not reserve a command");

    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        onboarding_snapshot_command_consumer_read(fd, COMMAND, &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && reservation_matches(&reservation) &&
        snprintf(reservation_path, sizeof(reservation_path), "%s/%s.reservation",
            directory, COMMAND) < (int)sizeof(reservation_path) &&
        stat(reservation_path, &status) == 0 &&
        S_ISREG(status.st_mode) && (status.st_mode & 0777) == 0600 &&
        status.st_nlink == 1,
        "an exact pending activation candidate must become one private reservation");

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND,
        "55555555-5555-4555-8555-555555555555",
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
            "66666666-6666-4666-8666-666666666666") ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_read(fd, COMMAND, &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && reservation_matches(&reservation),
        "a character, mode, or correlation substitution must leave the reservation intact");

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, "not-a-uuid",
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, "not-a-uuid",
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID &&
        onboarding_snapshot_command_consumer_read(fd, "not-a-uuid", &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID,
        "malformed identifiers must never become a descriptor-relative name");

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY &&
        onboarding_snapshot_command_consumer_read(fd, COMMAND, &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && reservation_matches(&reservation),
        "an exact duplicate reservation must be idempotent");

    /* Recovery needs the retained reservation and its source metadata.  The
     * consumer neither deletes nor mutates the activation source binding. */
    failed += expect(onboarding_activation_binding_path(COMMAND, source_path,
        sizeof(source_path)) == 0 && stat(source_path, &status) == 0 &&
        onboarding_activation_binding_read(COMMAND, &source) == 0 &&
        !strcmp(source.command_id, COMMAND) && close(fd) == 0 &&
        (fd = open(directory, O_RDONLY | O_DIRECTORY)) >= 0 &&
        onboarding_snapshot_command_consumer_read(fd, COMMAND, &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && reservation_matches(&reservation),
        "restart-relevant reserved evidence and its source binding must be retained");

    if(fd >= 0) close(fd);
    snprintf(reservation_path, sizeof(reservation_path), "%s/%s.reservation", directory,
        COMMAND);
    unlink(reservation_path);
    if(onboarding_activation_binding_path(COMMAND, source_path, sizeof(source_path)) == 0)
        unlink(source_path);
    snprintf(source_path, sizeof(source_path), "%s/onboarding-activation-bindings", root);
    rmdir(source_path);
    rmdir(directory);
    unsetenv("MUHAN_HOME");
    rmdir(root);
    return failed ? 1 : 0;
}
