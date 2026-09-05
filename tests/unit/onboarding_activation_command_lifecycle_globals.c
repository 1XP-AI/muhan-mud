/* Keep the focused command handler harness independent from global.c's
 * command table.  The descriptor globals retain their production layout. */
#include "mstruct.h"

int Tablesize;
int Spy[PMAX];
struct {
    creature *ply;
    iobuf *io;
    extra *extr;
} Ply[PMAX];
