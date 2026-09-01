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

int file_player_store_load(char *str, creature **ply_ptr)
{
	int fd, n;
	struct stat st;

	if(!ply_ptr)
		return(PLAYER_STORE_IO_ERROR);
	*ply_ptr = 0;
	fd = player_path_open_readonly(str);
	if(fd < 0)
		return(errno == ENOENT ? PLAYER_STORE_NOT_FOUND : PLAYER_STORE_IO_ERROR);
	if(fstat(fd, &st) < 0) {
		close(fd);
		return(PLAYER_STORE_IO_ERROR);
	}
	if(!S_ISREG(st.st_mode)) {
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
	*ply_ptr = (creature *)malloc(sizeof(creature));
	if(!*ply_ptr) {
		close(fd);
		return(PLAYER_STORE_CORRUPT);
	}
	zero(*ply_ptr, sizeof(creature));
	n = read_crt_player(fd, *ply_ptr);
	if(close(fd) < 0) {
		if(n >= 0) {
			(*ply_ptr)->type = PLAYER;
			free_crt(*ply_ptr);
			*ply_ptr = 0;
			return(PLAYER_STORE_IO_ERROR);
		}
	}
	if(n < 0) {
		(*ply_ptr)->type = PLAYER;
		free_crt(*ply_ptr);
		*ply_ptr = 0;
		return(PLAYER_STORE_CORRUPT);
	}
	return(PLAYER_STORE_OK);
#endif
}
