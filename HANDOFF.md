# Muhan MUD 포팅 핸드오프

## 현재 로컬 체크포인트 — 2026-09-08

작업이 초기화된 것이 아니라, 애플리케이션과 인프라의 로컬 브랜치가 원격보다
앞선 상태다. 애플리케이션 기능 기준 체크포인트는
`aecdb60` (`test: cover canonical inspection replay`)이며 기능 구현 기준은
`288c8d2` (`feat: port canonical search and inspection targets`)이다. 이전 settings 기능
기준은 `2b82e32`이며, 인프라의 검토된 소스 고정 커밋은 `21863341`로 이 기능
체크포인트를 가리킨다. door hardening은 root `25beba8`, infra pin은 `2f15179a`다.
최신 key-door source pin은 infra `32ecdd3a`이며 root `7671582`를 가리킨다. canonical
search/inspection source pin은 다음 infra 커밋에서 root `aecdb60`로 갱신한다. 이전
source pin은 `21863341`이었다. 원격에는 아직 push하지
않았으므로 새 clone이나 다른 에이전트가 이 로컬 진행분을 보지 못하는 것이 정상이다.
사용자 소유의 `src/frp.new` 변경은 계속 dirty로 보존한다.

현재 Go 수직 슬라이스에는 `환영`, `도움말`, `외쳐`, `검색`/`찾아`, `추적`,
`숨겨`/`숨어`, `엿봐 <대상>`, `설정`/`해제`, 제한된 원작 감정표현 alias, 자유 문장 `표현`, 같은 방의
정확한 대상에 대한 `보아`, `열어`/`닫아`, `풀어`/`잠궈`/`따`가 포함된다. 각 명령은
parser→world 계획→PostgreSQL receipt/replay→WebSocket room event 경계를 가지며,
unit/race/vet, ARM64 PostgreSQL 17 회귀,
Linux ARM64 cross-build와 실제 Go+PG+Chromium E2E를 통과했다. 객체/출구 stealth,
전체 C 명령 parity, tick/경제/배포 인수는 남아 있다. 이는 전체 C 명령 인수 완료가
아니라 다음 포팅을 이어갈 수 있는 보존된 체크포인트다.

## 최신 병렬 포팅 증거 — 2026-09-08 search/inspection

Luna max 병렬 wave에서 `검색`/`찾아`의 canonical secret-exit·room-object branch와
`보아`의 canonical floor-root·exact-exit branch를 파일 소유권을 분리해 구현했다.
root `288c8d2`와 replay 회귀 `aecdb60`에 반영됐으며, 기존 player/NPC target 및
search RNG 순서를 유지한다. prefix/occurrence·legacy linked-list·full ANSI formatting은
계약 밖으로 남겼다.

검증: 전체 Go race/vet, Linux ARM64 cross-build와 targeted look/search 회귀 통과.
이번 턴에는 `postgres:17-alpine` 이미지가 없어 새 PG 컨테이너를 만들지 않았으므로
canonical object/exit PG replay는 미검증 상태다. `src/frp.new`는 여전히 사용자 dirty
변경으로 보존한다.

## 현재 인계 기준 — 2026-09-08 Go 전환

사용자 결정에 따라 **Go 게임 서버 + self-hosted Supabase**로 전환한다.
[Go 실행 계획](docs/porting-research/go-server-execution-plan.md)과 루트
[작업 지침](AGENTS.md)이 현재 기준이다. C→DB 확장과 Rust 런타임 개발은 중단한다.
C/Rust는 비교 테스트·이관 참고 자산으로 보존한다. Go에서 터미널 가입·로그인,
캐릭터 생성 초안의 PostgreSQL 저장과 재로그인, WebSocket 및 xterm 연결을 구현했다.
추가로 실제 Go 실행 파일(-race)의 터미널 가입→월드 입장→보기→SIGTERM→재시작→
재로그인과 상태 보존을 격리 PG17에서 검증했다. 실행 경로는 명시적 -world/
-templates/-game-hour 옵션이며 배포하지 않았다. `-player-tick` durable phase와 위
수직 명령들은 결정론적 slot/receipt로 저장하고, 불확실한 저장 결과는 같은 요청으로
재시도하며 종료 전에 worker를 멈추도록 연결했다. 전체 C 명령 parity, 지속 게임 시계와
full tick scheduler(NPC/room spawn·combat round는 여전히 별도), 이관 예외 63개,
실제 브라우저/모바일 및 비정상 종료 인수는 남아 있다.
최신 테스트와 제한은 server/README.md의 마지막 기록을 따른다.

## 최신 작업 증거 — 2026-09-08 key-door slice

root `7671582`에서 원본 `command6.c`의 제한된 `풀어`/`잠궈`/`따` 경계를 Go로
연결했다. 열쇠 object type·key 번호·내구도·잠금/닫힘 순서, 도둑/무적 권한,
blind·`XLOCKD`, `LT_PICKL` 10초 cooldown, deterministic picklock RNG, unlock 시
열쇠 사용 횟수 차감, actor `PHIDDN`과 room event 순서를 world proposal/apply와
durable receipt/replay로 보존했다. `풀어`/`잠궈` 성공 event와 `따` 시도/성공 event는
최초 commit에서만 다른 연결에 fan-out한다.

검증은 전체 Go race/vet, Linux ARM64 cross-build, 격리 ARM64 PostgreSQL 17
`TestPostgresDoorKeyCommandPersistsAndReplays`, 실제 Go+PostgreSQL+Chromium의
`따 __missing_door__` 권한 경계 **1 passed (11.3s)**로 완료했다. 인프라 root
source-path 검증은 infra `32ecdd3a`에서 2/2 통과하며 root `7671582`를 고정한다.
두 저장소 모두 아직 push/deploy하지 않았다. key lookup은 현재 canonical/legacy
root inventory의 exact case-insensitive 이름으로 제한하며 nested/equipped occurrence,
전체 C handler/ANSI parity, NPC/tick·경제와 testnet 운영 인수는 계속 남아 있다.

추가 사용자 결정: 웹 가입/로그인 화면 없이 새롬 데이터맨 느낌의 중앙 xterm에서
원작의 캐릭터 생성·이름/비밀번호 로그인을 제공한다. Supabase 웹 계정 연결은
필수 조건에서 제외한다. [터미널 전용 UI 계획](docs/web-mud/terminal-only-ui-plan.md)의
입력 포커스·모바일·한글 입력 인수 기준까지 구현 범위에 포함한다.

## 이전 인계 기록 (역사적 체크포인트)

Go 서버·터미널 UI·실행 계획은 로컬 커밋
`518502bd4c86a5ab73d6bce962004ddd3ec07188` (`Go 서버 전환 기반과 터미널 런타임 정리`)로
묶었다. 인프라 저장소의 Go/ARM64 Helm·Docker·migration/seed 경로도 로컬 커밋
`ba9a9c14` (`Go 런타임과 ARM64 Helm 경로 연결`)로 묶었다. 두 커밋 모두 아직 원격에
push하지 않았기 때문에 GitHub의 기존 브랜치나 immutable Docker fetch에서는 Go 서버가
보이지 않는다. `src/frp.new`는 사용자 변경으로 계속 dirty 상태이며 stage하거나
되돌리지 않았다.

당시 최신 애플리케이션 로컬 증거는 `e9599edf7801059dfd2bee0d50b809b8f7bae86b`
(`정보` read-only 첫 페이지와 중앙 xterm 브라우저 경계 포함), 인프라는
`d9f2f74dbc86fd887f7f8a8c9afe0071b93df69f` (immutable Docker source gate 갱신)다.
당시 애플리케이션 브랜치는 private 원격보다 317개, 인프라 브랜치는 origin보다 14개
앞서 있었으므로 원격 clone/새 세션에서 과거 상태처럼 보이는 것이 정상이다. 이 로컬 커밋들은
사용자 승인 전까지 push·배포하지 않는다.

현재 인프라 로컬 검증은 Go/legacy Helm lint·render, migration graph 47개, Secret·release
wrapper·Docker source-path를 포함한 72개 Node 테스트, ARM64 BuildKit Dockerfile check를
통과했다. Go는 `go test ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go test -race`와
`go vet`를 통과했고, web typecheck/test/build 및 `pnpm test:browser`의 중앙 xterm 2개
case도 통과했다. 최신 Go PostgreSQL 브라우저 E2E와 `표현`/`외쳐` 흐름도 통과했다. 다만 `private` 원격 브랜치는 아직 `a45ec8e...`이고 Go 커밋을
포함하지 않으므로 실제 Docker registry build/deploy는 push와 immutable source 승인 뒤에만
재현할 수 있다. 다음 인계 시 먼저 `git status --short`와 이 문서의 최신 추가 기록을
확인한다.

사용자가 이전 목표를 삭제한 뒤 새 Go 목표 생성과 실행을 지시했다. 현재 원본 방
데이터를 Go로 읽는 G1 작업 중이다. 엄격한 디코더는 3,216개 중 3,153개만 허용한다.
별도 `InspectLegacyRoom`은 3,216개 모두 구조를 읽고 원본 전체/해시/소비 위치를
보존한다. 63개 방에서 문자 변환 80건, 고정 문자열 종료 누락 13건, 추가 데이터
7건을 보고한다. 이 조사 결과는 게임 데이터 승인이나 수정 완료가 아니다.
엄격한 전체 corpus 인수 테스트는 여전히 실패한다. 테스트를 건너뛰거나 원본을
삭제하지 말고 명시적 변환/검토 정책을 이어서 구현한다.
Docker는 이후 실제 PostgreSQL 17 격리 테스트로 확인했다. 이번 Go 작업에서
CI·push·배포는 실행하지 않았다. 최신 증거는 `server/README.md`와
`docs/porting-research/go-browser-validation-20260908.md`를 참조한다.

Go 추가 진행: `server/internal/world/transfer.go`는 이동·양쪽 방 재실자·입장
갱신을 단일 후보로 구성한다. `engine.Execute`와 PostgreSQL 상태/영수증 저장을
통해 이 후보의 저장·동일 명령 재생을 격리 PG17에서 확인했다. 실제 게임 세션과
연결된 완성형 이동 명령은 아니다. 다음에는 권위 있는 플레이어/월드 상태 모델과
명령 루프에 연결하고 NPC 활성·추종·함정·사망 등 남은 원작 동작을 구현한다.
63개 방의 엄격한 이관 실패는 그대로 남아 있다. 최신 상세 증거는 server/README.md.

이어서 `world.State`/`ApplyTransfer`를 추가했다. 방은 플레이어 ID만 참조하고
플레이어 속성은 단일 맵에서 관리하며 위치/접속/방 소속의 무결성을 검사한다.
engine PG17 테스트도 이 실제 타입의 decode→이동→적용→저장/재생으로 전환했다.
장비·관계·NPC ID·재시작 시 Online 재조정은 미구현이며 운영 입장 승인 타입이 아니다.

추가: `State.RecoverOffline`로 콜드 스타트 시 Online/재실자만 초기화하는 순수
변경안과 PG 저장/중복 요청 테스트를 구현했다. 실제 시작 경로에는 아직 미연결이다.
독점 소유권·세션 세대·시작 시 복구 커밋 후 접속 허용 절차를 이어서 구현해야 한다.

G1 후속 최신 상태: `PlanArrivalTrap`/`ApplyArrivalTrap`가 C `check_traps`의
도착 후 회피·독화살·낙석·MP 피해·구덩이 trapexit 재배치·주문 타이머 제거·장비
손실 표식을 순수 계획과 원자적 상태 적용으로 분리했다. 성공한 방향 이동에만
arrival trap proposal이 붙고 거절/경비/낙하 정지에는 붙지 않는다. 구덩이 치명상은
기존 `PlanPlayerDeath`를 재사용하고, 경보 NPC 이동과 플레이어 추종자 재귀는 아직
미완료다. 집중 race/vet와 trap 회귀가 통과했으며 전체 world corpus의 63개 원본
데이터 예외는 계속 실패 증거로 남아 있다. 격리 ARM64 PostgreSQL 17에서
`TestPostgresDirectionalArrivalTrapPersistsAndReplays`를 `-race`로 통과시켜
HP/독 표식 저장과 동일 command ID 재생 무재실행도 확인했다.
같은 격리 컨테이너에서 `TestPostgresDirectionalFollowerPersistsAndReplays`도
통과해 leader/follower 관계·양쪽 방 소속과 replay 무재실행을 확인했다.

추종 후속도 추가했다. `PlayerState`에 leader ID와 C `first_fol` head-insertion 순서를
보존하고, 방향 이동 시 리더 commit 후 destination occupancy를 기준으로 follower를
재판정한다. nested follower는 depth-first, 각 follower 함정은 자식 처리 뒤, 리더
함정은 전체 follower 뒤에 적용한다. `RONEPL` capacity 재평가, self/cycle/비상호
관계 거절, 퇴장·cold recovery 관계 정리 회귀를 통과했다. follower별 방송과
command6의 별도 MDMFOL monster-follower 경로는 여전히 미완료다. `go test -race
./internal/world -skip '^TestRoomBodyCorpus$'` 전체 회귀와 session/transport/cmd
집중 검증이 통과했으며, corpus 포함 전체 world 실행의 63개 원본 예외는 계속 기록한다.

2026-09-08 G1 NPC 추적/경보 후속: `NPCPermanentOrigin(room,slot)`을 canonical NPC에
추가하고 `PlanNPCFollowerChase`/`ApplyNPCFollowerChase`를 구현했다. C command2의
source `NPCIDs` 순서, first enemy canonical player ID, MFOLLO/MDMFOL·PINVIS/PDMINV·
MDINVI visibility predicate, `1..50`/`15-dex+npcDex` 경계, MPERMT의 due origin timer
선갱신·이동·ActiveNPC prepend·MPERMT 해제를 clone-commit으로 고정했다. `DirectionalStep`
이 리더/재귀 follower 뒤, arrival trap 앞에서 이 chase를 같은 receipt에 포함한다.
영구 origin·enemy·active order·RNG가 미확정이면 `State{}`로 폐기한다.

`TRAP_ALARM`은 `ApplyArrivalAlarmWithCatalog`에서 C의 trapexit due 영구 NPC
catalog/allocator 생성을 먼저 원자 후보에 포함한 뒤 경보 발생 방으로 이동하고
MAGGRE/MPERMT/timer/active order를 적용한다. catalog/allocator가 필요한데 없으면
조용히 누락하지 않고 fail-closed로 거절한다. actor-local 경보 문구는 receipt 응답에
포함하며, `WorldConnector`의 committed movement room-event hub와 slow-client drop
정책도 연결했다. 원작의 모든 분기별 broadcast/room 메시지 formatting과 일반
room-entry/tick spawn orchestration은 아직 미완료다.

추가로 `NPCFollowerIDs`/`FollowingPlayerID`, `FollowNPCToPlayer`와
`PlanNPCMonsterFollowers`/`ApplyNPCMonsterFollowers`를 구현했다. `FollowerRefs`가
해결된 snapshot에서는 player와 monster가 섞인 C first_fol 단일 링크 순서를 재귀
순회하고, command6의 MDMFOL monster follower를 그 위치에서 이동해 ActiveNPC prepend·
MPERMT clear를 적용하며 `die_perm_crt` timer는 건드리지 않는다. DirectionalStep의
같은 receipt와 PG replay에 연결했고 logout/cold recovery 관계 정리도 포함했다.
follower별 network broadcast/room 메시지는 여전히 남은 출력 경계다.

검증: `go test -race ./internal/world -skip '^TestRoomBodyCorpus$'`,
`go test -race ./internal/session ./internal/transport ./cmd/...`, 관련 `go vet` 통과.
격리 `linux/aarch64` PostgreSQL 17에서 기존 방향/함정/follower와 신규
`TestPostgresDirectionalNPCChasePersistsAndReplays`/`TestPostgresDirectionalAlarmPersistsAndReplays`/
`TestPostgresDirectionalAlarmSpawnsDueNPCPersistsAndReplays`/
`TestPostgresDirectionalMDMFOLPersistsAndReplays`와 mixed first_fol 순서 회귀를 receipt
replay·무재실행 RNG까지 통과시켰고, room-event 순서/WebSocket async event 회귀도
통과했다. 전용 컨테이너는 정리했다. 전체 world corpus의
원본 방 63개 예외, 일반 room-entry/tick spawn orchestration, 전투 tick, 원작 전체 follower별 네트워크 출력,
full `die_perm_crt` quest/XP/summon/death-description side effects, 나머지 명령 및
실제 브라우저/배포 E2E는 남아 있다.

아이템 추가 진행: PlayerState.Items에 고유 ID 맵/컨테이너/인벤토리/20개 장비
슬롯을 저장하도록 구현했고, PG17의 이동·복구·재입장 후 ID와 소유 관계 보존을
검증했다. ImportItems/RestoreEquipment는 명시적 이관용이며 로그인마다 재실행하지
않는다. AC와 THAC0 계산은 원본 C differential로 검증했지만 아직 PlayerState에
반영하는 실행 경로와 실제 아이템 명령은 미연결이다.

추가로 canonical Items가 있는 EnterSavedPlayer에서 AC/THAC0 재계산을 연결하고
PG17에 장비 ID/슬롯과 함께 저장되는 것을 검증했다. 아직 WebSocket 로그인 완료
경로에는 연결하지 않았으며 update_ply/전체 초기화/실제 아이템 명령은 남아 있다.

2026-09-08 플레이어 tick 수직 슬라이스: `State.PlayerUpdateOrder`가 room membership
순서를 기준으로 deterministic한 비-DM 온라인 플레이어 phase 입력을 만들고,
`WorldConnector.RunPlayerVitalPhase`가 이를 `engine.Execute`/PostgreSQL receipt로
묶었다. 기존 `UpdatePlayer`의 effect 만료·HP/MP vitals·inline death continuation·
light tick을 한 후보로 저장하며, 같은 command ID 재시도는 reducer/RNG를 재실행하지
않는다. 실제 `linux/aarch64` PostgreSQL 17 컨테이너에서
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`를 `-race`로 통과시켰다.
이는 전체 legacy `update.c` scheduler가 아니다. NPC/room spawn·combat round·전체
broadcast와 브라우저 tick loop는 여전히 남아 있으며, 메서드 이름과 문서도 이 제한을
명시한다. 이번 작업으로 추가된 Go 파일은 아직 untracked이므로 커밋 전에는 다른
워크트리/GitHub에서 보이지 않는다.

같은 날 전투의 첫 수직 slice도 연결했다. `공격`/`공`/`쳐`/`때려 <NPC 이름>`은
canonical room NPC ID를 exact name으로 선택하고 C의 THAC0/방어력 hit gate, 주사위
피해·치명타 판정, 공격자의 은신 해제와 NPC enemy relation을 하나의 durable receipt에
저장한다. lethal 결과도 같은 후보에서 NPC ID/room·active/enemy/follower 관계를 제거하고,
XP/성향·quest bit/proficiency, 금화·legacy inventory의 canonical floor graph, MPERMT
origin timer를 원자적으로 처리한다. allocator가 없거나 summon 생성이 필요한 경우에는
fail-closed하여 부분 사망을 저장하지 않는다. 로컬 회귀와 격리 `linux/aarch64`
PostgreSQL 17의 `TestPostgresAttackCommandPersistsAndReplays`,
`TestPostgresLethalAttackCommandPersistsNPCDeath`, `TestPostgresLethalAttackCommandPersistsNPCDrops`를 통과했다. player-vs-player,
도망·summon/death-description broadcast와 전체 전투 tick은 아직 남아 있다.

2026-09-08 NPC attack weapon/multi-swing slice: command5의 `shotscur < 1` 선행 경계, 치명타
파괴(`OALCRT`/`ONSHAT`/`ONEWEV`), 일반 명중 시 숙련도 기반 드롭, 명중 후
`mrand(0,3)` 내구도 감소를 원래 RNG 순서로 구현했다. canonical ready/inventory
ID graph를 원자 후보에서 갱신하고 파괴 시 nested subtree를 함께 제거하며, NPC
피해 비례 proficiency award와 터미널 부서짐/드롭 응답을 저장한다. 로컬 TDD 및
`TestExecuteAttackLineCommitsWeaponDropAndReplays`, ARM64 PostgreSQL 17
`TestPostgresAttackCommandPersistsWeaponDrop`가 동일 command ID replay에서 난수·
장비 mutation을 재실행하지 않음을 확인했다. 전체 room corpus의 기존 63개 예외,
PVP/도망, summon 생성·death description broadcast와 전체 NPC 전투 tick은
여전히 남아 있다. 같은 slice에서 PUPDMG 추가 swing 수와 lethal/무기 소진 시 loop
중단을 연결했고, `TestPlanNPCMeleeAttackPowerDamageAddsDeterministicExtraSwing`,
`TestPlanNPCMeleeAttackPowerDamageStopsAfterLethalSwing`, ARM64 PostgreSQL 17
`TestPostgresAttackCommandPersistsPowerDamageSequence`가 총 피해·적대 damage 저장과
동일 command ID replay 무재실행을 확인했다. PVP/도망, summon 생성·death description
broadcast와 전체 NPC 전투 tick은 여전히 남아 있다.

명령 루프에는 원작 `건강`/`점수`의 numeric status receipt도 연결했다. canonical body와
equipment에서 HP/MP/방어력/경험치 목표/돈을 계산하고 blind 상태와 동일 명령 replay를
테스트한다. 이 역시 전체 명령 parser나 ANSI/title formatting 인수는 아니다.

`따라 <플레이어>`와 `내보내`도 `FollowPlayer`/`UnfollowPlayer`의 same-room/exact-name/
reciprocal first_fol 상태 전이를 실제 `ExecuteFollowLine` receipt에 연결했다. `내보내`는
인자 없이 자신이 따르던 대상을 그만 따르는 C `lose` 경로와, 플레이어 이름을 지정해
자기 follower를 내보내는 경로를 모두 테스트하며 replay한다. NPC follower 명령과 원작의
전체 명령 약어/occurrence parser는 남아 있다. 이번 변경 뒤
`go test -race ./... -skip '^TestRoomBodyCorpus$'`, `go vet ./...`, `git diff --check`가
통과했고, 전용 ARM64 PostgreSQL 17 컨테이너에서
`TestPostgresFollowAndLoseCommandPersistsAndReplays`의 두 command receipt 저장·재생도
통과했다. 컨테이너는 테스트 후 정리했다.

읽기 명령 slice도 추가했다. `소지품`과 `장비`/`장`을 canonical item ID·20개 ready
슬롯 순서로 렌더링하고 C의 blind/invisible 경계를 적용한 `ExecuteItemsLine`을
transport dispatch와 durable receipt에 연결했다. `TestPostgresItemsCommandPersistsAndReplays`가
전용 ARM64 PostgreSQL 17에서 inventory/equipment 응답과 replay를 확인했으며, 전체
명령 parser·get/drop/wear/remove/hold/ready mutation은 아직 남아 있다.

2026-09-08 G1 room admission/catalog slice: 엄격한 `DecodeLegacyRoom`은 malformed
text/trailing bytes를 계속 거절한다. `AdmitLegacyRoom`과
`LegacyRoomAdmissionPolicy`를 별도 compatibility 경계로 추가해 invalid EUC-KR,
bounded fixed-text NUL 누락, trailing data, C `load_rom`의 path-ID 우선 mismatch를
각각 명시적으로 허용할 때만 입장시킨다. 잘림·invalid count·depth/size 초과는 어떤
정책에서도 fail-closed하며, `LegacyInspection`의 원본 bytes/SHA-256/issue offset은
그대로 보존한다.

`LoadLegacyRoomCatalog`은 C path `rooms/r%02d/r%05d`만 canonical으로 읽는다. 현재
3,216개 파일 중 canonical 2,341개를 catalog에 넣고, 경로가 맞지 않는 875개 역사적
artifact는 `Ignored()`에 deterministic하게 기록한다. `r09/r09000`은 raw header ID가
0이어도 C처럼 path ID 9000을 runtime identity로 적용하고 mismatch evidence를 남긴다.
`LegacyRoomCatalog.NewState`는 빈 플레이어의 `world.State`를 검증 가능하게 만들지만
legacy monsters/objects의 canonical NPC/item graph 이관이나 PG seed는 아직 다음
작업이다. 관련 TDD는 `TestAdmitLegacyRoomRequiresExplicitPolicyForNoncanonicalText`,
`TestAdmitLegacyRoomPreservesSourceEvidence`,
`TestAdmitLegacyRoomNeverRelaxesStructuralValidation`,
`TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts`다. 기존 엄격한
`TestRoomBodyCorpus`의 63개 실패 증거는 의도적으로 유지한다.

`engine.SeedWorldFromCatalog`와 CLI `-seed-world <id> -seed-rooms <rooms-dir>`도 연결했다.
`-migrate` 이후 명시적으로 실행하면 catalog의 빈 플레이어 snapshot을
`mud_go.worlds`에 최초 생성하며, 기존 world를 덮어쓰지 않는다. ARM64 PostgreSQL 17의
`TestPostgresSeedLegacyRoomCatalog`와 실제 CLI 실행이 2,341개 room 저장·재로드까지
통과했다. legacy monster/object의 canonical NPC/item ID graph 변환, PG room graph
참조 검증, 실제 player admission은 이 seed 경계 뒤의 다음 작업이다.

`말`/따옴표 say 명령도 추가했다. `PlanSay`는 C의 침묵·local echo·말할 때 은신 해제를
원자 후보로 만들고, `ExecuteSayLine` receipt commit 뒤 같은 방의 다른 연결에만
비동기 room event를 fan-out한다. 침묵/빈 입력은 broadcast하지 않으며 receipt replay는
재방송하지 않는다. `TestPostgresSayCommandPersistsAndReplays`와 room event/transport
회귀가 ARM64 PostgreSQL 17 및 전체 race/vet에서 통과했다. 전체 broadcast formatting,
대화/DM/나머지 명령 parser와 persistent tick은 아직 남아 있다.

`누구`/`그룹` read-only 명령도 추가했다. `PlayerWho`는 room membership 우선의
deterministic 온라인 목록과 blind/invisible/detect 경계를 사용하고, `PlayerGroup`은
canonical mixed `first_fol` 순서로 player/NPC follower의 HP/MP를 출력한다.
`ExecuteSocialLine` transport와 receipt/replay를 연결하고
`TestPostgresSocialCommandPersistsAndReplays`를 ARM64 PostgreSQL 17에서 통과시켰다.
descriptor-order/ANSI title의 완전한 C 출력 동등성 및 group mutation은 아직 남아 있다.

아이템 이동의 첫 mutation도 추가했다. `주워`/`주`/`가져`/`꺼내`와 `버려`/`넣어`가
canonical floor/player root 사이에서 nested subtree 전체를 ID 그대로 이동하며,
blind/invisible 및 equipped-root 경계를 fail-closed로 적용한다. `ExecuteItemMutationLine`
transport와 durable receipt/replay를 연결했고 `TestPostgresItemMutationCommandPersistsAndReplays`를
ARM64 PostgreSQL 17에서 통과시켰다. 모두 줍기, guard/weight/capacity 규칙 및
wear/remove/hold/ready mutation은 아직 남아 있다.

최신 장비 mutation slice: `ExecuteEquipmentLine`이 원작 alias인 `입어`, `쥐어`, `무장`,
`벗어`를 canonical inventory root와 C MAXWEAR 20개 ready slot 사이의 하나의 durable
receipt로 연결했다. `ReadyItem`은 목/손가락의 첫 빈 슬롯, 파손·저주·슬롯 충돌·기본
직업/성향/크기/무기 제한을 fail-closed로 검증하고 OWEARS/OWHELD 플래그 및 AC/THAC0를
후보 안에서 갱신한다. `RemoveItem`은 cursed item을 보존하고 나머지는 C식 이름/보정치
순서로 inventory에 되돌린다. local unit/race/transport와 ARM64 PostgreSQL 17의
`TestPostgresEquipmentCommandPersistsAndReplays`가 동일 command ID replay까지 통과했다.
전체 C의 class/race/quest/event/charge 예외, `모두` 일괄 처리, 장비 사용 효과 및
전체 command parser는 아직 미완료다. 이번 변경 뒤 `go test -race ./... -skip
'^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, `git diff --check`가 통과했고,
전용 PostgreSQL 컨테이너는 테스트 후 정리했다.

아이템 이동 후속: `주워 모두`/`버려 모두`를 canonical floor/player root batch receipt로
연결했다. `ItemCollection.Weight`/`CapacityCount`는 C `weight_obj`/`weight_ply`와
`count_inv(...,-1)` 경계를 보존하고, get 경로는 blind/invisible·경비·ONOTAK/OSCENE·
무게·소지 수·gold/quest 미구현 경계를 fail-closed로 적용한다. drop 경로는 일반
플레이어의 quest/event root 및 nested child를 보호하고 DM만 통과시킨다. 단일 명령과
batch의 unit/race/transport 및 ARM64 PostgreSQL 17 replay가 통과했다. 이어서
`꺼내 <가방> <물건>`/`넣어 <물건> <가방>` direct child 이동도 canonical ID·nested
소유권·container counter를 유지하며 연결했다. gold/quest 보상, 제물 방·은행·상점
연동은 아직 남아 있다.

은행 후속: `잔액`/`입금`/`출금`과 `보관물`/`받아`를 C `RBANK` 방에서만 허용하고,
3억냥 상한·`모두` 해석·player gold/은행 잔액 원자 갱신 및 canonical object-root
graph 이동을 durable receipt로 연결했다. 로컬 unit/session/transport와 ARM64
PostgreSQL 17 `TestPostgresBankMoneyCommandPersistsAndReplays`/
`TestPostgresBankItemCommandPersistsAndReplays`의 입금·아이템 보관·동일 command ID
replay·출금을 통과했다. 기존 bank graph 전체 이관, 상점·거래는 아직 미구현이다.

세션 종료 후속: `끝`을 explicit quit receipt로 연결하고 WebSocket `Closed` 표시 뒤
기존 `Depart`/cleanup queue가 실행되도록 했다. 정상 종료와 소켓 단절은 같은 durable
departure 경계를 공유한다. 전체 quit alias·자동저장·재접속 UX는 아직 미구현이다.

`시간` read-only 명령도 중앙 parser와 `WorldConnector`에 연결했다. C `prt_time`의
게임 시각(주/야간 표시와 12시간 변환) 및 PST wall-clock을 request에 고정하고,
world snapshot bytes는 그대로 반환하는 durable receipt로 처리한다. 같은 command ID의
재시도는 새 시각을 렌더링하거나 reducer를 다시 실행하지 않으며, unsupported
`도움말`/인자 포함 시간 명령은 receipt 없이 fail-closed한다. `WallClock` 주입점을 둬
transport 테스트가 실제 시계에 의존하지 않도록 했고, local unit/race/vet와
`TestWorldConnectorSubmitDispatchesReadTimeWithoutMutatingWorld`를 통과했다. C의
continuation 기반 도움말/정보 명령과 전체 출력 포맷은 아직 남아 있다.

후속 `정보` slice는 C `command4.c:info`의 첫 페이지 통계를 `PlayerInfo` pure
projection과 `ExecuteInfoLine` receipt로 연결했다. canonical Items와 정의된
class/race/proficiency가 없으면 fail-closed하고, title/ANSI 및 `[엔터]` 후 `info_2`
주문 continuation은 제외했다. parser/transport와 replay·state purity·race/vet 회귀가
통과했다. 브라우저는 `tests/browser-e2e/classic-terminal.spec.ts`와 feature-off 회귀로
중앙 xterm/line protocol/focus/웹 계정 경계만 검증했으며, 실제 Go+PG 브라우저 게임,
IME/mobile, 전체 command parity는 아직 남아 있다. 상세는
`docs/porting-research/go-terminal-only-browser-20260908.md`에 기록했다.

## 아래는 이전 C/Rust 작업의 역사적 인계 기록

아래 날짜·완료 상태·후속 작업은 당시 기록이며 Go 개발 지시나 최신 검증 결과가 아니다.

- 최종 갱신: 2026-09-05 KST
- 브랜치: `codex/mud-identity-foundation`
- 포팅 기능 기준 커밋: `6055ab0`
- 최신 전체 CI 검증: 기존 [`51dd7be`](https://github.com/1XP-Inc/muhan-mud/actions/runs/33936856759)가 마지막 확인 완료 run이다. `6055ab0`의 PG17 CI 결과는 아직 이 문서에 기록하지 않는다.

## 먼저 알아야 할 상태

이 브랜치는 레거시 C MUD의 파일 영속 상태를 Supabase/PostgreSQL로 단계적으로
이전하고, Rust가 검증 가능한 canonical CDTO 경계만 사용하도록 포팅하는 작업이다.
현재 branch에는 M3 observer/durable handoff, `PlayerSnapshotV1`의 C/Rust/PG17
검증 경계와 DB_ACKED receipt-pair outbox, M4 manifest relay, 그리고 C↔Rust helper
wake 프로토콜의 첫 계약이 들어 있다.

macOS의 case-insensitive filesystem에서 일반 clone을 막던
`objmon/Celduin_sign`/`objmon/celduin_sign` 충돌은 `9f7bf24`에서 해결됐다.
새 clone은 충돌 경고 없이 clean checkout되고, 서로 다른 두 역사적 blob도 보존된다.

**현재 코드는 DB 권위 전환 완료본이 아니다.** PostgreSQL full
`PlayerSnapshotV1` validation과 opt-in idle consumer는 구현·계약 검증이 끝났지만,
기본 경로는 여전히 OFF이고 legacy player file이 authority다. Rust helper wake는
전송·helper process·DB 작업·배포를 전혀 포함하지 않는 16-byte 무상태 신호 계약일
뿐이다. 별도 TDD/PG17/운영 gate 없이 M3 runtime을 켜거나 game save를 DB 권위로
바꾸지 않는다.

## 저장소와 원격

- Private repository: `https://github.com/1XP-Inc/muhan-mud`
- Branch: `codex/mud-identity-foundation`
- 작업 디렉터리: repository root
- push 대상: remote `private`만 사용한다.
- remote `origin`은 `1XP-AI/muhan-mud`이다. 이 작업을 `origin`에 push하지 않는다.
- 커밋 메시지와 PR 설명은 한국어로 작성한다.
- 로컬의 `src/frp.new`는 사용자 소유 dirty file이다. 열기, 수정, stage, commit,
  revert하지 않는다. 원격 branch에는 포함되지 않았다.

새 디렉터리에 받을 때는 이제 sparse checkout 없이 일반 clone을 사용해도 된다.

```sh
git clone --branch codex/mud-identity-foundation --single-branch \
  https://github.com/1XP-Inc/muhan-mud.git muhan-mud.nosync
```

다른 checkout에서 시작할 때:

```sh
git fetch private codex/mud-identity-foundation
git switch --create codex/mud-identity-foundation \
  --track private/codex/mud-identity-foundation
```

이미 같은 이름의 로컬 브랜치가 있으면 새 브랜치를 만들지 말고 해당 브랜치가
올바른 private remote를 추적하는지 먼저 확인한다.

## 장기 목표

레거시 C MUD의 영속 상태를 Supabase/Postgres 권위 모델로 단계적으로 이전하고
Rust 포팅 경계를 구축한다. TDD, 결정론적 differential test, dual-write, shadow
검증을 통과한 기능만 전환한다. 첫 사용자 결과는 다음 두 흐름이다.

1. Supabase 로그인 후 xterm에서 기존 텔넷 가입 절차로 새 캐릭터를 만든다.
2. 기존 MUD 이름과 게임 비밀번호를 xterm에서 한 번 검증해 Supabase 계정과 연결한다.

웹 로그인 계정과 게임 캐릭터는 별도 identity다. Auth 로그인만으로 기존 MUD
캐릭터가 자동 연결되지 않는다. 명시적인 claim/link 절차가 필요하다.

## macOS casefold 리소스 리팩터링 `9f7bf24`

- `objmon/Celduin_sign`은 원본 blob
  `49b3a1975def2762e68f2663351ee55ffb387e61`로 복구했다.
- 기존 소문자 리소스의 원본 blob
  `4eb75435475df4df6d4cb20050754c1ab09baefe`는 casefold-safe 물리 경로
  `objmon/celduin_sign__4eb75435`로 이동했다.
- 레거시 논리 경로 `objmon/celduin_sign`은
  `tools/revive/path-relocations.v1.tsv`와 기존 manifest에 보존된다. 이 raw 소문자
  물리 경로를 다시 만들면 macOS clone 충돌이 재발하므로 만들지 않는다.
- `tools/revive/extract-legacy-blobs.sh`는 relocation source path, legacy path, canonical
  path와 Git blob을 함께 검증한다. 누락 relocation이나 중복 legacy path는 fail한다.
- C/Rust 경로 해석기는 이름이 실제로 바뀐 alias만 canonical-first로 연다. 동일 경로
  alias는 mutable raw file 우선순위를 유지한다. renamed canonical target이 없으면 다른
  대소문자 raw file을 잘못 여는 대신 `ENOENT`로 fail-closed한다.
- `tests/unit/resource_tree_manifest_test.py`는 전체 Git index의 NFD+casefold 유일성,
  두 manifest의 일치, canonical blob과 최소 재생성 계약을 검사한다. `src/Makefile`의
  `unit-test`에 연결돼 Ubuntu CI에서도 실행된다.

## 고정된 설계 결정

- Big-bang rewrite를 하지 않는다.
- 검증된 slice만 `legacy file → DB shadow → durable dual-write → DB authority` 순으로
  승격한다.
- C 프로세스가 레거시 ABI와 raw file의 oracle이다. Rust는 native struct나 raw file을
  직접 읽지 않고 C exporter가 만든 canonical CDTO만 decode한다.
- 권위 전환 gate가 끝날 때까지 legacy player file이 authority다.
- 신규 가입과 기존 캐릭터 claim 입력은 xterm의 C wizard 흐름을 유지한다. 웹 폼에
  게임 validation을 복제하지 않는다.
- journal, receipt, snapshot artifact, outbox manifest는 immutable evidence다. 자동
  삭제나 수정으로 수렴시키지 않는다.
- 새 런타임 경로는 exact opt-in feature flag이며 기본값은 OFF다.
- `replicas: 1`과 Kubernetes `Recreate`는 writer fence가 아니다. PVC process lock,
  DB epoch, journal/reconciliation gate가 별도로 필요하다.

## 현재 포팅 경계

### 1. M3 observer 연결

- `src/character_save_journal_v2_protocol.*`
- `src/character_save_journal_v2_recovery.*`
- `src/character_save_journal_v2_player_store.*`
- `src/character_save_journal_v2_process_owner.*`

durable `PREPARED` 뒤 observer callback과 진단 report를 추가했다. startup recovery와
live player store가 같은 observer를 전달한다. observer 실패가 legacy authority
결과를 바꾸지 않는 테스트가 있다.

### 2. PlayerSnapshotV1 artifact와 native capture

- `src/character_player_snapshot_v1_artifact.*`
- `src/character_player_snapshot_v1_capture.*`
- `src/character_player_snapshot_v1_capture_native.*`
- 관련 `tests/unit/character_player_snapshot_v1_*_test.c`

artifact store는 실제 `PlayerSnapshotV1` decoder와 re-encoder를 통해 exact canonical
bytes를 검증한다. capture는 held writer, PREPARED, stage owner/mode/link/inode/hash를
다시 확인한다. native bridge는 bounded `read_crt_player`를 사용하며 게임 비밀번호를
snapshot payload에 포함하지 않는다.

### 3. PostgreSQL full artifact 계약

- `supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql`
- `supabase/tests/player_snapshot_v1_artifact_contract.sql`
- `supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh`

immutable artifact table, receipt anchor, record/reconciliation RPC와 role 경계가 있다.
`20260915000000`은 canonical envelope, 정확히 40개 field의 id/type/고정 길이,
`PLAYER`, HP/MP 범위, fixed string과 `ObjectGraphV1`의 node/depth/list 예산을
PostgreSQL에서 검사한다. `20260916000000`은 source octets를 acknowledged receipt에
정확히 묶는다. PG17 계약은 migration 140의 RPC 부재를 RED로 확인한 뒤 150/160 적용과
C/Rust exact-byte 재읽기를 GREEN으로 고정한다.

### 4. PlayerSnapshotV1 raw-U8 level projection

- `supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql`
- `services/m4-file-snapshot-manifest-relay/src/store.ts`
- `services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-artifact-relay.ts`

`cb6ab08`에서 C와 Rust가 `PlayerSnapshotV1` field 7의 level을 gameplay range가 아닌
raw U8로 취급하는 0/42/255 differential proof를 고정했다. `08e3a6a`과 `cf98e72`은
receipt-bound artifact에서만 읽는 additive·immutable PostgreSQL projection과 PG17 role
fixture를 추가했다.

`9710b85`는 artifact relay에 선택적 projection store를 추가했다. artifact RPC가
`RECORDED` 또는 `EXACT_RETRY`로 확인된 뒤에만 projection RPC를 호출하며, projection의
invalid/conflict/retryable/unknown 결과나 예외는 artifact 결과, immutable evidence,
legacy authority를 바꾸지 않고 다음 artifact 처리도 막지 않는다. production CLI의
기본 호출에는 projection store를 주입하지 않으므로 이 커밋만으로 runtime 또는 배포에서
projection이 켜지지 않는다.

Linux disposable PostgreSQL 17 E2E는 raw level `42`, artifact와 projection의 exact retry,
projection 행 불변성, 주입된 projection 실패의 격리, legacy/evidence 불변성을 모두
확인한다. migration은 payload를 복제하지 않는 projection만 추가하며, journal v1에는
level이 없으므로 이를 억지로 replay source로 사용하지 않는다.

### 5. PlayerSnapshotV1 replay journal v2 level metadata

- `rust/muhan-core-dto/src/bin/player_snapshot_v2_replay_verify.rs`
- `services/m4-file-snapshot-manifest-relay/src/player-snapshot-v2-replay-verifier.ts`
- `services/m4-file-snapshot-manifest-relay/src/player-snapshot-v2-replay-observer.ts`
- `services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-replay-differential.ts`

`959361c`은 기존 v1 runner, Node parser, observer, journal API의 7-line/version-1
계약을 그대로 보존했다. raw U8 level을 포함하는 8-line/version-2 report와 v2 journal은
별도 이름의 Rust runner·Node verifier·observer·writer로만 만들 수 있으며, 기존 relay
CLI/runtime/configuration은 이를 import하거나 선택하지 않는다.

v2 observer는 한 번의 canonical CDTO decode 결과에서 raw level 0/42/255을 그대로
기록한다. v2 journal은 command/character/receipt/source hash와 snapshot digest·octets,
raw level만 담는 닫힌 metadata이고 payload, legacy file, DB write를 읽거나 만들지 않는다.
level input reader는 v2 journal만 입력으로 허용하고 v1 journal은 level source로 승격하지
않는다. 기존 artifact differential은 v1·v2 journal 모두의 identity/digest metadata를
읽어 비교할 수 있다.

### 6. PlayerSnapshotV1 level projection comparator와 replay reader

- `services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-level-comparator.ts`
- `services/m4-file-snapshot-manifest-relay/src/store.ts`
- `supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql`

`4a7df89`의 pure comparator는 v2 journal의 닫힌 level metadata와 injected immutable
projection evidence만 비교한다. 여섯 identity binding을 raw U8 level보다 먼저 검사하고
`MATCH`, `MISMATCH_LEVEL`, `MISSING_PROJECTION`, `IDENTITY_MISMATCH`, `INVALID_INPUT`,
`UNEXPECTED_DUPLICATE`, `PROJECTION_READ_ERROR`으로 결과를 고정한다.

`51dd7be`는 `PostgresPlayerSnapshotV1LevelProjectionReader`를 추가했다. 이 reader는
전용 replay-reader URL과 read-only session에서 `command_id` 기준으로 정확히 일곱 metadata
열만 한 번 SELECT한다. additive migration은 `mud_replay_reader_login`에 그 일곱 열의
SELECT만 부여하며, payload, writer revision, writer RPC, legacy file, DB write와 runtime
activation은 이 경계에 포함하지 않는다. PG17 contract는 migration replay, exact column
grant, no mutation과 raw U8 `0/42/255` reader/comparator 결과를 고정한다.

이 reader는 아직 production CLI나 기본 relay에 연결되지 않았다. 다음 slice의 명시적
`--once` shadow comparator만 이를 dependency injection으로 사용할 수 있으며, legacy
player file authority와 기본 OFF 동작은 유지한다.

### 7. M4 manifest relay

- `services/m4-file-snapshot-manifest-relay/`

strict 13-line manifest를 lexical order로 읽고 direct PostgreSQL RPC를 호출하는 one-shot
Node service다. player payload를 읽지 않고 outbox evidence를 삭제·수정하지 않는다.

### 8. Durable handoff consumer의 idle lifecycle

`USE_M3_RUNTIME` build에서만 `main.c`가 optional native runtime을 시작한다. exact
`MUD_M3_PLAYER_SNAPSHOT_V1=handoff` opt-in일 때 `io.c`의 serialized game loop가
`output_buf`·command 처리·`update_game` 뒤에 idle hook을 호출한다. cadence는 1초마다
최대 한 token이고 OFF/BUSY/NOT_READY는 no-op이며 실패 진단은 rate-limited다. 일반 종료는
idempotent teardown을 거치고 SIGKILL은 기존 durable recovery 경계로 남는다. 기본 build와
기본 환경은 runtime symbol을 link하거나 I/O를 하지 않는다.

동일한 exact opt-in 안에서만 durable local `DB_ACKED` marker가 검증된 artifact에
대해 `$command.manifest` receipt evidence를 추가한다. generic handoff는 callback이
없으면 기존 capture→cleanup 흐름을 그대로 유지한다. manifest의 `snapshot_octets`는
serialized `PlayerSnapshotV1` bytes가 아니라 relay가 재확인할 legacy source bytes
(`artifact.source_octets`)이며, 이 consumer는 DB 권위나 legacy save 결과를 바꾸지
않는다.

### 9. M3 helper wake protocol v1

`dfcaace`의 `m3_wake_v1.*`와 `rust/muhan-m3-wake-protocol/`은 identity나 durable-state
참조가 전혀 없는 exact 16-byte wake frame을 C/Rust differential test로 고정한다. 이는
미래 helper가 durable artifact/outbox를 다시 스캔하라는 best-effort 힌트일 뿐이며,
loss/duplicate/reorder에 의존하지 않는다. production transport, Unix socket, helper
process, database work, chart와 MUD runtime linkage는 이 slice에 포함되지 않는다.

### 10. 웹 onboarding의 활성화-명령 binding `6055ab0`

웹 xterm의 가입/claim 완료는 이제 DB handoff activation만으로 browser에 성공을 알리지
않는다. Gateway가 lowercase UUID command id를 만들어 `MUD1O ACTIVATED|id`를 C에 보내고,
C는 COMMIT 또는 CLAIMED 뒤에만 이를 받아 actor/correlation/character/mode/id의
non-secret binding을 private local evidence로 먼저 durable write한 뒤
`MUD1O ACTIVE|id`를 보낸다. Gateway는 같은 id의 ACTIVE를 확인한 뒤에만 service-role
RPC `register_game_character_onboarding_snapshot_command_binding`을 호출하고 browser
completion/close를 진행한다.

PostgreSQL migration `20260927000000_onboarding_snapshot_command_binding.sql`은
correlation 당 하나의 immutable command binding과 service-only registration RPC를
추가했다. fulfillment는 artifact 후보를 추론하지 않고, supplied command id의 exact
binding 및 binding 이후 receipt acknowledgement만 받아들인다. Gateway adapter는
`BOUND` 또는 `EXACT_RETRY` 한 행만 허용하고 그 밖의 응답은 fail-closed한다.

이것은 M3 command consumption을 아직 연결하지 않은 안전한 seam이다. 따라서 새
activation id가 실제 save artifact command id로 소비되는 경로, production fulfillment,
DB authority 전환은 다음 별도 slice의 RED-first 증거 없이는 켜지지 않는다. default
legacy player-file authority와 feature-OFF runtime은 유지된다.

## 남은 선행 작업

1. helper transport/process supervision은 별도 설계와 RED 테스트로 시작한다. wake frame은
   identity, path, credential, payload, acknowledgement 또는 authority 신호로 확장하지
   않는다.
2. M3 shadow opt-in을 검토하려면 feature-OFF testnet 배포, PVC/restart/rollback, PG17
   reconciliation과 실제 browser/xterm smoke를 별도 증거로 축적한다. DB authority 전환은
   그보다 뒤의 명시적 gate다.

### 해결된 scoped 항목 (다시 열지 말 것)

- **P1b–P1e durable handoff:** legacy stage의 hard-link 대신 command별 private source
  copy를 만들고, hash-verified rename-only promotion을 사용한다. consumer-visible source는
  항상 `nlink==1`이며 source/source.tmp 2-link 또는 불명 leaf는 evidence를 보존한 poison
  경로로 처리한다. `MAX_PENDING=128` identity(최대 8 GiB payload ceiling)에는 poison도
  포함되고, unsafe A가 `limit=1`에서 뒤의 valid B를 막지 않는다.
- **publish independence:** stage가 publish 뒤 사라진 partial source는 game save가 아니라
  shadow snapshot만 명시적으로 drop한다. publish/ACK는 capture consumer를 기다리지 않는다.
- **P2 relay:** `de80115`에서 Linux descriptor-rooted scan을 사용하고 non-Linux는
  fail-closed로 바꿨다. root pathname fallback을 다시 추가하지 않는다.
- **Linux CI graph:** `d2910bd`에서 PG17 process-owner/runtime-shadow harness에만 required
  handoff/capture closure와 fail-closed fixture decoder를 더했다. default legacy Makefile
  graph에는 handoff/libpq가 추가되지 않았다.

## 다음 위임 권장안

### onboarding 우선 slice

1. **Terra/고난도:** M3 journal/capture/manifest/relay에서 artifact `command_id`가
   생성·보존·ACK되는 전체 경로를 추적한다. Gateway activation id를 한 번만 안전하게
   소비할 수 있는 최소 경계와 failure/retry/rollback matrix, RED tests를 설계한다.
   default-OFF, legacy file authority, payload/credential 비노출을 유지하고 live DB·배포를
   건드리지 않는다.
2. **Luna/중간 난도:** 위 설계의 C↔Gateway control framing과 Gateway RPC adapter를
   read-only로 교차 검토한다. packet coalescing, UUID substitution, duplicate ACTIVE,
   timeout, browser close ordering을 표로 확인한다.
3. **Terra/고난도:** M3 command consumption 설계와 PG17 contract가 모두 GREEN이 된
   뒤에만 opt-in shadow artifact wiring을 별도 TDD slice로 구현한다. activation id를
   generic save command 또는 authority signal로 바꾸지 않는다.

서로 다른 checkout/worktree에서 다음 세 묶음을 병렬화할 수 있다.

1. **Terra/고난도:** v2 level journal reader, pure comparator, dedicated PostgreSQL reader를
   explicit `--once` shadow CLI로만 연결한다. RED-first Node tests에서 stable JSON result,
   index ordering, missing/duplicate/read-error/mismatch, nonzero exit 및 reader close를
   고정한다. 기본 relay, M3 runtime, telnet/login, writer URL, payload·legacy file·DB write는
   절대로 연결하지 않는다.
2. **Luna/중간 난도:** 위 CLI의 dependency boundary와 default-OFF 보존을 읽기 전용으로
   리뷰한다. reader가 exact seven metadata columns 외의 것을 읽지 않는지, CLI가 v2 journal
   이외의 level source를 받지 않는지, default production entrypoint가 import하지 않는지를
   확인한다.
3. **Terra/고난도:** CLI와 PG17/replay CI evidence가 쌓인 뒤에만 raw-U8 level projection의
   production activation configuration/operational 경계를 별도 설계한다. 실제 활성화·배포는
   이 작업의 권한이 아니다.

각 에이전트는 자기 묶음만 수정하고, 커밋하지 않은 다른 에이전트 파일을 정리하거나
덮어쓰지 않는다. 결과 회수 후 실행 세션과 Orca terminal을 0개로 정리한다.

## 현재 검증 증거

### Activation command binding `6055ab0`

- C: `make -C src onboarding-admission-test onboarding-admission-sanitizer-test
  onboarding-activation-binding-test onboarding-activation-binding-sanitizer-test
  command1.o onboarding_admission.o onboarding_activation_binding.o` 통과.
  기존 K&R-style non-prototype 경고는 출력되지만 새 컴파일 오류는 없다.
- Gateway: `npm test` 102/102 pass, `npm run typecheck` pass.
- SQL: migration replay와 command-binding contract 실행은 GitHub Actions의 disposable
  PostgreSQL 17 job에 연결했다. 이 checkout에서는 live DB 또는 container를 실행하지
  않았으므로 해당 CI 결과를 아직 GREEN으로 주장하지 않는다.
- `src/frp.new`는 이 커밋에 stage/commit되지 않았다.

### 리소스 경로 리팩터링 `9f7bf24`

2026-09-04 KST에 다음 로컬 검증이 통과했다.

```sh
python3 tests/unit/resource_tree_manifest_test.py
make -C src unit-test CC=cc
cargo test --workspace --manifest-path rust/Cargo.toml
```

- manifest 검사: Git index 7,877개 경로, alias 3,824개, NFD+casefold 충돌 0건.
- C-only와 `USE_RUST_RESOLVER` runtime alias 회귀 테스트 모두 통과.
- fresh macOS 일반 clone: 충돌 warning 0건, dirty path 0건, 두 canonical blob 일치.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33828127616>
  (`macOS`, `Windows`, `Ubuntu ARM`, `Ubuntu`, `Supabase ownership contract` 모두 GREEN)
- staged credential scan: HIGH/MEDIUM/LOW/WARN 모두 0건.

2026-09-04 KST, WIP 커밋 직전에 다음이 통과했다.

```sh
pnpm --filter @muhan/m4-file-snapshot-manifest-relay test
pnpm --filter @muhan/m4-file-snapshot-manifest-relay typecheck
pnpm --filter @muhan/m4-file-snapshot-manifest-relay build

make -C src \
  character-player-snapshot-v1-artifact-test \
  character-player-snapshot-v1-capture-test \
  character-player-snapshot-v1-capture-native-test \
  character-save-journal-v2-player-store-test \
  character-save-journal-v2-process-owner-test \
  character-save-journal-v2-protocol-test \
  character-save-journal-v2-recovery-test CC=cc

make -C src \
  character-player-snapshot-v1-artifact-sanitizer-test \
  character-player-snapshot-v1-capture-sanitizer-test \
  character-player-snapshot-v1-capture-native-sanitizer-test CC=cc

PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE=1 \
  supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh
```

- relay: 5/5 pass, typecheck/build pass.
- C target과 ASan/UBSan target: pass.
- disposable PostgreSQL 17 migration replay: pass.
- staged diff credential scan: 0 findings.
- `files1.c` native capture build는 레거시 non-prototype warning 67개를 출력하지만 pass.
- 위 GREEN은 당시 WIP 기준 증거다. durable handoff/relay 수정의 최신 CI 증거는 아래 별도 항목을 따른다.

### Durable handoff P1e 및 Linux CI 복구 `372b5e8`, `d2910bd`

2026-09-04 KST에 다음 검증이 통과했다.

```sh
make -C src \
  character-player-snapshot-v1-handoff-static-test \
  character-player-snapshot-v1-handoff-test \
  character-player-snapshot-v1-handoff-sanitizer-test \
  character-player-snapshot-v1-capture-native-test \
  character-save-journal-v2-process-owner-test CC=cc
make -C src unit-test CC=cc
bash -n supabase/tests/m3_process_owner_pg17_integration.sh \
  supabase/tests/m3_runtime_shadow_pg17_integration.sh
```

- focused handoff/capture/process-owner tests, ASan/UBSan, 전체 C unit: pass.
- 독립 Luna review: source rename/poison 경계와 Linux PG17 closure에 P0/P1/P2 없음.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33849595161>
  (`Supabase ownership contract`, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN).
- Linux PG17 E2E는 process-owner/runtime-shadow, full M3 runtime link, artifact contract,
  named-volume restart까지 통과했다. macOS의 native lifecycle target은 Linux-only이므로
  local에서 skip되는 것이 정상이다.

### 2026-09-05 현재 검증

다음은 현재 branch에서 로컬로 다시 확인했다.

```sh
make -C src character-save-journal-v2-bootstrap-test CC=cc
make -C src unit-test CC=cc
./scripts/run-m3-wake-differential.sh
cargo fmt --manifest-path rust/Cargo.toml --all -- --check
cargo test --manifest-path rust/Cargo.toml -p muhan-core-dto
pnpm --filter @muhan/m4-file-snapshot-manifest-relay test
pnpm --filter @muhan/m4-file-snapshot-manifest-relay typecheck
pnpm --filter @muhan/m4-file-snapshot-manifest-relay build
```

- focused bootstrap과 전체 C unit: pass.
- C ASan/UBSan wake oracle, Rust unit, C↔Rust malformed corpus differential: pass.
- relay: 59 pass, 3 expected skip; typecheck/build: pass.
- `PlayerSnapshotV1` full DB contract의 독립 Terra 검토는 P0/P1 구현 누락 없음으로
  판정했다. 선택적 P2 negative fixture 증강은 다음 별도 slice다.
- 이 handoff와 함께 들어가는 bounded bootstrap fixture 수정은 GitHub Ubuntu GCC의
  `-Werror=format-truncation` 경고를 해소하기 위한 것이다.

### 2026-09-05 CI 전체 복구 `67b6bc6`, `72e4099`, `06d10f0`, `d941550`

- `67b6bc6`은 Linux GCC bootstrap fixture의 bounded pathname 경고만 고쳤다.
- `72e4099`은 M3 RPC expiry fixture가 `expires_at > issued_at` 계약을 항상 만족하도록
  두 timestamp를 한 statement에서 재기준화했다.
- `06d10f0`은 successor writer epoch 2에 맞춰 M3 receipt exact-retry golden digest 두 개만
  동기화했다.
- `d941550`은 `player_store`가 쓰는
  `character_save_journal_v2_bootstrap_absent_head` 구현을 PG17 process-owner와
  runtime-shadow E2E harness의 명시 링크 목록에 각각 추가했다. 제품 runtime, SQL migration,
  DB authority, chart와 UI는 바꾸지 않았다.
- 로컬에서는 두 harness의 shell syntax/diff check 및 `player_store`·`bootstrap` 정적 compile와
  bootstrap unit test가 통과했다. PostgreSQL 17 실통합은 GitHub Actions에서 확인했다.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33915490900>
  (`Supabase ownership contract`, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN). 이 run에는
  M3 RPC receipt/login/startup, PlayerSnapshot/relay, process-owner+runtime-shadow, named-volume
  restart와 importer integration이 포함된다.
- 사용자 소유 `src/frp.new`는 이번 네 개 커밋 어디에도 포함되지 않았다.

### PlayerSnapshotV1 receipt-pair opt-in `2fb20c6`–`a861f4c`

- `2fb20c6`은 M3 native runtime의 exact `handoff` opt-in에서만 receipt-pair callback을
  연결했다. generic handoff와 default object graph는 callback, libpq, outbox dependency를
  추가하지 않는다.
- `b40bccc`은 Linux native runtime test harness가 실제 callback ABI를 링크·호출하도록
  고정했고, `36fa7bf`은 PostgreSQL runtime-shadow harness의 명시 링크 closure에
  receipt-pair/outbox 구현 두 개만 추가했다.
- `a861f4c`은 runtime-shadow PG17 E2E가 one `*.player-snapshot-v1` artifact와 같은
  command의 one `*.manifest`를 정확히 검증하게 했다. prepared receipt identity,
  artifact decode/source bytes, manifest의 `source_octets`, file owner/mode/nlink, 불필요한
  regular evidence 부재와 두 번째 idle tick의 inode/content/authority 불변성을 모두
  확인한다.
- 로컬에서는 forced Linux-branch syntax check, handoff/receipt-pair/outbox ASan·UBSan,
  focused handoff tests, 전체 C unit, runtime static/idle-hook static, shell syntax가
  통과했다. macOS에는 Linux libpq/PG17 runtime 환경이 없으므로 실제 disposable E2E는
  GitHub Actions를 authority로 삼는다.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33923184270>
  (`Supabase ownership contract`, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN). 이 run의
  runtime-shadow E2E는 paired evidence, named-volume restart와 importer integration까지
  통과했다.
- 이 slice는 testnet 배포를 변경하지 않았다. `m3.mode=off`와 legacy file authority는
  그대로다.

### PlayerSnapshotV1 raw-U8 level projection relay `cb6ab08`–`9710b85`

- `cb6ab08`의 C/Rust differential은 field 7 level의 raw U8 값 0, 42, 255를 canonical
  bytes로 round-trip한다. value를 gameplay range로 clamp하거나 reinterpret하지 않는다.
- `20260919000000_player_snapshot_v1_level_projection.sql`은 verified artifact receipt를
  source로 하는 payload-free immutable projection과 `mud_writer` RPC를 추가한다.
  `08e3a6a`의 초기 schema offset 가정은 `cf98e72`에서 actual PG17 role fixture와 함께
  바로잡았다.
- `9710b85`의 optional relay store는 artifact `RECORDED`/`EXACT_RETRY` 뒤에만 exact five
  parameter projection RPC를 호출한다. projection failure category는 별도 summary로
  남고 artifact counter/evidence/legacy authority와 이후 파일 처리를 바꾸지 않는다.
- 로컬에서 relay test 53 pass/3 expected skip, typecheck, build, E2E shell syntax가
  통과했다. disposable PostgreSQL 17 전체 E2E는
  [GitHub Actions run 33932493006](https://github.com/1XP-Inc/muhan-mud/actions/runs/33932493006)에서
  raw level 42, exact retry, UPDATE 거부, failure isolation까지 통과했다. 동일 run의
  Supabase ownership contract, Ubuntu ARM, Ubuntu, Windows, macOS도 모두 GREEN이다.
- 이 slice는 production CLI 기본 경로, M3 runtime, C save path, DB authority, chart,
  testnet 설정과 데이터를 바꾸지 않았다. `m3.mode=off`와 legacy file authority는 그대로다.

### PlayerSnapshotV1 replay journal v2 level metadata `959361c`

- 기존 `player_snapshot_v1_replay_verify`와 v1 Node parser/observer/journal은 exact
  version-1 7-line contract를 유지한다. v2는 별도 `player_snapshot_v2_replay_verify`,
  `PlayerSnapshotV2ReplayObserver`, v2 verifier 및 v2 journal writer로만 생성되며 기본 relay
  CLI, runtime, configuration은 이를 import하거나 활성화하지 않는다.
- v2 report/journal은 동일 canonical decode에서 field 7 raw U8 0/42/255을 metadata로만
  전달한다. `readPlayerSnapshotV1ReplayLevelDifferentialInputs`는 v2 journal만 level input으로
  허용하고, v1 journal은 level 부재 때문에 `JOURNAL_INVALID`로 닫힌다. artifact differential은
  v1·v2의 identity/digest·octets metadata를 모두 비교할 수 있다.
- 로컬 Rust 전체 `muhan-core-dto` test, relay 59 pass/3 expected skip, typecheck, build,
  cargo fmt와 scoped diff check가 통과했다. independent Luna final review에는 P0/P1/P2 blocker가
  없었다. private GitHub Actions
  [run 33934527031](https://github.com/1XP-Inc/muhan-mud/actions/runs/33934527031)도
  Supabase ownership contract, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN이다.
- 이 slice는 migration, C runtime, M3 activation, chart, testnet, live DB data를 바꾸지 않았고
  legacy player file authority를 유지한다.

### PlayerSnapshotV1 level projection comparator `4a7df89`

- `player-snapshot-v1-level-comparator.ts`는 v2 journal에서 이미 닫힌 level metadata만 받고,
  injected `findByCommandId` reader로 immutable projection evidence를 읽는 순수 비교 경계다.
  PostgreSQL client, writer RPC, payload, legacy file, CLI, runtime activation을 import하거나
  호출하지 않는다. v1 journal은 raw U8 level source가 될 수 없다.
- `characterId`, `commandId`, receipt request SHA-256, source post SHA-256, snapshot SHA-256,
  snapshot octets의 여섯 binding을 raw U8 비교보다 먼저 exact하게 검사한다. 결과는
  `MATCH`, `MISMATCH_LEVEL`, `MISSING_PROJECTION`, `IDENTITY_MISMATCH`, `INVALID_INPUT`,
  `UNEXPECTED_DUPLICATE`, `PROJECTION_READ_ERROR`으로 안정적으로 분류된다.
- TDD는 raw U8 0/42/255 exactness, identity-first ordering, missing/duplicate/reader failure,
  malformed input/evidence fail-closed를 포함한다. relay local test는 63 pass/3 expected skip,
  typecheck와 build가 통과했다. private GitHub Actions
  [run 33935442808](https://github.com/1XP-Inc/muhan-mud/actions/runs/33935442808)의 재실행은
  Supabase ownership contract, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN이다. 최초 실행의
  M3 disposable-container readiness 실패는 비교기 단계 이전의 일시 실행 환경 문제였고, 동일
  커밋 재실행에서 전체 contract가 통과했다.
- 이 slice는 migration, PG adapter, C runtime, M3 activation, chart, testnet, live DB data를
  바꾸지 않았고 legacy player file authority를 유지한다.

### PlayerSnapshotV1 level projection replay reader `51dd7be`

- `PostgresPlayerSnapshotV1LevelProjectionReader`는 replay-reader 전용 URL과 read-only
  session을 사용해 `private.game_character_player_snapshot_v1_level_projections`에서
  `commandId`, `characterId`, receipt/source/snapshot SHA-256, `snapshotOctets`, `rawLevelU8`
  일곱 metadata만 parameterized one-shot SELECT한다. reader는 payload, legacy file, writer
  RPC, SET ROLE, mutation API를 노출하지 않는다.
- additive migration `20260920000000`은 projection table의 기존 wide 권한을 회수하고
  `mud_replay_reader_login`에 정확히 위 일곱 열의 SELECT만 준다. PG17 fixture는 migration
  idempotence, column grant, RLS, restricted `writer_revision` read 거부와 mutation/RPC 거부를
  확인한다.
- focused reader/comparator tests는 raw U8 `0/42/255`, missing, duplicate, read failure와
  malformed evidence를 확인한다. 로컬 relay test는 67 pass/3 expected skip, typecheck,
  build, PG17 shell syntax가 통과했다.
- private GitHub Actions [run 33936856759](https://github.com/1XP-Inc/muhan-mud/actions/runs/33936856759)는
  replay-reader PostgreSQL 17 contract, relay E2E, Linux/ARM, Windows, macOS, browser xterm,
  Rust CDTO 및 scenario contracts까지 모두 GREEN이다.
- 이 slice는 production CLI, default relay, C/M3 runtime, MUD save path, chart, testnet와
  live data를 바꾸지 않았다. legacy player file authority와 feature-OFF 상태를 유지한다.

### M3 wake supervisor 계약 기반 `8e8d09e`

- `m3_wake_supervisor.*`에는 injected operations만 사용하는 test-only 감독 계약을
  추가했다. default `OBJECTS`와 `M3_RUNTIME_OBJECTS`, `main.c`, `io.c`, native runtime,
  Helm chart는 바꾸지 않았다.
- supervisor는 `m3_wake_v1`의 canonical 16-byte frame만 best-effort로 전송한다. durable
  queue/outbox, game save, DB/RPC를 소유하거나 수정하지 않으며, OFF 상태에서는 callback과
  I/O를 수행하지 않는다.
- `EAGAIN`/`EWOULDBLOCK`은 현재 wake만 버리고 owner를 유지한다. short/unclassified/fatal
  send 및 성공한 reap은 owner를 정확히 한 번 release하고 deterministic backoff로 전환하며,
  shutdown도 one-shot·nonblocking이다.
- TDD RED→GREEN, focused C/ASan·UBSan, default-link static audit, C↔Rust wake differential을
  통과했다. private GitHub Actions도
  <https://github.com/1XP-Inc/muhan-mud/actions/runs/33924942926>에서 모든 job이 GREEN이다.
- 실제 Linux child, Unix socket, helper executable/identity, credentials, runtime hook,
  endpoint policy, deployment 및 feature flag는 의도적으로 이 slice 밖에 있다. 이는 별도
  승인과 Linux restart/reconciliation 증명 없이는 연결하지 않는다. testnet의 `m3.mode=off`와
  legacy file authority는 그대로다.

### M3 wake supervisor owner-token refinement `349aaf9`

- owner는 raw descriptor가 아닌 **양수의 opaque token**으로 명시했다. `claim_owner`의 0 또는
  음수 결과는 unavailable로 정규화되어 send/release 없이 deterministic backoff로 전환한다.
  따라서 미래 Linux adapter가 descriptor 0을 포함한 실제 resource를 token 뒤에 안전하게
  보관할 수 있고, supervisor 자체는 OS handle을 해석하지 않는다.
- pure fake-ops TDD는 negative/zero/missing claim, exact retry deadline과 overflow saturation,
  missing send/release callback, non-positive/missing reap, idle shutdown을 추가로 고정했다.
  re-init semantics와 real FD ownership은 의도적으로 아직 정의하지 않았다.
- RED→GREEN, focused C/ASan·UBSan, default-link static audit, C↔Rust wake differential 및
  private GitHub Actions <https://github.com/1XP-Inc/muhan-mud/actions/runs/33926283456>의
  모든 job이 GREEN이다. default object graph, runtime wiring, MUD save path, DB/Auth,
  deployment와 testnet 상태에는 변화가 없다.

### Replay journal regular-file boundary `10969b1`

- offline `PlayerSnapshotV1` replay differential의 default journal reader는
  `entry.json`이 regular file일 때만 읽는다. symlink/non-regular entry와 metadata read
  error는 `JOURNAL_INVALID`로 기록하고 artifact reader/DB query를 수행하지 않는다.
- RED→GREEN test는 valid JSON target을 향하는 `entry.json` symlink가 기존에는 `MATCH`와
  one reader query를 만들었음을 재현했고, 수정 후 `JOURNAL_INVALID`와 zero query를
  고정했다. symlink fixture를 만들 수 없는 platform만 이유와 함께 skip한다.
- relay test 50 pass/3 expected skip, typecheck/build 및 private GitHub Actions
  <https://github.com/1XP-Inc/muhan-mud/actions/runs/33927246345>의 모든 job이 GREEN이다.
  injected-reader, lexical/256 bound, metadata-only semantics는 유지했다.
- 이 변경은 offline reconciliation read boundary만 다룬다. M3 runtime, MUD save,
  DB state/schema, Auth, deployment, testnet 데이터는 바꾸지 않았다.

### Feature-OFF legacy authority contract `ea542e4`

- `MUD_M3_MODE`가 absent 또는 `off`일 때 failing shadow starter는 시작되지 않으며, 실제
  legacy `FileStore`의 atomic save 뒤 `load_ply`가 같은 legacy bytes를 다시 읽는 계약을
  추가했다. save 성공은 receipt·DB·shadow 결과에 의존하지 않는다.
- normal/ASan·UBSan target, 전체 C `unit-test`, 독립 Luna review 및 private GitHub Actions
  <https://github.com/1XP-Inc/muhan-mud/actions/runs/33928831076>가 GREEN이다. test fixture는
  실행 후 player bytes와 temporary root를 정리한다.
- production save wiring, M3 enablement, Supabase schema/RPC, game data, deployment와
  testnet 상태에는 변화가 없다.

### Opt-in web onboarding smoke harness `2c734b2`

- default로는 browser/network를 만들지 않고 두 Playwright case를 skip한다. 단위 guard는
  literal enable flag, HTTPS target, 별도 durable-data/uniqueness attestation, 서로 다른
  pre-created fixture accounts/names, non-placeholder inputs를 요구한다.
- 명시 opt-in 후의 provision은 기존 C/Gateway wizard의 name → gender → class → stats →
  weapon → alignment → race → game-password 순서를 완료하고, C `SAVED`, Gateway finalize,
  C `COMMIT` 뒤 active roster와 normal game admission까지 확인한다. claim도 named legacy
  fixture에서 같은 roster/admission을 확인한다.
- web 26 tests, typecheck, default Playwright skip, 독립 Terra/Luna review 및 위 private CI가
  GREEN이다. 이 하네스는 signup/fixture 생성/전역 uniqueness query를 자동화하지 않는다.
  실제 실행은 사용자에게서 받은 별도 승인과 pre-created disposable fixtures가 있어야 하며,
  영속 Auth/onboarding/character/session data를 만든다.

## Kubernetes 현황

- context: `testnet-1xp`
- release: `muhan-mud-testnet`
- URL: <https://muhan.1xp.vc>
- Helm revision: `9` (`deployed`, 2026-09-05 KST 확인)
- web, gateway, MUD, Auth, PostgREST, Realtime와 PostgreSQL은 모두 Ready였다.
- running app digest:
  `sha256:5f025ee1732981b2cd6d9aaef18ffc858b90905f6cc1e80a9bbf8d141ce174ec`
- chart repo: sibling checkout `../tesnet-1xp.nosync/muhan-mud/charts`
- onboarding과 reconciler는 enabled다. `m3.mode=off`, replay differential도 OFF다.
- 웹 Auth 계정은 MUD 캐릭터를 자동 생성하거나 자동 연결하지 않는다. 연결된 캐릭터가
  없는 계정이 게임 진입 전 안내를 보는 것은 정상이며, 실제 신규 가입/기존 캐릭터 link는
  xterm의 원래 wizard/claim 흐름으로 검증해야 한다.
- 영속 QA 계정이나 캐릭터는 아직 만들지 않았다. 사용자 데이터가 생기는 실제 smoke는
  명시적으로 기록하고 정리 가능한 test account만 사용한다.

## 다음 완료 순서

1. M3는 OFF인 채로 testnet의 xterm 신규 가입과 기존 캐릭터 claim/link를 실제 browser
   smoke로 검증한다. harness/guard/CI는 준비됐고, 이제 명시 승인된 disposable web users,
   provision name, imported unclaimed character fixture 및 전역 uniqueness 확인이 있어야
   실행한다. game account와 Auth account의 분리는 유지한다.
2. raw-U8 PlayerSnapshotV1 level proof, receipt-bound immutable projection, v2 closed journal
   metadata input gate, pure comparator와 dedicated `mud_replay_reader` PostgreSQL adapter는
   완료됐다. 다음 slice는 이 세 경계를 explicit `--once` shadow CLI로만 조합하는 것이다.
   stable JSON records/index/comparison 결과와 missing/duplicate/read failure/mismatch의
   fail-closed 분류를 RED→GREEN으로 고정하며, writer RPC·payload·legacy file·runtime activation은
   쓰지 않는다. production activation configuration은 그 다음 별도 gate다.
3. 실제 Linux helper transport/process supervision은 endpoint, helper
   identity, credential inheritance, shutdown policy를 명시 설계하고 test-only contract에
   Linux fake-ops/FD hygiene RED gate를 추가한 뒤 별도 slice로 시작한다. wake protocol
   자체에는 identity/path/credential/payload/ack/authority field를 덧붙이지 않는다.
4. feature-OFF 배포, PVC/restart, rollback, PG17 reconciliation과 충분한 shadow evidence가
   모두 쌓인 뒤에만 별도 승인으로 M3 opt-in을 검토한다. DB authority 전환은 그 이후다.

## 먼저 읽을 문서

- `docs/porting-research/execution-plan.md`
- `docs/porting-research/m3-helper-wake-protocol-v1.md`
- `docs/porting-research/m3-journal-v2-gates.md`
- `docs/porting-research/persistence-supabase.md`
- `docs/porting-research/rust-differential.md`
- `docs/web-mud/game-identity-refactor.md`
- `docs/web-mud/live-onboarding-smoke.md`
- `docs/web-mud/trusted-admission.md`

`execution-plan.md`의 날짜가 있는 snapshot은 당시의 역사적 근거다. 현재 상태는 이
handoff와 이후 GitHub CI 결과를 우선한다. 완료율을 과장하지 않고, 검증된 gate와 아직
검증하지 않은 경계를 분리해 보고한다.

## 2026-09-08 resource graph conversion 후속

이름이 빈 zeroed creature record를 `empty-monster-placeholder` evidence로 격리하는
compatibility admission을 추가했다. `ImportNPCs`는 room/id와 원본 monster slice 순서로
NPC identity를 만들고, `ImportRoomItems`는 room/root/nested preorder로 floor ID graph를
만든다. `ImportNPCItems`는 canonical NPC membership 순서로 NPC inventory를
`NPCState.Items`로 옮긴다. `State.Validate`는 NPC/floor/player/bank 사이의 item ID
중복과 legacy/canonical 이중 소유를 거절하고, allocator 실패는 partial snapshot을
반환하지 않는다. projection은 legacy view만 만들며, canonical NPC death drop은 기존
item ID를 room graph로 이동한다.

`engine.SeedCanonicalWorldFromCatalog`와 CLI `-seed-world <id> -seed-canonical
-seed-rooms <rooms-dir>`를 추가했다. deterministic `npc-XXXXXXXX`/`item-XXXXXXXX`
allocator로 2,341 room을 one-shot seed한다. `TestLegacyRoomCatalogImportsCanonicalResourceGraphs`,
`TestPostgresSeedCanonicalRoomGraphs`, ARM64 PostgreSQL 17 실제 CLI canonical seed,
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
`git diff --check`가 통과했다. strict `TestRoomBodyCorpus`의 기존 63개 실패는 그대로
남아 있으며, 전체 player/bank 이관·scheduler/tick·브라우저 E2E·testnet 전환은 미완료다.

2026-09-08 player-vital scheduler 후속: `RunPlayerVitalScheduler`를 world-mode worker에
연결하고 `-player-tick` cadence, `player-vitals-<slot>` receipt ID, slot boundary
timestamp, pending retry, duplicate skip, writer serialization을 추가했다. shutdown은
server drain 뒤 scheduler/cleanup worker를 취소하고 session cleanup을 마지막으로
재시도한다. `TestRunPlayerVitalTickUsesDeterministicSlotAndSkipsDuplicate`,
`TestRunPlayerVitalTickRetriesExactPendingCommand`,
`TestRunPlayerVitalSchedulerStopsAfterContextCancellation`, ARM64 PG17의
 `TestWorldConnectorPlayerVitalTickReplaysAcrossConnectorRestart`가 통과했다. 이는
player-vital 부분 루프만 운영 연결한 것이며 NPC/room spawn, combat tick, persistent
game clock와 전체 `update.c` scheduler는 여전히 미완료다.

## 2026-09-08 실제 Go + PostgreSQL + 브라우저 E2E

실제 브라우저 경계를 가짜 WebSocket과 분리한 하네스를 추가했다. 실행 명령은
`bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`이며,
로컬 ARM64 `postgres:17-alpine` 전용 컨테이너를 무작위 loopback 포트에 만들고
종료 시 자기 컨테이너만 제거한다. Go `cmd/muhan`과 fixture seed 명령을 `-race`로
빌드한 뒤 Next 중앙 xterm을 실제 Go WebSocket에 연결한다.

Playwright는 xterm 안에서 이름·한글 IME 커밋(예/남/선/봐)·성별/직업/능력치/무기/
성향/종족·암호를 입력하고, PostgreSQL에 캐릭터와 세계를 저장한 뒤 첫 방에 입장한다.
페이지 재로드 후 같은 이름/암호로 재로그인하고 `봐`를 실행하며, 암호가 출력되지
않는 것도 검사한다. 2026-09-08 실행 결과는 **1 passed (9.7s)**였고, 전용 컨테이너는
정리 후 남지 않았다. 상세 계약과 제한은
`docs/porting-research/go-process-postgres-browser-e2e-20260908.md`를 따른다.

이 증거는 가입·월드 입장·재로그인 경계를 증명하지만 전체 legacy 명령/전투/tick,
OS IME·모바일 키보드, WSS/Ingress, testnet 배포 인수를 의미하지 않는다. strict
`TestRoomBodyCorpus`의 기존 63개 예외와 전체 게임 기능 미완료 상태는 유지한다.

## 2026-09-08 `도움말` 문서 receipt 연결

`server/internal/session/help_command.go`와 `CommandHelp`를 추가해 C `help()`의
문서 경계를 실제 Go 월드 커넥터까지 연결했다. `도움말`/`?`는 `helpfile`, `도움말
주술`은 `spellfile`, `도움말 정책`은 `policy`, 현재 durable Go handler가 있는
명령 주제는 원본 `help.<cmdno>`를 읽는다. source는 `fs.FS` 주입이며, 배포 기본은
`-help-dir /home/muhan/help`, 로컬 브라우저 하네스는 repository `help/`다. missing
또는 invalid UTF-8 문서는 추정하지 않고 실패하고, unknown topic은 C의 고정 no-help
응답을 receipt로 저장한다.

TDD는 문서 읽기, unknown topic, missing document fail-closed, state purity, 동일
command ID replay, connector dispatch를 확인한다. 실제 `bash
scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`도
`도움말 정보`까지 포함해 **1 passed (11.0s)**였고, 전용 PostgreSQL 컨테이너는
정리됐다. 이 slice는 전체 C help alias/약어, continuation prompt, info title,
전체 command table parity를 완료한 것이 아니다. `src/frp.new`는 여전히 사용자
변경으로 dirty이며 손대지 않았다.

## 2026-09-08 `환영`·`외쳐`·감정표현 후속 체크포인트

이번 로컬 체크포인트에서는 `환영`·`외쳐`와 bounded 일반 플레이어 감정표현을 Go
월드 receipt 경계에 연결했다. `환영`은 프로세스가 주입한 `help/welcome` 문서를
read-only로 읽고, 누락/비 UTF-8이면 fail-closed하며 동일 command ID replay에서
파일 재읽기/commit을 하지 않는다. 브라우저 검증은 실제 문서의
`레벨 5가 넘으면 많은 제약이 따릅니다.` 문장을 확인한다.

`외쳐`는 빈 입력·침묵·PHIDDN 해제 순서를 보존하고, commit 뒤 현재 방의 이름 있는
메시지와 ordered exit의 익명 메시지를 fan-out한다. 본인·replay에는 재방송하지 않고,
알 수 없는 출구는 후보 저장 전에 거절한다. `미소` 등 `src/action.c`에서 현재 exact
출력 계약을 확보한 감정표현 alias는 exact same-room online player target만 허용한다.
target에는 개인 출력, 같은 방의 다른 연결에는 room 출력, 본인에는 비동기 event를
보낸다. NPC/prefix/occurrence/미검증 target은 receipt 없이 거절한다.

추가 파일:

- `server/internal/session/welcome_command.go`, `yell_command.go`, `emote_command.go`
  및 단위/PG 회귀 테스트
- `server/internal/world/yell.go`, `emote.go` 및 deterministic state/event 테스트
- `server/internal/transport/world_connector_*_test.go` fan-out/dispatch 회귀

검증 결과:

- `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1` 통과
- `go vet ./...` 통과
- 전용 ARM64 `postgres:17-alpine`에서 `TestPostgres(Emote|Yell)CommandPersistsAndReplays`
  통과 후 해당 컨테이너 제거
- `bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`:
  실제 Go `-race` + PostgreSQL + Chromium **1 passed (10.4s)**
- `pnpm test:browser`: 표준 xterm과 feature-off **각 1 passed**

이는 전체 `action.c` alias/154 명령, strict `TestRoomBodyCorpus`의 기존 63개 예외,
모바일 IME/WSS/Ingress, testnet 배포 인수를 완료했다는 뜻이 아니다. `src/frp.new`는
사용자 dirty 상태라 보존했고 수정하지 않았다. 다음 통합 후 root commit SHA를 infra
저장소의 Docker source revision test에 반영하고, infra 72-test suite를 재실행한다.

## 2026-09-08 `표현`·`보아 <대상>` 통합 체크포인트

다음 병렬 Luna max slice도 통합했다. `표현`은 `command11.c:emote`의 free-form UTF-8
payload를 255바이트·제어문자 경계로 제한하고, 빈 입력/침묵/PLECHO/PHIDDN ordering을
receipt로 고정했다. actor 응답만 durable receipt에 넣고, commit 뒤 같은 방의
`:이름님이 <text>.` event를 본인과 replay를 제외해 fan-out한다. 임의 payload는
receipt에 저장하지 않는다.

`보아 <대상>`은 `action.c`의 explicit target branch를 bounded 포팅했다. NPC-first
canonical room traversal, exact display-name, same-room online/visibility/detect 경계를
적용하고, player target은 대상자 개인 projection과 observer room projection을 분리한다.
NPC target은 검증된 room projection만 제공한다. bare `보아`, prefix/occurrence/object
inspection 및 전체 `조사` parity는 아직 남아 있다. source처럼 PHIDDN은 PSILNC보다 먼저
clear된다.

추가/검증 파일:

- `server/internal/world/express.go`, `look_at_target.go` 및 reducer 테스트
- `server/internal/session/express_command.go`, `look_at_target_command.go` 및 unit/PG 테스트
- `server/internal/transport/world_connector_express_look_test.go`와 parser/transport 통합

검증 결과:

- 전체 Go race `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1` 통과
- `go vet ./...`, Linux ARM64 CGO-free cross-build 통과
- 전용 ARM64 PostgreSQL 17에서 `TestPostgres(Emote|Express|LookAtTarget|Yell)CommandPersistsAndReplays` 통과
- 실제 Go+PostgreSQL+Chromium 가입→월드→`환영`→`도움말 정보`→`표현`→`외쳐`→재로그인 E2E **1 passed (10.1s)**

이 체크포인트도 전체 154 C handler/전체 action alias, strict room corpus 63개 legacy
예외, 모바일/WSS/Ingress 및 testnet 전환 인수를 완료한 것은 아니다. 다음 commit에서
root/infra source revision과 이 문서를 함께 확인한다.

## 2026-09-08 `설정`·`해제` settings slice

원본 `command5.c:set`/`clear`의 player option 경계를 Go에 연결했다. legacy flag 번호를
그대로 사용해 일반 display/broadcast/room 옵션, `도망수치`, `패거리귀환`과
`hexline`/`eavesdropper`/`~robot~`/`수동공격`을 canonical state에 저장한다.
`설정` 인자 없음의 flag-list, `해제` 도움말·오류, source-backed 응답 문구를 고정했고,
`WimpyValue`는 raw C decoder와 분리된 JSON canonical 상태 필드다.

parser→world proposal/apply→PostgreSQL receipt/replay→WebSocket Submit 경계를 통과한다.
ordinary toggle의 expected bit, wimpy value, family membership을 Apply에서 재검증하며
stale proposal과 malformed numeric input은 fail-closed한다. unknown `설정`은 C처럼
flag-list, unknown `해제`는 오류 문구를 반환한다.

검증: settings world/session/transport TDD, 전체 Go race/vet, Linux ARM64 cross-build,
격리 ARM64 PostgreSQL 17 `TestPostgresSettingsCommandPersistsAndReplays`, 실제 Go+
PostgreSQL+Chromium 가입→`설정 색`→재로그인 E2E **1 passed (11.2s)**. 전체 C flag-list
ANSI/관리자 옵션과 나머지 명령·tick·경제·배포 인수는 여전히 남아 있다. 이 slice의
root 변경 후 infra Docker source revision pin을 갱신해야 한다.

## 2026-09-08 `열어`·`닫아` door slice

`command6.c:openexit`/`closeexit`의 bounded same-room 출구 전이를 Go에 연결했다.
첫 prefix match, `XLOCKD`/`XCLOSD`/`XCLOSS`, open timestamp, actor `PHIDDN`을
하나의 world proposal/apply와 PostgreSQL receipt/replay로 처리한다. 성공 receipt만
committed room event를 다른 연결에 보내며, actor/replay에는 중복 event가 없다.
새 출구가 계획 이후 나타나도 stale proposal로 거절한다.

검증: door world/session/transport TDD, 전체 Go race/vet, Linux ARM64 build,
격리 ARM64 PostgreSQL 17 `TestPostgresDoorCommandPersistsAndReplays`, 실제 Go+
PostgreSQL+Chromium `열어 __missing_door__` 경로 **1 passed (10.8s)**. `풀어`/`잠궈`/
`따`의 key object·내구도·picklock 및 전체 occurrence/ANSI formatting은 다음 slice다.

## 2026-09-08 ChatGPT 직접 병렬 통합 체크포인트

세 개의 독립 Luna max lane을 서로 다른 worktree에서 실행한 뒤 root가 직접 diff·테스트
검증하고 통합했다. 데이터 lane `6631e5c`는 3,216개 방 중 strict 예외 63개(총 이슈
100건)의 SHA/소비 위치/이슈 분류와 raw 불변·strict 거부 회귀를 고정했다. C 출력
oracle이 없어 자동 EUC-KR 치환·NUL 합성·tail 절삭은 보류했다.

웹 lane `9b49c55`는 중앙 xterm reconnect/resize/submit focus 복구, 한글 IME 보호,
모바일 visualViewport, 비밀번호 로컬 echo 차단과 미전송 입력 폐기를 추가했다. load
lane `500aee6`는 운영 endpoint를 바꾸지 않는 httptest REST-like/persistent capacity
probe와 100/250/500/1000 staged target, cleanup/namespace/loopback 충돌 검증을
추가했다. persistent probe의 32-session 결과는 운영 동접 보증이 아니다.

통합 검증은 Go exception audit, `go test -race ./... -skip '^TestRoomBodyCorpus$'`,
`go vet ./...`, Linux ARM64 CGO-free build, web typecheck/42 tests/build,
classic-terminal Playwright 2 tests, load harness race/100·1000 smoke에서 통과했다.
실제 Go+PostgreSQL+Chromium은 PostgreSQL 이미지가 없는 환경에서 실행하지 않았다.
infra Docker source revision pin은 `eacaa9d7`에서
`500aee64b8af73370e880c8e9202246edafaf22c`로 갱신했다.

root 작업 트리의 유일한 미커밋 변경은 사용자 소유 `src/frp.new`이며 보존한다.

## 2026-09-08 bounded reconnect follow-up

직접 Luna max read-only review가 유효한 view마다 `reconnectAttempt`를 0으로 되돌려
반복 transient close가 무한 재연결할 수 있는 P2를 발견했다. `6e86daa`에서 메시지별
reset을 제거하고 반복 drop 뒤 다섯 번째 WebSocket 연결이 생기지 않는 Playwright
회귀를 추가했다. 최종 web typecheck/42 unit tests/build와 xterm Playwright 3 tests가
통과했다. infra source pin은 `e0b2161d`에서 최종 root SHA
`6e86daac1dab719f80c4bb5d0a3c45c431d075b9`를 가리킨다.

## 2026-09-08 ChatGPT 직접 Luna max 후속 병렬 통합

Orca를 사용하지 않고 root가 직접 세 개의 Luna max lane을 배치·회수했다. lane은
서로 다른 파일 경계를 사용했고 root가 각 커밋을 재검토한 뒤 통합했다.

- `153efb7` 도움말 catalog: 원본 `help.21`, `22`, `24–29`, `32–35`, `61`, `100`이
  실제 존재하는지 확인한 뒤 exact alias를 `도움말`에 연결했다. 없는 `help.31`은 연결하지
  않았고, 문서 누락/invalid UTF-8은 receipt 전에 거부하며 replay에서는 문서를 다시 읽지
  않는다.
- `f09b0a4` target occurrence: `보아 <prefix> <positive occurrence>`를 canonical
  NPC→player 순서로 연결했다. C `find_crt`의 display name/세 key prefix와 1-based
  occurrence를 적용하되 object/exit occurrence는 canonical identity가 없어 fail-closed한다.
  WebSocket room/recipient fan-out도 동일 occurrence를 재해석하며 replay에서는 event를
  재방송하지 않는다.
- `cbf6b8b` NPC scheduler boundary: 현재 `RunPlayerVitalTick`이 NPC/room refresh를
  주장하지 않도록 contract regression만 추가했다. canonical spawn origin과 active-order
  replay가 준비되지 않은 상태에서 새 scheduler를 추측해 만들지 않았다.

통합 검증은 `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`와 새 session/world/transport
focused tests가 통과했다. strict room corpus의 기존 63개 예외, 전체 C command/tick/경제,
실제 PostgreSQL/Chromium 재실행, WSS/Ingress와 testnet 인수는 여전히 미완료다.
인프라 저장소의 `scripts/docker-source-paths.test.mjs` reviewed source pin은 새 root
`f09b0a4a606f61d0ffb8505f660d421d31bda030`을 가리키도록 갱신했으며, infra 테스트 재실행과
commit은 별도 통합 단계다. `src/frp.new`는 계속 사용자 dirty 변경으로 보존한다.
