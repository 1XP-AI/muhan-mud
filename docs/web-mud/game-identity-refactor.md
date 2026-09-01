# MUD identity·저장 구조 리팩터링

상태: **코드·계약 테스트 완료, testnet 적용 전**  
작성일: 2026-09-01

## 결론

`mud:4000`은 데이터를 저장하는 곳이 아니라 C MUD가 TCP 연결을 받는
리스너다. 기존 게임 아이디와 비밀번호, 레벨, 인벤토리, 위치는
`player/<SHA-1 shard>/<canonical name>` 파일 안의 raw C struct에 저장된다.
따라서 웹 로그인만 추가하면 Supabase 계정과 MUD 캐릭터가 서로 다른 신원으로
남는다.

이번 리팩터링은 권위를 다음처럼 명확히 나눈다.

| 데이터 | 현재 권위 | 브라우저 접근 |
| --- | --- | --- |
| 사람의 로그인·JWT | `auth.users` | Supabase Auth API |
| 계정 ↔ 캐릭터 1:N 소유권·수명주기 | `public.game_characters` | 자기 active row만 RLS SELECT |
| 한 캐릭터의 온라인 세션 임대 | `private.game_character_sessions` | 없음 |
| 레벨·인벤토리·위치·월드 상태 | legacy player/world files | 없음 |
| 향후 구조화 snapshot | `private.game_character_snapshots` | 없음; 아직 권위가 아님 |

즉, Postgres를 곧바로 두 번째 gameplay 저장소로 만들지 않는다. 계정 소유권은
DB가, 게임 상태는 `PlayerStore` 뒤의 파일이 단일 권위를 갖는다. raw struct를
버전 있는 schema로 변환하고 shadow/복구 검증을 마친 뒤에만 게임 상태를 DB로
옮기는 것이 다음 단계다.

## 구현된 접속 흐름

```text
Browser
  ├─ Supabase Auth JWT
  ├─ RLS로 자기 active 캐릭터 목록 조회
  └─ {type, accessToken, characterId}
                    │ WSS
                    ▼
Gateway
  ├─ JWT 서명/issuer/audience/expiry/sub 검증
  ├─ service-role begin RPC: owner + active + canonical name + lease
  ├─ 120초 이하 lease, 60초 주기 exact session/gateway 갱신
  └─ 15초 이하 HMAC admission ticket
                    │ private TCP :4000
                    ▼
C MUD
  ├─ bounded parser/HMAC/expiry/nonce replay/canonical name 검증
  ├─ 기존 동일 캐릭터 저장·종료 후 authoritative file 재로드
  ├─ MUD1 OK 이전에는 game byte를 Gateway가 브라우저로 전달하지 않음
  └─ user UUID/character UUID/nonce는 비영속 session metadata만 사용
```

브라우저가 보낸 `characterId` 자체는 권한 증명이 아니다. Gateway가 같은
transaction/row lock에서 얻은 owner-checked RPC 결과만 신뢰한다. Gateway의
service-role은 identity table을 직접 읽거나 쓸 수 없고
`claim/begin/renew/end` SECURITY DEFINER RPC 실행 권한만 가진다. 종료 RPC도
exact session UUID와 이를 만든 Gateway instance가 모두 일치해야 한다.

Gateway와 C의 ticket은 다음 한 줄이다.

```text
MUD1|<exp>|<nonce32hex>|<user_uuid>|<character_uuid>|<name_utf8_hex>|<hmac64hex>\n
```

JWT, service-role key, legacy password는 ticket·player metadata·DB audit에 넣지
않는다. C의 `MUD_REQUIRE_TRUSTED_ADMISSION=1`은 secret이 누락돼도 legacy
password listener로 내려가지 않고 bind 전 종료하게 한다. Helm은 MUD replica를
1개, `Recreate`로 고정하고 NetworkPolicy로 Gateway만 4000에 접근시킨다.

## 플레이어 저장 경계

`src/player_store.h`는 레거시 호출부와 저장 구현 사이의 seam이다.

- load 결과는 `OK`, `NOT_FOUND`, `CORRUPT`, `IO_ERROR`로 구분한다. 손상 파일을
  없는 캐릭터로 취급해 재생성하지 않는다.
- save는 sibling temp write → file `fsync` → atomic `rename` → parent directory
  `fsync` 순서다. rename 이전 실패는 기존 파일을 보존한다.
- 저장 실패로 소유권을 잃지 않도록 bounded recovery queue가 player object를
  보관한다. recovery가 남아 있으면 새 login은 fail closed한다.
- `MUHAN_HOME`을 명시한 테스트/컨테이너에서는 `/home/muhan` fallback을
  거부해 fixture 밖 파일을 건드리지 않는다.
- 저장 byte format은 아직 `sizeof(creature)` 기반이다. 현재 ABI와 동일한
  Linux/amd64 단일 writer가 필요하다.

## AI가 실행하는 검증 구조

검증은 mock 하나에 의존하지 않고 아래 층을 모두 실행한다.

1. C 단위 계약
   - player path·UTF-8·serializer partial write
   - atomic FileStore 실패 분류와 recovery queue
   - SHA-256/HMAC 공식 vector, ticket parser 경계, canonical name, replay cache
2. 실제 C legacy 시나리오
   - disposable `MUHAN_HOME`에서 생성 → 명령 → 저장 → 종료 → 재로그인
   - 저장 파일 shard/checksum 확인과 truncate corruption 거부
3. 실제 trusted-admission 시나리오
   - no legacy prompt, fragmented ticket, exact ACK, replay/bad MAC/expiry 거부
   - secret 누락/오류 시 listener 기동 거부
   - Node Gateway가 만든 ticket → 실제 C `frp` → WebSocket binary 명령 왕복
   - evidence와 로그에서 password/secret/ticket을 재검사
4. Gateway/Web 계약
   - ownership 거부 전에는 MUD TCP를 열지 않음
   - begin/renew/end 반환값 mismatch와 expiry를 fail closed
   - ACK fragmentation/coalescing, lease renewal, JWT expiry, bounded release retry
   - 캐릭터 선택 전 WebSocket 없음, 정확한 auth frame, ACK 전 binary 거부
5. PostgreSQL 계약
   - migration 2회 적용 idempotency
   - RLS A/B 격리와 browser/service-role privilege surface
   - claim idempotency·경쟁, lease takeover/renew/wrong-gateway/stale-end
   - C와 같은 이름·UTF-8 byte·SHA-1 shard 제약

CI는 C를 Linux amd64/arm64, macOS, Windows에서 빌드·smoke하고, Ubuntu에서 위
단위/실제 시나리오와 Gateway/Web build, PostgreSQL 17 계약을 실행한다.

## 단계별 데이터 이관

### 단계 0 — 저장·테스트 seam (완료)

파일 byte format을 바꾸지 않고 PlayerStore, atomic save, recovery, deterministic
scenario를 도입했다. metadata-only inventory exporter는 filename, payload name,
shard, checksum, size/mtime만 내보내며 password나 raw record를 출력하지 않는다.

### 단계 1 — DB 소유권과 trusted admission (이번 배포)

- `auth.users` 1 : N `game_characters`
- browser own-active RLS와 read-only roster
- service-only session lease
- Gateway HMAC ticket과 C password-bypass admission
- PostgREST를 Supabase Docker 구성 요소로 Helm에 포함

이 단계는 **기존 캐릭터를 자동으로 계정에 연결하지 않는다.** 운영 backfill이나
검증된 claim을 거쳐 `active + owner_user_id`가 된 row만 웹에서 보인다.

### 단계 2 — legacy claim·신규 provisioning (후속)

아직 미구현이다. 기존 password를 한 번만 trusted C control에서 검증하고,
one-time correlation으로 unclaimed row를 정확히 한 계정에 claim해야 한다.
password/proof는 Gateway 로그나 DB에 저장하지 않는다. rate limit, 경쟁 claim,
사용자 알림, 분쟁 기간이 필요하다. 신규 캐릭터는 DB 이름 reservation → C 생성
→ durable save ACK → active 순으로 만든다.

### 단계 3 — 게임 상태 구조화 (후속)

raw player file을 C가 versioned DTO로 export/import하게 하고, file→DB shadow
write, checksum/revision, restore drill을 먼저 운영한다. DB snapshot을 gameplay
read source로 전환하는 cutover는 dual-write 비교가 충분히 통과한 뒤 별도
migration으로 수행한다. 브라우저가 전투·인벤토리 table을 직접 쓰는 구조는
사용하지 않는다.

## 운영 불변조건과 잔여 위험

- C MUD는 단일 replica/single writer다. replay cache도 프로세스 로컬이므로
  다중 replica는 shared replay store 없이는 금지한다.
- ticket TTL은 15초 이하이며 MUD 재시작 시 cache가 초기화된다. ticket이
  로그·브라우저에 노출되지 않는 private hop이라는 전제를 NetworkPolicy와
  secret mount로 유지한다.
- release가 일시 실패하면 Gateway가 bounded retry하고, 최악에는 120초 이하
  lease expiry 뒤 재접속할 수 있다.
- legacy password는 raw player file에 아직 남는다. 이번 단계에서 DB로
  복사하지 않으며, 단계 2 완료와 versioned file migration 전에는 필드를
  제거하지 않는다.
- board/post/family 등 모든 legacy direct I/O가 PlayerStore로 바뀐 것은 아니다.
  이번 seam은 player identity/save 경계다.

적용·검증·rollback 순서는 [Game identity migration 실행 가이드](game-identity-migration.md),
wire 세부사항은 [Trusted admission 계약](trusted-admission.md)을 따른다.
