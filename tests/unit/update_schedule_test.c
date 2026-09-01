#include <stdio.h>

extern void init_update_game(long t);
extern int update_game_spawn_schedule_is(long t);
extern long last_exit_update;

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "update_schedule_test: %s\n", message);
    return 1;
}

int main(void)
{
    int failed = 0;

    last_exit_update = 0;
    init_update_game(123456L);

    failed += expect(update_game_spawn_schedule_is(123456L),
                     "startup must baseline all three random world spawners");
    failed += expect(last_exit_update == 0,
                     "startup must retain legacy first-tick exit maintenance");
    return failed ? 1 : 0;
}
