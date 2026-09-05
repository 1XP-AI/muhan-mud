/* Durable, non-secret MUD1O ACTIVATED -> ACTIVE binding contract. */
#include "onboarding_activation_binding.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"
#define OTHER_COMMAND "55555555-5555-4555-8555-555555555555"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_binding_test: %s\n", message);
    return 1;
}

static int read_text(path, out, out_size)
const char *path;
char *out;
unsigned long out_size;
{
    int fd, count;
    if(!path || !out || out_size < 2) return -1;
    fd = open(path, O_RDONLY);
    if(fd < 0) return -1;
    count = read(fd, out, out_size - 1);
    if(count < 0 || close(fd) != 0) return -1;
    out[count] = 0;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-activation-binding.XXXXXX";
    char path[1024], text[512];
    struct stat status;
    onboarding_activation_binding binding;
    int failed;
    static const char expected[] =
        "actor_user_id=11111111-1111-4111-8111-111111111111\n"
        "correlation_id=22222222-2222-4222-8222-222222222222\n"
        "character_id=33333333-3333-4333-8333-333333333333\n"
        "mode=provision\n"
        "command_id=44444444-4444-4444-8444-444444444444\n";

    failed = 0;
    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) != 0) return 1;
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) == 0,
        "the provision completion tuple must be durable before ACTIVE");
    failed += expect(onboarding_activation_binding_path(COMMAND, path,
        sizeof(path)) == 0 && stat(path, &status) == 0 &&
        S_ISREG(status.st_mode) && (status.st_mode & 0777) == 0600,
        "the command-keyed binding must be private and durable");
    failed += expect(read_text(path, text, sizeof(text)) == 0 &&
        strcmp(text, expected) == 0,
        "the binding must contain exactly the non-secret activation tuple");
    failed += expect(onboarding_activation_binding_read(COMMAND, &binding) == 0 &&
        !strcmp(binding.actor_user_id, ACTOR) &&
        !strcmp(binding.correlation_id, CORRELATION) &&
        !strcmp(binding.character_id, CHARACTER) &&
        binding.mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        !strcmp(binding.command_id, COMMAND),
        "the persisted binding must round-trip exactly");
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) == 0 &&
        read_text(path, text, sizeof(text)) == 0 && !strcmp(text, expected),
        "an exact ACTIVATED replay must be idempotent");
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, COMMAND) < 0 &&
        read_text(path, text, sizeof(text)) == 0 && !strcmp(text, expected),
        "a tuple substitution for an existing command must fail closed");
    failed += expect(onboarding_activation_binding_write(ACTOR, CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, OTHER_COMMAND) == 0 &&
        onboarding_activation_binding_write(ACTOR, CORRELATION,
        "55555555-5555-4555-8555-555555555555",
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, OTHER_COMMAND) < 0,
        "a second tuple for a command must fail closed");
    failed += expect(onboarding_activation_binding_write("not-a-uuid", CORRELATION,
        CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) < 0 &&
        onboarding_activation_binding_path("not-a-uuid", path, sizeof(path)) < 0,
        "non-canonical identifiers must never create a binding path");
    failed += expect(strstr(expected, "ticket") == 0 &&
        strstr(expected, "password") == 0 && strstr(expected, "nonce") == 0,
        "the binding must contain no bearer or credential material");

    /* A command-keyed filename is not authority by itself: the in-file command
     * value must match it, or a substituted record is unusable and unwritable. */
    if(onboarding_activation_binding_path(COMMAND, path, sizeof(path)) == 0) {
        int fd;
        int length;
        length = snprintf(text, sizeof(text),
            "actor_user_id=%s\ncorrelation_id=%s\ncharacter_id=%s\n"
            "mode=provision\ncommand_id=%s\n",
            ACTOR, CORRELATION, CHARACTER, OTHER_COMMAND);
        fd = open(path, O_WRONLY | O_TRUNC);
        if(fd < 0 || length < 0 || write(fd, text, (unsigned long)length) != length ||
           close(fd) != 0) failed += expect(0, "test command substitution setup");
        else failed += expect(onboarding_activation_binding_read(COMMAND, &binding) < 0 &&
            onboarding_activation_binding_write(ACTOR, CORRELATION, CHARACTER,
                ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, COMMAND) < 0,
            "a command substitution in a command-keyed record must fail closed");
    }

    if(onboarding_activation_binding_path(COMMAND, path, sizeof(path)) == 0) unlink(path);
    if(onboarding_activation_binding_path(OTHER_COMMAND, path, sizeof(path)) == 0) unlink(path);
    snprintf(path, sizeof(path), "%s/onboarding-activation-bindings", root);
    rmdir(path);
    unsetenv("MUHAN_HOME");
    rmdir(root);
    return failed ? 1 : 0;
}
