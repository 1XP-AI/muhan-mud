#ifndef BANK_STORE_H
#define BANK_STORE_H

struct object;

/* The legacy bank backend reports only success (0) or failure (-1).  In
 * particular, read_obj does not provide enough information here to classify
 * a failed decode as corruption rather than a general read failure. */
typedef enum bank_store_result {
    BANK_STORE_OK = 0,
    BANK_STORE_ERROR = -1
} bank_store_result;

typedef struct bank_store_ops {
    int (*save)(void *opaque, char *name, struct object *object);
    int (*load)(void *opaque, char *name, struct object **object);
    void *opaque;
} bank_store_ops;

/* A binding owns the facade until unbound or explicitly superseded.  It must
 * be zero-initialized and remain alive for the lifetime of that binding. */
typedef struct bank_store_binding {
    bank_store_ops previous;
    int active;
} bank_store_binding;

typedef enum bank_store_unbind_result {
    BANK_STORE_UNBIND_RESTORED = 0,
    BANK_STORE_UNBIND_NOT_CURRENT = 1,
    BANK_STORE_UNBIND_INVALID = -1
} bank_store_unbind_result;

int bank_store_set(const bank_store_ops *ops);
void bank_store_reset(void);
int bank_store_bind(const bank_store_ops *ops, bank_store_binding *binding);
bank_store_unbind_result bank_store_unbind(bank_store_binding *binding);

int bank_store_save(char *name, struct object *object);
int bank_store_load(char *name, struct object **object);

/* The FileStore uses the unchanged legacy open/read_obj/write_obj mechanics. */
int file_bank_store_save(char *name, struct object *object);
int file_bank_store_load(char *name, struct object **object);
/* Fixed FileStore locator for read-only, metadata-only evidence.  It never
 * consults or changes the active bank_store binding. */
int file_bank_store_open_readonly(char *name);

#endif
