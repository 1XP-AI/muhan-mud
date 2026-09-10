# Go 엔진 중심 포팅·AI GM 실행 계획

상태: 2026-09-10 설계 확정, 구현 진행 중

## 결정

게임 서버의 중심을 “명령을 처리하는 서버”에서 “콘텐츠를 검증·시뮬레이션·발행하는
엔진”으로 확장한다. 맵, 방, 몬스터 템플릿, 스폰 규칙, 시나리오, 이벤트는 모두
엔진이 소유하는 버전 있는 콘텐츠가 된다. AI agent는 GM으로서 엔진 도구를 호출해
제안만 만들고, 엔진의 검증·결정론적 시뮬레이션·발행 경계를 통과한 콘텐츠만 운영
월드에 보이게 한다.

현재 구현된 `server/internal/engine`은 순수 reducer와 PostgreSQL 명령 receipt의
원자 커밋·재생을 조정한다. `world.LegacyRoomCatalog`, `TemplateCatalog`,
`SpawnCatalog`, canonical NPC/room tick은 이미 입력과 규칙을 분리하고 있다. 그러나
콘텐츠 revision/head, 시나리오·이벤트 모델, AI GM typed tool API는 아직 없었다.
이번 배치에서 그 계약의 첫 버전을 `server/internal/engine/content.go`에 추가하고,
`content_catalog.go`에 proposal을 revision-zero 카탈로그에서 다음 immutable head로
materialize하는 순수 함수를 추가했다. 이는 운영 발행이나 AI 호출을 완료했다는 뜻이
아니며, 이후 DB publisher와 tool adapter가 따라야 할 순수 계약이다.

## 권위와 데이터 경계

```text
legacy C resources ──read-only importer──┐
operator/AI GM ─────typed proposal───────┼─> Go content engine
                                        │     validate
                                        │     deterministic simulate
                                        │     publish/rollback
                                        ▼
                         Supabase PostgreSQL content revisions
                                        │
                              published content head
                                        │
                         Go world reducer + tick scheduler
                                        ▼
                         Supabase PostgreSQL live world state
                                        │
                                     xterm.js
```

- **콘텐츠**는 맵·방·출구·몬스터 템플릿·스폰·시나리오·이벤트의 불변 revision이다.
- **라이브 상태**는 플레이어 위치/능력치, NPC 인스턴스, 전투, 아이템, event 실행
  cursor처럼 계속 변하는 값이다. 콘텐츠 revision을 바꿔도 플레이어 상태를 덮어쓰지
  않는다.
- `mud_go.worlds.state`는 당분간 live state 권위를 유지한다. 콘텐츠를 그 JSON에
  임의로 섞거나 브라우저가 DB를 직접 수정하는 경로는 만들지 않는다.
- C 원본은 `Source=legacy-import` proposal로만 엔진에 들어온다. 원본 파일을
  runtime이 직접 읽거나 C/Rust 프로세스를 하위 실행하지 않는다.

## 엔진 콘텐츠 계약

`ContentProposal`은 다음 envelope를 갖는다.

| 필드 | 의미 |
| --- | --- |
| `schema_version` | 엔진 콘텐츠 계약 버전. live snapshot 버전과 분리 |
| `world_id` | 콘텐츠가 속한 세계 |
| `base_revision` | 제안이 기준으로 삼은 published head |
| `proposal_id` | 재시도·idempotency 키 |
| `provenance` | `ai-gm`, `operator`, `legacy-import`, 모델/프롬프트 digest/seed |
| `operations` | 닫힌 typed entity/action 목록 |

허용되는 entity는 `map`, `room`, `monster_template`, `spawn_rule`, `scenario`,
`event`뿐이다. 각 operation은 create/update/delete 중 하나이며, payload의 ID와
operation ID가 일치해야 한다. 임의 SQL, 임의 코드, 임의 player mutation payload는
계약에 없다.

정적 검증은 다음을 검사한다.

- UTF-8·ID·텍스트·레벨 범위와 operation/방/출구/이벤트/스폰 개수 상한
- 동일 entity·출구 방향·tag·scenario event의 중복
- 방의 map 소속, 출구 destination ID 형식, 몬스터 HP/XP/금화 범위
- 이벤트 종류별 필수 대상과 지연·반복 시간, 스폰 수와 level offset
- AI 제안의 model과 SHA-256 prompt digest, parent digest 형식
- action에 따른 위험 등급(`additive`, `mutating`, `destructive`)

operation 배열 순서는 표시상의 차이로 보고 canonical digest에서 정렬한다. 방의
출구와 시나리오 event 배열 순서는 원작 출력·실행 순서를 보존하므로 그대로 둔다.
같은 `proposal_id`와 digest는 재생할 수 있지만, 같은 ID에 다른 digest를 붙이면
거부한다.

## AI GM typed tools

AI agent에게 노출하는 도구는 다음처럼 엔진 API에 한정한다. 각 도구는 world/content
head와 actor policy를 서버에서 주입하며, AI가 `world_id`, revision, SQL을 임의로
대체할 수 없게 한다.

1. `inspect_world` — 공개 가능한 맵·레벨 분포·활성 이벤트·NPC 밀도만 read-only로
   조회한다.
2. `draft_map` — 방/출구/레벨 구간을 `map` operation으로 만든다.
3. `draft_monsters` — typed `monster_template`과 `spawn_rule`을 만든다.
4. `draft_scenario` — 목표와 시작 방, event 순서를 만든다.
5. `draft_events` — message/spawn/unlock-exit/objective만 일정으로 만든다.
6. `validate_content` — 순수 정적 검사와 위험 등급을 반환한다.
7. `simulate_content` — 고정 seed와 fixture로 이동·전투·스폰·event tick을 shadow
   실행하고 before/after digest와 실패 목록을 반환한다.
8. `publish_content` — base head 조건, validation/simulation receipt, 정책을 확인한
   뒤 PostgreSQL에 한 revision을 원자 기록한다.
9. `rollback_content` — 삭제 대신 이전 published head를 가리키는 rollback revision을
   만든다. 이미 실행된 플레이어 보상은 별도 보정 event 없이는 되돌리지 않는다.

AI는 도구 결과로 다음 계획을 이어가지만, `publish_content`가 돌려준 revision만
운영 기준으로 인정한다. 도구 호출 자체는 게임 명령 receipt와 분리된 GM run으로
추적한다.

## GM 운영 루프

```text
관찰(read-only) → 제안(draft) → 정적 검증 → 결정론적 shadow 실행
       → 정책 gate → content revision commit → published head 갱신
       → runtime materialize/tick → 결과 관찰 및 다음 제안
```

- 새 지역을 만드는 additive proposal은 quota·레벨 곡선·시작 지역 연결성·탈출
  경로·NPC 밀도를 통과하면 자동 발행 후보가 될 수 있다.
- 기존 방 삭제, 출구 차단, 이미 사용 중인 event/monster template 변경, 플레이어가
  가진 보상/경제/identity에 영향을 주는 event는 `destructive` 또는 `mutating`으로
  분류하고 operator approval 없이는 publish하지 않는다.
- AI GM은 플레이어 XP/금화/비밀번호/소유권을 직접 변경하지 않는다. 보상이 필요하면
  schema가 정의한 `objective`/`spawn` event를 만들고, 실제 보상은 기존 Go reducer의
  atomic receipt가 수행한다.
- 한 world에는 한 published head만 있다. concurrent GM run은 base revision과
  digest가 맞지 않으면 재계획하며, 마지막 writer가 조용히 덮어쓰지 않는다.
- 실패한 시뮬레이션·quota 초과·참조 해소 실패는 운영 상태를 바꾸지 않는다. rollback은
  revision을 보존한 채 head 포인터만 이동한다.

## C 포팅 경로

1. **Room importer**: `LegacyRoomCatalog`의 reviewed manifest·source digest를
   보존하면서 `map/room` proposal로 변환한다. path/header mismatch와 역사적
   예외는 proposal provenance/quarantine에 남긴다.
2. **Template importer**: `TemplateCatalog`의 mNN/oNN을 typed monster/object
   catalog로 변환한다. 현재 object/item graph는 별도 engine kind를 추가하기 전까지
   runtime에 직접 발행하지 않고 검토 대기한다.
3. **Spawn adapter**: 기존 `SpawnCatalog`, `PlanNPCResourceTick`, room refresh와
   C fixture의 RNG call order를 같은 seed로 실행한다. 차이가 있으면 publish하지
   않고 differential report를 남긴다.
4. **Scenario/event adapter**: C의 room special·trap·quest·talk 분기를 event
   kind별로 매핑한다. arbitrary callback으로 뭉개지 않고, 매핑되지 않은 분기는
   engine에 등록될 때까지 fail-closed한다.
5. **Cutover**: legacy revision과 Go revision의 room graph, level band, spawn
   counts, event trace를 비교한 뒤 published head를 Go로 바꾼다. 기존 player/live
   state는 별도 migration/receipt 없이 변경하지 않는다.

## PostgreSQL 영속화(다음 단계)

Go `storage.Postgres.Migrate` 또는 별도 Supabase migration에 아래 논리 스키마를
추가한다. 실제 SQL은 계약 테스트를 먼저 만들고 그 뒤 추가한다.

- `mud_go.content_revisions(world_id, revision, proposal_id, base_revision,
  status, digest, canonical_content, provenance, simulation_receipt, created_at)`
  — immutable append-only row. `(world_id, proposal_id)`와 `(world_id, revision)`은
  unique다.
- `mud_go.content_heads(world_id, published_revision, head_digest, updated_at)`
  — published head 한 개를 transaction으로 교체한다.
- `mud_go.content_events(world_id, revision, event_id, status, next_at, run_key,
  payload)` — published event의 idempotent scheduler cursor. payload도 엔진 typed
  event에서 나온 canonical JSON만 허용한다.
- `mud_go.gm_runs(world_id, run_id, actor, model, prompt_digest, base_revision,
  proposal_digest, outcome, evidence)` — AI가 본 private prompt/raw player data를
  저장하지 않고 digest와 결과만 남긴다.

DB row만으로 콘텐츠를 publish하지 않는다. Go가 `ValidateContentProposal`과
simulation receipt를 통과시킨 canonical bytes를 공급하고, head update와 revision
insert를 한 transaction으로 묶는다. 동일 run 재시도는 receipt를 읽으며, 서로 다른
bytes는 conflict다.

## 단계별 완료 기준

| 단계 | 범위 | 완료 증거 |
| --- | --- | --- |
| E0 | typed proposal/digest/validation (완료) | 순수 TDD, malformed corpus, stable digest |
| E1 | 순수 content catalog/materializer와 legacy importer (진행 중) | reviewed room/template fixture가 engine revision으로 재현됨 |
| E2 | published content revision/head와 Go world materializer | 이동·look·spawn trace가 published fixture와 일치 |
| E3 | scenario/event scheduler | seed-bound tick, duplicate suppression, restart/replay |
| E4 | AI GM tool adapter와 policy gate | draft→validate→simulate→publish/rollback 전체 계약 |
| E5 | Supabase PostgreSQL 실제 영속화 | migration idempotency, conflict/lease, backup/restore |
| E6 | testnet-1xp 승격 | ARM64 image/Helm, 브라우저 xterm, 장시간 GM expansion canary |

E0~E4는 기존 Go 기능 포팅과 병렬로 진행할 수 있지만, published head가 안정되기
전에는 AI가 운영 월드에 직접 새 방/NPC를 만들지 않는다. E5~E6은 main/release
경계에서 한 번 검증하며 기능 레인마다 PostgreSQL·ARM64·브라우저 검증을 반복하지
않는다.

## TDD·결정론 검증

- 모든 engine tool은 입력/출력 DTO와 순수 validator 테스트를 먼저 만든다.
- proposal 배열 순서, UTF-8/control 문자, 중복 ID, dangling reference, level/수량/
  시간 상한, 위험 분류를 malformed corpus로 고정한다.
- simulation은 clock·RNG·ID allocator를 주입하고 `seed + base_revision + digest`를
  receipt에 기록한다. 동일 입력은 동일 event order/state digest를 내야 한다.
- C importer는 source bytes/digest와 canonical Go bytes/digest를 함께 비교한다.
  차이를 출력 정규화로 숨기지 않고 first differing path/offset으로 남긴다.
- PostgreSQL 계약은 revision conflict, duplicate proposal replay, head rollback,
  scheduler run_key 재실행을 실제 disposable DB에서 검증한다.
- 브라우저는 published content를 읽는 xterm 플레이와 별도로 검증한다. xterm은
  플레이어 입력 장치이며 AI GM 권한이나 DB write API를 노출하지 않는다.

## 이번 배치 결과와 다음 한정 작업

완료한 것은 engine content contract와 순수 `ContentCatalog` materializer의 테스트다.
아직 하지 않은 것은 DB migration/head publisher, legacy room/template importer, AI
모델 호출, 자동 발행, scenario/event scheduler, testnet 배포다. 다음 작업은 E0/E1
계약을 기준으로 reviewed legacy room/template importer를 작은 실패 테스트부터
추가하는 것이다. 그 뒤에만 scenario/event scheduler와 AI tool adapter를 연결한다.
