#ifndef MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OBSERVER_H
#define MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OBSERVER_H

#include <stddef.h>
#include <stdint.h>

/* This observer is an explicitly compiled test seam, never a production
 * alias-save API.  The bytes are borrowed only for the callback. */
#ifdef ALIAS_TITLE_SNAPSHOT_V1_TEST_SEAM
typedef int (*alias_title_snapshot_v1_observer_fn)(const uint8_t *wire,
    size_t wire_length, void *context);

/* The callback result is ignored and cannot alter legacy save behavior. */
void alias_title_snapshot_v1_observer_register(
    alias_title_snapshot_v1_observer_fn observer, void *context);
void alias_title_snapshot_v1_observer_clear(void);
#endif

#endif
