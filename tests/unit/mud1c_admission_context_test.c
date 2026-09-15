#include "mud1c_admission_context.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

#define TEST_SECRET "0123456789abcdef0123456789abcdef"

static void context_fill(context)
mud1c_admission_context *context;
{
    unsigned int index;
    memset(context, 0, sizeof(*context));
    strcpy(context->world_id, "muhan");
    strcpy(context->actor_id, "123e4567-e89b-12d3-a456-426614174000");
    strcpy(context->character_id, "123e4567-e89b-12d3-a456-426614174001");
    strcpy(context->canonical_legacy_name_key, "416c696365");
    context->expires_at = 1000L;
    for(index=0U; index<sizeof(context->nonce); ++index) context->nonce[index] = (unsigned char)index;
}

static int zeroed(value, length)
const void *value;
size_t length;
{
    const unsigned char *bytes = value;
    size_t index;
    for(index=0U; index<length; ++index) if(bytes[index]) return 0;
    return 1;
}

static void test_vector(void)
{
    static const unsigned char data[] = "what do ya want for nothing?";
    char mac[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    assert(mud1c_admission_context_hmac_sha256_hex("Jefe", data, sizeof(data) - 1U, mac) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(!strcmp(mac, "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"));
    assert(mud1c_admission_context_hmac_sha256_hex(TEST_SECRET, (const unsigned char *)"MUD1C|vector", 12U, mac) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(!strcmp(mac, "ebf774ea200bbedfa06a2f3e425bcd41d877b09f44993d38e2d8374ef4567efc"));
}

static void test_format_parse_validate(void)
{
    mud1c_admission_context input, parsed, validated;
    unsigned char wire[MUD1C_ADMISSION_CONTEXT_MAX_WIRE + 1U];
    char supplied[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    size_t length;
    context_fill(&input);
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(!strcmp((char *)wire, "MUD1C|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|c5429c2f0a9ef7a155c1a89987611c54aa9470aa924e02762672f9421ca01a19"));
    assert(mud1c_admission_context_parse(wire, length, &parsed, supplied) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(!strcmp(supplied, "c5429c2f0a9ef7a155c1a89987611c54aa9470aa924e02762672f9421ca01a19"));
    assert(!strcmp(parsed.world_id, input.world_id) && !strcmp(parsed.actor_id, input.actor_id) && !memcmp(parsed.nonce, input.nonce, sizeof(input.nonce)));
    assert(mud1c_admission_context_validate(wire, length, TEST_SECRET, 1000L, &validated) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(!memcmp(&validated, &input, sizeof(input)));
}

static void test_rejections(void)
{
    mud1c_admission_context input, output;
    unsigned char wire[MUD1C_ADMISSION_CONTEXT_MAX_WIRE + 2U];
    unsigned char tiny[1];
    char mac[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    size_t length, capacity;
    context_fill(&input);
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    capacity = length;
    memset(wire, 0xa5, sizeof(wire));
    length = 99U;
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, capacity, &length) == MUD1C_ADMISSION_CONTEXT_INVALID && !length && zeroed(wire, capacity));
    tiny[0] = 0xa5U;
    length = 99U;
    assert(mud1c_admission_context_format(&input, TEST_SECRET, tiny, sizeof(tiny), &length) == MUD1C_ADMISSION_CONTEXT_INVALID && !length && !tiny[0]);
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    wire[length - 1U] = wire[length - 1U] == '0' ? '1' : '0';
    memset(&output, 0xa5, sizeof(output));
    assert(mud1c_admission_context_validate(wire, length, TEST_SECRET, 1000L, &output) == MUD1C_ADMISSION_CONTEXT_INVALID && zeroed(&output, sizeof(output)));
    context_fill(&input);
    input.expires_at = 999L;
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(mud1c_admission_context_validate(wire, length, TEST_SECRET, 1000L, &output) == MUD1C_ADMISSION_CONTEXT_INVALID);
    context_fill(&input);
    input.expires_at = 1031L;
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    assert(mud1c_admission_context_validate(wire, length, TEST_SECRET, 1000L, &output) == MUD1C_ADMISSION_CONTEXT_INVALID);
    context_fill(&input);
    strcpy(input.world_id, "Muhan");
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_INVALID && !length && !wire[0]);
    context_fill(&input);
    strcpy(input.canonical_legacy_name_key, "416C696365");
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_INVALID);
    context_fill(&input);
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    wire[7] = 0;
    assert(mud1c_admission_context_parse(wire, length, &output, mac) == MUD1C_ADMISSION_CONTEXT_INVALID);
    context_fill(&input);
    assert(mud1c_admission_context_format(&input, TEST_SECRET, wire, sizeof(wire), &length) == MUD1C_ADMISSION_CONTEXT_OK);
    wire[length++] = '|';
    assert(mud1c_admission_context_parse(wire, length, &output, mac) == MUD1C_ADMISSION_CONTEXT_INVALID);
    assert(mud1c_admission_context_parse((const unsigned char *)"MUD1C|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|0000000000000000000000000000000000000000000000000000000000000000|x", strlen("MUD1C|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|0000000000000000000000000000000000000000000000000000000000000000|x"), &output, mac) == MUD1C_ADMISSION_CONTEXT_INVALID);
    assert(mud1c_admission_context_parse((const unsigned char *)"MUD1C|muhan|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|0000000000000000000000000000000000000000000000000000000000000000", strlen("MUD1C|muhan|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|0000000000000000000000000000000000000000000000000000000000000000"), &output, mac) == MUD1C_ADMISSION_CONTEXT_INVALID);
}

int main(void)
{
    assert(mud1c_admission_context_enabled());
    test_vector();
    test_format_parse_validate();
    test_rejections();
    puts("mud1c_admission_context_test: ok");
    return 0;
}
