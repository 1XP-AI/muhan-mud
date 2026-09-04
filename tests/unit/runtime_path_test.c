#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

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

static int expect_truncated_legacy_path_rejected(const char *legacy, const char *expected)
{
    char actual[512];
    unsigned long boundary;

    boundary = (unsigned long)strlen(expected);
    if(boundary == 0 || boundary >= sizeof(actual)) {
        fprintf(stderr, "runtime_path_test: test_resolve_legacy_path_rejects_truncated_paths has invalid fixture\n");
        return 1;
    }

    if(resolve_legacy_path(legacy, actual, boundary) != -1 || actual[0] != 0) {
        fprintf(stderr, "runtime_path_test: test_resolve_legacy_path_rejects_truncated_paths must reject %s\n", legacy);
        return 1;
    }
    return 0;
}

static int test_alias_precedes_raw_resource_path(void)
{
    char root[] = "/tmp/muhan-resource-path-XXXXXX";
    char manifest_dir[512], resources_dir[512], normalized_dir[512];
    char raw_dir[512], raw_identity_dir[512], normalized_identity_dir[512];
    char alias_file[512], raw_file[512], normalized_file[512];
    char raw_identity_file[512], normalized_identity_file[512], raw_missing_file[512];
    char canonical_file[512], expected[512], actual[512];
    char overlong_rel[1025], truncated_hex[2047];
    FILE *fp;
    struct stat missing_stat;
    unsigned long i;
    int failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }

    snprintf(manifest_dir, sizeof(manifest_dir), "%s/resources_manifest", root);
    snprintf(resources_dir, sizeof(resources_dir), "%s/resources_utf8", root);
    snprintf(normalized_dir, sizeof(normalized_dir), "%s/objmon", resources_dir);
    snprintf(raw_dir, sizeof(raw_dir), "%s/objmon", root);
    snprintf(raw_identity_dir, sizeof(raw_identity_dir), "%s/help", root);
    snprintf(normalized_identity_dir, sizeof(normalized_identity_dir), "%s/help", resources_dir);
    snprintf(alias_file, sizeof(alias_file), "%s/path-alias.v1.tsv", manifest_dir);
    snprintf(raw_file, sizeof(raw_file), "%s/celduin_sign", raw_dir);
    snprintf(normalized_file, sizeof(normalized_file), "%s/celduin_sign__4eb75435", normalized_dir);
    snprintf(raw_identity_file, sizeof(raw_identity_file), "%s/welcome", raw_identity_dir);
    snprintf(normalized_identity_file, sizeof(normalized_identity_file), "%s/welcome", normalized_identity_dir);
    snprintf(raw_missing_file, sizeof(raw_missing_file), "%s/missing_sign", raw_dir);
    snprintf(canonical_file, sizeof(canonical_file), "%s/canonical_only", resources_dir);
    snprintf(expected, sizeof(expected), "%s", normalized_file);
    memset(overlong_rel, 'a', sizeof(overlong_rel) - 1);
    overlong_rel[sizeof(overlong_rel) - 1] = 0;
    for(i = 0; i < sizeof(truncated_hex) - 1; i += 2) {
        truncated_hex[i] = '6';
        truncated_hex[i + 1] = '1';
    }
    truncated_hex[sizeof(truncated_hex) - 1] = 0;

    if(mkdir(manifest_dir, 0700) < 0 || mkdir(resources_dir, 0700) < 0 ||
       mkdir(normalized_dir, 0700) < 0 || mkdir(raw_dir, 0700) < 0 ||
       mkdir(raw_identity_dir, 0700) < 0 || mkdir(normalized_identity_dir, 0700) < 0) {
        perror("mkdir");
        failed = 1;
        goto cleanup;
    }

    fp = fopen(alias_file, "w");
    if(!fp ||
       fputs("legacy_path_hex\tlegacy_path_cp949\tnormalized_utf8_path\tblob_sha1\n", fp) < 0 ||
       fputs("6F626A6D6F6E2F63656C6475696E5F7369676E\tobjmon/celduin_sign\tobjmon/celduin_sign__4eb75435\t4eb75435475df4df6d4cb20050754c1ab09baefe\n", fp) < 0 ||
       fputs("68656C702F77656C636F6D65\thelp/welcome\thelp/welcome\t49b3a1975def2762e68f2663351ee55ffb387e61\n", fp) < 0 ||
       fputs("6F626A6D6F6E2F6D697373696E675F7369676E\tobjmon/missing_sign\tobjmon/missing_sign__canonical\t4eb75435475df4df6d4cb20050754c1ab09baefe\n", fp) < 0 ||
       fputs(truncated_hex, fp) < 0 ||
       fputs("\toverlong\tcanonical_only\t4eb75435475df4df6d4cb20050754c1ab09baefe\n", fp) < 0) {
        perror("write alias manifest");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(canonical_file, "w");
    if(!fp || fputs("canonical-only resource\n", fp) < 0) {
        perror("write canonical-only resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(raw_missing_file, "w");
    if(!fp || fputs("wrong raw fallback resource\n", fp) < 0) {
        perror("write raw missing resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(raw_identity_file, "w");
    if(!fp || fputs("current raw resource\n", fp) < 0) {
        perror("write raw identity resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(normalized_identity_file, "w");
    if(!fp || fputs("stale normalized resource\n", fp) < 0) {
        perror("write normalized identity resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(raw_file, "w");
    if(!fp || fputs("wrong raw resource\n", fp) < 0) {
        perror("write raw resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    fp = fopen(normalized_file, "w");
    if(!fp || fputs("canonical resource\n", fp) < 0) {
        perror("write normalized resource");
        failed = 1;
        if(fp)
            fclose(fp);
        goto cleanup;
    }
    fclose(fp);

    if(setenv("MUHAN_HOME", root, 1) != 0) {
        perror("setenv");
        failed = 1;
        goto cleanup;
    }
    if(resolve_legacy_path("/home/muhan/objmon/celduin_sign", actual, sizeof(actual)) != 0 ||
       strcmp(actual, expected) != 0) {
        fprintf(stderr, "runtime_path_test: manifest alias must win over raw resource path\n");
        failed = 1;
    }
    snprintf(expected, sizeof(expected), "%s", raw_identity_file);
    if(resolve_legacy_path("/home/muhan/help/welcome", actual, sizeof(actual)) != 0 ||
       strcmp(actual, expected) != 0) {
        fprintf(stderr, "runtime_path_test: identity alias must keep raw resource precedence\n");
        failed = 1;
    }
    failed += expect_truncated_legacy_path_rejected(
        "/home/muhan/objmon/celduin_sign", normalized_file);
    failed += expect_truncated_legacy_path_rejected("canonical_only", canonical_file);
    memset(actual, 'X', sizeof(actual));
    if(resolve_legacy_path(overlong_rel, actual, sizeof(actual)) != -1 || actual[0] != 0) {
        fprintf(stderr, "runtime_path_test: overlong legacy alias key must fail closed\n");
        failed = 1;
    }
    errno = 0;
    fp = rp_fopen("/home/muhan/objmon/missing_sign", "r");
    if(fp || errno != ENOENT) {
        fprintf(stderr, "runtime_path_test: missing renamed alias target must fail closed\n");
        failed = 1;
        if(fp)
            fclose(fp);
    }
    errno = 0;
    if(rp_open("/home/muhan/objmon/missing_sign", O_RDONLY, 0) != -1 || errno != ENOENT) {
        fprintf(stderr, "runtime_path_test: rp_open must fail closed for missing renamed alias target\n");
        failed = 1;
    }
    errno = 0;
    if(rp_stat("/home/muhan/objmon/missing_sign", &missing_stat) != -1 || errno != ENOENT) {
        fprintf(stderr, "runtime_path_test: rp_stat must fail closed for missing renamed alias target\n");
        failed = 1;
    }

cleanup:
    unlink(canonical_file);
    unlink(normalized_identity_file);
    unlink(raw_identity_file);
    unlink(raw_missing_file);
    unlink(normalized_file);
    unlink(raw_file);
    unlink(alias_file);
    rmdir(normalized_dir);
    rmdir(normalized_identity_dir);
    rmdir(raw_dir);
    rmdir(raw_identity_dir);
    rmdir(resources_dir);
    rmdir(manifest_dir);
    rmdir(root);
    return failed;
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

    failed += test_alias_precedes_raw_resource_path();

    if(failed)
        return 1;

    puts("runtime_path_test: ok");
    return 0;
}
