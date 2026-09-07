#include "bank_transfer_snapshot_v1.h"
#include "cdto_v1.h"
#include <stdlib.h>
#include <string.h>

#ifdef BANK_TRANSFER_SNAPSHOT_V1_TESTING
static int fail_allocation;
void bank_transfer_snapshot_v1_test_fail_allocation(int fail) { fail_allocation=fail; }
#endif
static void write32(unsigned char *out,size_t value)
{
    out[0]=(unsigned char)(value>>24); out[1]=(unsigned char)(value>>16);
    out[2]=(unsigned char)(value>>8); out[3]=(unsigned char)value;
}
int bank_transfer_snapshot_v1_encode(const creature *player,const otag *bank,
    unsigned char **wire,size_t *length)
{
    unsigned char *p=NULL,*b=NULL,*frame=NULL;
    size_t pl=0,bl=0;
    int status;
    if(wire) *wire=NULL;
    if(length) *length=0;
    if(!wire || !length || !player || !bank) return CDTO_V1_INVALID_ARGUMENT;
    status=player_snapshot_v1_encode_loaded(player,&p,&pl);
    if(status!=CDTO_V1_OK) goto done;
    status=bank_snapshot_v1_encode(bank,&b,&bl);
    if(status!=CDTO_V1_OK) goto done;
    if(pl>4194304U || bl>4194304U) { status=CDTO_V1_SIZE_LIMIT_EXCEEDED; goto done; }
#ifdef BANK_TRANSFER_SNAPSHOT_V1_TESTING
    if(!fail_allocation)
#endif
        frame=(unsigned char *)malloc(8+pl+bl);
    if(!frame) { status=CDTO_V1_ALLOCATION_FAILED; goto done; }
    write32(frame,pl); write32(frame+4,bl);
    memcpy(frame+8,p,pl); memcpy(frame+8+pl,b,bl);
    *wire=frame; *length=8+pl+bl;
done:
    cdto_v1_free_wire(p); bank_snapshot_v1_free_wire(b);
    return status;
}
