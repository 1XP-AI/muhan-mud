#ifndef CHARACTER_SAVE_JOURNAL_V2_BOOTSTRAP_H
#define CHARACTER_SAVE_JOURNAL_V2_BOOTSTRAP_H

/* First-save absent-head bootstrap is deliberately upstream of serializer and
 * save_held_v3.  It only reads through the held root until its fixed seed RPC
 * has succeeded and an exact absent/revision-zero route has been rebound. */
#include "character_save_journal_v2_live_ops.h"

int character_save_journal_v2_bootstrap_absent_head(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_live_ops *live_ops,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length);

#endif
