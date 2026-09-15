#include "mstruct.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <stdio.h>
extern void uninit_ply(creature *);
/* Nonparticipating room/combat/network effects fail loudly if reached.
 * The actual uninit/update/add_obj/clear_enm functions are linked below. */
struct {creature *ply;iobuf *io;extra *extr;} Ply[PMAX];
long all_broad_time;
int bonus[35];short thaco_list[1][20];
void del_ply_rom(creature *p,room *r) {(void)p;(void)r;abort();}
void print(int fd,char *fmt,...) {(void)fd;(void)fmt;abort();}
void broadcast(char *fmt,...) {(void)fmt;abort();}
void broadcast_rom(int fd,int room_id,char *fmt,...) {(void)fd;(void)room_id;(void)fmt;abort();}
void log_f(char *fmt,...) {(void)fmt;}
void log_dm(char *fmt,...) {(void)fmt;abort();}
void merror(char *fmt,char kind) {(void)fmt;(void)kind;abort();}
int find_enm_crt(char *name,creature *p) {(void)name;(void)p;abort();}
void del_enm_crt(char *name,creature *p) {(void)name;(void)p;abort();}
int dice(int n,int sides,int plus) {(void)n;(void)sides;(void)plus;abort();}
void die(creature *p) {(void)p;abort();}
static int periodic_saves;
int savegame(creature *p)
{
    assert(!p->ready[0]&&p->first_obj&&p->first_obj->obj->parent_crt==p);
    assert(p->lasttime[LT_HOURS].ltime==1000&&p->lasttime[LT_HOURS].interval==17);
    periodic_saves++;return 0;
}
time_t time(time_t *out) { if(out) *out=1000; return 1000; }
int main(void)
{
    creature p;object blade;otag *tag;
    memset(&p,0,sizeof(p));memset(&blade,0,sizeof(blade));
    strcpy(p.name,"Exitplayer");strcpy(blade.name,"Zblade");
    p.fd=-1;p.ready[0]=&blade;p.lasttime[LT_HOURS].ltime=990;
    p.lasttime[LT_HOURS].interval=7;
    F_SET(&p,PDMINV);
    uninit_ply(&p);
    assert(!p.ready[0]&&p.first_obj&&p.first_obj->obj==&blade);
    assert(!p.first_obj->next_tag&&blade.parent_crt==&p);
    assert(p.lasttime[LT_HOURS].ltime==1000&&p.lasttime[LT_HOURS].interval==17);
    assert(periodic_saves==1&&p.lasttime[LT_PSAVE].ltime==1000);
    /* At the same time the periodic save is no longer due; no duplicate gear. */
    uninit_ply(&p);
    assert(periodic_saves==1&&!p.first_obj->next_tag&&!p.ready[0]);
    tag=p.first_obj;free(tag);
    puts("player_uninit_real_test: actual equipment, elapsed time and nested periodic-save ordering passed");
    return 0;
}
