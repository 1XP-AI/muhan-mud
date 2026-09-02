#!/usr/bin/env bash
set -euo pipefail

# M3 092/094 prerequisite evidence: a Linux container-kill plus named-volume
# restart.  This intentionally makes no host-power-loss claim.

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
fixture="$script_dir/muhan_v2_named_volume_fixture.c"
# gcc 14.2.0 bookworm, immutable linux/amd64 manifest digest.
image="${MUHAN_V2_DOCKER_IMAGE:-gcc:14.2.0-bookworm@sha256:82549aa8f90ada3236a8be70c74543132a76662ef33f0c3271ed802b81584a82}"
platform="${MUHAN_V2_DOCKER_PLATFORM:-linux/amd64}"

if ! command -v docker >/dev/null 2>&1; then
    echo "SKIP: Docker is required for named-volume restart evidence" >&2
    exit 77
fi
if [[ ! -f "$fixture" ]]; then
    echo "RED: required v2 crash/protocol fixture is absent: $fixture" >&2
    exit 1
fi

run_id="$(date +%s)-$$-${RANDOM}"
volume="muhan-m3-092-094-${run_id}"
save_container="muhan-m3-092-094-save-${run_id}"
recovery_container="muhan-m3-092-094-recovery-${run_id}"
recovery2_container="muhan-m3-092-094-recovery2-${run_id}"
snapshot_container="muhan-m3-092-094-snapshot-${run_id}"
snapshot2_container="muhan-m3-092-094-snapshot2-${run_id}"
pre_snapshot="$(mktemp)"
post_snapshot="$(mktemp)"
post2_snapshot="$(mktemp)"
cleanup_status=0

cleanup() {
    set +e
    docker rm -f "$save_container" "$recovery_container" "$recovery2_container" \
        "$snapshot_container" "$snapshot2_container" >/dev/null 2>&1
    docker volume rm "$volume" >/dev/null 2>&1
    for container in "$save_container" "$recovery_container" "$recovery2_container" \
        "$snapshot_container" "$snapshot2_container"; do
        docker container inspect "$container" >/dev/null 2>&1 && cleanup_status=1
    done
    docker volume inspect "$volume" >/dev/null 2>&1 && cleanup_status=1
    rm -f "$pre_snapshot" "$post_snapshot" "$post2_snapshot"
    rm -f "${pre_snapshot}".* "${post_snapshot}".* "${post2_snapshot}".*
    if [[ "$cleanup_status" -ne 0 ]]; then
        echo "cleanup assertion failed: disposable containers or volume remain" >&2
    fi
    exit "$(( ${status:-1} != 0 || cleanup_status != 0 ? 1 : 0 ))"
}
trap 'status=$?; cleanup' EXIT INT TERM

docker volume create "$volume" >/dev/null

compile_and_exec='
set -euo pipefail
gcc -std=gnu89 -fcommon -Wall -Wextra -Werror \
  -DCHARACTER_SAVE_JOURNAL_V2_TESTING \
  -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING \
  -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING \
  -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING \
  -Isrc \
  tests/integration/muhan_v2_named_volume_fixture.c \
  src/character_save_journal_v2_protocol.c \
  src/character_save_journal_v2_recovery.c \
  src/character_save_journal_v2_ack.c \
  src/character_save_journal_v2_publish.c \
  src/character_save_journal_v2_route.c \
  src/character_save_journal_v2_writer.c \
  src/character_save_journal_v2.c \
  src/utf8_text.c -o /tmp/muhan-v2-named-volume-fixture
exec /tmp/muhan-v2-named-volume-fixture "$@"
'

docker run --detach --name "$save_container" --entrypoint bash --platform "$platform" \
    --volume "$volume:/home/muhan" --volume "$repo_root:/work:ro" \
    --workdir /work --env MUHAN_HOME=/home/muhan \
    "$image" -ceu "$compile_and_exec" _ save-crash >/dev/null

for attempt in $(seq 1 60); do
    if docker logs "$save_container" 2>&1 | grep -Fq 'SAVE_CRASH_READY'; then
        break
    fi
    if ! docker container inspect -f '{{.State.Running}}' "$save_container" 2>/dev/null | grep -Fxq true; then
        docker logs "$save_container" 2>&1 || true
        echo "save fixture exited before durable crash evidence" >&2
        exit 1
    fi
    sleep 1
    if [[ "$attempt" -eq 60 ]]; then
        echo "timed out waiting for save crash fixture" >&2
        exit 1
    fi
done

echo "evidence: container-kill plus named-volume restart (not host power loss)"
docker kill "$save_container" >/dev/null
save_status="$(docker wait "$save_container")"
[[ "$save_status" != 0 ]] || { echo "container kill did not terminate save fixture" >&2; exit 1; }

snapshot() {
    local name="$1" phase="$2" output="$3"
    docker run --name "$name" --entrypoint bash --platform "$platform" --rm \
        --volume "$volume:/home/muhan:ro" "$image" -ceu '
phase="$1"
root=/home/muhan
test "$(stat -c %a "$root")" = 700
for d in player player/ba character-save-stage character-save-journal; do
    test "$(stat -c %a "$root/$d")" = 700
done
player="$root/player/ba/M3volume"
test -f "$player"
test "$(od -An -tx1 -v "$player" | tr -d " \n")" = 6d336e616d65642d766f6c756d652d76322d7061796c6f61640a
test "$(stat -c "%a:%h" "$player")" = 600:1
player_digest="$(sha256sum "$player" | awk "{print \$1}")"
test "$player_digest" = 35a0200c0a2907dbe0abc39c439810ba34e4e2b0b3b6f120b96fcbb5ae89767a
printf "file %s %s %s\n" "player/ba/M3volume" "$(stat -c "%a:%h:%d:%i:%s" "$player")" "$player_digest"
for f in character-save-journal/writer-instance.v2 \
         character-save-journal/writer-epoch.v2 \
         character-save-journal/11111111-0000-4000-8000-000000000092.prepared \
         character-save-journal/11111111-0000-4000-8000-000000000092.published; do
    digest="$(sha256sum "$root/$f" | awk "{print \$1}")"
    test -f "$root/$f"
    test "$(stat -c "%a:%h" "$root/$f")" = 600:1
    printf "file %s %s %s\n" "$f" "$(stat -c "%a:%h:%d:%i:%s" "$root/$f")" "$digest"
done
grep -Fq "state=PREPARED" "$root/character-save-journal/11111111-0000-4000-8000-000000000092.prepared"
grep -Fq "state=LEGACY_PUBLISHED" "$root/character-save-journal/11111111-0000-4000-8000-000000000092.published"
if [[ "$phase" == pre ]]; then
    test ! -e "$root/character-save-journal/11111111-0000-4000-8000-000000000092.acked"
else
    acked="$root/character-save-journal/11111111-0000-4000-8000-000000000092.acked"
    test -f "$acked"
    test "$(stat -c "%a:%h" "$acked")" = 600:1
    grep -Fq "state=DB_ACKED" "$acked"
    digest="$(sha256sum "$acked" | awk "{print \$1}")"
    printf "file %s %s %s\n" "character-save-journal/11111111-0000-4000-8000-000000000092.acked" "$(stat -c "%a:%h:%d:%i:%s" "$acked")" "$digest"
fi
test -z "$(find "$root/character-save-journal" -maxdepth 1 -type f -name "*.tmp" -print -quit)"
' _ "$phase" >"$output"
}

snapshot "$snapshot_container" pre "$pre_snapshot"

docker run --name "$recovery_container" --entrypoint bash --platform "$platform" --rm \
    --volume "$volume:/home/muhan" --volume "$repo_root:/work:ro" \
    --workdir /work --env MUHAN_HOME=/home/muhan \
    "$image" -ceu "$compile_and_exec" _ recover

snapshot "$snapshot_container" post "$post_snapshot"

docker run --name "$recovery2_container" --entrypoint bash --platform "$platform" --rm \
    --volume "$volume:/home/muhan" --volume "$repo_root:/work:ro" \
    --workdir /work --env MUHAN_HOME=/home/muhan \
    "$image" -ceu "$compile_and_exec" _ recover

snapshot "$snapshot2_container" post "$post2_snapshot"

grep '^file player/ba/M3volume ' "$pre_snapshot" >"${pre_snapshot}.player"
grep '^file player/ba/M3volume ' "$post_snapshot" >"${post_snapshot}.player"
cmp -s "${pre_snapshot}.player" "${post_snapshot}.player"
for journal_file in writer-instance.v2 writer-epoch.v2 \
    11111111-0000-4000-8000-000000000092.prepared \
    11111111-0000-4000-8000-000000000092.published; do
    grep "^file character-save-journal/$journal_file " "$pre_snapshot" \
        >"${pre_snapshot}.${journal_file}"
    grep "^file character-save-journal/$journal_file " "$post_snapshot" \
        >"${post_snapshot}.${journal_file}"
    cmp -s "${pre_snapshot}.${journal_file}" "${post_snapshot}.${journal_file}"
done
cmp -s "$post_snapshot" "$post2_snapshot"
rm -f "${pre_snapshot}.player" "${post_snapshot}.player"
rm -f "${pre_snapshot}".* "${post_snapshot}".*

echo "PASS: exact player bytes/SHA-256, journal bytes/SHA-256, 0700/0600 modes, nlink=1, and dev/inode stability survived container replacement; recovery was idempotent"
