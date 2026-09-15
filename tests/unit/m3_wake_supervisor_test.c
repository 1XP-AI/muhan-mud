/*
 * TDD record:
 * RED: this contract is intentionally compiled before its supervisor exists.
 * GREEN: cc -std=gnu89 -fcommon -Wall -Wextra -Werror -I src
 *   tests/unit/m3_wake_supervisor_test.c src/m3_wake_supervisor.c
 *   src/m3_wake_v1.c -o /tmp/muhan-unit/m3_wake_supervisor_test
 */
#include "m3_wake_supervisor.h"

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>

static const unsigned char canonical[M3_WAKE_V1_FRAME_LENGTH] = {
    0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00,
    0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00
};

typedef struct fixture {
    int claim_calls, send_calls, error_calls, reap_calls, release_calls;
    long next_owner, last_owner, send_result;
    int send_error, reap_result;
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];
    size_t frame_length;
} fixture;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "m3_wake_supervisor_test: %s\n", message);
    return 1;
}

static long fake_claim(void *opaque)
{
    fixture *test = (fixture *)opaque;
    test->claim_calls++;
    return test->next_owner;
}

static long fake_send(void *opaque, long owner, const unsigned char *frame,
    size_t frame_length)
{
    fixture *test = (fixture *)opaque;
    test->send_calls++;
    test->last_owner = owner;
    test->frame_length = frame_length;
    if(frame_length <= sizeof(test->frame))
        memcpy(test->frame, frame, frame_length);
    return test->send_result;
}

static int fake_last_error(void *opaque)
{
    fixture *test = (fixture *)opaque;
    test->error_calls++;
    return test->send_error;
}

static int fake_reap(void *opaque, long owner)
{
    fixture *test = (fixture *)opaque;
    test->reap_calls++;
    test->last_owner = owner;
    return test->reap_result;
}

static void fake_release(void *opaque, long owner)
{
    fixture *test = (fixture *)opaque;
    test->release_calls++;
    test->last_owner = owner;
}

static const m3_wake_supervisor_operations fake_operations = {
    fake_claim, fake_send, fake_last_error, fake_reap, fake_release
};

static const m3_wake_supervisor_operations fake_operations_without_error = {
    fake_claim, fake_send, 0, fake_reap, fake_release
};

static const m3_wake_supervisor_operations fake_operations_without_claim = {
    0, fake_send, fake_last_error, fake_reap, fake_release
};

static const m3_wake_supervisor_operations fake_operations_without_send = {
    fake_claim, 0, fake_last_error, fake_reap, fake_release
};

static const m3_wake_supervisor_operations fake_operations_without_reap = {
    fake_claim, fake_send, fake_last_error, 0, fake_release
};

static const m3_wake_supervisor_operations fake_operations_without_release = {
    fake_claim, fake_send, fake_last_error, fake_reap, 0
};

static void fixture_init_with_operations(fixture *test,
    m3_wake_supervisor *supervisor, m3_wake_supervisor_mode mode,
    const m3_wake_supervisor_operations *operations, unsigned long backoff)
{
    memset(test, 0, sizeof(*test));
    test->next_owner = 7;
    test->send_result = M3_WAKE_V1_FRAME_LENGTH;
    m3_wake_supervisor_init(supervisor, mode, operations, test, backoff);
}

static void fixture_init(fixture *test, m3_wake_supervisor *supervisor,
    m3_wake_supervisor_mode mode)
{
    fixture_init_with_operations(test, supervisor, mode, &fake_operations, 5);
}

static int test_off_never_claims_or_sends(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_OFF);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_WAKE_OFF, "OFF wake reports OFF");
    failed |= expect(m3_wake_supervisor_reap(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_REAP_OFF, "OFF reap reports OFF");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.claim_calls == 0 && test.send_calls == 0 &&
        test.reap_calls == 0 && test.release_calls == 0,
        "OFF mode never invokes an injected operation");
    return failed;
}

static int test_wake_uses_exact_canonical_packet(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_WAKE_SENT, "owned helper receives a wake");
    failed |= expect(test.claim_calls == 1 && test.send_calls == 1 &&
        test.last_owner == 7, "one injected owner is claimed and signalled");
    failed |= expect(test.frame_length == sizeof(canonical) &&
        memcmp(test.frame, canonical, sizeof(canonical)) == 0,
        "wake packet is exactly m3_wake_v1 canonical bytes");
    return failed;
}

static int test_negative_claim_is_unavailable(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    test.next_owner = -7;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 40) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED,
        "negative claim is unavailable");
    failed |= expect(test.claim_calls == 1 && test.send_calls == 0 &&
        test.release_calls == 0 && !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 45,
        "negative claim sends and releases nothing, then backs off exactly");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 44) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "negative claim backoff is deterministic");
    return failed;
}

static int test_zero_or_missing_claim_backs_off(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    test.next_owner = 0;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 100) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED && test.claim_calls == 1 &&
        test.send_calls == 0 && test.release_calls == 0 &&
        m3_wake_supervisor_retry_at(&supervisor) == 105,
        "zero claim is unavailable with an exact retry deadline");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 104) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "zero claim honors the exact retry deadline");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 105) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED && test.claim_calls == 2 &&
        m3_wake_supervisor_retry_at(&supervisor) == 110,
        "zero claim can retry at the exact deadline");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 0,
        "idle shutdown after an unavailable claim releases nothing");

    fixture_init_with_operations(&test, &supervisor, M3_WAKE_SUPERVISOR_ON,
        &fake_operations_without_claim, 10);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, ULONG_MAX - 4) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED && test.claim_calls == 0 &&
        test.send_calls == 0 && test.release_calls == 0 &&
        m3_wake_supervisor_retry_at(&supervisor) == ULONG_MAX,
        "missing claim callback saturates the retry deadline");
    return failed;
}

static int test_missing_send_or_release_callback_drops_owner(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init_with_operations(&test, &supervisor, M3_WAKE_SUPERVISOR_ON,
        &fake_operations_without_send, 5);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED && test.claim_calls == 1 &&
        test.send_calls == 0 && test.release_calls == 1 &&
        !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 15,
        "missing send callback releases a claimed token and backs off");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 1,
        "idle shutdown after missing send does not release twice");

    fixture_init_with_operations(&test, &supervisor, M3_WAKE_SUPERVISOR_ON,
        &fake_operations_without_release, 5);
    test.send_result = 0;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 20) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED && test.send_calls == 1 &&
        test.release_calls == 0 && !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 25,
        "absent release callback still drops the token and backs off");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 21) ==
        M3_WAKE_SUPERVISOR_WAKE_OFF && test.release_calls == 0,
        "shutdown remains terminal without a release callback");
    return failed;
}

static int test_reap_nonpositive_or_missing_callback_keeps_owner(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    (void)m3_wake_supervisor_wake(&supervisor, 10);
    test.reap_result = 0;
    failed |= expect(m3_wake_supervisor_reap(&supervisor, 11) ==
        M3_WAKE_SUPERVISOR_REAP_NONE && test.reap_calls == 1 &&
        test.release_calls == 0 && m3_wake_supervisor_has_owner(&supervisor),
        "zero reap result keeps the token");
    test.reap_result = -1;
    failed |= expect(m3_wake_supervisor_reap(&supervisor, 12) ==
        M3_WAKE_SUPERVISOR_REAP_NONE && test.reap_calls == 2 &&
        test.release_calls == 0 && m3_wake_supervisor_has_owner(&supervisor),
        "negative reap result keeps the token");
    m3_wake_supervisor_shutdown(&supervisor);

    fixture_init_with_operations(&test, &supervisor, M3_WAKE_SUPERVISOR_ON,
        &fake_operations_without_reap, 5);
    (void)m3_wake_supervisor_wake(&supervisor, 20);
    failed |= expect(m3_wake_supervisor_reap(&supervisor, 21) ==
        M3_WAKE_SUPERVISOR_REAP_NONE && test.reap_calls == 0 &&
        test.release_calls == 0 && m3_wake_supervisor_has_owner(&supervisor),
        "missing reap callback keeps the token");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 1,
        "shutdown releases a token that could not be reaped");
    return failed;
}

static int test_transient_loss_is_deliberately_dropped(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    test.send_result = -1;
    test.send_error = EAGAIN;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED, "EAGAIN drops a wake");
    failed |= expect(test.error_calls == 1 &&
        m3_wake_supervisor_has_owner(&supervisor),
        "EAGAIN neither blocks nor abandons the owner");

    test.send_error = EWOULDBLOCK;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 11) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED, "EWOULDBLOCK drops a wake");
    failed |= expect(test.error_calls == 2 && test.claim_calls == 1 &&
        test.release_calls == 0 && m3_wake_supervisor_has_owner(&supervisor),
        "EWOULDBLOCK preserves the existing owner");
    return failed;
}

static int test_unrecoverable_send_loss_releases_and_backs_off(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    test.send_result = M3_WAKE_V1_FRAME_LENGTH - 1;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 11) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED, "short write drops a wake");
    failed |= expect(test.error_calls == 0 && test.release_calls == 1 &&
        !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 16,
        "short write releases the owner and schedules the configured backoff");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 15) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "short write cannot immediately claim a replacement");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 1,
        "short-write release is not repeated during shutdown");

    fixture_init_with_operations(&test, &supervisor, M3_WAKE_SUPERVISOR_ON,
        &fake_operations_without_error, 5);
    test.send_result = -1;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 20) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED,
        "unclassified send failure drops a wake");
    failed |= expect(test.error_calls == 0 && test.release_calls == 1 &&
        !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 25,
        "missing error classification releases and backs off");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 24) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "unclassified failure cannot immediately claim a replacement");

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    test.send_result = -1;
    test.send_error = EPIPE;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 30) ==
        M3_WAKE_SUPERVISOR_WAKE_DROPPED, "send fault drops a wake");
    failed |= expect(test.error_calls == 1 && test.release_calls == 1 &&
        !m3_wake_supervisor_has_owner(&supervisor) &&
        m3_wake_supervisor_retry_at(&supervisor) == 35,
        "fatal send fault releases and backs off");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 34) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "fatal failure cannot immediately claim a replacement");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 1,
        "fatal-send release is not repeated during shutdown");
    return failed;
}

static int test_one_owner_reap_and_backoff(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 10) ==
        M3_WAKE_SUPERVISOR_WAKE_SENT, "initial wake claims owner");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 11) ==
        M3_WAKE_SUPERVISOR_WAKE_SENT && test.claim_calls == 1,
        "live owner is never claimed twice");
    test.reap_result = 1;
    failed |= expect(m3_wake_supervisor_reap(&supervisor, 12) ==
        M3_WAKE_SUPERVISOR_REAPED, "reaped owner is recorded");
    failed |= expect(!m3_wake_supervisor_has_owner(&supervisor) &&
        test.release_calls == 1 && m3_wake_supervisor_retry_at(&supervisor) == 17,
        "reap releases once and sets the configured backoff deadline");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 16) ==
        M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF && test.claim_calls == 1,
        "backoff prevents a premature replacement");
    test.next_owner = 8;
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 17) ==
        M3_WAKE_SUPERVISOR_WAKE_SENT && test.claim_calls == 2 &&
        m3_wake_supervisor_has_owner(&supervisor),
        "replacement can be claimed after the deadline");
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 2,
        "the reaped owner is not released again during shutdown");
    return failed;
}

static int test_shutdown_is_idempotent_and_nonblocking(void)
{
    fixture test;
    m3_wake_supervisor supervisor;
    int failed = 0;

    fixture_init(&test, &supervisor, M3_WAKE_SUPERVISOR_ON);
    (void)m3_wake_supervisor_wake(&supervisor, 10);
    m3_wake_supervisor_shutdown(&supervisor);
    m3_wake_supervisor_shutdown(&supervisor);
    failed |= expect(test.release_calls == 1 && test.reap_calls == 0,
        "shutdown releases once without waiting for a reap");
    failed |= expect(m3_wake_supervisor_wake(&supervisor, 11) ==
        M3_WAKE_SUPERVISOR_WAKE_OFF && !m3_wake_supervisor_has_owner(&supervisor),
        "shutdown is terminal and idempotent");
    return failed;
}

int main(void)
{
    int failed = 0;
    failed |= test_off_never_claims_or_sends();
    failed |= test_wake_uses_exact_canonical_packet();
    failed |= test_negative_claim_is_unavailable();
    failed |= test_zero_or_missing_claim_backs_off();
    failed |= test_missing_send_or_release_callback_drops_owner();
    failed |= test_reap_nonpositive_or_missing_callback_keeps_owner();
    failed |= test_transient_loss_is_deliberately_dropped();
    failed |= test_unrecoverable_send_loss_releases_and_backs_off();
    failed |= test_one_owner_reap_and_backoff();
    failed |= test_shutdown_is_idempotent_and_nonblocking();
    return failed;
}
