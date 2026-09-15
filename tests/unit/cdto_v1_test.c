#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cdto_v1.h"

static int expect(condition, message)
int condition;
const char *message;
{
    if (condition)
        return 0;
    fprintf(stderr, "cdto_v1_test: %s\n", message);
    return 1;
}

static void hex(out, input, length)
char *out;
const unsigned char *input;
size_t length;
{
    static const char digits[] = "0123456789abcdef";
    size_t index;

    for (index = 0; index < length; ++index) {
        out[index * 2] = digits[input[index] >> 4];
        out[index * 2 + 1] = digits[input[index] & 15];
    }
    out[length * 2] = 0;
}

static int nibble(value)
char value;
{
    if (value >= '0' && value <= '9') return value - '0';
    if (value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static uint64_t read_u64(input)
const unsigned char *input;
{
    uint64_t value = 0;
    unsigned int index;

    for (index = 0; index < 8; ++index)
        value = (value << 8) | input[index];
    return value;
}

static int expect_hex_decode(input, expected, message)
const char *input;
int expected;
const char *message;
{
    size_t length = strlen(input) / 2;
    unsigned char *wire = (unsigned char *)malloc(length);
    cdto_v1_decoded_record decoded;
    size_t index;
    int actual;
    int failed;

    if (!wire) return expect(0, "test allocation failed");
    for (index = 0; index < length; ++index)
        wire[index] = (unsigned char)((nibble(input[index * 2]) << 4) |
                                      nibble(input[index * 2 + 1]));
    memset(&decoded, 0, sizeof(decoded));
    actual = cdto_v1_decode(wire, length, &decoded);
    failed = expect(actual == expected, message);
    cdto_v1_free_decoded(&decoded);
    free(wire);
    return failed;
}

int main(void)
{
    static const unsigned char terra[] = "Terra";
    static const unsigned char answer[] = { 0, 0, 0, 42 };
    static const char rust_golden[] =
        "4d55484344544f000001000100000017000109000000055465727261"
        "000203000000040000002abd42d992f5822c03c8cd5f3d1f5d59ba"
        "f16d471022c81989109304a43cd57347";
    cdto_v1_field fields[2];
    cdto_v1_record record;
    cdto_v1_decoded_record decoded;
    unsigned char *wire;
    unsigned char *abi_wire;
    unsigned char *player_snapshot_wire;
    unsigned char *player_snapshot_limit_value;
    unsigned char *trailing_wire;
    size_t wire_length;
    size_t abi_length;
    size_t player_snapshot_length;
    char fixture_hex[(16 + 23 + 32) * 2 + 1];
    int failed;

    fields[0].id = 1;
    fields[0].type_tag = CDTO_V1_TYPE_BYTES;
    fields[0].value = terra;
    fields[0].length = 5;
    fields[1].id = 2;
    fields[1].type_tag = CDTO_V1_TYPE_U32;
    fields[1].value = answer;
    fields[1].length = sizeof(answer);
    record.kind = CDTO_V1_KIND_CREATURE;
    record.fields = fields;
    record.field_count = 2;
    wire = 0;
    abi_wire = 0;
    player_snapshot_wire = 0;
    player_snapshot_limit_value = 0;
    trailing_wire = 0;
    memset(&decoded, 0, sizeof(decoded));
    failed = 0;

    failed += expect(cdto_v1_encode(&record, &wire, &wire_length) == CDTO_V1_OK,
                     "canonical fixture must encode");
    failed += expect(wire_length == 71, "fixture envelope length must be stable");
    failed += expect(!memcmp(wire, "MUHCDTO\0", 8), "magic must be exact");
    failed += expect(wire[8] == 0 && wire[9] == 1 && wire[10] == 0 && wire[11] == 1,
                     "version and kind must be big-endian");
    failed += expect(wire[12] == 0 && wire[13] == 0 && wire[14] == 0 && wire[15] == 23,
                     "payload length must be big-endian");
    hex(fixture_hex, wire, wire_length);
    failed += expect(!strcmp(fixture_hex, rust_golden),
                     "C fixture must equal the Rust-decodable golden bytes");
    failed += expect(cdto_v1_decode(wire, wire_length, &decoded) == CDTO_V1_OK,
                     "canonical fixture must decode");
    failed += expect(decoded.kind == CDTO_V1_KIND_CREATURE && decoded.field_count == 2,
                     "decoded fields must retain kind and count");
    failed += expect(decoded.fields[0].id == 1 && decoded.fields[1].id == 2 &&
                     decoded.fields[1].length == 4 &&
                     !memcmp(decoded.fields[0].value, terra, 5),
                     "decoded field values must retain bytes");
    cdto_v1_free_decoded(&decoded);

    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    failed += expect(cdto_v1_encode(&record, &player_snapshot_wire,
                                    &player_snapshot_length) == CDTO_V1_OK,
                     "player snapshot kind must encode within its explicit limit");
    failed += expect(player_snapshot_wire != 0 &&
                     player_snapshot_wire[10] == 0 && player_snapshot_wire[11] == 7,
                     "player snapshot wire kind must be explicit and big-endian");
    if (player_snapshot_wire) {
        failed += expect(cdto_v1_decode(player_snapshot_wire, player_snapshot_length,
                                        &decoded) == CDTO_V1_OK,
                         "player snapshot kind must decode");
        failed += expect(decoded.kind == CDTO_V1_KIND_PLAYER_SNAPSHOT,
                         "decoded player snapshot kind must be retained");
        cdto_v1_free_decoded(&decoded);
    }
    cdto_v1_free_wire(player_snapshot_wire);
    player_snapshot_wire = 0;
    player_snapshot_limit_value = (unsigned char *)malloc(
        CDTO_V1_PLAYER_SNAPSHOT_PAYLOAD_LIMIT);
    failed += expect(player_snapshot_limit_value != 0,
                     "player snapshot limit fixture allocation must succeed");
    if (player_snapshot_limit_value) {
        fields[0].value = player_snapshot_limit_value;
        fields[0].length = CDTO_V1_PLAYER_SNAPSHOT_PAYLOAD_LIMIT -
                           CDTO_V1_FIELD_HEADER_LENGTH;
        record.field_count = 1;
        failed += expect(cdto_v1_encode(&record, &player_snapshot_wire,
                                        &player_snapshot_length) == CDTO_V1_OK,
                         "player snapshot payload at 4 MiB must encode");
        cdto_v1_free_wire(player_snapshot_wire);
        player_snapshot_wire = 0;
        fields[0].length = CDTO_V1_PLAYER_SNAPSHOT_PAYLOAD_LIMIT -
                           CDTO_V1_FIELD_HEADER_LENGTH + 1;
        failed += expect(cdto_v1_encode(&record, &player_snapshot_wire,
                                        &player_snapshot_length) ==
                         CDTO_V1_SIZE_LIMIT_EXCEEDED,
                         "player snapshot payload above 4 MiB must be rejected");
        free(player_snapshot_limit_value);
        player_snapshot_limit_value = 0;
        fields[0].value = terra;
        fields[0].length = 5;
        record.field_count = 2;
    }
    record.kind = CDTO_V1_KIND_CREATURE;

    wire[wire_length - 1] ^= 1;
    failed += expect(cdto_v1_decode(wire, wire_length, &decoded) == CDTO_V1_DIGEST_MISMATCH,
                     "digest mutation must be rejected");
    wire[wire_length - 1] ^= 1;
    failed += expect(cdto_v1_decode(wire, wire_length - 1, &decoded) == CDTO_V1_TRUNCATED,
                     "truncated envelope must be rejected");
    trailing_wire = (unsigned char *)malloc(wire_length + 1);
    failed += expect(trailing_wire != 0, "trailing test allocation must succeed");
    if (trailing_wire) {
        memcpy(trailing_wire, wire, wire_length);
        trailing_wire[wire_length] = 0;
        failed += expect(cdto_v1_decode(trailing_wire, wire_length + 1, &decoded) == CDTO_V1_TRAILING_BYTES,
                         "trailing bytes must be rejected");
        free(trailing_wire);
        trailing_wire = 0;
    }
    wire[16] = 0;
    wire[17] = 2;
    failed += expect(cdto_v1_decode(wire, wire_length, &decoded) == CDTO_V1_DIGEST_MISMATCH,
                     "payload mutation before canonical checks must fail digest");
    wire[16] = 0;
    wire[17] = 1;

    fields[1].id = 1;
    failed += expect(cdto_v1_encode(&record, &abi_wire, &abi_length) == CDTO_V1_DUPLICATE_FIELD,
                     "encoder must reject duplicate field IDs");
    fields[0].id = 2;
    fields[1].id = 1;
    failed += expect(cdto_v1_encode(&record, &abi_wire, &abi_length) == CDTO_V1_OUT_OF_ORDER_FIELD,
                     "encoder must reject out-of-order field IDs");
    fields[0].id = 1;
    fields[1].id = 2;
    fields[1].type_tag = CDTO_V1_TYPE_BYTES;
    fields[1].length = 0xffffffffU;
    failed += expect(cdto_v1_encode(&record, &abi_wire, &abi_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED,
                     "payloads above the per-kind allocation cap must be rejected");
    fields[1].type_tag = CDTO_V1_TYPE_U32;
    fields[1].length = sizeof(answer);
    failed += expect_hex_decode(
        "4d55484344544f00000100040000001000010100000001010001010000000102"
        "56721d45f3407a216bb58edd8098250a5415e56ed4530401cb47c8709e7e3e13",
        CDTO_V1_DUPLICATE_FIELD, "decoder must reject duplicate field IDs");
    failed += expect_hex_decode(
        "4d55484344544f00000100040000001000020100000001010001010000000102"
        "78c551a43bba76ed6e648a5174c3db78f38e75ca3f3c05c5241bfd74bd166c47",
        CDTO_V1_OUT_OF_ORDER_FIELD, "decoder must reject out-of-order field IDs");
    failed += expect_hex_decode(
        "4d55484344544f0000010004000000080001610000000101"
        "d53a187de31420abb6d0dd9126dffb0b5e51f43a495177c95fe97cab87d1ac0e",
        CDTO_V1_UNKNOWN_MANDATORY_TYPE, "decoder must reject unknown mandatory types");
    failed += expect_hex_decode(
        "4d55484344544f00000100040000000800010a00000001ff"
        "7e343920d6a498fccbdb7b8ae8e13605131de3849d1bfbae8e346b6db8c2c286",
        CDTO_V1_INVALID_UTF8, "decoder must reject invalid UTF-8 text");
    failed += expect_hex_decode(
        "4d55484344544f0000010004000000080001010000000201"
        "69baae4ad396e395cb570c5f08b43d33d7616654b2909ba6811be14748ce64fd",
        CDTO_V1_INVALID_FIELD_LENGTH, "decoder must reject field lengths past payload");

    failed += expect(cdto_v1_encode_abi_fingerprint(&abi_wire, &abi_length) == CDTO_V1_OK,
                     "ABI fingerprint must encode");
    failed += expect(abi_wire[10] == 0 && abi_wire[11] == CDTO_V1_ABI_FINGERPRINT_KIND,
                     "ABI fingerprint kind must be explicit and Rust-known");
    failed += expect(cdto_v1_decode(abi_wire, abi_length, &decoded) == CDTO_V1_OK,
                     "ABI fingerprint must decode as CDTO");
    failed += expect(decoded.kind == CDTO_V1_ABI_FINGERPRINT_KIND && decoded.field_count == 22,
                     "ABI fingerprint must expose logical layout fields");
    failed += expect(decoded.fields[2].id == 3 && decoded.fields[2].type_tag == CDTO_V1_TYPE_U64 &&
                     decoded.fields[2].length == 8 && read_u64(decoded.fields[2].value) == sizeof(long) &&
                     decoded.fields[3].id == 4 && read_u64(decoded.fields[3].value) == sizeof(void *) &&
                     decoded.fields[4].id == 5 && read_u64(decoded.fields[4].value) > 0 &&
                     decoded.fields[5].id == 6 && read_u64(decoded.fields[5].value) > 0 &&
                     decoded.fields[6].id == 7 && read_u64(decoded.fields[6].value) > 0 &&
                     decoded.fields[7].id == 8 && read_u64(decoded.fields[7].value) > 0,
                     "ABI fingerprint must use explicit logical sizeof fields");
    failed += expect(decoded.fields[8].id == 9 && decoded.fields[8].length == 8 &&
                     decoded.fields[21].id == 22 && decoded.fields[21].length == 8,
                     "ABI fingerprint must include only numeric offsetof metadata after sizes");
    cdto_v1_free_decoded(&decoded);

    printf("cdto_v1_test rust_fixture_hex=%s\n", fixture_hex);
    cdto_v1_free_wire(abi_wire);
    cdto_v1_free_wire(player_snapshot_wire);
    cdto_v1_free_wire(wire);
    return failed ? 1 : 0;
}
