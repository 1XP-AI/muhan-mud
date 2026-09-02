#include "character_save_journal_v2_rpc_transport.h"

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define RPC_INITIALIZED 0x4d335250U
#define RPC_TEXT_MAX 63

static const unsigned int rpc_types[] = {
    25, 25, 25, 25, 25, 25, 25, 25, 25, 25, 25, 25
};
static const int rpc_formats[] = {
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
};

static size_t rpc_length(const char *text, size_t maximum)
{
    size_t length = 0;

    if (!text)
        return maximum + 1;
    while (length <= maximum) {
        if (!text[length])
            return length;
        length++;
    }
    return maximum + 1;
}

static int rpc_text(const char *text, size_t maximum)
{
    size_t i;
    size_t length = rpc_length(text, maximum);

    if (!length || length > maximum)
        return 0;
    for (i = 0; i < length; i++) {
        if ((unsigned char)text[i] < 0x20 || (unsigned char)text[i] == 0x7f)
            return 0;
    }
    return 1;
}

static int rpc_lower_hex(const char *text, size_t length)
{
    size_t i;

    if (rpc_length(text, length) != length)
        return 0;
    for (i = 0; i < length; i++) {
        if (!((text[i] >= '0' && text[i] <= '9') ||
              (text[i] >= 'a' && text[i] <= 'f')))
            return 0;
    }
    return 1;
}

static int rpc_uuid(const char *text)
{
    size_t i;

    if (rpc_length(text, 36) != 36)
        return 0;
    for (i = 0; i < 36; i++) {
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            if (text[i] != '-')
                return 0;
        } else if (!((text[i] >= '0' && text[i] <= '9') ||
                     (text[i] >= 'a' && text[i] <= 'f'))) {
            return 0;
        }
    }
    return 1;
}

static int rpc_world(const char *text)
{
    size_t i;
    size_t length = rpc_length(text, 64);

    if (!length || length > 64 || text[0] < 'a' || text[0] > 'z')
        return 0;
    for (i = 1; i < length; i++) {
        if (!((text[i] >= 'a' && text[i] <= 'z') ||
              (text[i] >= '0' && text[i] <= '9') || text[i] == '_' ||
              text[i] == '-'))
            return 0;
    }
    return 1;
}

static int rpc_utf8(const unsigned char *bytes, size_t length, size_t *points)
{
    size_t i = 0;
    size_t n = 0;
    unsigned char first;

    while (i < length) {
        first = bytes[i++];
        if (first < 0x80) {
            n++;
            continue;
        }
        if (first >= 0xc2 && first <= 0xdf) {
            if (i >= length || bytes[i] < 0x80 || bytes[i] > 0xbf)
                return 0;
            i++;
        } else if (first == 0xe0) {
            if (i + 1 >= length || bytes[i] < 0xa0 || bytes[i] > 0xbf ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf)
                return 0;
            i += 2;
        } else if (first >= 0xe1 && first <= 0xec) {
            if (i + 1 >= length || bytes[i] < 0x80 || bytes[i] > 0xbf ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf)
                return 0;
            i += 2;
        } else if (first == 0xed) {
            if (i + 1 >= length || bytes[i] < 0x80 || bytes[i] > 0x9f ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf)
                return 0;
            i += 2;
        } else if (first >= 0xee && first <= 0xef) {
            if (i + 1 >= length || bytes[i] < 0x80 || bytes[i] > 0xbf ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf)
                return 0;
            i += 2;
        } else if (first == 0xf0) {
            if (i + 2 >= length || bytes[i] < 0x90 || bytes[i] > 0xbf ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf ||
                bytes[i + 2] < 0x80 || bytes[i + 2] > 0xbf)
                return 0;
            i += 3;
        } else if (first >= 0xf1 && first <= 0xf3) {
            if (i + 2 >= length || bytes[i] < 0x80 || bytes[i] > 0xbf ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf ||
                bytes[i + 2] < 0x80 || bytes[i + 2] > 0xbf)
                return 0;
            i += 3;
        } else if (first == 0xf4) {
            if (i + 2 >= length || bytes[i] < 0x80 || bytes[i] > 0x8f ||
                bytes[i + 1] < 0x80 || bytes[i + 1] > 0xbf ||
                bytes[i + 2] < 0x80 || bytes[i + 2] > 0xbf)
                return 0;
            i += 3;
        } else {
            return 0;
        }
        n++;
    }
    if (points)
        *points = n;
    return 1;
}

static int rpc_name(const unsigned char *name, size_t length)
{
    size_t i;
    size_t points;
    int spaces = 1;

    if (!name || !length || length > 14)
        return 0;
    for (i = 0; i < length; i++) {
        if (!name[i] || name[i] < 0x20 || name[i] == 0x7f ||
            name[i] == '/' || name[i] == '\\' || name[i] == ':')
            return 0;
        if (name[i] != ' ')
            spaces = 0;
        if ((i == 0 && name[i] >= 'a' && name[i] <= 'z') ||
            (i > 0 && name[i] >= 'A' && name[i] <= 'Z'))
            return 0;
    }
    return !spaces && !(length == 1 && name[0] == '.') &&
        !(length == 2 && name[0] == '.' && name[1] == '.') &&
        rpc_utf8(name, length, &points) && points && points <= 12;
}

static int rpc_name_text(const char *name)
{
    size_t length = rpc_length(name, 14);

    return length <= 14 && rpc_name((const unsigned char *)name, length);
}

static int rpc_equal(const char *text, const char *expected, size_t length)
{
    return rpc_length(text, length) == length && !memcmp(text, expected, length);
}

static int rpc_operations_complete(
    const character_save_journal_v2_rpc_transport_operations *o)
{
    return o && o->connection_ok && o->transaction_status && o->exec_params &&
        o->result_status && o->result_rows && o->result_columns &&
        o->result_value && o->result_value_length && o->result_sqlstate &&
        o->result_clear && o->connection_finish;
}

static void rpc_finish(
    const character_save_journal_v2_rpc_transport_operations *o,
    void *connection)
{
    if (connection && o && o->connection_finish)
        o->connection_finish(connection);
}

static void rpc_unavailable(character_save_journal_v2_rpc_transport *t)
{
    if (!t || t->initialized != RPC_INITIALIZED)
        return;
    rpc_finish(t->operations, t->connection);
    t->connection = 0;
    if (t->state != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED)
        t->state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE;
}

void character_save_journal_v2_rpc_transport_init(
    character_save_journal_v2_rpc_transport *t)
{
    if (!t)
        return;
    memset(t, 0, sizeof(*t));
    t->initialized = RPC_INITIALIZED;
    t->state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NEW;
}

void character_save_journal_v2_rpc_transport_close(
    character_save_journal_v2_rpc_transport *t)
{
    if (!t || t->initialized != RPC_INITIALIZED)
        return;
    rpc_finish(t->operations, t->connection);
    t->connection = 0;
    t->state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED;
}

character_save_journal_v2_rpc_transport_state
character_save_journal_v2_rpc_transport_get_state(
    const character_save_journal_v2_rpc_transport *t)
{
    return t && t->initialized == RPC_INITIALIZED ? t->state :
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE;
}

static int rpc_tuples(const character_save_journal_v2_rpc_transport *t,
    void *r, int rows, int columns)
{
    const char *state = t->operations->result_sqlstate(r);

    return t->operations->result_status(r) ==
               CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_TUPLES_OK &&
        (!state || !state[0]) && t->operations->result_rows(r) == rows &&
        t->operations->result_columns(r) == columns;
}

static character_save_journal_v2_rpc_transport_outcome rpc_error(
    character_save_journal_v2_rpc_transport *t, void *r)
{
    char state[6];
    const char *value = t->operations->result_sqlstate(r);

    memset(state, 0, sizeof(state));
    if (value && rpc_length(value, 5) == 5)
        memcpy(state, value, 5);
    t->operations->result_clear(r);
    if (!t->operations->connection_ok(t->connection) || !memcmp(state, "08", 2)) {
        rpc_unavailable(t);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    }
    if (!memcmp(state, "22", 2))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    if (!memcmp(state, "P0001", 5))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED;
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
}

static int rpc_assert(character_save_journal_v2_rpc_transport *t)
{
    void *r;

    if (!t || t->initialized != RPC_INITIALIZED ||
        t->state != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY ||
        !t->connection || !t->operations ||
        !t->operations->connection_ok(t->connection) ||
        !t->operations->transaction_status(t->connection)) {
        rpc_unavailable(t);
        return 0;
    }
    r = t->operations->exec_params(t->connection,
        "select private.m3_assert_writer_session()", 0, 0, 0, 0, 0, 0);
    if (!r) {
        /* A missing assertion result proves neither identity nor session
         * integrity, even if libpq has not yet observed a broken socket. */
        rpc_unavailable(t);
        return 0;
    }
    if (!rpc_tuples(t, r, 1, 1) ||
        t->operations->result_value_length(r, 0, 0) != 1 ||
        !t->operations->result_value(r, 0, 0) ||
        t->operations->result_value(r, 0, 0)[0] != 't') {
        t->operations->result_clear(r);
        rpc_unavailable(t);
        return 0;
    }
    t->operations->result_clear(r);
    return 1;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_start(
    character_save_journal_v2_rpc_transport *t,
    const character_save_journal_v2_rpc_transport_operations *o,
    void *opaque, void *connection)
{
    if (!t || t->initialized != RPC_INITIALIZED ||
        t->state == CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY) {
        /* Re-presenting the owned pointer is not a new ownership transfer.
         * Finishing it would leave READY holding a dangling connection. */
        if (!t || t->initialized != RPC_INITIALIZED ||
            connection != t->connection)
            rpc_finish(o, connection);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    }
    rpc_unavailable(t);
    t->operations = o;
    t->operations_opaque = opaque;
    t->connection = connection;
    t->state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NEW;
    if (!connection || !rpc_operations_complete(o) ||
        !o->connection_ok(connection) || !o->transaction_status(connection)) {
        rpc_unavailable(t);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    }
    t->state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
    return rpc_assert(t) ? CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK :
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
}

static character_save_journal_v2_rpc_transport_outcome rpc_execute(
    character_save_journal_v2_rpc_transport *t, const char *sql, int count,
    const unsigned int *types, const char *const *values, void **out)
{
    void *r;

    if (out)
        *out = 0;
    if (!rpc_assert(t))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    r = t->operations->exec_params(t->connection, sql, count, types, values,
        0, rpc_formats, 0);
    if (!r) {
        if (!t->operations->connection_ok(t->connection)) {
            rpc_unavailable(t);
            return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
        }
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    if (t->operations->result_status(r) !=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_TUPLES_OK)
        return rpc_error(t, r);
    if (out)
        *out = r;
    else
        t->operations->result_clear(r);
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
}

static int rpc_copy(const character_save_journal_v2_rpc_transport *t, void *r,
    int column, char *destination, size_t capacity)
{
    const char *value = t->operations->result_value(r, 0, column);
    int length = t->operations->result_value_length(r, 0, column);

    if (!value || length < 0 || (size_t)length >= capacity ||
        memchr(value, 0, (size_t)length))
        return 0;
    memcpy(destination, value, (size_t)length);
    destination[length] = 0;
    return 1;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_lookup_route(
    character_save_journal_v2_rpc_transport *t, const char *world,
    const char *name, character_save_journal_v2_rpc_route *route)
{
    static const char sql[] =
        "select world_id::text,character_id::text,legacy_name_key::text,"
        "legacy_shard::text,storage_format::text,lifecycle::text,"
        "coalesce(imported_file_sha256,'')::text from "
        "private.resolve_game_character_writer_route_v2($1::text,$2::text)";
    const char *values[2];
    void *r;
    char storage[12];
    char *end;
    unsigned long n;
    character_save_journal_v2_rpc_transport_outcome answer;

    if (route)
        memset(route, 0, sizeof(*route));
    if (!route || !rpc_world(world) || !rpc_name_text(name))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    values[0] = world;
    values[1] = name;
    answer = rpc_execute(t, sql, 2, rpc_types, values, &r);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return answer;
    if (!rpc_tuples(t, r, 1, 7) ||
        !rpc_copy(t, r, 0, route->world_id, sizeof(route->world_id)) ||
        !rpc_copy(t, r, 1, route->character_id, sizeof(route->character_id)) ||
        !rpc_copy(t, r, 2, route->legacy_name_key,
            sizeof(route->legacy_name_key)) ||
        !rpc_copy(t, r, 3, route->legacy_shard,
            sizeof(route->legacy_shard)) ||
        !rpc_copy(t, r, 4, storage, sizeof(storage)) ||
        !rpc_copy(t, r, 5, route->lifecycle, sizeof(route->lifecycle)) ||
        !rpc_copy(t, r, 6, route->imported_file_sha256,
            sizeof(route->imported_file_sha256)) ||
        strcmp(route->world_id, world) || strcmp(route->legacy_name_key, name) ||
        !rpc_world(route->world_id) || !rpc_uuid(route->character_id) ||
        !rpc_name_text(route->legacy_name_key) ||
        !rpc_lower_hex(route->legacy_shard, 2) ||
        !rpc_text(route->lifecycle, 31) ||
        (strcmp(route->lifecycle, "imported_unclaimed") &&
            strcmp(route->lifecycle, "provisioning") &&
            strcmp(route->lifecycle, "active")) ||
        (route->imported_file_sha256[0] &&
            !rpc_lower_hex(route->imported_file_sha256, 64))) {
        t->operations->result_clear(r);
        memset(route, 0, sizeof(*route));
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    errno = 0;
    n = strtoul(storage, &end, 10);
    if (errno == ERANGE || end == storage || *end || n != 1) {
        t->operations->result_clear(r);
        memset(route, 0, sizeof(*route));
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    route->storage_format = (unsigned int)n;
    t->operations->result_clear(r);
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
}

static character_save_journal_v2_rpc_transport_outcome rpc_epoch_call(
    character_save_journal_v2_rpc_transport *t, const char *sql, int count,
    const char *const *values, unsigned long long *epoch, char expires[64])
{
    void *r;
    char number[32];
    char *end;
    unsigned long long value;
    character_save_journal_v2_rpc_transport_outcome answer =
        rpc_execute(t, sql, count, rpc_types, values, &r);

    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return answer;
    if (!rpc_tuples(t, r, 1, 2) || !rpc_copy(t, r, 0, number, sizeof(number)) ||
        !rpc_copy(t, r, 1, expires, 64) || !rpc_text(expires, RPC_TEXT_MAX)) {
        t->operations->result_clear(r);
        *epoch = 0;
        expires[0] = 0;
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    errno = 0;
    value = strtoull(number, &end, 10);
    if (errno == ERANGE || end == number || *end || !value ||
        value > (unsigned long long)LLONG_MAX) {
        t->operations->result_clear(r);
        *epoch = 0;
        expires[0] = 0;
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    *epoch = value;
    t->operations->result_clear(r);
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_acquire(
    character_save_journal_v2_rpc_transport *t, const char *world,
    const char *writer, const char *expires, unsigned long long *epoch,
    char out[64])
{
    static const char sql[] =
        "select writer_epoch::text,expires_at::text from "
        "private.acquire_game_world_writer_epoch($1::text,$2::uuid,"
        "$3::timestamptz)";
    const char *values[3];

    if (epoch)
        *epoch = 0;
    if (out)
        out[0] = 0;
    if (!epoch || !out || !rpc_world(world) || !rpc_uuid(writer) ||
        !rpc_text(expires, RPC_TEXT_MAX))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    values[0] = world;
    values[1] = writer;
    values[2] = expires;
    return rpc_epoch_call(t, sql, 3, values, epoch, out);
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_renew(
    character_save_journal_v2_rpc_transport *t, const char *world,
    const char *writer, unsigned long long epoch, const char *expires,
    char out[64])
{
    static const char sql[] =
        "select writer_epoch::text,expires_at::text from "
        "private.renew_game_world_writer_epoch($1::text,$2::uuid,"
        "$3::bigint,$4::timestamptz)";
    char number[32];
    const char *values[4];
    unsigned long long returned = 0;

    if (out)
        out[0] = 0;
    if (!out || !rpc_world(world) || !rpc_uuid(writer) ||
        !rpc_text(expires, RPC_TEXT_MAX) || !epoch ||
        epoch > (unsigned long long)LLONG_MAX)
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    snprintf(number, sizeof(number), "%llu", epoch);
    values[0] = world;
    values[1] = writer;
    values[2] = number;
    values[3] = expires;
    {
        character_save_journal_v2_rpc_transport_outcome answer =
            rpc_epoch_call(t, sql, 4, values, &returned, out);

        if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
            return answer;
        if (returned != epoch) {
            out[0] = 0;
            return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
        }
        return answer;
    }
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_seal(
    character_save_journal_v2_rpc_transport *t, const char *world,
    const char *writer, unsigned long long epoch)
{
    static const char sql[] =
        "select private.seal_game_world_writer_epoch($1::text,$2::uuid,"
        "$3::bigint)";
    char number[32];
    const char *values[3];
    void *r;
    character_save_journal_v2_rpc_transport_outcome answer;

    if (!rpc_world(world) || !rpc_uuid(writer) || !epoch ||
        epoch > (unsigned long long)LLONG_MAX)
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    snprintf(number, sizeof(number), "%llu", epoch);
    values[0] = world;
    values[1] = writer;
    values[2] = number;
    answer = rpc_execute(t, sql, 3, rpc_types, values, &r);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return answer;
    if (!rpc_tuples(t, r, 1, 1) ||
        t->operations->result_value_length(r, 0, 0) != 0) {
        t->operations->result_clear(r);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    t->operations->result_clear(r);
    return answer;
}

static int rpc_expected(const character_save_journal_v2_receipt *x)
{
    if (rpc_equal(x->expected_state, "absent", 6))
        return x->expected_sha256 == 0;
    return rpc_equal(x->expected_state, "existing", 8) &&
        rpc_lower_hex(x->expected_sha256, 64);
}

static int rpc_receipt(const character_save_journal_v2_receipt *x,
    char name[15])
{
    if (!x || !rpc_world(x->world_id) ||
        !rpc_name(x->legacy_name_key, x->legacy_name_key_length) ||
        !rpc_uuid(x->character_id) || !rpc_uuid(x->command_id) ||
        !rpc_uuid(x->writer_instance_id) ||
        !rpc_lower_hex(x->request_sha256, 64) || !x->writer_epoch ||
        x->writer_epoch > (unsigned long long)LLONG_MAX ||
        !x->writer_revision ||
        x->writer_revision > (unsigned long long)LLONG_MAX ||
        !rpc_expected(x) || !rpc_lower_hex(x->post_sha256, 64) ||
        !x->storage_format || x->storage_format > (unsigned int)SHRT_MAX)
        return 0;
    memcpy(name, x->legacy_name_key, x->legacy_name_key_length);
    name[x->legacy_name_key_length] = 0;
    return 1;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_receipt(
    character_save_journal_v2_rpc_transport *t,
    const character_save_journal_v2_receipt *x)
{
    static const char sql[] =
        "select private.record_legacy_published_receipt($1::text,$2::text,"
        "$3::uuid,$4::uuid,$5::uuid,$6::text,$7::bigint,$8::bigint,"
        "$9::text,$10::text,$11::text,$12::smallint)";
    char name[15];
    char epoch[32];
    char revision[32];
    char format[8];
    const char *values[12];
    void *r;
    character_save_journal_v2_rpc_transport_outcome answer;

    if (!rpc_receipt(x, name))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    snprintf(epoch, sizeof(epoch), "%llu", x->writer_epoch);
    snprintf(revision, sizeof(revision), "%llu", x->writer_revision);
    snprintf(format, sizeof(format), "%u", x->storage_format);
    values[0] = x->world_id;
    values[1] = name;
    values[2] = x->character_id;
    values[3] = x->command_id;
    values[4] = x->writer_instance_id;
    values[5] = x->request_sha256;
    values[6] = epoch;
    values[7] = revision;
    values[8] = x->expected_state;
    values[9] = x->expected_sha256;
    values[10] = x->post_sha256;
    values[11] = format;
    answer = rpc_execute(t, sql, 12, rpc_types, values, &r);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return answer;
    if (!rpc_tuples(t, r, 1, 1) ||
        t->operations->result_value_length(r, 0, 0) != 0) {
        t->operations->result_clear(r);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED;
    }
    t->operations->result_clear(r);
    return answer;
}
