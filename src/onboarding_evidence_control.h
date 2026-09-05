#ifndef ONBOARDING_EVIDENCE_CONTROL_H
#define ONBOARDING_EVIDENCE_CONTROL_H

#include "legacy_identity_evidence_wire.h"

/* This test-only composition seam is deliberately separate from the live
 * onboarding session and control/state parsers.  The environment gate is off
 * unless it is exactly 1. */
#define ONBOARDING_EVIDENCE_CONTROL_PREFIX "MUD1O EVIDENCE|1|"
#define ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH 17U
#define ONBOARDING_EVIDENCE_CONTROL_MIN_RECORD_LENGTH \
    (ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + \
     2U * 30U + 1U)
#define ONBOARDING_EVIDENCE_CONTROL_MAX_RECORD_LENGTH \
    (ONBOARDING_EVIDENCE_CONTROL_PREFIX_LENGTH + \
     2U * LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_LENGTH + 1U)

typedef enum onboarding_evidence_control_status {
    ONBOARDING_EVIDENCE_CONTROL_OK = 0,
    ONBOARDING_EVIDENCE_CONTROL_DISABLED = 1,
    ONBOARDING_EVIDENCE_CONTROL_INVALID_ARGUMENT = -1,
    ONBOARDING_EVIDENCE_CONTROL_INVALID_RECORD = -2,
    ONBOARDING_EVIDENCE_CONTROL_INVALID_WIRE = -3,
    ONBOARDING_EVIDENCE_CONTROL_OUTPUT_TOO_SMALL = -4
} onboarding_evidence_control_status;

/* The boundary returns decoded canonical metadata, never a caller-owned wire
 * string.  It has no player, session, relay, or state-machine linkage. */
typedef struct onboarding_evidence_control_record {
    legacy_identity_evidence evidence;
} onboarding_evidence_control_record;

int onboarding_evidence_control_enabled(void);
onboarding_evidence_control_status onboarding_evidence_control_parse(
    const char *line, onboarding_evidence_control_record *record);
onboarding_evidence_control_status onboarding_evidence_control_format(
    char *out, unsigned long out_size,
    const onboarding_evidence_control_record *record);

#endif
