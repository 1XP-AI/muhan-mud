# Go PlayerSnapshotV1 이관 manifest 계약

`cmd/muhan`의 `-import-player-snapshot-manifest`는 운영자가 검토한 CDTO
`PlayerSnapshotV1` 파일을 Go PostgreSQL 이관 경계로 전달하는 명시적 일회성 모드다.
일반 서버를 시작하지 않으며, `-import-player-snapshot-manifest-dry-run`을 함께 쓰면
PostgreSQL에 연결하거나 쓰지 않고 모든 입력을 먼저 검증한다.

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
raw player 파일 자동 수집, 대량 데이터 대조·복구, 운영 Supabase 승인, 전체 명령 parity, 브라우저/
IME/mobile, WSS/Ingress와 testnet 승격은 별도 인수 조건으로 남는다.
