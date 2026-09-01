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

static int parent_dir(const char *path, char *dir, unsigned long dir_sz)
{
	char *slash;
	struct stat st;

	if(snprintf(dir, dir_sz, "%s", path) >= (int)dir_sz)
		return(-1);
	slash = strrchr(dir, '/');
	if(!slash || slash == dir)
		return(-1);
	*slash = 0;
	if(stat(dir, &st) == 0)
		return(S_ISDIR(st.st_mode) ? 0 : -1);
	if(errno != ENOENT)
		return(-1);
	return(mkdir(dir, 0770) == 0 ? 0 : -1);
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

	fd = open(dir, O_RDONLY | O_BINARY, 0);
	if(fd < 0)
		return(-1);
	if(fsync(fd) < 0) {
		close(fd);
		return(-1);
	}
	if(close(fd) < 0)
		return(-1);
	return(0);
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

	if(!str || !ply_ptr || player_path_from_name(str, file, sizeof(file)) < 0 ||
	   parent_dir(file, dir, sizeof(dir)) < 0)
		return(PLAYER_STORE_IO_ERROR);
	if(snprintf(temp, sizeof(temp), "%s.tmp.XXXXXX", file) >= (int)sizeof(temp))
		return(PLAYER_STORE_IO_ERROR);
	fd = mkstemp(temp);
	if(fd < 0)
		return(PLAYER_STORE_IO_ERROR);
	if(fchmod(fd, ACC) < 0) {
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
	char file[PLAYER_STORE_PATH_MAX];
	int fd, n;
	struct stat st;
#ifdef COMPRESS
	char *a_buf, *b_buf;
	int size;
#endif

	if(!ply_ptr)
		return(PLAYER_STORE_IO_ERROR);
	*ply_ptr = 0;
	if(player_path_from_name(str, file, sizeof(file)) < 0)
		return(PLAYER_STORE_IO_ERROR);
	fd = open(file, O_RDONLY | O_BINARY, 0);
	if(fd < 0)
		return(errno == ENOENT ? PLAYER_STORE_NOT_FOUND : PLAYER_STORE_IO_ERROR);
	if(fstat(fd, &st) < 0) {
		close(fd);
		return(PLAYER_STORE_IO_ERROR);
	}
#ifndef COMPRESS
	if(st.st_size < (off_t)(sizeof(creature) + sizeof(int))) {
		close(fd);
		return(PLAYER_STORE_CORRUPT);
	}
#endif
	*ply_ptr = (creature *)malloc(sizeof(creature));
	if(!*ply_ptr) merror("load_ply", FATAL);
	zero(*ply_ptr, sizeof(creature));
#ifdef COMPRESS
	a_buf = (char *)malloc(50000);
	if(!a_buf) merror("Memory allocation", FATAL);
	size = read(fd, a_buf, 50000);
	if(size >= 50000) merror("Player too large", FATAL);
	if(size < 1) {
		free(a_buf);
		close(fd);
		free(*ply_ptr);
		*ply_ptr = 0;
		return(PLAYER_STORE_CORRUPT);
	}
	b_buf = (char *)malloc(100000);
	if(!b_buf) merror("Memory allocation", FATAL);
	n = uncompress(a_buf, b_buf, size);
	if(n > 100000) merror("Player too large", FATAL);
	n = read_crt_from_mem(b_buf, *ply_ptr, 0);
	free(a_buf);
	free(b_buf);
#else
	n = read_crt(fd, *ply_ptr);
#endif
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
}
