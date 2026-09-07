#ifndef MUHAN_PLAYER_PAIRED_LOAD_NATIVE_H
#define MUHAN_PLAYER_PAIRED_LOAD_NATIVE_H
struct creature;
/* PlayerStore load callback; context is player_paired_route_context from a
 * successful selector. Returns a detached owned canonical clone only after
 * route/revision revalidation and complete codec/name validation. Does not
 * restore credentials, attach sessions or install the gameplay loader. */
int player_paired_load_native(void *,char *,struct creature **);
#endif
