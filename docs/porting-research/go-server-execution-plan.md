# Go 게임 서버 전환 실행 계획

## 2026-09-10 정리 후속 실행 기록

완료된 Orca 작업의 clean worktree 108개는 제거하고, uncommitted 변경이 있는 30개는
보존했다. 현재 Go 기능 통합은 주 worktree에서만 진행하며, 직접 관리한 Luna max
레인은 종료 상태로 정리했다. 이번 배치는 `읽어 <두루마리>`, `초대`,
`패거리누구`·`패거리원`·`모든패거리`의 world plan/apply receipt와
parser→session→WebSocket transport 경계를 추가했다. 스크롤은 RNG·소비·숨김 해제·
읽기 로그·방 이벤트를 원자 적용하고, 초대와 패거리 조회는 canonical identity·
visibility·ordered projection을 보수적으로 적용한다.

검증은 영향 패키지 race, 로컬 `fast`, 조립 `integration`, 대상 `go vet` 및 diff
검사까지 통과했다. 직접 world 전체 실행은 기존 strict room corpus의 알려진 63개
예외로 계속 실패하므로 승격 조건으로 기록한다. ARM64 main build, 실제 PostgreSQL,
브라우저/IME·모바일, release matrix, WSS/Ingress 및 testnet 배포는 기능 레인마다
반복하지 않고 해당 승격 경계에서 한 번만 실행한다. `src/frp.new` 사용자 변경은
이번 기록에서도 수정·stage하지 않는다.

결정일: 2026-09-08 · 상태: 새 목표 활성화, Go G1/G2 수직 명령·저장 경계를 확장하는 중

이 문서가 기존 C→Supabase 확장/Rust 포팅 실행 계획을 대체한다. 기존 코드를 삭제하거나 현재 배포의 저장 권위를 변경하는 결정은 아니다.

## 목표에 넣을 문구

레거시 무한대전 MUD의 게임 기능을 독립 실행 가능한 Go 서버로 이전한다. 웹은 새롬 데이터맨 느낌의 중앙 xterm 단일 화면으로 만들고, 별도 웹 회원가입 없이 원작처럼 터미널 안에서 캐릭터를 생성하고 게임 이름/비밀번호로 로그인한다. 터미널 입력 포커스와 한글·모바일 입력 편의성을 검증한다. 게임 계정과 영속 상태는 Supabase PostgreSQL에 저장한다. C와 Rust는 기존 동작 비교·이관 검증 자산으로만 보존하고 Go 운영 런타임에서는 의존하지 않는다. TDD, 결정론적 동작 비교, 실제 DB 통합 테스트, 브라우저 플레이와 장애 복구 검증으로 전체 기능 목록을 단계적으로 완성한다. 검증은 로컬 우선으로 수행하고, 승인된 배포 절차로 testnet-1xp Helm 배포 및 실사용 검증까지 완료한다. 비용 정책에 따라 모든 하위 작업은 Luna max만 배치하고 독립 작업만 필요한 만큼 병렬화한다. 다른 모델로 자동 승격하지 않는다. 테스트 통과를 절대적 무결성으로 과장하지 않으며, 누락 기능·미검증 항목·실패 증거를 계속 기록한다.

사용자가 이전 목표를 삭제한 뒤 새 Go 목표를 생성했으며 실행 지시가 확인됐다. 이전 목표의 완료를 주장하지 않는다.

## 최신 구현 상태 (전체 인수 전)

- Go 터미널 가입/로그인, 캐릭터 생성 초안의 PostgreSQL 저장/재로그인과 WebSocket
  연결을 구현했다. 격리 PostgreSQL 17과 로컬 브라우저 테스트 기록이 있다.
- 중앙 xterm UI에서 가입/재로그인을 확인했지만, 실제 한글 IME·모바일 키보드 및
  로그인 뒤 월드 플레이는 아직 완료되지 않았다.
- 원본 방 3,216개의 구조 조사와 원본 증거 보존을 구현했다. 엄격한 읽기는 63개의
  이관 예외를 거절하며 전체 corpus 인수 테스트는 실패한다.
- 명시적 Go 월드 실행 경로에서 터미널 가입→입장→대상 없는 `보기`→재로그인을
  실제 PostgreSQL 및 WebSocket/Go 프로세스로 검증했다. 정상 종료와 SIGKILL 뒤
  재시작 복구 테스트도 있다. 브라우저 전체 플레이 인수나 전체 명령 완료는 아니다.
- 방향 이동의 순수 상태 전이와 낙하 사망→아이템 드롭→부활→PG 저장/재생을
  구현했다. 저장된 장비로 휴대 무게·낙하 보정·민첩 보너스를 계산하며,
  방향 이동 뒤 도착 함정의 결정론적 계획/원자 적용(독화살·낙석·MP 피해·구덩이
  재배치·치명상)도 연결했다. 플레이어 leader/follower ID와 recursive movement,
  destination capacity 재평가도 연결했다. 이어서 command2 MFOLLO 추적과
  TRAP_ALARM의 canonical trapexit 영구 NPC 이동/timer/active order, command6
  MDMFOL monster follower 이동/MPERMT 해제/active order를 같은
  DirectionalStep receipt에 연결했다. 해결된 `FollowerRefs`는 C의 단일 mixed
  first_fol 링크 순서를 보존하며 PG replay까지 검증했다. TRAP_ALARM의 due
  catalog/allocator spawn도 원자 receipt에 연결했다. committed movement의
  room-event hub까지 연결했지만, 일반 room-entry/tick spawn orchestration, 원작 전체
  follower별 네트워크/room formatting, 전투 tick과 전체 명령은 아직 남아 있다.
- `State.PlayerUpdateOrder`와 `WorldConnector.RunPlayerVitalPhase`로 player phase의
  첫 영속 수직 슬라이스를 연결했다. effect 만료·HP/MP vitals·inline death continuation·
  light tick은 동일한 `engine.Execute` receipt로 원자 저장되고, 동일 command ID replay는
  reducer/RNG를 다시 실행하지 않는다. `RunPlayerVitalScheduler`가 이를 `-player-tick`
  cadence에 연결하고 `player-vitals-<slot>` ID와 고정 timestamp를 유지한다. 실패하면
  다음 cadence에서 같은 request를 재시도하고, shutdown은 scheduler/cleanup worker를
  먼저 취소한 뒤 session cleanup을 drain한다. 이는 full `update.c` scheduler가 아니며
  NPC/room spawn, combat round, 전체 broadcast와 persistent game clock는 아직 별도
  경계다. ARM64 PG17 통합 테스트는
  `TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`이며 scheduler의
  deterministic slot/retry/shutdown 계약은 `world_tick_test.go`에서 검증한다.
- `ExecuteAttackLine`/`PlanNPCMeleeAttack`로 NPC 대상 전투의 첫 receipt slice를 연결했다.
  exact room NPC 선택, C hit gate·damage/critical RNG·은신 해제·적대 관계를 검증하고,
  lethal 결과는 같은 후보 안에서 NPC identity 제거·active/enemy/follower 정리·XP/성향
  보상·quest bit/proficiency 보상·금화/legacy item graph의 canonical floor 이동·영구 NPC
  timer 갱신까지 처리한다. ID allocator가 없거나 summon 런타임이 필요한 경우에는
  fail-closed하며 부분 사망을 저장하지 않는다. 이어서 command5의 무기 내구도 경계,
  치명타 파괴/일반 명중 드롭, 내구도 감소, NPC 피해 비례 숙련도 보상을 같은 후보에
  연결했다. 이어서 PUPDMG(잠력격발)의 추가 swing 수, lethal/무기 소진 시 loop 중단,
  누적 피해를 하나의 receipt로 처리했다. 로컬 `TestPostgresLethalAttackCommandPersistsNPCDeath`/
  `TestPostgresLethalAttackCommandPersistsNPCDrops`/`TestPostgresAttackCommandPersistsWeaponDrop`/
  `TestPostgresAttackCommandPersistsPowerDamageSequence`와 `TestPostgresAttackCommandPersistsAndReplays`가
  ARM64 PG17 동일 command replay를 검증한다. PVP·flee·summon 생성·death description
  broadcast 및 전체 전투 tick은 아직 G3 범위다.
- `ExecuteStatusLine`은 원작 `건강`/`점수`의 canonical numeric status를 no-state-change
  receipt로 연결했다. blind 분기와 replay를 검증했지만 전체 command parser, ANSI/title 및
  나머지 상태 명령은 후속 범위다.
- `ExecuteFollowLine`으로 `따라 <플레이어>`와 `내보내`를 canonical same-room exact-name
  조회와 `FollowPlayer`/`UnfollowPlayer` reciprocal 관계 저장/replay에 연결했다.
  `내보내`는 인자 없이 자기 leader를 떠나는 C `lose` 경로와 이름을 지정해 자기
  follower를 내보내는 경로를 포함한다. NPC follower 및 legacy 약어/occurrence parser는
  아직 남아 있다. `TestPostgresFollowAndLoseCommandPersistsAndReplays`가 ARM64
  PostgreSQL 17에서 두 명령의 저장·동일 command ID 재생을 확인했다.
- `ExecuteItemsLine`으로 `소지품`과 `장비`/`장` read-only 명령을 canonical item ID 및
  ready-slot 순서에서 렌더링하고 blind/invisible 경계를 적용했다. ARM64 PostgreSQL
  17 `TestPostgresItemsCommandPersistsAndReplays`가 두 응답의 저장·replay를 확인했지만,
  get/drop/wear/remove/hold/ready mutation 및 전체 명령 parser는 남아 있다.
- `ExecuteSayLine`은 `말` 및 따옴표 별칭을 canonical speaker 상태에 적용해 침묵·local
  echo·발화 시 hidden 해제를 receipt로 저장한다. committed receipt 뒤 같은 방의 다른
  연결에만 비동기 event를 보내고 replay에서는 재방송하지 않는다. ARM64 PostgreSQL 17
  `TestPostgresSayCommandPersistsAndReplays` 및 transport event 회귀를 통과했지만,
  전체 broadcast formatting·대화/DM·나머지 parser는 남아 있다.
- `ExecuteSocialLine`은 `누구`/`그룹` read-only 명령을 receipt/replay에 연결했다.
  `PlayerWho`는 room membership 우선 deterministic 온라인 목록과 blind/invisible/detect
  경계를 사용하고, `PlayerGroup`은 mixed `first_fol` 순서의 player/NPC follower HP/MP를
  렌더링한다. ARM64 PostgreSQL 17 `TestPostgresSocialCommandPersistsAndReplays`를
  통과했지만 C descriptor-order/ANSI title 동등성과 group mutation은 남아 있다.
- `ExecuteItemMutationLine`은 `주워`/`주`/`가져`/`꺼내` 및 `버려`/`넣어`를 canonical
  floor/player root 간 nested subtree 이동으로 연결했다. blind/invisible와 equipped-root
  경계를 적용하고 ARM64 PostgreSQL 17 `TestPostgresItemMutationCommandPersistsAndReplays`
  저장·replay를 확인했다. 이어서 `ExecuteEquipmentLine`이 `입어`/`쥐어`/`무장`/`벗어`를
  canonical inventory root와 20개 ready slot 사이의 원자 mutation으로 연결하고, 슬롯
  충돌·파손·저주·기본 직업/성향/크기/무기 제한과 AC/THAC0 재계산을 적용한다.
  ARM64 PostgreSQL 17 `TestPostgresEquipmentCommandPersistsAndReplays` 저장·replay와
  full race/vet/diff-check가 통과했지만, 모두 일괄 명령·전체 C 예외·장비 사용 효과 및
  전체 parser 인수로 승격하지 않는다.
  `주워 모두`/`버려 모두`와 `ItemCollection.Weight`/`CapacityCount`도 추가해 floor
  root의 가시성·경비·무게·소지 수·퀘스트/이벤트 보호를 후보에서 검증한다. container
  내부 `꺼내 <container> <item>`/`넣어 <item> <container>` direct child 이동도
  canonical ID/중첩 소유권과 capacity counter를 보존해 연결했다. gold/quest 보상,
  제물 방·은행·상점 연동은 아직 남아 있다.
- `ExecuteBankLine`은 `잔액`/`입금`/`출금`과 `보관물`/`받아`를 C `RBANK` 방 경계,
  3억냥 상한, `모두` 해석, player gold/계정 잔액 원자성, canonical object-root graph
  이동까지 하나의 durable receipt로 연결했다. ARM64 PostgreSQL 17의
  `TestPostgresBankMoneyCommandPersistsAndReplays`와
  `TestPostgresBankItemCommandPersistsAndReplays`에서 입금·아이템 보관·동일 command ID
  replay·출금을 통과했지만 기존 bank graph 전체 이관과 상점·거래는 아직 남아 있다.
- `끝`도 explicit quit receipt와 WebSocket `Closed` 신호에 연결했다. 응답 전송 뒤 기존
  `Depart`/cleanup 경계가 실행되므로 정상 종료와 소켓 단절의 durable departure가
  동일하다. 전체 quit alias·자동저장·재접속 UX는 아직 별도 인수다.
- 세부 실행 증거/남은 조건은 `server/README.md` 및 기능 원장을 따른다.
  아래 출발점은 준비 당시 기록이며 최신 구현이 없다는 뜻으로 해석하지 않는다.

### 2026-09-09 bounded batch 및 검증 비용 감사

`줄임말`은 목록/추가/삭제와 C suffix 문법을 canonical ordered alias state에 연결했고,
단일 명령 범위의 `$1..$16`/`$*` substitution도 안전하게 확장한다. `;` 다중 명령 queue와
잘못된 치환 문법은 저장·실행 모두 fail-closed로 둔다.
`태워`/`소각`은 direct inventory root occurrence, ONOBUN·quest·event 보호,
관리자 예외, PHIDDN·cooldown·reward/jackpot을 receipt에 기록한다. `배워`/`연마`는
scroll type/level/alignment/class gate, spell catalog 1..56, spell bit·scroll 삭제 및
alignment rejection room 이동을 receipt로 연결한다. 세 명령은 parser/WorldConnector와
replay 억제 room event까지 이어졌으며, legacy `Body.Inventory`가 남은 actor와 unresolved
spell/item은 fail-closed다. unit/session/transport TDD와 connector 회귀가 통과했다.

반복 검증 감사 결과, `fast`는 변경 경로에 영향받는 Go 패키지만 race 검사하고, pre-push는
push diff에 해당하는 migration/stack contract만 실행한다. 전체 race·vet·diff는 필요할 때
`integration`에서, Linux ARM64 cross-build는 실제 main 병합의 `main`에서 batch당 한 번,
durable receipt PG 검사는
`TestPostgresBoundedLanesPersistAndReplay`를 disposable PostgreSQL에서 한 번 실행한다.
정책·self-hosted routing·migration coverage 및 `scripts/run-go-validation.sh integration`이
통과했다. ARM64 이미지/Helm, browser/IME/mobile, strict room corpus, full parity와
testnet 배포는 여전히 release/후속 G3~G5 조건이다.

### 2026-09-09 원작 `!` 명령 재실행 경계

`src/command1.c:1439-1445`의 연결별 `lastcommand` 동작을 Go
`WorldConnector`에 연결했다. `!`은 직전 명령을, `!suffix`는 직전 명령 뒤에 suffix를
붙인 명령을 다음 parser에 넘긴다. 선행 ASCII 공백 제거와 79바이트 UTF-8 안전
history budget을 유지하며, 빈 확장은 기존 history를 보존한다. history는 연결 로컬
상태라서 `State`·Supabase 영속 데이터·receipt에는 저장하지 않는다. 따라서 재접속 시
history가 남지 않고, 확장된 명령만 기존 parser/reducer/receipt 경계를 통과한다.

session pure TDD와 live connector 회귀가 `go test -race`를 통과했고, 전용 ARM64
PostgreSQL 17과 Chromium 가입 시나리오에서 줄임말 등록→실행→`!` 재실행을 포함한
2개 브라우저 테스트가 통과했다. full C parser의
약어 우선순위, 다중 alias command queue 및 전체 출력 parity는 별도 원장 항목으로
남아 있다.

## 확인된 출발점 (역사적 준비 기록)

- 기존 C 게임 서버가 게임 동작의 기준이다. C의 PostgreSQL 연결 확장은 동결한다.
- Rust는 4개 crate의 데이터 계약·인덱스·검증 및 일부 계산 코드다. 독립 로그인/네트워크/게임 서버 구현 완료 상태가 아니다.
- `server/` Go 모듈과 터미널 입력 프레이머를 만들었다. 게임 기능·네트워크 서버·DB 저장은 아직 미구현이며 입력 유틸리티를 게임 완료율로 계산하지 않는다.
- 기본 설치는 Go 1.25.8이나 모듈은 Go 1.27.1로 고정하고 해당 darwin/arm64 툴체인을 내려받아 확인했다. [공식 릴리스 기록](https://go.dev/doc/devel/release)의 지원 정책과 1.27.1 배포를 확인했다. 전역 Go 설치는 변경하지 않았다.
- 준비 시점 Docker 소켓 연결 실패는 재확인에서 해소됐다. `docker info`가 linux/aarch64로
  성공했다. 이후 날짜가 있는 실행 기록에서 PostgreSQL 17 격리 DB 통합 검증을
  추가했지만, 이것이 전체 Docker/배포 인수를 뜻하지는 않는다.
- CI 관련 파일 등 기존 미커밋 변경이 있다. 이 준비 작업의 범위에 포함하거나 덮어쓰지 않는다.

## 목표 구조

브라우저의 xterm.js는 입출력 장치다. 가입과 로그인은 반드시 터미널 안에서 제공하며 소유권과 명령 처리는 서버가 판정한다. 화면/입력 상세 기준은 [터미널 전용 UI 계획](../web-mud/terminal-only-ui-plan.md)을 따른다.

- 웹: 기존 xterm.js와 빌드 자산을 재사용하되 별도 웹 가입/로그인·계정 연결 장벽을 제거한다.
- 연결: WSS 연결 직후에는 가입/로그인 전용 세션만 허용하고, 터미널 인증 성공 후 Go 게임 세션으로 전환한다. 기존 게이트웨이의 웹 JWT 필수 조건도 수정 대상이다. C 텔넷 연결은 필수가 아니다.
- Go: 세션, 캐릭터 생성/연결, 명령, 월드 상태 전이, 저장을 소유하는 단일 서비스로 시작한다.
- 게임 인증: Go가 게임 이름/비밀번호와 세션을 처리한다. 이름과 분리된 내부 ID로 계정/캐릭터 관계를 저장한다. Supabase Auth는 필수 경로에서 제외하고, 재사용이 유리한지 G0에서 판단한다. 가짜 이메일을 만들어 웹 계정을 강제하거나 기존 Auth 데이터를 삭제하지 않는다.
- PostgreSQL: 캐릭터·게임 영속 상태의 최종 권위다. 게임 변경은 Go를 통하며 브라우저가 상태를 직접 수정하지 않는다.
- 운영: 기존 testnet-1xp Helm 및 도메인 `muhan.1xp.vc`를 재사용한다. 도메인·현재 배포 상태는 배포 전에 다시 확인한다.

Go 코드는 `server/`에서 시작한다. 모듈 경로는 `github.com/1XP-Inc/muhan-mud/server`다.

핵심 규칙은 입출력 없는 상태 전이 함수로 분리하고 시간·난수·ID 생성을 주입한다. 네트워크와 저장소는 외부 계층이다. 초기에는 월드별 단일 명령 처리 루프를 사용하며 다중 writer를 허용하지 않는다. 느린 클라이언트의 출력 큐는 제한하고 종료 시 저장·세션 정리를 명시한다. 불필요한 마이크로서비스·Redis 추가는 하지 않는다.

저장은 상태 버전과 명령 ID를 포함하는 트랜잭션으로 설계한다. 동일 명령 재시도는 중복 적용하지 않고, ID는 같지만 내용이 다르면 거부한다. 상태·처리 결과·필요한 이벤트를 원자적으로 기록하여 저장 성공 후 응답 유실을 복구한다. 다중 인스턴스를 도입하기 전에는 lease/fencing 및 중복 실행 검증을 통과해야 한다.

## 단계와 완료 조건

| 단계 | 작업 | 통과 증거 |
| --- | --- | --- |
| G0 | C 명령·저장 포맷·게임 기능 목록, 기존 테스트 매핑, Go 모듈/도구 버전 및 계약 확정 | 전체 기능 ledger, 제외 항목의 사용자 승인, 첫 실패 테스트 |
| G1 | 순수 Go 월드 코어, 리소스 읽기, look/이동과 결정론적 실행 | C 기준 fixture와 출력·상태 비교, 경계/오류 테스트 |
| G2 | 터미널 게임 가입/로그인, 기존 캐릭터 로그인, PG 저장/재접속, 터미널 전용 UI | 웹 계정 없이 브라우저 가입→플레이→재로그인, 포커스/한글/모바일 검증, C 프로세스 없이 성공 |
| G3 | 전투·NPC/tick·아이템/장비·경제·기술 등 전체 기능 단계적 이전 | 기능마다 실패→통과→회귀 검증 및 ledger 갱신 |
| G4 | 전체 데이터 이관, 중복/유실/재시작·동시성·백업복구 | 실제 DB 장애 시나리오, 재실행 가능한 이관 및 복원 증거 |
| G5 | 격리 배포 검증, 기존 Helm 통합, 승인된 testnet 전환 | ARM64 이미지, E2E 플레이, 복구 연습, 전체 인수 기준 통과 |

G0에서 반드시 조사할 범위: 가입·소유권·접속, 방/출구/이동, 전투·사망·부활, NPC·리젠·시간 경과, 인벤토리·장비, 은행·상점·거래, 기술·마법·퀘스트, 채팅·게시판, 관리 기능, 모든 저장 데이터. 실제 소스에서 추가 항목을 발견하면 ledger에 포함한다. 첫 수직 기능만으로 전체 목표를 완료하지 않는다.

각 ledger 항목은 원본 명령/함수, 입력 fixture, 예상 출력/상태, Go 구현, 테스트 명령과 결과, 이관 필요 여부, 인수 상태를 기록한다. 미구현과 의도적으로 변경한 동작을 구분한다. 기능 목록이 확정되기 전 전체 예상 기간이나 분모가 있는 진행률을 제시하지 않는다.

## TDD 및 검증

1. 기존 테스트와 실제 C 동작으로 기대값을 확보한다. 기존 테스트가 의도한 동작과 실제 동작이 다르면 먼저 차이를 기록한다.
2. 작은 Go 실패 테스트를 작성하고 최소 구현으로 통과시킨 후 리팩터링한다.
3. C/Rust golden fixture·리소스·직렬화 검증을 재사용한다. 순서/시간 등 비교에서 제외하는 요소는 개별 문서화하며 포괄적인 출력 정규화로 실패를 숨기지 않는다.
4. 인코딩·숫자 범위·잘린 파일·참조 무결성·중복 캐릭터·다중 로그인·자동 저장·종료 저장·응답 유실을 검증한다. 기존 캐릭터는 기존 비밀번호 검증과 이관을 거쳐 접속하며 이름만으로 소유권을 부여하지 않는다. 웹 계정 연동을 추가 가입 단계로 요구하지 않는다.
5. 순수 테스트, race 검사, 제한된 fuzz 실행, 실제 PG/게임 인증 통합, 브라우저 xterm 흐름, 재시작·복원 검증을 구분해 기록한다.

모듈 생성 이후 표적 로컬 검증은 `scripts/run-go-validation.sh fast`다. 조립된 batch의 전체 Go gate가 필요하면 `scripts/run-go-validation.sh integration`을 실행하며, 이 명령은 ARM64 cross-build 없이 race/vet/diff만 수행한다. 메인 브랜치 병합 시에만 `scripts/run-go-validation.sh main`을 한 번 실행해 ARM64 cross-build를 추가한다. 현재 존재하거나 통과한 명령으로 보고하지 않는다. 통합 테스트는 기존 Docker 구성을 조사해 고유 Compose 프로젝트 이름·임시 볼륨·충돌 없는 포트를 적용한다. 공유 컨테이너/캐시 일괄 삭제는 금지한다.

### 검증 비용 cadence — 2026-09-09

동일한 전체 검증을 병렬 레인마다 반복하지 않는다. 실행 경계는 다음처럼 고정한다.

- **레인 단위**: 담당 파일의 `gofmt`, 영향 패키지 targeted `go test -race`만 실행한다.
  `scripts/run-go-validation.sh fast`가 변경 경로를 자동 분류한다(world→session/transport,
  session→transport, transport-only→transport). 문서·명령 외 변경은 Go 표적 검사를
  건너뛰며, 필요하면 `GO_FAST_RUN`/`GO_FAST_PACKAGES`(`all` 포함)를 주어 덮어쓴다.
  `server/cmd/muhan/` 변경은 `./cmd/muhan`을 추가해 flag·scheduler·listener wiring을
  확인하고, `server/cmd/muhan-browser-e2e/` 변경은 해당 helper 패키지를 추가한다.
  기본적으로 현재 작업 트리만 읽고 깨끗한 트리의 직전 커밋을 반복하지 않는다.
  단, 수동 CI의 clean checkout fast job은 depth 2와 `GO_FAST_COMMIT=1`을 사용해
  마지막 checkout commit의 표적 검사를 실제로 수행한다.
  커밋 자체를 다시 검사할 때만 `GO_FAST_COMMIT=1` 또는 명시적 `GO_FAST_BASE`를 사용한다.
  전체 저장소 race, `go vet ./...`, Linux ARM64 cross-build, disposable PG,
  브라우저·Helm 검증은 레인 완료 조건이 아니다.
- **조립 batch**: 여러 레인을 parser/transport/docs에 합친 뒤 필요할 때
  `scripts/run-go-validation.sh integration`을 한 번 실행한다. 이 명령은 전체 race(엄격
  corpus 예외 제외), vet, diff check만 담당하며 ARM64 cross-build를 실행하지 않는다.
- **메인 병합**: 실제 main 브랜치에 병합하는 시점에만
  `scripts/run-go-validation.sh main`을 한 번 실행한다. 조립 batch gate에 더해 Linux
  ARM64 cross-build를 수행하는 유일한 Go 로컬 경계다. 예전 `merge` 이름은 실수로 이
  비용을 소비하지 않도록 거부한다.

### 2026-09-09 cadence 감사와 신규 bounded lanes

workflow·재사용 경로·matrix·pre-push 호출을 다시 대조한 결과, 자동 push/PR 실행은
없고 기능 레인에서 ARM64·실제 PostgreSQL·브라우저·호환성 matrix가 중복 실행되는
경로도 없었다. release job의 migration 2회 적용은 job 재실행 안전성을 검증하는
의도된 replay라서 제거하지 않는다. 독립 lane은 계속 병렬로 진행하되 각 lane은
담당 패키지 race만, parser/transport 조립 후에만 integration을 한 번 수행한다.

이번 bounded 구현은 `시간`(고정 PST read receipt), `수련`(원작 XP/gold·class
전이의 atomic reducer), `선택 <NPC> [occurrence]`(canonical merchant catalog
read receipt)를 추가했다. 모두 Go world/session/transport와 xterm parser에 연결하고
동일 command ID replay를 검증했다. family/global broadcast 및 전체 merchant purchase
원장이 아직 없으므로 해당 가지는 fail-closed이며 전체 G3 인수로 승격하지 않는다.

이번 배치에서 실행한 것은 `fast`, 조립 `integration`, 정책·shell·migration coverage,
diff 검사다. ARM64 cross-build·실제 DB/browser·release matrix는 각각 `main`/`release`
경계에서만 실행한다.
- **영속성 변경 batch**: 해당 batch의 PG receipt 테스트를 하나의 격리 PostgreSQL에서
  한 번만 묶어 실행한다. 현재 bounded receipt batch는
  `TestPostgresBoundedLanesPersistAndReplay`이며, `MUHAN_BOUNDED_LANES_TEST_DATABASE_URL`
  로 연결한다. 레인별로 같은 이미지/DB를 재생성하지 않으며, 테스트가 만든 컨테이너·볼륨·포트만 정리한다.
- **main/release gate**: ARM64 이미지·차트, x64/Windows 호환, macOS 전용, 브라우저
  IME/mobile, 장애복구·백업은 각각 승인된 통합/릴리스 시점에만 실행한다. x64/Windows와
  macOS 검증을 삭제하지 않지만 Go 기능 레인에서 재실행하지 않는다.

이 정책은 검증을 생략하는 것이 아니라 같은 증거를 가장 가까운 통합 경계에서 한 번
확보하는 것이다. 실패 시 해당 경계를 고정해 원인을 수정하고, 통과한 이전 경계는
변경 파일이 영향을 줄 때만 다시 실행한다.

기존 C 테스트는 oracle 유지를 위해 실행하되 새 C DB 기능 구현으로 범위를 확장하지 않는다. 알려진 C 버그까지 그대로 복제하지 않고 차이를 명시하고 기대 동작을 결정한다. 운영 자격 정보나 실제 사용자 데이터를 테스트 fixture로 복사하지 않는다.

## 재개 시 병렬 배치

오케스트레이터가 G0 계약·기능 ledger·파일 소유권을 확정한 뒤 최대 3개 독립 구현 작업을 배치한다. 선행 소스 조사는 독립 문서에 한해 먼저 병렬화한다.

- Luna max A: 월드 코어/명령/결정론적 상태 전이. `server/internal/game/` 소유.
- Luna max B: A와 공유 계약 확정 후 DB 저장·identity 이관. `server/internal/storage/`, `server/internal/identity/` 소유.
- Luna max C: 확정된 fixture 변환·계약 테스트·검증 문서. 배정된 테스트/문서만 수정.

공유 타입·모듈·migration 순서·빌드 진입점은 오케스트레이터가 통합한다. 인터페이스가 미정인 두 작업을 동시에 구현하지 않는다. 결과를 독립 검증하고 통합한 뒤 에이전트 작업을 종료하며, worktree는 병합 및 모든 변경 보존 여부를 확인한 경우만 정리한다.

## 배포·이관 제한

인프라 저장소는 `/Users/jjangg96/Documents/1xp/tesnet-1xp.nosync`다. 실제 Dockerfile/차트의 기존 관례를 먼저 확인한다. Linux ARM64 로컬 이미지와 의존 바이너리 호환성을 확인하고, CI가 필요할 때 일반 작업은 `[self-hosted, Linux, ARM64]`, macOS 전용만 `[self-hosted, macOS, ARM64]`를 사용한다. 기존 CI 이전 작업은 별도 변경으로 보존하고 완료 여부를 확인한다.

레거시 데이터 이관은 복사본→dry-run→건수/상태/소유권 비교→복구 연습 순서다. 최종 전환은 writer 중지, 최종 이관, 검증, Go writer 시작으로 단일 권위를 유지한다. C/Go 양쪽이 운영 데이터를 동시에 변경하게 하지 않는다. Go 전환 뒤 새 데이터가 생기면 단순히 C 서버를 다시 켜는 것은 안전한 롤백이 아니다. 검증된 역변환 또는 Go 버전 복구 경로가 필요하다.

## 준비 상태와 시작 순서

- 완료: Go 방향과 금지 범위, 실행 순서, 테스트/인수 기준, 병렬 분담, 인계 지침 작성.
- 진행: Go 모듈, 터미널 입력 TDD, C 기능 목록 조사. 게임 서버·CI 실행·데이터 이관·배포는 미완료다.
- Docker daemon 응답을 확인했다. 다음 DB 작업에서는 기존 구성과 격리 자원 이름을 확인한 뒤 통합 테스트를 실행한다.
- 목표는 새 Go 문구로 생성·활성화됐다. G0부터 진행하며 기존 C DB 확장 작업을 재개하지 않는다.

최종 완료는 전체 ledger의 인수 조건과 승인된 배포 검증이 충족될 때만 선언한다. 테스트가 많다는 이유로 “100% 버그 없음”을 보장하지 않는다.

## 2026-09-08 최신 G1 방 리소스 입장 경계

엄격한 `DecodeLegacyRoom`은 원본의 malformed text/trailing bytes를 계속 거절한다.
이를 약화해 corpus 테스트를 녹색으로 만들지 않고, `AdmitLegacyRoom`과
`LegacyRoomAdmissionPolicy`를 별도 호환 경계로 추가했다. 정책은 invalid EUC-KR,
고정 문자열 NUL 종료 누락, trailing data, C `load_rom`의 path-ID 우선 규칙을 각각
명시적으로 허용해야 하며, 잘림·음수/과도한 count·깊이 초과 같은 구조 오류는 어떤
정책에서도 fail-closed다. `LegacyInspection.Source`/SHA-256/issue offset은 admission
뒤에도 원본 증거로 보존하고, replacement text는 클라이언트에 직접 보내지 않는다.

`LoadLegacyRoomCatalog`은 C의 `rooms/r%02d/r%05d` 경로 규칙만 canonical room으로
읽는다. 현재 체크인 트리의 3,216개 파일 중 2,341개가 canonical path에 있고, 다른
875개(예: `r01/r00001`처럼 path shard가 room ID와 맞지 않는 역사적 artifact)는
`Ignored()`에 deterministic하게 기록되어 alternate room 정의로 사용되지 않는다.
`r09/r09000`처럼 raw header ID가 path와 다른 canonical 파일은 정책이 허용할 때
runtime ID를 path ID로 정규화하고 mismatch issue를 evidence에 남긴다.
`LegacyRoomCatalog.NewState`는 빈 플레이어의 초기 `world.State`를 만들지만 legacy
monster/object를 canonical NPC/item ID로 자동 변환하지 않는다. 그 변환과 PG seed,
room graph 참조 검증은 다음 G1/G4 작업이다.

검증: `TestAdmitLegacyRoomRequiresExplicitPolicyForNoncanonicalText`,
`TestAdmitLegacyRoomPreservesSourceEvidence`,
`TestAdmitLegacyRoomNeverRelaxesStructuralValidation`,
`TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts`가 통과했다.
엄격한 `TestRoomBodyCorpus`의 63개 예외 실패 증거는 그대로 유지한다.

`engine.SeedWorldFromCatalog`와 `cmd/muhan -seed-world <id> -seed-rooms <rooms-dir>`도
추가했다. 이 경로는 기존 world를 덮어쓰지 않고 `mud_go.worlds`에 빈 플레이어
snapshot을 한 번만 생성하며, `-migrate`와 분리된 명시적 provisioning 단계다. 실제
ARM64 PostgreSQL 17에서 `TestPostgresSeedLegacyRoomCatalog`와 CLI seed 실행이
2,341개 room snapshot 재로드까지 통과했다. seed 뒤에도 legacy monster/object는
canonical NPC/item graph가 아니므로 게임 입장·tick을 완료했다고 보지 않는다.

## 2026-09-08 최신 G1/G4 resource graph seed

catalog admission은 이름이 비어 있는 zeroed creature record를
`empty-monster-placeholder` issue로 evidence에 추가하고 compatibility policy에서
격리한다. raw source/hash는 그대로 보존하며 strict policy에서는 허용하지 않는다.
이는 C blob의 의미를 추측해 Type 1 NPC로 만드는 대신, 원본과 런타임 admission을
분리한 것이다.

canonical 변환은 세 개의 명시적 경계를 갖는다.

1. `State.ImportNPCs`: 방 ID 오름차순과 room monster slice 순서를 유지해 NPC identity를
   발급하고 `Room.NPCIDs`가 유일한 membership이 되도록 한다.
2. `State.ImportRoomItems`: 방 ID 오름차순, root 순서, nested preorder를 유지해 floor
   `ItemCollection`을 만들고 legacy `Resource.Objects`를 비운다. 빈 방도 빈 canonical
   collection을 갖는다.
3. `State.ImportNPCItems`: canonical NPC room membership 순서로 NPC nested inventory를
   ID graph로 변환하고 `NPCState.Items`를 owner로 만든다. room/player/bank/NPC 간
   duplicate ID는 `State.Validate`에서 거절한다.

각 importer는 clone-commit 방식이며 allocator 실패·중복·최종 참조 오류가 있으면
`State{}`만 반환한다. NPC 표시/입장 projection은 `LegacyInventory`를 임시 view로 만들고,
NPC death drop은 canonical ID를 다시 만들지 않고 room graph로 transfer한다.

`engine.SeedCanonicalWorldFromCatalog`와 `cmd/muhan -seed-world <id> -seed-canonical
-seed-rooms <rooms-dir>`는 검토된 catalog를 deterministic `npc-XXXXXXXX`/
`item-XXXXXXXX` namespace로 one-shot seed한다. `TestLegacyRoomCatalogImportsCanonicalResourceGraphs`,
`TestPostgresSeedCanonicalRoomGraphs`, 실제 ARM64 PostgreSQL 17 CLI seed가 통과했다.
이 경계는 전체 player/bank 이관, full command/tick, 브라우저 E2E, 63개 strict corpus
예외의 최종 승인까지 포함하지 않는다.

## 2026-09-08 명령 수직 슬라이스 후속

`환영`·`외쳐`·bounded 감정표현을 G1/G2의 receipt 경계에 추가했다. 문서 명령은
프로세스 주입 `fs.FS`에서만 읽고, yell/emote의 room event는 committed snapshot에서만
파생한다. 대상·출구·침묵·은신 상태를 확인하지 못하면 부분 저장 없이 fail-closed하며,
동일 command ID replay는 reducer/RNG/event fan-out을 반복하지 않는다.

현재 검증은 unit/race/vet, 전용 ARM64 PostgreSQL 17의 emote/yell 저장·replay, 실제 Go
`-race` + PostgreSQL + Chromium 가입→월드 입장→`환영`→`도움말 정보`→재로그인 E2E
**1 passed (10.4s)**, 표준 xterm/feature-off browser 각 1 passed다. 이는 전체 C
`action.c` alias/154 positive handler, strict room corpus의 63개 예외, 전체 tick/경제/
이관/Ingress/testnet 인수를 완료한 증거가 아니다. 다음 구현은 C oracle fixture와
미구현 ledger 행을 우선순위별로 계속 줄이고, root commit 뒤 infra Docker source
revision pin과 ARM64 Helm 검증을 갱신하는 것이다.

## 2026-09-08 bounded action/command 후속

`표현`과 `보아 <대상>`을 기존 `외쳐`·감정표현 slice와 함께 central parser/transport에
연결했다. free-form payload는 receipt에 직접 저장하지 않고, committed snapshot에서만
room event를 파생한다. target action은 exact canonical identity, room membership,
visibility/detect와 NPC-first 순서를 확인하며 미검증 prefix/occurrence/object 경계는
추측하지 않는다. 동일 command ID replay는 state reducer와 event fan-out을 반복하지 않는다.

검증은 unit/race/vet, 전용 ARM64 PG17 command replay, Linux ARM64 cross-build, 실제 Go+
PG+Chromium E2E **1 passed (10.1s)**다. 전체 C command/action alias와 strict room corpus
63개 예외는 여전히 G3/G4 전환 조건이며, 다음은 hide/track/search 및 combat/economy
잔여 handler를 같은 TDD/differential 기준으로 줄이는 작업이다.

## 2026-09-08 `추적`·`숨겨`/`숨어` bounded stealth 후속

원본 `command4.c:track`의 ranger/관리자 bare 추적 경계를 `PlanTrack`/`ApplyTrack`으로
옮겼다. `LT_TRACK` cooldown, PHIDDN 해제 순서, DEX·레벨 확률, blind/빈 흔적/성공
응답과 같은 방 broadcast를 명시적 proposal/result로 저장한다. 클라이언트가 방향이나
대상을 제출하지 않으며, 객체·출구 추적은 권위 graph가 준비될 때까지 추측하지 않는다.

원본 `command5.c:hide`의 bare player branch도 `PlanHide`/`ApplyHide`로 옮겼다. C의
ASSASSIN/THIEF/RANGER/관리자 확률·interval, blind cap, `LT_HIDES`, 단일 RNG 소비,
성공·실패 PHIDDN 상태와 room projection을 고정했다. 객체 숨김은 ONOTAK 및 object
inventory 권위가 아직 없어 `ErrHideObjectUnsupported`로 fail-closed한다. 입력은 C의
실제 별칭 `숨겨`/`숨어`만 허용하고 추가 토큰은 receipt 전에 거절한다.

두 명령은 central parser → session durable receipt → PostgreSQL replay → WebSocket
fan-out 경계를 통과한다. receipt에 actor 응답과 broadcast outcome을 보존해 같은 command
ID 재시도에서 RNG·reducer·room event가 재실행되지 않는다. unit/race/vet, 실제 격리
ARM64 PostgreSQL 17의 hide replay, Linux ARM64 cross-build, Go+PostgreSQL+Chromium
가입→입장→재로그인 E2E **1 passed (10.9s)**를 통과했다. strict room corpus의 기존
63개 예외, 객체/출구 hide/track/search, 전체 C 명령 table·tick·경제·배포 인수는
여전히 남아 있다.

## 2026-09-08 `엿봐 <대상>` bounded peek 후속

원본 `command4.c:peek`의 NPC-first same-room 대상 선택과 도둑/무적 이상 권한,
blind·invisible/DM-invisible 경계를 Go world proposal로 고정했다. exact 대상 이름만
허용하며 prefix/occurrence와 미이관 object identity는 추측하지 않는다. `LT_PEEKS`의
5초 cooldown, 보호 대상(`MUNSTL`/`MTRADE`/`MPURIT`)의 timer 선행 기록, 레벨 차이
확률과 성공 뒤 발각 확률의 두 RNG 순서를 유지한다. canonical item graph 또는 명시된
legacy inventory projection에서 보이는 root 이름만 응답하고 대상 item ID는 클라이언트에
노출하지 않는다.

발각 결과는 durable receipt에 대상 ID/kind, 대상 개인 메시지와 room 메시지를 함께
저장한다. WebSocket은 최초 commit에서만 `broadcast_rom2` 경계를 투영하고 replay에서는
재전송하지 않는다. unit/session/transport TDD, 격리 ARM64 PostgreSQL 17 replay,
Linux ARM64 cross-build, 실제 Go+PG+Chromium E2E의 권한 거절 경로 **1 passed (10.9s)**를
통과했다. full prefix/occurrence parser, object peek, PVP/NPC 출력 포맷의 전체 C 동등성,
strict room corpus 63개 예외와 나머지 명령/tick/경제/배포 인수는 여전히 남아 있다.

## 2026-09-08 `설정`·`해제` durable player settings 후속

원본 `command5.c:set`/`clear`의 player option 경계를 Go에 연결했다. C의 legacy flag
번호를 그대로 사용해 이야기/잡담/환호/묘사, 소환, 행삽입, 상태, 반향, 색·밝은색,
방이름·설명·출구 표시와 `hexline`/`eavesdropper`/`~robot~`/`수동공격`을 처리한다.
`도망수치`는 `WimpyValue`와 `PWIMPY`를 한 후보에서 갱신하고, `패거리귀환`은
`PFAMIL`이 확인되지 않으면 경고만 반환한다. `설정`의 인자 없는 flag-list와
`해제`의 도움말·명시적 오류 응답도 source formatting으로 고정했다.

입력은 중앙 parser → session reducer → PostgreSQL receipt/replay → WebSocket response로
연결되며, client가 flag 번호를 제출하지 않는다. ordinary `설정` 토글은 계획 당시의
flag를 재검증하고, stale proposal·malformed numeric value·미확인 actor는 fail-closed한다.
`WimpyValue`는 raw C decoder에 영향을 주지 않는 canonical JSON 상태 필드로만 저장한다.

`settings_test.go`, session/transport TDD, `MUHAN_SETTINGS_TEST_DATABASE_URL`를 사용하는
격리 ARM64 PostgreSQL 17 replay가 통과했다. 실제 Go `-race` + PostgreSQL + Chromium
가입→월드 입장→`설정 색`→재로그인 E2E도 **1 passed (11.2s)**다. 전체 C flag-list
출력의 ANSI/약어·관리자 전용 설정, 나머지 154 handler와 full tick/경제/배포 인수는
여전히 미완료다.

## 2026-09-08 `풀어`·`잠궈`·`따` key-door 후속

`command6.c:unlock`/`lock`/`picklock`의 bounded same-room 경계를 다음 Go 수직 조각으로
옮겼다. parser는 `풀어 [출구] [열쇠]`, `잠궈 [출구] [열쇠]`, `따 [출구]`를 원작의
인자 수·응답 순서로 분류하고, world proposal은 권위 snapshot의 첫 exit prefix와
canonical/legacy root key identity를 캡처한다. unlock 성공만 key `shotscur`를 감소시키고
exit `ltime`을 기록하며, lock은 C처럼 내구도를 확인하되 감소시키지 않는다.

picklock은 THIEF/INVINCIBLE 이상·blind·`XLOCKD`를 source 순서대로 검사하고,
`LT_PICKL` 슬롯의 10초 cooldown, DEX bonus·level band·`XUNPCK` chance 0 및 단일
`1..100` RNG를 deterministic receipt outcome으로 보존한다. eligible 시도와 성공을
순서 있는 room event로 fan-out하며 replay에서는 RNG/reducer/event를 재실행하지 않는다.
cooldown에도 C와 같이 PHIDDN을 먼저 해제한다. stale room/exit/key/timer/RNG는
부분 상태 없이 거절한다.

이번 경계의 item lookup은 canonical `ItemCollection.Inventory` 또는 아직 legacy인
`Body.Inventory`의 exact case-insensitive root name으로만 제한한다. nested/equipped
objects와 full prefix/occurrence `find_obj` 정책은 item identity ledger가 확정될 때까지
추측하지 않는다.

검증 증거: world/session/transport/parser TDD, 격리 ARM64 PostgreSQL 17
`TestPostgresDoorKeyCommandPersistsAndReplays`, 전체 Go race/vet, Linux ARM64 cross-build,
실제 Go+PostgreSQL+Chromium `따 __missing_door__` 권한 경계 **1 passed (11.3s)**.
전체 C key lookup/ANSI·occurrence parity, NPC/tick·경제·전체 배포 인수는 여전히
미완료이며 다음 우선순위 ledger에서 계속 줄인다.

## 2026-09-08 `열어`·`닫아` door state 후속

원본 `command6.c:openexit`/`closeexit`의 same-room 출구 경계를 Go에 연결했다.
권위 방 snapshot에서 source-style 첫 prefix match를 선택하고, 잠김/닫힘/문 여부를
검사한 뒤 `XCLOSD`·`ltime`·actor `PHIDDN`을 한 후보에서 원자적으로 갱신한다.
실패 문구는 상태를 바꾸지 않으며, 계획 시점의 room/exit와 actor 상태가 달라지면
Apply가 fail-closed한다. 성공 결과만 committed room snapshot에서 다른 연결에
fan-out하고 actor/replay에는 중복 event를 보내지 않는다.

`풀어`/`잠궈`/`따`는 key object type, key 번호, 내구도, `LT_PICKL` 확률과
`XUNPCK` 예외가 필요한 별도 slice라 이번 경계에 포함하지 않았다. 또한 full
occurrence parser는 아직 source-compatible 전체 명령 table로 승격하지 않았다.

world/session/transport TDD, `MUHAN_DOOR_TEST_DATABASE_URL` 격리 ARM64 PostgreSQL
17 replay, Linux ARM64 build와 실제 Go+PG+Chromium의 `열어 __missing_door__` 경로
**1 passed (10.8s)**를 통과했다. 전체 door/key alias, ANSI/legacy formatting 및
나머지 명령·tick·경제·배포 인수는 계속 남아 있다.

## 2026-09-08 canonical search/inspection 후속

`검색`/`찾아`를 C `command5.c:search`의 exit/object branch까지 bounded하게 확장했다.
출구의 `XSECRT`/`XINVIS`/`XNOSEE`와 canonical `RoomState.Items` root의 `OHIDDN`/
`OINVIS`를 source 순서대로 검사하고, player/NPC 뒤의 기존 hidden target RNG 순서를
변경하지 않는다. exit identity는 room ID+ordered index, object identity는 root item ID로
receipt에 저장하며 Apply와 room event가 stale 이동/visibility를 거절한다. legacy linked
list, nested object, prefix/occurrence는 fail-closed한다.

`보아 <대상>`은 player/NPC 다음 canonical floor root와 exact visible exit를 검사하는
proposal/apply 경계를 추가했다. actor `PHIDDN` 선행 해제, silent branch, target identity와
event replay를 보존하며, full ANSI object description·bare action·legacy fallback은
범위 밖이다. unit/session/transport replay 회귀와 Linux ARM64 race/vet/build는 통과했지만
실제 PG replay는 전용 PostgreSQL 컨테이너 부재로 이번 턴 실행하지 않았다.

## 2026-09-08 직접 관리 병렬 lane 결과

데이터 lane은 strict room corpus의 63개 예외를 원본 증거 fixture로 고정했으며,
oracle이 없는 자동 EUC-KR 치환·NUL 합성·trailing tail 절삭은 하지 않았다. 웹 lane은
중앙 xterm focus와 IME/mobile/reconnect 경계를 보강했고, load lane은 production API를
변경하지 않는 httptest connector probe를 추가했다. 세 커밋은 root에서 diff 검토 후
통합했으며 `src/frp.new`는 제외했다.

load probe의 REST-like 1000 target은 모두 완료되지만 persistent는 현재
MaxSessions=32에서 32개만 admission된다. DB pool 16, connector 전역 command mutex,
chart gateway 200 제한은 별도 운영 부하 검증이 필요하며, 이번 수치만으로 1000 동접
승격·배포 승인을 하지 않는다. 다음 게이트는 실제 disposable PostgreSQL 17 ARM64와
Chromium stack E2E가 가능한 환경에서 replay/ingress를 재실행하는 것이다.

추가 안정성 검토에서 xterm reconnect budget이 view 수신마다 초기화되던 P2를
`6e86daa`에서 제거했다. 반복 transient drop 회귀를 포함한 Playwright 3 tests,
web typecheck/build가 통과했으며, 운영 WSS/Ingress와 실제 모바일 키보드 검증은
여전히 별도 배포 게이트다.

## 2026-09-09 NPC combat/death/shop 후속 경계

`RunNPCCombatTick`/`RunNPCCombatPhase`를 player-vital/resource scheduler와 분리된
durable receipt phase로 추가했다. C `update_active`의 canonical active-NPC·room·첫
enemy/player 순서를 고정하고 `npc-combat-<slot>` request를 pending retry/replay에
재사용한다. 여러 non-lethal `PlanNPCCombatRound` 결과를 하나의 후보에 적용하며,
lethal PLAYER는 `PlanNPCPlayerDeath`와 아직 같은 후보로 조합되지 않아 fail-closed
summary로 남긴다. 실제 ARM64 PostgreSQL test는 저장/재생, request conflict, rollback,
RNG 미재실행을 확인했다.

`PlanNPCPlayerDeath`는 C `creature.c:die` PLAYER branch의 NPC attacker 경계를 별도
순수 reducer로 고정한다. `MSUMMO`는 이 branch에서 검사하지 않으므로 허용하고,
progression/equipment/timer, NPC enemy 제거, source drop, room 1008 admission, war 결과를
원자 후보로 검증한다. death description/broadcast/savegame/summon side effect와
combat tick 연결은 남아 있다.

`QuoteShopPurchase`/`BuyShopItem`과 `RunShopPurchase`는 `RSHOPP`/`RNOTEL` 저장고의
exact canonical stock ID/value를 nested graph deep-copy와 durable receipt로 연결했다.
gold/weight/capacity/duplicate/temporary flag 및 성공 구매 `PHIDDN` 해제를 검증하며,
parser/list/sell/trade/merchant와 실제 shop PG replay는 다음 gate다.

## 2026-09-09 병렬 리뷰 후속 게이트

PR 리뷰와 다음 이슈를 파일 소유권이 겹치지 않는 lane으로 병렬 처리했다.
`fe8408e`는 C `update_active`의 NPC→PLAYER 직접 공격 RNG·피해 경계를 복원했다.
직접 공격은 hit `mrand(1,20)`와 `mdice - armor/5`만 소비하고, player→NPC
`attack_crt` 전용 critical RNG와 NPC PHIDDN/PINVIS 해제는 섞지 않는다.
`1e00ed8`은 lethal NPC→PLAYER를 `PlanNPCPlayerDeath`와 같은 durable candidate에 묶고
사망 후 C `first_active` 재시작 경계에서 tick을 멈춘다.

`f759f49`는 실제 ARM64 PostgreSQL 17에서 `RunShopPurchase`의 receipt 생성·재생·요청
충돌·receipt INSERT rollback을 검증한다. `5992018`은 중앙 xterm의 포커스/IME/mobile
resize 경계를 보강했다. 통합 검증은 Go race(legacy corpus 제외), vet, Linux ARM64
cross-build, web 44/44, 실제 PG 전투·상점 receipt race, Chromium stack E2E 2 passed다.

여전히 전체 C 명령/경제 parity, strict room corpus 63개, NPC 전체 scheduler/broadcast,
IME/mobile 실기기, backup/restore, WSS/Ingress 및 testnet 배포는 별도 인수 조건이다.

## 2026-09-09 G3/G4 병렬 bounded slices 후속

`41e0a47`은 `품목`/`팔아`를 canonical shop/pawn storage와 durable receipt에 연결했다.
정확한 root occurrence, source flag·visibility·quality·weight·gold 검증 및 원자 소유권
전환을 통과시키고 prefix/key·이중지급 RNG·merchant/repair는 fixture가 없어
fail-closed한다. `451bec4`는 `RunNPCCombatTick` 위에 scheduler lifecycle을 추가해
cadence·pending retry·replay·shutdown을 검증하고, `07f63cb`에서 `-npc-combat-tick`과
main worker WaitGroup/shutdown에 연결했다. 전체 `update.c` cadence와 room broadcast는
남아 있다.

`9d8d95c`/`f73b5b1`은 state SHA-256과 format/version/revision을 갖는 `WorldBackup`
envelope와 PostgreSQL restore 경계를 추가했다. 기본 복구는 expected revision 및
receipt 없는 target만 허용하며 Force는 receipt 삭제와 writer fencing을 수행한다.
실제 ARM64 PostgreSQL 17에서 marketplace·combat·backup receipt 통합 테스트와 전체
Go race/vet/ARM64 build, web 44/44를 확인했다. 백업 파일 운영 보관·암호화·복원 연습,
`b9b1abf`의 explicit CLI export/restore와 `c2a0fa5`의 canonical JSON checksum 수정도
실제 PostgreSQL 17에서 확인했다. 백업 파일 운영 보관·암호화·복원 연습,
전체 명령/경제/NPC broadcast, strict corpus 63개, 실기기 IME/mobile, WSS/Ingress와
testnet 배포는 계속 미완료다.

## 2026-09-09 PR 리뷰 병렬 레인: 터미널 구매와 Go 차트 경계

리뷰 대기 중인 독립 범위는 계약과 파일 소유권을 고정한 뒤 병렬 처리하고, main 연결과
최종 통합 검증만 직렬화한다. `bbfd6ee`/`f22df5c`/`5299a18`은 C cmd 42/74의 `사`/`구입`
bounded slice를 live connector에 연결했다. 클라이언트 상품 ID를 권위로 받지 않고 exact
name/positive occurrence로 canonical storage item을 해석하며, `Ownership.RunGame`을
통해 admission을 재검증한다. nested product allocator, receipt replay, request conflict와
실패 시 무저장 경계를 실제 PG17에서 확인했다. merchant/trade/value/수리와 전체 경제
parity는 여전히 별도 gate다.

infra `f96fa6ff`는 Go runtime mode가 PVC를 마운트하지 않는 계약에 맞춰 help directory를
이미지의 `/opt/muhan-seed/help`로 고정하고, source checkout pin을 최신 로컬 검증 커밋
`42ef468`로 갱신했다. Helm/Docker/Secret/차트 전체 로컬 Node test는 **89/89** 통과했다.
이 변경은 원격 push나 testnet 배포를 수행하지 않았으므로 배포 인수 증거가 아니다.

## 2026-09-09 직접 관리 병렬 레인: NPC `교환`

PR 리뷰와 무관한 파일 경계를 유지한 채 Luna max 하위 레인이 C `command10.c:trade`의
원본 suffix 입력(`물건 괴물이름 교환`)과 `MTRADE` offer migration을 조사했고, 메인
세션이 결과를 인수해 통합했다. `NPCTradeOffers`는 `carry` 숫자 쌍을 catalog object
template로 명시적으로 변환하며, unresolved template·비거래 NPC·미이관 offer는
fail-closed한다. live 명령은 exact canonical player root/NPC 이름과 양수 occurrence만
받고, 보상 object graph는 command ID 기반 ID로 원자 복제한다. 동일 command ID 재시도는
receipt의 저장 응답을 그대로 반환하며 RNG/allocator/reducer와 room event를 재실행하지
않는다.

검증 결과:

- `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1` 통과
- `go vet ./...` 통과
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...` 통과
- 실제 ARM64 `postgres:17`에서 trade 저장·replay·request conflict 통과
- 브라우저 mock xterm 3개 + feature-off 1개, 실제 Go+PostgreSQL Chromium 2개 통과

이 레인은 bounded economy progress이며 merchant/repair/value, 전체 C prefix/key matcher,
NPC 전체 cadence/broadcast, strict room corpus 63건, 실기기 IME/mobile, WSS/Ingress와
testnet 배포 인수는 완료로 승격하지 않는다.

## 2026-09-09 직접 관리 병렬 레인: 가치·수리·개인 메시지

파일 소유권을 새 `world`/`session` 파일로 분리해 Luna max 세 레인을 병렬 실행한 뒤,
메인 세션이 공용 parser·connector·recipient event 경계를 통합했다. 원작 등록표의
`가치`/`가격`, `수리`, `얘기`/`이야기`를 터미널 명령으로 분류하며 웹 계정이나 별도
로그인 단계를 추가하지 않는다.

- `가치`/`가격`: RPAWNS·RREPAI 방에서 direct inventory exact name/occurrence를
  읽고 pawn `value/2`(100,000 상한) 또는 repair `value/4`를 read-only receipt로 저장한다.
- `수리`: RREPAI·ONOFIX·무기/방호구·손상도·gold를 확인한 뒤 주입 RNG와 piety 보정으로
  파손/환불/삭제 또는 adjustment 제거·shots 복구를 하나의 atomic candidate로 저장한다.
- `얘기`/`이야기`: online player exact 우선·prefix fallback, PINVIS/PDMINV/PDINVI,
  PIGNOR·PSILNC·빈 메시지 경계를 검증하고 receipt의 deterministic event를 정확한
  수신 연결에만 전달한다. 현재 모델에 없는 last-message/ignore-list 영속 필드는
  추측해 추가하지 않았다.

검증: 대상 world/session race 테스트, transport live connector 테스트, 전체
`go test -race ./... -skip '^TestRoomBodyCorpus$'`, `go vet ./...`, Linux ARM64
cross-build, 실제 ARM64 PostgreSQL 17에서 세 명령의 저장·replay receipt 테스트가
통과했다. 전체 C alias/prefix/key parity, merchant full behavior, strict corpus 63건,
실기기 IME/mobile, WSS/Ingress와 testnet 배포는 여전히 미완료다.

## 2026-09-09 직접 관리 병렬 레인: 묘사·사용자 조회·귀환

다음 세 bounded slice를 Luna max 하위 에이전트가 신규 `world`/`session` 파일만 나눠
구현하고, 메인 세션이 공용 parser와 connector를 직렬 연결했다. `묘사`는 C의 fullstr
31바이트 경계와 canonical 설명 저장을, `사용자검색`/`사용자정보`는 online canonical
identity와 가시성 fail-closed를, `귀환`은 combat/group/destination/MP/event 순서를
각각 source-backed reducer와 durable `ExecuteGame` receipt로 고정한다.

통합 후 live connector는 actor 응답과 observer room event를 분리하며, receipt replay는
상태 변경과 fan-out을 재실행하지 않는다. 실제 ARM64 PostgreSQL 17에서 세 레인의 저장·
재생을 확인했다. 전체 명령 parity, legacy room corpus 63건, NPC full tick/broadcast,
IME/mobile 실기기, WSS/Ingress와 testnet 배포는 아직 남아 있다.

## 2026-09-09 병렬 서비스 lane 통합 계획 결과

다음 병렬 작업은 서로 다른 신규 `world`/`session`/transport test 파일만 소유하고, 메인
세션에서 parser·receipt·recipient fan-out을 통합한다.

1. `상인 구입`은 `WorldConnectorConfig.MerchantOffers`에 server-owned catalog를 주입한다.
   terminal은 NPC/item display name과 positive occurrence만 제출하고, canonical identity와
   reward allocation은 world reducer가 결정한다.
2. `대화`는 receipt의 `NPCTalkEvent`를 commit 이후 observer 연결에만 전달한다. actor는
   receipt response를 받고 replay에서는 event를 재방송하지 않는다.
3. `그룹말`은 receipt의 exact player recipient event만 websocket에 전달하며 NPC follower
   event는 audit 데이터로 보존한다.

각 lane은 malformed input, unresolved migration data, stale/replay 요청을 먼저 테스트하고,
통합 후 `go test -race ./... -skip '^TestRoomBodyCorpus$'`, `go vet ./...`, Linux ARM64
cross-build, disposable ARM64 PostgreSQL 17을 순서대로 실행한다. 이 결과가 통과해도
strict room corpus 63건, 전체 C parity, NPC 전체 cadence, 실기기 IME/mobile, WSS/Ingress와
testnet 배포 인수는 별도 게이트다.

## 2026-09-09 직접 오케스트레이션 후속: 아이템 정보·이름 변경

메인 세션이 Orca를 사용하지 않고 Luna max 에이전트를 세 독립 레인으로 직접 배치했다.
각 레인은 신규 world/session 파일만 소유했고, parser·transport·receipt 통합과 최종
검증은 메인 세션에서 수행했다.

1. `비교` — canonical direct inventory 기반 source-level weapon/armor comparison.
2. `감정` — THIEF/INVINCIBLE 권한과 deterministic object appraisal projection.
3. `명명` — OCNAME/ONAMED를 포함한 80-byte bounded atomic item rename.

세 명령은 모두 `ExecuteGame` receipt/replay를 사용하고, replay에서 reducer·mutation·room
fan-out을 재실행하지 않는다. 실제 ARM64 PostgreSQL 17에서 저장·재생을 확인했다. 이
게이트는 전체 C command table, prefix/key matcher, strict room corpus 63개, NPC full
cadence/broadcast, WSS/Ingress·testnet 배포 인수를 승격하지 않는다.

## 2026-09-09 직접 관리 병렬 후속: 능력 명령과 칭호

두 Luna max 레인이 신규 world/session 파일을 분리해 `활보법`·`신원법`, `경계`·
`잠력격발`, `칭호`·`칭호삭제`의 source-backed reducer와 TDD를 구현했다. 메인 세션은
parser와 WebSocket connector를 직렬 연결하고, committed result의 room event만 첫
실행에서 fan-out하도록 유지했다.

각 reducer는 authenticated actor와 canonical PlayerState만 사용하며, random roll/clock은
주입하고 proposal/apply로 stale snapshot을 거부한다. `ExecuteGame` receipt가 response와
state transition을 함께 저장하므로 retry/replay에서 RNG, mutation, room broadcast가
중복되지 않는다. title은 별도 웹 로그인 없이 xterm에서 생성·조회·삭제하는 78-byte
canonical PlayerState 필드로 제한했다.

통합 검증 명령은 전체 race 테스트, vet, Linux ARM64 cross-build, diff check 및 disposable
ARM64 PostgreSQL 17 receipt 테스트다. 이 레인은 G3 일부 기능의 bounded progress이며,
전체 C parity·strict room corpus·NPC cadence·실기기 IME/mobile·WSS/Ingress·testnet
배포 인수 조건은 여전히 남는다.

## 2026-09-09 검증 비용 전수 점검과 수동 CI scope

반복 실행 경로를 저장소 전체에서 점검한 결과, Go 개발 레인은
`scripts/run-go-validation.sh fast`로 제한하고, 조립된 batch의 전체 확인이 필요할 때만
`scripts/run-go-validation.sh integration`을 실행한다. 이 명령에는 ARM64 cross-build가
없으며, 실제 main 브랜치 병합 시에만 `scripts/run-go-validation.sh main`을 한 번 실행해
그 비용을 추가한다. 레인마다 반복하던 전체 race·`go vet`·Linux ARM64 cross-build·실제
PostgreSQL·브라우저·차트 검증은 레인 완료 조건에서 제외했다. 변경 batch에 영속성 경계가
있을 때만 고유 ARM64 PostgreSQL 컨테이너에서 receipt/replay를 한 번 실행한다.

`.github/workflows/ci.yml`는 자동 push/PR 트리거를 추가하지 않고 수동
`validation_scope` 입력을 제공한다.

- `fast`(기본): self-hosted Linux ARM64에서 Go world/session/transport 표적 race만 실행
- `integration`: 전체 Go race(기존 strict room corpus 예외), vet, diff check만 실행하며
  cross-architecture build는 건너뜀
- `main`: `integration`에 더해 Linux ARM64 cross-build를 실제 main 병합 시 한 번 실행
- `release`: 기존 Supabase/PostgreSQL 계약, 브라우저/stack, x64·Windows·macOS 호환 matrix를
  승인된 release 검토 시에만 실행

수동 workflow의 scope가 기본값 `fast`이므로 기능 레인이나 일반 수동 확인이 ARM64 전체
matrix와 브라우저/DB 비용을 자동으로 소비하지 않는다. 호환성·차트·실기기 IME/mobile과
testnet 배포는 삭제하지 않고 release gate로 남겼다. 현재 적용 후 정책/runner 정적 검사는
통과했고, 실제 원격 workflow dispatch·push·배포는 실행하지 않았다.
전수 정적 검사에서 stack E2E runner가 누락했던 20261016~20261027 migration 12개도
시간순 apply 목록에 보강했고, migration coverage 57개·shell 문법 검사를 통과했다.

### 2026-09-09 release scope 재검사

`release`는 DB 계약과 Linux ARM64·x64·Windows·macOS 호환 matrix를 포함하므로 기능
브랜치 피드백에 재사용하지 않는다. 수동 입력 실수로 이 scope를 feature branch에서
선택하는 경우를 막기 위해 `release-scope-guard`를 추가했다. 기본 브랜치가 아니면
checkout, PostgreSQL 서비스, 의존성 설치와 matrix fan-out 전에 실패한다. 따라서
개발 중에는 `fast` 또는 필요한 batch의 `integration`만 실행하고, ARM64 cross-build는
실제 main 병합 시 `main`에서 한 번, release 호환성 matrix는 승인된 기본 브랜치 검토에서
한 번만 실행한다.

## 2026-09-09 직접 관리 병렬 후속: 뇌물·물건 숨기기·도망 함정

Luna max 세 레인을 서로 다른 world/session 파일 소유권으로 병렬 실행하고 메인 세션에서
parser·connector·room event를 통합했다. `뇌물`은 NPC visibility와 원작 threshold, gold
debit·MTRADE 상태·follower/enemy 정리를 atomic receipt로 저장한다. `숨겨`/`숨어`는 같은
방 canonical floor object와 occurrence를 선택하고 `OHIDDN` stealth와 C식 확률/RNG를
한 번 적용한다. `도망`은 기존 arrival-trap hook을 실제 dart/pit/alarm/death 전이에
연결하며 성공 이동과 실패 사망·경보를 모두 replay-safe receipt에 기록한다.

검증은 레인 targeted race, parser/live connector 회귀, `scripts/run-go-validation.sh fast`,
이전 통합 증거에서는 기존 `merge` gate를 통과했다. 현재 cadence에서는 조립 batch의
`integration`과 main 병합의 `main`으로 분리한다. 별도 ARM64 `postgres:17`에서
`TestPostgresBribeCommandPersistsAndReplays` 저장·동일 command replay도 통과했으며,
컨테이너는 테스트 직후 제거하고 공유 `sws26-db`는 건드리지 않았다. 전체 C parity, strict
room corpus, NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet 배포는 아직 남아 있다.

## 2026-09-09 직접 관리 병렬 후속: NPC 순서·저장·대화 자산 provenance

세 Luna max 레인을 파일 소유권으로 분리해 다음 경계를 추가했다.

1. `NPCWorldScheduler`가 maintenance → resource → combat phase를 하나의 worker에서
   순서대로 실행한다. phase별 cadence(각각 1초/20초/1초 기본값)는 유지하고, 최소 cadence로
   wake한 뒤 각 durable tick의 slot suppression을 사용한다. 중간 phase가 실패하면 후속
   phase를 실행하지 않으며 다음 cadence에서 동일 pending request를 재시도한다.
2. 원작 `저장` cmdno 52를 Go canonical state의 no-state durable receipt로 연결했다. 모든
   mutation은 이미 자체 receipt로 저장되므로 별도 player-file write를 만들지 않는다. xterm
   `저장`과 명시적 호환 `save`만 허용하고 offline/인자/제어문자는 fail-closed한다.
3. `아파트_수위_아저씨-127`는 manifest와 Git blob이 byte-for-byte 같지만 CP949 offset
   216의 standalone `0xBA` 때문에 strict decode가 불가능하다. 정확한 원본을 찾지 못한
   상태에서 임의 수정하지 않고 provenance 문서와 resource regression으로 admission을 계속
   fail-closed한다.

검증은 scheduler/session/live connector targeted race와 `cmd/muhan` 프로세스 테스트를
새 변경에 대해 실행한다. 전체 integration, Linux ARM64 cross-build, 실제 PostgreSQL,
브라우저·release matrix는 이전 batch 증거를 재사용하며 이번 기능 레인에서 반복하지 않는다.
전체 C command parity, strict room corpus, NPC full combat/broadcast, 실기기 IME/mobile,
WSS/Ingress와 testnet 배포 인수는 여전히 남아 있다.

### 2026-09-09 xterm 메일·게시판 continuation 연결

`편지보내기 <이름>`과 `써`를 `WorldConnector`의 연결별 continuation으로 연결했다.
작성 중 입력은 history/alias/일반 parser보다 먼저 소비하고, 최종 `.` 한 번만
`ExecuteGame` receipt를 생성한다. 메일 본문에서 `!`와 `!!`는 그대로 보존하며, 게시판
본문의 `!!`와 제목 단계의 빈 줄은 영수증 없이 취소한다. transient commit 오류에도
command ID·메일 ID·시각·payload를 초안에 유지해 같은 요청을 재시도하고, 완료/연결 종료
시 초안을 폐기한다. 게시판 event는 최초 commit 성공 뒤에만 전송한다.

world/session/transport targeted race와 전체 Go integration을 이 변경을 조립한 뒤 한 번
검증했다. `MUHAN_MAIL_BOARD_TEST_DATABASE_URL`이 설정된 경우에만 하나의 disposable
PostgreSQL에서 mail/board receipt와 replay를 실행하도록 테스트를 추가했다. 원작의
제목 직후 `.` 빈 게시글 등록은 현재 fail-closed 차이이며 differential 결정 전까지
운영 경로에서 허용하지 않는다. ARM64 cross-build, browser/IME/mobile, release matrix와
testnet 배포는 해당 cadence 경계에서만 실행한다.

## 2026-09-09 메일·게시판 bounded slice 및 검증 중복 감사

세 Luna max 레인을 직접 병렬 실행한 뒤 메인 세션에서 공용 parser와 WebSocket connector를
통합했다.

1. `편지받기`/`편지삭제`는 `src/post.c`의 RPOSTO(10) 방 경계와 canonical
   `State.Mailboxes`를 사용한다. 수신자별 ordered message, sender identity, UTF-8/control
   및 body 크기를 검증하고, 전체 mailbox 삭제는 하나의 durable receipt에서만 수행한다.
   `편지보내기`의 다중 행 editor는 이번 경계 밖으로 명시적으로 fail-closed한다.
2. `게시판`/`읽어 게시판 <번호>`/`글삭제 게시판 <번호>`는 `src/board.c`의 닫힌 board ID
   집합(100–116, 120), newest-first 목록, 삭제 tombstone, 작성자/DM 권한, 비작성자 조회수
   증가를 canonical `BoardState`에 연결했다. 게시판 object와 board ID는 현재 방의 canonical
   item에서만 해석하며, `써`의 다중 행 editor와 전체 board object migration은 후속이다.
3. validation workflow를 다시 전수 점검했다. `fast`는 영향 패키지 race만, `integration`은
   전체 Go race/vet/diff만, `main`은 기본 브랜치 ARM64 cross-build를 한 번, `release`만
   DB·browser·x64/Windows/macOS matrix를 실행한다. pre-push는 변경된 migration/stack
   계약만 검사하고 다중 커밋 push에서 remote tip을 한 번만 기준으로 삼는다. 중복 실행을
   유발하는 추가 결함은 발견되지 않았다. 다만 같은 release job 안에서 반복되던 Node
   toolchain 초기화 5회를 2회(각 job 1회)로 줄여 설치·캐시 초기화 시간을 절약했다.

이번 변경에서 `go test -race` 영향 패키지와 통합 gate, 정책·shell·YAML·migration coverage
검사를 통과했다. ARM64 cross-build, 실제 PostgreSQL/browser/compatibility matrix, strict
room corpus 63건, 전체 C command/prefix/key/ANSI parity, full board/mail editor, NPC full
cadence, IME/mobile 실기기, WSS/Ingress와 testnet 배포는 cadence 정책에 따라 반복하지 않았고
아직 전체 인수 조건으로 남아 있다.

## 2026-09-09 직접 관리 병렬 후속: 듣기거부·훔쳐

이번 batch는 기능 파일을 분리한 두 Luna max 레인을 병렬 처리한 뒤 메인 세션에서
parser·connector를 통합했다.

- `듣기거부`는 `first_ignore`를 connection-local `IgnoreList`로 유지한다. target 추가
  시 authoritative online exact identity와 PDMINV를 확인하고, 직접 메시지 경로는 대상
  connection의 목록을 receipt 전에 검사해 차단한다. 목록은 world state·receipt·DB에
  저장하지 않는다.
- `훔쳐`는 canonical NPC/player identity와 `ItemCollection` root subtree만 대상으로
  한다. 권한·5초 cooldown·stealth reveal·시야/정렬/안전방/blind·quest/ONEWEV·RNG와
  실패 적대화, player-kill timer를 하나의 snapshot-bound receipt로 처리한다.
- 기능 레인 검증은 영향 패키지 race와 조립 후 `integration`까지로 제한한다. ARM64는
  `main`에서 한 번, PostgreSQL·브라우저·호환성 matrix는 `release`에서 한 번만 실행한다.
  이번 batch에서 해당 고비용 검증을 반복하지 않았으며, strict room corpus 63건·전체
  C prefix/occurrence/ANSI parity·NPC full cadence·IME/mobile·WSS/Ingress·testnet 배포는
별도 인수 조건으로 남긴다.

## 2026-09-09 직접 관리 병렬 후속: 기습·물약·주문

현재 G3 기능 포팅은 파일 소유권이 겹치지 않는 세 Luna max 레인으로 분할한다.

1. **기습 레인** — `command7.c:backstab`의 canonical NPC/player 선택, 권한·무기·쿨다운·
   stealth·보호 게이트와 비치명 damage/proficiency를 world proposal/apply와 durable
   receipt로 구현한다. lethal 결과는 `PlanPlayerDeath`/NPC death와 동일 트랜잭션으로 조합되기
   전까지 거부한다.
2. **물약 레인** — `magic1.c:drink`의 POTION root와 ready fallback, charge/subtree 수명,
   self-target 상태효과 및 OSPECI 1–6을 구현한다. 정확한 상태/반환 계약이 없는 공격·대상
   지정 주문과 restore 부분 성공은 소비 없이 fail-closed한다.
3. **주문 전수 레인** — `magic1.c:teach`의 교사 class/주문 level, same-room online player
   key prefix·occurrence, visibility와 spell bit mutation을 구현한다. NPC fallback과
   미확인 spell catalog는 거부한다.

메인 통합은 `command_parser.go`, `world_connector.go`, 신규 event adapter에서만 수행하며,
각 receipt의 actor response와 observer/target event를 분리한다. 레인별 targeted race와
`go vet` 뒤 조립 batch에서 `scripts/run-go-validation.sh fast` 및 `integration`을 한 번씩
실행한다. ARM64는 기본 브랜치의 `main` scope에서 한 번, 실제 PostgreSQL·브라우저·호환성
matrix는 `release` scope에서 한 번만 실행한다. strict room corpus 63건·전체 C prefix/key/
ANSI parity·NPC full cadence·IME/mobile 실기기·WSS/Ingress·testnet 배포는 별도 승격 조건이다.

코드 커밋 `c194824`에서 세 레인의 world/session 구현과 parser/connector 통합을 완료했다.
검증은 영향 패키지 race, focused transport 회귀, `fast`, `integration`, `vet`, diff check가
통과했다. 사용자 소유 `src/frp.new`는 계속 보존한다.

## 2026-09-09 G3 전투 후속: 교란·맹공·혈도봉쇄

파일 소유권이 겹치지 않는 세 Luna max 레인을 병렬 실행하고, 메인 세션에서 공용 parser와
live connector만 직렬 조립한다.

1. `교란`은 `command8.c:circle`의 same-room canonical identity와 NPC 우선 순서, 권한/
   PVP·가문전쟁·안전방·시야·stealth·쿨다운·확률·befuddle·적대 경계를 snapshot-bound
   proposal/apply와 receipt로 고정한다. 사망/미해결 관계는 거부한다.
2. `맹공`은 `command8.c:bash`의 권한·무기/내구도·명중·damage dice·befuddle·NPC 적대/
   proficiency·비치명 HP 전이를 고정한다. canonical `die`/`check_for_flee`가 조합되기
   전에는 lethal 결과를 영수증 없이 거부한다.
3. `혈도봉쇄`는 `command7.c:magic_stop`의 NPC-only lookup, visibility·occurrence·reveal·
   cooldown·MUNKIL 순서를 먼저 고정한다. 원본의 적대 추가와 반 HP damage/death/flee
   후속이 아직 canonical State에 없으므로 일반 대상은 `ErrMagicStopCombatSideEffectPending`
   으로 fail-closed하고, connector는 세션을 닫지 않고 unsupported 응답을 반환한다.

이번 조립 커밋은 `c5609e3`이다. 검증은 영향 패키지 race와 전체 Go integration만 수행한다.
ARM64 cross-build는 기본 브랜치 `main`에서 한 번, 실제 PostgreSQL·브라우저·호환성 matrix는
`release`에서 한 번 실행하며 기능 레인에서 반복하지 않는다. strict room corpus 63건, 전체
C prefix/key/ANSI parity, NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet 배포는
여전히 승격 조건이다.

## 2026-09-09 G3 bounded 후속: 물품전달·독살포·적상태

세 개의 독립 Luna max 레인을 파일 소유권으로 병렬 실행한 뒤, 메인 세션에서 공용
parser·`WorldConnector`·room/target projection을 한 번 조립했다. 구현 커밋은
`17c0619`이다.

1. `줘`는 원작의 item/money suffix source 순서를 유지한다. canonical same-room
   player와 `ItemCollection`을 확인해 item subtree 또는 gold를 원자 전이하고,
   target private 응답과 observer event를 분리한다. NPC 수령, legacy inventory,
   보호 quest/event/nested object, capacity/overflow는 영수증 전 fail-closed한다.
2. `독살포`는 ASSASSIN/INVINCIBLE 권한, canonical NPC exact display-name(대소문자 무시),
   visibility·stealth reveal·cooldown·MUNKIL·deterministic RNG와 poison/HP/timer/enemy
   projection을 snapshot-bound receipt로 고정한다. 적대·사망·도주 후속을 현재 상태와
   원자 조합할 수 없으면 RNG/커밋을 만들지 않는다.
3. `상태`는 canonical `room.NPCIDs`의 같은 방 NPC만 읽어 C `display_status`의 15칸
   의미 바와 blindness/visibility를 반환하는 read-only receipt다. player/legacy
   monster fallback, prefix/occurrence 추측, 불능 HPMax는 허용하지 않는다.

검증은 영향 패키지 race와 transport 회귀, `go vet`, `scripts/run-go-validation.sh fast`,
`scripts/run-go-validation.sh integration`, `git diff --check`가 통과했다. ARM64
cross-build는 기본 브랜치 `main`에서 한 번, 실제 PostgreSQL·브라우저·차트 및
x64/Windows/macOS 호환 matrix는 승인된 `release`에서 한 번만 실행한다. 따라서 이
기능 레인에서 해당 고비용 검증을 반복하지 않는다. 전체 C prefix/key/ANSI parity,
strict room corpus 63건, NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet
배포는 여전히 별도 승격 조건이다.

## 2026-09-09 다음 병렬 bounded batch: 방혼술·흡성대법·차기

G3 전투의 독립적인 세 경계를 Luna max 에이전트가 각각 world/session 파일에 구현하고,
메인 세션이 parser·`WorldConnector`·room/target projection만 조립한다.

1. `방혼술`은 cleric/paladin/invincible 권한, canonical same-room NPC occurrence, undead·
   visibility·`MUNKIL`, `LT_TURNS`/`LT_ATTCK`, chance·소멸/반 HP damage와 enemy relation을
   snapshot-bound proposal/apply로 고정한다. death/drop graph가 완전히 조합되기 전 lethal은
   `ErrTurnDeathTransitionPending`으로 거부한다.
2. `흡성대법`은 mage/invincible gate, exact NPC, reveal·cooldown·`MUNKIL`, source chance/
   damage 및 undead MP 소진/HP 흡수·enemy damage를 receipt로 고정한다. unresolved combat,
   death, overflow는 RNG·commit 전에 fail-closed한다.
3. `차기`는 barbarian/invincible 및 NPC/player PVP 안전·war·charm 경계, 무기·명중/damage
   dice·stealth·`LT_KICK`, 비치명 HP/enemy projection을 연결한다. lethal `die` 조합 전에는
   `ErrKickDeathTransitionPending`으로 영수증을 만들지 않는다.

검증 cadence는 레인별 focused race 후 조립 batch에서 `integration` 한 번으로 고정한다.
ARM64 cross-build는 기본 브랜치 `main`에서만, PostgreSQL·브라우저·x64/Windows/macOS는
승인된 `release`에서만 실행한다. 기능 레인에서 고비용 검사를 반복하지 않으며 strict room
corpus 63건, NPC full tick, 전체 C prefix/key/ANSI parity, IME/mobile·WSS/Ingress와 testnet
배포는 이후 승격 조건으로 남긴다.

## 2026-09-09 직접 관리 병렬 후속: 사용·암호·직업전환과 비용 최적화

G3의 세 독립 경계를 Luna max 레인으로 병렬 구현하고, 메인 세션은 공용 parser·connector
통합만 수행한다.

1. `사용`은 `command9.c:use`의 direct root 선택과 OUSEFL/SP_WAR 권한을 유지하고,
   이미 검증된 장비·물약 reducer만 위임한다. 하나의 proposal/apply에서 floor 이동,
   PHIDDN 해제, 소비·장비 변경·event를 함께 저장한다. scroll/wand/key와 미확인
   reducer는 원자 후속이 준비될 때까지 fail-closed한다.
2. `암호`는 `command11.c:passwd`를 account credential boundary로 분리한다. 현재 암호
   검증→새 bcrypt hash 1회 생성→확인→expected hash 조건부 UPDATE를 수행하며, 응답
   유실 재시도는 replacement hash를 확인해 중복 변경을 막는다. 평문은 상태기·receipt·
   로그에 남기지 않는다. 게임 연결에 `암호` continuation을 노출하는 것은 별도 계약으로
   남긴다.
3. `직업전환`은 blind/RTRAIN/class/XP/PFAMIL gate와 C의 RTRAIN+1..+3 fold, XP
   100000 차감 및 `down_level` 효과를 snapshot-bound receipt로 연결한다. bare 명령은
   prompt no-op, `직업전환 예`만 현재 bounded slice에서 commit하며 family roster가
   준비되기 전에는 PFAMIL을 거부한다.

검증 cadence는 다음과 같이 고정한다.

- 기능 레인: 변경 패키지의 `go test -race`만 실행한다.
- 조립 batch: `scripts/run-go-validation.sh integration`(전체 Go race/vet/diff)을 한 번
  실행한다.
- 기본 브랜치 병합: `main` scope에서만 Linux ARM64 cross-build를 한 번 실행한다.
- 명시적 release: PostgreSQL/browser와 x64·Windows·macOS 호환성 matrix만 실행한다.

workflow는 manual dispatch만 가지므로 기능 개발 중 고비용 gate가 자동 반복되지 않는다.
migration 2회 적용과 command replay는 재실행 안전성의 의도된 증거라 유지한다. 이번
batch에서는 strict room corpus 63건·실제 PG·브라우저/IME·ARM64 main·release matrix를
반복하지 않는다. 전체 C prefix/key/ANSI parity, NPC full tick, WSS/Ingress와 testnet
배포는 승격 후속이다.

## 2026-09-09 터미널 암호·게이트웨이 좌표 후속과 검증 비용 감사

`암호`의 account-only continuation을 `WorldConnector`에 연결할 때도 게임 상태와
credential 상태의 경계를 분리한다. `storage.Character.Name`은 계정의 canonical name을
담지만 world/player ID를 대체하지 않으며, `PasswordStore`만 credential hash를 조회·조건부
갱신한다. WebSocket은 다음 입력의 `secret` 여부만 xterm에 전달하고, 암호 line은
history/alias/receipt/event/log에 저장하지 않는다. hash 생성은 한 번, 응답 유실 시 같은
replacement hash 재시도만 허용한다. 실제 PG 계정 변경은 별도 opt-in 통합 증거 없이는
완료로 승격하지 않는다.

웹 루트의 연결 좌표는 `MUD_GO_GATEWAY_URL` 우선, `MUD_GATEWAY_URL` fallback으로 고정한다.
주소 검증은 ws/wss scheme, HTTPS mixed-content와 malformed/missing 입력을 순수 helper로
판정해 빌드 시점 환경값을 굽지 않는다. 이 경로에는 웹 가입이나 Supabase Auth가 필수
단계로 들어오지 않는다.

검증 호출 그래프 감사 결과는 다음으로 고정한다.

- 병렬 Luna max 레인: 담당 파일 gofmt와 영향 패키지 targeted race만 실행한다.
- 조립 batch: `scripts/run-go-validation.sh integration`을 한 번 실행해 전체 Go
  race/vet/diff를 확인한다. 이미 이 gate를 통과한 batch에서 fast를 다시 호출하지 않는다.
- 기본 브랜치 병합: `scripts/run-go-validation.sh main`에서만 Linux ARM64 cross-build를
  한 번 실행한다. `integration`과 기능 레인에는 ARM64 build가 없다.
- 승인된 기본 브랜치 release: PostgreSQL·browser·x64/Windows/macOS 호환성 matrix를
  한 번 실행한다. migration 2회 적용과 command replay는 재실행 안전성 증거이므로
  중복으로 분류해 제거하지 않는다.

이번 후속에서는 위 경계를 벗어난 ARM64/PG/browser/release 검사를 반복하지 않는다.
