#define _POSIX_C_SOURCE 200809L

#include "character_save_journal_v2_deadline_native.h"

#if !defined(__linux__)
#error "character_save_journal_v2_deadline_native requires Linux CLOCK_REALTIME"
#endif

#include <stdint.h>
#include <string.h>
#include <time.h>

static int deadline_native_clock(void *opaque, int64_t *unix_seconds)
{
    struct timespec now;

    (void)opaque;
    if (!unix_seconds)
        return -1;
    if (clock_gettime(CLOCK_REALTIME, &now) != 0)
        return -1;
    if (now.tv_sec < (time_t)0)
        return -1;
    if ((uintmax_t)now.tv_sec > (uintmax_t)INT64_MAX)
        return -1;
    *unix_seconds = (int64_t)now.tv_sec;
    return 0;
}

void character_save_journal_v2_deadline_native_init(
    character_save_journal_v2_deadline_native *native,
    int64_t extension_seconds)
{
    if (!native)
        return;
    character_save_journal_v2_deadline_provider_init(
        &native->provider, deadline_native_clock, native, extension_seconds);
}

int character_save_journal_v2_deadline_native_callback(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE])
{
    character_save_journal_v2_deadline_native *native;

    if (output)
        memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE);
    if (!opaque || !output)
        return -1;
    native = (character_save_journal_v2_deadline_native *)opaque;
    return character_save_journal_v2_deadline_callback(&native->provider,
        output);
}
