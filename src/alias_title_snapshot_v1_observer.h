#ifndef MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OBSERVER_H
#define MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OBSERVER_H

#include <stddef.h>
#include <stdint.h>

/* A deliberately inert, in-process notification seam.  The bytes are a
 * borrowed canonical AliasTitleSnapshotV1 CDTO envelope (including its
 * digest) and are valid only for the callback.  Registration does not make
 * alias saves durable, atomic, receipted, or connected to another service. */
typedef int (*alias_title_snapshot_v1_observer_fn)(const uint8_t *wire,
    size_t wire_length, void *context);

/* No observer is registered by default.  The callback return value is always
 * ignored so a consumer cannot alter legacy alias/title gameplay behavior. */
void alias_title_snapshot_v1_observer_register(
    alias_title_snapshot_v1_observer_fn observer, void *context);
void alias_title_snapshot_v1_observer_clear(void);

#endif
