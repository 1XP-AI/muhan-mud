/*
 * Bounded encoder for the legacy write_crt player-file representation.
 *
 * The destination belongs to the caller.  On every error *written is zero
 * and destination bytes are left unchanged.  The encoder does not allocate
 * or perform I/O.
 */
#ifndef PLAYER_RECORD_SERIALIZER_H
#define PLAYER_RECORD_SERIALIZER_H

#define PLAYER_RECORD_SERIALIZER_OK                 0
#define PLAYER_RECORD_SERIALIZER_INVALID           -1
#define PLAYER_RECORD_SERIALIZER_NO_SPACE          -2
#define PLAYER_RECORD_SERIALIZER_OVERFLOW          -3
#define PLAYER_RECORD_SERIALIZER_DEPTH_LIMIT       -4
#define PLAYER_RECORD_SERIALIZER_OBJECT_LIMIT      -5
#define PLAYER_RECORD_SERIALIZER_INCONSISTENT      -6

struct creature;

typedef struct player_record_serializer_limits {
	unsigned long max_depth;
	unsigned long max_objects;
} player_record_serializer_limits;

/* Writes precisely the bytes produced by write_crt for supported graphs.
 * perm_only must be the canonical boolean value 0 or 1.  max_depth is object
 * nesting depth (a top-level inventory item is depth 1).
 * max_objects bounds inspected object tags and serialized object records.
 */
extern int player_record_serialize_bounded(
	struct creature *crt_ptr, char perm_only, char *buffer, unsigned long capacity,
	unsigned long *written,
	const player_record_serializer_limits *limits);

#endif
