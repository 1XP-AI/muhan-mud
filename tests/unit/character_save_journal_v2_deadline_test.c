#include "character_save_journal_v2_deadline.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>

typedef struct clock_case {
    int64_t value;
    int result;
    int calls;
} clock_case;

static int fixed_clock(void *opaque, int64_t *seconds)
{
    clock_case *state = (clock_case *)opaque;

    state->calls++;
    if (state->result)
        return state->result;
    *seconds = state->value;
    return 0;
}

static int expect(int condition, const char *message)
{
    if (condition)
        return 0;
    fprintf(stderr, "character_save_journal_v2_deadline_test: %s\n", message);
    return 1;
}

static int is_zeroed(const char *output)
{
    int index;

    for (index = 0; index < CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE;
         index++)
        if (output[index] != 0)
            return 0;
    return 1;
}

static int render(int64_t now, int64_t extension, const char *expected)
{
    clock_case state;
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE];
    int failed = 0;

    memset(&state, 0, sizeof(state));
    state.value = now;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state, extension, output) == 0, expected);
    failed += expect(!strcmp(output, expected), expected);
    failed += expect(state.calls == 1, "one clock read per generation");
    failed += expect(output[20] == '\0' && output[21] == 0,
                     "canonical text is NUL terminated and does not leak");
    return failed;
}

static int test_calendar_boundaries(void)
{
    int failed = 0;

    failed += render(0, 1, "1970-01-01T00:00:01Z");
    failed += render(1582934399, 1, "2020-02-29T00:00:00Z");
    failed += render(1614556799, 1, "2021-03-01T00:00:00Z");
    failed += render(1577836799, 1, "2020-01-01T00:00:00Z");
    failed += render(2147483647, 1, "2038-01-19T03:14:08Z");
    failed += render(253402300499LL, 300, "9999-12-31T23:59:59Z");
    return failed;
}

static int test_failures_clear_output(void)
{
    clock_case state;
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE];
    int failed = 0;

    memset(&state, 0, sizeof(state));
    state.value = 0;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state, 0, output) != 0 && is_zeroed(output),
                     "zero extension fails and clears output");
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state,
        CHARACTER_SAVE_JOURNAL_V2_DEADLINE_EXTENSION_MAX_SECONDS + 1,
        output) != 0 && is_zeroed(output),
                     "oversized extension fails and clears output");

    state.value = -1;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state, 1, output) != 0 && is_zeroed(output),
                     "negative epoch fails and clears output");
    state.value = INT64_MAX;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state, 1, output) != 0 && is_zeroed(output),
                     "addition overflow fails and clears output");

    state.value = 0;
    state.result = -7;
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        fixed_clock, &state, 1, output) != 0 && is_zeroed(output),
                     "clock failure clears output");
    memset(output, 'X', sizeof(output));
    failed += expect(character_save_journal_v2_deadline_generate(
        0, &state, 1, output) != 0 && is_zeroed(output),
                     "missing clock clears output");
    return failed;
}

static int test_provider_callback(void)
{
    character_save_journal_v2_deadline_provider provider;
    clock_case state;
    char first[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE];
    char second[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE];
    int (*callback)(void *, char[64]);
    int failed = 0;

    memset(&state, 0, sizeof(state));
    state.value = 1704067199;
    character_save_journal_v2_deadline_provider_init(&provider, fixed_clock,
        &state, 1);
    callback = character_save_journal_v2_deadline_callback;
    memset(first, 'X', sizeof(first));
    memset(second, 'X', sizeof(second));
    failed += expect(callback(&provider, first) == 0 &&
                     callback(&provider, second) == 0,
                     "caller-owned provider callback succeeds");
    failed += expect(!strcmp(first, "2024-01-01T00:00:00Z") &&
                     !strcmp(first, second),
                     "callback output is exact and deterministic");
    failed += expect(state.calls == 2, "callback retains injected clock state");

    provider.extension_seconds = 301;
    memset(first, 'X', sizeof(first));
    failed += expect(callback(&provider, first) != 0 && is_zeroed(first),
                     "invalid provider extension clears output");
    failed += expect(character_save_journal_v2_deadline_callback(
        0, first) != 0 && is_zeroed(first),
                     "null provider clears output");
    return failed;
}

int main(void)
{
    return test_calendar_boundaries() |
        test_failures_clear_output() |
        test_provider_callback();
}
