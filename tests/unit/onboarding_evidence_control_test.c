#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "onboarding_admission.h"
#include "onboarding_evidence_control.h"

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_evidence_control_test: %s\n", message);
    return 1;
}

static void valid(record, name)
onboarding_evidence_control_record *record;
const char *name;
{
    memset(record, 0, sizeof(*record));
    record->evidence.version = LEGACY_IDENTITY_EVIDENCE_VERSION;
    record->evidence.result = LEGACY_IDENTITY_EVIDENCE_OK;
    record->evidence.canonicalization = LEGACY_IDENTITY_EVIDENCE_NORMALIZED;
    strcpy(record->evidence.canonical_name, name);
    strcpy(record->evidence.legacy_shard, "35");
    strcpy(record->evidence.player_file_sha256,
           "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49");
    strcpy(record->evidence.storage_format, LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT);
}

static int test_disabled(void)
{
    onboarding_evidence_control_record record;
    onboarding_control control;
    char line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1];
    int failed;

    failed = 0;
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    valid(&record, "Alice");
    failed += expect(!onboarding_evidence_control_enabled() &&
                     onboarding_evidence_control_format(line, sizeof(line), &record) ==
                     ONBOARDING_EVIDENCE_CONTROL_DISABLED &&
                     onboarding_evidence_control_parse("MUD1O EVIDENCE|1|00\n", &record) ==
                     ONBOARDING_EVIDENCE_CONTROL_DISABLED,
                     "evidence boundary is disabled by default");
    failed += expect(onboarding_parse_c_control("MUD1O VERIFIED|416c696365|"
                     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", &control) == 0 &&
                     control.kind == ONBOARDING_CONTROL_VERIFIED &&
                     onboarding_parse_c_control("MUD1O SAVED|11111111-1111-4111-8111-111111111111|"
                     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef|player-v1\n", &control) == 0 &&
                     control.kind == ONBOARDING_CONTROL_SAVED,
                     "feature-off preserves SAVED and VERIFIED behavior");
    return failed;
}

static int test_round_trip_and_limits(void)
{
    onboarding_evidence_control_record record, parsed;
    char line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1];
    static const char fixture_hex[] =
        "4d55444c494500000001000100000056000105416c696365023335403138663864326562346133383762626331653337656330393961373332363830353733396263396339396563663066313462383038613562636236356266343909706c617965722d7631";
    int failed;

    failed = 0;
    setenv("MUD_ENABLE_ONBOARDING_EVIDENCE", "1", 1);
    snprintf(line, sizeof(line), ONBOARDING_EVIDENCE_CONTROL_PREFIX "%s\n",
             fixture_hex);
    failed += expect(onboarding_evidence_control_parse(line, &parsed) == 0 &&
                     parsed.evidence.result == LEGACY_IDENTITY_EVIDENCE_OK &&
                     !strcmp(parsed.evidence.canonical_name, "Alice"),
                     "shared canonical V1 fixture parses to typed metadata");
    valid(&record, "Alice");
    failed += expect(onboarding_evidence_control_format(line, sizeof(line), &record) == 0 &&
                     onboarding_evidence_control_parse(line, &parsed) == 0 &&
                     !memcmp(&record, &parsed, sizeof(record)),
                     "fixture evidence formats and parses as typed canonical metadata");
    valid(&record, "Abcdefghijklmn");
    failed += expect(onboarding_evidence_control_format(line, sizeof(line), &record) == 0 &&
                     strlen(line) == ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH &&
                     onboarding_evidence_control_parse(line, &parsed) == 0 &&
                     !strcmp(parsed.evidence.canonical_name, "Abcdefghijklmn"),
                     "valid 14-byte name produces the exact 240-byte maximum record");
    return failed;
}

static int test_rejections(void)
{
    onboarding_evidence_control_record record;
    char line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 2];
    unsigned long at;
    int failed;

    failed = 0;
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    at = ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH;
    line[at] = 'A';
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "upper hex is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    line[at] = 'g';
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "nonhex is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    memmove(line + strlen(line) - 2U, line + strlen(line) - 1U, 2U);
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "odd hex is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    memcpy(line, "MUD1O EVIDENCE|2|", ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH);
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "wrong envelope version is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    line[strlen(line) - 1U] = '\r';
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "CR and missing LF are rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    line[at] = ' ';
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "whitespace is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    line[at] = 0;
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "NUL is rejected");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    strcpy(line + strlen(line) - 1U, "00\n");
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "trailing wire bytes are rejected by the canonical decoder");
    valid(&record, "Alice");
    onboarding_evidence_control_format(line, sizeof(line), &record);
    line[ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + 1U] = 'e';
    failed += expect(onboarding_evidence_control_parse(line, &record) ==
                     ONBOARDING_EVIDENCE_CONTROL_INVALID_WIRE,
                     "malformed canonical wire is rejected by the existing decoder");
    memset(line, 'a', ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1U);
    line[ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH + 1U] = 0;
    failed += expect(onboarding_evidence_control_parse(line, &record) < 0,
                     "oversize records are rejected without scanning beyond the limit");
    return failed;
}

int main(void)
{
    int failed;
    failed = test_disabled() + test_round_trip_and_limits() + test_rejections();
    unsetenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    if(!failed) puts("onboarding_evidence_control_test: ok");
    return failed ? 1 : 0;
}
