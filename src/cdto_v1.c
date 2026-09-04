/* Test-only canonical CDTO v1 envelope codec.  This file intentionally has
 * no dependency on legacy persistence readers/writers. */
#include "cdto_v1.h"
#include "mstruct.h"

#include <stddef.h>
#include <stdlib.h>
#include <string.h>

typedef unsigned int cdto_u32;

typedef struct cdto_sha256_ctx {
    cdto_u32 state[8];
    cdto_u32 bit_hi;
    cdto_u32 bit_lo;
    unsigned char block[64];
    unsigned int used;
} cdto_sha256_ctx;

static cdto_u32 cdto_rotr(value, shift)
cdto_u32 value;
unsigned int shift;
{
    return (value >> shift) | (value << (32 - shift));
}

static cdto_u32 cdto_load32(value)
const unsigned char *value;
{
    return ((cdto_u32)value[0] << 24) | ((cdto_u32)value[1] << 16) |
           ((cdto_u32)value[2] << 8) | (cdto_u32)value[3];
}

static void cdto_store32(value, out)
cdto_u32 value;
unsigned char *out;
{
    out[0] = (unsigned char)(value >> 24);
    out[1] = (unsigned char)(value >> 16);
    out[2] = (unsigned char)(value >> 8);
    out[3] = (unsigned char)value;
}

static void cdto_sha256_block(ctx, block)
cdto_sha256_ctx *ctx;
const unsigned char *block;
{
    static const cdto_u32 k[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };
    cdto_u32 w[64], a, b, c, d, e, f, g, h, t1, t2;
    unsigned int i;

    for (i = 0; i < 16; ++i) w[i] = cdto_load32(block + 4 * i);
    for (i = 16; i < 64; ++i) {
        cdto_u32 s0 = cdto_rotr(w[i - 15], 7) ^ cdto_rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
        cdto_u32 s1 = cdto_rotr(w[i - 2], 17) ^ cdto_rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
        w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    a = ctx->state[0]; b = ctx->state[1]; c = ctx->state[2]; d = ctx->state[3];
    e = ctx->state[4]; f = ctx->state[5]; g = ctx->state[6]; h = ctx->state[7];
    for (i = 0; i < 64; ++i) {
        cdto_u32 s1 = cdto_rotr(e, 6) ^ cdto_rotr(e, 11) ^ cdto_rotr(e, 25);
        cdto_u32 ch = (e & f) ^ ((~e) & g);
        cdto_u32 s0 = cdto_rotr(a, 2) ^ cdto_rotr(a, 13) ^ cdto_rotr(a, 22);
        cdto_u32 maj = (a & b) ^ (a & c) ^ (b & c);
        t1 = h + s1 + ch + k[i] + w[i];
        t2 = s0 + maj;
        h = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
    }
    ctx->state[0] += a; ctx->state[1] += b; ctx->state[2] += c; ctx->state[3] += d;
    ctx->state[4] += e; ctx->state[5] += f; ctx->state[6] += g; ctx->state[7] += h;
}

static void cdto_sha256_init(ctx)
cdto_sha256_ctx *ctx;
{
    ctx->state[0]=0x6a09e667U; ctx->state[1]=0xbb67ae85U;
    ctx->state[2]=0x3c6ef372U; ctx->state[3]=0xa54ff53aU;
    ctx->state[4]=0x510e527fU; ctx->state[5]=0x9b05688cU;
    ctx->state[6]=0x1f83d9abU; ctx->state[7]=0x5be0cd19U;
    ctx->bit_hi = ctx->bit_lo = 0;
    ctx->used = 0;
}

static void cdto_sha256_add_bits(ctx, bytes)
cdto_sha256_ctx *ctx;
unsigned int bytes;
{
    cdto_u32 old = ctx->bit_lo;
    ctx->bit_lo += ((cdto_u32)bytes << 3);
    if (ctx->bit_lo < old) ++ctx->bit_hi;
    ctx->bit_hi += ((cdto_u32)bytes >> 29);
}

static void cdto_sha256_update(ctx, data, length)
cdto_sha256_ctx *ctx;
const unsigned char *data;
size_t length;
{
    unsigned int take;
    while (length) {
        take = 64 - ctx->used;
        if (take > length) take = (unsigned int)length;
        memcpy(ctx->block + ctx->used, data, take);
        ctx->used += take;
        data += take;
        length -= take;
        cdto_sha256_add_bits(ctx, take);
        if (ctx->used == 64) {
            cdto_sha256_block(ctx, ctx->block);
            ctx->used = 0;
        }
    }
}

static void cdto_sha256_final(ctx, out)
cdto_sha256_ctx *ctx;
unsigned char out[CDTO_V1_DIGEST_LENGTH];
{
    unsigned int i;
    cdto_u32 hi = ctx->bit_hi, lo = ctx->bit_lo;
    ctx->block[ctx->used++] = 0x80;
    if (ctx->used > 56) {
        while (ctx->used < 64) ctx->block[ctx->used++] = 0;
        cdto_sha256_block(ctx, ctx->block);
        ctx->used = 0;
    }
    while (ctx->used < 56) ctx->block[ctx->used++] = 0;
    cdto_store32(hi, ctx->block + 56);
    cdto_store32(lo, ctx->block + 60);
    cdto_sha256_block(ctx, ctx->block);
    for (i = 0; i < 8; ++i) cdto_store32(ctx->state[i], out + 4 * i);
}

static void cdto_sha256(data, length, digest)
const unsigned char *data;
size_t length;
unsigned char digest[CDTO_V1_DIGEST_LENGTH];
{
    cdto_sha256_ctx ctx;
    cdto_sha256_init(&ctx);
    cdto_sha256_update(&ctx, data, length);
    cdto_sha256_final(&ctx, digest);
}

static void cdto_put16(out, value)
unsigned char *out;
uint16_t value;
{
    out[0] = (unsigned char)(value >> 8);
    out[1] = (unsigned char)value;
}

static void cdto_put32(out, value)
unsigned char *out;
uint32_t value;
{
    out[0] = (unsigned char)(value >> 24);
    out[1] = (unsigned char)(value >> 16);
    out[2] = (unsigned char)(value >> 8);
    out[3] = (unsigned char)value;
}

static uint16_t cdto_get16(input)
const unsigned char *input;
{
    return (uint16_t)(((uint16_t)input[0] << 8) | input[1]);
}

static uint32_t cdto_get32(input)
const unsigned char *input;
{
    return ((uint32_t)input[0] << 24) | ((uint32_t)input[1] << 16) |
           ((uint32_t)input[2] << 8) | input[3];
}

static size_t cdto_payload_limit(kind)
uint16_t kind;
{
    switch (kind) {
    case CDTO_V1_KIND_CREATURE: return CDTO_V1_CREATURE_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_OBJECT: return CDTO_V1_OBJECT_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_ROOM: return CDTO_V1_ROOM_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_SESSION: return CDTO_V1_SESSION_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_ABI_FINGERPRINT: return CDTO_V1_SESSION_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_OBJECT_GRAPH: return CDTO_V1_OBJECT_GRAPH_PAYLOAD_LIMIT;
    case CDTO_V1_KIND_PLAYER_SNAPSHOT: return CDTO_V1_PLAYER_SNAPSHOT_PAYLOAD_LIMIT;
    }
    return 0;
}

static int cdto_utf8(value, length)
const unsigned char *value;
uint32_t length;
{
    uint32_t i = 0;
    unsigned char c;
    while (i < length) {
        c = value[i++];
        if (c <= 0x7f) continue;
        if (c >= 0xc2 && c <= 0xdf) {
            if (i >= length || (value[i] & 0xc0) != 0x80) return 0;
            ++i;
        } else if (c == 0xe0) {
            if (i + 1 >= length || value[i] < 0xa0 || value[i] > 0xbf ||
                (value[i + 1] & 0xc0) != 0x80) return 0;
            i += 2;
        } else if (c >= 0xe1 && c <= 0xec) {
            if (i + 1 >= length || (value[i] & 0xc0) != 0x80 ||
                (value[i + 1] & 0xc0) != 0x80) return 0;
            i += 2;
        } else if (c == 0xed) {
            if (i + 1 >= length || value[i] < 0x80 || value[i] > 0x9f ||
                (value[i + 1] & 0xc0) != 0x80) return 0;
            i += 2;
        } else if (c >= 0xee && c <= 0xef) {
            if (i + 1 >= length || (value[i] & 0xc0) != 0x80 ||
                (value[i + 1] & 0xc0) != 0x80) return 0;
            i += 2;
        } else if (c == 0xf0) {
            if (i + 2 >= length || value[i] < 0x90 || value[i] > 0xbf ||
                (value[i + 1] & 0xc0) != 0x80 || (value[i + 2] & 0xc0) != 0x80) return 0;
            i += 3;
        } else if (c >= 0xf1 && c <= 0xf3) {
            if (i + 2 >= length || (value[i] & 0xc0) != 0x80 ||
                (value[i + 1] & 0xc0) != 0x80 || (value[i + 2] & 0xc0) != 0x80) return 0;
            i += 3;
        } else if (c == 0xf4) {
            if (i + 2 >= length || value[i] < 0x80 || value[i] > 0x8f ||
                (value[i + 1] & 0xc0) != 0x80 || (value[i + 2] & 0xc0) != 0x80) return 0;
            i += 3;
        } else return 0;
    }
    return 1;
}

static int cdto_validate_type(field)
const cdto_v1_field *field;
{
    unsigned char base = field->type_tag & ~CDTO_V1_OPTIONAL_TYPE_BIT;
    int optional = (field->type_tag & CDTO_V1_OPTIONAL_TYPE_BIT) != 0;
    if (field->length && !field->value) return CDTO_V1_INVALID_ARGUMENT;
    switch (base) {
    case CDTO_V1_TYPE_U8: case CDTO_V1_TYPE_I8:
        return field->length == 1 ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
    case CDTO_V1_TYPE_U16: case CDTO_V1_TYPE_I16:
        return field->length == 2 ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
    case CDTO_V1_TYPE_U32: case CDTO_V1_TYPE_I32:
        return field->length == 4 ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
    case CDTO_V1_TYPE_U64: case CDTO_V1_TYPE_I64:
        return field->length == 8 ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
    case CDTO_V1_TYPE_BYTES:
        return CDTO_V1_OK;
    case CDTO_V1_TYPE_TEXT:
        return cdto_utf8(field->value, field->length) ? CDTO_V1_OK : CDTO_V1_INVALID_UTF8;
    case CDTO_V1_TYPE_BOOL:
        if (field->length != 1 || !field->value || (field->value[0] != 0 && field->value[0] != 1))
            return CDTO_V1_INVALID_BOOLEAN;
        return CDTO_V1_OK;
    default:
        return optional ? CDTO_V1_OK : CDTO_V1_UNKNOWN_MANDATORY_TYPE;
    }
}

static int cdto_validate_fields(fields, field_count, payload_length)
const cdto_v1_field *fields;
size_t field_count;
size_t *payload_length;
{
    size_t i, total = 0;
    int status;
    if (field_count > CDTO_V1_MAX_FIELDS || (field_count && !fields)) return CDTO_V1_INVALID_ARGUMENT;
    for (i = 0; i < field_count; ++i) {
        if (i && fields[i].id == fields[i - 1].id) return CDTO_V1_DUPLICATE_FIELD;
        if (i && fields[i].id < fields[i - 1].id) return CDTO_V1_OUT_OF_ORDER_FIELD;
        status = cdto_validate_type(&fields[i]);
        if (status != CDTO_V1_OK) return status;
        if (total > (size_t)-1 - CDTO_V1_FIELD_HEADER_LENGTH)
            return CDTO_V1_LENGTH_OVERFLOW;
        total += CDTO_V1_FIELD_HEADER_LENGTH;
        if (total > (size_t)-1 - (size_t)fields[i].length)
            return CDTO_V1_LENGTH_OVERFLOW;
        total += (size_t)fields[i].length;
    }
    *payload_length = total;
    return CDTO_V1_OK;
}

int cdto_v1_encode(record, wire, wire_length)
const cdto_v1_record *record;
uint8_t **wire;
size_t *wire_length;
{
    size_t limit, payload_length, total, i, cursor;
    unsigned char digest[CDTO_V1_DIGEST_LENGTH];
    int status;
    if (!record || !wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    *wire = 0;
    *wire_length = 0;
    limit = cdto_payload_limit(record->kind);
    if (!limit) return CDTO_V1_UNKNOWN_KIND;
    status = cdto_validate_fields(record->fields, record->field_count, &payload_length);
    if (status != CDTO_V1_OK) return status;
    if (payload_length > limit) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    if (payload_length > (size_t)-1 - CDTO_V1_PREFIX_LENGTH - CDTO_V1_DIGEST_LENGTH)
        return CDTO_V1_LENGTH_OVERFLOW;
    total = CDTO_V1_PREFIX_LENGTH + payload_length + CDTO_V1_DIGEST_LENGTH;
    if (total > CDTO_V1_MAX_ENVELOPE_SIZE) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    *wire = (uint8_t *)malloc(total);
    if (!*wire) return CDTO_V1_ALLOCATION_FAILED;
    memcpy(*wire, CDTO_V1_MAGIC, CDTO_V1_MAGIC_LENGTH);
    cdto_put16(*wire + 8, CDTO_V1_WIRE_VERSION);
    cdto_put16(*wire + 10, record->kind);
    cdto_put32(*wire + 12, (uint32_t)payload_length);
    cursor = CDTO_V1_PREFIX_LENGTH;
    for (i = 0; i < record->field_count; ++i) {
        cdto_put16(*wire + cursor, record->fields[i].id);
        (*wire)[cursor + 2] = record->fields[i].type_tag;
        cdto_put32(*wire + cursor + 3, record->fields[i].length);
        cursor += CDTO_V1_FIELD_HEADER_LENGTH;
        if (record->fields[i].length) memcpy(*wire + cursor, record->fields[i].value, record->fields[i].length);
        cursor += record->fields[i].length;
    }
    cdto_sha256(*wire + CDTO_V1_PREFIX_LENGTH, payload_length, digest);
    memcpy(*wire + cursor, digest, CDTO_V1_DIGEST_LENGTH);
    *wire_length = total;
    return CDTO_V1_OK;
}

void cdto_v1_free_wire(wire)
uint8_t *wire;
{
    free(wire);
}

void cdto_v1_free_decoded(record)
cdto_v1_decoded_record *record;
{
    size_t i;
    if (!record) return;
    for (i = 0; i < record->field_count; ++i) free(record->fields[i].value);
    free(record->fields);
    record->kind = 0;
    record->fields = 0;
    record->field_count = 0;
}

int cdto_v1_decode(wire, wire_length, record)
const uint8_t *wire;
size_t wire_length;
cdto_v1_decoded_record *record;
{
    size_t limit, payload_length, payload_end, cursor, count, capacity;
    uint16_t kind, previous;
    unsigned char digest[CDTO_V1_DIGEST_LENGTH];
    cdto_v1_field field;
    cdto_v1_decoded_field *next;
    int status;
    if (!record || (!wire && wire_length)) return CDTO_V1_INVALID_ARGUMENT;
    memset(record, 0, sizeof(*record));
    if (wire_length > CDTO_V1_MAX_ENVELOPE_SIZE) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    if (wire_length < CDTO_V1_MAGIC_LENGTH) return CDTO_V1_TRUNCATED;
    if (memcmp(wire, CDTO_V1_MAGIC, CDTO_V1_MAGIC_LENGTH)) return CDTO_V1_INVALID_MAGIC;
    if (wire_length < CDTO_V1_PREFIX_LENGTH) return CDTO_V1_TRUNCATED;
    if (cdto_get16(wire + 8) != CDTO_V1_WIRE_VERSION) return CDTO_V1_UNSUPPORTED_VERSION;
    kind = cdto_get16(wire + 10);
    limit = cdto_payload_limit(kind);
    if (!limit) return CDTO_V1_UNKNOWN_KIND;
    payload_length = cdto_get32(wire + 12);
    if (payload_length > limit) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    payload_end = CDTO_V1_PREFIX_LENGTH + payload_length;
    if (wire_length < payload_end + CDTO_V1_DIGEST_LENGTH) return CDTO_V1_TRUNCATED;
    if (wire_length > payload_end + CDTO_V1_DIGEST_LENGTH) return CDTO_V1_TRAILING_BYTES;
    cdto_sha256(wire + CDTO_V1_PREFIX_LENGTH, payload_length, digest);
    if (memcmp(digest, wire + payload_end, CDTO_V1_DIGEST_LENGTH)) return CDTO_V1_DIGEST_MISMATCH;
    cursor = CDTO_V1_PREFIX_LENGTH;
    count = capacity = 0;
    previous = 0;
    while (cursor < payload_end) {
        if (payload_end - cursor < CDTO_V1_FIELD_HEADER_LENGTH) { status = CDTO_V1_TRUNCATED; goto fail; }
        field.id = cdto_get16(wire + cursor);
        field.type_tag = wire[cursor + 2];
        field.length = cdto_get32(wire + cursor + 3);
        cursor += CDTO_V1_FIELD_HEADER_LENGTH;
        if ((size_t)field.length > payload_end - cursor) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto fail; }
        field.value = wire + cursor;
        if (count && field.id == previous) { status = CDTO_V1_DUPLICATE_FIELD; goto fail; }
        if (count && field.id < previous) { status = CDTO_V1_OUT_OF_ORDER_FIELD; goto fail; }
        status = cdto_validate_type(&field);
        if (status != CDTO_V1_OK) goto fail;
        if (count == CDTO_V1_MAX_FIELDS) { status = CDTO_V1_SIZE_LIMIT_EXCEEDED; goto fail; }
        if (count == capacity) {
            size_t new_capacity = capacity ? capacity * 2 : 8;
            if (new_capacity > CDTO_V1_MAX_FIELDS) new_capacity = CDTO_V1_MAX_FIELDS;
            next = (cdto_v1_decoded_field *)realloc(record->fields, new_capacity * sizeof(*next));
            if (!next) { status = CDTO_V1_ALLOCATION_FAILED; goto fail; }
            record->fields = next;
            capacity = new_capacity;
        }
        record->fields[count].id = field.id;
        record->fields[count].type_tag = field.type_tag;
        record->fields[count].length = field.length;
        record->fields[count].value = 0;
        if (field.length) {
            record->fields[count].value = (uint8_t *)malloc(field.length);
            if (!record->fields[count].value) { status = CDTO_V1_ALLOCATION_FAILED; goto fail; }
            memcpy(record->fields[count].value, field.value, field.length);
        }
        ++count;
        record->field_count = count;
        previous = field.id;
        cursor += field.length;
    }
    record->kind = kind;
    return CDTO_V1_OK;
fail:
    cdto_v1_free_decoded(record);
    return status;
}

static void cdto_u64(value, out)
uint64_t value;
unsigned char out[8];
{
    unsigned int i;
    for (i = 0; i < 8; ++i) out[i] = (unsigned char)(value >> (56 - 8 * i));
}

#if defined(__APPLE__)
#define CDTO_OS_NAME "macos"
#elif defined(__linux__)
#define CDTO_OS_NAME "linux"
#elif defined(_WIN32)
#define CDTO_OS_NAME "windows"
#else
#define CDTO_OS_NAME "unknown"
#endif

#if defined(__aarch64__) || defined(__arm64__)
#define CDTO_ARCH_NAME "aarch64"
#elif defined(__x86_64__) || defined(_M_X64)
#define CDTO_ARCH_NAME "x86_64"
#elif defined(__i386__) || defined(_M_IX86)
#define CDTO_ARCH_NAME "x86"
#else
#define CDTO_ARCH_NAME "unknown"
#endif

int cdto_v1_encode_abi_fingerprint(wire, wire_length)
uint8_t **wire;
size_t *wire_length;
{
    enum { FIELD_COUNT = 22 };
    cdto_v1_field fields[FIELD_COUNT];
    unsigned char values[FIELD_COUNT - 2][8];
    const char *os = CDTO_OS_NAME;
    const char *arch = CDTO_ARCH_NAME;
    uint64_t numbers[FIELD_COUNT - 2];
    cdto_v1_record record;
    unsigned int i;

    numbers[0] = sizeof(long); numbers[1] = sizeof(void *);
    numbers[2] = sizeof(object); numbers[3] = sizeof(creature);
    numbers[4] = sizeof(room); numbers[5] = sizeof(iobuf);
    numbers[6] = offsetof(object, first_obj); numbers[7] = offsetof(object, parent_obj);
    numbers[8] = offsetof(object, parent_rom); numbers[9] = offsetof(object, parent_crt);
    numbers[10] = offsetof(creature, ready); numbers[11] = offsetof(creature, first_obj);
    numbers[12] = offsetof(creature, parent_rom); numbers[13] = offsetof(room, first_ext);
    numbers[14] = offsetof(room, first_obj); numbers[15] = offsetof(room, first_mon);
    numbers[16] = offsetof(room, first_ply); numbers[17] = offsetof(iobuf, input);
    numbers[18] = offsetof(iobuf, output); numbers[19] = offsetof(iobuf, ltime);
    fields[0].id = 1; fields[0].type_tag = CDTO_V1_TYPE_TEXT;
    fields[0].value = (const uint8_t *)os; fields[0].length = (uint32_t)strlen(os);
    fields[1].id = 2; fields[1].type_tag = CDTO_V1_TYPE_TEXT;
    fields[1].value = (const uint8_t *)arch; fields[1].length = (uint32_t)strlen(arch);
    for (i = 0; i < FIELD_COUNT - 2; ++i) {
        cdto_u64(numbers[i], values[i]);
        fields[i + 2].id = (uint16_t)(i + 3);
        fields[i + 2].type_tag = CDTO_V1_TYPE_U64;
        fields[i + 2].value = values[i];
        fields[i + 2].length = 8;
    }
    record.kind = CDTO_V1_ABI_FINGERPRINT_KIND;
    record.fields = fields;
    record.field_count = FIELD_COUNT;
    return cdto_v1_encode(&record, wire, wire_length);
}
