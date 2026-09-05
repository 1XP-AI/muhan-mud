#ifndef PLAYER_STORE_H
#define PLAYER_STORE_H

struct creature;

typedef enum player_store_result {
    PLAYER_STORE_OK = 0,
    PLAYER_STORE_NOT_FOUND = -1,
    PLAYER_STORE_CORRUPT = -2,
    PLAYER_STORE_IO_ERROR = -3
} player_store_result;

typedef struct player_store_ops {
    int (*save)(void *opaque, char *name, struct creature *player);
    int (*load)(void *opaque, char *name, struct creature **player);
    void *opaque;
} player_store_ops;

/* A managed binding preserves the previously active store and restores it
 * only while this exact binding still owns the global facade.  Callers must
 * zero-initialize the binding and keep it alive until unbind. */
typedef struct player_store_binding {
    player_store_ops previous;
    int active;
} player_store_binding;

typedef enum player_store_unbind_result {
    PLAYER_STORE_UNBIND_RESTORED = 0,
    PLAYER_STORE_UNBIND_NOT_CURRENT = 1,
    PLAYER_STORE_UNBIND_INVALID = -1
} player_store_unbind_result;

int player_store_set(const player_store_ops *ops);
void player_store_reset(void);
int player_store_bind(const player_store_ops *ops,
    player_store_binding *binding);
player_store_unbind_result player_store_unbind(
    player_store_binding *binding);

/* Load through the immutable legacy FileStore without consulting the active
 * repository.  This is the recursion-safe fallback seam for composed stores. */
int player_store_default_load(char *name, struct creature **player);

int save_ply(char *name, struct creature *player);
int load_ply(char *name, struct creature **player);

int file_player_store_save(char *name, struct creature *player);
int file_player_store_load(char *name, struct creature **player);
/* Metadata-only FileStore inspection.  It never exposes the descriptor or a
 * decoded creature, and writes a digest only after the same opened file has
 * passed the legacy decoder unchanged. */
int file_player_store_inspect(char *name, char out_sha256[65]);

#endif
