#ifndef M3_WAKE_SUPERVISOR_H
#define M3_WAKE_SUPERVISOR_H

#include <stddef.h>

#include "m3_wake_v1.h"

typedef enum m3_wake_supervisor_mode {
    M3_WAKE_SUPERVISOR_OFF = 0,
    M3_WAKE_SUPERVISOR_ON = 1
} m3_wake_supervisor_mode;

typedef enum m3_wake_supervisor_wake_result {
    M3_WAKE_SUPERVISOR_WAKE_OFF = 0,
    M3_WAKE_SUPERVISOR_WAKE_SENT = 1,
    M3_WAKE_SUPERVISOR_WAKE_DROPPED = 2,
    M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF = 3
} m3_wake_supervisor_wake_result;

typedef enum m3_wake_supervisor_reap_result {
    M3_WAKE_SUPERVISOR_REAP_OFF = 0,
    M3_WAKE_SUPERVISOR_REAP_NONE = 1,
    M3_WAKE_SUPERVISOR_REAPED = 2
} m3_wake_supervisor_reap_result;

/* Every effect is supplied by the embedding caller. */
typedef struct m3_wake_supervisor_operations {
    long (*claim_owner)(void *opaque);
    long (*send_frame)(void *opaque, long owner, const unsigned char *frame,
        size_t frame_length);
    int (*last_error)(void *opaque);
    int (*reap_owner)(void *opaque, long owner);
    void (*release_owner)(void *opaque, long owner);
} m3_wake_supervisor_operations;

typedef struct m3_wake_supervisor {
    const m3_wake_supervisor_operations *operations;
    void *opaque;
    unsigned long retry_at;
    unsigned long backoff;
    long owner;
    m3_wake_supervisor_mode mode;
} m3_wake_supervisor;

void m3_wake_supervisor_init(m3_wake_supervisor *supervisor,
    m3_wake_supervisor_mode mode,
    const m3_wake_supervisor_operations *operations, void *opaque,
    unsigned long backoff);

/* A wake is a best-effort hint: would-block loss keeps its owner; other
 * send loss releases the owner and starts the configured retry backoff. */
m3_wake_supervisor_wake_result m3_wake_supervisor_wake(
    m3_wake_supervisor *supervisor, unsigned long now);

/* The caller provides the event-loop time; this function never waits. */
m3_wake_supervisor_reap_result m3_wake_supervisor_reap(
    m3_wake_supervisor *supervisor, unsigned long now);

/* Release is one-shot and never waits for completion. */
void m3_wake_supervisor_shutdown(m3_wake_supervisor *supervisor);

int m3_wake_supervisor_has_owner(const m3_wake_supervisor *supervisor);
unsigned long m3_wake_supervisor_retry_at(const m3_wake_supervisor *supervisor);

#endif
