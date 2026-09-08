# Go 게임 서버 전환 실행 계획

결정일: 2026-09-08 · 상태: 새 목표 활성화, G1 월드 읽기/표시 및 G2 가입 초안 구현 중

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

모듈 생성 이후 기본 로컬 검증은 `cd server && go test ./...`와 `go test -race ./...`다. 현재 존재하거나 통과한 명령으로 보고하지 않는다. 통합 테스트는 기존 Docker 구성을 조사해 고유 Compose 프로젝트 이름·임시 볼륨·충돌 없는 포트를 적용한다. 공유 컨테이너/캐시 일괄 삭제는 금지한다.

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
