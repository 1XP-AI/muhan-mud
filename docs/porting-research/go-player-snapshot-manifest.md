# Go PlayerSnapshotV1 이관 manifest 계약

`cmd/muhan`의 `-import-player-snapshot-manifest`는 운영자가 검토한 CDTO
`PlayerSnapshotV1` 파일을 Go PostgreSQL 이관 경계로 전달하는 명시적 일회성 모드다.
일반 서버를 시작하지 않으며, `-import-player-snapshot-manifest-dry-run`을 함께 쓰면
PostgreSQL에 연결하거나 쓰지 않고 모든 입력을 먼저 검증한다.

`-inspect-player-snapshot-dir <dir> -inspect-player-snapshot-world <world>`는 별도의
read-only 수집 모드다. private `0700` 디렉터리의 파일을 lexical 순서로 한 번 스캔하고,
각 파일의 source path/SHA-256/크기/parser·ABI/result/quarantine reason/graph node 수만
`mud_go.player_snapshot_import_ledger`에 기록한다. CDTO payload는 DB에 저장하지 않는다.
`-inspect-player-snapshot-dry-run`은 같은 스캔을 DB 없이 실행한다. malformed·symlink·public
파일은 quarantine evidence가 되며 자동 이관이나 identity claim으로 승격되지 않는다.

원본 C player save를 조사할 때는 형식을 명시적으로 바꾼다.

```sh
# native raw player 파일은 audited little-endian ABI로만 읽고, CDTO로
# 정규화한 뒤 metadata만 ledger에 기록한다(게임 런타임/계정 claim 없음).
go run ./cmd/muhan \
  -inspect-player-snapshot-dir /private/muhan-players/raw \
  -inspect-player-snapshot-world muhan-01 \
  -inspect-player-snapshot-format legacy-player-raw-v1 \
  -inspect-player-snapshot-dry-run
```

raw 형식에는 self-describing ABI가 없으므로 수집 승인 시
`LegacyPlayerSnapshotRawV1ABI` 계약(`creature=1952`, `object=376`, little-endian
`long/pointer=64`)을 별도로 확인해야 한다. Go reader는 `read_crt_player`의 HP/MP·shot
clamp와 NUL 문자열 정규화를 재현하고, descriptor·native pointer·password를 snapshot이나
로그에 넣지 않는다. 지원하지 않는 ABI는 자동 추정하지 않고 격리해야 하며, raw 파일을
직접 `-import-player-snapshot-manifest`에 넣지 말고 operator가 검토한 CDTO와 hash/item
manifest를 만든 뒤 import한다.

검토용 CDTO 산출물은 다음의 별도 변환 모드로 만들 수 있다. 이 모드는 DB에 연결하지
않고 raw source와 output tree를 모두 미리 검증한 뒤 canonical CDTO 파일과
`player-snapshot-review.json`만 기록한다.

```sh
go run ./cmd/muhan \
  -convert-player-snapshot-raw-dir /private/muhan-players/raw \
  -convert-player-snapshot-raw-world muhan-01 \
  -convert-player-snapshot-raw-abi '<LegacyPlayerSnapshotRawV1ABI 전체 문자열>' \
  -convert-player-snapshot-cdto-dir /private/muhan-players/cdto
```

`-convert-player-snapshot-raw-dry-run`을 추가하면 output을 쓰지 않고 같은 검증만
수행한다. source는 private `0700` tree와 `0600` regular file이어야 하며, output은
별도의 private `0700` tree여야 한다. 같은 output bytes의 재실행은 허용하지만 다른
bytes로 덮어쓰지는 않는다. review JSON의 `suggested_account_name`은 snapshot에서
계산한 검토용 제안일 뿐 소유권 증명이 아니며, player ID·command ID·item ID·bcrypt
credential hash는 의도적으로 없다. 운영자는 source와 사람 확인을 거쳐 기존 manifest에
정확한 identity/item mapping과 별도 bcrypt hash를 채운 뒤에만 import해야 한다. raw의
password field는 변환 결과·review·로그에 복사되지 않는다.

## 파일 보안·구성

- manifest와 각 `snapshot_file`은 심볼릭 링크가 아닌 정규 파일이고 권한이 정확히 `0600`이어야 한다.
- manifest는 JSON `version: 1`, 하나의 `world_id`, 하나 이상의 `records`를 가진다. 최대 1024건,
  snapshot 합계 64MiB로 제한된다.
- `credential_hash_b64`는 원본 비밀번호가 아닌 Go가 지원하는 기본 cost의 bcrypt hash bytes를
  표준 Base64로 표현한 값이다. `password`, 평문 비밀번호, 자동 hash 생성 필드는 허용하지 않는다.
- `source_sha256`는 snapshot 파일의 정확한 raw bytes에 대한 소문자 hex SHA-256이며, 검토된 파일과
  일치하지 않으면 전체 manifest가 거부된다.
- `item_ids`는 object graph preorder와 같은 순서로 operator가 미리 할당한 ID 목록이다. ID를
  이름이나 graph 값에서 추측하지 않으며, 개수·중복·제어문자를 검사한다.

예시(실제 hash와 digest는 도구로 채운다):

```json
{
  "version": 1,
  "world_id": "muhan-01",
  "records": [
    {
      "command_id": "import-player-0001",
      "expected_revision": 12,
      "account_name": "Alice",
      "credential_hash_b64": "<bcrypt-hash-bytes-base64>",
      "player_id": "legacy-player-0001",
      "snapshot_file": "snapshots/alice.cdto",
      "source_sha256": "<64 lowercase hex characters>",
      "item_ids": ["item-0001", "item-0002"]
    }
  ]
}
```

`snapshot_file`이 상대 경로이면 manifest 디렉터리를 기준으로 해석하고, 절대 경로도 허용한다.
운영자는 manifest·snapshot을 별도 승인 산출물로 보관하고, 실제 비밀번호를 manifest·명령행·로그에
넣지 않는다.

## 실행·원자성

```sh
# DB 없이 파일·CDTO·hash·manifest만 검증
go run ./cmd/muhan \
  -import-player-snapshot-manifest /private/muhan-players.json \
  -import-player-snapshot-manifest-dry-run

# schema가 이미 migrate된 DATABASE_URL에서 명시적으로 이관
DATABASE_URL='postgresql://...' go run ./cmd/muhan \
  -import-player-snapshot-manifest /private/muhan-players.json
```

`mud_go` schema는 이 모드에서 자동 생성하지 않는다. 처음 한 번만 별도 승인된
`-migrate` 실행으로 schema를 준비한 뒤 import/inspection을 실행하며, 두 모드는
`-migrate`, seed, world listener와 함께 사용할 수 없다.

모든 record와 snapshot은 DB에 접근하기 전에 읽고 검증한다. 이후 record 배열 순서대로
`Postgres.ImportPlayerSnapshot`을 호출하며 각 record는 account·linked character·world snapshot·
이관 evidence·command receipt를 하나의 transaction으로 저장한다. 중간 record에서 DB 충돌이
나면 앞선 transaction은 이미 커밋되어 있을 수 있으므로, 이 도구는 자동 rollback 전체 배치를
약속하지 않는다. 각 record의 `expected_revision`을 명시해 이 점을 운영 manifest 검토에서
확인한다.

네트워크·프로세스 오류 뒤에는 동일 manifest를 같은 `command_id`, hash bytes, snapshot bytes,
item ID 순서로 재실행한다. 이미 커밋된 record는 receipt response만 재생하고 revision을 다시
올리지 않는다. command ID·source SHA·account/player identity·item manifest를 바꾸면
fail-closed conflict가 난다.

## 현재 범위와 승격 조건

이 계약은 한 번에 검토된 snapshot 묶음을 Go import API에 전달하는 재현 가능한 경계다. legacy
raw player 파일의 운영 대량 수집·대조·복구, 운영 Supabase 승인, 전체 명령 parity, 브라우저/
IME/mobile, WSS/Ingress와 testnet 승격은 별도 인수 조건으로 남는다.
