#ifndef M3_WAKE_V1_H
#define M3_WAKE_V1_H

#include <stddef.h>

/* M3 wake v1 is a fixed, payload-free, big-endian optimization signal. */
#define M3_WAKE_V1_FRAME_LENGTH 16

typedef enum m3_wake_v1_result {
    M3_WAKE_V1_OK = 0,
    M3_WAKE_V1_INVALID = 1
} m3_wake_v1_result;

/* Writes exactly the canonical 16-byte v1 WAKE frame. */
m3_wake_v1_result m3_wake_v1_encode(unsigned char *output,
    size_t output_length);

/* Accepts only the exact canonical 16-byte v1 WAKE frame. */
m3_wake_v1_result m3_wake_v1_decode(const unsigned char *input,
    size_t input_length);

#endif
