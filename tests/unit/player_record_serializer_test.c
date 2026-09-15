#include <stdio.h>
#include <string.h>
#include <unistd.h>

#include "mstruct.h"
#include "player_record_serializer.h"

extern int write_crt(int fd, creature *crt_ptr, char perm_only);

#define TEST_BUFFER_SIZE 65536

static int expect(int condition, const char *message)
{
	if(condition)
		return(0);
	fprintf(stderr, "player_record_serializer_test: %s\n", message);
	return(1);
}

static void init_limits(player_record_serializer_limits *limits)
{
	limits->max_depth = 64;
	limits->max_objects = 8192;
}

static int descriptor_bytes(creature *player, char perm_only,
	unsigned char *buffer, unsigned long capacity, unsigned long *written)
{
	int fds[2];
	int n;
	unsigned long total;

	*written = 0;
	if(pipe(fds) < 0)
		return(-1);
	if(write_crt(fds[1], player, perm_only) < 0) {
		close(fds[0]);
		close(fds[1]);
		return(-1);
	}
	close(fds[1]);
	total = 0;
	while((n = (int)read(fds[0], buffer + total, capacity - total)) > 0) {
		total += (unsigned long)n;
		if(total == capacity) {
			close(fds[0]);
			return(-1);
		}
	}
	close(fds[0]);
	if(n < 0)
		return(-1);
	*written = total;
	return(0);
}

static int compare_descriptor(creature *player, char perm_only,
	const char *case_name)
{
	unsigned char descriptor[TEST_BUFFER_SIZE];
	unsigned char bounded[TEST_BUFFER_SIZE];
	unsigned long descriptor_size;
	unsigned long bounded_size;
	player_record_serializer_limits limits;
	int failed;

	init_limits(&limits);
	failed = 0;
	failed += expect(descriptor_bytes(player, perm_only, descriptor,
					 TEST_BUFFER_SIZE, &descriptor_size) == 0, case_name);
	failed += expect(player_record_serialize_bounded(player, perm_only,
					(char *)bounded, sizeof(bounded), &bounded_size,
					&limits) == PLAYER_RECORD_SERIALIZER_OK, case_name);
	failed += expect(descriptor_size == bounded_size, case_name);
	failed += expect(descriptor_size == 0 ||
			memcmp(descriptor, bounded, descriptor_size) == 0, case_name);
	return(failed);
}

static int test_differential_graphs(void)
{
	creature player;
	object first, second, container, nested;
	otag first_tag, second_tag, container_tag, nested_tag;
	int failed;

	failed = 0;
	memset(&player, 0, sizeof(player));
	failed += compare_descriptor(&player, 0, "empty record differs from write_crt");

	memset(&first, 0, sizeof(first));
	memset(&second, 0, sizeof(second));
	first.value = 101;
	second.value = 202;
	first_tag.obj = &first;
	first_tag.next_tag = &second_tag;
	second_tag.obj = &second;
	second_tag.next_tag = 0;
	player.first_obj = &first_tag;
	failed += compare_descriptor(&player, 0, "multi-item ordering differs from write_crt");

	memset(&container, 0, sizeof(container));
	memset(&nested, 0, sizeof(nested));
	container.value = 303;
	nested.value = 404;
	nested_tag.obj = &nested;
	nested_tag.next_tag = 0;
	container.first_obj = &nested_tag;
	container_tag.obj = &container;
	container_tag.next_tag = 0;
	player.first_obj = &container_tag;
	failed += compare_descriptor(&player, 0, "nested container differs from write_crt");

	memset(&first, 0, sizeof(first));
	memset(&second, 0, sizeof(second));
	F_SET(&first, OPERMT);
	F_SET(&second, OPERM2);
	first.value = 505;
	second.value = 606;
	first_tag.obj = &first;
	first_tag.next_tag = &second_tag;
	second_tag.obj = &second;
	second_tag.next_tag = 0;
	player.first_obj = &first_tag;
	failed += compare_descriptor(&player, 1, "perm_only differs from write_crt");
	return(failed);
}

static int test_rejections(void)
{
	creature player;
	object item;
	otag tag;
	unsigned char buffer[sizeof(creature) + sizeof(int) + sizeof(object) + sizeof(int)];
	unsigned long written;
	player_record_serializer_limits limits;
	int failed;

	failed = 0;
	memset(&player, 0, sizeof(player));
	memset(&item, 0, sizeof(item));
	tag.obj = &item;
	tag.next_tag = 0;
	player.first_obj = &tag;
	init_limits(&limits);

	written = 99;
	failed += expect(player_record_serialize_bounded(0, 0, (char *)buffer,
		sizeof(buffer), &written, &limits) == PLAYER_RECORD_SERIALIZER_INVALID &&
		written == 0, "null creature must fail without a result");
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, 0, 0,
		sizeof(buffer), &written, &limits) == PLAYER_RECORD_SERIALIZER_INVALID &&
		written == 0, "null buffer must fail without a result");
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, (char)-1,
		(char *)buffer, sizeof(buffer), &written, &limits) ==
		PLAYER_RECORD_SERIALIZER_INVALID && written == 0,
		"non-boolean perm_only must not claim write_crt parity");

	memset(buffer, 0xa5, sizeof(buffer));
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, 0, (char *)buffer,
		sizeof(buffer) - 1, &written, &limits) == PLAYER_RECORD_SERIALIZER_NO_SPACE &&
		written == 0 && buffer[0] == 0xa5 && buffer[sizeof(buffer) - 1] == 0xa5,
		"short capacity must not expose partial output");

	limits.max_depth = 0;
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, 0, (char *)buffer,
		sizeof(buffer), &written, &limits) == PLAYER_RECORD_SERIALIZER_DEPTH_LIMIT &&
		written == 0, "depth budget must reject the graph");
	init_limits(&limits);
	limits.max_objects = 0;
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, 0, (char *)buffer,
		sizeof(buffer), &written, &limits) == PLAYER_RECORD_SERIALIZER_OBJECT_LIMIT &&
		written == 0, "object budget must reject the graph");

	init_limits(&limits);
	tag.obj = 0;
	written = 99;
	failed += expect(player_record_serialize_bounded(&player, 0, (char *)buffer,
		sizeof(buffer), &written, &limits) == PLAYER_RECORD_SERIALIZER_INCONSISTENT &&
		written == 0, "null tagged object must reject inconsistent counts");
	tag.obj = &item;
	return(failed);
}

int main(void)
{
	int failed;

	failed = test_differential_graphs();
	failed += test_rejections();
	if(failed)
		return(1);
	puts("player_record_serializer_test: ok");
	return(0);
}
