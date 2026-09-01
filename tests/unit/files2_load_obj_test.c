#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "mstruct.h"

extern int load_obj(int index, object **obj_ptr);

void merror(char *message, char kind)
{
    (void)message;
    (void)kind;
    abort();
}

/* load_obj's LRU eviction path is not reached by this focused test. */
void free_obj(object *obj_ptr)
{
    free(obj_ptr);
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "files2_load_obj_test: %s\n", message);
    return 1;
}

static int object_links_are_clear(object *obj_ptr)
{
    return obj_ptr && !obj_ptr->first_obj && !obj_ptr->parent_obj &&
           !obj_ptr->parent_rom && !obj_ptr->parent_crt;
}

int main(void)
{
    char root[] = "/tmp/muhan-load-obj-XXXXXX";
    char objdir[512];
    char catalog[512];
    object on_disk;
    object *first = 0, *cached = 0, *truncated = (object *)1;
    int fd, failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    snprintf(objdir, sizeof(objdir), "%s/objmon", root);
    snprintf(catalog, sizeof(catalog), "%s/o00", objdir);
    if(mkdir(objdir, 0700) < 0) {
        perror("mkdir");
        return 1;
    }

    memset(&on_disk, 0, sizeof(on_disk));
    strcpy(on_disk.name, "stale links");
    /* Model an object catalog written by a different process. */
    on_disk.first_obj = (otag *)1;
    on_disk.parent_obj = (object *)2;
    on_disk.parent_rom = (room *)3;
    on_disk.parent_crt = (creature *)4;
    fd = open(catalog, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write(fd, &on_disk, sizeof(on_disk)) != sizeof(on_disk)) {
        perror("write catalog");
        if(fd >= 0)
            close(fd);
        return 1;
    }
    close(fd);

    if(setenv("MUHAN_HOME", root, 1) < 0) {
        perror("setenv");
        return 1;
    }
    failed += expect(load_obj(0, &first) == 0,
                     "raw object catalog record must load");
    failed += expect(object_links_are_clear(first),
                     "raw object runtime pointers must be cleared");
    failed += expect(load_obj(0, &cached) == 0,
                     "cached object catalog record must load");
    failed += expect(object_links_are_clear(cached),
                     "cached clone runtime pointers must remain clear");
    failed += expect(load_obj(1, &truncated) == -1 && !truncated,
                     "short catalog reads must free and clear the result pointer");
    truncated = (object *)1;
    failed += expect(load_obj(1000, &truncated) == -1 && !truncated,
                     "invalid object indexes must clear the result pointer");

    free(first);
    free(cached);
    unlink(catalog);
    rmdir(objdir);
    rmdir(root);
    return failed ? 1 : 0;
}
