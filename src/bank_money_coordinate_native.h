#ifndef MUHAN_BANK_MONEY_COORDINATE_NATIVE_H
#define MUHAN_BANK_MONEY_COORDINATE_NATIVE_H
#include "bank_money_commit_native.h"
typedef struct bank_money_coordinate_result {
    uint64_t revision;
    uint64_t amount;
    unsigned char *frame;
    size_t frame_length;
} bank_money_coordinate_result;
/* Fresh command only; args use the eleven qualified commit fields, but amount
 * may be a <=24-byte decimal/Korean money token, all, or 모두. Rust resolves
 * against the digest-bound DB pair; only canonical numeric amount is persisted.
 * Reads authoritative state, requires the caller's exact expected revision,
 * plans with Rust, durably prepares and commits. No automatic retry/replanning.
 * Only a new CONFIRMED commit returns resolved amount and owned bytes (caller frees frame).
 * Historical RETRY never publishes a wallet snapshot. Existing pending work
 * must be reconciled separately; this is not a recovery entry point.
 * Each phase has its own timeout. On failure discard the borrowed PGconn.
 * Does not mutate live creatures or install the bank command route. */
int bank_money_coordinate_native(void *,const char *,const char *,const char *,const char *,const char *const [11],int,bank_money_coordinate_result *);
#endif
