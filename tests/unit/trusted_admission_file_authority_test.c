/* Hermetic MUD1 admission/file-authority boundary.  The ticket parser is
 * real; load_ply is a controlled legacy-file fixture so no socket, database,
 * gateway, or runtime filesystem is involved. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "mstruct.h"
#include "player_store.h"
#include "trusted_admission.h"

#define TEST_SECRET "0123456789abcdef0123456789abcdef"
#define TEST_FD 3

int Tablesize = 1;
int Spy[PMAX];
struct cmdstruct {
    char *cmdstr;
    int cmdno;
    int (*cmdfn)();
};
struct {
    creature *ply;
    iobuf *io;
    extra *extr;
} Ply[PMAX];
struct cmdstruct cmdlist[1];

typedef enum fixture_result {
    FIXTURE_OK,
    FIXTURE_NOT_FOUND,
    FIXTURE_MISMATCHED_NAME,
    FIXTURE_CORRUPT
} fixture_result;

static fixture_result fixture_results[2];
static int load_calls;
static int disconnect_calls;
static int write_calls;
static char last_write[16];

static creature *fixture_player(const char *name)
{
    creature *player;

    player = (creature *)calloc(1, sizeof(*player));
    if(!player) return 0;
    strcpy(player->name, name);
    return player;
}

int load_ply(name, player)
char *name;
creature **player;
{
    fixture_result result;

    (void)name;
    if(!player) return PLAYER_STORE_IO_ERROR;
    *player = 0;
    result = fixture_results[load_calls < 2 ? load_calls : 1];
    load_calls++;
    switch(result) {
    case FIXTURE_OK:
        *player = fixture_player("Alice");
        return *player ? PLAYER_STORE_OK : PLAYER_STORE_IO_ERROR;
    case FIXTURE_NOT_FOUND:
        return PLAYER_STORE_NOT_FOUND;
    case FIXTURE_MISMATCHED_NAME:
        *player = fixture_player("Mallory");
        return *player ? PLAYER_STORE_OK : PLAYER_STORE_IO_ERROR;
    case FIXTURE_CORRUPT:
        return PLAYER_STORE_CORRUPT;
    }
    return PLAYER_STORE_IO_ERROR;
}

void free_crt(player)
creature *player;
{
    free(player);
}

int scwrite(fd, text, length)
int fd;
char *text;
int length;
{
    (void)fd;
    write_calls++;
    if(length >= (int)sizeof(last_write)) length = sizeof(last_write) - 1;
    memcpy(last_write, text, (size_t)length);
    last_write[length] = 0;
    return length;
}

void disconnect(fd)
int fd;
{
    (void)fd;
    disconnect_calls++;
}

int player_recovery_login_blocked(void)
{
    return 0;
}

int checkdouble(name)
char *name;
{
    (void)name;
    return 0;
}

void check_item(player)
creature *player;
{
    (void)player;
}

int init_ply(player)
creature *player;
{
    (void)player;
    return 0;
}

void init_alias(player)
creature *player;
{
    (void)player;
}

int alias_cmd(void)
{
    return 0;
}

void log_dmcmd(void)
{
}

void lowercize(void)
{
}

void print(void)
{
}

int special_cmd(void)
{
    return 0;
}

extern void trusted_admission_login();

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "trusted_admission_file_authority_test: %s\n", message);
    return 1;
}

static void signed_ticket(out)
char *out;
{
    char signed_part[190];
    char mac[TRUSTED_ADMISSION_HMAC_HEX_LEN + 1];

    sprintf(signed_part,
            "MUD1|%ld|00112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|416c696365",
            (long)time(0) + 1L);
    trusted_admission_hmac_hex(TEST_SECRET, signed_part, mac);
    sprintf(out, "%s|%s", signed_part, mac);
}

static void reset_fixture(first, second)
fixture_result first;
fixture_result second;
{
    static iobuf io;
    static extra extr;

    if(Ply[TEST_FD].ply) {
        free_crt(Ply[TEST_FD].ply);
        Ply[TEST_FD].ply = 0;
    }
    memset(Ply, 0, sizeof(Ply));
    memset(&io, 0, sizeof(io));
    memset(&extr, 0, sizeof(extr));
    Ply[TEST_FD].io = &io;
    Ply[TEST_FD].extr = &extr;
    fixture_results[0] = first;
    fixture_results[1] = second;
    load_calls = disconnect_calls = write_calls = 0;
    last_write[0] = 0;
    trusted_admission_set_secret_for_test(TEST_SECRET);
}

static int rejection_case(label, first, second, expected_loads)
const char *label;
fixture_result first;
fixture_result second;
int expected_loads;
{
    char line[TRUSTED_ADMISSION_MAX_LINE + 1];
    int failed;

    reset_fixture(first, second);
    signed_ticket(line);
    trusted_admission_login(TEST_FD, 1, (unsigned char *)line);
    failed = 0;
    failed += expect(load_calls == expected_loads, label);
    failed += expect(write_calls == 1 && !strcmp(last_write, "MUD1 ERR\n"),
                     "unusable legacy-file identity must emit only MUD1 ERR");
    failed += expect(disconnect_calls == 1 && Ply[TEST_FD].ply == 0,
                     "unusable legacy-file identity must not create a session");
    return failed;
}

int main(void)
{
    char line[TRUSTED_ADMISSION_MAX_LINE + 1];
    int failed;

    failed = 0;
    reset_fixture(FIXTURE_OK, FIXTURE_OK);
    signed_ticket(line);
    trusted_admission_login(TEST_FD, 1, (unsigned char *)line);
    failed += expect(load_calls == 2,
                     "success requires both canonical-file preflight and reload");
    failed += expect(write_calls == 1 && !strcmp(last_write, "MUD1 OK\n") &&
                     !disconnect_calls && Ply[TEST_FD].ply &&
                     !strcmp(Ply[TEST_FD].ply->name, "Alice"),
                     "only a usable canonical legacy file may emit MUD1 OK");

    failed += rejection_case("absent canonical player file must fail at preflight",
                             FIXTURE_NOT_FOUND, FIXTURE_OK, 1);
    failed += rejection_case("mismatched canonical player name must fail at preflight",
                             FIXTURE_MISMATCHED_NAME, FIXTURE_OK, 1);
    failed += rejection_case("corrupt canonical player file must fail at preflight",
                             FIXTURE_CORRUPT, FIXTURE_OK, 1);
    failed += rejection_case("absent canonical player file must fail at reload",
                             FIXTURE_OK, FIXTURE_NOT_FOUND, 2);
    failed += rejection_case("mismatched canonical player name must fail at reload",
                             FIXTURE_OK, FIXTURE_MISMATCHED_NAME, 2);
    failed += rejection_case("corrupt canonical player file must fail at reload",
                             FIXTURE_OK, FIXTURE_CORRUPT, 2);

    reset_fixture(FIXTURE_OK, FIXTURE_OK);
    strcpy(line, "MUD1|malformed");
    trusted_admission_login(TEST_FD, 1, (unsigned char *)line);
    failed += expect(!load_calls && write_calls == 1 &&
                     !strcmp(last_write, "MUD1 ERR\n") && disconnect_calls == 1,
                     "malformed MUD1 identity must not reach file authority or success");

    trusted_admission_reset_for_test();
    if(failed) return 1;
    puts("trusted_admission_file_authority_test: ok");
    return 0;
}
