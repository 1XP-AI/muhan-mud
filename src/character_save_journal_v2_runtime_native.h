#ifndef CHARACTER_SAVE_JOURNAL_V2_RUNTIME_NATIVE_H
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_NATIVE_H

#include "character_save_journal_v2_runtime.h"

#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
#include "character_save_journal_v2_deadline_native.h"
#include "character_save_journal_v2_process_owner.h"
#include "character_save_journal_v2_rpc_transport_native.h"
#include "character_player_snapshot_v1_capture_native.h"
#include "character_player_snapshot_v1_read_rehearsal.h"
#endif

/* This is the only M3 runtime unit that includes or calls libpq.  The live
 * executable includes it only when USE_M3_RUNTIME=1; no save path owns it. */
typedef struct character_save_journal_v2_runtime_native {
    character_save_journal_v2_runtime_dependencies dependencies;
#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
    character_save_journal_v2_rpc_transport_native transport_native;
    character_save_journal_v2_deadline_native deadline_native;
    character_save_journal_v2_process_owner process_owner;
    /* process_owner retains these pointers for the complete writer lifetime;
     * the generic runtime's root is stack storage and getenv is borrowed. */
    char muhan_home[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PATH_MAX];
    char world_id[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX+1];
    char *serializer_buffer;
    unsigned long serializer_buffer_capacity;
    /* Native owner storage for the optional durable snapshot observer. */
    character_player_snapshot_v1_capture snapshot_capture;
    character_player_snapshot_v1_handoff snapshot_handoff;
    int snapshot_handoff_enabled;
    /* Native runtime owns this one private descriptor.  The activation gate
     * only borrows it while dispatching an explicit capability. */
    int activation_reservation_directory_fd;
    /* A separate read-only descriptor backs an exact, opt-in rehearsal.
     * It is never a directory scan, capture consumer, or write capability. */
    int read_rehearsal_artifact_directory_fd;
    character_player_snapshot_v1_artifact_metadata read_rehearsal_artifact;
    character_player_snapshot_v1_read_rehearsal_result read_rehearsal_last_result;
    int read_rehearsal_armed;
    int shadow_active;
    /* The MUD host owns these injected seams and invokes the bounded drain
     * only from its serialized idle-turn boundary. */
    long (*snapshot_idle_clock)(void *opaque);
    void *snapshot_idle_clock_opaque;
    void (*snapshot_idle_diagnostic)(void *opaque, const char *message);
    void *snapshot_idle_diagnostic_opaque;
    long snapshot_idle_next_at;
    long snapshot_idle_last_clock_at;
    long snapshot_idle_last_failure_log_at;
    int snapshot_idle_clock_seen;
    int snapshot_idle_cadence_exhausted;
    int snapshot_idle_failure_logged;
#endif
} character_save_journal_v2_runtime_native;

/* Initialize fresh storage for a native owner lifetime.  Teardown is performed
 * through the paired generic runtime shutdown; an accidental repeated init of
 * the active owner is a non-destructive no-op. */
void character_save_journal_v2_runtime_native_init(
    character_save_journal_v2_runtime_native *native);

#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
/* Explicit host lifecycle boundary for the opt-in handoff consumer.  Native
 * startup, recovery, and PlayerStore save intentionally never call this. */
character_save_journal_v2_process_owner_snapshot_tick_result
character_save_journal_v2_runtime_native_snapshot_tick(
    character_save_journal_v2_runtime_native *native, unsigned int limit);

/* Returns the native caller-owned reservation directory only for a live,
 * explicitly enabled shadow runtime; it never duplicates or transfers it. */
int character_save_journal_v2_runtime_native_activation_reservation_directory_fd(
    const character_save_journal_v2_runtime_native *native);

/* The caller supplies clock/log seams so cadence and diagnostics remain
 * deterministic in tests.  A configured idle tick consumes one token at most
 * once per second, never participates in a synchronous save, and treats
 * OFF/BUSY/NOT_READY as silent no-ops. */
void character_save_journal_v2_runtime_native_snapshot_idle_configure(
    character_save_journal_v2_runtime_native *native,
    long (*clock)(void *opaque), void *clock_opaque,
    void (*diagnostic)(void *opaque, const char *message),
    void *diagnostic_opaque);
void character_save_journal_v2_runtime_native_snapshot_idle_tick(
    character_save_journal_v2_runtime_native *native);
#endif

#endif
