#include "alias_title_snapshot_v1.h"
#include "alias_title_snapshot_v1_observer.h"
#include "cdto_v1.h"
#include "mstruct.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

extern int add_alias(int, char *, char *);
extern void save_alias(creature *);
extern char *ply_titles[PMAX];

static char test_output[256];
static char expected_output[1024];
static size_t expected_length;
static int last_save_handle = -1;
static int observer_calls;
static int observer_saw_closed_handle;
static int observer_saw_complete_file;
static int observer_saw_bounded_canonical_wire;
static int observer_saw_snapshot_values;
static int failing_observer_calls;
static int force_open_failure;
static int legacy_print_calls;
static creature *callback_player;
static int reentrant_observer_calls;
static int registering_observer_calls;
static int replacement_observer_calls;
static int clearing_observer_calls;
static int replacement_saw_context;
static char replacement_context;

/* save_alias's legacy errors are irrelevant to this focused write seam test. */
void print()
{
    ++legacy_print_calls;
}

void merror()
{
}

int rp_open(path, flags, mode)
const char *path;
int flags, mode;
{
    (void)path;
    if(force_open_failure) return -1;
    last_save_handle = open(test_output, flags, mode);
    return last_save_handle;
}

static int file_is_complete(void)
{
    char actual[sizeof(expected_output)];
    int handle;
    ssize_t amount;

    handle = open(test_output, O_RDONLY);
    if(handle < 0) return 0;
    amount = read(handle, actual, sizeof(actual));
    close(handle);
    return amount == (ssize_t)expected_length &&
        !memcmp(actual, expected_output, expected_length);
}

static int observing_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    alias_title_snapshot_v1 snapshot;

    (void)context;
    ++observer_calls;
    errno = 0;
    observer_saw_closed_handle =
        fcntl(last_save_handle, F_GETFD) == -1 && errno == EBADF;
    observer_saw_complete_file = file_is_complete();
    observer_saw_bounded_canonical_wire =
        wire_length <= CDTO_V1_ALIAS_TITLE_SNAPSHOT_PAYLOAD_LIMIT +
        CDTO_V1_PREFIX_LENGTH + CDTO_V1_DIGEST_LENGTH &&
        alias_title_snapshot_v1_decode(wire, wire_length, &snapshot) == CDTO_V1_OK;
    observer_saw_snapshot_values = observer_saw_bounded_canonical_wire &&
        snapshot.alias_count == 1U &&
        snapshot.aliases[0].alias_length == ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES &&
        snapshot.aliases[0].process_length == ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES &&
        snapshot.title_present == 1U &&
        snapshot.title_length == ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES;
    return 0;
}

static int failing_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    (void)wire;
    (void)wire_length;
    (void)context;
    ++failing_observer_calls;
    return -1;
}

static int reentrant_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    (void)wire;
    (void)wire_length;
    (void)context;
    ++reentrant_observer_calls;
    save_alias(callback_player);
    return -1;
}

static int replacement_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    (void)wire;
    (void)wire_length;
    ++replacement_observer_calls;
    replacement_saw_context = context == &replacement_context;
    return 0;
}

static int registering_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    (void)wire;
    (void)wire_length;
    (void)context;
    ++registering_observer_calls;
    alias_title_snapshot_v1_observer_register(replacement_callback,
        &replacement_context);
    return 0;
}

static int clearing_callback(wire, wire_length, context)
const uint8_t *wire;
size_t wire_length;
void *context;
{
    (void)wire;
    (void)wire_length;
    (void)context;
    ++clearing_observer_calls;
    alias_title_snapshot_v1_observer_clear();
    return 0;
}

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "alias_title_snapshot_v1_observer_test: %s\n", message);
    return 1;
}

int main(void)
{
    creature player;
    char alias[ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES + 1U];
    char process[ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES + 1U];
    char title[ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES + 1U];
    int temporary, failed;

    strcpy(test_output, "/tmp/muhan-alias-title-observer-XXXXXX");
    temporary = mkstemp(test_output);
    if(temporary < 0) return 2;
    close(temporary);
    memset(&player, 0, sizeof(player));
    player.fd = 7;
    callback_player = &player;
    strcpy(player.name, "observer-test");
    memset(alias, 'a', sizeof(alias) - 1U); alias[sizeof(alias) - 1U] = 0;
    memset(process, 'p', sizeof(process) - 1U); process[sizeof(process) - 1U] = 0;
    memset(title, 't', sizeof(title) - 1U); title[sizeof(title) - 1U] = 0;
    if(add_alias(player.fd, alias, process) != 0) return 2;
    ply_titles[player.fd] = (char *)malloc(sizeof(title));
    if(!ply_titles[player.fd]) return 2;
    memcpy(ply_titles[player.fd], title, sizeof(title));
    expected_length = (size_t)snprintf(expected_output, sizeof(expected_output),
        "%s\n%s\n~!\n%s\n~!\n", alias, process, title);
    if(expected_length >= sizeof(expected_output)) return 2;

    failed = 0;
    alias_title_snapshot_v1_observer_clear();
    save_alias(&player);
    failed |= expect(observer_calls == 0 && file_is_complete(),
        "default-unregistered observer must be detached from the legacy save");

    alias_title_snapshot_v1_observer_register(observing_callback, NULL);
    force_open_failure = 1;
    save_alias(&player);
    force_open_failure = 0;
    failed |= expect(observer_calls == 0 && legacy_print_calls == 1 &&
        file_is_complete(),
        "legacy open failure must retain its error path and skip observation");

    save_alias(&player);
    failed |= expect(observer_calls == 1 && observer_saw_closed_handle &&
        observer_saw_complete_file,
        "registered observer must run only after the legacy file is complete and closed");
    failed |= expect(observer_saw_bounded_canonical_wire &&
        observer_saw_snapshot_values,
        "observer must receive a bounded, digest-verified AliasTitleSnapshotV1 wire");

    alias_title_snapshot_v1_observer_register(failing_callback, NULL);
    save_alias(&player);
    failed |= expect(failing_observer_calls == 1 && file_is_complete(),
        "observer failure must be ignored without changing the legacy save result");

    alias_title_snapshot_v1_observer_register(reentrant_callback, NULL);
    save_alias(&player);
    failed |= expect(reentrant_observer_calls == 1 && file_is_complete(),
        "an observer-triggered nested save must not recursively notify");
    save_alias(&player);
    failed |= expect(reentrant_observer_calls == 2 && file_is_complete(),
        "the callback-in-progress guard must clear after every callback path");

    alias_title_snapshot_v1_observer_register(registering_callback, NULL);
    save_alias(&player);
    failed |= expect(registering_observer_calls == 1 &&
        replacement_observer_calls == 0,
        "registration during a callback must affect only a later save");
    save_alias(&player);
    failed |= expect(replacement_observer_calls == 1 && replacement_saw_context,
        "a later save must use the callback and context registered in the prior callback");

    alias_title_snapshot_v1_observer_register(clearing_callback, NULL);
    save_alias(&player);
    save_alias(&player);
    failed |= expect(clearing_observer_calls == 1 &&
        replacement_observer_calls == 1 && file_is_complete(),
        "clearing during a callback must suppress only later notifications");
    alias_title_snapshot_v1_observer_clear();
    unlink(test_output);
    return failed ? 1 : 0;
}
