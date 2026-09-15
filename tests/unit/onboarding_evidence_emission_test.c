#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "legacy_identity_evidence.h"
#include "mstruct.h"
#include "onboarding_admission.h"
#include "onboarding_evidence_control.h"
#include "onboarding_evidence_emission.h"
#include "player_path.h"

static int decoder_result;

void lowercize(value, flag)
char *value;
int flag;
{
    unsigned long i;
    for(i = 0; value && value[i]; i++)
        if(value[i] >= 'A' && value[i] <= 'Z') value[i] += 'a' - 'A';
    if(value && (flag & 1) && value[0] >= 'a' && value[0] <= 'z')
        value[0] -= 'a' - 'A';
}

void zero(memory, size)
void *memory;
int size;
{ memset(memory, 0, (size_t)size); }

int write_crt(fd, player, perm_only)
int fd;
creature *player;
char perm_only;
{ (void)fd; (void)player; (void)perm_only; return -1; }

int utf8_validate(value, length)
const unsigned char *value;
unsigned long length;
{ (void)value; (void)length; return 1; }

unsigned long utf8_codepoint_len(value)
const unsigned char *value;
{ return value ? (unsigned long)strlen((const char *)value) : 0; }

int read_crt_player(fd, player)
int fd;
creature *player;
{ (void)fd; (void)player; return decoder_result; }

void free_crt(player)
creature *player;
{ free(player); }

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_evidence_emission_test: %s\n", message);
    return 1;
}

static int make_player_file(path)
const char *path;
{
    unsigned char bytes[8192];
    int fd;
    memset(bytes, 'x', sizeof(bytes));
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    if(write(fd, bytes, sizeof(bytes)) != (ssize_t)sizeof(bytes) || close(fd) < 0)
        return -1;
    return 0;
}

static int legacy_controls_unchanged(void)
{
    onboarding_control control;
    char line[ONBOARDING_ADMISSION_MAX_LINE + 1];
    int failed;

    failed = 0;
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    memset(&control, 0, sizeof(control));
    control.kind = ONBOARDING_CONTROL_VERIFIED;
    strcpy(control.name_hex, "416c696365");
    strcpy(control.file_sha256,
           "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49");
    failed += expect(onboarding_format_c_control(line, sizeof(line), &control) == 0 &&
                     !strcmp(line, "MUD1O VERIFIED|416c696365|"
                              "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49\n"),
                     "feature-off claim output remains byte-for-byte VERIFIED");
    memset(&control, 0, sizeof(control));
    control.kind = ONBOARDING_CONTROL_SAVED;
    strcpy(control.character_id, "11111111-1111-4111-8111-111111111111");
    strcpy(control.file_sha256,
           "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49");
    strcpy(control.storage_format, "player-v1");
    failed += expect(onboarding_format_c_control(line, sizeof(line), &control) == 0 &&
                     !strcmp(line, "MUD1O SAVED|11111111-1111-4111-8111-111111111111|"
                              "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49|player-v1\n"),
                     "feature-off provision output remains byte-for-byte SAVED");
    return failed;
}

int main(void)
{
    static const char digest[] =
        "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49";
    char root[] = "/tmp/muhan-onboarding-evidence-emission.XXXXXX";
    char player_dir[512], player_file[512], shard_dir[512];
    char line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1];
    unsigned char before[8192], after[8192];
    onboarding_evidence_control_record parsed;
    onboarding_state state;
    int fd, failed;

    failed = legacy_controls_unchanged();
    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) != 0) return failed + 1;
    snprintf(player_dir, sizeof(player_dir), "%s/player", root);
    if(mkdir(player_dir, 0700) != 0 ||
       player_path_from_name("Alice", player_file, sizeof(player_file)) != 0) return failed + 1;
    strcpy(shard_dir, player_file);
    *strrchr(shard_dir, '/') = 0;
    if(mkdir(shard_dir, 0700) != 0 || make_player_file(player_file) != 0) return failed + 1;
    fd = open(player_file, O_RDONLY);
    if(fd < 0 || read(fd, before, sizeof(before)) != (ssize_t)sizeof(before) || close(fd) != 0)
        return failed + 1;

    decoder_result = 0;
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    memset(line, 'x', sizeof(line));
    state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
    failed += expect(onboarding_evidence_emission_prepare("Alice", digest, line,
                     sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_DISABLED &&
                     !line[0] && state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "feature-off preparation emits no record and does not advance claim");
    setenv("MUD_ENABLE_ONBOARDING_EVIDENCE", "1", 1);
    memset(line, 0, sizeof(line));
    failed += expect(onboarding_evidence_emission_prepare("Alice", digest, line,
                     sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_OK &&
                     onboarding_evidence_control_parse(line, &parsed) ==
                     ONBOARDING_EVIDENCE_CONTROL_OK &&
                     parsed.evidence.result == LEGACY_IDENTITY_EVIDENCE_OK &&
                     !strcmp(parsed.evidence.canonical_name, "Alice") &&
                     !strcmp(parsed.evidence.player_file_sha256, digest) &&
                     !strcmp(parsed.evidence.storage_format, "player-v1"),
                     "provision durable file prepares exactly one checked EVIDENCE record");
    state = ONBOARDING_STATE_PROVISION_RESERVED;
    failed += expect(onboarding_state_apply_evidence(&state) == 0 &&
                     state == ONBOARDING_STATE_PROVISION_AWAIT_COMMIT,
                     "provision evidence waits for the unchanged COMMIT transition");
    fd = open(player_file, O_RDONLY);
    if(fd < 0 || read(fd, after, sizeof(after)) != (ssize_t)sizeof(after) || close(fd) != 0)
        return failed + 1;
    failed += expect(!memcmp(before, after, sizeof(before)),
                     "evidence preparation is read-only and leaves no game relay state");

    memset(line, 0, sizeof(line));
    state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
    failed += expect(onboarding_evidence_emission_prepare("Alice", digest, line,
                     sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_OK &&
                     onboarding_state_apply_evidence(&state) == 0 &&
                     state == ONBOARDING_STATE_CLAIM_AWAIT_CLAIMED,
                     "claim durable file evidence waits for the unchanged CLAIMED transition");

    memset(line, 'x', sizeof(line));
    state = ONBOARDING_STATE_PROVISION_RESERVED;
    failed += expect(onboarding_evidence_emission_prepare("Alice",
                     "0000000000000000000000000000000000000000000000000000000000000000",
                     line, sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_MISMATCH &&
                     !line[0] && state == ONBOARDING_STATE_PROVISION_RESERVED,
                     "mismatched durable SHA emits no fallback and does not complete provision");
    memset(line, 'x', sizeof(line));
    state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
    failed += expect(onboarding_evidence_emission_prepare("Alice",
                     "0000000000000000000000000000000000000000000000000000000000000000",
                     line, sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_MISMATCH &&
                     !line[0] && state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "mismatched durable SHA emits no fallback and does not complete claim");
    decoder_result = -1;
    memset(line, 'x', sizeof(line));
    state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
    failed += expect(onboarding_evidence_emission_prepare("Alice", digest, line,
                     sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_INSPECTION_FAILED &&
                     !line[0] && state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "failed inspection emits no fallback and does not complete claim");

    decoder_result = 0;
    if(unlink(player_file) != 0) return failed + 1;
    memset(line, 'x', sizeof(line));
    state = ONBOARDING_STATE_CLAIM_PASSWORD_READY;
    failed += expect(onboarding_evidence_emission_prepare("Alice", digest, line,
                     sizeof(line)) == ONBOARDING_EVIDENCE_EMISSION_INSPECTION_FAILED &&
                     !line[0] && state == ONBOARDING_STATE_CLAIM_PASSWORD_READY,
                     "absent durable file emits no fallback and does not complete claim");
    rmdir(shard_dir);
    rmdir(player_dir);
    rmdir(root);
    unsetenv("MUHAN_HOME");
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    if(failed) return 1;
    puts("onboarding_evidence_emission_test: ok");
    return 0;
}
