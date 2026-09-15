/* Test-only, process-local bridge for the detached MUD1C codec. */
#include "mud1c_admission_context.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int hex(value)
unsigned char value;
{
    if(value >= '0' && value <= '9') return value - '0';
    if(value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static int nonce_from_hex(value, nonce)
const char *value;
unsigned char nonce[MUD1C_ADMISSION_CONTEXT_NONCE_BYTES];
{
    size_t i;
    int high, low;
    if(!value || strlen(value) != MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN) return 0;
    for(i = 0U; i < MUD1C_ADMISSION_CONTEXT_NONCE_BYTES; ++i) {
        high = hex((unsigned char)value[i * 2U]);
        low = hex((unsigned char)value[i * 2U + 1U]);
        if(high < 0 || low < 0) return 0;
        nonce[i] = (unsigned char)((high << 4) | low);
    }
    return 1;
}

static int format(argc, argv)
int argc;
char **argv;
{
    mud1c_admission_context context;
    unsigned char wire[MUD1C_ADMISSION_CONTEXT_MAX_WIRE + 1U];
    size_t length;
    char *end;
    if(argc != 9) return 2;
    memset(&context, 0, sizeof(context));
    if(strlen(argv[3]) > MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX ||
       strlen(argv[4]) != MUD1C_ADMISSION_CONTEXT_UUID_LEN ||
       strlen(argv[5]) != MUD1C_ADMISSION_CONTEXT_UUID_LEN ||
       strlen(argv[6]) > MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX ||
       !nonce_from_hex(argv[8], context.nonce)) return 2;
    context.expires_at = strtol(argv[7], &end, 10);
    if(!argv[7][0] || *end) return 2;
    strcpy(context.world_id, argv[3]);
    strcpy(context.actor_id, argv[4]);
    strcpy(context.character_id, argv[5]);
    strcpy(context.canonical_legacy_name_key, argv[6]);
    if(mud1c_admission_context_format(&context, argv[2], wire, sizeof(wire),
                                      &length) != MUD1C_ADMISSION_CONTEXT_OK)
        return 2;
    fwrite(wire, 1U, length, stdout);
    return 0;
}

static int parse(wire)
const char *wire;
{
    mud1c_admission_context context;
    char mac[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    puts(mud1c_admission_context_parse((const unsigned char *)wire,
        strlen(wire), &context, mac) == MUD1C_ADMISSION_CONTEXT_OK ?
        "accepted" : "rejected");
    return 0;
}

static int validate(secret, now, wire)
const char *secret;
const char *now;
const char *wire;
{
    mud1c_admission_context context;
    char *end;
    long parsed_now = strtol(now, &end, 10);
    if(!now[0] || *end) return 2;
    puts(mud1c_admission_context_validate((const unsigned char *)wire,
        strlen(wire), secret, parsed_now, &context) ==
        MUD1C_ADMISSION_CONTEXT_OK ? "accepted" : "rejected");
    return 0;
}

int main(argc, argv)
int argc;
char **argv;
{
    if(argc == 9 && !strcmp(argv[1], "format")) return format(argc, argv);
    if(argc == 3 && !strcmp(argv[1], "parse")) return parse(argv[2]);
    if(argc == 5 && !strcmp(argv[1], "validate"))
        return validate(argv[2], argv[3], argv[4]);
    return 2;
}
