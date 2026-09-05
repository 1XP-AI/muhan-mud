/* Test-only C oracle for the closed legacy identity evidence wire. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "legacy_identity_evidence_wire.h"

static int digit(value)
unsigned char value;
{ if(value>='0'&&value<='9')return value-'0'; if(value>='a'&&value<='f')return value-'a'+10; return -1; }

static int unhex(text, out, length)
const char *text; unsigned char **out; size_t *length;
{ size_t n,i; int high,low; unsigned char *value;
  n=strlen(text); if(n&1)return -1; value=(unsigned char *)malloc(n/2+1); if(!value)return -1;
  for(i=0;i<n;i+=2) { high=digit((unsigned char)text[i]); low=digit((unsigned char)text[i+1]); if(high<0||low<0){free(value);return -1;} value[i/2]=(unsigned char)((high<<4)|low); }
  *out=value; *length=n/2; return 0; }

static void hex(value,length)
const unsigned char *value; size_t length;
{ static const char digits[]="0123456789abcdef"; size_t i; for(i=0;i<length;i++){putchar(digits[value[i]>>4]);putchar(digits[value[i]&15]);} putchar('\n'); }

static int fixture(value, name)
legacy_identity_evidence *value; const char *name;
{ memset(value,0,sizeof(*value)); value->version=1; strcpy(value->storage_format,"player-v1");
  if(!strcmp(name,"invalid-input")) { value->result=LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT; value->canonicalization=LEGACY_IDENTITY_EVIDENCE_INVALID; return 0; }
  strcpy(value->canonical_name,"Alice"); strcpy(value->legacy_shard,"35");
  value->canonicalization=!strcmp(name,"ok")?LEGACY_IDENTITY_EVIDENCE_NORMALIZED:LEGACY_IDENTITY_EVIDENCE_CANONICAL;
  if(!strcmp(name,"ok")) { value->result=LEGACY_IDENTITY_EVIDENCE_OK; strcpy(value->player_file_sha256,"18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49"); return 0; }
  if(!strcmp(name,"not-found")) { value->result=LEGACY_IDENTITY_EVIDENCE_NOT_FOUND; return 0; }
  if(!strcmp(name,"corrupt")) { value->result=LEGACY_IDENTITY_EVIDENCE_CORRUPT; return 0; }
  if(!strcmp(name,"io-error")) { value->result=LEGACY_IDENTITY_EVIDENCE_IO_ERROR; return 0; }
  return -1; }

int main(argc,argv)
int argc; char **argv;
{ legacy_identity_evidence value; unsigned char *wire; size_t length; int status;
  if(argc==3&&!strcmp(argv[1],"decode")){if(unhex(argv[2],&wire,&length))return 2; status=legacy_identity_evidence_wire_decode(wire,length,&value);free(wire);puts(status==0?"0":"1");return 0;}
  if((argc==2||argc==3)&&!strcmp(argv[1],"fixture")){if(fixture(&value,argc==3?argv[2]:"ok")||legacy_identity_evidence_wire_encode(&value,&wire,&length))return 2;hex(wire,length);legacy_identity_evidence_wire_free(wire);return 0;}
  if(argc==3&&!strcmp(argv[1],"roundtrip")){if(unhex(argv[2],&wire,&length))return 2;status=legacy_identity_evidence_wire_decode(wire,length,&value);free(wire);if(status||legacy_identity_evidence_wire_encode(&value,&wire,&length))return 2;hex(wire,length);legacy_identity_evidence_wire_free(wire);return 0;}
  return 2;
}
