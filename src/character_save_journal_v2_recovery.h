#ifndef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_H
#define CHARACTER_SAVE_JOURNAL_V2_RECOVERY_H

#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_publish.h"

/* Test-only recovery driver.  It deliberately has no production caller and
 * accepts only a held writer capability, the existing receipt seam, and a
 * zeroed report destination. */
/* 1024 commands bounds a validated recovery snapshot while covering a
 * practical restart backlog; installations with a tighter operational bound
 * can test it explicitly without changing recovery authority. */
#define CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES 1024

typedef enum character_save_journal_v2_recovery_result {
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID = 2,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_STALE = 3,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_LOCK = 4,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_JOURNAL = 5,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE = 6,
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_NOMEM = 7,
    /* At least one command retained durable retry/freeze evidence. */
    CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE = 8
} character_save_journal_v2_recovery_result;

typedef struct character_save_journal_v2_recovery_report {
    unsigned int discovered;
    unsigned int visited;
    /* Shadow-only diagnostics.  Observer failures never make recovery
     * incomplete, fence a character, or change publish/ACK authority. */
    unsigned int snapshot_attempted;
    unsigned int snapshot_succeeded;
    unsigned int snapshot_failed;
    unsigned int publish_attempted;
    unsigned int ack_attempted;
    unsigned int publish_results[CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO + 1];
    unsigned int ack_results[CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE + 1];
} character_save_journal_v2_recovery_report;

/* Called only for a validated durable PREPARED entry and before its stage can
 * be consumed by publish.  The held writer and command UUID are capabilities;
 * no caller-provided path or player bytes cross this seam. */
typedef int (*character_save_journal_v2_prepared_stage_observer)(
    void *opaque, const character_save_journal_v2_writer_context *writer,
    const char *command_uuid);

character_save_journal_v2_recovery_result
character_save_journal_v2_recovery_run_with_stage_observer(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_receipt_callback receipt_callback,
    void *receipt_opaque,
    character_save_journal_v2_prepared_stage_observer stage_observer,
    void *stage_observer_opaque,
    character_save_journal_v2_recovery_report *report_out);

character_save_journal_v2_recovery_result
character_save_journal_v2_recovery_run(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_receipt_callback receipt_callback,
    void *receipt_opaque,
    character_save_journal_v2_recovery_report *report_out);

#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
/* The allocation limit applies to snapshot allocation attempts only. */
void character_save_journal_v2_recovery_set_entry_cap_for_test(unsigned int cap);
void character_save_journal_v2_recovery_fail_allocation_for_test(int enabled);
void character_save_journal_v2_recovery_fail_closedir_for_test(int enabled);
void character_save_journal_v2_recovery_fail_root_close_for_test(int enabled);
int character_save_journal_v2_recovery_scan_fd_cloexec_for_test(void);
#endif

#endif
