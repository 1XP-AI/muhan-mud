# Go 은행 snapshot manifest 생성·적용

레거시 bank raw 변환은 먼저 kind-8 `BankSnapshotV1`와 metadata-only review를 만든다.
다음 단계에서 operator가 source player 이름과 이미 연결된 Go account/character, 각
non-root object의 immutable item ID를 대조해 별도 mapping을 작성한다. 이름만으로
계정·캐릭터를 claim하지 않으며 평문 비밀번호나 credential은 이 schema에 넣지 않는다.

```json
{
  "version": 1,
  "world_id": "muhan-01",
  "records": [
    {
      "snapshot_file": "bank.bin",
      "command_id": "bank-import-20260910-01",
      "expected_revision": 0,
      "account_name": "Alice",
      "player_id": "character-alice",
      "item_ids": ["bank-item-01"]
    }
  ]
}
```

mapping 파일은 raw conversion review와 같은 private `0700` 디렉터리의 `0600` 파일로
두고, `snapshot_file`은 review의 `canonical_file`과 정확히 일치시킨다. builder는 review
metadata, canonical SHA-256/크기, kind-8 canonical round-trip, root/node 수, source player
name↔account name, command/revision/player/item 중복을 모두 확인한 뒤 import manifest를
생성한다. output은 review 옆 private `0600` immutable 파일이며 같은 bytes 재실행만
허용한다.

```text
muhan \
  -build-bank-snapshot-manifest-review /private/review/bank.bin.review.json \
  -build-bank-snapshot-manifest-mapping /private/review/bank-mapping.json \
  -build-bank-snapshot-manifest-output /private/review/bank-import-manifest.json
```

`-build-bank-snapshot-manifest-dry-run`은 파일 검증만 한다. 생성물은 path-only
`-import-bank-snapshot-manifest`로 다시 검증할 수 있고, 실제 PostgreSQL 변경은 명시적인
`-import-bank-snapshot-manifest-apply`에서만 발생한다. import는 world snapshot·bank
evidence·command receipt를 기존 `ImportBankSnapshot` transaction에 넘기며, 중단된 batch는
동일 command ID와 expected revision으로 재개한다. artifact 변경, unknown field, path
traversal, digest 불일치, linked character 부재는 fail-closed한다.
