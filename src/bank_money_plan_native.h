#ifndef MUHAN_BANK_MONEY_PLAN_NATIVE_H
#define MUHAN_BANK_MONEY_PLAN_NATIVE_H
#include <stddef.h>
#include <stdint.h>
/* Explicit trusted absolute executable; no shell or inherited credentials.
 * args: direction, amount, player SHA256, bank SHA256. Standard descriptors
 * must be open. Caller must own child reaping (no concurrent wait-any handler).
 * Linux/glibc close-from spawn actions prevent inherited game/DB descriptors.
 * Synchronous deadline-bounded child exchange; no game mutation.
 * Failure publishes no output. Caller frees a successful output. */
int bank_money_plan_native(const char *,const char *const [4],const unsigned char *,size_t,int,unsigned char **,size_t *);
/* Internal framed subprocess exchange; used by the durable preparation helper. */
int bank_money_process_native(const char *,const char *const *,int,const unsigned char *,size_t,int,unsigned char **,size_t *);
/* Versioned reply: positive resolved i64 then the normal pair frame.
 * Strips metadata; caller persists resolved amount with the returned bytes. */
int bank_money_plan_resolved_native(const char *,const char *const [4],const unsigned char *,size_t,int,unsigned char **,size_t *,uint64_t *);
#endif
