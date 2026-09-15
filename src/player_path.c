#include "mstruct.h"
#include "mextern.h"
#include "player_path.h"
#include "resource_path.h"
#include "utf8_text.h"

#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

static unsigned int rol32(unsigned int v, int n)
{
    return (v << n) | (v >> (32 - n));
}

static unsigned int load_u32_be(const unsigned char *p)
{
    return ((unsigned int)p[0] << 24) |
           ((unsigned int)p[1] << 16) |
           ((unsigned int)p[2] << 8) |
           (unsigned int)p[3];
}

static void store_u32_be(unsigned int v, unsigned char *p)
{
    p[0] = (unsigned char)((v >> 24) & 0xff);
    p[1] = (unsigned char)((v >> 16) & 0xff);
    p[2] = (unsigned char)((v >> 8) & 0xff);
    p[3] = (unsigned char)(v & 0xff);
}

static void sha1_digest(const unsigned char *data, unsigned long len, unsigned char out[20])
{
    unsigned int h0, h1, h2, h3, h4;
    unsigned char block[128];
    unsigned long total, off;
    unsigned int w[80];
    int i;

    h0 = 0x67452301U;
    h1 = 0xEFCDAB89U;
    h2 = 0x98BADCFEU;
    h3 = 0x10325476U;
    h4 = 0xC3D2E1F0U;

    total = (len + 9 <= 64) ? 64 : 128;
    memset(block, 0, sizeof(block));
    if(len > 0)
        memcpy(block, data, len);
    block[len] = 0x80;

    {
        unsigned int bit_hi;
        unsigned int bit_lo;
        unsigned long idx;
        idx = total - 8;
        bit_hi = (unsigned int)(len >> 29);
        bit_lo = (unsigned int)(len << 3);
        store_u32_be(bit_hi, &block[idx]);
        store_u32_be(bit_lo, &block[idx + 4]);
    }

    for(off = 0; off < total; off += 64) {
        unsigned int a, b, c, d, e, f, k, t, temp;

        for(i = 0; i < 16; i++)
            w[i] = load_u32_be(&block[off + (i * 4)]);
        for(i = 16; i < 80; i++)
            w[i] = rol32(w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16], 1);

        a = h0;
        b = h1;
        c = h2;
        d = h3;
        e = h4;

        for(i = 0; i < 80; i++) {
            if(i < 20) {
                f = (b & c) | ((~b) & d);
                k = 0x5A827999U;
            } else if(i < 40) {
                f = b ^ c ^ d;
                k = 0x6ED9EBA1U;
            } else if(i < 60) {
                f = (b & c) | (b & d) | (c & d);
                k = 0x8F1BBCDCU;
            } else {
                f = b ^ c ^ d;
                k = 0xCA62C1D6U;
            }

            t = (rol32(a, 5) + f + e + k + w[i]) & 0xffffffffU;
            e = d;
            d = c;
            c = rol32(b, 30);
            b = a;
            a = t;
        }

        h0 = (h0 + a) & 0xffffffffU;
        h1 = (h1 + b) & 0xffffffffU;
        h2 = (h2 + c) & 0xffffffffU;
        h3 = (h3 + d) & 0xffffffffU;
        h4 = (h4 + e) & 0xffffffffU;
    }

    store_u32_be(h0, &out[0]);
    store_u32_be(h1, &out[4]);
    store_u32_be(h2, &out[8]);
    store_u32_be(h3, &out[12]);
    store_u32_be(h4, &out[16]);
}

int player_path_shard_from_name(const char *name, char out[3])
{
    static const char hex[] = "0123456789abcdef";
    unsigned char digest[20];
    unsigned char b;

    if(!name || !out) return -1;
    sha1_digest((const unsigned char *)name, (unsigned long)strlen(name), digest);
    b = digest[0];
    out[0] = hex[(b >> 4) & 0x0f];
    out[1] = hex[b & 0x0f];
    out[2] = 0;
    return 0;
}

int player_path_from_name(const char *name, char *out, unsigned long out_sz)
{
    char shard[3];
    char legacy[512];

    if(!name || !name[0] || !out || out_sz == 0)
        return -1;

    if(player_path_shard_from_name(name, shard) != 0) return -1;
    if(snprintf(legacy, sizeof(legacy), "%s/%s/%s", PLAYERPATH, shard, name) >=
       (int)sizeof(legacy))
        return -1;
    return resolve_runtime_path(legacy, out, out_sz);
}

int player_path_ensure_dir(const char *name)
{
    char shard[3], legacy[512], dir[512];
    struct stat st;
    int fd;

    if(!name || !name[0])
        return -1;

    if(player_path_shard_from_name(name, shard) != 0) return -1;
    if(snprintf(legacy, sizeof(legacy), "%s/%s", PLAYERPATH, shard) >=
       (int)sizeof(legacy))
        return -1;
    if(resolve_runtime_path(legacy, dir, sizeof(dir)) < 0)
        return -1;
    if(lstat(dir, &st) < 0) {
        if(errno != ENOENT || mkdir(dir, 0700) < 0)
            return -1;
    }

    /* A shard is a security boundary.  Verify the opened object rather than
     * following a path that may have changed after lstat(). */
#ifndef O_NOFOLLOW
    errno = ENOTSUP;
    return -1;
#else
    fd = open(dir, O_RDONLY | O_NOFOLLOW | O_BINARY, 0);
    if(fd < 0)
        return -1;
    if(fstat(fd, &st) < 0 || !S_ISDIR(st.st_mode) || fchmod(fd, 0700) < 0) {
        close(fd);
        return -1;
    }
    if(close(fd) < 0)
        return -1;
    return 0;
#endif
}

int player_path_open_readonly(const char *name)
{
#if !defined(O_NOFOLLOW) || !defined(O_DIRECTORY)
    (void)name;
    errno = ENOTSUP;
    return -1;
#else
    char root[512], shard[3];
    int root_fd, player_fd, shard_fd, file_fd;
    int saved_errno;
    struct stat st;

    /* MUHAN_HOME itself is the configured trust root.  Every component below
     * it is opened by descriptor with no symlink traversal. */
    if(!name || !player_name_is_valid((const unsigned char *)name,
                                     PLAYER_NAME_MIN_CODEPOINTS,
                                     PLAYER_NAME_MAX_CODEPOINTS) ||
       resolve_runtime_path(MUDHOME, root, sizeof(root)) < 0) {
        errno = EINVAL;
        return -1;
    }
    root_fd = player_fd = shard_fd = file_fd = -1;
    root_fd = open(root, O_RDONLY | O_DIRECTORY | O_BINARY, 0);
    if(root_fd < 0) goto fail;
    if(fstat(root_fd, &st) < 0) goto fail;
    if(!S_ISDIR(st.st_mode)) { errno = ENOTDIR; goto fail; }
    player_fd = openat(root_fd, "player",
                       O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_BINARY, 0);
    if(player_fd < 0) goto fail;
    if(fstat(player_fd, &st) < 0) goto fail;
    if(!S_ISDIR(st.st_mode)) { errno = ENOTDIR; goto fail; }
    if(player_path_shard_from_name(name, shard) != 0) goto fail;
    shard_fd = openat(player_fd, shard,
                      O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_BINARY, 0);
    if(shard_fd < 0) goto fail;
    if(fstat(shard_fd, &st) < 0) goto fail;
    if(!S_ISDIR(st.st_mode)) { errno = ENOTDIR; goto fail; }
    file_fd = openat(shard_fd, name,
                     O_RDONLY | O_NONBLOCK | O_NOFOLLOW | O_BINARY, 0);
    if(file_fd < 0) goto fail;
    if(fstat(file_fd, &st) < 0) goto fail;
    if(!S_ISREG(st.st_mode)) { errno = EINVAL; goto fail; }
    if(st.st_size < 0) { errno = EIO; goto fail; }
    if(st.st_size > (off_t)PLAYER_PATH_READ_MAX_BYTES) { errno = EFBIG; goto fail; }
    if(close(shard_fd) < 0) { shard_fd = -1; goto fail; }
    shard_fd = -1;
    if(close(player_fd) < 0) { player_fd = -1; goto fail; }
    player_fd = -1;
    if(close(root_fd) < 0) { root_fd = -1; goto fail; }
    return file_fd;
fail:
    saved_errno = errno;
    if(file_fd >= 0) close(file_fd);
    if(shard_fd >= 0) close(shard_fd);
    if(player_fd >= 0) close(player_fd);
    if(root_fd >= 0) close(root_fd);
    errno = saved_errno;
    return -1;
#endif
}

int player_name_is_valid(const unsigned char *name, unsigned long min_cp, unsigned long max_cp)
{
    unsigned long i, n, cp_len;

    if(!name || !name[0])
        return 0;

    n = (unsigned long)strlen((const char *)name);
    if(!utf8_validate(name, n))
        return 0;
    if(n > PLAYER_NAME_MAX_BYTES)
        return 0;

    cp_len = utf8_codepoint_len(name);
    if(cp_len < min_cp || cp_len > max_cp)
        return 0;

    if(strcmp((const char *)name, ".") == 0 || strcmp((const char *)name, "..") == 0)
        return 0;

    for(i = 0; i < n; i++) {
        if(name[i] < 32 || name[i] == 127)
            return 0;
        if(name[i] == '/' || name[i] == '\\' || name[i] == ':')
            return 0;
    }

    return 1;
}
