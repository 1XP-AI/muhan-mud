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
    int (*save)(char *name, struct creature *player);
    int (*load)(char *name, struct creature **player);
} player_store_ops;

int player_store_set(const player_store_ops *ops);
void player_store_reset(void);

int save_ply(char *name, struct creature *player);
int load_ply(char *name, struct creature **player);

int file_player_store_save(char *name, struct creature *player);
int file_player_store_load(char *name, struct creature **player);

#endif
