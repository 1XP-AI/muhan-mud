#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "mstruct.h"

extern int load_bank(char *name, object **object);
extern int save_bank(char *name, object *object);

static const char original_tail[] = "old-tail-must-survive";

void free_obj(object *object)
{
    free(object);
}

int write_obj(int fd, object *object, char permanent_only)
{
    (void)permanent_only;
    return write(fd, &object->value, sizeof(object->value)) ==
        sizeof(object->value) ? 0 : -1;
}

int read_obj(int fd, object *object)
{
    return read(fd, &object->value, sizeof(object->value)) ==
        sizeof(object->value) ? 0 : -1;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "bank_legacy_abi_test: %s\n", message);
    return 1;
}

int main(void)
{
    char root[] = "/tmp/muhan-bank-store-XXXXXX";
    char player[512], bank[512], bank_file[512];
    object input, *output = 0;
    struct stat st;
    int fd, failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    snprintf(player, sizeof(player), "%s/player", root);
    snprintf(bank, sizeof(bank), "%s/bank", player);
    snprintf(bank_file, sizeof(bank_file), "%s/Legacy", bank);
    if(mkdir(player, 0700) < 0 || mkdir(bank, 0700) < 0 ||
       setenv("MUHAN_HOME", root, 1) < 0) {
        perror("bank fixture setup");
        return 1;
    }
    fd = open(bank_file, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write(fd, original_tail, sizeof(original_tail) - 1) !=
       sizeof(original_tail) - 1 || close(fd) < 0) {
        perror("bank fixture write");
        return 1;
    }

    memset(&input, 0, sizeof(input));
    input.value = 1234;
    failed += expect(save_bank("Legacy", &input) == 0,
                     "legacy save ABI must retain file-backed success");
    failed += expect(stat(bank_file, &st) == 0 && st.st_size ==
                     (off_t)(sizeof(original_tail) - 1),
                     "legacy save must retain its existing non-truncating I/O behavior");
    failed += expect(load_bank("Legacy", &output) == 0 && output &&
                     output->value == 1234,
                     "legacy load ABI must use the default file store");
    free(output);
    output = (object *)1;
    failed += expect(load_bank("Missing", &output) == -1 && !output,
                     "legacy file failures must retain the -1 result and clear output");

    unlink(bank_file);
    rmdir(bank);
    rmdir(player);
    rmdir(root);
    return failed ? 1 : 0;
}
