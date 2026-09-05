/* TDD RED contract for the bounded, test-only wake transport boundary. */
#include "m3_wake_transport.h"

#include <stdio.h>
#include <string.h>

static const unsigned char canonical[M3_WAKE_V1_FRAME_LENGTH] = {
    0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00,
    0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00
};

typedef struct sink_fixture {
    int calls;
    int mutate;
    int reenter;
    int reinit;
    int repump;
    int clear_pumping;
    int cross_reenter;
    size_t nested_result;
    size_t cross_other_result;
    size_t cross_same_result;
    int nested_calls;
    m3_wake_transport *transport;
    m3_wake_transport *other_transport;
    unsigned char *storage;
    size_t storage_length;
    m3_wake_transport_sink_result result;
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];
    size_t length;
} sink_fixture;

static m3_wake_transport_sink_result nonrecursive_sink(void *opaque,
    const unsigned char *frame, size_t length)
{
    sink_fixture *fixture = (sink_fixture *)opaque;
    (void)frame;
    (void)length;
    fixture->nested_calls++;
    return M3_WAKE_TRANSPORT_SINK_ACCEPTED;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "m3_wake_transport_test: %s\n", message);
    return 1;
}

static m3_wake_transport_sink_result sink(void *opaque,
    const unsigned char *frame, size_t length)
{
    sink_fixture *fixture = (sink_fixture *)opaque;
    fixture->calls++;
    fixture->length = length;
    memcpy(fixture->frame, frame, length);
    if(fixture->mutate) ((unsigned char *)frame)[0] ^= 0xff;
    if(fixture->reenter) {
        m3_wake_transport_disconnect(fixture->transport);
        m3_wake_transport_connect(fixture->transport);
        (void)m3_wake_transport_wake(fixture->transport);
    }
    if(fixture->reinit) {
        (void)m3_wake_transport_init(fixture->transport, fixture->storage,
            fixture->storage_length);
    }
    if(fixture->repump) {
        if(fixture->clear_pumping) fixture->transport->pumping = 0;
        fixture->nested_result = m3_wake_transport_pump(fixture->transport,
            nonrecursive_sink, fixture, 1);
    }
    if(fixture->cross_reenter) {
        fixture->cross_other_result = m3_wake_transport_pump(
            fixture->other_transport, nonrecursive_sink, fixture, 1);
        fixture->cross_same_result = m3_wake_transport_pump(
            fixture->transport, nonrecursive_sink, fixture, 1);
    }
    return fixture->result;
}

static int test_validation_and_disconnect(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH];
    unsigned char bad[M3_WAKE_V1_FRAME_LENGTH];
    m3_wake_transport transport;
    int failed = 0;

    memset(bad, 0, sizeof(bad));
    failed |= expect(!m3_wake_transport_init(0, storage, sizeof(storage)),
        "null transport is rejected");
    failed |= expect(!m3_wake_transport_init(&transport, storage, 15),
        "partial frame storage is rejected");
    failed |= expect(m3_wake_transport_init(&transport, storage, sizeof(storage)),
        "one-frame storage is accepted");
    failed |= expect(m3_wake_transport_enqueue(&transport, bad, sizeof(bad)) ==
        M3_WAKE_TRANSPORT_INVALID, "noncanonical frame is rejected");
    failed |= expect(m3_wake_transport_wake(&transport) ==
        M3_WAKE_TRANSPORT_DISCONNECTED, "disconnected queue rejects wake");
    m3_wake_transport_connect(&transport);
    failed |= expect(m3_wake_transport_wake(&transport) ==
        M3_WAKE_TRANSPORT_ACCEPTED, "connected queue accepts wake");
    m3_wake_transport_disconnect(&transport);
    failed |= expect(m3_wake_transport_pending(&transport) == 0,
        "disconnect clears pending frames");
    return failed;
}

static int test_backpressure_and_blocked_sink(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_BLOCKED;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    failed |= expect(m3_wake_transport_wake(&transport) == M3_WAKE_TRANSPORT_ACCEPTED,
        "first frame accepted");
    failed |= expect(m3_wake_transport_wake(&transport) == M3_WAKE_TRANSPORT_ACCEPTED,
        "second frame accepted");
    failed |= expect(m3_wake_transport_wake(&transport) == M3_WAKE_TRANSPORT_BACKPRESSURE,
        "full queue applies finite backpressure");
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 2) == 0 &&
        m3_wake_transport_pending(&transport) == 2,
        "blocked sink preserves both frames");
    failed |= expect(fixture.calls == 1 && fixture.length == M3_WAKE_V1_FRAME_LENGTH,
        "blocked sink is called once without waiting");
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 1 &&
        m3_wake_transport_pending(&transport) == 1,
        "pump honors max frame bound");
    failed |= expect(memcmp(fixture.frame, canonical, sizeof(canonical)) == 0,
        "sink receives unchanged canonical frame");
    return failed;
}

static int test_sink_disconnect_drops_queue(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_DISCONNECTED;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 2) == 0 &&
        m3_wake_transport_pending(&transport) == 0,
        "sink disconnect clears queued frames");
    failed |= expect(m3_wake_transport_wake(&transport) ==
        M3_WAKE_TRANSPORT_DISCONNECTED, "sink disconnect is terminal");
    return failed;
}

static int test_alias_enqueue_preserves_frames(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_enqueue(&transport, storage,
        M3_WAKE_V1_FRAME_LENGTH) == M3_WAKE_TRANSPORT_ACCEPTED,
        "enqueue supports a frame aliasing ring storage");
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 2) == 2 &&
        m3_wake_transport_pending(&transport) == 0 && fixture.calls == 2,
        "aliased enqueue preserves both canonical frames");
    return failed;
}

static int test_hostile_sink_cannot_mutate_pending_frame(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_BLOCKED;
    fixture.mutate = 1;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0 &&
        m3_wake_transport_pending(&transport) == 1,
        "hostile blocked sink leaves frame pending");
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 1 &&
        m3_wake_transport_pending(&transport) == 0 &&
        memcmp(fixture.frame, canonical, sizeof(canonical)) == 0,
        "sink mutation cannot corrupt queued canonical frame");
    return failed;
}

static int test_reentrant_sink_fails_closed(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    fixture.reenter = 1;
    fixture.transport = &transport;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0,
        "reentrant sink does not report a delivered frame");
    failed |= expect(fixture.calls == 1 && !transport.connected &&
        m3_wake_transport_pending(&transport) == 0,
        "reentrant sink fails closed and clears transport state");
    return failed;
}

static int test_reentrant_init_fails_closed(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    fixture.reinit = 1;
    fixture.transport = &transport;
    fixture.storage = storage;
    fixture.storage_length = sizeof(storage);
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0,
        "reentrant init does not report a delivered frame");
    failed |= expect(fixture.calls == 1 && !transport.connected &&
        m3_wake_transport_pending(&transport) == 0,
        "reentrant init fails closed and clears transport state");
    return failed;
}

static int test_reentrant_pump_fails_closed(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    fixture.repump = 1;
    fixture.transport = &transport;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0,
        "reentrant pump does not report a delivered frame");
    failed |= expect(fixture.nested_result == 0 && fixture.nested_calls == 0,
        "nested pump is rejected before invoking its sink");
    failed |= expect(fixture.calls == 1 && !transport.connected &&
        m3_wake_transport_pending(&transport) == 0,
        "reentrant pump fails closed and clears transport state");
    return failed;
}

static int test_reentrant_pump_private_guard_survives_public_tamper(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    fixture.repump = 1;
    fixture.clear_pumping = 1;
    fixture.transport = &transport;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0,
        "tampered reentrant pump does not report a delivered frame");
    failed |= expect(fixture.nested_result == 0 && fixture.nested_calls == 0,
        "private active marker blocks nested sink after public tamper");
    failed |= expect(fixture.calls == 1 && !transport.connected &&
        m3_wake_transport_pending(&transport) == 0,
        "tampered reentrant pump fails closed and clears state");
    return failed;
}

static int test_cross_transport_reentry_restores_active_marker(void)
{
    unsigned char storage_a[M3_WAKE_V1_FRAME_LENGTH * 2];
    unsigned char storage_b[M3_WAKE_V1_FRAME_LENGTH * 2];
    m3_wake_transport transport_a;
    m3_wake_transport transport_b;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    fixture.cross_reenter = 1;
    fixture.transport = &transport_a;
    fixture.other_transport = &transport_b;
    m3_wake_transport_init(&transport_a, storage_a, sizeof(storage_a));
    m3_wake_transport_init(&transport_b, storage_b, sizeof(storage_b));
    m3_wake_transport_connect(&transport_a);
    m3_wake_transport_connect(&transport_b);
    m3_wake_transport_wake(&transport_a);
    m3_wake_transport_wake(&transport_b);
    failed |= expect(m3_wake_transport_pump(&transport_a, sink, &fixture, 1) == 0,
        "cross-transport reentry does not report an A delivery");
    failed |= expect(fixture.cross_other_result == 1 &&
        fixture.cross_same_result == 0 && fixture.nested_calls == 1,
        "B can pump but A remains blocked during A callback");
    failed |= expect(!transport_a.connected &&
        m3_wake_transport_pending(&transport_a) == 0 &&
        m3_wake_transport_pending(&transport_b) == 0,
        "cross-transport reentry fails closed for A without corrupting B");
    return failed;
}

static int test_storage_and_transport_overlap_is_rejected(void)
{
    m3_wake_transport transport;
    int failed = 0;

    failed |= expect(!m3_wake_transport_init(&transport,
        (unsigned char *)&transport, M3_WAKE_V1_FRAME_LENGTH),
        "storage overlapping transport object is rejected");
    return failed;
}

static int test_storage_tamper_fails_closed_before_sink(void)
{
    unsigned char storage[M3_WAKE_V1_FRAME_LENGTH];
    m3_wake_transport transport;
    sink_fixture fixture;
    int failed = 0;

    memset(&fixture, 0, sizeof(fixture));
    fixture.result = M3_WAKE_TRANSPORT_SINK_ACCEPTED;
    m3_wake_transport_init(&transport, storage, sizeof(storage));
    m3_wake_transport_connect(&transport);
    m3_wake_transport_wake(&transport);
    storage[0] ^= 0xff;
    failed |= expect(m3_wake_transport_pump(&transport, sink, &fixture, 1) == 0,
        "tampered frame is not delivered");
    failed |= expect(fixture.calls == 0 && m3_wake_transport_pending(&transport) == 0,
        "tamper fails closed before sink and clears queue");
    failed |= expect(m3_wake_transport_wake(&transport) ==
        M3_WAKE_TRANSPORT_DISCONNECTED, "tamper disconnects transport");
    return failed;
}

int main(void)
{
    int failed = 0;
    failed |= test_validation_and_disconnect();
    failed |= test_backpressure_and_blocked_sink();
    failed |= test_sink_disconnect_drops_queue();
    failed |= test_alias_enqueue_preserves_frames();
    failed |= test_hostile_sink_cannot_mutate_pending_frame();
    failed |= test_reentrant_sink_fails_closed();
    failed |= test_reentrant_init_fails_closed();
    failed |= test_reentrant_pump_fails_closed();
    failed |= test_reentrant_pump_private_guard_survives_public_tamper();
    failed |= test_cross_transport_reentry_restores_active_marker();
    failed |= test_storage_tamper_fails_closed_before_sink();
    failed |= test_storage_and_transport_overlap_is_rejected();
    return failed;
}
