/*
 * FILES1.C:
 *
 *	File I/O Routines.
 *
 *	Copyright (C) 1991, 1992, 1993 Brett J. Vickers
 *
 */

#include "mstruct.h"
#include "mextern.h"
#include <errno.h>

#define MAX_NESTED_OBJECTS 4096
#ifndef PLAYER_DECODE_MAX_DEPTH
#define PLAYER_DECODE_MAX_DEPTH 64
#endif
#ifndef PLAYER_DECODE_MAX_OBJECTS
#define PLAYER_DECODE_MAX_OBJECTS 8192
#endif
#define MAX_ROM_EXITS 200
#define MAX_ROM_MOBS 4096
#define MAX_ROM_ITEMS 8192
#define MAX_ROM_DESC_BYTES (1024 * 1024)

#ifdef PLAYER_DECODER_TEST_HOOK
extern void *player_decoder_malloc(unsigned long size);
#define PLAYER_DECODER_MALLOC(size) player_decoder_malloc(size)
#else
#define PLAYER_DECODER_MALLOC(size) malloc(size)
#endif

/* Return failure to the caller instead of terminating the server when a
 * player record cannot be written.  write(2) may be interrupted or may
 * complete only part of the record. */
static int write_full(fd, buf, size)
int fd;
char *buf;
unsigned long size;
{
	int n;

	while(size) {
		n = write(fd, buf, size);
		if(n > 0) {
			buf += n;
			size -= n;
			continue;
		}
		if(n < 0 && errno == EINTR)
			continue;
		return(-1);
	}
	return(0);
}

/**********************************************************************/
/*				count_obj			      */
/**********************************************************************/

/* Return the total number of objects contained within an object.  */
/* If perm_only != 0, then only the permanent objects are counted. */

int count_obj(obj_ptr, perm_only)
object 	*obj_ptr;
char 	perm_only;
{
	otag	*op;
	int	total=0;

	op = obj_ptr->first_obj;
	while(op) {
		if(!perm_only || (perm_only && (F_ISSET(op->obj, OPERMT))))
			total++;
		op = op->next_tag;
	}
	return(total);
}

/**********************************************************************/
/*				write_obj			      */
/**********************************************************************/

/* Save an object to a file that has already been opened and positioned. */
/* This function recursively saves the items that are contained inside   */
/* the object as well.  If perm_only != 0 then only permanent objects	 */
/* within the object are saved with it.					 */

int write_obj(fd, obj_ptr, perm_only)
int 	fd;
object 	*obj_ptr;
char 	perm_only;
{
	int 	cnt, cnt2=0, error=0;
	otag	*op;

	if(write_full(fd, (char *)obj_ptr, sizeof(object)) < 0)
		return(-1);
	
	cnt = count_obj(obj_ptr, perm_only);
	if(write_full(fd, (char *)&cnt, sizeof(int)) < 0)
		return(-1);

	if(cnt > 0) {
		op = obj_ptr->first_obj;
		while(op) {
			if(!perm_only || (perm_only && 
			   (F_ISSET(op->obj, OPERMT)))) {
				if(write_obj(fd, op->obj, perm_only) < 0)
					error = 1;
				cnt2++;
			}
			op = op->next_tag;
		}
	}

	if(cnt != cnt2 || error)
		return(-1);
	else
		return(0);

}

/**********************************************************************/
/*				count_inv			      */
/**********************************************************************/

/* Returns the number of objects a creature has in his inventory. */
/* If perm_only != 0 then only those objects which are permanent  */
/* will be counted. if -1 count container			  */

int count_inv(crt_ptr, perm_only)
creature 	*crt_ptr;
char 		perm_only;
{
	otag	*op;
	int	total=0, cnt_cont=0;

	if(perm_only== -1) {
		perm_only=0;
		cnt_cont=1;
	}

	op = crt_ptr->first_obj;
	while(op) {
		if(!perm_only || (perm_only && (F_ISSET(op->obj, OPERMT)))) 
			total++;
		/* 보따리 물건 체크 */
		if(!perm_only && cnt_cont ==1 && F_ISSET(op->obj, OCONTN))
			total+=op->obj->shotscur;
		op = op->next_tag;
	}
	return(MIN(200,total));
}

/**********************************************************************/
/*				write_crt			      */
/**********************************************************************/

/* Save a creature to an already open and positioned file.  This function */
/* also saves all the items the creature is holding.  If perm_only != 0   */
/* then only those items which the creature is carrying that are 	  */
/* permanent will be saved.						  */

int write_crt(fd, crt_ptr, perm_only)
int 		fd;
creature 	*crt_ptr;
char 		perm_only;
{
	int 	cnt, cnt2=0, error=0;
	otag	*op;

	if(write_full(fd, (char *)crt_ptr, sizeof(creature)) < 0)
		return(-1);

	cnt = count_inv(crt_ptr, perm_only);
	if(write_full(fd, (char *)&cnt, sizeof(int)) < 0)
		return(-1);

	if(cnt > 0) {
		op = crt_ptr->first_obj;
		while(op && cnt2<cnt) {
			if(!perm_only || (perm_only && 
			   (F_ISSET(op->obj, OPERMT)))) {
				if(write_obj(fd, op->obj, perm_only) < 0)
					error = 1;
				cnt2++;
			}
			op = op->next_tag;
		}
	}

	if(cnt != cnt2 || error)
		return(-1);
	else
		return(0);
}

/**********************************************************************/
/*				count_mon			      */
/**********************************************************************/

/* Returns the number of monsters in a given room.  If perm_only is */
/* nonzero, then only the number of permanent monsters in the room  */
/* is returned.							    */

int count_mon(rom_ptr, perm_only)
room 	*rom_ptr;
char 	perm_only;
{
	ctag	*cp;
	int	total=0;

	cp = rom_ptr->first_mon;
	while(cp) {
		if(!perm_only || (perm_only && F_ISSET(cp->crt, MPERMT)))
			total++;
		cp = cp->next_tag;
	}
	return(total);
}

/**********************************************************************/
/*				count_ite			      */
/**********************************************************************/

/* Returns the number of items in the room.  If perm_only is nonzero */
/* then only the number of permanent items in the room is returned.  */

int count_ite(rom_ptr, perm_only)
room 	*rom_ptr;
char 	perm_only;
{
	otag 	*op;
	int	total=0;

	op = rom_ptr->first_obj;
	while(op) {
		if(!perm_only || (perm_only && (F_ISSET(op->obj, OPERMT))))
			total++;
		op = op->next_tag;
	}
	return(total);
}

/**********************************************************************/
/*				count_ext			      */
/**********************************************************************/

/* Returns the number of exits in the room */

int count_ext(rom_ptr)
room 	*rom_ptr;
{
	xtag	*xp;
	int	total = 0;

	xp = rom_ptr->first_ext;
	while(xp) {
		total++;
		xp = xp->next_tag;
	}
	return(total);
}

/**********************************************************************/
/*				count_ply			      */
/**********************************************************************/

/* Returns the number of players in the room */

int count_ply(rom_ptr)
room 	*rom_ptr;
{
	ctag	*pp;
	int	total = 0;

	pp = rom_ptr->first_ply;
	while(pp) {
		total++;
		pp = pp->next_tag;
	}
	return(total);
}

/**********************************************************************/
/*				write_rom			      */
/**********************************************************************/

/* Saves the room pointed to by rom_ptr with all its contents.  Its */
/* contents include exits, descriptions, items and monsters.  If    */
/* perm_only is nonzero, then only items and monsters in the room   */
/* which are permanent will be saved.				    */

int write_rom(fd, rom_ptr, perm_only)
int 	fd;
room 	*rom_ptr;
char 	perm_only;
{
	int 	n, cnt, error=0;
	xtag	*xp;
	ctag	*cp;
	otag	*op;
	char	pcontain;

	n = write(fd, rom_ptr, sizeof(room));
	if(n < sizeof(room))
		merror("write_rom", FATAL);

	cnt = count_ext(rom_ptr);
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

	xp = rom_ptr->first_ext;
	while(xp) {
		n = write(fd, xp->ext, sizeof(exit_));
		if(n < sizeof(exit_))
			merror("write_rom", FATAL);
		xp = xp->next_tag;
	}

	cnt = count_mon(rom_ptr, perm_only);
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

	cp = rom_ptr->first_mon;
	while(cp) {
		if(!perm_only || (perm_only && F_ISSET(cp->crt, MPERMT)))
			if(write_crt(fd, cp->crt, 0) < 0)
				error = 1;
		cp = cp->next_tag;
	}

	cnt = count_ite(rom_ptr, perm_only);
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

op = rom_ptr->first_obj;
        while(op) {
                if(!perm_only || (perm_only && (F_ISSET(op->obj, OPERMT)))){
                        if (F_ISSET(op->obj,OCONTN)
                                && (F_ISSET(op->obj, OPERMT)))
                                pcontain =  ALLITEMS;
                        else
                                pcontain = perm_only;
                                if(write_obj(fd, op->obj, pcontain) < 0)
                                        error = 1;
                }
                op = op->next_tag;
	}

	if(!rom_ptr->short_desc)
		cnt = 0;
	else
		cnt = strlen(rom_ptr->short_desc) + 1;
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

	if(cnt) {
		n = write(fd, rom_ptr->short_desc, cnt);
		if(n < cnt)
			merror("write_rom", FATAL);
	}

	if(!rom_ptr->long_desc)
		cnt = 0;
	else
		cnt = strlen(rom_ptr->long_desc) + 1;
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

	if(cnt) {
		n = write(fd, rom_ptr->long_desc, cnt);
		if(n < cnt)
			merror("write_rom", FATAL);
	}

	if(!rom_ptr->obj_desc)
		cnt = 0;
	else
		cnt = strlen(rom_ptr->obj_desc) + 1;
	n = write(fd, &cnt, sizeof(int));
	if(n < sizeof(int))
		merror("write_rom", FATAL);

	if(cnt) {
		n = write(fd, rom_ptr->obj_desc, cnt);
		if(n < cnt)
			merror("write_rom", FATAL);
	}

	if(error)
		return(-1);
	else
		return(0);
}

/**********************************************************************/
/*				read_obj			      */
/**********************************************************************/

/* Loads the object from the open and positioned file described by fd */
/* and also loads every object which it might contain.  Returns -1 if */
/* there was an error.						      */

int read_obj(fd, obj_ptr)
int 	fd;
object 	*obj_ptr;
{
	int 		n, cnt, error=0;
	otag		*op;
	otag		**prev;
	object 		*obj;

	n = read(fd, obj_ptr, sizeof(object));
	if(n < sizeof(object))
		error = 1;

	obj_ptr->first_obj = 0;
	obj_ptr->parent_obj = 0;
	obj_ptr->parent_rom = 0;
	obj_ptr->parent_crt = 0;
	if(obj_ptr->shotscur > obj_ptr->shotsmax)
		obj_ptr->shotscur = obj_ptr->shotsmax;

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_NESTED_OBJECTS) {
		error = 1;
		cnt = 0;
	}

	prev = &obj_ptr->first_obj;
	while(cnt > 0) {
		cnt--;
		op = (otag *)malloc(sizeof(otag));
		if(op) {
			obj = (object *)malloc(sizeof(object));
			if(obj) {
				if(read_obj(fd, obj) < 0)
					error = 1;
				obj->parent_obj = obj_ptr;
				op->obj = obj;
				op->next_tag = 0;
				*prev = op;
				prev = &op->next_tag;
			}
			else
				merror("read_obj", FATAL);
		}
		else
			merror("read_obj", FATAL);
	}

	if(error)
		return(-1);
	else
		return(0);
}

/**********************************************************************/
/*				read_crt			      */
/**********************************************************************/

/* Loads a creature from an open and positioned file.  The creature is */
/* loaded at the mem location specified by the second parameter.  In   */
/* addition, all the creature's objects have memory allocated for them */
/* and are loaded as well.  Returns -1 on fail.			       */

int read_crt(fd, crt_ptr)
int 		fd;
creature 	*crt_ptr;
{
	int 		n, cnt, error=0;
	otag		*op;
	otag		**prev;
	object 		*obj;

	n = read(fd, crt_ptr, sizeof(creature));
	if(n < sizeof(creature))
		error = 1;

	crt_ptr->first_obj = 0;
	crt_ptr->first_fol = 0;
	crt_ptr->first_enm = 0;
	crt_ptr->first_tlk = 0;
	crt_ptr->parent_rom = 0;
	crt_ptr->following = 0;
	for(n=0; n<20; n++)
		crt_ptr->ready[n] = 0;
	if(crt_ptr->mpcur > crt_ptr->mpmax)
		crt_ptr->mpcur = crt_ptr->mpmax;
	if(crt_ptr->hpcur > crt_ptr->hpmax)
		crt_ptr->hpcur = crt_ptr->hpmax;

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_NESTED_OBJECTS) {
		error = 1;
		cnt = 0;
	}

	prev = &crt_ptr->first_obj;
	while(cnt > 0) {
		cnt--;
		op = (otag *)malloc(sizeof(otag));
		if(op) {
			obj = (object *)malloc(sizeof(object));
			if(obj) {
				if(read_obj(fd, obj) < 0)
					error = 1;
				obj->parent_crt = crt_ptr;
				op->obj = obj;
				op->next_tag = 0;
				*prev = op;
				prev = &op->next_tag;
			}
			else
				merror("read_crt", FATAL);
		}
		else
			merror("read_crt", FATAL);
	}

	if(error)
		return(-1);
	else
		return(0);
}

/**********************************************************************/
/*                         read_crt_player                            */
/**********************************************************************/

/* Player files can be supplied by an internet-facing MUD1O claim/load
 * request.  Keep this bounded reader separate from read_crt(), which is
 * also used for trusted room blobs and must retain its legacy behavior. */

typedef struct player_decode_context {
	unsigned long object_count;
	unsigned long max_objects;
	unsigned int max_depth;
} player_decode_context;

static int player_read_full(fd, buf, size)
int fd;
char *buf;
unsigned long size;
{
	int n;

	while(size > 0) {
		n = read(fd, buf, size);
		if(n > 0) {
			buf += n;
			size -= n;
			continue;
		}
		if(n < 0 && errno == EINTR)
			continue;
		return(-1);
	}
	return(0);
}

static void free_player_object(obj_ptr)
object *obj_ptr;
{
	otag *op, *next;

	if(!obj_ptr)
		return;
	op = obj_ptr->first_obj;
	while(op) {
		next = op->next_tag;
		free_player_object(op->obj);
		free(op);
		op = next;
	}
	free(obj_ptr);
}

static void free_player_object_list(head)
otag *head;
{
	otag *op, *next;

	op = head;
	while(op) {
		next = op->next_tag;
		free_player_object(op->obj);
		free(op);
		op = next;
	}
}

static void reset_player_object_links(obj_ptr)
object *obj_ptr;
{
	obj_ptr->first_obj = 0;
	obj_ptr->parent_obj = 0;
	obj_ptr->parent_rom = 0;
	obj_ptr->parent_crt = 0;
}

static void reset_player_creature_links(crt_ptr)
creature *crt_ptr;
{
	int n;

	crt_ptr->first_obj = 0;
	crt_ptr->first_fol = 0;
	crt_ptr->first_enm = 0;
	crt_ptr->first_tlk = 0;
	crt_ptr->parent_rom = 0;
	crt_ptr->following = 0;
	for(n = 0; n < 20; ++n)
		crt_ptr->ready[n] = 0;
}

static int player_has_nul(value, size)
char *value;
unsigned long size;
{
	while(size > 0) {
		if(*value == 0)
			return(1);
		value++;
		size--;
	}
	return(0);
}

static int player_creature_strings_valid(crt_ptr)
creature *crt_ptr;
{
	return(player_has_nul(crt_ptr->name, sizeof(crt_ptr->name)) &&
	       player_has_nul(crt_ptr->description, sizeof(crt_ptr->description)) &&
	       player_has_nul(crt_ptr->talk, sizeof(crt_ptr->talk)) &&
	       player_has_nul(crt_ptr->password, sizeof(crt_ptr->password)) &&
	       player_has_nul(crt_ptr->key[0], sizeof(crt_ptr->key[0])) &&
	       player_has_nul(crt_ptr->key[1], sizeof(crt_ptr->key[1])) &&
	       player_has_nul(crt_ptr->key[2], sizeof(crt_ptr->key[2])));
}

static int player_object_strings_valid(obj_ptr)
object *obj_ptr;
{
	return(player_has_nul(obj_ptr->name, sizeof(obj_ptr->name)) &&
	       player_has_nul(obj_ptr->description, sizeof(obj_ptr->description)) &&
	       player_has_nul(obj_ptr->key[0], sizeof(obj_ptr->key[0])) &&
	       player_has_nul(obj_ptr->key[1], sizeof(obj_ptr->key[1])) &&
	       player_has_nul(obj_ptr->key[2], sizeof(obj_ptr->key[2])) &&
	       player_has_nul(obj_ptr->use_output, sizeof(obj_ptr->use_output)));
}

static int player_require_eof(fd)
int fd;
{
	char extra;
	int n;

	do {
		n = read(fd, &extra, sizeof(extra));
	} while(n < 0 && errno == EINTR);
	return(n == 0 ? 0 : -1);
}

static int read_player_obj(fd, obj_ptr, context, depth)
int fd;
object *obj_ptr;
player_decode_context *context;
unsigned int depth;
{
	int cnt, i;
	otag *op;
	object *obj;
	otag **prev;

	if(!obj_ptr || !context)
		return(-1);
	memset(obj_ptr, 0, sizeof(object));
	if(depth > context->max_depth ||
	   context->object_count >= context->max_objects)
		return(-1);
	context->object_count++;
	if(player_read_full(fd, (char *)obj_ptr, sizeof(object)) < 0) {
		reset_player_object_links(obj_ptr);
		goto fail;
	}
	reset_player_object_links(obj_ptr);
	if(!player_object_strings_valid(obj_ptr))
		goto fail;
	if(obj_ptr->shotscur > obj_ptr->shotsmax)
		obj_ptr->shotscur = obj_ptr->shotsmax;
	if(player_read_full(fd, (char *)&cnt, sizeof(int)) < 0)
		goto fail;
	if(cnt < 0 || cnt > MAX_NESTED_OBJECTS ||
	   (unsigned long)cnt > context->max_objects - context->object_count)
		goto fail;

	prev = &obj_ptr->first_obj;
	for(i = 0; i < cnt; ++i) {
		op = (otag *)PLAYER_DECODER_MALLOC(sizeof(otag));
		if(!op)
			goto fail;
		obj = (object *)PLAYER_DECODER_MALLOC(sizeof(object));
		if(!obj) {
			free(op);
			goto fail;
		}
		if(read_player_obj(fd, obj, context, depth + 1) < 0) {
			free(obj);
			free(op);
			goto fail;
		}
		obj->parent_obj = obj_ptr;
		op->obj = obj;
		op->next_tag = 0;
		*prev = op;
		prev = &op->next_tag;
	}
	return(0);

fail:
	free_player_object_list(obj_ptr->first_obj);
	obj_ptr->first_obj = 0;
	return(-1);
}

int read_crt_player(fd, crt_ptr)
int fd;
creature *crt_ptr;
{
	int n, cnt, i;
	otag *op;
	otag **prev;
	object *obj;
	player_decode_context context;

	if(!crt_ptr)
		return(-1);
	memset(crt_ptr, 0, sizeof(creature));
	n = player_read_full(fd, (char *)crt_ptr, sizeof(creature));
	/* Even a short read may have copied hostile bytes over pointer fields. */
	reset_player_creature_links(crt_ptr);
	if(n < 0)
		return(-1);
	if(crt_ptr->type != PLAYER || !player_creature_strings_valid(crt_ptr))
		return(-1);
	/* A disk record must never reattach a player's persisted socket. */
	crt_ptr->fd = -1;
	if(crt_ptr->mpcur > crt_ptr->mpmax)
		crt_ptr->mpcur = crt_ptr->mpmax;
	if(crt_ptr->hpcur > crt_ptr->hpmax)
		crt_ptr->hpcur = crt_ptr->hpmax;
	if(player_read_full(fd, (char *)&cnt, sizeof(int)) < 0)
		return(-1);
	context.object_count = 0;
	context.max_objects = PLAYER_DECODE_MAX_OBJECTS;
	context.max_depth = PLAYER_DECODE_MAX_DEPTH;
	if(cnt < 0 || cnt > MAX_NESTED_OBJECTS ||
	   (unsigned long)cnt > context.max_objects)
		return(-1);

	prev = &crt_ptr->first_obj;
	for(i = 0; i < cnt; ++i) {
		op = (otag *)PLAYER_DECODER_MALLOC(sizeof(otag));
		if(!op)
			goto fail;
		obj = (object *)PLAYER_DECODER_MALLOC(sizeof(object));
		if(!obj) {
			free(op);
			goto fail;
		}
		if(read_player_obj(fd, obj, &context, 1) < 0) {
			free_player_object(obj);
			free(op);
			goto fail;
		}
		obj->parent_crt = crt_ptr;
		op->obj = obj;
		op->next_tag = 0;
		*prev = op;
		prev = &op->next_tag;
	}
	if(player_require_eof(fd) < 0)
		goto fail;
	return(0);

fail:
	free_player_object_list(crt_ptr->first_obj);
	crt_ptr->first_obj = 0;
	return(-1);
}

/**********************************************************************/
/*				read_rom			      */
/**********************************************************************/

/* Loads a room from an already open and positioned file into the memory */
/* pointed to by the second parameter.  Also loaded are all creatures,   */
/* exits, objects and descriptions for the room.  -1 is returned upon    */
/* failure.								 */

int read_rom(fd, rom_ptr)
int 	fd;
room 	*rom_ptr;
{
	int		n, cnt, error=0;
	xtag		*xp;
	xtag		**xprev;
	exit_		*ext;
	ctag		*cp;
	ctag		**cprev;
	creature	*crt;
	otag		*op;
	otag		**oprev;
	object 		*obj;
	char		*sh, *lo, *ob;

	n = read(fd, rom_ptr, sizeof(room));
	if(n < sizeof(room))
		error = 1;

	rom_ptr->short_desc = 0;
	rom_ptr->long_desc  = 0;
	rom_ptr->obj_desc   = 0;
	rom_ptr->first_ext = 0;
	rom_ptr->first_obj = 0;
	rom_ptr->first_mon = 0;
	rom_ptr->first_ply = 0;

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_EXITS) {
		/* Legacy room blobs may carry non-canonical count bytes. */
		cnt = 0;
	}

	/* Load the exits */

	xprev = &rom_ptr->first_ext;
	while(cnt > 0) {
		cnt--;
		xp = (xtag *)malloc(sizeof(xtag));
		if(xp) {
			ext = (exit_ *)malloc(sizeof(exit_));
			if(ext) {
				n = read(fd, ext, sizeof(exit_));
				if(n < sizeof(exit_))
					error = 1;
				xp->ext = ext;
				xp->next_tag = 0;
				*xprev = xp;
				xprev = &xp->next_tag;
			}
			else
				merror("read_rom", FATAL);
		}
		else
			merror("read_rom", FATAL);
	}

	/* Read the monsters */

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_MOBS) {
		/* Treat invalid counts as empty to keep room load non-fatal. */
		cnt = 0;
	}

	cprev = &rom_ptr->first_mon;
	while(cnt > 0) {
		cnt--;
		cp = (ctag *)malloc(sizeof(ctag));
		if(cp) {
			crt = (creature *)malloc(sizeof(creature));
			if(crt) {
				if(read_crt(fd, crt) < 0)
					error = 1;
				crt->parent_rom = rom_ptr;
				cp->crt = crt;
				cp->next_tag = 0;
				*cprev = cp;
				cprev = &cp->next_tag;
			}
			else
				merror("read_rom", FATAL);
		}
		else
			merror("read_rom", FATAL);
	}

	/* Read the items */

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_ITEMS) {
		cnt = 0;
	}

	oprev = &rom_ptr->first_obj;
	while(cnt > 0) {
		cnt--;
		op = (otag *)malloc(sizeof(otag));
		if(op) {
			obj = (object *)malloc(sizeof(object));
			if(obj) {
				if(read_obj(fd, obj) < 0)
					error = 1;
				obj->parent_rom = rom_ptr;
				op->obj = obj;
				op->next_tag = 0;
				*oprev = op;
				oprev = &op->next_tag;
			}
			else
				merror("read_rom", FATAL);
		}
		else
			merror("read_rom", FATAL);
	}

	/* Read the descriptions */

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_DESC_BYTES) {
		cnt = 0;
	}

	if(cnt > 0) {
		sh = (char *)malloc(cnt);
		if(sh) {
			n = read(fd, sh, cnt);
			if(n < cnt)
				error = 1;
			rom_ptr->short_desc = sh;
		}
		else
			merror("read_rom", FATAL);
	}

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_DESC_BYTES) {
		cnt = 0;
	}

	if(cnt > 0) {
		lo = (char *)malloc(cnt);
		if(lo) {
			n = read(fd, lo, cnt);
			if(n < cnt)
				error = 1;
			rom_ptr->long_desc = lo;
		}
		else
			merror("read_rom", FATAL);
	}

	n = read(fd, &cnt, sizeof(int));
	if(n < sizeof(int)) {
		error = 1;
		cnt = 0;
	}
	else if(cnt < 0 || cnt > MAX_ROM_DESC_BYTES) {
		cnt = 0;
	}

	if(cnt > 0) {
		ob = (char *)malloc(cnt);
		if(ob) {
			n = read(fd, ob, cnt);
			if(n < cnt)
				error = 1;
			rom_ptr->obj_desc = ob;
		}
		else
			merror("read_rom", FATAL);
	}

	if(error)
		return(-1);
	else
		return(0);
}

/**********************************************************************/
/*				free_obj			      */
/**********************************************************************/

/* This function is called to release the object pointed to by the first */
/* parameter from memory.  All objects contained within it are also      */
/* released from memory.  *ASSUMPTION*:  This function will only be      */
/* called from free_rom() or free_crt().  Otherwise there may be 	 */
/* unresolved links in memory which would cause a game crash.		 */

void free_obj(obj_ptr)
object	*obj_ptr;
{
	otag	*op, *temp;

	op = obj_ptr->first_obj;
	while(op) {
		temp = op->next_tag;
		free_obj(op->obj);
		free(op);
		op = temp;
	}
	free(obj_ptr);
}

/**********************************************************************/
/*				free_crt			      */
/**********************************************************************/

/* This function releases the creature pointed to by the first parameter */
/* from memory.  All items that creature has readied or carries will     */
/* also be released.  **ASSUMPTION:  This function will only be called   */
/* from free_rom().  If it is called from somewhere else, unresolved     */
/* links may remain and cause a game crash.  *EXCEPTION*: This function  */
/* can be called independently to free a player's information from       */
/* memory (but not a monster).						 */

void free_crt(crt_ptr)
creature	*crt_ptr;
{
	otag	*op, *tempo;
	etag	*ep, *tempe;
	ctag	*cp, *tempc;
	ttag	*tp, *tempt;
	int	i;
	for(i=0; i<20; i++)
		if(crt_ptr->ready[i]) {
			free_obj(crt_ptr->ready[i]);
			crt_ptr->ready[i] = 0;
		}

	op = crt_ptr->first_obj;
	while(op) {
		tempo = op->next_tag;
		free_obj(op->obj);
		free(op);
		op = tempo;
	}

	cp = crt_ptr->first_fol;
	while(cp) {
		tempc = cp->next_tag;
		free(cp);
		cp = tempc;
	}

	ep = crt_ptr->first_enm;
	while(ep) {
		tempe = ep->next_tag;
		free(ep);
		ep = tempe;
	}

	tp = crt_ptr->first_tlk;
	crt_ptr->first_tlk = 0;
	while(tp) {
		tempt = tp->next_tag; 
		if(tp->key) free(tp->key);
		if(tp->response) free(tp->response);
		if(tp->action) free(tp->action);
		if(tp->target) free(tp->target);
		free(tp);
		tp = tempt;
	}	

	if (crt_ptr->type == MONSTER)
		del_active(crt_ptr);

	free(crt_ptr);
}

/**********************************************************************/
/*				free_rom			      */
/**********************************************************************/

/* This function releases the a room and its contents from memory.  The */
/* function is passed the room's pointer as its parameter.  The exits,  */
/* descriptions, objects and monsters are all released from memory.     */

void free_rom(rom_ptr)
room	*rom_ptr;
{
	xtag 	*xp, *xtemp;
	otag	*op, *otemp;
	ctag	*cp, *ctemp;

	free(rom_ptr->short_desc);
	free(rom_ptr->long_desc);
	free(rom_ptr->obj_desc);

	xp = rom_ptr->first_ext;
	while(xp) {
		xtemp = xp->next_tag;
		free(xp->ext);
		free(xp);
		xp = xtemp;
	}

	op = rom_ptr->first_obj;
	while(op) {
		otemp = op->next_tag;
		free_obj(op->obj);
		free(op);
		op = otemp;
	}

	cp = rom_ptr->first_mon;
	while(cp) {
		ctemp = cp->next_tag;
		free_crt(cp->crt);
		free(cp);
		cp = ctemp;
	}

	cp = rom_ptr->first_ply;
	while(cp) {
		ctemp = cp->next_tag;
		free(cp);
		cp = ctemp;
	}

	free(rom_ptr);

}
