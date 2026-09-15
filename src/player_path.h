#ifndef PLAYER_PATH_H
#define PLAYER_PATH_H

#define PLAYER_NAME_MIN_CODEPOINTS 1UL
#define PLAYER_NAME_MAX_CODEPOINTS 12UL
/* Keep compatibility with legacy fixed-width name buffers (15 incl. NUL). */
#define PLAYER_NAME_MAX_BYTES 14UL
/* Read-side cap shared with the importer/reconciler.  It bounds untrusted
 * legacy files before a claim digest can occupy the event loop. */
#define PLAYER_PATH_READ_MAX_BYTES (64UL * 1024UL * 1024UL)

int player_path_from_name(const char *name, char *out, unsigned long out_sz);
/* Returns the two lowercase hex digits of SHA-1(name)'s first byte. */
int player_path_shard_from_name(const char *name, char out[3]);
int player_path_ensure_dir(const char *name);
int player_name_is_valid(const unsigned char *name, unsigned long min_cp, unsigned long max_cp);
/* Opens only a regular player file by walking from configured MUHAN_HOME with
 * descriptor-relative, no-follow opens.  The caller owns the returned fd. */
int player_path_open_readonly(const char *name);

#endif
