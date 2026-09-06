/* Test-only bridge: exercise existing C admission and evidence seams from one
 * fixture-driven Gateway conformance test.  It owns no gameplay state. */
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "legacy_identity_evidence_wire.h"
#include "mstruct.h"
#include "onboarding_evidence_emission.h"
#include "player_path.h"
#include "trusted_admission.h"

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

static int player_file(path)
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

static int emit_evidence(name, known_sha256)
const char *name;
const char *known_sha256;
{
    char root[] = "/tmp/muhan-admission-identity.XXXXXX";
    char player_dir[512], player_file_path[512], shard_dir[512];
    char line[241];
    int status;

    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) != 0 ||
       setenv("MUD_ENABLE_ONBOARDING_EVIDENCE", "1", 1) != 0) {
        fputs("evidence fixture setup failed\n", stderr);
        return 2;
    }
    snprintf(player_dir, sizeof(player_dir), "%s/player", root);
    if(mkdir(player_dir, 0700) != 0 ||
       player_path_from_name(name, player_file_path, sizeof(player_file_path)) != 0) {
        fputs("evidence fixture path setup failed\n", stderr);
        return 2;
    }
    strcpy(shard_dir, player_file_path);
    *strrchr(shard_dir, '/') = 0;
    if(mkdir(shard_dir, 0700) != 0 || player_file(player_file_path) != 0) {
        fputs("evidence fixture file setup failed\n", stderr);
        return 2;
    }

    decoder_result = 0;
    status = onboarding_evidence_emission_prepare(name, known_sha256, line,
                                                   sizeof(line));
    if(status == ONBOARDING_EVIDENCE_EMISSION_OK) fputs(line, stdout);
    unlink(player_file_path);
    rmdir(shard_dir);
    rmdir(player_dir);
    rmdir(root);
    unsetenv("MUHAN_HOME");
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    return status == ONBOARDING_EVIDENCE_EMISSION_OK ? 0 : 1;
}

static int ticket_line(secret, line, twice)
const char *secret;
const char *line;
int twice;
{
    trusted_admission_ticket ticket;
    int first, second;
    if(trusted_admission_set_secret_for_test(secret) != 0) return 2;
    first = trusted_admission_validate(line, 1700000000L, &ticket);
    if(!twice) {
        if(first == 0) printf("accepted|%s\n", ticket.name);
        else puts("rejected");
    }
    else {
        second = trusted_admission_validate(line, 1700000000L, &ticket);
        printf("%s|%s\n", first == 0 ? "accepted" : "rejected",
               second == 0 ? "accepted" : "rejected");
    }
    trusted_admission_reset_for_test();
    return 0;
}

static int ticket_name(secret, name_hex)
const char *secret;
const char *name_hex;
{
    char signed_part[190], mac[65], line[256];
    snprintf(signed_part, sizeof(signed_part),
             "MUD1|1700000015|00112233445566778899aabbccddeeff|"
             "123e4567-e89b-12d3-a456-426614174000|"
             "123e4567-e89b-12d3-a456-426614174002|%s", name_hex);
    if(trusted_admission_hmac_hex(secret, signed_part, mac) != 0) return 2;
    snprintf(line, sizeof(line), "%s|%s", signed_part, mac);
    return ticket_line(secret, line, 0);
}

static int hex_value(value)
unsigned char value;
{
    if(value >= '0' && value <= '9') return value - '0';
    if(value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static int evidence_wire(hex)
const char *hex;
{
    legacy_identity_evidence value;
    unsigned char wire[LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_LENGTH];
    size_t length, i;
    int high, low;

    length = strlen(hex);
    if(!length || (length & 1U) || length / 2U > sizeof(wire)) return 1;
    for(i = 0; i < length; i += 2U) {
        high = hex_value((unsigned char)hex[i]);
        low = hex_value((unsigned char)hex[i + 1U]);
        if(high < 0 || low < 0) return 1;
        wire[i / 2U] = (unsigned char)((high << 4) | low);
    }
    return legacy_identity_evidence_wire_decode(wire, length / 2U, &value) ==
        LEGACY_IDENTITY_EVIDENCE_WIRE_OK ? 0 : 1;
}

int main(argc, argv)
int argc;
char **argv;
{
    if(argc == 4 && !strcmp(argv[1], "ticket")) return ticket_line(argv[2], argv[3], 0);
    if(argc == 4 && !strcmp(argv[1], "ticket-twice")) return ticket_line(argv[2], argv[3], 1);
    if(argc == 4 && !strcmp(argv[1], "ticket-name")) return ticket_name(argv[2], argv[3]);
    if(argc == 4 && !strcmp(argv[1], "emit-evidence")) return emit_evidence(argv[2], argv[3]);
    if(argc == 3 && !strcmp(argv[1], "evidence")) return evidence_wire(argv[2]);
    return 2;
}
