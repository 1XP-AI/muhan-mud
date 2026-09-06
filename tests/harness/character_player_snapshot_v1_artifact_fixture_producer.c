/* Test-only producer for the Rust artifact observer boundary.
 *
 * The positive fixture is deliberately created through the real C immutable
 * artifact store.  This harness is never linked into OBJECTS or the live MUD.
 */
#include "character_player_snapshot_v1_artifact.h"
#include "cdto_v1.h"
#include "player_snapshot_v1.h"

#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static void fixture(value)
character_player_snapshot_v1_artifact_metadata *value;
{
    memset(value, 0, sizeof(*value));
    strcpy(value->world_id, "muhan-01");
    strcpy(value->character_id, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    strcpy(value->command_id, "22222222-2222-4222-8222-222222222222");
    strcpy(value->canonical_name_hex, "4d336865726f");
    strcpy(value->request_sha256,
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    strcpy(value->source_post_sha256,
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    strcpy(value->writer_instance_id, "11111111-1111-4111-8111-111111111111");
    strcpy(value->snapshot_format, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT);
    value->writer_epoch = 7;
    value->writer_revision = 2;
    value->storage_format = 1;
    value->source_octets = 9;
}

static int snapshot(bytes, length)
uint8_t **bytes;
size_t *length;
{
    creature player;

    memset(&player, 0, sizeof(player));
    player.type = PLAYER;
    player.fd = -1;
    strcpy(player.name, "M3hero");
    return player_snapshot_v1_encode_loaded(&player, bytes, length);
}

static int copy_file(directory_fd, name)
int directory_fd;
const char *name;
{
    int fd;
    struct stat metadata;
    unsigned char buffer[4096];
    ssize_t amount;

    fd = openat(directory_fd, name, O_RDONLY | O_CLOEXEC);
    if(fd < 0 || fstat(fd, &metadata) || metadata.st_size <= 0) {
        if(fd >= 0) close(fd);
        return -1;
    }
    while((amount = read(fd, buffer, sizeof(buffer))) > 0)
        if(fwrite(buffer, 1U, (size_t)amount, stdout) != (size_t)amount) {
            close(fd);
            return -1;
        }
    if(amount < 0 || close(fd) || fflush(stdout)) return -1;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-player-snapshot-artifact-fixture-XXXXXX";
    char name[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
    character_player_snapshot_v1_artifact_metadata metadata;
    uint8_t *wire;
    size_t wire_length;
    int directory_fd;
    int result;

    wire = 0;
    wire_length = 0U;
    result = 1;
    if(!mkdtemp(root) || chmod(root, 0700)) goto done;
    directory_fd = open(root, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(directory_fd < 0) goto done;
    fixture(&metadata);
    if(snapshot(&wire, &wire_length) != CDTO_V1_OK ||
       character_player_snapshot_v1_artifact_store(directory_fd, &metadata,
           wire, wire_length) != CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK ||
       character_player_snapshot_v1_artifact_filename(&metadata, name,
           sizeof(name)) || copy_file(directory_fd, name)) {
        close(directory_fd);
        goto done;
    }
    close(directory_fd);
    result = 0;
done:
    cdto_v1_free_wire(wire);
    if(root[0]) {
        char command[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
        int cleanup_fd = open(root, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        if(cleanup_fd >= 0) {
            fixture(&metadata);
            if(!character_player_snapshot_v1_artifact_filename(&metadata, command,
                sizeof(command))) unlinkat(cleanup_fd, command, 0);
            close(cleanup_fd);
        }
        rmdir(root);
    }
    return result;
}
