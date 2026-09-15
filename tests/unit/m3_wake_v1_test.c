#include "m3_wake_v1.h"

#include <stdio.h>
#include <string.h>

static const unsigned char canonical[M3_WAKE_V1_FRAME_LENGTH] = {
    0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00,
    0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00
};

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

int main(void)
{
    unsigned char frame[M3_WAKE_V1_FRAME_LENGTH];
    unsigned char mutated[M3_WAKE_V1_FRAME_LENGTH];
    unsigned char long_frame[M3_WAKE_V1_FRAME_LENGTH + 1];
    int failed = 0;

    failed |= expect(m3_wake_v1_encode(frame, sizeof(frame)) == M3_WAKE_V1_OK,
        "encoder accepts the exact 16-byte output buffer");
    failed |= expect(memcmp(frame, canonical, sizeof(canonical)) == 0,
        "encoder writes the canonical big-endian v1 wake frame");
    failed |= expect(m3_wake_v1_decode(frame, sizeof(frame)) == M3_WAKE_V1_OK,
        "decoder accepts the canonical frame");

    failed |= expect(m3_wake_v1_encode(0, sizeof(frame)) == M3_WAKE_V1_INVALID,
        "encoder rejects a null output buffer");
    failed |= expect(m3_wake_v1_encode(frame, sizeof(frame) - 1) == M3_WAKE_V1_INVALID,
        "encoder rejects a short output buffer");
    failed |= expect(m3_wake_v1_decode(0, sizeof(frame)) == M3_WAKE_V1_INVALID,
        "decoder rejects a null input buffer");
    failed |= expect(m3_wake_v1_decode(frame, sizeof(frame) - 1) == M3_WAKE_V1_INVALID,
        "decoder rejects a short frame");
    memcpy(long_frame, frame, sizeof(frame));
    long_frame[sizeof(frame)] = 0;
    failed |= expect(m3_wake_v1_decode(long_frame, sizeof(long_frame)) == M3_WAKE_V1_INVALID,
        "decoder rejects a long frame");

    memcpy(mutated, canonical, sizeof(mutated));
    mutated[0] ^= 1;
    failed |= expect(m3_wake_v1_decode(mutated, sizeof(mutated)) == M3_WAKE_V1_INVALID,
        "decoder rejects a different magic");
    memcpy(mutated, canonical, sizeof(mutated));
    mutated[9] = 2;
    failed |= expect(m3_wake_v1_decode(mutated, sizeof(mutated)) == M3_WAKE_V1_INVALID,
        "decoder rejects a different version");
    memcpy(mutated, canonical, sizeof(mutated));
    mutated[11] = 2;
    failed |= expect(m3_wake_v1_decode(mutated, sizeof(mutated)) == M3_WAKE_V1_INVALID,
        "decoder rejects a different kind");
    memcpy(mutated, canonical, sizeof(mutated));
    mutated[15] = 1;
    failed |= expect(m3_wake_v1_decode(mutated, sizeof(mutated)) == M3_WAKE_V1_INVALID,
        "decoder rejects a nonzero payload length");
    return failed;
}
