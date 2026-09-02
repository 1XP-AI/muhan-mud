#ifndef CHARACTER_SAVE_JOURNAL_V2_RUNTIME_H
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_H

#include <stddef.h>
#include <sys/stat.h>

/* This module is deliberately an unattached startup probe.  It never starts
 * a writer, publishes a save, or calls live player storage. */
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX 4096
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX 64
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PATH_MAX 1024
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_TUPLES_OK 1

typedef enum character_save_journal_v2_runtime_state {
    CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED=0,
    CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY=1,
    CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED=2
} character_save_journal_v2_runtime_state;

/* The result must describe exactly one boolean field.  No connection string
 * or result error text crosses this boundary into the runtime outcome. */
typedef struct character_save_journal_v2_runtime_database_operations {
    void *(*connect)(void *opaque, const char *conninfo);
    int (*connection_ok)(void *connection);
    void *(*exec)(void *connection, const char *sql);
    int (*result_status)(void *result);
    int (*result_rows)(void *result);
    int (*result_columns)(void *result);
    const char *(*result_value)(void *result, int row, int column);
    int (*result_value_length)(void *result, int row, int column);
    const char *(*result_sqlstate)(void *result);
    void (*result_clear)(void *result);
    void (*connection_finish)(void *connection);
} character_save_journal_v2_runtime_database_operations;

/* Custom file operations are intended for deterministic tests or platform
 * ports.  open_readonly_nofollow must have O_NOFOLLOW-equivalent semantics. */
typedef struct character_save_journal_v2_runtime_file_operations {
    int (*open_readonly_nofollow)(void *opaque, const char *absolute_path);
    int (*descriptor_stat)(void *opaque, int descriptor, struct stat *status);
    long (*read_bytes)(void *opaque, int descriptor, void *buffer, size_t length);
    int (*close_descriptor)(void *opaque, int descriptor);
} character_save_journal_v2_runtime_file_operations;

typedef struct character_save_journal_v2_runtime_dependencies {
    const char *(*environment_get)(void *opaque, const char *name);
    void *environment_opaque;
    const character_save_journal_v2_runtime_database_operations *database_operations;
    void *database_opaque;
    const character_save_journal_v2_runtime_file_operations *file_operations;
    void *file_opaque;
} character_save_journal_v2_runtime_dependencies;

typedef struct character_save_journal_v2_runtime {
    character_save_journal_v2_runtime_dependencies dependencies;
    int has_dependencies;
    character_save_journal_v2_runtime_state current_state;
    size_t conninfo_length;
    char conninfo[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX+1];
} character_save_journal_v2_runtime;

void character_save_journal_v2_runtime_init(
    character_save_journal_v2_runtime *runtime,
    const character_save_journal_v2_runtime_dependencies *dependencies);

/* MUD_M3_MODE: absent/off disables; probe performs the isolated assertion;
 * every other value fails closed. */
character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_start(character_save_journal_v2_runtime *runtime);

void character_save_journal_v2_runtime_shutdown(character_save_journal_v2_runtime *runtime);
character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_get_state(const character_save_journal_v2_runtime *runtime);

#endif
