#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "resource_path.h"

static int expect_path(const char *legacy, const char *expected)
{
    char actual[512];

    if(resolve_runtime_path(legacy, actual, sizeof(actual)) < 0) {
        fprintf(stderr, "runtime_path_test: failed to resolve %s\n", legacy);
        return 1;
    }
    if(strcmp(actual, expected)) {
        fprintf(stderr, "runtime_path_test: %s != %s\n", actual, expected);
        return 1;
    }
    return 0;
}

int main(void)
{
    char tiny[4];
    int failed = 0;

    if(setenv("MUHAN_HOME", "/tmp/muhan-fixture", 1) != 0) {
        perror("setenv");
        return 1;
    }

    failed += expect_path("/home/muhan/player/ab/test",
                          "/tmp/muhan-fixture/player/ab/test");
    failed += expect_path("/home/muhan", "/tmp/muhan-fixture");
    failed += expect_path("/outside/muhan", "/outside/muhan");

    if(runtime_path_allows_legacy_fallback("/home/muhan/player/ab/test")) {
        fprintf(stderr, "runtime_path_test: explicit MUHAN_HOME must block /home/muhan fallback\n");
        failed++;
    }
    if(!runtime_path_allows_legacy_fallback("/outside/muhan")) {
        fprintf(stderr, "runtime_path_test: unrelated paths may retain their normal fallback\n");
        failed++;
    }

    if(resolve_runtime_path("/home/muhan/player", tiny, sizeof(tiny)) != -1) {
        fprintf(stderr, "runtime_path_test: truncation must fail\n");
        failed++;
    }

    if(failed)
        return 1;

    puts("runtime_path_test: ok");
    return 0;
}
