#include "mstruct.h"
#include "onboarding_admission.h"
#include "onboarding_recovery.h"
#include "player_store.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define DIGEST "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

static int test_mode;
static int test_load_result;
static int test_mark_result;
static int marks;
static char player_path[1024];
static char internal_name[80];
static char marked_actor[37], marked_correlation[37], marked_character[37];
static char marked_name[80], marked_format[80], marked_digest[65];

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_recovery_test: %s\n", message);
    return 1;
}

static int write_all(fd, text)
int fd;
const char *text;
{
    unsigned long length;
    int written;

    length = (unsigned long)strlen(text);
    while(length) {
        written = write(fd, text, length);
        if(written <= 0) return -1;
        text += written;
        length -= (unsigned long)written;
    }
    return 0;
}

static int make_receipt(root, filename_correlation, body_correlation, state,
                        name_hex, digest)
const char *root;
const char *filename_correlation;
const char *body_correlation;
const char *state;
const char *name_hex;
const char *digest;
{
    char path[1024], text[1024];
    int fd;

    if(snprintf(path, sizeof(path), "%s/onboarding-receipts/%s.receipt", root,
                filename_correlation) >= (int)sizeof(path) ||
       snprintf(text, sizeof(text),
                "version=1\n"
                "state=%s\n"
                "actor_uuid=" ACTOR "\n"
                "correlation_uuid=%s\n"
                "character_uuid=" CHARACTER "\n"
                "canonical_name_hex=%s\n"
                "storage_format=player-v1\n"
                "saved_file_sha256=%s\n",
                state, body_correlation, name_hex, digest) >= (int)sizeof(text))
        return -1;
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    if(write_all(fd, text) < 0 || close(fd) < 0) return -1;
    return 0;
}

static int make_player_file(path)
const char *path;
{
    int fd;
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    if(write_all(fd, "player-v1") < 0 || close(fd) < 0) return -1;
    return 0;
}

static void reset_stubs(void)
{
    test_mode = 1;
    test_load_result = PLAYER_STORE_OK;
    test_mark_result = 0;
    marks = 0;
    player_path[0] = 0;
    strcpy(internal_name, "Alice");
    memset(marked_actor, 0, sizeof(marked_actor));
    memset(marked_correlation, 0, sizeof(marked_correlation));
    memset(marked_character, 0, sizeof(marked_character));
    memset(marked_name, 0, sizeof(marked_name));
    memset(marked_format, 0, sizeof(marked_format));
    memset(marked_digest, 0, sizeof(marked_digest));
}

int onboarding_session_mode(void)
{
    return test_mode;
}

int player_path_from_name(name, out, out_size)
const char *name;
char *out;
unsigned long out_size;
{
    (void)name;
    if(!player_path[0] || snprintf(out, out_size, "%s", player_path) >=
       (int)out_size) return -1;
    return 0;
}

int player_name_is_valid(name, min_cp, max_cp)
const unsigned char *name;
unsigned long min_cp;
unsigned long max_cp;
{
    (void)min_cp;
    (void)max_cp;
    return name && name[0];
}

void lowercize(str, flag)
char *str;
int flag;
{
    int i;
    for(i=0; str && str[i]; i++)
        if(str[i] >= 'A' && str[i] <= 'Z') str[i] += 'a' - 'A';
    if(str && str[0] && (flag & 1) && str[0] >= 'a' && str[0] <= 'z')
        str[0] -= 'a' - 'A';
}

int load_ply(name, player)
char *name;
creature **player;
{
    creature *value;
    (void)name;
    if(!player) return PLAYER_STORE_IO_ERROR;
    *player = 0;
    if(test_load_result != PLAYER_STORE_OK) return test_load_result;
    value = (creature *)calloc(1, sizeof(*value));
    if(!value) return PLAYER_STORE_IO_ERROR;
    strcpy(value->name, internal_name);
    *player = value;
    return PLAYER_STORE_OK;
}

void free_crt(player)
creature *player;
{
    free(player);
}

int onboarding_session_file_sha256(name, out)
const char *name;
char out[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
{
    (void)name;
    strcpy(out, DIGEST);
    return 0;
}

int onboarding_receipt_mark_saved(actor, correlation, character, name, format,
                                  digest)
const char *actor;
const char *correlation;
const char *character;
const char *name;
const char *format;
const char *digest;
{
    marks++;
    strcpy(marked_actor, actor);
    strcpy(marked_correlation, correlation);
    strcpy(marked_character, character);
    strcpy(marked_name, name);
    strcpy(marked_format, format);
    strcpy(marked_digest, digest);
    return test_mark_result;
}

int main(void)
{
    char root[] = "/tmp/muhan-onboarding-recovery.XXXXXX";
    char receipts[1024], bad[1024];
    int failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    if(setenv("MUHAN_HOME", root, 1) < 0 ||
       snprintf(receipts, sizeof(receipts), "%s/onboarding-receipts", root) >=
           (int)sizeof(receipts) || mkdir(receipts, 0700) < 0 ||
       snprintf(player_path, sizeof(player_path), "%s/player-file", root) >=
           (int)sizeof(player_path)) {
        perror("setup");
        return 1;
    }

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    failed += expect(make_receipt(root, CORRELATION, CORRELATION, "pending",
                                  "416c696365", "") == 0 &&
                     onboarding_recovery_startup() == 0 && marks == 0,
                     "a pending receipt without a player file must remain pending");
    failed += expect(make_player_file(player_path) == 0 &&
                     onboarding_recovery_startup() == 0 && marks == 1 &&
                     !strcmp(marked_actor, ACTOR) &&
                     !strcmp(marked_correlation, CORRELATION) &&
                     !strcmp(marked_character, CHARACTER) &&
                     !strcmp(marked_name, "Alice") &&
                     !strcmp(marked_format, "player-v1") &&
                     !strcmp(marked_digest, DIGEST),
                     "a published matching player must promote exactly its pending receipt");

    unlink(player_path);
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts, CORRELATION) <
       (int)sizeof(bad)) unlink(bad);

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    failed += expect(make_player_file(player_path) == 0 &&
                     make_receipt(root, "44444444-4444-4444-8444-444444444444",
                                  "44444444-4444-4444-8444-444444444444",
                                  "saved", "416c696365", DIGEST) == 0 &&
                     make_receipt(root, "55555555-5555-4555-8555-555555555555",
                                  "55555555-5555-4555-8555-555555555555",
                                  "committed", "416c696365", DIGEST) == 0 &&
                     onboarding_recovery_startup() == 0 && marks == 0,
                     "saved and committed receipts must remain immutable at startup");

    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "66666666-6666-4666-8666-666666666666") < (int)sizeof(bad)) {
        int fd = open(bad, O_WRONLY | O_CREAT | O_TRUNC, 0600);
        if(fd >= 0) {
            write_all(fd, "not a receipt\n");
            close(fd);
        }
    }
    failed += expect(onboarding_recovery_startup() < 0,
                     "a malformed receipt must fail startup generically");
    unlink(bad);

    failed += expect(make_receipt(root, "77777777-7777-4777-8777-777777777777",
                                  CORRELATION, "pending", "416c696365", "") == 0 &&
                     onboarding_recovery_startup() < 0,
                     "a filename and receipt correlation mismatch must fail closed");
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "77777777-7777-4777-8777-777777777777") < (int)sizeof(bad)) unlink(bad);

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    strcpy(internal_name, "Other");
    failed += expect(make_receipt(root, "88888888-8888-4888-8888-888888888888",
                                  "88888888-8888-4888-8888-888888888888",
                                  "pending", "416c696365", "") == 0 &&
                     onboarding_recovery_startup() < 0 && marks == 0,
                     "a loadable player with a mismatched internal name must fail closed");
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "88888888-8888-4888-8888-888888888888") < (int)sizeof(bad)) unlink(bad);

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    test_load_result = PLAYER_STORE_CORRUPT;
    failed += expect(make_receipt(root, "99999999-9999-4999-8999-999999999999",
                                  "99999999-9999-4999-8999-999999999999",
                                  "pending", "416c696365", "") == 0 &&
                     onboarding_recovery_startup() < 0 && marks == 0,
                     "a corrupt player file must fail startup instead of becoming saved");
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "99999999-9999-4999-8999-999999999999") < (int)sizeof(bad)) unlink(bad);

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    failed += expect(make_receipt(root, "abababab-abab-4bab-8bab-abababababab",
                                  "abababab-abab-4bab-8bab-abababababab",
                                  "pending", "414c494345", "") == 0 &&
                     onboarding_recovery_startup() < 0 && marks == 0,
                     "a non-canonical hex-decoded name must fail before loading");
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "abababab-abab-4bab-8bab-abababababab") < (int)sizeof(bad)) unlink(bad);

    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "bcbcbcbc-bcbc-4cbc-8cbc-bcbcbcbcbcbc") < (int)sizeof(bad)) {
        unlink(bad);
        symlink("missing-receipt", bad);
    }
    failed += expect(onboarding_recovery_startup() < 0,
                     "a symlinked receipt must fail no-follow startup scanning");
    unlink(bad);

    reset_stubs();
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    unlink(player_path);
    symlink("missing-player", player_path);
    failed += expect(make_receipt(root, "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd",
                                  "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd",
                                  "pending", "416c696365", "") == 0 &&
                     onboarding_recovery_startup() < 0 && marks == 0,
                     "a symlinked player path must fail before load_ply");
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd") < (int)sizeof(bad)) unlink(bad);
    unlink(player_path);

    reset_stubs();
    test_mode = 0;
    snprintf(player_path, sizeof(player_path), "%s/player-file", root);
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa") < (int)sizeof(bad)) {
        int fd = open(bad, O_WRONLY | O_CREAT | O_TRUNC, 0600);
        if(fd >= 0) {
            write_all(fd, "not a receipt\n");
            close(fd);
        }
    }
    failed += expect(onboarding_recovery_startup() == 0 && marks == 0,
                     "feature-off startup must not scan or write receipts");

    unlink(player_path);
    unlink(bad);
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "44444444-4444-4444-8444-444444444444") < (int)sizeof(bad)) unlink(bad);
    if(snprintf(bad, sizeof(bad), "%s/%s.receipt", receipts,
                "55555555-5555-4555-8555-555555555555") < (int)sizeof(bad)) unlink(bad);
    rmdir(receipts);
    rmdir(root);
    if(failed) return 1;
    puts("onboarding_recovery_test: ok");
    return 0;
}
