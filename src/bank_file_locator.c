/* Fixed FileStore source locator for non-authoritative observers.  It is
 * deliberately separate from bank.c: no gameplay operation or mutable store
 * binding is entered merely to inspect legacy evidence. */
#include "mtype.h"
#include "resource_path.h"
#include "bank_store.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>

int file_bank_store_open_readonly(str)
char *str;
{
    char file[1024];

    if(!str || snprintf(file,sizeof(file),"%s/bank/%s",PLAYERPATH,str)>=
       (int)sizeof(file))
        return -1;
#if !defined(O_NOFOLLOW) || !defined(O_CLOEXEC)
    errno=ENOTSUP;
    return -1;
#else
    return rp_open(file,O_RDONLY|O_BINARY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC,0);
#endif
}
