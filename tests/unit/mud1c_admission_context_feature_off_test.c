#include "mud1c_admission_context.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static int zeroed(value, length)
const void *value;
size_t length;
{
    const unsigned char *bytes = value;
    size_t index;
    for(index=0U; index<length; ++index) if(bytes[index]) return 0;
    return 1;
}

int main(void)
{
    mud1c_admission_context context;
    mud1c_admission_context empty;
    unsigned char wire[8] = "MUD1C";
    char mac[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    size_t written = 99U;
    memset(&context, 0xa5, sizeof(context));
    memset(&empty, 0, sizeof(empty));
    memset(mac, 0xa5, sizeof(mac));
    assert(!mud1c_admission_context_enabled());
    assert(mud1c_admission_context_hmac_sha256_hex("0123456789abcdef0123456789abcdef", wire, 5U, mac) == MUD1C_ADMISSION_CONTEXT_DISABLED && !mac[0]);
    assert(mud1c_admission_context_parse(wire, 5U, &context, mac) == MUD1C_ADMISSION_CONTEXT_DISABLED && !mac[0]);
    assert(!memcmp(&context, &empty, sizeof(context)));
    memset(wire, 0xa5, sizeof(wire));
    assert(mud1c_admission_context_format(&context, "0123456789abcdef0123456789abcdef", wire, sizeof(wire), &written) == MUD1C_ADMISSION_CONTEXT_DISABLED && !written && !wire[0]);
    assert(zeroed(wire, sizeof(wire)));
    memset(&context, 0xa5, sizeof(context));
    assert(mud1c_admission_context_validate(wire, 0U, "0123456789abcdef0123456789abcdef", 0L, &context) == MUD1C_ADMISSION_CONTEXT_DISABLED);
    assert(!memcmp(&context, &empty, sizeof(context)));
    puts("mud1c_admission_context_feature_off_test: ok");
    return 0;
}
