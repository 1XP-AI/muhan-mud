#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "mstruct.h"

extern void add_permobj_rom(room *);
extern void add_permcrt_rom(room *);
extern void check_exits(room *);

static time_t fake_now;
static int load_obj_calls;
static int load_crt_calls;

time_t time(time_t *timer)
{
	if(timer)
		*timer = fake_now;
	return fake_now;
}

int load_obj(short index, object **obj_ptr)
{
	load_obj_calls++;
	*obj_ptr = 0;
	return -1;
}

int load_crt(short index, creature **crt_ptr)
{
	load_crt_calls++;
	*crt_ptr = 0;
	return -1;
}

void free_obj(object *obj_ptr)
{
}

void free_crt(creature *crt_ptr)
{
}

void rand_enchant(object *obj_ptr)
{
}

void add_obj_crt(object *obj_ptr, creature *crt_ptr)
{
}

void add_active(creature *crt_ptr)
{
}

void broadcast_rom(int fd, int rom_num, char *fmt, ...)
{
}

void merror(char *str, char errtype)
{
}

static int expect(int condition, const char *message)
{
	if(condition)
		return 0;
	fprintf(stderr, "room_time_test: %s\n", message);
	return 1;
}

static void reset_room(room *rom_ptr)
{
	memset(rom_ptr, 0, sizeof(*rom_ptr));
	rom_ptr->rom_num = 1;
	load_obj_calls = 0;
	load_crt_calls = 0;
}

static int test_permcrt_boundaries()
{
	room rom;
	int failed;

	failed = 0;
	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[0].ltime = 100;
	rom.perm_mon[0].interval = 10;
	fake_now = 110;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 1,
		"permanent monsters must expire exactly at their expiry time");

	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[1].misc = 1;
	rom.perm_mon[0].ltime = rom.perm_mon[1].ltime = 100;
	rom.perm_mon[0].interval = rom.perm_mon[1].interval = 10;
	fake_now = 110;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 2,
		"monsters due exactly now must retain the legacy non-grouped behavior");

	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[1].misc = 1;
	rom.perm_mon[0].ltime = rom.perm_mon[1].ltime = 100;
	rom.perm_mon[0].interval = rom.perm_mon[1].interval = 10;
	fake_now = 111;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 1,
		"monsters before now must retain the legacy grouped behavior");

	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[0].ltime = 100;
	rom.perm_mon[0].interval = 10;
	fake_now = 109;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 0,
		"permanent monsters must not expire before their expiry time");

	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[0].ltime = LONG_MAX;
	rom.perm_mon[0].interval = 1;
	fake_now = LONG_MAX;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 0,
		"permanent monsters must not expire when ltime + interval exceeds LONG_MAX");

	reset_room(&rom);
	rom.perm_mon[0].misc = 1;
	rom.perm_mon[0].ltime = LONG_MIN;
	rom.perm_mon[0].interval = -1;
	fake_now = LONG_MIN;
	add_permcrt_rom(&rom);
	failed += expect(load_crt_calls == 1,
		"permanent monsters must expire when ltime + interval falls below LONG_MIN");

	return failed;
}

static int test_permobj_boundaries()
{
	room rom;
	int failed;

	failed = 0;
	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[0].ltime = 100;
	rom.perm_obj[0].interval = 10;
	fake_now = 110;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 1,
		"permanent objects must retain their legacy expiry-at-now behavior");

	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[1].misc = 1;
	rom.perm_obj[0].ltime = rom.perm_obj[1].ltime = 100;
	rom.perm_obj[0].interval = rom.perm_obj[1].interval = 10;
	fake_now = 110;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 2,
		"objects due exactly now must retain the legacy non-grouped behavior");

	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[1].misc = 1;
	rom.perm_obj[0].ltime = rom.perm_obj[1].ltime = 100;
	rom.perm_obj[0].interval = rom.perm_obj[1].interval = 10;
	fake_now = 111;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 1,
		"objects before now must retain the legacy grouped behavior");

	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[0].ltime = 100;
	rom.perm_obj[0].interval = 10;
	fake_now = 109;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 0,
		"permanent objects must not reload before their expiry time");

	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[0].ltime = LONG_MAX;
	rom.perm_obj[0].interval = 1;
	fake_now = LONG_MAX;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 0,
		"permanent objects must not expire when ltime + interval exceeds LONG_MAX");

	reset_room(&rom);
	rom.perm_obj[0].misc = 1;
	rom.perm_obj[0].ltime = LONG_MIN;
	rom.perm_obj[0].interval = -1;
	fake_now = LONG_MIN;
	add_permobj_rom(&rom);
	failed += expect(load_obj_calls == 1,
		"permanent objects must expire when ltime + interval falls below LONG_MIN");

	return failed;
}

static int test_exit_boundaries()
{
	room rom;
	exit_ ext;
	xtag tag;
	int failed;

	failed = 0;
	memset(&rom, 0, sizeof(rom));
	memset(&ext, 0, sizeof(ext));
	memset(&tag, 0, sizeof(tag));
	tag.ext = &ext;
	rom.first_ext = &tag;
	F_SET(&ext, XLOCKS);
	ext.ltime.ltime = 100;
	ext.ltime.interval = 10;
	fake_now = 110;
	check_exits(&rom);
	failed += expect(!F_ISSET(&ext, XLOCKD) && !F_ISSET(&ext, XCLOSD),
		"locked exits must retain strict-after-expiry behavior");

	fake_now = 111;
	check_exits(&rom);
	failed += expect(F_ISSET(&ext, XLOCKD) && F_ISSET(&ext, XCLOSD),
		"locked exits must close after their expiry time");

	memset(&ext, 0, sizeof(ext));
	F_SET(&ext, XCLOSS);
	ext.ltime.ltime = LONG_MAX;
	ext.ltime.interval = 1;
	fake_now = LONG_MAX;
	check_exits(&rom);
	failed += expect(!F_ISSET(&ext, XCLOSD),
		"closable exits must not expire when ltime + interval exceeds LONG_MAX");

	memset(&ext, 0, sizeof(ext));
	F_SET(&ext, XCLOSS);
	ext.ltime.ltime = LONG_MIN;
	ext.ltime.interval = -1;
	fake_now = LONG_MIN;
	check_exits(&rom);
	failed += expect(F_ISSET(&ext, XCLOSD),
		"closable exits must expire when ltime + interval falls below LONG_MIN");

	return failed;
}

int main(void)
{
	int failed;

	failed = test_permcrt_boundaries();
	failed += test_permobj_boundaries();
	failed += test_exit_boundaries();
	return failed ? 1 : 0;
}
