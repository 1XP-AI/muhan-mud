#include "character_save_journal_v2_uuid_native.h"

#if !defined(__linux__)
#error "character_save_journal_v2_uuid_native requires Linux getrandom"
#endif

#include <errno.h>
#include <sys/random.h>
#include <sys/types.h>

static int uuid_native_entropy(void *opaque, unsigned char *buffer,
                               size_t capacity, size_t *filled)
{
    ssize_t n;
    (void)opaque;
    if (!buffer || !filled || capacity == 0) return -1;
    do n = getrandom(buffer, capacity, 0); while (n < 0 && errno == EINTR);
    if (n <= 0) return -1;
    *filled = (size_t)n;
    return 0;
}

character_save_journal_v2_uuid_result character_save_journal_v2_uuid_generate_native(
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1])
{
    return character_save_journal_v2_uuid_generate(uuid_native_entropy, 0, output);
}
