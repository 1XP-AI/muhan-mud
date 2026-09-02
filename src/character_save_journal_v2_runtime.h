#ifndef CHARACTER_SAVE_JOURNAL_V2_RUNTIME_H
#define CHARACTER_SAVE_JOURNAL_V2_RUNTIME_H

#include <stddef.h>
#include <sys/stat.h>

/* Opt-in M3 process bootstrap.  Probe mode performs only the writer-session
 * assertion; shadow mode delegates construction of the live PlayerStore and
 * its process-owned writer to the native lifecycle boundary. */
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

/* Shadow startup owns no credential bytes beyond the duration of this call.
 * The implementation must take the supplied conninfo only to establish its
 * one connection, then retain only the resources needed by the live writer.
 * shutdown is required to be idempotent and to release every partially
 * constructed resource in reverse order. */
typedef struct character_save_journal_v2_runtime_shadow_operations {
    int (*start)(void *opaque, const char *muhan_home, const char *world_id,
                 const char *conninfo);
    void (*shutdown)(void *opaque);
} character_save_journal_v2_runtime_shadow_operations;

typedef struct character_save_journal_v2_runtime_dependencies {
    const char *(*environment_get)(void *opaque, const char *name);
    void *environment_opaque;
    const character_save_journal_v2_runtime_database_operations *database_operations;
    void *database_opaque;
    const character_save_journal_v2_runtime_file_operations *file_operations;
    void *file_opaque;
    const character_save_journal_v2_runtime_shadow_operations *shadow_operations;
    void *shadow_opaque;
} character_save_journal_v2_runtime_dependencies;

typedef struct character_save_journal_v2_runtime {
    character_save_journal_v2_runtime_dependencies dependencies;
    int has_dependencies;
    character_save_journal_v2_runtime_state current_state;
    size_t conninfo_length;
    int shadow_active;
    char conninfo[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX+1];
} character_save_journal_v2_runtime;

/* Initialize fresh storage, or storage whose prior runtime was shut down.
 * An accidental call for the active shadow owner is a non-destructive no-op;
 * callers use start for a deliberate restart because start shuts down first. */
void character_save_journal_v2_runtime_init(
    character_save_journal_v2_runtime *runtime,
    const character_save_journal_v2_runtime_dependencies *dependencies);

/* MUD_M3_MODE: absent/off disables; probe performs the isolated assertion;
 * shadow requires explicit MUHAN_HOME, world id, and conninfo path before it
 * builds the opt-in live writer.  Every other value fails closed. */
character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_start(character_save_journal_v2_runtime *runtime);

void character_save_journal_v2_runtime_shutdown(character_save_journal_v2_runtime *runtime);
character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_get_state(const character_save_journal_v2_runtime *runtime);

#endif
