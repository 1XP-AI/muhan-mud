#include "character_save_journal_v2_receipt_transport.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

static size_t receipt_text_length(const char *text, size_t maximum)
{
    size_t i;
    if(!text) return maximum+1;
    for(i=0;i<=maximum;i++) if(!text[i]) return i;
    return maximum+1;
}

static int receipt_lower_hex(const char *text, size_t length)
{
    size_t i;
    if(receipt_text_length(text,length)!=length) return 0;
    for(i=0;i<length;i++)
        if(!((text[i]>='0'&&text[i]<='9')||(text[i]>='a'&&text[i]<='f')))
            return 0;
    return 1;
}

static int receipt_uuid(const char *text)
{
    size_t i;
    if(receipt_text_length(text,36)!=36) return 0;
    for(i=0;i<36;i++) {
        if(i==8||i==13||i==18||i==23) {
            if(text[i]!='-') return 0;
        } else if(!((text[i]>='0'&&text[i]<='9')||(text[i]>='a'&&text[i]<='f')))
            return 0;
    }
    return 1;
}

static int receipt_world(const char *text)
{
    size_t i, length=receipt_text_length(text,64);
    if(!length||length>64||text[0]<'a'||text[0]>'z') return 0;
    for(i=1;i<length;i++)
        if(!((text[i]>='a'&&text[i]<='z')||(text[i]>='0'&&text[i]<='9')||
             text[i]=='_'||text[i]=='-')) return 0;
    return 1;
}

static int receipt_text_equal(const char *text, const char *expected,
                              size_t length)
{
    return receipt_text_length(text,length)==length&&
           !memcmp(text,expected,length);
}

static int receipt_utf8(const unsigned char *bytes, size_t length,
                        size_t *codepoints)
{
    size_t i=0, points=0;
    unsigned char first;
    while(i<length) {
        first=bytes[i++];
        if(first<0x80) {
            points++;
            continue;
        }
        if(first>=0xc2&&first<=0xdf) {
            if(i>=length||bytes[i]<0x80||bytes[i]>0xbf) return 0;
            i++;
        } else if(first==0xe0) {
            if(i+1>=length||bytes[i]<0xa0||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first>=0xe1&&first<=0xec) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first==0xed) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0x9f||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first>=0xee&&first<=0xef) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first==0xf0) {
            if(i+2>=length||bytes[i]<0x90||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else if(first>=0xf1&&first<=0xf3) {
            if(i+2>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else if(first==0xf4) {
            if(i+2>=length||bytes[i]<0x80||bytes[i]>0x8f||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else return 0;
        points++;
    }
    if(codepoints) *codepoints=points;
    return 1;
}

static int receipt_canonical_name(const unsigned char *name, size_t length)
{
    size_t i, points;
    int all_spaces=1;
    if(!name||!length||
       length>CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NAME_MAX) return 0;
    for(i=0;i<length;i++) {
        if(!name[i]||name[i]<0x20||name[i]==0x7f||name[i]=='/'||
           name[i]=='\\'||name[i]==':') return 0;
        if(name[i]!=' ') all_spaces=0;
        if((i==0&&name[i]>='a'&&name[i]<='z')||
           (i>0&&name[i]>='A'&&name[i]<='Z')) return 0;
    }
    if(all_spaces||(length==1&&name[0]=='.')||
       (length==2&&name[0]=='.'&&name[1]=='.')||
       !receipt_utf8(name,length,&points)||!points||points>12) return 0;
    return 1;
}

static int receipt_expected(const character_save_journal_v2_receipt *receipt)
{
    if(receipt_text_equal(receipt->expected_state,"absent",6))
        return receipt->expected_sha256==0;
    if(!receipt_text_equal(receipt->expected_state,"existing",8)) return 0;
    return receipt_lower_hex(receipt->expected_sha256,64);
}

static int receipt_valid(const character_save_journal_v2_receipt *receipt,
                         char name[CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NAME_MAX + 1])
{
    if(!receipt||!receipt_world(receipt->world_id)||
       !receipt_canonical_name(receipt->legacy_name_key,
                               receipt->legacy_name_key_length)||
       !receipt_uuid(receipt->character_id)||!receipt_uuid(receipt->command_id)||
       !receipt_uuid(receipt->writer_instance_id)||
       !receipt_lower_hex(receipt->request_sha256,64)||
       !receipt->writer_epoch||receipt->writer_epoch>(unsigned long long)INT64_MAX||
       !receipt->writer_revision||receipt->writer_revision>(unsigned long long)INT64_MAX||
       !receipt_expected(receipt)||!receipt_lower_hex(receipt->post_sha256,64)||
       !receipt->storage_format||receipt->storage_format>(unsigned int)INT16_MAX)
        return 0;
    memcpy(name,receipt->legacy_name_key,receipt->legacy_name_key_length);
    name[receipt->legacy_name_key_length]=0;
    return 1;
}

character_save_journal_v2_receipt_result
character_save_journal_v2_receipt_transport_callback(
    void *opaque, const character_save_journal_v2_receipt *receipt)
{
    static const char sql[]=
        "select private.record_legacy_published_receipt($1::text,$2::text,$3::uuid,$4::uuid,$5::uuid,$6::text,$7::bigint,$8::bigint,$9::text,$10::text,$11::text,$12::smallint)";
    static const unsigned int types[]={25,25,2950,2950,2950,25,20,20,25,25,25,21};
    static const int formats[]={0,0,0,0,0,0,0,0,0,0,0,0};
    character_save_journal_v2_receipt_transport *transport=(character_save_journal_v2_receipt_transport *)opaque;
    char name[CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_NAME_MAX+1];
    char epoch[32], revision[32], storage_format[8];
    const char *values[12];
    void *connection=0, *result=0;
    character_save_journal_v2_receipt_result answer=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    int n;

    if(!transport||!transport->operations||!transport->operations->connect||
       !transport->operations->connection_ok||!transport->operations->exec_params||
       !transport->operations->result_status||!transport->operations->result_sqlstate||
       !transport->operations->result_clear||!transport->operations->connection_finish||
       !receipt_valid(receipt,name)) return answer;
    n=snprintf(epoch,sizeof(epoch),"%llu",receipt->writer_epoch);
    if(n<0||(size_t)n>=sizeof(epoch)) return answer;
    n=snprintf(revision,sizeof(revision),"%llu",receipt->writer_revision);
    if(n<0||(size_t)n>=sizeof(revision)) return answer;
    n=snprintf(storage_format,sizeof(storage_format),"%u",receipt->storage_format);
    if(n<0||(size_t)n>=sizeof(storage_format)) return answer;
    values[0]=receipt->world_id; values[1]=name; values[2]=receipt->character_id;
    values[3]=receipt->command_id; values[4]=receipt->writer_instance_id;
    values[5]=receipt->request_sha256; values[6]=epoch; values[7]=revision;
    values[8]=receipt->expected_state; values[9]=receipt->expected_sha256;
    values[10]=receipt->post_sha256; values[11]=storage_format;
    connection=transport->operations->connect(transport->operations_opaque);
    if(!connection) goto done;
    if(!transport->operations->connection_ok(connection)) goto done;
    result=transport->operations->exec_params(connection,sql,12,types,values,0,formats,0);
    if(!result) goto done;
    if(transport->operations->result_status(result)==CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_TUPLES_OK)
        answer=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    else {
        const char *state=transport->operations->result_sqlstate(result);
        if(receipt_text_equal(state,"22023",5))
            answer=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;
        else if(receipt_text_equal(state,"P0001",5))
            answer=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    }
done:
    if(result) transport->operations->result_clear(result);
    if(connection) transport->operations->connection_finish(connection);
    memset(name,0,sizeof(name)); memset(epoch,0,sizeof(epoch));
    memset(revision,0,sizeof(revision)); memset(storage_format,0,sizeof(storage_format));
    return answer;
}
