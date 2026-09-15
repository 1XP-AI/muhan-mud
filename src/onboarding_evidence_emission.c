/* Durable metadata proof preparation for the MUD1O evidence feature. */
#include "onboarding_evidence_emission.h"

#include "legacy_identity_evidence.h"

#include <string.h>

onboarding_evidence_emission_status onboarding_evidence_emission_prepare(
    canonical_name, known_file_sha256, out, out_size)
const char *canonical_name;
const char *known_file_sha256;
char *out;
unsigned long out_size;
{
    onboarding_evidence_control_record record;
    onboarding_evidence_control_status format_status;
    onboarding_evidence_emission_status result;

    if(out && out_size) out[0] = 0;
    memset(&record, 0, sizeof(record));
    result = ONBOARDING_EVIDENCE_EMISSION_INVALID_ARGUMENT;
    if(!canonical_name || !known_file_sha256 || !out || !out_size)
        goto out;
    if(!onboarding_evidence_control_enabled()) {
        result = ONBOARDING_EVIDENCE_EMISSION_DISABLED;
        goto out;
    }
    if(legacy_identity_evidence_inspect(canonical_name, &record.evidence) !=
       LEGACY_IDENTITY_EVIDENCE_OK ||
       record.evidence.result != LEGACY_IDENTITY_EVIDENCE_OK) {
        result = ONBOARDING_EVIDENCE_EMISSION_INSPECTION_FAILED;
        goto out;
    }
    if(record.evidence.version != LEGACY_IDENTITY_EVIDENCE_VERSION ||
       strcmp(record.evidence.storage_format,
              LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT) != 0 ||
       strcmp(record.evidence.canonical_name, canonical_name) != 0 ||
       strcmp(record.evidence.player_file_sha256, known_file_sha256) != 0) {
        result = ONBOARDING_EVIDENCE_EMISSION_MISMATCH;
        goto out;
    }
    format_status = onboarding_evidence_control_format(out, out_size, &record);
    if(format_status != ONBOARDING_EVIDENCE_CONTROL_OK) {
        if(out && out_size) out[0] = 0;
        result = ONBOARDING_EVIDENCE_EMISSION_FORMAT_FAILED;
        goto out;
    }
    result = ONBOARDING_EVIDENCE_EMISSION_OK;
out:
    memset(&record, 0, sizeof(record));
    return result;
}
