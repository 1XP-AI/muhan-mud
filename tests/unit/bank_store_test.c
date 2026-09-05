#include <stdio.h>
#include <string.h>

#include "bank_store.h"

struct object {
    int marker;
};

static int file_save_calls;
static int file_load_calls;
static int memory_save_calls;
static int memory_load_calls;
static int memory_opaque_calls;
static int save_context;
static int changed_context;

int file_bank_store_save(char *name, struct object *object)
{
    file_save_calls++;
    return (!strcmp(name, "file") && object && object->marker == 7) ?
        BANK_STORE_OK : BANK_STORE_ERROR;
}

int file_bank_store_load(char *name, struct object **object)
{
    static struct object loaded = { 8 };

    file_load_calls++;
    if(strcmp(name, "file"))
        return BANK_STORE_ERROR;
    *object = &loaded;
    return BANK_STORE_OK;
}

static int memory_save(void *opaque, char *name, struct object *object)
{
    memory_save_calls++;
    if(opaque == &save_context)
        memory_opaque_calls++;
    return (!strcmp(name, "memory") && object && object->marker == 7) ?
        BANK_STORE_OK : BANK_STORE_ERROR;
}

static int memory_load(void *opaque, char *name, struct object **object)
{
    static struct object loaded = { 9 };

    memory_load_calls++;
    if(opaque == &save_context)
        memory_opaque_calls++;
    if(strcmp(name, "memory"))
        return BANK_STORE_ERROR;
    *object = &loaded;
    return BANK_STORE_OK;
}

static int failing_load(void *opaque, char *name, struct object **object)
{
    static struct object partial = { 10 };

    (void)opaque;
    (void)name;
    *object = &partial;
    return BANK_STORE_ERROR;
}

static int positive_save(void *opaque, char *name, struct object *object)
{
    (void)opaque;
    (void)name;
    (void)object;
    return 7;
}

static int positive_load(void *opaque, char *name, struct object **object)
{
    static struct object partial = { 11 };

    (void)opaque;
    (void)name;
    *object = &partial;
    return 7;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "bank_store_test: %s\n", message);
    return 1;
}

int main(void)
{
    struct object input = { 7 };
    struct object *output = 0;
    bank_store_binding binding;
    bank_store_binding second_binding;
    bank_store_ops memory_store = { memory_save, memory_load, &save_context };
    bank_store_ops failing_store = { memory_save, failing_load, &save_context };
    bank_store_ops positive_store = { positive_save, positive_load, 0 };
    bank_store_ops invalid_store = { memory_save, 0, &save_context };
    int failed = 0;

    failed += expect(BANK_STORE_OK == 0 && BANK_STORE_ERROR < 0,
                     "contract must retain legacy success/error checks");
    failed += expect(bank_store_unbind(0) == BANK_STORE_UNBIND_INVALID,
                     "null bindings must be rejected");
    failed += expect(bank_store_save("file", &input) == BANK_STORE_OK &&
                     bank_store_load("file", &output) == BANK_STORE_OK &&
                     output && output->marker == 8,
                     "default store must retain file authority");
    failed += expect(file_save_calls == 1 && file_load_calls == 1,
                     "default calls must reach only the file store");
    failed += expect(bank_store_set(&invalid_store) == -1 &&
                     bank_store_save("file", &input) == BANK_STORE_OK &&
                     file_save_calls == 2,
                     "an invalid set must preserve the active file store");
    failed += expect(bank_store_set(&memory_store) == 0,
                     "complete stores must be injectable");
    memory_store.opaque = &changed_context;
    output = 0;
    failed += expect(bank_store_save("memory", &input) == BANK_STORE_OK &&
                     bank_store_load("memory", &output) == BANK_STORE_OK &&
                     output && output->marker == 9,
                     "injected store must receive bank calls");
    failed += expect(memory_save_calls == 1 && memory_load_calls == 1 &&
                     memory_opaque_calls == 2,
                     "store callbacks must retain copied opaque context");

    memset(&binding, 0, sizeof(binding));
    memset(&second_binding, 0, sizeof(second_binding));
    failed += expect(bank_store_bind(&failing_store, &binding) == 0 &&
                     binding.active,
                     "binding must install an owned temporary store");
    failed += expect(bank_store_bind(&failing_store, &binding) == -1 &&
                     binding.active,
                     "an active binding must not be rebound");
    failed += expect(bank_store_bind(&memory_store, &second_binding) == -1 &&
                     !second_binding.active,
                     "a second binding must not steal ownership");
    output = (struct object *)1;
    failed += expect(bank_store_load("memory", &output) == BANK_STORE_ERROR &&
                     output == 0,
                     "error mapping must clear partial output and retain legacy error");
    failed += expect(bank_store_unbind(&binding) ==
                     BANK_STORE_UNBIND_RESTORED && !binding.active,
                     "unbind must restore the previous store");
    failed += expect(bank_store_save("memory", &input) == BANK_STORE_OK &&
                     memory_opaque_calls == 3,
                     "unbind must exactly restore copied prior state");

    memset(&binding, 0, sizeof(binding));
    failed += expect(bank_store_bind(&failing_store, &binding) == 0,
                     "released binding storage must be reusable after zero init");
    failed += expect(bank_store_set(&memory_store) == 0,
                     "explicit replacement must supersede a binding");
    failed += expect(bank_store_unbind(&binding) ==
                     BANK_STORE_UNBIND_NOT_CURRENT && !binding.active,
                     "stale unbind must not leak an old store back into service");
    failed += expect(bank_store_save("memory", &input) == BANK_STORE_OK &&
                     memory_opaque_calls == 3,
                     "newer store must remain active after stale unbind");

    output = (struct object *)1;
    failed += expect(bank_store_load("memory", &output) == BANK_STORE_OK &&
                     output && output->marker == 9,
                     "success mapping must preserve callback return values");
    failed += expect(bank_store_load("memory", 0) == BANK_STORE_ERROR,
                     "null outputs must be rejected as a legacy error");

    failed += expect(bank_store_set(&positive_store) == 0,
                     "nonzero callback fixture must be injectable");
    output = (struct object *)1;
    failed += expect(bank_store_save("positive", &input) == BANK_STORE_ERROR &&
                     bank_store_load("positive", &output) == BANK_STORE_ERROR &&
                     output == 0,
                     "only exact zero callback results may be successful");

    bank_store_reset();
    failed += expect(bank_store_save("file", &input) == BANK_STORE_OK &&
                     file_save_calls == 3,
                     "reset must restore file authority");

    if(failed)
        return 1;
    puts("bank_store_test: ok");
    return 0;
}
