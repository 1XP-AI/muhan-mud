#include "character_save_journal_v2_deadline.h"

#include <string.h>

#define DEADLINE_SECONDS_PER_MINUTE 60
#define DEADLINE_SECONDS_PER_HOUR 3600
#define DEADLINE_SECONDS_PER_DAY 86400
#define DEADLINE_DAYS_TO_1970 719468
#define DEADLINE_DAYS_PER_400_YEARS 146097

static int deadline_extension_valid(int64_t extension_seconds)
{
    return extension_seconds > 0 &&
        extension_seconds <=
        (int64_t)CHARACTER_SAVE_JOURNAL_V2_DEADLINE_EXTENSION_MAX_SECONDS;
}

static int deadline_add(int64_t now, int64_t extension, int64_t *result)
{
    if (now < 0 || !deadline_extension_valid(extension) || !result)
        return -1;
    if (now > INT64_MAX - extension)
        return -1;
    *result = now + extension;
    return 0;
}

/* 9999-12-31T23:59:59Z.  Keeping this explicit also avoids depending on
 * time_t width or the host's calendar/timezone implementation. */
static int64_t deadline_max_unix_seconds(void)
{
    return (int64_t)253402300799LL;
}

/* Convert a non-negative count of Unix days to Gregorian calendar fields.
 * This is the civil_from_days algorithm, with all arithmetic well inside
 * int64_t for the supported range. */
static void deadline_calendar(int64_t days, int64_t *year, int64_t *month,
                              int64_t *day)
{
    int64_t z, era, doe, yoe, y, doy, month_part;

    z = days + (int64_t)DEADLINE_DAYS_TO_1970;
    era = z / (int64_t)DEADLINE_DAYS_PER_400_YEARS;
    doe = z - era * (int64_t)DEADLINE_DAYS_PER_400_YEARS;
    yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    y = yoe + era * 400;
    doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    month_part = (5 * doy + 2) / 153;
    *day = doy - (153 * month_part + 2) / 5 + 1;
    *month = month_part + (month_part < 10 ? 3 : -9);
    *year = y + (*month <= 2 ? 1 : 0);
}

static void deadline_two_digits(char *output, int64_t value)
{
    output[0] = (char)('0' + value / 10);
    output[1] = (char)('0' + value % 10);
}

static void deadline_four_digits(char *output, int64_t value)
{
    output[0] = (char)('0' + value / 1000);
    value %= 1000;
    output[1] = (char)('0' + value / 100);
    value %= 100;
    output[2] = (char)('0' + value / 10);
    output[3] = (char)('0' + value % 10);
}

static int deadline_format(int64_t timestamp, char output[64])
{
    int64_t days, remainder, year, month, day;
    int64_t hour, minute, second;

    if (!output || timestamp < 0 || timestamp > deadline_max_unix_seconds())
        return -1;
    days = timestamp / (int64_t)DEADLINE_SECONDS_PER_DAY;
    remainder = timestamp % (int64_t)DEADLINE_SECONDS_PER_DAY;
    hour = remainder / (int64_t)DEADLINE_SECONDS_PER_HOUR;
    remainder %= (int64_t)DEADLINE_SECONDS_PER_HOUR;
    minute = remainder / (int64_t)DEADLINE_SECONDS_PER_MINUTE;
    second = remainder % (int64_t)DEADLINE_SECONDS_PER_MINUTE;
    deadline_calendar(days, &year, &month, &day);
    if (year < 1970 || year > 9999)
        return -1;

    deadline_four_digits(output, year);
    output[4] = '-';
    deadline_two_digits(output + 5, month);
    output[7] = '-';
    deadline_two_digits(output + 8, day);
    output[10] = 'T';
    deadline_two_digits(output + 11, hour);
    output[13] = ':';
    deadline_two_digits(output + 14, minute);
    output[16] = ':';
    deadline_two_digits(output + 17, second);
    output[19] = 'Z';
    output[20] = '\0';
    return 0;
}

int character_save_journal_v2_deadline_generate(
    character_save_journal_v2_deadline_clock clock,
    void *clock_opaque, int64_t extension_seconds, char output[64])
{
    int64_t now, deadline;

    if (output)
        memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE);
    if (!output || !clock || !deadline_extension_valid(extension_seconds))
        return -1;
    /* A successful clock must write its result.  The sentinel makes a
     * malformed success fail closed instead of accidentally using epoch. */
    now = INT64_MIN;
    if (clock(clock_opaque, &now) != 0)
        return -1;
    if (deadline_add(now, extension_seconds, &deadline) != 0)
        return -1;
    return deadline_format(deadline, output);
}

void character_save_journal_v2_deadline_provider_init(
    character_save_journal_v2_deadline_provider *provider,
    character_save_journal_v2_deadline_clock clock,
    void *clock_opaque, int64_t extension_seconds)
{
    if (!provider)
        return;
    provider->clock = clock;
    provider->clock_opaque = clock_opaque;
    provider->extension_seconds = extension_seconds;
}

int character_save_journal_v2_deadline_callback(void *opaque, char output[64])
{
    character_save_journal_v2_deadline_provider *provider;

    if (output)
        memset(output, 0, CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE);
    if (!opaque || !output)
        return -1;
    provider = (character_save_journal_v2_deadline_provider *)opaque;
    return character_save_journal_v2_deadline_generate(
        provider->clock, provider->clock_opaque, provider->extension_seconds,
        output);
}
