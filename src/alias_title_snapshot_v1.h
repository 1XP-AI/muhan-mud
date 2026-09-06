#ifndef MUHAN_ALIAS_TITLE_SNAPSHOT_V1_H
#define MUHAN_ALIAS_TITLE_SNAPSHOT_V1_H

#include <stddef.h>
#include <stdint.h>

/* Bounded canonical representation of the legacy alias/title sidecar for an
 * explicitly compiled test seam.  It has no production save/load route,
 * persistence contract, or text semantics of its own. */
#define ALIAS_TITLE_SNAPSHOT_V1_SCHEMA 1U
#define ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES 50U
#define ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES 13U
#define ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES 253U
#define ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES 78U

typedef struct alias_title_snapshot_v1_alias {
    uint8_t alias_length;
    uint8_t alias[ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES];
    uint16_t process_length;
    uint8_t process[ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES];
} alias_title_snapshot_v1_alias;

typedef struct alias_title_snapshot_v1 {
    uint16_t alias_count;
    alias_title_snapshot_v1_alias aliases[ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES];
    uint8_t title_present;
    uint8_t title_length;
    uint8_t title[ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES];
} alias_title_snapshot_v1;

/* Canonical fields are schema=1, ordered alias sequence, title-present, and
 * title bytes. Successful wires carry the CDTO v1 SHA-256 digest. */
int alias_title_snapshot_v1_encode(const alias_title_snapshot_v1 *, uint8_t **,
    size_t *);
int alias_title_snapshot_v1_decode(const uint8_t *, size_t,
    alias_title_snapshot_v1 *);

#endif
