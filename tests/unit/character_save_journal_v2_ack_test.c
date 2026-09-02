#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_publish.h"
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char W[]="m3-contract", I[]="11111111-1111-4111-8111-111111111111", C[]="22222222-2222-4222-8222-222222222222", CMD[]="40000000-0000-0000-0000-000000000001", BYTES[]="091b-3 exact published bytes\n";
static const unsigned char NAME[]="M3hero";
static const char GOLDEN_POST[]="604219f87e7a86f7ac2b8435e6d291f316193ccc0076428425ceb0c1ec87b135";
static const char GOLDEN_EXPECTED[]="a60c95de8a968a1ff5623101317e9893c745a22b9146ab4e24a692b22c05a30d";
static const char GOLDEN_ABSENT_REQUEST[]="f73bd6f78d85ff7d65a22df5a2d601a5384c4593ac39b2452bc2cfab0f52a82e";
static const char GOLDEN_EXISTING_REQUEST[]="843091b620e44c3b7258b22a0910de4f7e36f47d5a130f93e6d581ffa3847572";
typedef struct mock { int calls, reopen_writer, mutate_live, expect_existing; const char *root; character_save_journal_v2_writer_context *context; character_save_journal_v2_receipt_result result; } mock;
static int bad(ok,s) int ok;const char*s; {if(ok)return 0;fprintf(stderr,"ack: %s\n",s);return 1;}
static int p(out,n,root,rel) char*out;size_t n;const char*root,*rel; {int r=snprintf(out,n,"%s/%s",root,rel);return r<0||(size_t)r>=n?-1:0;}
static int wa(fd,b,n) int fd;const void*b;size_t n; {const char*q=b;ssize_t r;while(n){r=write(fd,q,n);if(r<0&&errno==EINTR)continue;if(r<=0)return -1;q+=r;n-=(size_t)r;}return 0;}
static int leaf(root,rel,b,n) const char*root,*rel,*b;size_t n; {char x[PATH_MAX];int fd,r=-1;if(p(x,sizeof(x),root,rel))return-1;fd=open(x,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0)return-1;if(!wa(fd,b,n)&&!fsync(fd))r=0;if(close(fd))r=-1;return r;}
static int unlink_leaf(root,rel) const char*root,*rel; {char x[PATH_MAX];return p(x,sizeof(x),root,rel)||unlink(x);}
static int digest(root,b,n,out) const char*root,*b;size_t n;char out[65]; {char x[PATH_MAX];int fd,r;if(p(x,sizeof(x),root,"hash"))return-1;fd=open(x,O_RDWR|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0||wa(fd,b,n)||lseek(fd,0,SEEK_SET)<0){if(fd>=0)close(fd);return-1;}r=character_save_journal_v2_hash_fd(fd,out);close(fd);unlink(x);return r;}
static int seed(root) char*root; {static const char A[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n",E[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";char x[PATH_MAX];if(p(x,sizeof(x),root,"player")||mkdir(x,0700)||p(x,sizeof(x),root,"player/11")||mkdir(x,0700)||p(x,sizeof(x),root,"character-save-stage")||mkdir(x,0700)||p(x,sizeof(x),root,"character-save-journal")||mkdir(x,0700))return-1;return leaf(root,"character-save-journal/writer-instance.v2",A,sizeof(A)-1)||leaf(root,"character-save-journal/writer-epoch.v2",E,sizeof(E)-1)?-1:0;}
static int clear(root,rel) const char*root,*rel; {char x[PATH_MAX],y[PATH_MAX];DIR*d;struct dirent*e;if(p(x,sizeof(x),root,rel)||!(d=opendir(x)))return-1;while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;if(snprintf(y,sizeof(y),"%s/%s",x,e->d_name)<0||unlink(y)){closedir(d);return-1;}}if(closedir(d))return-1;return rmdir(x);}
static int down(root) char*root; {char x[PATH_MAX];return clear(root,"character-save-stage")||clear(root,"character-save-journal")||clear(root,"player/11")||p(x,sizeof(x),root,"player")||rmdir(x)||rmdir(root)?-1:0;}
static character_save_journal_v2_route_lookup_result route(o,w,n,l,r) void*o;const char*w;const unsigned char*n;size_t l;character_save_journal_v2_route_reply*r; {(void)o;memset(r,0,sizeof(*r));if(strcmp(w,W)||l!=sizeof(NAME)-1||memcmp(n,NAME,l))return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;r->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;r->row_count=1;strcpy(r->world_id,W);strcpy(r->character_id,C);memcpy(r->legacy_name,NAME,sizeof(NAME)-1);r->legacy_name_length=sizeof(NAME)-1;strcpy(r->legacy_shard,"11");r->storage_format=1;r->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;}
static int prep(root) const char*root; {character_save_journal_v2_wire v;memset(&v,0,sizeof(v));v.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;strcpy(v.writer_instance_id,I);strcpy(v.character_id,C);strcpy(v.world_id,W);strcpy(v.legacy_name_key_hex,"4d336865726f");strcpy(v.legacy_shard,"11");strcpy(v.command_uuid,CMD);v.writer_epoch=7;v.writer_revision=1;v.expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;v.storage_format=1;if(digest(root,BYTES,sizeof(BYTES)-1,v.post_sha256)||strcmp(v.post_sha256,GOLDEN_POST)||character_save_journal_v2_request_sha256(&v,v.request_sha256)||strcmp(v.request_sha256,GOLDEN_ABSENT_REQUEST)||character_save_journal_v2_prepare(root,&v,BYTES,sizeof(BYTES)-1))return-1;return 0;}
static int prep_existing(root)
const char *root;
{static const char before[]="091b-3 exact existing bytes\n";character_save_journal_v2_wire v;memset(&v,0,sizeof(v));v.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;strcpy(v.writer_instance_id,I);strcpy(v.character_id,C);strcpy(v.world_id,W);strcpy(v.legacy_name_key_hex,"4d336865726f");strcpy(v.legacy_shard,"11");strcpy(v.command_uuid,CMD);v.writer_epoch=7;v.writer_revision=1;v.expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING;v.storage_format=1;if(leaf(root,"player/11/M3hero",before,sizeof(before)-1)||digest(root,before,sizeof(before)-1,v.expected_sha256)||strcmp(v.expected_sha256,GOLDEN_EXPECTED)||digest(root,BYTES,sizeof(BYTES)-1,v.post_sha256)||strcmp(v.post_sha256,GOLDEN_POST)||character_save_journal_v2_request_sha256(&v,v.request_sha256)||strcmp(v.request_sha256,GOLDEN_EXISTING_REQUEST)||character_save_journal_v2_prepare(root,&v,BYTES,sizeof(BYTES)-1))return-1;return 0;}
static int exists(root,rel) const char*root,*rel; {char x[PATH_MAX];struct stat s;return !p(x,sizeof(x),root,rel)&&!lstat(x,&s);}
typedef struct evidence_snapshot { struct stat st; unsigned char bytes[1600]; size_t length; } evidence_snapshot;
static int snapshot(root,rel,out)
const char *root,*rel;
evidence_snapshot *out;
{char full[PATH_MAX];int fd;ssize_t n,extra;if(!out||p(full,sizeof(full),root,rel)||lstat(full,&out->st)||!S_ISREG(out->st.st_mode)||out->st.st_size<0||out->st.st_size>=(off_t)sizeof(out->bytes))return-1;fd=open(full,O_RDONLY|O_NOFOLLOW);if(fd<0)return-1;do n=read(fd,out->bytes,sizeof(out->bytes));while(n<0&&errno==EINTR);do extra=read(fd,out->bytes,1);while(extra<0&&errno==EINTR);if(close(fd)||n<0||extra!=0||(off_t)n!=out->st.st_size)return-1;out->length=(size_t)n;return 0;}
static int same_snapshot(left,right)
const evidence_snapshot *left,*right;
{return left&&right&&left->st.st_dev==right->st.st_dev&&left->st.st_ino==right->st.st_ino&&left->st.st_nlink==right->st.st_nlink&&left->st.st_mode==right->st.st_mode&&left->st.st_uid==right->st.st_uid&&left->st.st_size==right->st.st_size&&left->length==right->length&&!memcmp(left->bytes,right->bytes,left->length);}
static character_save_journal_v2_receipt_result receipt(o,x) void*o;const character_save_journal_v2_receipt*x; {mock*m=o;const char*request=m->expect_existing?GOLDEN_EXISTING_REQUEST:GOLDEN_ABSENT_REQUEST;m->calls++;if(!x||strcmp(x->world_id,W)||x->legacy_name_key_length!=sizeof(NAME)-1||memcmp(x->legacy_name_key,NAME,sizeof(NAME)-1)||strcmp(x->character_id,C)||strcmp(x->command_id,CMD)||strcmp(x->writer_instance_id,I)||!x->request_sha256||strcmp(x->request_sha256,request)||x->writer_epoch!=7||x->writer_revision!=1||(m->expect_existing?(strcmp(x->expected_state,"existing")||!x->expected_sha256||strcmp(x->expected_sha256,GOLDEN_EXPECTED)):(strcmp(x->expected_state,"absent")||x->expected_sha256))||!x->post_sha256||strcmp(x->post_sha256,GOLDEN_POST)||x->storage_format!=1)return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;if(m->mutate_live){m->mutate_live=0;if(!m->root||leaf(m->root,"player/11/M3hero","changed",7))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;}if(m->reopen_writer){m->reopen_writer=0;if(!m->root||!m->context||character_save_journal_v2_writer_close(m->context)||character_save_journal_v2_writer_open(m->root,W,m->context))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;}return m->result;}
static int pair(root)
const char *root;
{char a[PATH_MAX],b[PATH_MAX];struct stat x,y;return !p(a,sizeof(a),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")&&!p(b,sizeof(b),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp")&&!lstat(a,&x)&&!lstat(b,&y)&&x.st_nlink==2&&y.st_nlink==2&&x.st_dev==y.st_dev&&x.st_ino==y.st_ino;}
static int setup_ack(root,ctx)
char *root;character_save_journal_v2_writer_context *ctx;
{if(!realpath("/tmp",root)||strlen(root)+32>=PATH_MAX)return-1;strcat(root,"/muhan-v2-ack-fault-XXXXXX");if(!mkdtemp(root)||seed(root))return-1;memset(ctx,0,sizeof(*ctx));return character_save_journal_v2_writer_open(root,W,ctx)||prep(root)||character_save_journal_v2_publish(ctx,NAME,sizeof(NAME)-1,route,0,CMD);}
static int fault_case(read_close,live_close,marker_close,post_link,first_sync,temp_unlink,final_sync,first_result,first_calls,retry_calls,label)
int read_close,live_close,marker_close,post_link,first_sync,temp_unlink,final_sync;
character_save_journal_v2_ack_result first_result;
int first_calls,retry_calls;
const char *label;
{char root[PATH_MAX];character_save_journal_v2_writer_context ctx;mock m;int failed=0;if(setup_ack(root,&ctx)){fprintf(stderr,"ack fault setup: %s\n",label);return 1;}memset(&m,0,sizeof(m));m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;character_save_journal_v2_ack_faults_for_test(read_close,live_close,marker_close,post_link,first_sync,temp_unlink,final_sync);failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==first_result&&m.calls==first_calls,label);if(post_link||first_sync||temp_unlink)failed+=bad(pair(root),"post-link failure retains only exact target/temp nlink2 pair");m.calls=0;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==retry_calls,"fault retry is deterministic and never double-closes a descriptor");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;return failed;}
static int fault_matrix(void)
{int failed=0;failed+=fault_case(1,0,0,0,0,0,0,CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL,0,1,"prepared read close fails closed before callback");failed+=fault_case(0,1,0,0,0,0,0,CHARACTER_SAVE_JOURNAL_V2_ACK_LIVE,0,1,"live close fails closed before callback");failed+=fault_case(0,0,1,0,0,0,0,CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE,1,1,"marker close consumes fd once and preserves temp evidence");failed+=fault_case(0,0,0,1,0,0,0,CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE,1,1,"post-link exact pair performs idempotent DB retry");failed+=fault_case(0,0,0,0,1,0,0,CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE,1,1,"first journal fsync exact pair performs idempotent DB retry");failed+=fault_case(0,0,0,0,0,1,0,CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE,1,1,"temp unlink exact pair performs idempotent DB retry");failed+=fault_case(0,0,0,0,0,0,1,CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE,1,1,"final journal fsync requires exact DB retry");return failed;}
static int negative_marker_evidence(void)
{char root[PATH_MAX];character_save_journal_v2_writer_context ctx;mock m;evidence_snapshot a,b,c,A,B,C;int failed=0;if(setup_ack(root,&ctx))return 1;memset(&m,0,sizeof(m));m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;character_save_journal_v2_ack_faults_for_test(0,0,0,1,0,0,0);if(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)!=CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE)return 1;if(unlink_leaf(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")||leaf(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked","conflict",8)||snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&a)||snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&b)||a.st.st_nlink!=1||b.st.st_nlink!=1||(a.st.st_dev==b.st.st_dev&&a.st.st_ino==b.st.st_ino))return 1;m.calls=0;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL&&!m.calls&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&A)==0&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&B)==0&&same_snapshot(&a,&A)&&same_snapshot(&b,&B),"different-inode evidence byte-for-byte unchanged");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;if(setup_ack(root,&ctx))return failed+1;memset(&m,0,sizeof(m));m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;character_save_journal_v2_ack_faults_for_test(0,0,0,1,0,0,0);if(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)!=CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE)return failed+1;{char t[PATH_MAX],q[PATH_MAX];if(p(t,sizeof(t),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")||p(q,sizeof(q),root,"character-save-journal/ack-alias")||link(t,q)||snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&a)||snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&b)||snapshot(root,"character-save-journal/ack-alias",&c))return failed+1;}m.calls=0;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL&&!m.calls&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&A)==0&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&B)==0&&snapshot(root,"character-save-journal/ack-alias",&C)==0&&same_snapshot(&a,&A)&&same_snapshot(&b,&B)&&same_snapshot(&c,&C),"nlink3 evidence byte-for-byte unchanged");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;if(setup_ack(root,&ctx)||leaf(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp","partial",7)||snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&a))return failed+1;memset(&m,0,sizeof(m));m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL&&!m.calls&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp",&A)==0&&same_snapshot(&a,&A),"partial temp byte-for-byte unchanged");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;return failed;}
static int existing_receipt(void)
{char root[PATH_MAX];character_save_journal_v2_writer_context ctx;mock m;int failed=0;if(!realpath("/tmp",root)||strlen(root)+36>=sizeof(root))return 1;strcat(root,"/muhan-v2-ack-existing-XXXXXX");if(!mkdtemp(root)||seed(root))return 1;memset(&ctx,0,sizeof(ctx));memset(&m,0,sizeof(m));if(character_save_journal_v2_writer_open(root,W,&ctx)||prep_existing(root)||character_save_journal_v2_publish(&ctx,NAME,sizeof(NAME)-1,route,0,CMD))return 1;m.expect_existing=1;m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"existing receipt sends exact expected_sha256 and golden request/post hashes");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;return failed;}
/* This is deliberately a DB-boundary model rather than an ACK test hook:
 * the local ACK code gets only the receipt outcome.  Its state machine makes
 * an expired A reject until the exact A tuple renews, then permanently fences
 * A once a sealed predecessor has installed B. */
typedef struct epoch_model { int online, expired, renewed, sealed, successor, calls, heads, receipts; } epoch_model;
static int epoch_is_a_tuple(const char *world_id, const char *writer_instance_id,
                            unsigned long long writer_epoch)
{
    return world_id&&writer_instance_id&&!strcmp(world_id,W)&&
           !strcmp(writer_instance_id,I)&&writer_epoch==7;
}
static int epoch_renew_a(epoch_model *state, const char *world_id,
                         const char *writer_instance_id,
                         unsigned long long writer_epoch)
{
    if(!state||!state->online||!epoch_is_a_tuple(world_id,writer_instance_id,writer_epoch)||
       state->sealed||state->successor) return 0;
    state->renewed=1;
    state->expired=0;
    return 1;
}
static int epoch_seal_a(epoch_model *state, const char *world_id,
                        const char *writer_instance_id,
                        unsigned long long writer_epoch)
{
    if(!state||!state->online||!state->renewed||
       !epoch_is_a_tuple(world_id,writer_instance_id,writer_epoch)||
       state->sealed||state->successor) return 0;
    state->sealed=1;
    return 1;
}
static character_save_journal_v2_receipt_result epoch_receipt(o,x)
void *o;const character_save_journal_v2_receipt *x;
{epoch_model*state=o; if(!state||!x||strcmp(x->world_id,W)||strcmp(x->writer_instance_id,I)||x->writer_epoch!=7||strcmp(x->command_id,CMD))return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;state->calls++;if(!state->online)return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;if((state->expired&&!state->renewed)||state->sealed||state->successor)return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;state->heads++;state->receipts++;return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;}
static int expired_offline_then_successor_fence(void)
{char root[PATH_MAX];character_save_journal_v2_writer_context ctx;epoch_model state;evidence_snapshot published_before,published_after,acked_before,acked_after;int failed=0;if(setup_ack(root,&ctx))return 1;memset(&state,0,sizeof(state));failed+=bad(character_save_journal_v2_ack(&ctx,CMD,epoch_receipt,&state)==CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED&&state.calls==1&&!state.heads&&!state.receipts&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.published",&published_before)==0&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"offline deferred receipt leaves LEGACY_PUBLISHED backlog durable and DB-unacknowledged");state.online=1;state.expired=1;failed+=bad(character_save_journal_v2_ack(&ctx,CMD,epoch_receipt,&state)==CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE&&state.calls==2&&!state.heads&&!state.receipts&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.published",&published_after)==0&&same_snapshot(&published_before,&published_after)&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"expired A cannot ACK before its exact renewal and retains local evidence");failed+=bad(!epoch_renew_a(&state,"other",I,7)&&!epoch_renew_a(&state,W,C,7)&&!epoch_renew_a(&state,W,I,8)&&!state.renewed,"wrong world, writer instance, or epoch cannot renew A");failed+=bad(epoch_renew_a(&state,W,I,7)&&character_save_journal_v2_ack(&ctx,CMD,epoch_receipt,&state)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&state.calls==3&&state.heads==1&&state.receipts==1&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&acked_before)==0,"restored DB permits ACK only after exact A renewal");failed+=bad(!epoch_seal_a(&state,"other",I,7)&&!epoch_seal_a(&state,W,C,7)&&!epoch_seal_a(&state,W,I,8)&&epoch_seal_a(&state,W,I,7),"only the exact drained A tuple seals before successor installation");state.successor=1;failed+=bad(!epoch_renew_a(&state,W,I,7)&&!epoch_seal_a(&state,W,I,7)&&character_save_journal_v2_ack(&ctx,CMD,epoch_receipt,&state)==CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE&&state.calls==4&&state.heads==1&&state.receipts==1&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.published",&published_after)==0&&snapshot(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked",&acked_after)==0&&same_snapshot(&published_before,&published_after)&&same_snapshot(&acked_before,&acked_after),"successor permanently fences A renew, seal, receipt and preserves head, receipt, and local evidence");if(character_save_journal_v2_writer_close(&ctx)||down(root))return failed+1;return failed;}
int main(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context ctx;
    mock m;
    int failed = 0, result;
    if(!realpath("/tmp",root)||strlen(root)+32>=sizeof(root)) return 1;
    strcat(root,"/muhan-v2-ack-XXXXXX");
    if(!mkdtemp(root)||seed(root)) return 1;
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    memset(&ctx,0,sizeof(ctx)); memset(&m,0,sizeof(m));
    result=character_save_journal_v2_writer_open(root,W,&ctx);
    if(result||prep(root)||character_save_journal_v2_publish(&ctx,NAME,sizeof(NAME)-1,route,0,CMD)) return 1;
    if(leaf(root,"character-save-stage/40000000-0000-0000-0000-000000000001.stage","retained",8)) return 1;
    m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_LIVE&&!m.calls,"present stage freezes before DB callback");
    {
        char stage[PATH_MAX];
        if(p(stage,sizeof(stage),root,"character-save-stage/40000000-0000-0000-0000-000000000001.stage")||unlink(stage)) return 1;
    }
    m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED&&m.calls==1,"offline deferred after exact SQL receipt fields");
    failed+=bad(exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.published")&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"deferred preserves durable evidence");
    if(leaf(root,"player/11/M3hero","changed",7)) return 1;
    m.calls=0; m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_LIVE&&!m.calls,"changed live bytes never reach DB callback");
    if(leaf(root,"player/11/M3hero",BYTES,sizeof(BYTES)-1)) return 1;
    m.calls=0; m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE&&m.calls==1&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"invalid DB receipt freezes without ACK marker");
    m.calls=0; m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE&&m.calls==1&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"rejected DB receipt freezes without ACK marker");
    m.calls=0; m.result=(character_save_journal_v2_receipt_result)99;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE&&m.calls==1&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"unknown callback enum freezes without ACK marker");
    m.calls=0; m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED; m.root=root; m.mutate_live=1;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&m.calls==1&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"callback live mutation exposes DB ACK as local-incomplete");
    if(leaf(root,"player/11/M3hero",BYTES,sizeof(BYTES)-1)) return 1;
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"post-callback live mutation converges through exact DB retry");
    {
        char acked[PATH_MAX];
        if(p(acked,sizeof(acked),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")||unlink(acked)) return 1;
    }
    m.calls=0; m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED; m.root=root; m.context=&ctx; m.reopen_writer=1;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&m.calls==1&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"callback generation change exposes DB ACK as local-incomplete");
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"post-callback writer change converges through exact DB retry");
    {
        char acked[PATH_MAX];
        if(p(acked,sizeof(acked),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")||unlink(acked)) return 1;
    }
    character_save_journal_v2_ack_fail_marker_fsync_for_test(1);
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&m.calls==1&&exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked.tmp")&&!exists(root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked"),"marker file fsync failure retains only exact temp evidence");
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"exact temp retry re-fsyncs before marker promotion");
    {
        char acked[PATH_MAX];
        if(p(acked,sizeof(acked),root,"character-save-journal/40000000-0000-0000-0000-000000000001.acked")||unlink(acked)) return 1;
    }
    m.result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    character_save_journal_v2_ack_faults_for_test(0,0,0,1,0,0,0);
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&m.calls==1&&pair(root),"post-link failure exposes exact two-name DB evidence");
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"two-name retry repeats exact DB receipt before local repair");
    m.calls=0;
    failed+=bad(character_save_journal_v2_ack(&ctx,CMD,receipt,&m)==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED&&m.calls==1,"durable DB_ACKED retry remains an exact DB retry");
    if(character_save_journal_v2_writer_close(&ctx)||down(root)) return failed+1;
    failed+=fault_matrix();
    failed+=negative_marker_evidence();
    failed+=existing_receipt();
    failed+=expired_offline_then_successor_fence();
    return failed?1:0;
}
