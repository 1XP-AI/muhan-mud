/* Closed canonical wire for legacy identity evidence.  It deliberately does
 * not inspect files, invoke PlayerStore, canonicalize names, or derive shards. */
#include "legacy_identity_evidence_wire.h"

#include <stdlib.h>
#include <string.h>

static void wire_put16(out, value)
unsigned char *out;
unsigned int value;
{ out[0]=(unsigned char)(value>>8); out[1]=(unsigned char)value; }

static void wire_put32(out, value)
unsigned char *out;
unsigned long value;
{ out[0]=(unsigned char)(value>>24); out[1]=(unsigned char)(value>>16);
  out[2]=(unsigned char)(value>>8); out[3]=(unsigned char)value; }

static unsigned int wire_get16(value)
const unsigned char *value;
{ return ((unsigned int)value[0]<<8)|value[1]; }

static unsigned long wire_get32(value)
const unsigned char *value;
{ return ((unsigned long)value[0]<<24)|((unsigned long)value[1]<<16)|
         ((unsigned long)value[2]<<8)|value[3]; }

/* C inputs use fixed, zero-filled string buffers.  Reject bytes after the
 * first terminator so an attempted embedded NUL cannot be silently shortened
 * before it becomes a length-delimited wire field. */
static int wire_text_length(value, limit, length)
const char *value;
size_t limit,*length;
{ size_t i;
  if(!value)return 0;
  for(i=0;i<=limit;i++) if(!value[i]) {
    *length=i;
    for(++i;i<=limit;i++) if(value[i])return 0;
    return 1;
  }
  return 0; }

/* Canonical wire text has no embedded NUL.  Check it while it is still a
 * byte slice, before any bytes can reach a C string buffer. */
static int wire_text_nul_free(value, length)
const unsigned char *value;
size_t length;
{ size_t i; for(i=0;i<length;i++)if(!value[i])return 0; return 1; }

static int wire_lower_hex(value, length)
const char *value;
size_t length;
{ size_t i; for(i=0;i<length;i++) if(!((value[i]>='0'&&value[i]<='9')||
     (value[i]>='a'&&value[i]<='f'))) return 0; return 1; }

/* This validates UTF-8 representation only.  Canonicalization remains solely
 * the responsibility of legacy_identity_evidence_inspect and lowercize(). */
static int wire_utf8(value, length)
const unsigned char *value;
size_t length;
{ size_t i=0; unsigned char c;
  while(i<length) { c=value[i++];
    if(c<=0x7f) continue;
    if(c>=0xc2&&c<=0xdf) { if(i>=length||(value[i]&0xc0)!=0x80)return 0; ++i; }
    else if(c==0xe0) { if(i+1>=length||value[i]<0xa0||value[i]>0xbf||(value[i+1]&0xc0)!=0x80)return 0; i+=2; }
    else if(c>=0xe1&&c<=0xec) { if(i+1>=length||(value[i]&0xc0)!=0x80||(value[i+1]&0xc0)!=0x80)return 0; i+=2; }
    else if(c==0xed) { if(i+1>=length||value[i]<0x80||value[i]>0x9f||(value[i+1]&0xc0)!=0x80)return 0; i+=2; }
    else if(c>=0xee&&c<=0xef) { if(i+1>=length||(value[i]&0xc0)!=0x80||(value[i+1]&0xc0)!=0x80)return 0; i+=2; }
    else if(c==0xf0) { if(i+2>=length||value[i]<0x90||value[i]>0xbf||(value[i+1]&0xc0)!=0x80||(value[i+2]&0xc0)!=0x80)return 0; i+=3; }
    else if(c>=0xf1&&c<=0xf3) { if(i+2>=length||(value[i]&0xc0)!=0x80||(value[i+1]&0xc0)!=0x80||(value[i+2]&0xc0)!=0x80)return 0; i+=3; }
    else if(c==0xf4) { if(i+2>=length||value[i]<0x80||value[i]>0x8f||(value[i+1]&0xc0)!=0x80||(value[i+2]&0xc0)!=0x80)return 0; i+=3; }
    else return 0;
  } return 1; }

static int wire_result_encode(value, out)
legacy_identity_evidence_result value;
unsigned char *out;
{ switch(value) {
  case LEGACY_IDENTITY_EVIDENCE_OK:*out=0;return 1;
  case LEGACY_IDENTITY_EVIDENCE_NOT_FOUND:*out=1;return 1;
  case LEGACY_IDENTITY_EVIDENCE_CORRUPT:*out=2;return 1;
  case LEGACY_IDENTITY_EVIDENCE_IO_ERROR:*out=3;return 1;
  case LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT:*out=4;return 1;
  } return 0; }

static int wire_result_decode(value, out)
unsigned char value;
legacy_identity_evidence_result *out;
{ switch(value) {
  case 0:*out=LEGACY_IDENTITY_EVIDENCE_OK;return 1;
  case 1:*out=LEGACY_IDENTITY_EVIDENCE_NOT_FOUND;return 1;
  case 2:*out=LEGACY_IDENTITY_EVIDENCE_CORRUPT;return 1;
  case 3:*out=LEGACY_IDENTITY_EVIDENCE_IO_ERROR;return 1;
  case 4:*out=LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT;return 1;
  } return 0; }

static int wire_canonicalization(value)
legacy_identity_evidence_canonicalization value;
{ return value==LEGACY_IDENTITY_EVIDENCE_CANONICAL ||
         value==LEGACY_IDENTITY_EVIDENCE_NORMALIZED ||
         value==LEGACY_IDENTITY_EVIDENCE_INVALID; }

static int wire_evidence_valid(value, name_length, shard_length, digest_length,
                               storage_length)
const legacy_identity_evidence *value;
size_t name_length, shard_length, digest_length, storage_length;
{ if(value->version!=LEGACY_IDENTITY_EVIDENCE_VERSION ||
     !wire_canonicalization(value->canonicalization) ||
     storage_length!=LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN ||
     memcmp(value->storage_format,LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT,
            storage_length)) return 0;
  if(value->result==LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT)
    return value->canonicalization==LEGACY_IDENTITY_EVIDENCE_INVALID &&
           !name_length&&!shard_length&&!digest_length;
  if(value->result!=LEGACY_IDENTITY_EVIDENCE_OK &&
     value->result!=LEGACY_IDENTITY_EVIDENCE_NOT_FOUND &&
     value->result!=LEGACY_IDENTITY_EVIDENCE_CORRUPT &&
     value->result!=LEGACY_IDENTITY_EVIDENCE_IO_ERROR) return 0;
  return value->canonicalization!=LEGACY_IDENTITY_EVIDENCE_INVALID &&
         name_length && name_length<=PLAYER_NAME_MAX_BYTES &&
         wire_utf8((const unsigned char *)value->canonical_name,name_length) &&
         shard_length==2 && wire_lower_hex(value->legacy_shard,shard_length) &&
         ((value->result==LEGACY_IDENTITY_EVIDENCE_OK &&
           digest_length==LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN &&
           wire_lower_hex(value->player_file_sha256,digest_length)) ||
          (value->result!=LEGACY_IDENTITY_EVIDENCE_OK && !digest_length)); }

int legacy_identity_evidence_wire_encode(evidence, wire, wire_length)
const legacy_identity_evidence *evidence;
unsigned char **wire;
size_t *wire_length;
{ size_t name_length,shard_length,digest_length,storage_length,payload,at;
  unsigned char result,*out;
  if(!evidence||!wire||!wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_INVALID_ARGUMENT;
  *wire=0;*wire_length=0;
  if(!wire_text_length(evidence->canonical_name,PLAYER_NAME_MAX_BYTES,&name_length)||
     !wire_text_length(evidence->legacy_shard,2,&shard_length)||
     !wire_text_length(evidence->player_file_sha256,
                       LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN,&digest_length)||
     !wire_text_length(evidence->storage_format,
                       LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN,&storage_length))
    return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  if(!wire_result_encode(evidence->result,&result)||
     !wire_evidence_valid(evidence,name_length,shard_length,digest_length,
                          storage_length)) return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  payload=1+1+1+name_length+1+shard_length+1+digest_length+1+storage_length;
  if(payload>LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_PAYLOAD)
    return LEGACY_IDENTITY_EVIDENCE_WIRE_SIZE_LIMIT;
  out=(unsigned char *)malloc(LEGACY_IDENTITY_EVIDENCE_WIRE_HEADER_LENGTH+payload);
  if(!out)return LEGACY_IDENTITY_EVIDENCE_WIRE_ALLOCATION_FAILED;
  memcpy(out,LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC,LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC_LENGTH);
  wire_put16(out+8,LEGACY_IDENTITY_EVIDENCE_WIRE_SCHEMA);
  wire_put16(out+10,LEGACY_IDENTITY_EVIDENCE_WIRE_VERSION);
  wire_put32(out+12,(unsigned long)payload); at=16;
  out[at++]=result; out[at++]=(unsigned char)evidence->canonicalization;
  out[at++]=(unsigned char)name_length; memcpy(out+at,evidence->canonical_name,name_length);at+=name_length;
  out[at++]=(unsigned char)shard_length; memcpy(out+at,evidence->legacy_shard,shard_length);at+=shard_length;
  out[at++]=(unsigned char)digest_length; memcpy(out+at,evidence->player_file_sha256,digest_length);at+=digest_length;
  out[at++]=(unsigned char)storage_length; memcpy(out+at,evidence->storage_format,storage_length);
  *wire=out;*wire_length=LEGACY_IDENTITY_EVIDENCE_WIRE_HEADER_LENGTH+payload;
  return LEGACY_IDENTITY_EVIDENCE_WIRE_OK; }

static int wire_read(wire, total, at, length, value)
const unsigned char *wire;
size_t total,*at,length;
const unsigned char **value;
{ if(*at>total||length>total-*at)return 0;*value=wire+*at;*at+=length;return 1; }

int legacy_identity_evidence_wire_decode(wire, wire_length, evidence)
const unsigned char *wire;
size_t wire_length;
legacy_identity_evidence *evidence;
{ size_t payload,at,name_length,shard_length,digest_length,storage_length;
  const unsigned char *name,*shard,*digest,*storage;
  legacy_identity_evidence parsed;
  if(!wire||!evidence)return LEGACY_IDENTITY_EVIDENCE_WIRE_INVALID_ARGUMENT;
  if(wire_length<LEGACY_IDENTITY_EVIDENCE_WIRE_HEADER_LENGTH)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(memcmp(wire,LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC,LEGACY_IDENTITY_EVIDENCE_WIRE_MAGIC_LENGTH))return LEGACY_IDENTITY_EVIDENCE_WIRE_INVALID_MAGIC;
  if(wire_get16(wire+8)!=LEGACY_IDENTITY_EVIDENCE_WIRE_SCHEMA)return LEGACY_IDENTITY_EVIDENCE_WIRE_UNSUPPORTED_SCHEMA;
  if(wire_get16(wire+10)!=LEGACY_IDENTITY_EVIDENCE_WIRE_VERSION)return LEGACY_IDENTITY_EVIDENCE_WIRE_UNSUPPORTED_VERSION;
  payload=(size_t)wire_get32(wire+12);
  if(payload>LEGACY_IDENTITY_EVIDENCE_WIRE_MAX_PAYLOAD)return LEGACY_IDENTITY_EVIDENCE_WIRE_SIZE_LIMIT;
  if(payload>wire_length-16)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(payload<wire_length-16)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRAILING_BYTES;
  at=16; memset(&parsed,0,sizeof(parsed));parsed.version=LEGACY_IDENTITY_EVIDENCE_VERSION;
  if(at>=wire_length||!wire_result_decode(wire[at++],&parsed.result)||at>=wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  parsed.canonicalization=(legacy_identity_evidence_canonicalization)wire[at++];
  if(at>=wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;name_length=wire[at++];
  if(!wire_read(wire,wire_length,&at,name_length,&name))return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(at>=wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;shard_length=wire[at++];
  if(!wire_read(wire,wire_length,&at,shard_length,&shard))return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(at>=wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;digest_length=wire[at++];
  if(!wire_read(wire,wire_length,&at,digest_length,&digest))return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(at>=wire_length)return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;storage_length=wire[at++];
  if(!wire_read(wire,wire_length,&at,storage_length,&storage))return LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED;
  if(at!=wire_length||name_length>PLAYER_NAME_MAX_BYTES||shard_length>2||
     digest_length>LEGACY_IDENTITY_EVIDENCE_SHA256_HEX_LEN||
     storage_length>LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT_LEN)return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  if(!wire_text_nul_free(name,name_length)||!wire_text_nul_free(shard,shard_length)||
     !wire_text_nul_free(digest,digest_length)||
     !wire_text_nul_free(storage,storage_length))return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  memcpy(parsed.canonical_name,name,name_length);memcpy(parsed.legacy_shard,shard,shard_length);
  memcpy(parsed.player_file_sha256,digest,digest_length);memcpy(parsed.storage_format,storage,storage_length);
  if(!wire_evidence_valid(&parsed,name_length,shard_length,digest_length,storage_length))return LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL;
  *evidence=parsed;return LEGACY_IDENTITY_EVIDENCE_WIRE_OK; }

void legacy_identity_evidence_wire_free(wire)
unsigned char *wire;
{ free(wire); }
