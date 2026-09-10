# Go 소셜 원장 manifest 생성 절차

`cmd/muhan`의 소셜 manifest builder는 레거시 파일을 직접 PostgreSQL에 쓰지 않고,
사람이 승인한 immutable character ID 매핑을 canonical `family-ledger-v1` 또는
`character-memos-v1` manifest로 변환한다. source root·mapping·output은 별도 경로로
두고, mapping/output 디렉터리는 `0700`, 파일은 `0600`이어야 한다.

## 가족 원장

mapping JSON은 `family_identity` 안에 모든 `family_list` 행의 boss와 member 이름을
명시적으로 연결한다. 이름은 파일의 무결성 대조에만 사용되고 ID가 권한이나 계정을
자동으로 claim하지 않는다.

```json
{
  "version": 1,
  "kind": "family-ledger-v1",
  "world_id": "muhan-01",
  "command_id": "family-import-20260910-01",
  "expected_revision": 0,
  "family_identity": {
    "families": {
      "1": {
        "boss_id": "character-boss",
        "members": {
          "Boss": "character-boss",
          "Alice": "character-alice"
        }
      }
    }
  },
  "recipients": null
}
```

실행 예:

```text
muhan \
  -build-social-family-root /private/muhan-source \
  -build-social-manifest-mapping /private/review/family-mapping.json \
  -build-social-manifest-output /private/review/family-manifest.json
```

## 메모 원장

`recipients` 배열의 각 항목은 `player/fal/<name>` 파일 하나와 정확히 대응한다. 수신자
ID와 발신자 이름→ID는 모두 operator가 공급해야 하며, 생략된 파일을 추측하거나 이름으로
ID를 만들지 않는다. `timestamp_location`을 생략하면 결정론적 UTC를 사용하고, 레거시
ctime의 실제 지역시를 알고 있을 때만 IANA timezone을 명시한다.

```json
{
  "version": 1,
  "kind": "character-memos-v1",
  "world_id": "muhan-01",
  "command_id": "memo-import-20260910-01",
  "expected_revision": 0,
  "recipients": [
    {
      "name": "Bob",
      "id": "character-bob",
      "sender_ids": {
        "Alice": "character-alice"
      },
      "timestamp_location": "UTC"
    }
  ]
}
```

실행 예:

```text
muhan \
  -build-social-memo-root /private/muhan-source \
  -build-social-manifest-mapping /private/review/memo-mapping.json \
  -build-social-manifest-output /private/review/memo-manifest.json
```

`-build-social-manifest-dry-run`은 동일한 locator/parser/identity 검증만 수행하고
manifest·DB·listener를 만들지 않는다. 출력은 같은 mapping 디렉터리에서만 생성되며,
source root와 겹치거나 기존 파일의 bytes가 다르면 fail-closed한다. 생성된 manifest는
내용을 검토한 뒤에만 `-import-social-manifest`로 재검증하고, 실제 변경은 별도
`-import-social-manifest-apply`를 명시할 때만 발생한다. manifest에는 raw path, raw bytes,
password, credential, account claim이 들어가지 않는다.
