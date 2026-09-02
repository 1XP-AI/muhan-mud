#ifndef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_H
#define CHARACTER_SAVE_JOURNAL_V2_PUBLISH_H

#include "character_save_journal_v2.h"
#include "character_save_journal_v2_route.h"

/* Test-only local publish boundary.  It has no DB callback and never enters
 * the production MUD object list.  The route callback is invoked internally
 * against the exact held writer; no caller-provided route bytes or root path
 * can select a file or identity. */
typedef enum character_save_journal_v2_publish_result {
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_INVALID = 2,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_STALE = 3,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_LOCK = 4,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY = 5,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL = 6,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_STAGE = 7,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE = 8,
    CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO = 9
} character_save_journal_v2_publish_result;

/* No caller path, route tuple, or payload is accepted.  The only bytes that
 * may become the live legacy leaf are the previously fsynced, derived stage
 * leaf named by the canonical PREPARED journal. */
character_save_journal_v2_publish_result character_save_journal_v2_publish(
    const character_save_journal_v2_writer_context *writer,
    const unsigned char *canonical_legacy_name,
    size_t canonical_legacy_name_length,
    character_save_journal_v2_route_lookup lookup, void *lookup_opaque,
    const char *command_uuid);

/* v3 repeats the authoritative head-aware route lookup after immutable
 * PREPARED evidence is reread and immediately before any local promotion.
 * The route must prove the PREPARED identity, expected preimage, and the
 * predecessor revision; neither caller input nor reply fields select files. */
character_save_journal_v2_publish_result character_save_journal_v2_publish_v3(
    const character_save_journal_v2_writer_context *writer,
    const unsigned char *canonical_legacy_name,
    size_t canonical_legacy_name_length,
    character_save_journal_v2_route_lookup_v3 lookup, void *lookup_opaque,
    const char *command_uuid);

/* Route-free, test-only recovery of one immutable PREPARED journal.  The
 * held writer supplies the only root capability; journal identity supplies
 * the canonical name, shard, expected preimage and staged payload.  No path,
 * payload, route callback, or route bytes are accepted. */
character_save_journal_v2_publish_result
character_save_journal_v2_publish_recover(
    const character_save_journal_v2_writer_context *writer,
    const char *command_uuid);

#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
#include <sys/types.h>
void character_save_journal_v2_publish_set_trusted_uid_for_test(uid_t uid);
void character_save_journal_v2_publish_faults_for_test(
    int write_eintr_once, int write_short_once, int write_zero_once,
    int write_eio_once, int journal_file_fsync_once,
    int live_parent_fsync_once, int live_promotion_once, int journal_promotion_once,
    int journal_parent_fsync_once, int read_close_once,
    int stage_close_once, int live_close_once, int journal_write_close_once);
void character_save_journal_v2_publish_operation_faults_for_test(
    int destination_parent_fsync_once, int stage_parent_fsync_once,
    int stage_unlink_once, int journal_temp_unlink_once);
void character_save_journal_v2_publish_crash_after_live_promotion_for_test(int enabled);
void character_save_journal_v2_publish_pause_before_absent_link_for_test(
    int ready_fd, int release_fd);
void character_save_journal_v2_publish_pause_before_marker_link_for_test(
    int ready_fd, int release_fd);
void character_save_journal_v2_publish_reset_cleanup_close_failures_for_test(void);
unsigned int character_save_journal_v2_publish_cleanup_close_failures_for_test(void);
#endif

#endif
