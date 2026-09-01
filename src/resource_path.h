#ifndef RESOURCE_PATH_H
#define RESOURCE_PATH_H

#include <stdio.h>
#include <sys/stat.h>

int resolve_legacy_path(const char *legacy_path, char *out, unsigned long out_sz);
int resolve_runtime_path(const char *legacy_path, char *out, unsigned long out_sz);
int runtime_path_allows_legacy_fallback(const char *legacy_path);
FILE *rp_fopen(const char *path, const char *mode);
int rp_open(const char *path, int flags, int mode);
int rp_stat(const char *path, struct stat *st);
int rp_unlink(const char *path);

#endif
