#define _GNU_SOURCE
#include "bank_money_live_snapshot.h"
#include "player_record_serializer.h"
#include "player_snapshot_v1.h"
#include <stdlib.h>
#ifdef __linux__
#include <sys/mman.h>
#include <unistd.h>
#include <errno.h>
extern int read_crt_player(int,creature *);
#endif
int bank_money_live_snapshot(const creature *player,unsigned char **out,size_t *length)
{
    if(out) *out=NULL;
    if(length) *length=0;
    if(!out||!length||!player||player->type!=PLAYER) return -1;
#ifdef __linux__
    {
        const size_t capacity=8388608;
        player_record_serializer_limits limits={64,4096};
        unsigned char *raw=NULL,*wire=NULL; size_t used=0,wire_length=0;
        unsigned long written=0; int fd=-1,status=-1; ssize_t n;
        creature *decoded=NULL; volatile unsigned char *wipe;
        raw=(unsigned char *)malloc(capacity); decoded=(creature *)calloc(1,sizeof(*decoded));
        if(!raw||!decoded) goto done;
        /* Serializer accepts a historical non-const pointer but does not mutate. */
        if(player_record_serialize_bounded((creature *)player,0,(char *)raw,capacity,&written,&limits)) goto done;
        fd=memfd_create("muhan-bank-normalize",MFD_CLOEXEC); if(fd<0) goto done;
        while(used<written) {
            n=write(fd,raw+used,written-used);
            if(n<0&&errno==EINTR) continue;
            if(n<=0) goto done;
            used+=(size_t)n;
        }
        if(lseek(fd,0,SEEK_SET)<0||read_crt_player(fd,decoded)<0) goto done;
        if(player_snapshot_v1_encode_loaded(decoded,&wire,&wire_length)) goto done;
        status=0;
done:
        if(fd>=0&&close(fd)<0) status=-1;
        /* The intermediate legacy image contains credentials, unlike CDTO. */
        wipe=raw; while(written && wipe) { *wipe++=0; written--; }
        free(raw); player_snapshot_v1_free_clone(decoded);
        if(status) {free(wire); return -1;}
        *out=wire; *length=wire_length; return 0;
    }
#else
    return -1;
#endif
}
