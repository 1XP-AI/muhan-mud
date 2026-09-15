#include "character_save_journal_v2_protocol.h"
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

static const char world[] = "m3-protocol", instance[] = "11111111-1111-4111-8111-111111111111",
                character[] = "33333333-3333-4333-8333-333333333333", command[] = "10000000-0000-0000-0000-000000000001";
static const unsigned char name[] = "M3alpha", payload[] = "crash-payload";
static const char old_hash[] = "5353b2ef3f874939193b748c3a6fecae7c34f2aaba769c2ed9acbb4db45fc663",
                post_hash[] = "d59bfcb3bd6347e7a698af0cf1298f329d8bf29f8c4f5c0921b8879e24d43cfa",
                request_hash[] = "5ad7810336fe5b9d68a23a158b4b847e22fab8f56ca022221b62337b382c7bd0",
                existing_request_hash[] = "4955ba2f6e973a577b13f80ab6a85406ae9a4ae20c57e45d01d693746410b85d";
typedef struct receipt_oracle {
    unsigned int    attempts, advances;
}               receipt_oracle;
typedef struct crash_case {
    character_save_journal_v2_crash_cutpoint cutpoint;
    int             existing;
    unsigned int    expected_callback_attempts;
}               crash_case;
typedef struct recovery_crash_case {
    character_save_journal_v2_crash_cutpoint cutpoint;
    int             existing;
    unsigned int    expected_callback_attempts;
}               recovery_crash_case;
static const char *
request_for(int existing)
{
    return existing ? existing_request_hash : request_hash;
}

static int      join_path(char *out, size_t size, const char *root, const char *leaf){
    int             n = snprintf(out, size, "%s/%s", root, leaf);
    return n < 0 || (size_t) n >= size ? -1 : 0;
}
static int      write_all(int fd, const void *bytes, size_t length){
    const unsigned char *p = bytes;
    ssize_t         n;
    while (length) {
        n = write(fd, p, length);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0)
            return -1;
        p += n;
        length -= (size_t) n;
    } return 0;
}
static int      put_file(const char *root, const char *leaf, const char *text){
    char            path[PATH_MAX];
    int             fd;
    if (join_path(path, sizeof(path), root, leaf) || (fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_NOFOLLOW, 0600)) < 0)
        return -1;
    return fchmod(fd, 0600) || write_all(fd, text, strlen(text)) || fsync(fd) || close(fd) ? -1 : 0;
}
static int      make_directory(const char *root, const char *leaf){
    char            path[PATH_MAX];
    return join_path(path, sizeof(path), root, leaf) || mkdir(path, 0700);
}
static int      stat_leaf(const char *root, const char *leaf, struct stat *st){
    char            path[PATH_MAX];
    return join_path(path, sizeof(path), root, leaf) ? -1 : lstat(path, st);
}
static int
leaf_is_absent(const char *root, const char *leaf)
{
    struct stat     st;
    return stat_leaf(root, leaf, &st) < 0 && errno == ENOENT;
}
static int      leaf_bytes_are(const char *root, const char *leaf, const void *wanted, size_t wanted_length){
    char            path[PATH_MAX];
    const unsigned char *expected = wanted;
    unsigned char   actual[256], extra;
    size_t          total = 0;
    ssize_t         n;
    int             fd;
    if (join_path(path, sizeof(path), root, leaf) || (fd = open(path, O_RDONLY | O_NOFOLLOW | O_NONBLOCK)) < 0)
        return 0;
    while (total < wanted_length) {
        n = read(fd, actual, wanted_length - total > sizeof(actual) ? sizeof(actual) : wanted_length - total);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0 || memcmp(actual, expected + total, (size_t)n)) {
            close(fd);
            return 0;
        }
        total += (size_t)n;
    }
    do n = read(fd, &extra, 1); while (n < 0 && errno == EINTR);
    if (n != 0 || close(fd))
        return 0;
    return 1;
}
static void     set_trusted_uid(void){
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
}

static int      fixture(const char *root, int existing){
    char            a[160], b[220];
    int             n = snprintf(a, sizeof(a), "version=2\nkind=writer-instance\nwriter_instance_id=%s\n", instance);
    if (n < 0 || (size_t) n >= sizeof(a))
        return -1;
    n = snprintf(b, sizeof(b), "version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=7\n", world, instance);
    return n < 0 || (size_t) n >= sizeof(b) || make_directory(root, "player") || make_directory(root, "player/66") || make_directory(root, "character-save-stage") || make_directory(root, "character-save-journal") || put_file(root, "character-save-journal/writer-instance.v2", a) || put_file(root, "character-save-journal/writer-epoch.v2", b) || (existing && put_file(root, "player/66/M3alpha", "old-live")) ? -1 : 0;
}
static character_save_journal_v2_route_lookup_result route_lookup(void *opaque, const char *world_id, const unsigned char *legacy_name, size_t length, character_save_journal_v2_route_reply * reply){
    if (strcmp(world_id, world) || length != sizeof(name) - 1 || memcmp(legacy_name, name, sizeof(name) - 1))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(reply, 0, sizeof(*reply));
    reply->status = CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count = 1;
    strcpy(reply->world_id, world);
    strcpy(reply->character_id, character);
    memcpy(reply->legacy_name, name, sizeof(name) - 1);
    reply->legacy_name_length = sizeof(name) - 1;
    strcpy(reply->legacy_shard, "66");
    reply->storage_format = CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    reply->lifecycle = CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    if (opaque) {
        reply->has_imported_file_sha256 = 1;
        strcpy(reply->imported_file_sha256, old_hash);
    } return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}
static int
serialize(void *opaque, const character_save_journal_v2_writer_tuple * writer, const character_save_journal_v2_bound_route * route, const char *command_uuid, const unsigned char **bytes, size_t * length)
{
    (void)opaque;
    if (!writer || !route || !bytes || !length || strcmp(writer->world_id, world) || strcmp(route->character_id, character) || strcmp(command_uuid, command))
        return -1;
    *bytes = payload;
    *length = sizeof(payload) - 1;
    return 0;
}
static character_save_journal_v2_receipt_result receipt(void *opaque, const character_save_journal_v2_receipt * value){
    const char     *path = opaque;
    receipt_oracle  oracle;
    int             fd;
    ssize_t         n;
    int             existing;
    /* This is a fake local receipt oracle: its attempt and advance counters
     * make no claim about a remote callback service or its persistence. */
    if (!value || strcmp(value->world_id, world) || value->legacy_name_key_length != sizeof(name) - 1 || memcmp(value->legacy_name_key, name, sizeof(name) - 1) || strcmp(value->character_id, character) || strcmp(value->command_id, command) || strcmp(value->writer_instance_id, instance) || value->writer_epoch != 7 || value->writer_revision != 1 || strcmp(value->post_sha256, post_hash) || value->storage_format != 1)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    existing = !strcmp(value->expected_state, "existing");
    if ((!existing && strcmp(value->expected_state, "absent")) || strcmp(value->request_sha256, request_for(existing)) || (existing && (!value->expected_sha256 || strcmp(value->expected_sha256, old_hash))) || (!existing && value->expected_sha256))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    fd = open(path, O_RDWR | O_CREAT | O_NOFOLLOW, 0600);
    if (fd < 0)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    memset(&oracle, 0, sizeof(oracle));
    n = read(fd, &oracle, sizeof(oracle));
    if ((n && n != (ssize_t) sizeof(oracle)) || n < 0) {
        close(fd);
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    } oracle.attempts++;
    if (!oracle.advances)
        oracle.advances++;
    if (lseek(fd, 0, SEEK_SET) < 0 || write_all(fd, &oracle, sizeof(oracle)) || ftruncate(fd, sizeof(oracle)) || fsync(fd) || close(fd))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
}

static int      save_child(const char *root, const char *oracle_path, const char *cutpoint, int existing){
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    set_trusted_uid();
    if (setenv("M3_V2_CRASH_CUTPOINT", cutpoint, 1))
        return 2;
    memset(&request, 0, sizeof(request));
    memset(&operations, 0, sizeof(operations));
    request.root = root;
    request.world_id = world;
    request.canonical_legacy_name = name;
    request.canonical_legacy_name_length = sizeof(name) - 1;
    request.command_uuid = command;
    request.writer_revision = 1;
    operations.route_lookup = route_lookup;
    operations.route_opaque = existing ? (void *)1 : 0;
    operations.serialize = serialize;
    operations.receipt = receipt;
    operations.receipt_opaque = (void *)oracle_path;
    (void)character_save_journal_v2_protocol_save(&request, &operations, &report);
    return 3;
}
static int      recover_child(const char *root, const char *oracle_path, const char *cutpoint){
    character_save_journal_v2_protocol_report report;
    set_trusted_uid();
    if (cutpoint && setenv("M3_V2_CRASH_CUTPOINT", cutpoint, 1))
        return 2;
    if (!cutpoint)
        unsetenv("M3_V2_CRASH_CUTPOINT");
    return character_save_journal_v2_protocol_recover(root, world, receipt, (void *)oracle_path, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK ? 0 : 4;
}
static int      exec_child(const char *program, const char *mode, const char *root, const char *oracle_path, const char *cutpoint, int existing, int *status){
    pid_t           child = fork();
    struct timespec  now, deadline, pause;
    pid_t            waited;
    if (child < 0)
        return -1;
    if (!child) {
        if (!strcmp(mode, "save"))
            execl(program, program, "save", root, oracle_path, cutpoint, existing ? "1" : "0", (char *)0);
        else
            execl(program, program, "recover", root, oracle_path, cutpoint ? cutpoint : "", (char *)0);
        _exit(127);
    }
    if (clock_gettime(CLOCK_MONOTONIC, &now))
        goto reap;
    deadline = now;
    deadline.tv_sec += 10;
    pause.tv_sec = 0;
    pause.tv_nsec = 10000000L;
    for (;;) {
        waited = waitpid(child, status, WNOHANG);
        if (waited == child)
            return 0;
        if (waited < 0 && errno != EINTR)
            return -1;
        if (clock_gettime(CLOCK_MONOTONIC, &now))
            break;
        if (now.tv_sec > deadline.tv_sec ||
            (now.tv_sec == deadline.tv_sec && now.tv_nsec >= deadline.tv_nsec))
            break;
        (void)nanosleep(&pause, 0);
    }
reap:
    (void)kill(child, SIGKILL);
    do waited = waitpid(child, status, 0); while (waited < 0 && errno == EINTR);
    return -1;
}
static int      read_oracle(const char *path, receipt_oracle * oracle){
    int             fd = open(path, O_RDONLY | O_NOFOLLOW);
    ssize_t         n;
    if (fd < 0 && errno == ENOENT) {
        memset(oracle, 0, sizeof(*oracle));
        return 0;
    } if (fd < 0)
        return -1;
    n = read(fd, oracle, sizeof(*oracle));
    return close(fd) || n != (ssize_t) sizeof(*oracle) ? -1 : 0;
}

static int      file_contract(const char *root, const char *leaf, int present, nlink_t links, const void *bytes, size_t length){
    struct stat     st;
    if (!present)
        return leaf_is_absent(root, leaf);
    if (stat_leaf(root, leaf, &st) || !S_ISREG(st.st_mode) || (st.st_mode & 07777) != 0600 || st.st_nlink != links)
        return 0;
    return !bytes || leaf_bytes_are(root, leaf, bytes, length);
}
static int
same_inode(const char *root, const char *left, const char *right)
{
    struct stat     a, b;
    return !stat_leaf(root, left, &a) && !stat_leaf(root, right, &b) && a.st_dev == b.st_dev && a.st_ino == b.st_ino;
}
static int
prepared_contract(const char *root, int present, int existing)
{
    char            expected[1400];
    const char     *leaf = "character-save-journal/10000000-0000-0000-0000-000000000001.prepared";
    int             n;
    if (!file_contract(root, leaf, present, 1, 0, 0))
        return 0;
    if (!present)
        return 1;
    n = snprintf(expected, sizeof(expected), "version=2\nstate=PREPARED\nwriter_instance_id=%s\ncharacter_id=%s\nrequest_sha256=%s\nworld_id=%s\nlegacy_name_key_hex=4d33616c706861\nlegacy_shard=66\ncommand_uuid=%s\nwriter_epoch=7\nwriter_revision=1\nexpected_state=%s\nexpected_sha256=%s\npost_sha256=%s\nstorage_format=1\nstaged_leaf=10000000-0000-0000-0000-000000000001.stage\n", instance, character, request_for(existing), world, command, existing ? "existing" : "absent", existing ? old_hash : "-", post_hash);
    return n >= 0 && (size_t)n < sizeof(expected) && leaf_bytes_are(root, leaf, expected, (size_t)n);
}
static int      marker_contract(const char *root, const char *leaf, int present, nlink_t links, const char *state, int existing){
    char            expected[1400];
    int             n;
    if (!file_contract(root, leaf, present, links, 0, 0))
        return 0;
    if (!present)
        return 1;
    n = snprintf(expected, sizeof(expected), "version=2\nstate=%s\nwriter_instance_id=%s\ncharacter_id=%s\nrequest_sha256=%s\nworld_id=%s\nlegacy_name_key_hex=4d33616c706861\nlegacy_shard=66\ncommand_uuid=%s\nwriter_epoch=7\nwriter_revision=1\nexpected_state=%s\nexpected_sha256=%s\npost_sha256=%s\nstorage_format=1\nstaged_leaf=10000000-0000-0000-0000-000000000001.stage\n", state, instance, character, request_for(existing), world, command, existing ? "existing" : "absent", existing ? old_hash : "-", post_hash);
    return n >= 0 && (size_t)n < sizeof(expected) && leaf_bytes_are(root, leaf, expected, (size_t)n);
}

enum marker_shape { MARKER_ABSENT, MARKER_TEMPORARY, MARKER_PAIR, MARKER_FINAL };

static int
marker_topology(const char *root, const char *temporary, const char *final,
                const char *state, int existing, enum marker_shape shape)
{
    int temp_present = shape == MARKER_TEMPORARY || shape == MARKER_PAIR;
    int final_present = shape == MARKER_PAIR || shape == MARKER_FINAL;
    nlink_t links = shape == MARKER_PAIR ? 2 : 1;
    return marker_contract(root, temporary, temp_present, links, state, existing) &&
           marker_contract(root, final, final_present, links, state, existing) &&
           (shape != MARKER_PAIR || same_inode(root, temporary, final));
}

static int
pre_restart_topology(const char *root, const crash_case *test)
{
    const void      *stage_bytes = 0, *live_bytes = 0;
    size_t          stage_length = 0, live_length = 0;
    int             stage = 0, live = 0, prepared = 0;
    nlink_t         stage_links = 1, live_links = 1;
    enum marker_shape published = MARKER_ABSENT, acked = MARKER_ABSENT;

    switch (test->cutpoint) {
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC:
        live = test->existing;
        live_bytes = "old-live";
        live_length = sizeof("old-live") - 1;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_FILE_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_DIR_FSYNC:
        stage = 1;
        stage_bytes = payload;
        stage_length = sizeof(payload) - 1;
        live = test->existing;
        live_bytes = "old-live";
        live_length = sizeof("old-live") - 1;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_FILE_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC:
        stage = prepared = 1;
        stage_bytes = payload;
        stage_length = sizeof(payload) - 1;
        live = test->existing;
        live_bytes = "old-live";
        live_length = sizeof("old-live") - 1;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_LIVE_PARENT_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_STAGE_PARENT_FSYNC:
        if (!test->existing)
            return 0;
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC:
        if (test->existing)
            return 0;
        prepared = stage = live = 1;
        stage_bytes = live_bytes = payload;
        stage_length = live_length = sizeof(payload) - 1;
        stage_links = live_links = 2;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC:
        if (test->existing)
            return 0;
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = MARKER_TEMPORARY;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = MARKER_PAIR;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = MARKER_FINAL;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = MARKER_FINAL;
        acked = MARKER_TEMPORARY;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = MARKER_FINAL;
        acked = MARKER_PAIR;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        published = acked = MARKER_FINAL;
        break;
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC:
    case CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC:
        prepared = live = 1;
        live_bytes = payload;
        live_length = sizeof(payload) - 1;
        break;
    default:
        return 0;
    }
    return file_contract(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage", stage, stage_links, stage_bytes, stage_length) &&
           file_contract(root, "player/66/M3alpha", live, live_links, live_bytes, live_length) &&
           file_contract(root, "character-save-journal/.m3-writer.lock", 1, 1, "", 0) &&
           prepared_contract(root, prepared, test->existing) &&
           marker_topology(root, "character-save-journal/10000000-0000-0000-0000-000000000001.published.tmp", "character-save-journal/10000000-0000-0000-0000-000000000001.published", "LEGACY_PUBLISHED", test->existing, published) &&
           marker_topology(root, "character-save-journal/10000000-0000-0000-0000-000000000001.acked.tmp", "character-save-journal/10000000-0000-0000-0000-000000000001.acked", "DB_ACKED", test->existing, acked) &&
           (stage_links != 2 || same_inode(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage", "player/66/M3alpha"));
}

static int
post_restart_topology(const char *root, const crash_case *test)
{
    if (!test->expected_callback_attempts)
        return pre_restart_topology(root, test);
    return file_contract(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage", 0, 1, 0, 0) &&
           file_contract(root, "player/66/M3alpha", 1, 1, payload, sizeof(payload) - 1) &&
           file_contract(root, "character-save-journal/.m3-writer.lock", 1, 1, "", 0) &&
           prepared_contract(root, 1, test->existing) &&
           marker_topology(root, "character-save-journal/10000000-0000-0000-0000-000000000001.published.tmp", "character-save-journal/10000000-0000-0000-0000-000000000001.published", "LEGACY_PUBLISHED", test->existing, MARKER_FINAL) &&
           marker_topology(root, "character-save-journal/10000000-0000-0000-0000-000000000001.acked.tmp", "character-save-journal/10000000-0000-0000-0000-000000000001.acked", "DB_ACKED", test->existing, MARKER_FINAL);
}
static int      make_root(char root[PATH_MAX]){
    char            base[PATH_MAX];
    int             n;
    set_trusted_uid();
    if (!realpath("/tmp", base))
        return -1;
    n = snprintf(root, PATH_MAX, "%s/m3-v2-crash-XXXXXX", base);
    return n < 0 || n >= PATH_MAX || !mkdtemp(root) ? -1 : 0;
}
static void
cleanup_fixture(const char *root, const char *oracle_path)
{
    static const char *const leaves[] = {
        "player/66/M3alpha",
        "character-save-stage/10000000-0000-0000-0000-000000000001.stage",
        "character-save-journal/10000000-0000-0000-0000-000000000001.prepared",
        "character-save-journal/10000000-0000-0000-0000-000000000001.published",
        "character-save-journal/10000000-0000-0000-0000-000000000001.published.tmp",
        "character-save-journal/10000000-0000-0000-0000-000000000001.acked",
        "character-save-journal/10000000-0000-0000-0000-000000000001.acked.tmp",
        "character-save-journal/writer-instance.v2",
        "character-save-journal/writer-epoch.v2",
        "character-save-journal/.m3-writer.lock",
        0
    };
    char path[PATH_MAX];
    unsigned int i;
    for(i = 0; leaves[i]; i++)
        if(!join_path(path, sizeof(path), root, leaves[i])) (void)unlink(path);
    if(!join_path(path, sizeof(path), root, "player/66")) (void)rmdir(path);
    if(!join_path(path, sizeof(path), root, "player")) (void)rmdir(path);
    if(!join_path(path, sizeof(path), root, "character-save-stage")) (void)rmdir(path);
    if(!join_path(path, sizeof(path), root, "character-save-journal")) (void)rmdir(path);
    (void)unlink(oracle_path);
    (void)rmdir(root);
}

static int
cutpoint_selector(char *out, size_t out_size,
                  character_save_journal_v2_crash_cutpoint cutpoint)
{
    int n = snprintf(out, out_size, "%lu", (unsigned long)cutpoint);
    return n < 0 || (size_t)n >= out_size ? -1 : 0;
}

int             main(int argc, char **argv){
    static const crash_case cases[] = {
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC, 0, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC, 1, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC, 0, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC, 1, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_FILE_FSYNC, 0, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_FILE_FSYNC, 1, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_DIR_FSYNC, 0, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_DIR_FSYNC, 1, 0},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_FILE_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_FILE_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_LIVE_PARENT_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_STAGE_PARENT_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC, 1, 2}
    };
    static const recovery_crash_case recovery_cases[] = {
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_LIVE_PARENT_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_STAGE_PARENT_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC, 0, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC, 1, 2},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC, 1, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC, 0, 1},
        {CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC, 1, 1}
    };
    char            root[PATH_MAX], oracle_path[PATH_MAX], cutpoint[32];
    receipt_oracle  oracle;
    size_t          i;
    int             status;
    if (argc == 6 && !strcmp(argv[1], "save"))
        return save_child(argv[2], argv[3], argv[4], !strcmp(argv[5], "1"));
    if (argc == 5 && !strcmp(argv[1], "recover"))
        return recover_child(argv[2], argv[3], argv[4][0] ? argv[4] : 0);
    if (argc != 1)
        return 2;
    for (i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
        if (make_root(root) || snprintf(oracle_path, sizeof(oracle_path), "%s.oracle", root) < 0 || fixture(root, cases[i].existing) || cutpoint_selector(cutpoint, sizeof(cutpoint), cases[i].cutpoint) || exec_child(argv[0], "save", root, oracle_path, cutpoint, cases[i].existing, &status) || !WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL || !pre_restart_topology(root, &cases[i])) {
            fprintf(stderr, "character_save_journal_v2_crash_test: named cutpoint pre-restart failed\n");
            return 1;
        }
        if (exec_child(argv[0], "recover", root, oracle_path, 0, 0, &status) || !WIFEXITED(status) || WEXITSTATUS(status) || read_oracle(oracle_path, &oracle) || oracle.attempts != cases[i].expected_callback_attempts || oracle.advances != (cases[i].expected_callback_attempts ? 1U : 0U) || !post_restart_topology(root, &cases[i])) {
            fprintf(stderr, "character_save_journal_v2_crash_test: named cutpoint recovery failed\n");
            return 1;
        }
        cleanup_fixture(root, oracle_path);
    }
    for (i = 0; i < sizeof(recovery_cases) / sizeof(recovery_cases[0]); i++) {
        crash_case interrupted;
        crash_case target;
        target.cutpoint = recovery_cases[i].cutpoint;
        target.existing = recovery_cases[i].existing;
        target.expected_callback_attempts = recovery_cases[i].expected_callback_attempts;
        if (target.cutpoint == CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC ||
            target.cutpoint == CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC)
            interrupted.cutpoint = target.existing ?
                CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT :
                CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT;
        else
            interrupted.cutpoint = CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC;
        interrupted.existing = recovery_cases[i].existing;
        interrupted.expected_callback_attempts = 1;
        if (make_root(root) ||
            snprintf(oracle_path, sizeof(oracle_path), "%s.oracle", root) < 0 ||
            fixture(root, interrupted.existing) ||
            cutpoint_selector(cutpoint, sizeof(cutpoint), interrupted.cutpoint) ||
            exec_child(argv[0], "save", root, oracle_path, cutpoint,
                       interrupted.existing, &status) ||
            !WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL ||
            !pre_restart_topology(root, &interrupted) ||
            cutpoint_selector(cutpoint, sizeof(cutpoint), recovery_cases[i].cutpoint) ||
            exec_child(argv[0], "recover", root, oracle_path, cutpoint, 0, &status) ||
            !WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL ||
            !pre_restart_topology(root,
                target.cutpoint == CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC ||
                target.cutpoint == CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC ?
                &interrupted : &target) ||
            read_oracle(oracle_path, &oracle) ||
            oracle.attempts != target.expected_callback_attempts - 1 ||
            oracle.advances != (target.expected_callback_attempts > 1 ? 1U : 0U)) {
            fprintf(stderr, "character_save_journal_v2_crash_test: recovery cutpoint restart failed\n");
            return 1;
        }
        if (exec_child(argv[0], "recover", root, oracle_path, 0, 0, &status) ||
            !WIFEXITED(status) || WEXITSTATUS(status) ||
            read_oracle(oracle_path, &oracle) ||
            oracle.attempts != target.expected_callback_attempts || oracle.advances != 1 ||
            !post_restart_topology(root, &target)) {
            fprintf(stderr, "character_save_journal_v2_crash_test: recovery cutpoint completion failed\n");
            return 1;
        }
        cleanup_fixture(root, oracle_path);
    }
    puts("character_save_journal_v2_crash_test: ok");
    return 0;
}
