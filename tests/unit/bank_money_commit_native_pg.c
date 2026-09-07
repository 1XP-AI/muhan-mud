#include "bank_money_commit_native.h"
#include <libpq-fe.h>
#include <stdio.h>
#include <stdlib.h>
int main(int argc,char **argv)
{
    unsigned char *frame;
    size_t length;
    PGconn *c; PGresult *role; uint64_t revision; int status;
    if(argc!=12) return 2;
    frame=(unsigned char *)malloc(8388617); if(!frame) return 2;
    length=fread(frame,1,8388617,stdin);
    if(ferror(stdin)||!feof(stdin)) { free(frame); return 2; }
    c=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    if(PQstatus(c)!=CONNECTION_OK) { free(frame); PQfinish(c); return 2; }
    role=PQexec(c,"set role mud_writer");
    if(PQresultStatus(role)!=PGRES_COMMAND_OK) { free(frame); PQclear(role); PQfinish(c); return 2; }
    PQclear(role);
    if(getenv("BANK_TRANSFER_PENDING_ROOT") && getenv("BANK_TRANSFER_PENDING_ROOT")[0])
      status=bank_money_commit_prepared_native(c,getenv("BANK_TRANSFER_PENDING_NODE"),getenv("BANK_TRANSFER_PENDING_CLI"),getenv("BANK_TRANSFER_PENDING_ROOT"),
        (const char *const *)(argv+1),frame,length,2000,&revision);
    else status=bank_money_commit_native(c,(const char *const *)(argv+1),frame,length,2000,&revision);
    free(frame); PQfinish(c);
    if(status==BANK_MONEY_COMMIT_CONFIRMED||status==BANK_MONEY_COMMIT_RETRY) {
        printf("%s %llu\n",status==BANK_MONEY_COMMIT_CONFIRMED?"COMMITTED":"EXACT_RETRY",(unsigned long long)revision);
        return 0;
    }
    return status==BANK_MONEY_COMMIT_REJECTED?1:status==BANK_MONEY_COMMIT_UNKNOWN?3:status==BANK_MONEY_COMMIT_NOT_SENT?4:2;
}
