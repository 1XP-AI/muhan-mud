#include "bank_money_plan_native.h"
#include <stdio.h>
#include <stdlib.h>
int main(int argc,char **argv)
{
    unsigned char *input,*output=NULL;
    size_t length,out_length=0; int result;
    if(argc!=5) return 2;
    input=(unsigned char *)malloc(8388617); if(!input) return 2;
    length=fread(input,1,8388617,stdin);
    if(ferror(stdin)||!feof(stdin)) { free(input); return 2; }
    result=bank_money_plan_native(getenv("BANK_TRANSFER_PLANNER"),(const char *const *)(argv+1),input,length,2000,&output,&out_length);
    free(input);
    if(result) { if(output||out_length) return 2; return 1; }
    result=fwrite(output,1,out_length,stdout)==out_length?0:2;
    free(output); return result;
}
