#ifndef MUHAN_BANK_EVIDENCE_H
#define MUHAN_BANK_EVIDENCE_H

#include <stdint.h>

/* Metadata-only, read-only evidence for one FileStore bank record.  This is
 * not a persistence API and never exposes a decoded game object or file
 * descriptor. */
#define BANK_EVIDENCE_VERSION 1U
#define BANK_EVIDENCE_SHA256_HEX_LENGTH 64U
#define BANK_EVIDENCE_MAX_OCTETS (4U * 1024U * 1024U)

typedef enum bank_evidence_result {
    BANK_EVIDENCE_OK = 0,
    BANK_EVIDENCE_NOT_FOUND = -1,
    BANK_EVIDENCE_INVALID_INPUT = -2,
    BANK_EVIDENCE_CORRUPT = -3,
    BANK_EVIDENCE_IO_ERROR = -4,
    BANK_EVIDENCE_LIMIT = -5
} bank_evidence_result;

typedef struct bank_evidence {
    unsigned int version;
    bank_evidence_result result;
    char sha256[BANK_EVIDENCE_SHA256_HEX_LENGTH + 1];
    uint64_t octet_count;
    /* Items exclude the synthetic legacy bank container root.  Depth is
     * likewise measured below that root, so an empty bank has depth zero. */
    uint32_t item_count;
    uint32_t max_depth;
} bank_evidence;

/* Opens only the immutable FileStore source through its fixed locator.  It
 * does not dispatch through the mutable bank_store binding, write any source,
 * or enter bank.c gameplay operations.  On all non-OK outcomes every
 * evidence field other than version/result is cleared. */
bank_evidence_result bank_evidence_inspect(const char *name,
    bank_evidence *out);

#ifdef BANK_EVIDENCE_TESTING
enum bank_evidence_test_fault {
    BANK_EVIDENCE_TEST_FAULT_ALLOC = 1,
    BANK_EVIDENCE_TEST_FAULT_READ = 2,
    BANK_EVIDENCE_TEST_FAULT_CLOSE = 3
};
void bank_evidence_test_fail_next(int fault);
void bank_evidence_test_set_after_read(void (*callback)(void *), void *opaque);
void bank_evidence_test_reset(void);
#endif

#endif
