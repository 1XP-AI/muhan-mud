#include "mstruct.h"
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
int bank_command_success;
long bank_command_amount,bank_command_balance;
int print(int fd,char *format,...)
{
    va_list args; (void)fd;va_start(args,format);
    if(!strcmp(format,"당신은 %ld냥을 출금했습니다.\n")||!strcmp(format,"당신은 %ld냥을 입금했습니다.\n")) {
        bank_command_success++;bank_command_amount=va_arg(args,long);
    }
    if(!strcmp(format,"은행의 잔고가 %ld냥이 되었습니다.")) bank_command_balance=va_arg(args,long);
    va_end(args);return 0;
}
/* Any legacy storage/parser call in a selected DB command fails the test. */
int bank_store_load(char *name,object **out) {(void)name;(void)out;abort();}
int bank_store_save(char *name,object *bank) {(void)name;(void)bank;abort();}
int savegame_nomsg(creature *p) {(void)p;abort();}
void free_obj(object *p) {(void)p;abort();}
void zero(void *p,int n) {(void)p;(void)n;abort();}
int utf8_ends_with(unsigned char *s,unsigned char *suffix) {(void)s;(void)suffix;abort();}
