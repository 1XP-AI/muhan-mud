/* Feature-gated, metadata-only text envelope for the already canonical V1
 * identity-evidence codec.  It intentionally stays out of OBJECTS. */
#include "onboarding_evidence_control.h"

#include <stdlib.h>
#include <string.h>

static unsigned long oec_bounded_strlen(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1U;
    for(length = 0; length <= limit; length++)
        if(value[length] == 0) return length;
    return limit + 1U;
}

static int oec_lower_hex(value)
unsigned char value;
{
    return (value >= '0' && value <= '9') ||
           (value >= 'a' && value <= 'f');
}

static int oec_hex_value(value)
unsigned char value;
{
    if(value >= '0' && value <= '9') return value - '0';
    return value - 'a' + 10;
}

int onboarding_evidence_control_enabled(void)
{
    const char *enabled;
    enabled = getenv("MUD_ENABLE_ONBOARDING_EVIDENCE");
    return enabled && strcmp(enabled, "1") == 0;
}

onboarding_evidence_control_status onboarding_evidence_control_parse(line, record)
const char *line;
onboarding_evidence_control_record *record;
{
    unsigned char wire[LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_LENGTH];
    unsigned long length, hex_length, i;
    int high, low, wire_status;
    onboarding_evidence_control_status result;

    result = ONBOARDING_EVIDENCE_CONTROL_INVALID_RECORD;
    if(record) memset(record, 0, sizeof(*record));
    if(!line || !record) return ONBOARDING_EVIDENCE_CONTROL_INVALID_ARGUMENT;
    if(!onboarding_evidence_control_enabled())
        return ONBOARDING_EVIDENCE_CONTROL_DISABLED;
    length = oec_bounded_strlen(line, ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH);
    if(length < ONBOARDING_EVIDENCE_CONTROL_MIN_RECORD_LENGTH ||
       length > ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH ||
       line[length - 1U] != '\n' ||
       memcmp(line, ONBOARDING_EVIDENCE_CONTROL_PREFIX,
              ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH))
        goto out;
    hex_length = length - ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH - 1U;
    if(!hex_length || (hex_length & 1U)) goto out;
    high = -1;
    for(i = 0; i < hex_length; i++) {
        unsigned char value;
        value = (unsigned char)line[ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + i];
        if(!oec_lower_hex(value)) goto out;
        low = oec_hex_value(value);
        if(high < 0) high = low;
        else { wire[i / 2U] = (unsigned char)((high << 4) | low); high = -1; }
    }
    if(high >= 0) goto out;
    wire_status = legacy_identity_evidence_wire_decode(wire, hex_length / 2U,
                                                       &record->evidence);
    if(wire_status != LEGACY_IDENTITY_EVIDENCE_WIRE_OK) {
        result = ONBOARDING_EVIDENCE_CONTROL_INVALID_WIRE;
        goto out;
    }
    result = ONBOARDING_EVIDENCE_CONTROL_OK;
out:
    if(result != ONBOARDING_EVIDENCE_CONTROL_OK) memset(record, 0, sizeof(*record));
    memset(wire, 0, sizeof(wire));
    return result;
}

onboarding_evidence_control_status onboarding_evidence_control_format(out, out_size,
                                                                       record)
char *out;
unsigned long out_size;
const onboarding_evidence_control_record *record;
{
    static const char hex[] = "0123456789abcdef";
    unsigned char *wire;
    size_t wire_length;
    unsigned long needed, i;
    int wire_status;

    if(out && out_size) out[0] = 0;
    if(!out || !out_size || !record)
        return ONBOARDING_EVIDENCE_CONTROL_INVALID_ARGUMENT;
    if(!onboarding_evidence_control_enabled())
        return ONBOARDING_EVIDENCE_CONTROL_DISABLED;
    wire = 0; wire_length = 0;
    wire_status = legacy_identity_evidence_wire_encode(&record->evidence, &wire,
                                                        &wire_length);
    if(wire_status != LEGACY_IDENTITY_EVIDENCE_WIRE_OK)
        return ONBOARDING_EVIDENCE_CONTROL_INVALID_WIRE;
    needed = ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH +
             2U * (unsigned long)wire_length + 1U;
    if(needed > ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH ||
       out_size <= needed) {
        legacy_identity_evidence_wire_free(wire);
        return ONBOARDING_EVIDENCE_CONTROL_OUTPUT_TOO_SMALL;
    }
    memcpy(out, ONBOARDING_EVIDENCE_CONTROL_PREFIX,
           ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH);
    for(i = 0; i < (unsigned long)wire_length; i++) {
        out[ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + 2U * i] = hex[wire[i] >> 4];
        out[ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + 2U * i + 1U] = hex[wire[i] & 15U];
    }
    out[needed - 1U] = '\n';
    out[needed] = 0;
    legacy_identity_evidence_wire_free(wire);
    return ONBOARDING_EVIDENCE_CONTROL_OK;
}
