# Go 투표 원장 manifest 생성·적용

레거시 `player/vote/<name>_v` 파일은 먼저 DB 없는 수집기와 ISSUE parser를 통과한 뒤,
operator가 승인한 이름→immutable character ID 매핑과 결합한다. builder는 원본의 선택지
바이트와 SHA-256을 검토 evidence로 사용하지만, raw 경로·파일 metadata·비밀번호를
manifest나 canonical `VoteState`에 넣지 않는다.

## 매핑 입력

mapping 파일은 private `0700` 디렉터리의 `0600` 파일이어야 한다. `players`의 각 행은
`player/vote/<name>_v` 하나와 정확히 대응하며, ID는 이미 존재하는 canonical character
ID여야 한다. 이름으로 ID를 추측하거나 누락/추가 파일을 자동 보정하지 않는다.

```json
{
  "version": 1,
  "kind": "vote-state-v1",
  "world_id": "muhan-01",
  "command_id": "vote-import-20260910-01",
  "expected_revision": 0,
  "catalog_digest": "optional-64-lowercase-hex-digest",
  "players": [
    {"name": "Alice", "id": "character-alice"}
  ]
}
```

`catalog_digest`를 입력에 넣으면 현재 명시한 `post/ISSUE` digest와 일치해야 한다.
`expected_revision`은 운영자가 확인한 world snapshot revision이며, builder가 자동으로
추정하지 않는다.

## 생성·검토

```text
muhan \
  -build-vote-manifest-root /private/muhan-source \
  -build-vote-manifest-issue-file /private/muhan-source/post/ISSUE \
  -build-vote-manifest-mapping /private/review/vote-mapping.json \
  -build-vote-manifest-output /private/review/vote-manifest.json
```

`-build-vote-manifest-dry-run`은 같은 locator/parser/identity 검증을 수행하지만 output,
PostgreSQL, listener를 만들지 않는다. 일반 실행은 source root와 mapping/output이 겹치면
거부하고, output을 immutable `0600`으로 기록한다. 같은 bytes 재실행만 허용하며 다른
bytes로 덮어쓰지 않는다.

생성 manifest는 다음 canonical envelope을 사용한다.

```json
{
  "version": 1,
  "kind": "vote-state-v1",
  "world_id": "muhan-01",
  "command_id": "vote-import-20260910-01",
  "expected_revision": 0,
  "catalog_digest": "64-lowercase-hex-digest",
  "ballots": [
    {
      "legacy_name": "Alice",
      "player_id": "character-alice",
      "choices": "AB",
      "source_sha256": "64-lowercase-hex-digest"
    }
  ]
}
```

`choices`는 ISSUE의 선택지 수와 일치하는 대문자 `A`~`G` 문자열이어야 하며, builder가
원본 bytes에서 다시 계산한다. `source_sha256`은 review evidence로만 보존되고 apply 시
원본 파일을 다시 읽는 권한으로 사용되지 않는다.

## 적용

path-only 검증 또는 `-import-vote-manifest-dry-run`은 DB 연결 없이 끝난다. 실제 변경은
다음 명령에서만 발생한다.

```text
muhan \
  -import-vote-manifest /private/review/vote-manifest.json \
  -import-vote-manifest-apply
```

`ImportVoteState`는 unresolved world snapshot에만 aggregate를 설치하고
`mud_go.vote_imports`와 `world_commands`를 한 transaction에 기록한다. 동일 command와
aggregate만 replay하며 stale revision, 이미 import된 snapshot, foreign character ID,
catalog digest 불일치, raw path/credential 필드는 fail-closed한다. 실제 운영 전에는
operator가 ISSUE digest, character 대조, backup/PITR 및 중단 batch 재개 절차를 별도로
승인해야 한다.
