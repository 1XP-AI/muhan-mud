#include "character_save_journal_v2_receipt_transport.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

#define FAKE_RESULT_ERROR 2

typedef struct fake_transport {
    int connect_calls, connection_ok_calls, execute_calls, status_calls;
    int sqlstate_calls, clear_calls, finish_calls;
    void *connect_result, *execute_result;
    int connection_ok, result_status;
    const char *sqlstate;
    char sql[256];
    int count, result_format;
    unsigned int types[12];
    int lengths[12], formats[12], nulls[12];
    char values[12][96];
} fake_transport;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "character_save_journal_v2_receipt_transport_test: %s\n", message);
    return 1;
}

static void *fake_connect(void *opaque)
{ fake_transport *fake=(fake_transport *)opaque; fake->connect_calls++; return fake->connect_result; }

static int fake_connection_ok(void *connection)
{ fake_transport *fake=(fake_transport *)connection; fake->connection_ok_calls++; return fake->connection_ok; }

static void *fake_exec(void *connection, const char *sql, int count,
                       const unsigned int *types, const char *const *values,
                       const int *lengths, const int *formats, int result_format)
{
    fake_transport *fake=(fake_transport *)connection;
    int i;
    fake->execute_calls++; fake->count=count; fake->result_format=result_format;
    (void)snprintf(fake->sql,sizeof(fake->sql),"%s",sql);
    for(i=0;i<12;i++) {
        fake->types[i]=types[i]; fake->lengths[i]=lengths ? lengths[i] : -1;
        fake->formats[i]=formats[i]; fake->nulls[i]=values[i]==0;
        if(values[i]) (void)snprintf(fake->values[i],sizeof(fake->values[i]),"%s",values[i]);
    }
    return fake->execute_result;
}

static int fake_status(void *result)
{ fake_transport *fake=(fake_transport *)result; fake->status_calls++; return fake->result_status; }

static const char *fake_sqlstate(void *result)
{ fake_transport *fake=(fake_transport *)result; fake->sqlstate_calls++; return fake->sqlstate; }

static void fake_clear(void *result)
{ fake_transport *fake=(fake_transport *)result; fake->clear_calls++; }

static void fake_finish(void *connection)
{ fake_transport *fake=(fake_transport *)connection; fake->finish_calls++; }

static const character_save_journal_v2_receipt_transport_operations fake_operations={
    fake_connect,fake_connection_ok,fake_exec,fake_status,fake_sqlstate,fake_clear,fake_finish
};

static character_save_journal_v2_receipt receipt_make(void)
{
    static const unsigned char name[]={ 'N','a','m','e' };
    character_save_journal_v2_receipt receipt;
    memset(&receipt,0,sizeof(receipt));
    receipt.world_id="m3-contract"; receipt.legacy_name_key=name;
    receipt.legacy_name_key_length=sizeof(name); receipt.character_id="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
    receipt.command_id="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
    receipt.writer_instance_id="cccccccc-cccc-4ccc-8ccc-cccccccccccc";
    receipt.request_sha256="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    receipt.writer_epoch=7; receipt.writer_revision=9; receipt.expected_state="existing";
    receipt.expected_sha256="abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
    receipt.post_sha256="fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210";
    receipt.storage_format=1;
    return receipt;
}

static void fake_ready(fake_transport *fake)
{ memset(fake,0,sizeof(*fake)); fake->connect_result=fake; fake->execute_result=fake; fake->connection_ok=1; fake->result_status=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_TUPLES_OK; }

static int test_exact_call(void)
{
    static const unsigned int types[]={25,25,2950,2950,2950,25,20,20,25,25,25,21};
    static const char *values[]={"m3-contract","Name","aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","cccccccc-cccc-4ccc-8ccc-cccccccccccc","0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","7","9","existing","abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210","1"};
    static const char sql[]="select private.record_legacy_published_receipt($1::text,$2::text,$3::uuid,$4::uuid,$5::uuid,$6::text,$7::bigint,$8::bigint,$9::text,$10::text,$11::text,$12::smallint)";
    fake_transport fake; character_save_journal_v2_receipt_transport transport; character_save_journal_v2_receipt receipt=receipt_make(); int i, failed=0;
    fake_ready(&fake); transport.operations=&fake_operations; transport.operations_opaque=&fake;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED,"exact call must acknowledge tuples result");
    failed|=expect(fake.connect_calls==1&&fake.execute_calls==1&&fake.clear_calls==1&&fake.finish_calls==1,"successful call must connect, execute, clear, and finish once");
    failed|=expect(!strcmp(fake.sql,sql)&&fake.count==12&&fake.result_format==0,"SQL, parameter count, and text result format must be exact");
    for(i=0;i<12;i++) failed|=expect(fake.types[i]==types[i]&&fake.formats[i]==0&&!fake.nulls[i]&&!strcmp(fake.values[i],values[i]),"OID, text format, or value order differs");
    return failed;
}

static int test_absent_and_bounds(void)
{
    fake_transport fake; character_save_journal_v2_receipt_transport transport; character_save_journal_v2_receipt receipt=receipt_make(); int failed=0;
    fake_ready(&fake); transport.operations=&fake_operations; transport.operations_opaque=&fake;
    receipt.expected_state="absent"; receipt.expected_sha256=0; receipt.writer_epoch=(unsigned long long)INT64_MAX; receipt.writer_revision=(unsigned long long)INT64_MAX; receipt.storage_format=INT16_MAX;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED,"absent and maximum legal values must execute");
    failed|=expect(fake.nulls[9]&&!strcmp(fake.values[6],"9223372036854775807")&&!strcmp(fake.values[7],"9223372036854775807")&&!strcmp(fake.values[11],"32767"),"NULL expected hash or canonical decimal conversion is wrong");
    fake_ready(&fake); receipt.writer_epoch=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"zero epoch must reject before connect");
    receipt.writer_epoch=(unsigned long long)INT64_MAX+1ULL;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"overflow epoch must reject before connect");
    receipt.writer_epoch=1; receipt.writer_revision=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"zero revision must reject before connect");
    receipt.writer_revision=1; receipt.storage_format=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"zero storage format must reject before connect");
    receipt.storage_format=(unsigned int)INT16_MAX+1U;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"overflow storage format must reject before connect");
    return failed;
}

static int test_name_rejection(void)
{
    static const unsigned char embedded[]={ 'A',0,'B' };
    static const unsigned char long_name[]="A234567890abcde";
    static const unsigned char too_many_points[]="Abcdefghijklm";
    static const unsigned char lower_first[]="name";
    static const unsigned char later_upper[]="NAme";
    static const unsigned char invalid_utf8[]={ 'N',0xc0,0x80 };
    static const unsigned char forbidden[]={ 'N',':' };
    static const unsigned char spaces[]={ ' ',' ' };
    static const unsigned char dotdot[]={ '.','.' };
    static const unsigned char valid_utf8[]={ 0xeb,0xac,0xb4,0xed,0x95,0x9c,'a','b' };
    fake_transport fake; character_save_journal_v2_receipt_transport transport; character_save_journal_v2_receipt receipt=receipt_make(); int failed=0;
    fake_ready(&fake); transport.operations=&fake_operations; transport.operations_opaque=&fake; receipt.legacy_name_key=embedded; receipt.legacy_name_key_length=sizeof(embedded);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"embedded NUL name must reject before connect");
    receipt.legacy_name_key=long_name; receipt.legacy_name_key_length=sizeof(long_name)-1;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"overlong name must reject before connect");
    receipt.legacy_name_key=too_many_points; receipt.legacy_name_key_length=sizeof(too_many_points)-1;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"more than twelve codepoints must reject before connect");
    receipt.legacy_name_key=lower_first; receipt.legacy_name_key_length=sizeof(lower_first)-1;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"lowercase ASCII first byte must reject before connect");
    receipt.legacy_name_key=later_upper; receipt.legacy_name_key_length=sizeof(later_upper)-1;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"uppercase ASCII after the first byte must reject before connect");
    receipt.legacy_name_key=invalid_utf8; receipt.legacy_name_key_length=sizeof(invalid_utf8);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"invalid UTF-8 must reject before connect");
    receipt.legacy_name_key=forbidden; receipt.legacy_name_key_length=sizeof(forbidden);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"filesystem-forbidden name byte must reject before connect");
    receipt.legacy_name_key=spaces; receipt.legacy_name_key_length=sizeof(spaces);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"all-space name must reject before connect");
    receipt.legacy_name_key=dotdot; receipt.legacy_name_key_length=sizeof(dotdot);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"dot-dot name must reject before connect");
    fake_ready(&fake); receipt.legacy_name_key=valid_utf8; receipt.legacy_name_key_length=sizeof(valid_utf8);
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED&&fake.execute_calls==1,"canonical multibyte UTF-8 name must execute once");
    return failed;
}

static int test_bounded_external_text(void)
{
    static const char unterminated_expected[9]={'e','x','i','s','t','i','n','g','x'};
    static const char unterminated_state[6]={'2','2','0','2','3','x'};
    fake_transport fake; character_save_journal_v2_receipt_transport transport; character_save_journal_v2_receipt receipt=receipt_make(); int failed=0;
    transport.operations=&fake_operations; transport.operations_opaque=&fake;
    fake_ready(&fake); receipt.expected_state=unterminated_expected;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.connect_calls==0,"unterminated expected state must reject within its bound");
    receipt=receipt_make(); fake_ready(&fake); fake.result_status=FAKE_RESULT_ERROR; fake.sqlstate=unterminated_state;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==1&&fake.clear_calls==1&&fake.finish_calls==1,"unterminated SQLSTATE must defer and clean up once");
    return failed;
}

static int test_outcomes_and_ownership(void)
{
    static const int other_statuses[]={0,2,3,4,5,6,7,8,9,99};
    fake_transport fake; character_save_journal_v2_receipt_transport transport; character_save_journal_v2_receipt receipt=receipt_make(); int failed=0, i;
    transport.operations=&fake_operations; transport.operations_opaque=&fake;
    fake_ready(&fake); fake.connect_result=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==0&&fake.finish_calls==0,"null connection must defer without finish");
    fake_ready(&fake); fake.connection_ok=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==0&&fake.finish_calls==1,"bad connection must finish once");
    fake_ready(&fake); fake.execute_result=0;
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==1&&fake.clear_calls==0&&fake.finish_calls==1,"null result must defer and preserve ownership counts");
    fake_ready(&fake); fake.result_status=FAKE_RESULT_ERROR; fake.sqlstate="22023";
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE&&fake.clear_calls==1&&fake.finish_calls==1,"22023 must freeze invalid evidence");
    fake_ready(&fake); fake.result_status=FAKE_RESULT_ERROR; fake.sqlstate="P0001";
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE,"P0001 must freeze rejected evidence");
    fake_ready(&fake); fake.result_status=FAKE_RESULT_ERROR; fake.sqlstate="57014";
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED,"timeout SQLSTATE must defer");
    fake_ready(&fake); fake.result_status=FAKE_RESULT_ERROR; fake.sqlstate="XX000";
    failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==1,"unexpected SQLSTATE must defer with no retry");
    for(i=0;i<(int)(sizeof(other_statuses)/sizeof(other_statuses[0]));i++) {
        fake_ready(&fake); fake.result_status=other_statuses[i]; fake.sqlstate=0;
        failed|=expect(character_save_journal_v2_receipt_transport_callback(&transport,&receipt)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED&&fake.execute_calls==1&&fake.clear_calls==1&&fake.finish_calls==1,"every non-tuples status must defer and clean up once");
    }
    return failed;
}

int main(void)
{
    int failed=0;
    failed|=test_exact_call();
    failed|=test_absent_and_bounds();
    failed|=test_name_rejection();
    failed|=test_bounded_external_text();
    failed|=test_outcomes_and_ownership();
    return failed;
}
