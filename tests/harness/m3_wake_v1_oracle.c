#include "m3_wake_v1.h"

#include <stdio.h>
#include <string.h>

static int hex_value(char character)
{
    if(character >= '0' && character <= '9') return character - '0';
    if(character >= 'a' && character <= 'f') return character - 'a' + 10;
    if(character >= 'A' && character <= 'F') return character - 'A' + 10;
    return -1;
}

static int decode_hex(const char *text, unsigned char *output,
    size_t *length_out)
{
    size_t length, i;
    int high, low;

    if(!text || !output || !length_out) return -1;
    length = strlen(text);
    if(length % 2 || length / 2 > M3_WAKE_V1_FRAME_LENGTH + 1) return -1;
    for(i = 0; i < length / 2; i++) {
        high = hex_value(text[i * 2]);
        low = hex_value(text[i * 2 + 1]);
        if(high < 0 || low < 0) return -1;
        output[i] = (unsigned char)((high << 4) | low);
    }
    *length_out = length / 2;
    return 0;
}

static void print_hex(const unsigned char *bytes, size_t length)
{
    static const char hex[] = "0123456789abcdef";
    size_t i;
    for(i = 0; i < length; i++) {
        putchar(hex[bytes[i] >> 4]);
        putchar(hex[bytes[i] & 15]);
    }
    putchar('\n');
}

int main(int argc, char **argv)
{
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH + 1];
    size_t frame_length;

    if(argc == 2 && strcmp(argv[1], "encode") == 0) {
        if(m3_wake_v1_encode(frame, M3_WAKE_V1_FRAME_LENGTH) != M3_WAKE_V1_OK)
            return 1;
        print_hex(frame, M3_WAKE_V1_FRAME_LENGTH);
        return 0;
    }
    if(argc == 3 && strcmp(argv[1], "decode") == 0) {
        if(decode_hex(argv[2], frame, &frame_length) == 0 &&
           m3_wake_v1_decode(frame, frame_length) == M3_WAKE_V1_OK)
            puts("accept");
        else
            puts("reject");
        return 0;
    }
    fprintf(stderr, "usage: %s encode | %s decode HEX\n", argv[0], argv[0]);
    return 2;
}
