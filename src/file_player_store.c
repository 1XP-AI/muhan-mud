/* File-backed player repository.  Its bytes remain the legacy
 * write_crt/read_crt representation; only replacement mechanics changed. */
#include "mstruct.h"
#include "mextern.h"
#include "player_path.h"
#include "player_store.h"

#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define PLAYER_STORE_PATH_MAX 1024

extern int read_crt_player(int fd, creature *crt_ptr);

#ifdef FILE_PLAYER_STORE_TESTING
static int file_player_store_fail_next_hash_read;

void file_player_store_test_fail_next_hash_read(void)
{
	file_player_store_fail_next_hash_read = 1;
}
#endif

static ssize_t file_player_store_hash_read(int fd, void *buffer, size_t length)
{
#ifdef FILE_PLAYER_STORE_TESTING
	if(file_player_store_fail_next_hash_read) {
		file_player_store_fail_next_hash_read = 0;
		errno = EIO;
		return -1;
	}
#endif
	return read(fd, buffer, length);
}

typedef unsigned int fps_u32;

typedef struct fps_sha256_ctx {
	fps_u32 state[8], bit_hi, bit_lo;
	unsigned char block[64];
	unsigned int used;
} fps_sha256_ctx;

static fps_u32 fps_rotr(fps_u32 value, unsigned int shift)
{
	return (value >> shift) | (value << (32 - shift));
}

static fps_u32 fps_load32(const unsigned char *value)
{
	return ((fps_u32)value[0] << 24) | ((fps_u32)value[1] << 16) |
		((fps_u32)value[2] << 8) | (fps_u32)value[3];
}

static void fps_store32(fps_u32 value, unsigned char *out)
{
	out[0] = (unsigned char)(value >> 24);
	out[1] = (unsigned char)(value >> 16);
	out[2] = (unsigned char)(value >> 8);
	out[3] = (unsigned char)value;
}

static void fps_sha256_block(fps_sha256_ctx *ctx, const unsigned char *block)
{
	static const fps_u32 k[64] = {
		0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
		0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
		0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
		0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
		0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
		0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
		0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
		0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
	};
	fps_u32 w[64], a, b, c, d, e, f, g, h, t1, t2;
	unsigned int i;

	for(i = 0; i < 16; i++) w[i] = fps_load32(block + 4 * i);
	for(i = 16; i < 64; i++) {
		fps_u32 s0 = fps_rotr(w[i-15], 7) ^ fps_rotr(w[i-15], 18) ^ (w[i-15] >> 3);
		fps_u32 s1 = fps_rotr(w[i-2], 17) ^ fps_rotr(w[i-2], 19) ^ (w[i-2] >> 10);
		w[i] = w[i-16] + s0 + w[i-7] + s1;
	}
	a=ctx->state[0]; b=ctx->state[1]; c=ctx->state[2]; d=ctx->state[3];
	e=ctx->state[4]; f=ctx->state[5]; g=ctx->state[6]; h=ctx->state[7];
	for(i = 0; i < 64; i++) {
		fps_u32 s1 = fps_rotr(e, 6) ^ fps_rotr(e, 11) ^ fps_rotr(e, 25);
		fps_u32 ch = (e & f) ^ ((~e) & g);
		fps_u32 s0 = fps_rotr(a, 2) ^ fps_rotr(a, 13) ^ fps_rotr(a, 22);
		fps_u32 maj = (a & b) ^ (a & c) ^ (b & c);
		t1 = h + s1 + ch + k[i] + w[i]; t2 = s0 + maj;
		h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
	}
	ctx->state[0]+=a; ctx->state[1]+=b; ctx->state[2]+=c; ctx->state[3]+=d;
	ctx->state[4]+=e; ctx->state[5]+=f; ctx->state[6]+=g; ctx->state[7]+=h;
}

static void fps_sha256_init(fps_sha256_ctx *ctx)
{
	ctx->state[0]=0x6a09e667U; ctx->state[1]=0xbb67ae85U;
	ctx->state[2]=0x3c6ef372U; ctx->state[3]=0xa54ff53aU;
	ctx->state[4]=0x510e527fU; ctx->state[5]=0x9b05688cU;
	ctx->state[6]=0x1f83d9abU; ctx->state[7]=0x5be0cd19U;
	ctx->bit_hi=ctx->bit_lo=ctx->used=0;
}

static void fps_sha256_update(fps_sha256_ctx *ctx, const unsigned char *data,
	unsigned long length)
{
	unsigned int take;
	fps_u32 old;
	while(length) {
		take = 64 - ctx->used;
		if(take > length) take = (unsigned int)length;
		memcpy(ctx->block + ctx->used, data, take);
		ctx->used += take; data += take; length -= take;
		old = ctx->bit_lo;
		ctx->bit_lo += (fps_u32)take << 3;
		if(ctx->bit_lo < old) ctx->bit_hi++;
		ctx->bit_hi += (fps_u32)take >> 29;
		if(ctx->used == 64) { fps_sha256_block(ctx, ctx->block); ctx->used = 0; }
	}
}

static void fps_sha256_final(fps_sha256_ctx *ctx, unsigned char out[32])
{
	unsigned int i;
	ctx->block[ctx->used++] = 0x80;
	if(ctx->used > 56) {
		while(ctx->used < 64) ctx->block[ctx->used++] = 0;
		fps_sha256_block(ctx, ctx->block); ctx->used = 0;
	}
	while(ctx->used < 56) ctx->block[ctx->used++] = 0;
	fps_store32(ctx->bit_hi, ctx->block + 56);
	fps_store32(ctx->bit_lo, ctx->block + 60);
	fps_sha256_block(ctx, ctx->block);
	for(i = 0; i < 8; i++) fps_store32(ctx->state[i], out + 4 * i);
}

static int player_file_same(const struct stat *left, const struct stat *right)
{
	if(!left || !right || left->st_dev != right->st_dev ||
	   left->st_ino != right->st_ino || left->st_mode != right->st_mode ||
	   left->st_size != right->st_size || left->st_mtime != right->st_mtime ||
	   left->st_ctime != right->st_ctime) return 0;
#if defined(__APPLE__)
	return left->st_mtimespec.tv_nsec == right->st_mtimespec.tv_nsec &&
		left->st_ctimespec.tv_nsec == right->st_ctimespec.tv_nsec;
#elif defined(__linux__) || defined(__FreeBSD__) || defined(__NetBSD__) || defined(__OpenBSD__)
	return left->st_mtim.tv_nsec == right->st_mtim.tv_nsec &&
		left->st_ctim.tv_nsec == right->st_ctim.tv_nsec;
#else
	return 1;
#endif
}

static int file_player_store_hash_fd(int fd, const struct stat *expected,
	char out_sha256[65])
{
	static const char hex[] = "0123456789abcdef";
	unsigned char bytes[4096], digest[32];
	fps_sha256_ctx ctx;
	struct stat after;
	ssize_t count;
	unsigned long total;
	unsigned int i;

	memset(out_sha256, 0, 65);
	memset(bytes, 0, sizeof(bytes));
	memset(digest, 0, sizeof(digest));
	fps_sha256_init(&ctx);
	total = 0;
	while((count = file_player_store_hash_read(fd, bytes, sizeof(bytes))) > 0) {
		if((unsigned long)count > PLAYER_PATH_READ_MAX_BYTES - total) goto fail;
		total += (unsigned long)count;
		fps_sha256_update(&ctx, bytes, (unsigned long)count);
	}
	if(count < 0 || fstat(fd, &after) < 0 || !player_file_same(expected, &after) ||
	   lseek(fd, 0, SEEK_SET) != 0) goto fail;
	fps_sha256_final(&ctx, digest);
	for(i = 0; i < sizeof(digest); i++) {
		out_sha256[2*i] = hex[digest[i] >> 4];
		out_sha256[2*i+1] = hex[digest[i] & 15];
	}
	memset(bytes, 0, sizeof(bytes));
	memset(digest, 0, sizeof(digest));
	memset(&ctx, 0, sizeof(ctx));
	return 0;
fail:
	memset(bytes, 0, sizeof(bytes));
	memset(digest, 0, sizeof(digest));
	memset(&ctx, 0, sizeof(ctx));
	return -1;
}

static int parent_dir(const char *path, char *dir, unsigned long dir_sz)
{
	char *slash;
	struct stat st;
	int fd;

	if(snprintf(dir, dir_sz, "%s", path) >= (int)dir_sz)
		return(-1);
	slash = strrchr(dir, '/');
	if(!slash || slash == dir)
		return(-1);
	*slash = 0;
	if(lstat(dir, &st) < 0 || !S_ISDIR(st.st_mode))
		return(-1);
#ifndef O_NOFOLLOW
	errno = ENOTSUP;
	return(-1);
#else
	fd = open(dir, O_RDONLY | O_NOFOLLOW | O_BINARY, 0);
	if(fd < 0)
		return(-1);
	if(fstat(fd, &st) < 0 || !S_ISDIR(st.st_mode)) {
		close(fd);
		return(-1);
	}
	return(close(fd) < 0 ? -1 : 0);
#endif
}

static void discard_temp(int fd, const char *temp)
{
	if(fd >= 0)
		close(fd);
	if(temp && temp[0])
		unlink(temp);
}

static int flush_parent(const char *dir)
{
	int fd;

	/* Without kernel no-follow support, do not claim a safe durable save. */
#ifndef O_NOFOLLOW
	errno = ENOTSUP;
	return(-1);
#else
	fd = open(dir, O_RDONLY | O_NOFOLLOW | O_BINARY, 0);
	if(fd < 0)
		return(-1);
	if(fsync(fd) < 0) {
		close(fd);
		return(-1);
	}
	if(close(fd) < 0)
		return(-1);
	return(0);
#endif
}

int file_player_store_save(char *str, creature *ply_ptr)
{
	char file[PLAYER_STORE_PATH_MAX];
	char dir[PLAYER_STORE_PATH_MAX], temp[PLAYER_STORE_PATH_MAX];
	int fd, n;
#ifdef COMPRESS
	char *a_buf, *b_buf;
	int size;
#endif

	if(!str || !ply_ptr || player_path_ensure_dir(str) < 0 ||
	   player_path_from_name(str, file, sizeof(file)) < 0 ||
	   parent_dir(file, dir, sizeof(dir)) < 0)
		return(PLAYER_STORE_IO_ERROR);
	if(snprintf(temp, sizeof(temp), "%s.tmp.XXXXXX", file) >= (int)sizeof(temp))
		return(PLAYER_STORE_IO_ERROR);
	fd = mkstemp(temp);
	if(fd < 0)
		return(PLAYER_STORE_IO_ERROR);
	if(fchmod(fd, 0600) < 0) {
		discard_temp(fd, temp);
		return(PLAYER_STORE_IO_ERROR);
	}

#ifdef COMPRESS
	a_buf = (char *)malloc(100000);
	if(!a_buf) merror("Memory allocation", FATAL);
	n = write_crt_to_mem(a_buf, ply_ptr, 0);
	if(n > 100000) merror(ply_ptr->name, FATAL);
	b_buf = (char *)malloc(n);
	if(!b_buf) merror("Memory allocation", FATAL);
	size = compress(a_buf, b_buf, n);
	n = write(fd, b_buf, size);
	free(a_buf);
	free(b_buf);
	if(n != size) {
		discard_temp(fd, temp);
		return(PLAYER_STORE_IO_ERROR);
	}
#else
	if(write_crt(fd, ply_ptr, 0) < 0) {
		discard_temp(fd, temp);
		return(PLAYER_STORE_IO_ERROR);
	}
#endif
	if(fsync(fd) < 0) {
		discard_temp(fd, temp);
		return(PLAYER_STORE_IO_ERROR);
	}
	if(close(fd) < 0) {
		unlink(temp);
		return(PLAYER_STORE_IO_ERROR);
	}
	if(rename(temp, file) < 0) {
		unlink(temp);
		return(PLAYER_STORE_IO_ERROR);
	}
	/* After this point readers see only the old record or the complete new one. */
	if(flush_parent(dir) < 0)
		return(PLAYER_STORE_IO_ERROR);
	return(PLAYER_STORE_OK);
}

static int file_player_store_load_open_fd(int fd, creature **ply_ptr,
const struct stat *expected)
{
	int n, close_result, changed;
	struct stat st, after;
	creature *player;

	if(fd < 0 || !ply_ptr) {
		if(fd >= 0) close(fd);
		return PLAYER_STORE_IO_ERROR;
	}
	*ply_ptr = 0;
	if(fstat(fd, &st) < 0) {
		close(fd);
		return(PLAYER_STORE_IO_ERROR);
	}
	if(!S_ISREG(st.st_mode) || (expected && !player_file_same(expected, &st))) {
		close(fd);
		return(PLAYER_STORE_IO_ERROR);
	}
#ifndef COMPRESS
	if(st.st_size < (off_t)(sizeof(creature) + sizeof(int))) {
		close(fd);
		return(PLAYER_STORE_CORRUPT);
	}
#endif
#ifdef COMPRESS
	/* The compressed decoder has no bounded input API.  Do not let a
	 * MUD1O-facing load bypass the player decoder's depth/object budget. */
	close(fd);
	return(PLAYER_STORE_IO_ERROR);
#else
	player = (creature *)malloc(sizeof(creature));
	if(!player) {
		close(fd);
		return(PLAYER_STORE_CORRUPT);
	}
	zero(player, sizeof(creature));
	n = read_crt_player(fd, player);
	changed = 0;
	if(expected && (fstat(fd, &after) < 0 || !player_file_same(expected, &after)))
		changed = 1;
	close_result = close(fd);
	if(n < 0 || changed || close_result < 0) {
		player->type = PLAYER;
		free_crt(player);
		return n < 0 && !changed && close_result >= 0 ?
			PLAYER_STORE_CORRUPT : PLAYER_STORE_IO_ERROR;
	}
	*ply_ptr = player;
	return(PLAYER_STORE_OK);
#endif
}

int file_player_store_load(char *str, creature **ply_ptr)
{
	int fd;

	if(!ply_ptr)
		return PLAYER_STORE_IO_ERROR;
	*ply_ptr = 0;
	fd = player_path_open_readonly(str);
	if(fd < 0)
		return errno == ENOENT ? PLAYER_STORE_NOT_FOUND : PLAYER_STORE_IO_ERROR;
	return file_player_store_load_open_fd(fd, ply_ptr, 0);
}

int file_player_store_inspect(char *str, char out_sha256[65])
{
	creature *player;
	struct stat before;
	int fd, result;

	if(out_sha256) memset(out_sha256, 0, 65);
	if(!str || !out_sha256) return PLAYER_STORE_IO_ERROR;
	fd = player_path_open_readonly(str);
	if(fd < 0) return errno == ENOENT ? PLAYER_STORE_NOT_FOUND : PLAYER_STORE_IO_ERROR;
	if(fstat(fd, &before) < 0 || !S_ISREG(before.st_mode) ||
	   file_player_store_hash_fd(fd, &before, out_sha256) != 0) {
		close(fd);
		memset(out_sha256, 0, 65);
		return PLAYER_STORE_IO_ERROR;
	}
	player = 0;
	result = file_player_store_load_open_fd(fd, &player, &before);
	if(result != PLAYER_STORE_OK || !player) {
		memset(out_sha256, 0, 65);
		return result == PLAYER_STORE_OK ? PLAYER_STORE_IO_ERROR : result;
	}
	player->type = PLAYER;
	free_crt(player);
	return PLAYER_STORE_OK;
}
