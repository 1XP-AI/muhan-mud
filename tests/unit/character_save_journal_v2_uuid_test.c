#include "character_save_journal_v2_uuid.h"

#include <stdio.h>
#include <string.h>

typedef struct entropy_case {
    unsigned char bytes[32];
    size_t length, offset, chunk;
    int calls, fail, zero;
} entropy_case;

static int expect(int condition, const char *message)
{
    if (condition) return 0;
    fprintf(stderr, "character_save_journal_v2_uuid_test: %s\n", message);
    return 1;
}

static int fill(void *opaque, unsigned char *buffer, size_t capacity,
                size_t *filled)
{
    entropy_case *state = (entropy_case *)opaque;
    size_t amount;
    state->calls++;
    if (state->fail) return -1;
    if (state->zero) { *filled = 0; return 0; }
    amount = state->chunk ? state->chunk : capacity;
    if (amount > capacity) amount = capacity;
    if (amount > state->length - state->offset) amount = state->length - state->offset;
    memcpy(buffer, state->bytes + state->offset, amount);
    state->offset += amount;
    *filled = amount;
    return 0;
}

static int oversized(void *opaque, unsigned char *buffer, size_t capacity,
                     size_t *filled)
{
    entropy_case *state = (entropy_case *)opaque;
    (void)buffer;
    (void)capacity;
    state->calls++;
    *filled = CHARACTER_SAVE_JOURNAL_V2_UUID_BINARY_LENGTH + 1;
    return 0;
}

static int one_byte_forever(void *opaque, unsigned char *buffer, size_t capacity,
                            size_t *filled)
{
    entropy_case *state = (entropy_case *)opaque;
    (void)capacity;
    state->calls++;
    buffer[0] = 0x5a;
    *filled = 1;
    return 0;
}

static int unchanged(const char *output)
{
    size_t i;
    for (i = 0; i < CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1; i++)
        if (output[i] != 0) return 0;
    return 1;
}

static int golden(void)
{
    entropy_case state;
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1];
    static const unsigned char input[] = {
        0x00,0x11,0x22,0x33,0x44,0x55,0x66,0x77,
        0x88,0x99,0xaa,0xbb,0xcc,0xdd,0xee,0xff
    };
    int failed = 0;
    memset(&state, 0, sizeof(state));
    memcpy(state.bytes, input, sizeof(input)); state.length = sizeof(input);
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_OK,
                     "fixed entropy succeeds");
    failed += expect(!strcmp(output, "00112233-4455-4677-8899-aabbccddeeff"),
                     "version, variant, and lowercase canonical formatting");
    failed += expect(strlen(output) == 36 && output[36] == 0,
                     "output is exactly 36 characters plus NUL");
    failed += expect(state.calls == 1, "one complete entropy source call is accounted");
    return failed;
}

static int failures(void)
{
    entropy_case state;
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1];
    int failed = 0;
    memset(&state, 0, sizeof(state)); state.length = 16; state.chunk = 1;
    memset(state.bytes, 0x5a, sizeof(state.bytes)); memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(0, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_INVALID_ARGUMENT && unchanged(output),
                     "null callback clears output");
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, 0) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_INVALID_ARGUMENT,
                     "null output rejects");
    state.zero = 1; memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_ZERO_PROGRESS && unchanged(output),
                     "zero progress clears output");
    memset(&state, 0, sizeof(state)); state.length = 16; state.chunk = 1; state.fail = 1;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_ENTROPY_FAILURE && unchanged(output),
                     "callback failure clears output");
    memset(&state, 0, sizeof(state)); state.length = 16; state.chunk = 1;
    /* The callback can only report what it was given; this helper deliberately
     * reports more to exercise the generic boundary. */
    state.zero = 0;
    failed += expect(character_save_journal_v2_uuid_generate(
                     (character_save_journal_v2_uuid_entropy_fill)0, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_INVALID_ARGUMENT,
                     "invalid callback is rejected");
    memset(&state, 0, sizeof(state));
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(one_byte_forever, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_ATTEMPTS_EXHAUSTED && unchanged(output),
                     "bounded attempts reject short source");
    memset(&state, 0, sizeof(state)); memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_uuid_generate(oversized, &state, output) ==
                     CHARACTER_SAVE_JOURNAL_V2_UUID_OVERSIZED_PROGRESS && unchanged(output),
                     "oversized callback progress clears output");
    return failed;
}

static int partial_and_unique_calls(void)
{
    entropy_case state;
    char one[37], two[37];
    int failed = 0;
    memset(&state, 0, sizeof(state)); state.length = 32; state.chunk = 3;
    memset(state.bytes, 0x01, 16); memset(state.bytes + 16, 0x02, 16);
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, one) == 0,
                     "partial callback accumulation succeeds");
    state.offset = 16;
    failed += expect(character_save_journal_v2_uuid_generate(fill, &state, two) == 0,
                     "second injected source succeeds");
    failed += expect(strcmp(one, two) != 0 && state.calls > 10,
                     "distinct source bytes and calls are observable");
    return failed;
}

int main(void)
{
    return golden() + failures() + partial_and_unique_calls();
}
