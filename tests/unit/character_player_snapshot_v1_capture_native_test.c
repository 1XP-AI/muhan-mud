#include "character_player_snapshot_v1_capture_native.h"

#include "player_record_serializer.h"
#include "player_snapshot_v1.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* free_crt references this legacy active-list hook even though a normalized
 * player can never take the MONSTER branch. */
void del_active(creature *player)
{ (void)player;abort(); }

static int expect(value,message)
int value;
const char *message;
{ if(value)return 0;fprintf(stderr,"character_player_snapshot_v1_capture_native_test: %s\n",message);return 1; }

static int write_all(fd,bytes,length)
int fd;const void *bytes;unsigned long length;
{ const char *cursor=(const char *)bytes;ssize_t count;while(length){count=write(fd,cursor,length);if(count<0&&errno==EINTR)continue;if(count<=0)return-1;cursor+=count;length-=(unsigned long)count;}return 0; }

static void fixture(player)
creature *player;
{
    memset(player,0,sizeof(*player));
    player->type=PLAYER;
    player->fd=42;
    player->level=17;
    player->hpmax=100;
    player->hpcur=91;
    player->mpmax=50;
    player->mpcur=41;
    player->gold=12345L;
    strcpy(player->name,"M3alpha");
    strcpy(player->description,"native bridge fixture");
    memset(player->password,0x5a,sizeof(player->password));
    player->password[sizeof(player->password)-1]=0;
}

int main(void)
{
    character_player_snapshot_v1_capture capture;
    player_record_serializer_limits limits;
    creature source,*loaded;
    char bytes[65536],path[]="/tmp/muhan-capture-native-XXXXXX";
    unsigned long length=0;
    int fd,failed=0;

    fixture(&source);
    limits.max_depth=64;
    limits.max_objects=8192;
    if(player_record_serialize_bounded(&source,0,bytes,sizeof(bytes),&length,
       &limits)!=PLAYER_RECORD_SERIALIZER_OK||!length)return 2;
    fd=mkstemp(path);
    if(fd<0||unlink(path)||write_all(fd,bytes,length)||lseek(fd,0,SEEK_SET)<0)
        return 2;
    character_player_snapshot_v1_capture_native_init(&capture);
    loaded=(creature *)1;
    failed+=expect(capture.decode&&capture.release&&
        capture.decode(capture.decode_opaque,fd,&loaded)==0&&loaded&&
        loaded->type==PLAYER&&loaded->fd==-1&&
        player_snapshot_v1_equal_persisted(&source,loaded),
        "native decoder must reconstruct the exact persisted projection from legacy bytes");
    if(loaded&&loaded!=(creature *)1)
        capture.release(capture.release_opaque,loaded);

    if(ftruncate(fd,(off_t)(length-1))||lseek(fd,0,SEEK_SET)<0)return 2;
    loaded=(creature *)1;
    failed+=expect(capture.decode(capture.decode_opaque,fd,&loaded)!=0&&loaded==0,
        "truncated legacy bytes must publish no partial player");
    if(close(fd))failed++;
    return failed?1:0;
}
