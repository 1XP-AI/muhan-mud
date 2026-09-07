/* Native ABI metadata only. Never reads a player file or prints its fields. */
#include <stddef.h>
#include <stdio.h>
#include "mstruct.h"

int main(void)
{
    printf("{\"passwordOffset\":%lu,\"passwordLength\":%lu}\n",
        (unsigned long)offsetof(creature, password),
        (unsigned long)sizeof(((creature *)0)->password));
    return 0;
}
