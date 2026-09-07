#ifndef MUHAN_BANK_MONEY_PLAN_NATIVE_H
#define MUHAN_BANK_MONEY_PLAN_NATIVE_H
#include <stddef.h>
/* Explicit trusted absolute executable; no shell or inherited credentials.
 * args: direction, amount, player SHA256, bank SHA256. Standard descriptors
 * must be open. Caller must own child reaping (no concurrent wait-any handler).
 * Linux/glibc close-from spawn actions prevent inherited game/DB descriptors.
 * Synchronous deadline-bounded child exchange; no game mutation.
 * Failure publishes no output. Caller frees a successful output. */
int bank_money_plan_native(const char *,const char *const [4],const unsigned char *,size_t,int,unsigned char **,size_t *);
#endif
