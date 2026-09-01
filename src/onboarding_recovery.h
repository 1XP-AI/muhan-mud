#ifndef ONBOARDING_RECOVERY_H
#define ONBOARDING_RECOVERY_H

/* Before the listener is opened, complete the only recoverable MUD1O crash
 * window: a player-v1 file was atomically published while its receipt still
 * says pending.  This is intentionally a startup-only operation. */
int onboarding_recovery_startup(void);

#endif
