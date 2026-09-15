/* Bounded, allocation-free encoder for the legacy write_crt format. */
#include "mstruct.h"
#include "player_record_serializer.h"

#include <limits.h>
#include <string.h>

#define PLAYER_RECORD_ROOT_OBJECT_LIMIT 200UL

typedef struct player_record_serializer_plan {
	const player_record_serializer_limits *limits;
	unsigned long size;
	unsigned long tag_count;
	unsigned long object_count;
} player_record_serializer_plan;

static int object_is_saved(object *obj_ptr, char perm_only)
{
	return(!perm_only || F_ISSET(obj_ptr, OPERMT));
}

static int plan_add_size(player_record_serializer_plan *plan,
	unsigned long amount)
{
	if(amount > ULONG_MAX - plan->size)
		return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
	plan->size += amount;
	return(PLAYER_RECORD_SERIALIZER_OK);
}

/* Tag visits are bounded as well as emitted objects.  This prevents a
 * malformed linked list made only of excluded objects from looping forever. */
static int plan_tag(player_record_serializer_plan *plan)
{
	if(plan->tag_count == ULONG_MAX)
		return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
	plan->tag_count++;
	if(plan->tag_count > plan->limits->max_objects)
		return(PLAYER_RECORD_SERIALIZER_OBJECT_LIMIT);
	return(PLAYER_RECORD_SERIALIZER_OK);
}

static int plan_object(object *obj_ptr, char perm_only, unsigned long depth,
	player_record_serializer_plan *plan)
{
	otag *op;
	int count;
	int result;

	if(!obj_ptr)
		return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
	if(depth > plan->limits->max_depth)
		return(PLAYER_RECORD_SERIALIZER_DEPTH_LIMIT);
	if(plan->object_count == ULONG_MAX)
		return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
	plan->object_count++;
	if(plan->object_count > plan->limits->max_objects)
		return(PLAYER_RECORD_SERIALIZER_OBJECT_LIMIT);
	result = plan_add_size(plan, (unsigned long)sizeof(object));
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);
	result = plan_add_size(plan, (unsigned long)sizeof(int));
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);

	count = 0;
	op = obj_ptr->first_obj;
	while(op) {
		result = plan_tag(plan);
		if(result != PLAYER_RECORD_SERIALIZER_OK)
			return(result);
		if(!op->obj)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(object_is_saved(op->obj, perm_only)) {
			if(count == INT_MAX)
				return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
			count++;
			if(depth == ULONG_MAX)
				return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
			result = plan_object(op->obj, perm_only, depth + 1, plan);
			if(result != PLAYER_RECORD_SERIALIZER_OK)
				return(result);
		}
		op = op->next_tag;
	}
	return(PLAYER_RECORD_SERIALIZER_OK);
}

static int plan_record(creature *crt_ptr, char perm_only,
	player_record_serializer_plan *plan, int *root_count)
{
	otag *op;
	int count;
	int result;

	result = plan_add_size(plan, (unsigned long)sizeof(creature));
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);
	result = plan_add_size(plan, (unsigned long)sizeof(int));
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);
	count = 0;
	op = crt_ptr->first_obj;
	while(op) {
		result = plan_tag(plan);
		if(result != PLAYER_RECORD_SERIALIZER_OK)
			return(result);
		if(!op->obj)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(object_is_saved(op->obj, perm_only)) {
			if(count == INT_MAX)
				return(PLAYER_RECORD_SERIALIZER_OVERFLOW);
			count++;
			if((unsigned long)count <= PLAYER_RECORD_ROOT_OBJECT_LIMIT) {
				result = plan_object(op->obj, perm_only, 1, plan);
				if(result != PLAYER_RECORD_SERIALIZER_OK)
					return(result);
			}
		}
		op = op->next_tag;
	}
	if((unsigned long)count > PLAYER_RECORD_ROOT_OBJECT_LIMIT)
		count = (int)PLAYER_RECORD_ROOT_OBJECT_LIMIT;
	*root_count = count;
	return(PLAYER_RECORD_SERIALIZER_OK);
}

static int emit_object(char **cursor, object *obj_ptr, char perm_only,
	const player_record_serializer_limits *limits, unsigned long depth,
	unsigned long *object_count, unsigned long *tag_count,
	unsigned long *remaining)
{
	otag *op;
	int count;
	int result;
	char *count_cursor;

	if(!obj_ptr || depth > limits->max_depth)
		return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
	if(*object_count == ULONG_MAX || ++*object_count > limits->max_objects)
		return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
	if(*remaining < (unsigned long)sizeof(object) + (unsigned long)sizeof(int))
		return(PLAYER_RECORD_SERIALIZER_NO_SPACE);
	memcpy(*cursor, obj_ptr, sizeof(object));
	*cursor += sizeof(object);
	*remaining -= (unsigned long)sizeof(object);
	count_cursor = *cursor;
	*cursor += sizeof(int);
	*remaining -= (unsigned long)sizeof(int);
	count = 0;
	op = obj_ptr->first_obj;
	while(op) {
		if(*tag_count == ULONG_MAX || ++*tag_count > limits->max_objects)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(!op->obj)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(object_is_saved(op->obj, perm_only)) {
			if(count == INT_MAX || depth == ULONG_MAX)
				return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
			count++;
			result = emit_object(cursor, op->obj, perm_only, limits,
				depth + 1, object_count, tag_count, remaining);
			if(result != PLAYER_RECORD_SERIALIZER_OK)
				return(result);
		}
		op = op->next_tag;
	}
	memcpy(count_cursor, &count, sizeof(int));
	return(PLAYER_RECORD_SERIALIZER_OK);
}

static int emit_record(creature *crt_ptr, char perm_only, char *buffer,
	const player_record_serializer_limits *limits, unsigned long expected_size,
	int expected_root_count)
{
	char *cursor;
	char *count_cursor;
	otag *op;
	unsigned long object_count;
	unsigned long tag_count;
	unsigned long remaining;
	int count;
	int result;

	cursor = buffer;
	remaining = expected_size;
	if(remaining < (unsigned long)sizeof(creature) + (unsigned long)sizeof(int))
		return(PLAYER_RECORD_SERIALIZER_NO_SPACE);
	memcpy(cursor, crt_ptr, sizeof(creature));
	cursor += sizeof(creature);
	remaining -= (unsigned long)sizeof(creature);
	count_cursor = cursor;
	cursor += sizeof(int);
	remaining -= (unsigned long)sizeof(int);
	object_count = 0;
	tag_count = 0;
	count = 0;
	op = crt_ptr->first_obj;
	while(op) {
		if(tag_count == ULONG_MAX || ++tag_count > limits->max_objects)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(!op->obj)
			return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
		if(object_is_saved(op->obj, perm_only)) {
			if(count == INT_MAX)
				return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
			count++;
			if((unsigned long)count <= PLAYER_RECORD_ROOT_OBJECT_LIMIT) {
			result = emit_object(&cursor, op->obj, perm_only, limits, 1,
					&object_count, &tag_count, &remaining);
				if(result != PLAYER_RECORD_SERIALIZER_OK)
					return(result);
			}
		}
		op = op->next_tag;
	}
	if((unsigned long)count > PLAYER_RECORD_ROOT_OBJECT_LIMIT)
		count = (int)PLAYER_RECORD_ROOT_OBJECT_LIMIT;
	if(count != expected_root_count || remaining != 0 ||
	   (unsigned long)(cursor - buffer) != expected_size)
		return(PLAYER_RECORD_SERIALIZER_INCONSISTENT);
	memcpy(count_cursor, &count, sizeof(int));
	return(PLAYER_RECORD_SERIALIZER_OK);
}

int player_record_serialize_bounded(struct creature *crt_ptr, char perm_only,
	char *buffer, unsigned long capacity, unsigned long *written,
	const player_record_serializer_limits *limits)
{
	player_record_serializer_plan plan;
	int root_count;
	int result;

	if(written)
		*written = 0;
	if(!crt_ptr || !buffer || !written || !limits ||
	   (perm_only != 0 && perm_only != 1))
		return(PLAYER_RECORD_SERIALIZER_INVALID);
	plan.limits = limits;
	plan.size = 0;
	plan.tag_count = 0;
	plan.object_count = 0;
	result = plan_record(crt_ptr, perm_only, &plan, &root_count);
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);
	if(plan.size > capacity)
		return(PLAYER_RECORD_SERIALIZER_NO_SPACE);
	result = emit_record(crt_ptr, perm_only, buffer, limits, plan.size,
		root_count);
	if(result != PLAYER_RECORD_SERIALIZER_OK)
		return(result);
	*written = plan.size;
	return(PLAYER_RECORD_SERIALIZER_OK);
}
