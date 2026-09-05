#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "legacy_identity_evidence_wire.h"

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr,"legacy_identity_evidence_wire_test: %s\n",message);
    return 1;
}

static int hex_digit(value)
unsigned char value;
{
    if(value>='0' && value<='9') return value-'0';
    if(value>='a' && value<='f') return value-'a'+10;
    return -1;
}

static int fixture(path, wire, wire_length)
const char *path;
unsigned char **wire;
size_t *wire_length;
{
    FILE *file;
    unsigned char *text, *out;
    long length;
    size_t i, used;
    int high, low;

    *wire=0; *wire_length=0;
    file=fopen(path,"rb");
    if(!file || fseek(file,0,SEEK_END) || (length=ftell(file))<0 ||
       fseek(file,0,SEEK_SET)) { if(file) fclose(file); return -1; }
    text=(unsigned char *)malloc((size_t)length+1);
    if(!text) { fclose(file); return -1; }
    if(fread(text,1,(size_t)length,file)!=(size_t)length || fclose(file)) {
        free(text); return -1;
    }
    out=(unsigned char *)malloc((size_t)length/2+1);
    if(!out) { free(text); return -1; }
    used=0; high=-1;
    for(i=0;i<(size_t)length;i++) {
        if(text[i]=='\n' || text[i]=='\r' || text[i]==' ' || text[i]=='\t') continue;
        low=hex_digit(text[i]);
        if(low<0) { free(text); free(out); return -1; }
        if(high<0) high=low;
        else { out[used++]=(unsigned char)((high<<4)|low); high=-1; }
    }
    free(text);
    if(high>=0) { free(out); return -1; }
    *wire=out; *wire_length=used;
    return 0;
}

static void valid(evidence)
legacy_identity_evidence *evidence;
{
    memset(evidence,0,sizeof(*evidence));
    evidence->version=LEGACY_IDENTITY_EVIDENCE_VERSION;
    evidence->result=LEGACY_IDENTITY_EVIDENCE_OK;
    evidence->canonicalization=LEGACY_IDENTITY_EVIDENCE_NORMALIZED;
    strcpy(evidence->canonical_name,"Alice");
    strcpy(evidence->legacy_shard,"35");
    strcpy(evidence->player_file_sha256,
           "18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49");
    strcpy(evidence->storage_format,LEGACY_IDENTITY_EVIDENCE_STORAGE_FORMAT);
}

int main(void)
{
    legacy_identity_evidence evidence, decoded;
    unsigned char *wire=0, *golden=0;
    size_t wire_length=0, golden_length=0;
    int failed=0;
    static const char *nul_fixtures[] = {
        "../tests/fixtures/legacy_identity_evidence_wire_v1_nul_name.hex",
        "../tests/fixtures/legacy_identity_evidence_wire_v1_nul_shard.hex",
        "../tests/fixtures/legacy_identity_evidence_wire_v1_nul_digest.hex",
        "../tests/fixtures/legacy_identity_evidence_wire_v1_nul_storage.hex"
    };
    size_t i;

    valid(&evidence);
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&wire,&wire_length)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_OK,
                   "valid metadata must encode");
    failed+=expect(fixture("../tests/fixtures/legacy_identity_evidence_wire_v1_ok.hex",
                           &golden,&golden_length)==0,
                   "shared OK golden must load");
    failed+=expect(wire_length==golden_length && !memcmp(wire,golden,wire_length),
                   "C encoding must equal the shared golden bytes");
    failed+=expect(legacy_identity_evidence_wire_decode(golden,golden_length,&decoded)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_OK && !memcmp(&evidence,&decoded,
                   sizeof(evidence)),"shared golden must decode to fixed metadata");
    free(golden); golden=0;

    for(i=0;i<sizeof(nul_fixtures)/sizeof(nul_fixtures[0]);i++) {
        failed+=expect(fixture(nul_fixtures[i],&golden,&golden_length)==0 &&
                       legacy_identity_evidence_wire_decode(golden,golden_length,&decoded)==
                       LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL,
                       "embedded NUL in a canonical text field must fail before C string use");
        free(golden); golden=0;
    }
    valid(&evidence);
    memcpy(evidence.canonical_name,"Al\0ice",6);
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&golden,&golden_length)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL,
                   "C encoder must reject an embedded-NUL canonical name instead of truncating it");
    free(golden); golden=0;
    valid(&evidence);

    evidence.result=LEGACY_IDENTITY_EVIDENCE_NOT_FOUND;
    evidence.canonicalization=LEGACY_IDENTITY_EVIDENCE_CANONICAL;
    strcpy(evidence.canonical_name,"Alice");
    memset(evidence.player_file_sha256,0,sizeof(evidence.player_file_sha256));
    legacy_identity_evidence_wire_free(wire); wire=0;
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&wire,&wire_length)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_OK,
                   "not-found outcome without digest must encode");
    failed+=expect(legacy_identity_evidence_wire_decode(wire,wire_length,&decoded)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_OK && decoded.result==evidence.result &&
                   !decoded.player_file_sha256[0],"not-found must round-trip without digest");

    evidence.result=LEGACY_IDENTITY_EVIDENCE_OK;
    evidence.player_file_sha256[0]=0;
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&golden,&golden_length)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL,
                   "OK outcome without exact SHA-256 must be rejected");
    evidence.result=LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT;
    evidence.canonicalization=LEGACY_IDENTITY_EVIDENCE_INVALID;
    memset(evidence.canonical_name,0,sizeof(evidence.canonical_name));
    memset(evidence.legacy_shard,0,sizeof(evidence.legacy_shard));
    memset(evidence.player_file_sha256,0,sizeof(evidence.player_file_sha256));
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&golden,&golden_length)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_OK,
                   "invalid-input outcome must encode with no name, shard, or digest");
    free(wire); wire=0;
    failed+=expect(fixture("../tests/fixtures/legacy_identity_evidence_wire_v1_invalid_input.hex",
                           &wire,&wire_length)==0,
                   "shared invalid-input golden must load");
    failed+=expect(wire_length==golden_length && !memcmp(wire,golden,wire_length) &&
                   legacy_identity_evidence_wire_decode(wire,wire_length,&decoded)==0 &&
                   decoded.result==LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT,
                   "invalid-input C encoding and decoding must equal shared golden bytes");
    free(golden); golden=0;

    valid(&evidence);
    legacy_identity_evidence_wire_free(wire); wire=0;
    failed+=expect(legacy_identity_evidence_wire_encode(&evidence,&wire,&wire_length)==0,
                   "fixture must re-encode for rejection cases");
    wire[11]=2;
    failed+=expect(legacy_identity_evidence_wire_decode(wire,wire_length,&decoded)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_UNSUPPORTED_VERSION,
                   "unknown wire version must fail closed");
    wire[11]=1; wire[16]=99;
    failed+=expect(legacy_identity_evidence_wire_decode(wire,wire_length,&decoded)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_NONCANONICAL,
                   "unknown outcome must fail closed");
    wire[16]=0; wire[18]=250;
    failed+=expect(legacy_identity_evidence_wire_decode(wire,wire_length,&decoded)==
                   LEGACY_IDENTITY_EVIDENCE_WIRE_TRUNCATED,
                   "inconsistent length must fail closed");
    legacy_identity_evidence_wire_free(wire);
    return failed ? 1 : 0;
}
