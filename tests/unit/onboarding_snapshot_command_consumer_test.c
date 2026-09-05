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
#define TEMP_ONLY_COMMAND "55555555-5555-4555-8555-555555555555"
#define LINKED_COMMAND "66666666-6666-4666-8666-666666666666"
#define MALFORMED_COMMAND "77777777-7777-4777-8777-777777777777"
#define AMBIGUOUS_COMMAND "88888888-8888-4888-8888-888888888888"
#define NO_CANDIDATE_COMMAND "99999999-9999-4999-8999-999999999999"

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

static int reservation_paths(directory, command_id, reservation_path, temporary_path)
const char *directory;
const char *command_id;
char *reservation_path;
char *temporary_path;
{
    return snprintf(reservation_path, 1024, "%s/%s.reservation", directory, command_id) >= 1024 ||
        snprintf(temporary_path, 1024, "%s/.%s.reservation.tmp", directory, command_id) >= 1024 ?
        -1:0;
}

static int write_text(path, text)
const char *path;
const char *text;
{
    int file, length;
    file=open(path, O_WRONLY|O_CREAT|O_EXCL, 0600);
    if(file < 0) return -1;
    length=(int)strlen(text);
    if(write(file, text, (unsigned long)length) != length || close(file)) return -1;
    return 0;
}

static int copy_file(source, destination)
const char *source;
const char *destination;
{
    char text[512];
    int input, output, count;
    input=open(source, O_RDONLY); output=-1;
    if(input < 0) return -1;
    count=read(input, text, sizeof(text));
    if(count <= 0 || read(input, text, 1) != 0 || close(input)) return -1;
    input=-1;
    output=open(destination, O_WRONLY|O_CREAT|O_EXCL, 0600);
    if(output < 0 || write(output, text, (unsigned long)count) != count || close(output)) return -1;
    return 0;
}

static int source_exists(command_id)
const char *command_id;
{
    char path[1024];
    struct stat status;
    return onboarding_activation_binding_path(command_id, path, sizeof(path)) == 0 &&
        stat(path, &status) == 0 && S_ISREG(status.st_mode);
}

int main(void)
{
    char root[] = "/tmp/muhan-snapshot-command-reservation.XXXXXX";
    char directory[1024], source_path[1024], reservation_path[1024], temporary_path[1024];
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
    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE,
        "no activation candidate must not reserve a command");

    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR, CHARACTER,
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

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR,
        "55555555-5555-4555-8555-555555555555",
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
            "66666666-6666-4666-8666-666666666666") ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND,
            "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH &&
        onboarding_snapshot_command_consumer_read(fd, COMMAND, &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED && reservation_matches(&reservation),
        "an actor, character, mode, or correlation substitution must leave the reservation intact");

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, "not-a-uuid", ACTOR,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID &&
        onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR, "not-a-uuid",
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID &&
        onboarding_snapshot_command_consumer_read(fd, "not-a-uuid", &reservation) ==
        ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID,
        "malformed identifiers must never become a descriptor-relative name");

    failed += expect(onboarding_snapshot_command_consumer_reserve(fd, COMMAND, ACTOR,
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

    /* A process may die after writing and syncing the deterministic PENDING
     * file, before it publishes the final name.  A matching retry must finish
     * that one transition, retain the source, and leave no PENDING name. */
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, TEMP_ONLY_COMMAND) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, TEMP_ONLY_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        reservation_paths(directory, TEMP_ONLY_COMMAND, reservation_path, temporary_path) == 0 &&
        link(reservation_path, temporary_path) == 0 && unlink(reservation_path) == 0 &&
        stat(temporary_path, &status) == 0 && status.st_nlink == 1 &&
        onboarding_snapshot_command_consumer_read(fd, TEMP_ONLY_COMMAND, &reservation) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        onboarding_snapshot_command_consumer_reserve(fd, TEMP_ONLY_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY &&
        onboarding_snapshot_command_consumer_read(fd, TEMP_ONLY_COMMAND, &reservation) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        !strcmp(reservation.activation.command_id, TEMP_ONLY_COMMAND) &&
        lstat(temporary_path, &status) != 0 && stat(reservation_path, &status) == 0 &&
        status.st_nlink == 1 && source_exists(TEMP_ONLY_COMMAND),
        "a crash before finalization must resume the exact pending reservation");

    /* linkat publishes before unlinkat.  The two deterministic names are a
     * valid, recoverable intermediate only when they are the same safe inode. */
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, LINKED_COMMAND) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, LINKED_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        reservation_paths(directory, LINKED_COMMAND, reservation_path, temporary_path) == 0 &&
        link(reservation_path, temporary_path) == 0 && stat(reservation_path, &status) == 0 &&
        status.st_nlink == 2 && onboarding_snapshot_command_consumer_read(fd,
            LINKED_COMMAND, &reservation) == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT &&
        onboarding_snapshot_command_consumer_reserve(fd,
        LINKED_COMMAND, ACTOR, CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
            CORRELATION) == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY &&
        lstat(temporary_path, &status) != 0 && stat(reservation_path, &status) == 0 &&
        status.st_nlink == 1 && source_exists(LINKED_COMMAND),
        "a final-plus-temp hardlink pair must finalize on exact retry");

    /* A leftover is not permission for broad cleanup.  Malformed evidence is
     * corrupt and remains available for investigation while its source stays. */
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, MALFORMED_COMMAND) == 0 &&
        reservation_paths(directory, MALFORMED_COMMAND, reservation_path, temporary_path) == 0 &&
        write_text(temporary_path, "not-a-reservation\n") == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, MALFORMED_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT &&
        lstat(temporary_path, &status) == 0 && source_exists(MALFORMED_COMMAND),
        "a malformed pending leftover must not be deleted or retried as a reservation");

    /* Same text in a distinct inode is ambiguous: only the exact linkat
     * intermediate may be finalized. */
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, AMBIGUOUS_COMMAND) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, AMBIGUOUS_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
        reservation_paths(directory, AMBIGUOUS_COMMAND, reservation_path, temporary_path) == 0 &&
        copy_file(reservation_path, temporary_path) == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, AMBIGUOUS_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT &&
        stat(reservation_path, &status) == 0 && lstat(temporary_path, &status) == 0 &&
        source_exists(AMBIGUOUS_COMMAND),
        "a distinct valid-looking pending file must remain corrupt and isolated");

    /* No source binding remains no candidate even when a same-name temporary
     * exists; the consumer must neither adopt nor remove it by default. */
    failed += expect(reservation_paths(directory, NO_CANDIDATE_COMMAND, reservation_path,
            temporary_path) == 0 && write_text(temporary_path, "orphan\n") == 0 &&
        onboarding_snapshot_command_consumer_reserve(fd, NO_CANDIDATE_COMMAND, ACTOR, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, CORRELATION) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        onboarding_snapshot_command_consumer_read(fd, NO_CANDIDATE_COMMAND, &reservation) ==
            ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE &&
        lstat(temporary_path, &status) == 0,
        "no candidate and reads must ignore and preserve unrelated temporary leftovers");

    if(fd >= 0) close(fd);
    reservation_paths(directory, COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    reservation_paths(directory, TEMP_ONLY_COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    reservation_paths(directory, LINKED_COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    reservation_paths(directory, MALFORMED_COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    reservation_paths(directory, AMBIGUOUS_COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    reservation_paths(directory, NO_CANDIDATE_COMMAND, reservation_path, temporary_path);
    unlink(reservation_path); unlink(temporary_path);
    if(onboarding_activation_binding_path(COMMAND, source_path, sizeof(source_path)) == 0) unlink(source_path);
    if(onboarding_activation_binding_path(TEMP_ONLY_COMMAND, source_path, sizeof(source_path)) == 0) unlink(source_path);
    if(onboarding_activation_binding_path(LINKED_COMMAND, source_path, sizeof(source_path)) == 0) unlink(source_path);
    if(onboarding_activation_binding_path(MALFORMED_COMMAND, source_path, sizeof(source_path)) == 0) unlink(source_path);
    if(onboarding_activation_binding_path(AMBIGUOUS_COMMAND, source_path, sizeof(source_path)) == 0) unlink(source_path);
    snprintf(source_path, sizeof(source_path), "%s/onboarding-activation-bindings", root);
    rmdir(source_path);
    rmdir(directory);
    unsetenv("MUHAN_HOME");
    rmdir(root);
    return failed ? 1 : 0;
}
