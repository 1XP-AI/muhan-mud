#include <stdio.h>
#ifndef WIN32
#include <signal.h>
#endif

extern void m3_runtime_install_idle_hook(void (*hook)(void));
extern void m3_runtime_remove_idle_hook(void);
extern void install_graceful_shutdown_handler(void);
extern void m3_runtime_idle_hook_test_set_shutdown_requested(int requested);
extern void m3_runtime_idle_hook_test_set_sigterm_pending(int pending);
extern void m3_runtime_idle_hook_test_set_sigprocmask_failure(int fail);
extern void m3_runtime_idle_hook_test_set_sigprocmask_restore_failure(int fail);
extern void m3_runtime_idle_hook_test_set_sigterm_observation_failure(int fail);
extern void m3_runtime_idle_hook_test_run(void);

static int tick_calls;

static void test_idle_hook(void)
{
    tick_calls++;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr,"FAIL: %s\n",message);
    return 1;
}

int main(void)
{
    int failed=0;

    m3_runtime_install_idle_hook(test_idle_hook);
    m3_runtime_idle_hook_test_set_shutdown_requested(0);
    m3_runtime_idle_hook_test_set_sigterm_pending(1);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==0,
        "a SIGTERM observed at the serialized idle boundary must prevent tick start");
    m3_runtime_idle_hook_test_set_sigterm_pending(0);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==1,
        "an idle boundary without shutdown must start exactly one tick");
    m3_runtime_idle_hook_test_set_sigprocmask_failure(1);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==1,
        "a sigprocmask failure must fail closed before an unprotected tick starts");
    m3_runtime_idle_hook_test_set_sigprocmask_failure(0);
    m3_runtime_idle_hook_test_set_sigprocmask_restore_failure(1);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==2,
        "a restoration-only sigprocmask failure must follow one protected tick");
    m3_runtime_idle_hook_test_set_sigprocmask_restore_failure(0);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==2,
        "a restoration failure must request shutdown before a later idle tick");
    m3_runtime_idle_hook_test_set_shutdown_requested(0);
    m3_runtime_idle_hook_test_set_sigterm_observation_failure(1);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==2,
        "a pending-SIGTERM observation failure must fail closed before tick start");
    m3_runtime_idle_hook_test_set_sigterm_observation_failure(0);
#ifndef WIN32
    {
        sigset_t term,previous;

        install_graceful_shutdown_handler();
        sigemptyset(&term);
        sigaddset(&term,SIGTERM);
        if(sigprocmask(SIG_BLOCK,&term,&previous)!=0) {
            fprintf(stderr,"FAIL: unable to block SIGTERM for controlled arrival test\n");
            failed=1;
        }
        else {
            raise(SIGTERM);
            m3_runtime_idle_hook_test_set_sigterm_pending(-1);
            m3_runtime_idle_hook_test_run();
            failed|=expect(tick_calls==2,
                "a real blocked SIGTERM arrival must prevent tick start");
            if(sigprocmask(SIG_SETMASK,&previous,0)!=0) {
                fprintf(stderr,"FAIL: unable to restore SIGTERM mask after controlled arrival test\n");
                failed=1;
            }
            m3_runtime_idle_hook_test_set_shutdown_requested(0);
        }
    }
#endif
    m3_runtime_idle_hook_test_set_shutdown_requested(1);
    m3_runtime_idle_hook_test_run();
    failed|=expect(tick_calls==2,
        "a delivered shutdown request must prevent later idle ticks");
    m3_runtime_remove_idle_hook();
    return failed;
}
