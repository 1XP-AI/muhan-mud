#include "m3_wake_v1.h"

#include <string.h>

static const unsigned char m3_wake_v1_canonical[M3_WAKE_V1_FRAME_LENGTH] = {
    0x4d, 0x55, 0x48, 0x4d, 0x33, 0x57, 0x4b, 0x00,
    0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00
};

m3_wake_v1_result m3_wake_v1_encode(unsigned char *output,
    size_t output_length)
{
    if(!output || output_length != M3_WAKE_V1_FRAME_LENGTH)
        return M3_WAKE_V1_INVALID;
    memcpy(output, m3_wake_v1_canonical, M3_WAKE_V1_FRAME_LENGTH);
    return M3_WAKE_V1_OK;
}

m3_wake_v1_result m3_wake_v1_decode(const unsigned char *input,
    size_t input_length)
{
    if(!input || input_length != M3_WAKE_V1_FRAME_LENGTH)
        return M3_WAKE_V1_INVALID;
    if(memcmp(input, m3_wake_v1_canonical, M3_WAKE_V1_FRAME_LENGTH) != 0)
        return M3_WAKE_V1_INVALID;
    return M3_WAKE_V1_OK;
}
