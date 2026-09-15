#include "bank_money_read_native.h"
#include <libpq-fe.h>
#include <stdlib.h>
#include <stdio.h>
int main(int argc,char **argv)
{
    PGconn *c;
    PGresult *role;
    bank_money_read_result result;
    int status;
    if(argc!=8) return 2;
    /* Connection setup deadline is independent of the adapter query deadline. */
    c=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    if(PQstatus(c)!=CONNECTION_OK) { PQfinish(c); return 2; }
    role=PQexec(c,"set role mud_writer");
    if(PQresultStatus(role)!=PGRES_COMMAND_OK) { PQclear(role); PQfinish(c); return 2; }
    PQclear(role);
    status=bank_money_read_native(c,(const char *const *)(argv+1),2000,&result);
    PQfinish(c);
    if(status!=0) return 1;
    fprintf(stderr,"%llu %s %s\n",(unsigned long long)result.revision,result.player_hash,result.bank_hash);
    if(fwrite(result.frame,1,result.frame_length,stdout)!=result.frame_length) { free(result.frame); return 2; }
    free(result.frame);
    return 0;
}
