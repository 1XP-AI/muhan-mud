#include "m3_wake_supervisor.h"

#include <errno.h>
#include <limits.h>
#include <string.h>

static void m3_wake_supervisor_schedule_retry(m3_wake_supervisor *supervisor,
    unsigned long now)
{
    if(supervisor->backoff > ULONG_MAX - now)
        supervisor->retry_at = ULONG_MAX;
    else
        supervisor->retry_at = now + supervisor->backoff;
}

static void m3_wake_supervisor_release_and_retry(
    m3_wake_supervisor *supervisor, unsigned long now)
{
    long owner;

    owner = supervisor->owner;
    if(owner && supervisor->operations &&
       supervisor->operations->release_owner)
        supervisor->operations->release_owner(supervisor->opaque, owner);
    supervisor->owner = 0;
    m3_wake_supervisor_schedule_retry(supervisor, now);
}

void m3_wake_supervisor_init(m3_wake_supervisor *supervisor,
    m3_wake_supervisor_mode mode,
    const m3_wake_supervisor_operations *operations, void *opaque,
    unsigned long backoff)
{
    if(!supervisor) return;
    memset(supervisor, 0, sizeof(*supervisor));
    supervisor->mode = mode;
    supervisor->operations = operations;
    supervisor->opaque = opaque;
    supervisor->backoff = backoff;
}

m3_wake_supervisor_wake_result m3_wake_supervisor_wake(
    m3_wake_supervisor *supervisor, unsigned long now)
{
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];
    long sent;
    int send_error;

    if(!supervisor || supervisor->mode != M3_WAKE_SUPERVISOR_ON)
        return M3_WAKE_SUPERVISOR_WAKE_OFF;
    if(!supervisor->owner) {
        if(now < supervisor->retry_at)
            return M3_WAKE_SUPERVISOR_WAKE_BACKING_OFF;
        if(!supervisor->operations || !supervisor->operations->claim_owner) {
            m3_wake_supervisor_schedule_retry(supervisor, now);
            return M3_WAKE_SUPERVISOR_WAKE_DROPPED;
        }
        supervisor->owner = supervisor->operations->claim_owner(supervisor->opaque);
        if(!supervisor->owner) {
            m3_wake_supervisor_schedule_retry(supervisor, now);
            return M3_WAKE_SUPERVISOR_WAKE_DROPPED;
        }
    }
    if(!supervisor->operations || !supervisor->operations->send_frame ||
       m3_wake_v1_encode(frame, sizeof(frame)) != M3_WAKE_V1_OK) {
        m3_wake_supervisor_release_and_retry(supervisor, now);
        return M3_WAKE_SUPERVISOR_WAKE_DROPPED;
    }
    sent = supervisor->operations->send_frame(supervisor->opaque,
        supervisor->owner, frame, sizeof(frame));
    if(sent == (long)sizeof(frame))
        return M3_WAKE_SUPERVISOR_WAKE_SENT;
    if(sent < 0 && supervisor->operations->last_error) {
        send_error = supervisor->operations->last_error(supervisor->opaque);
        if(send_error == EAGAIN || send_error == EWOULDBLOCK)
            return M3_WAKE_SUPERVISOR_WAKE_DROPPED;
    }
    m3_wake_supervisor_release_and_retry(supervisor, now);
    return M3_WAKE_SUPERVISOR_WAKE_DROPPED;
}

m3_wake_supervisor_reap_result m3_wake_supervisor_reap(
    m3_wake_supervisor *supervisor, unsigned long now)
{
    int reaped;

    if(!supervisor || supervisor->mode != M3_WAKE_SUPERVISOR_ON)
        return M3_WAKE_SUPERVISOR_REAP_OFF;
    if(!supervisor->owner || !supervisor->operations ||
       !supervisor->operations->reap_owner)
        return M3_WAKE_SUPERVISOR_REAP_NONE;
    reaped = supervisor->operations->reap_owner(supervisor->opaque,
        supervisor->owner);
    if(reaped <= 0) return M3_WAKE_SUPERVISOR_REAP_NONE;
    m3_wake_supervisor_release_and_retry(supervisor, now);
    return M3_WAKE_SUPERVISOR_REAPED;
}

void m3_wake_supervisor_shutdown(m3_wake_supervisor *supervisor)
{
    if(!supervisor || supervisor->mode == M3_WAKE_SUPERVISOR_OFF) return;
    if(supervisor->owner && supervisor->operations &&
       supervisor->operations->release_owner)
        supervisor->operations->release_owner(supervisor->opaque,
            supervisor->owner);
    supervisor->owner = 0;
    supervisor->retry_at = 0;
    supervisor->mode = M3_WAKE_SUPERVISOR_OFF;
}

int m3_wake_supervisor_has_owner(const m3_wake_supervisor *supervisor)
{
    return supervisor && supervisor->owner != 0;
}

unsigned long m3_wake_supervisor_retry_at(const m3_wake_supervisor *supervisor)
{
    return supervisor ? supervisor->retry_at : 0;
}
