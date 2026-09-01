#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "onboarding_admission.h"
#include "onboarding_session.h"
#include "trusted_admission.h"
#include "mstruct.h"

#define TEST_SECRET "0123456789abcdef0123456789abcdef"
#define FIXTURE_NOW 1788268990L

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_admission_test: %s\n", message);
    return 1;
}

typedef struct replay_log {
    char nonce[ONBOARDING_ADMISSION_NONCE_LEN + 1];
    long expires_at;
    int calls;
    int reject;
} replay_log;

static int consume_nonce(context, nonce, expires_at)
void *context;
const char *nonce;
long expires_at;
{
    replay_log *log = (replay_log *)context;
    log->calls++;
    if(log->reject || log->nonce[0]) return -1;
    strcpy(log->nonce, nonce);
    log->expires_at = expires_at;
    return 0;
}

static void finish_ticket(out, signed_part)
char *out;
const char *signed_part;
{
    char mac[TRUSTED_ADMISSION_HMAC_HEX_LEN + 1];
    trusted_admission_hmac_hex(TEST_SECRET, signed_part, mac);
    sprintf(out, "%s|%s\n", signed_part, mac);
}

static int test_fixture_tickets(void)
{
    static const char provision_ticket[] =
        "MUD1O|P|1788269000|00112233445566778899aabbccddeeff|"
        "11111111-1111-4111-8111-111111111111|"
        "22222222-2222-4222-8222-222222222222|"
        "b9c4f1e6bf4d17608c7bdfca6cd5d87a79b42b13c00cff731bb99d1d7200d9fe\n";
    static const char claim_ticket[] =
        "MUD1O|C|1788269000|ffeeddccbbaa99887766554433221100|"
        "33333333-3333-4333-8333-333333333333|"
        "44444444-4444-4444-8444-444444444444|"
        "86a8ba01472efbc25f0445a9a01c77484e0f2df5073c8510a43dc56a1259ceeb\n";
    onboarding_admission_ticket ticket;
    replay_log replay;
    int failed = 0;

    memset(&replay, 0, sizeof(replay));
    failed += expect(onboarding_admission_validate_ticket(provision_ticket, TEST_SECRET,
                     FIXTURE_NOW, consume_nonce, &replay, &ticket) == 0 &&
                     ticket.mode == ONBOARDING_ADMISSION_MODE_PROVISION &&
                     strcmp(ticket.user_id, "11111111-1111-4111-8111-111111111111") == 0 &&
                     strcmp(ticket.correlation_id, "22222222-2222-4222-8222-222222222222") == 0 &&
                     strcmp(ticket.nonce, "00112233445566778899aabbccddeeff") == 0 &&
                     ticket.expires_at == 1788269000L && replay.calls == 1 &&
                     strcmp(replay.nonce, ticket.nonce) == 0,
                     "fixture provision ticket must verify and consume its nonce");

    memset(&replay, 0, sizeof(replay));
    failed += expect(onboarding_admission_validate_ticket(claim_ticket, TEST_SECRET,
                     FIXTURE_NOW, consume_nonce, &replay, &ticket) == 0 &&
                     ticket.mode == ONBOARDING_ADMISSION_MODE_CLAIM &&
                     strcmp(ticket.user_id, "33333333-3333-4333-8333-333333333333") == 0 &&
                     strcmp(ticket.correlation_id, "44444444-4444-4444-8444-444444444444") == 0 &&
                     replay.calls == 1,
                     "fixture claim ticket must verify and consume its nonce");
    return failed;
}

static int test_ticket_rejections(void)
{
    char signed_part[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char line[ONBOARDING_ADMISSION_MAX_LINE + 3];
    onboarding_admission_ticket ticket;
    replay_log replay;
    int failed = 0;

    sprintf(signed_part, "MUD1O|P|1000|10112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    memset(&replay, 0, sizeof(replay));
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) == 0,
                     "expiry equal to now must be accepted");

    strcpy(line + strlen(line) - 2, "0\n");
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0 && replay.calls == 1,
                     "signature mismatch must reject before replay callback");

    sprintf(signed_part, "MUD1O|P|999|20112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "expired ticket must fail before replay callback");

    sprintf(signed_part, "MUD1O|X|1000|20112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "only P and C ticket modes may be accepted");

    sprintf(signed_part, "MUD1O|P|1000|A0112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "nonce must be exactly 32 lowerhex characters");

    sprintf(signed_part, "MUD1O|P|1031|30112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "ticket future bound must be 30 seconds");

    sprintf(signed_part, "MUD1O|P|1000|40112233445566778899aabbccddeeff|"
            "123E4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "UUIDs must be canonical lowerhex");

    sprintf(signed_part, "MUD1O|P|1000|50112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001|extra");
    finish_ticket(line, signed_part);
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "ticket field count must be exact");

    sprintf(signed_part, "MUD1O|P|1000|60112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    line[strlen(line) - 1] = 0;
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "MUD1O ticket requires exactly one terminal LF");
    strcat(line, "\n\n");
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "MUD1O ticket must reject extra terminal LF");

    memset(line, 'a', ONBOARDING_ADMISSION_MAX_LINE);
    line[ONBOARDING_ADMISSION_MAX_LINE] = '\n';
    line[ONBOARDING_ADMISSION_MAX_LINE + 1] = 0;
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0,
                     "MUD1O parser must reject a line beyond its bounded payload");
    failed += expect(onboarding_admission_validate_ticket(
                     "MUD1O|P|1000|70112233445566778899aabbccddeeff|"
                     "123e4567-e89b-12d3-a456-426614174000|"
                     "123e4567-e89b-12d3-a456-426614174001|"
                     "0000000000000000000000000000000000000000000000000000000000000000\n",
                     TEST_SECRET, 1000, 0, &replay, &ticket) < 0,
                     "a replay-consumption callback is mandatory");
    sprintf(signed_part, "MUD1O|P|1000|80112233445566778899aabbccddeeff|"
            "123e4567-e89b-12d3-a456-426614174000|"
            "123e4567-e89b-12d3-a456-426614174001");
    finish_ticket(line, signed_part);
    replay.reject = 1;
    failed += expect(onboarding_admission_validate_ticket(line, TEST_SECRET, 1000,
                     consume_nonce, &replay, &ticket) < 0 && replay.calls == 2,
                     "replay callback rejection must fail closed");
    return failed;
}

static int test_inprocess_replay(void)
{
    static const char provision_ticket[] =
        "MUD1O|P|1788269000|00112233445566778899aabbccddeeff|"
        "11111111-1111-4111-8111-111111111111|"
        "22222222-2222-4222-8222-222222222222|"
        "b9c4f1e6bf4d17608c7bdfca6cd5d87a79b42b13c00cff731bb99d1d7200d9fe\n";
    onboarding_admission_ticket ticket;
    int failed = 0;

    onboarding_session_reset_for_test();
    failed += expect(onboarding_session_validate_ticket(provision_ticket, TEST_SECRET,
                     FIXTURE_NOW, &ticket) == 0,
                     "runtime MUD1O validation must consume a valid nonce in-process");
    failed += expect(onboarding_session_validate_ticket(provision_ticket, TEST_SECRET,
                     FIXTURE_NOW, &ticket) < 0,
                     "a runtime MUD1O ticket must be rejected after one use");
    onboarding_session_reset_for_test();
    return failed;
}

static int test_controls(void)
{
    static const char c_lines[][128] = {
        "MUD1O OK\n",
        "MUD1O RESERVE|416c696365\n",
        "MUD1O CHALLENGE|416c696365|0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
        "MUD1O VERIFIED|416c696365|0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
        "MUD1O SAVED|11111111-1111-4111-8111-111111111111|0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef|player-v1\n",
        "MUD1O ERR\n"
    };
    static const char gateway_lines[][64] = {
        "MUD1O RESERVED|11111111-1111-4111-8111-111111111111\n",
        "MUD1O ALLOW\n",
        "MUD1O CLAIMED|22222222-2222-4222-8222-222222222222\n",
        "MUD1O COMMIT\n",
        "MUD1O ABORT\n"
    };
    struct guarded_control {
        onboarding_control value;
        unsigned char fence[128];
    } guarded;
    onboarding_control control, parsed;
    char output[ONBOARDING_ADMISSION_MAX_LINE + 1];
    char oversized[ONBOARDING_ADMISSION_MAX_LINE + 1];
    int i, fence_ok, failed = 0;

    for(i=0; i<6; i++) {
        failed += expect(onboarding_parse_c_control(c_lines[i], &control) == 0 &&
                         onboarding_format_c_control(output, sizeof(output), &control) == 0 &&
                         strcmp(output, c_lines[i]) == 0,
                         "every fixture C-to-Gateway control must parse and format exactly");
    }
    for(i=0; i<5; i++) {
        failed += expect(onboarding_parse_gateway_control(gateway_lines[i], &control) == 0 &&
                         onboarding_format_gateway_control(output, sizeof(output), &control) == 0 &&
                         strcmp(output, gateway_lines[i]) == 0,
                         "every fixture Gateway-to-C control must parse and format exactly");
    }
    failed += expect(onboarding_parse_c_control("MUD1O OK\r\n", &control) < 0 &&
                     onboarding_parse_c_control("MUD1O RESERVE|416c696365|extra\n", &control) < 0 &&
                     onboarding_parse_c_control("MUD1O VERIFIED|416c696365|ABC\n", &control) < 0 &&
                     onboarding_parse_c_control("MUD1O CHALLENGE|416c696365|claim-secret\n", &control) < 0 &&
                     onboarding_parse_c_control("MUD1O SAVED|11111111-1111-4111-8111-111111111111|"
                                               "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef|bad value\n", &control) < 0 &&
                     onboarding_parse_gateway_control("MUD1O COMMIT", &control) < 0 &&
                     onboarding_parse_gateway_control("MUD1O ALLOW|extra\n", &control) < 0 &&
                     onboarding_parse_gateway_control("MUD1O CLAIMED|11111111-1111-4111-8111-111111111111|x\n", &control) < 0,
                     "control parser must reject CRLF, unknown fields, and malformed values");

    memset(&control, 0, sizeof(control));
    control.kind = ONBOARDING_CONTROL_RESERVED;
    strcpy(control.character_id, "11111111-1111-4111-8111-111111111111");
    failed += expect(onboarding_format_c_control(output, sizeof(output), &control) < 0 &&
                     onboarding_format_gateway_control(output, sizeof(output), &control) == 0 &&
                     onboarding_parse_gateway_control(output, &parsed) == 0 &&
                     parsed.kind == ONBOARDING_CONTROL_RESERVED,
                     "formatters must enforce control direction and canonical values");

    strcpy(oversized, "MUD1O RESERVE|");
    memset(oversized + strlen(oversized), 'a', 220);
    oversized[strlen("MUD1O RESERVE|") + 220] = '\n';
    oversized[strlen("MUD1O RESERVE|") + 221] = 0;
    memset(&guarded, 0, sizeof(guarded));
    memset(guarded.fence, 0xa5, sizeof(guarded.fence));
    failed += expect(onboarding_parse_c_control(oversized, &guarded.value) < 0,
                     "oversized control fields must be rejected");
    fence_ok = 1;
    for(i=0; i<(int)sizeof(guarded.fence); i++)
        if(guarded.fence[i] != 0xa5) fence_ok = 0;
    failed += expect(fence_ok,
                     "rejected control fields must not write beyond the control value");
    return failed;
}

static int test_state_guard(void)
{
    onboarding_admission_ticket ticket;
    onboarding_control control;
    onboarding_state state;
    int failed = 0;

    memset(&ticket, 0, sizeof(ticket));
    ticket.mode = ONBOARDING_ADMISSION_MODE_PROVISION;
    state = ONBOARDING_STATE_NEW;
    failed += expect(onboarding_state_accept_ticket(&state, &ticket) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_READY,
                     "provision ticket must enter provision-ready state");
    onboarding_parse_c_control("MUD1O OK\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_READY,
                     "OK acknowledgement is only an acknowledgement");
    onboarding_parse_c_control("MUD1O RESERVE|416c696365\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_AWAIT_RESERVED,
                     "provision must reserve before it can save");
    onboarding_parse_gateway_control("MUD1O RESERVED|11111111-1111-4111-8111-111111111111\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_RESERVED,
                     "RESERVED is accepted only after RESERVE");
    onboarding_parse_c_control("MUD1O SAVED|11111111-1111-4111-8111-111111111111|"
                                "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef|player-v1\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_AWAIT_COMMIT,
                     "SAVED is accepted only after RESERVED");
    onboarding_parse_gateway_control("MUD1O COMMIT\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_READY,
                     "COMMIT makes provision ready");

    state = ONBOARDING_STATE_NEW;
    ticket.mode = ONBOARDING_ADMISSION_MODE_CLAIM;
    failed += expect(onboarding_state_accept_ticket(&state, &ticket) == 0,
                     "claim ticket must be accepted from new state");
    onboarding_parse_c_control("MUD1O CHALLENGE|416c696365|"
                                "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_CLAIM_AWAIT_ALLOW,
                     "claim must challenge before any password can be accepted");
    failed += expect(onboarding_state_apply_c_control(&state, &control) < 0 &&
                     state == ONBOARDING_STATE_CLAIM_AWAIT_ALLOW,
                     "duplicate challenge must fail closed without changing state");
    onboarding_parse_c_control("MUD1O VERIFIED|416c696365|"
                                "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) < 0 &&
                     state == ONBOARDING_STATE_CLAIM_AWAIT_ALLOW,
                     "VERIFIED before ALLOW must be rejected");
    onboarding_parse_gateway_control("MUD1O ALLOW\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "only ALLOW enables the single password comparison");
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) < 0 &&
                     state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "duplicate ALLOW must be rejected");
    onboarding_parse_c_control("MUD1O VERIFIED|416c696365|"
                                "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED,
                     "claim must verify only after ALLOW and password comparison");
    onboarding_parse_gateway_control("MUD1O CLAIMED|22222222-2222-4222-8222-222222222222\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_READY,
                     "CLAIMED makes claim ready");

    state = ONBOARDING_STATE_NEW;
    onboarding_parse_gateway_control("MUD1O COMMIT\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) < 0 &&
                     state == ONBOARDING_STATE_NEW,
                     "out-of-order control must leave state unchanged");
    state = ONBOARDING_STATE_CLAIM_READY;
    onboarding_parse_c_control("MUD1O RESERVE|416c696365\n", &control);
    failed += expect(onboarding_state_apply_c_control(&state, &control) < 0 &&
                     state == ONBOARDING_STATE_CLAIM_READY,
                     "provision control may not enter claim flow");
    onboarding_parse_gateway_control("MUD1O ABORT\n", &control);
    failed += expect(onboarding_state_apply_gateway_control(&state, &control) == 0 &&
                     state == ONBOARDING_STATE_FAILED,
                     "ABORT transitions any live onboarding flow to failed");
    return failed;
}

static int test_claim_window_and_file_mutation(void)
{
    char root[] = "/tmp/muhan-claim-digest.XXXXXX";
    char player_dir[1024], path[1024], symlink_target[1024];
    char shard_dir[1024], outside_player[1024], outside_shard[1024];
    char *slash, *shard_name;
    char before[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
    char after[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
    int fd, failed = 0;

    failed += expect(onboarding_session_claim_allow_live(1000, 1000) &&
                     onboarding_session_claim_allow_live(1000, 1090) &&
                     !onboarding_session_claim_allow_live(1000, 1091) &&
                     !onboarding_session_claim_allow_live(0, 1000),
                     "claim ALLOW must use one fixed 90-second fail-closed window");
    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) != 0) return failed + 1;
    snprintf(player_dir, sizeof(player_dir), "%s/player", root);
    if(mkdir(player_dir, 0700) != 0 || player_path_ensure_dir("Alice") != 0 ||
       player_path_from_name("Alice", path, sizeof(path)) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write(fd, "before", 6) != 6 || close(fd) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", before) == 0,
                     "challenge digest must read the canonical player file");
    fd = open(path, O_WRONLY | O_TRUNC, 0600);
    if(fd < 0 || write(fd, "after", 5) != 5 || close(fd) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", after) == 0 &&
                     strcmp(before, after) != 0,
                     "post-password digest must detect a player-file mutation");
    unlink(path);
    snprintf(symlink_target, sizeof(symlink_target), "%s/symlink-target", root);
    fd = open(symlink_target, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write(fd, "outside", 7) != 7 || close(fd) != 0 ||
       symlink(symlink_target, path) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", after) < 0,
                     "claim digest must reject a symlinked player file");
    unlink(path);
    unlink(symlink_target);
    snprintf(shard_dir, sizeof(shard_dir), "%s", path);
    slash = strrchr(shard_dir, '/');
    if(!slash) { unsetenv("MUHAN_HOME"); return failed + 1; }
    *slash = 0;
    shard_name = strrchr(shard_dir, '/');
    if(!shard_name) { unsetenv("MUHAN_HOME"); return failed + 1; }
    ++shard_name;
    if(rmdir(shard_dir) != 0 || rmdir(player_dir) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    snprintf(outside_player, sizeof(outside_player), "%s/outside-player", root);
    snprintf(outside_shard, sizeof(outside_shard), "%s/%s", outside_player, shard_name);
    if(mkdir(outside_player, 0700) != 0 || mkdir(outside_shard, 0700) != 0 ||
       snprintf(symlink_target, sizeof(symlink_target), "%s/Alice", outside_shard) >= (int)sizeof(symlink_target) ||
       (fd = open(symlink_target, O_WRONLY | O_CREAT | O_TRUNC, 0600)) < 0 ||
       write(fd, "outside", 7) != 7 || close(fd) != 0 ||
       symlink(outside_player, player_dir) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", after) < 0,
                     "claim digest must reject a MUHAN_HOME/player symlink");
    unlink(player_dir);
    if(mkdir(player_dir, 0700) != 0 ||
       symlink(outside_shard, shard_dir) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", after) < 0,
                     "claim digest must reject an intermediate shard symlink");
    unlink(shard_dir);
    if(mkdir(shard_dir, 0700) != 0 ||
       (fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600)) < 0 ||
       ftruncate(fd, (off_t)PLAYER_PATH_READ_MAX_BYTES + 1) != 0 || close(fd) != 0) {
        unsetenv("MUHAN_HOME");
        return failed + 1;
    }
    failed += expect(onboarding_session_file_sha256("Alice", after) < 0,
                     "claim digest must reject a regular player file above 64MiB");
    unlink(path);
    unlink(symlink_target);
    rmdir(outside_shard);
    rmdir(outside_player);
    rmdir(shard_dir);
    rmdir(player_dir);
    rmdir(root);
    unsetenv("MUHAN_HOME");
    memset(before, 0, sizeof(before));
    memset(after, 0, sizeof(after));
    return failed;
}

static int test_claim_credential_zeroization(void)
{
    unsigned char password[sizeof(((creature *)0)->password)];
    unsigned char input[96];
    unsigned int i;
    int failed = 0;

    /* Use shape-only bytes: the unit must never print or retain a credential
     * fixture while proving that every byte, including trailing capacity, is
     * erased before the owner is released. */
    memset(password, 'P', sizeof(password));
    memset(input, 'I', sizeof(input));
    onboarding_session_zeroize_claim_memory(password, sizeof(password),
                                             input, sizeof(input));
    for(i = 0; i < sizeof(password); i++)
        failed += expect(password[i] == 0,
                         "MUD1O claim password storage must be wiped byte-for-byte");
    for(i = 0; i < sizeof(input); i++)
        failed += expect(input[i] == 0,
                         "MUD1O claim input transient must be wiped byte-for-byte");
    return failed;
}

int main(void)
{
    int failed = 0;
    failed += test_fixture_tickets();
    failed += test_ticket_rejections();
    failed += test_inprocess_replay();
    failed += test_controls();
    failed += test_state_guard();
    failed += test_claim_window_and_file_mutation();
    failed += test_claim_credential_zeroization();
    if(failed) return 1;
    puts("onboarding_admission_test: ok");
    return 0;
}
