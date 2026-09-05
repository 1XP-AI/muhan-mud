#ifndef M3_WAKE_TRANSPORT_H
#define M3_WAKE_TRANSPORT_H

#include <stddef.h>

#include "m3_wake_v1.h"

/* Test-only local boundary. It owns no descriptors, process, identity, or
 * durable state; the embedding supplies storage and the sink effect. */
typedef enum m3_wake_transport_result {
    M3_WAKE_TRANSPORT_ACCEPTED = 0,
    M3_WAKE_TRANSPORT_BACKPRESSURE = 1,
    M3_WAKE_TRANSPORT_DISCONNECTED = 2,
    M3_WAKE_TRANSPORT_INVALID = 3
} m3_wake_transport_result;

typedef enum m3_wake_transport_sink_result {
    M3_WAKE_TRANSPORT_SINK_ACCEPTED = 0,
    M3_WAKE_TRANSPORT_SINK_BLOCKED = 1,
    M3_WAKE_TRANSPORT_SINK_DISCONNECTED = 2
} m3_wake_transport_sink_result;

typedef m3_wake_transport_sink_result (*m3_wake_transport_sink)(
    void *opaque, const unsigned char *frame, size_t frame_length);

typedef struct m3_wake_transport {
    unsigned char *storage;
    size_t capacity;
    size_t head;
    size_t count;
    int connected;
    unsigned long generation;
    int pumping;
    int reentry_violation;
    size_t storage_length;
} m3_wake_transport;

/* Storage is caller-owned and must hold an integral number of 16-byte frames. */
int m3_wake_transport_init(m3_wake_transport *transport,
    unsigned char *storage, size_t storage_length);
void m3_wake_transport_connect(m3_wake_transport *transport);
void m3_wake_transport_disconnect(m3_wake_transport *transport);

/* Enqueue accepts only the unchanged canonical v1 frame. */
m3_wake_transport_result m3_wake_transport_enqueue(
    m3_wake_transport *transport, const unsigned char *frame,
    size_t frame_length);
m3_wake_transport_result m3_wake_transport_wake(
    m3_wake_transport *transport);

/* Pump never waits. BLOCKED leaves the head frame queued; DISCONNECTED clears it. */
size_t m3_wake_transport_pump(m3_wake_transport *transport,
    m3_wake_transport_sink sink, void *opaque, size_t max_frames);
size_t m3_wake_transport_pending(const m3_wake_transport *transport);

#endif
