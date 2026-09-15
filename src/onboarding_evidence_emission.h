#ifndef ONBOARDING_EVIDENCE_EMISSION_H
#define ONBOARDING_EVIDENCE_EMISSION_H

#include "onboarding_evidence_control.h"

/* Prepare the one feature-gated C-to-Gateway evidence record after a durable
 * player save.  The caller supplies only already-established canonical state:
 * a canonical legacy name and the SHA-256 it has already observed.  This
 * helper has no socket, receipt, relay, or onboarding-state side effects. */
typedef enum onboarding_evidence_emission_status {
    ONBOARDING_EVIDENCE_EMISSION_OK = 0,
    ONBOARDING_EVIDENCE_EMISSION_DISABLED = 1,
    ONBOARDING_EVIDENCE_EMISSION_INVALID_ARGUMENT = -1,
    ONBOARDING_EVIDENCE_EMISSION_INSPECTION_FAILED = -2,
    ONBOARDING_EVIDENCE_EMISSION_MISMATCH = -3,
    ONBOARDING_EVIDENCE_EMISSION_FORMAT_FAILED = -4
} onboarding_evidence_emission_status;

onboarding_evidence_emission_status onboarding_evidence_emission_prepare(
    const char *canonical_name, const char *known_file_sha256, char *out,
    unsigned long out_size);

#endif
