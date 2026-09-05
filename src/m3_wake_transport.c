#include "m3_wake_transport.h"

#include <stdint.h>
#include <string.h>

static m3_wake_transport *m3_wake_transport_active;

static int m3_wake_transport_ranges_overlap(const void *left,
    size_t left_length, const void *right, size_t right_length)
{
    uintptr_t left_start = (uintptr_t)left;
    uintptr_t right_start = (uintptr_t)right;
    uintptr_t left_end;
    uintptr_t right_end;

    if(left_length > UINTPTR_MAX - left_start ||
       right_length > UINTPTR_MAX - right_start)
        return 1;
    left_end = left_start + left_length;
    right_end = right_start + right_length;
    return left_start < right_end && right_start < left_end;
}

static void m3_wake_transport_changed(m3_wake_transport *transport)
{
    transport->generation++;
}

static int m3_wake_transport_valid(const m3_wake_transport *transport)
{
    if(!transport || !transport->storage || transport->capacity == 0 ||
       transport->storage_length < M3_WAKE_V1_FRAME_LENGTH ||
       transport->storage_length % M3_WAKE_V1_FRAME_LENGTH != 0 ||
       transport->capacity != transport->storage_length /
           M3_WAKE_V1_FRAME_LENGTH || transport->head >= transport->capacity ||
       transport->count > transport->capacity ||
       (transport->connected != 0 && transport->connected != 1) ||
       (transport->pumping != 0 && transport->pumping != 1) ||
       (transport->reentry_violation != 0 && transport->reentry_violation != 1))
        return 0;
    return 1;
}

static void m3_wake_transport_fail_closed(m3_wake_transport *transport)
{
    if(!transport) return;
    transport->connected = 0;
    transport->head = 0;
    transport->count = 0;
    transport->pumping = 0;
    transport->reentry_violation = 0;
    m3_wake_transport_changed(transport);
}

static int m3_wake_transport_reentry(m3_wake_transport *transport)
{
    if(!transport->pumping) return 0;
    transport->reentry_violation = 1;
    return 1;
}

static unsigned char *m3_wake_transport_slot(m3_wake_transport *transport,
    size_t index)
{
    return transport->storage + (index * M3_WAKE_V1_FRAME_LENGTH);
}

int m3_wake_transport_init(m3_wake_transport *transport,
    unsigned char *storage, size_t storage_length)
{
    if(transport && m3_wake_transport_active == transport) {
        transport->reentry_violation = 1;
        return 0;
    }
    if(!transport || !storage || storage_length < M3_WAKE_V1_FRAME_LENGTH ||
       storage_length % M3_WAKE_V1_FRAME_LENGTH != 0 ||
       m3_wake_transport_ranges_overlap(transport, sizeof(*transport),
           storage, storage_length))
        return 0;
    memset(transport, 0, sizeof(*transport));
    transport->storage = storage;
    transport->capacity = storage_length / M3_WAKE_V1_FRAME_LENGTH;
    transport->storage_length = storage_length;
    return 1;
}

void m3_wake_transport_connect(m3_wake_transport *transport)
{
    if(transport && !m3_wake_transport_reentry(transport) &&
       m3_wake_transport_valid(transport)) {
        transport->connected = 1;
        m3_wake_transport_changed(transport);
    }
}

void m3_wake_transport_disconnect(m3_wake_transport *transport)
{
    if(!transport) return;
    if(m3_wake_transport_reentry(transport)) return;
    if(!m3_wake_transport_valid(transport)) {
        m3_wake_transport_fail_closed(transport);
        return;
    }
    transport->connected = 0;
    transport->head = 0;
    transport->count = 0;
    m3_wake_transport_changed(transport);
}

m3_wake_transport_result m3_wake_transport_enqueue(
    m3_wake_transport *transport, const unsigned char *frame,
    size_t frame_length)
{
    size_t tail;

    if(!m3_wake_transport_valid(transport) ||
       m3_wake_transport_reentry(transport) || !frame ||
       m3_wake_v1_decode(frame, frame_length) != M3_WAKE_V1_OK)
        return M3_WAKE_TRANSPORT_INVALID;
    if(!transport->connected) return M3_WAKE_TRANSPORT_DISCONNECTED;
    if(transport->count == transport->capacity)
        return M3_WAKE_TRANSPORT_BACKPRESSURE;
    tail = (transport->head + transport->count) % transport->capacity;
    memmove(m3_wake_transport_slot(transport, tail), frame,
        M3_WAKE_V1_FRAME_LENGTH);
    transport->count++;
    m3_wake_transport_changed(transport);
    return M3_WAKE_TRANSPORT_ACCEPTED;
}

m3_wake_transport_result m3_wake_transport_wake(
    m3_wake_transport *transport)
{
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];

    if(m3_wake_v1_encode(frame, sizeof(frame)) != M3_WAKE_V1_OK)
        return M3_WAKE_TRANSPORT_INVALID;
    return m3_wake_transport_enqueue(transport, frame, sizeof(frame));
}

size_t m3_wake_transport_pump(m3_wake_transport *transport,
    m3_wake_transport_sink sink, void *opaque, size_t max_frames)
{
    size_t delivered = 0;
    m3_wake_transport_sink_result result;
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];
    unsigned long generation;
    unsigned char *storage;
    size_t capacity;
    size_t head;
    size_t count;
    size_t storage_length;
    int connected;
    m3_wake_transport *previous_active;

    if(transport && (transport->pumping ||
       m3_wake_transport_active == transport)) {
        transport->reentry_violation = 1;
        return 0;
    }
    if(!m3_wake_transport_valid(transport) || !sink || !transport->connected) {
        if(transport && !m3_wake_transport_valid(transport))
            m3_wake_transport_fail_closed(transport);
        return 0;
    }
    while(transport->count > 0 && delivered < max_frames) {
        if(m3_wake_v1_decode(m3_wake_transport_slot(transport, transport->head),
               M3_WAKE_V1_FRAME_LENGTH) != M3_WAKE_V1_OK) {
            m3_wake_transport_disconnect(transport);
            break;
        }
        memmove(frame, m3_wake_transport_slot(transport, transport->head),
            M3_WAKE_V1_FRAME_LENGTH);
        generation = transport->generation;
        storage = transport->storage;
        capacity = transport->capacity;
        head = transport->head;
        count = transport->count;
        storage_length = transport->storage_length;
        connected = transport->connected;
        transport->pumping = 1;
        previous_active = m3_wake_transport_active;
        m3_wake_transport_active = transport;
        result = sink(opaque, frame, M3_WAKE_V1_FRAME_LENGTH);
        m3_wake_transport_active = previous_active;
        transport->pumping = 0;
        if(transport->reentry_violation || transport->generation != generation ||
           transport->storage != storage || transport->capacity != capacity ||
           transport->head != head || transport->count != count ||
           transport->storage_length != storage_length ||
           transport->connected != connected || !m3_wake_transport_valid(transport)) {
            m3_wake_transport_disconnect(transport);
            if(transport->connected || transport->count != 0)
                m3_wake_transport_fail_closed(transport);
            break;
        }
        if(result == M3_WAKE_TRANSPORT_SINK_BLOCKED) break;
        if(result == M3_WAKE_TRANSPORT_SINK_DISCONNECTED) {
            m3_wake_transport_disconnect(transport);
            break;
        }
        if(result != M3_WAKE_TRANSPORT_SINK_ACCEPTED) {
            m3_wake_transport_disconnect(transport);
            break;
        }
        transport->head = (transport->head + 1) % transport->capacity;
        transport->count--;
        delivered++;
    }
    return delivered;
}

size_t m3_wake_transport_pending(const m3_wake_transport *transport)
{
    return transport ? transport->count : 0;
}
