/* Test-only legacy alias/title characterization oracle.
 *
 * The real alias.c reader remains the authority for its persisted line order:
 * alias/process pairs end at "~!", followed by one optional title line and a
 * final "~!".  This oracle intentionally consumes only complete lines that
 * fit the old fgets buffers, rejects ambiguous/unsafe input before copying,
 * and emits the portable AliasTitleSnapshotV1 CDTO boundary.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#define RAW_FILE_LIMIT (32U * 1024U)

static int read_all(path, output, output_length)
const char *path;
unsigned char **output;
size_t *output_length;
{
    FILE *file;
    unsigned char *bytes;
    size_t length = 0U;
    int next;

    if (!path || !output || !output_length) return -1;
    *output = NULL; *output_length = 0U;
    file = fopen(path, "rb");
    if (!file) return -1;
    bytes = (unsigned char *)malloc(RAW_FILE_LIMIT);
    if (!bytes) { fclose(file); return -1; }
    while ((next = fgetc(file)) != EOF) {
        if (length == RAW_FILE_LIMIT || next == 0) {
            free(bytes); fclose(file); return -1;
        }
        bytes[length++] = (unsigned char)next;
    }
    if (ferror(file)) { free(bytes); fclose(file); return -1; }
    fclose(file);
    *output = bytes;
    *output_length = length;
    return 0;
}

/* Only complete LF-terminated lines are admitted. This prevents the legacy
 * reader's partial-line truncation from becoming a canonical value. */
static int line(bytes, length, at, value, value_length)
const unsigned char *bytes;
size_t length;
size_t *at;
const unsigned char **value;
size_t *value_length;
{
    size_t start;
    if (!bytes || !at || !value || !value_length || *at >= length) return -1;
    start = *at;
    while (*at < length && bytes[*at] != '\n') ++*at;
    if (*at == length) return -1;
    *value = bytes + start;
    *value_length = *at - start;
    ++*at;
    return 0;
}

static int marker(value, length)
const unsigned char *value;
size_t length;
{ return length == 2U && value[0] == '~' && value[1] == '!'; }

static int duplicate(value, alias, alias_length)
const alias_title_snapshot_v1 *value;
const unsigned char *alias;
size_t alias_length;
{
    size_t i;
    for (i = 0U; i < value->alias_count; ++i) {
        if (value->aliases[i].alias_length == alias_length &&
            !memcmp(value->aliases[i].alias, alias, alias_length)) return 1;
    }
    return 0;
}

static int parse_legacy_alias_title(bytes, length, output)
const unsigned char *bytes;
size_t length;
alias_title_snapshot_v1 *output;
{
    const unsigned char *alias, *process, *title;
    size_t at = 0U, alias_length, process_length, title_length;
    int titles = 0;

    if (!bytes || !output) return -1;
    memset(output, 0, sizeof(*output));
    while (!titles) {
        if (line(bytes, length, &at, &alias, &alias_length)) return -1;
        if (marker(alias, alias_length)) { titles = 1; break; }
        if (alias_length == 0U ||
            alias_length > ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES ||
            output->alias_count == ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES ||
            duplicate(output, alias, alias_length)) return -1;
        if (line(bytes, length, &at, &process, &process_length)) return -1;
        /* alias.c treats a process-side marker as the title transition. */
        if (marker(process, process_length)) { titles = 1; break; }
        if (process_length > ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES) return -1;
        output->aliases[output->alias_count].alias_length = (uint8_t)alias_length;
        memcpy(output->aliases[output->alias_count].alias, alias, alias_length);
        output->aliases[output->alias_count].process_length = (uint16_t)process_length;
        memcpy(output->aliases[output->alias_count].process, process, process_length);
        ++output->alias_count;
    }
    if (line(bytes, length, &at, &title, &title_length)) return -1;
    if (!marker(title, title_length)) {
        if (title_length > ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES) return -1;
        output->title_present = 1U;
        output->title_length = (uint8_t)title_length;
        memcpy(output->title, title, title_length);
        if (line(bytes, length, &at, &title, &title_length) || !marker(title, title_length))
            return -1;
    }
    return at == length ? 0 : -1;
}

static void hex(bytes, length)
const unsigned char *bytes;
size_t length;
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    for (i = 0U; i < length; ++i) {
        putchar(digits[bytes[i] >> 4]);
        putchar(digits[bytes[i] & 15U]);
    }
    putchar('\n');
}

static int classify(path)
const char *path;
{
    unsigned char *raw = NULL, *wire = NULL, *again = NULL;
    size_t raw_length = 0U, wire_length = 0U, again_length = 0U;
    alias_title_snapshot_v1 decoded, reread;
    int status;

    if (read_all(path, &raw, &raw_length) ||
        parse_legacy_alias_title(raw, raw_length, &decoded)) {
        free(raw); puts("reject"); return 0;
    }
    status = alias_title_snapshot_v1_encode(&decoded, &wire, &wire_length);
    if (status == CDTO_V1_OK)
        status = alias_title_snapshot_v1_decode(wire, wire_length, &reread);
    if (status == CDTO_V1_OK)
        status = alias_title_snapshot_v1_encode(&reread, &again, &again_length);
    if (status != CDTO_V1_OK || again_length != wire_length ||
        memcmp(again, wire, wire_length)) {
        free(raw); cdto_v1_free_wire(wire); cdto_v1_free_wire(again); return 1;
    }
    fputs("accept ", stdout); hex(wire, wire_length);
    free(raw); cdto_v1_free_wire(wire); cdto_v1_free_wire(again);
    return 0;
}

int main(argc, argv)
int argc;
char **argv;
{
    if (argc != 3 || strcmp(argv[1], "classify")) {
        fprintf(stderr, "usage: %s classify fixture\n", argv[0]);
        return 2;
    }
    return classify(argv[2]);
}
