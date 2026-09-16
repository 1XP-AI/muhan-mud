# Go 게임 서버 기능 원장 (G0 조사)

## 2026-09-15 G3 `notepad` 수직 slice 및 `group` 표시 parity

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `post.c:notepad`의 `*notepad`/`*메모`, view·append·clear·invalid option·`noteedit` dot continuation | `CommandNotepad`가 exact 두 alias를 중앙 분류하고, `State.Notepad`의 nil migration marker와 ordered line projection을 `PlanNotepad`/`ApplyNotepad`·JSON clone/validate로 고정한다. session은 CARETAKER actor를 canonical snapshot에서 확인하고, transport는 최신 canonical notepad와 draft/header를 매 body line 및 첫 dot에서 preflight한 79-byte UTF-8-safe connection-local buffer를 `.`에서만 durable receipt로 제출한다. 저장 불확실성 뒤의 `commitPending` dot 재시도는 preflight보다 durable receipt replay를 우선한다. raw `POSTPATH`/파일 경로/credential는 state·payload·authority로 사용하지 않는다. | Luna max `task_76627974420b`/`ctx_1189481fa7f3`, `task_953d12c6cfe3`/`ctx_5d0578a24d87`, `task_0647814ad8ed`/`ctx_46b456ced671`, `task_d67d7330185b`/`ctx_a49ac2f67804` 순으로 보강했다. 커밋 `12c212a`·`f76f8b4` push, notepad world/session/transport targeted race, full transport race, vet·gofmt·diff-check PASS. 독립 리뷰 `task_801c59af8716`/`ctx_a67b7174caf6`는 P0/P1/P2 없음, post-preflight canonical fill의 별도 P3를 기록했다. 기존 `TestRoomBodyCorpus` 63건과 NPC respawn combat notice 2건은 별도 baseline 실패. DM_pad 원본 import·운영 PG/복구·전체 C 출력/브라우저·배포는 미완료 |
| `command4.c:group`의 following leader·mixed `first_fol` 순서·PDMINV skip | `PlayerGroup`가 canonical mixed order와 following-edge leader를 유지하고 canonical NPC follower의 `PDMINV`도 숨긴다. | world `PlayerGroup` race·vet·gofmt·diff-check PASS. nil `FollowerRefs` category fallback, 그룹 구성/mutation·full C 출력·PG/E2E는 미완료 |

## 2026-09-15 G3 notepad continuation limit preflight follow-up

독립 PR review의 P3-2를 반영해 connection-local editor가 현재 canonical `State.Notepad`와
buffered draft/header를 함께 계산한다. line/byte 초과는 dot 이후 `commitPending` retry-only 상태로
잠기지 않고 입력 단계에서 안전하게 거절하며, dot 직전에도 canonical 변경을 다시 preflight한다.

Luna max `task_0647814ad8ed` / `ctx_46b456ced671`가 `world_connector.go`와
`world_connector_notepad_test.go`만 변경했고, coordinator가 notepad world/session/transport race,
full transport race, vet, gofmt, diff-check, scope와 `src/frp.new` SHA를 독립 검증했다.
커밋은 `12c212a`로 push됐으며 C `view_file` paging·receipt 크기·cross-connection recovery,
PG/브라우저/배포 인수는 여전히 미완료다.

## 2026-09-16 G3 notepad durable receipt replay follow-up

저장 성공 뒤 응답/receipt 확인이 끊긴 경우, `commitPending` 상태의 재시도 dot은 최신
canonical limit preflight로 draft를 지우지 않고 동일 command ID의 durable receipt replay를
먼저 시도한다. 첫 dot의 limit admission과 dot 직전 canonical 변경 거절은 그대로 유지한다.

Luna max `task_d67d7330185b` / `ctx_a49ac2f67804`가 두 connector 파일만 변경했고, coordinator가
notepad world/session/transport race, full transport race, vet, gofmt, diff-check, exact scope와
`src.frp.new` 보호 hash를 독립 검증했다. 커밋 `f76f8b4`는 PR #13의 `private` 원격 branch에
push됐다. 독립 리뷰 `task_801c59af8716` / `ctx_a67b7174caf6`는 P0/P1/P2 없음과 기존 saved-
before-error P3 해결을 확인했지만, 첫 dot preflight와 durable reducer 사이에 다른 writer가
한도를 채우면 fresh `ErrNotepadLimit`이 retry-only draft를 남길 수 있는 별도 P3를 기록했다.
해당 edge와 C `view_file` paging·receipt 크기·cross-connection recovery, PG/브라우저/배포
인수는 후속 작은 PR 범위로 남긴다.

## 2026-09-16 G3 notepad post-preflight rejection follow-up

첫 dot preflight 이후 durable reducer가 최신 canonical 상태의 `world.ErrNotepadLimit`을
반환한 경우는 저장 불확실성이 아닌 확정된 새 거절이다. transport는 이 경우에만
connection-local draft를 비우고 안전한 unknown 응답을 반환하며, 일반 storage/transport
오류는 stable command ID와 `commitPending`을 보존해 receipt-first 재시도를 계속한다.

Luna max `task_7009f5994d69` / `ctx_147a136de3c6`가 `world_connector.go`와
`world_connector_notepad_test.go`만 변경했고, coordinator가 새 interleaving 회귀와 기존
saved-before-error receipt 회귀를 포함한 transport/world/session race, full transport race,
vet, gofmt, diff-check, exact scope 및 `src.frp.new` 보호 hash를 독립 검증했다. 커밋은
`7781b46`이며 작은 PR #14로 push됐다. C `view_file` paging·receipt 크기·cross-connection
recovery, PG/브라우저/배포 인수는 여전히 미완료다.

## 2026-09-15 G4 실제 rooms/NPC/item full-data dry-run

| 원작/운영 경계 | Go 검증 결과 | 검증/남은 조건 |
| --- | --- | --- |
| 전체 데이터 이관의 rooms·NPC·item 입력 경계와 side-effect 없는 사전 점검 | `go run ./cmd/muhan -full-data-dry-run -full-data-rooms ../rooms`를 두 번 실행해 모두 예상된 `full-data dry-run missing-input`(exit 1)을 반환. 누락 입력은 `players`, `social_family_manifest`, `social_memo_manifest`, `social`, `bank`, `vote`; 제공된 rooms source 3,216개에서 canonical rooms 2,341개·NPC 639개·item node 8,622개를 재현했고 room digest `5e90290dc9e0f6d7190e4c80f13a2ef82fcd9f1be0289c6fc43f678a8115d85a`, graph digest `050ae89113b6a1b086ee1d4369bc628cda73493761c2ddc1648f87b4a7b90d35`를 확인 | Luna max `ctx_1c5d3f67da39` / `task_94a473d75945`, report `/tmp/g4-full-data-actual-npc-item-report.md`. 두 stdout byte-identical SHA-256 `96c8eceab1350e7eb1fdbc8411aae21046545282e54322c376bf530b96fab6a3`; `database_opened=false`, `listener_started=false`, `outputs_written=false`. worker race 2회·coordinator race·vet·JSON/status/hash/diff-check PASS 후 release. 전체 manifests/data, fencing/recovery, PG backup/PITR/cutover는 미완료 |

## 2026-09-15 `update.c` NPC MPOISS→PPOISN

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `src/update.c:471-490` ordinary NPC hit effect order | 성공 hit 뒤 `MPOISS` bit 13의 정확한 `1..100` 1회 추첨, `<=15`이면 victim `PPOISN` bit 16 설정. world proposal/apply와 durable NPC tick summary/state, lethal probe/replay에 `Poisoned`를 보존하고 miss·non-poisoner·invalid/tampered/stale 후보는 fail-closed | Luna max `ctx_8a120016169c` / `task_b67992572ae2`, report `/tmp/g7-npc-poison-report.md`, worker_done `msg_596526509b82`; coordinator race 2회·vet·gofmt·diff-check·4파일 범위 PASS 후 release. disease/blind/dissolve/breath, producer/tick/AI/추종/리젠, NPC corpus·PG·ARM64·browser/deploy는 미완료 |

## 2026-09-16 `update.c` NPC MDISIT→dissolve_item

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `src/update.c:507-509`, `src/command10.c:442-480` ordinary NPC hit의 MDISIT 및 `dissolve_item` | 성공 hit 뒤 poison·disease·blind 다음 `MDISIT` `1..100` 1회와 ready 후보 선택 RNG를 순서대로 기록한다. canonical `ItemCollection.Ready` 20칸 전체(held/wield 포함)를 원작 순서로 후보화하고, `ONEWEV`는 선택 RNG를 소비한 뒤 보존하며 그 밖의 선택 root와 전체 ID subtree를 삭제하고 armor/THAC0를 갱신한다. durable NPC phase summary/state와 lethal probe/replay도 동일 결과를 보존하고 replay에서 RNG를 재소비하지 않는다. nil/legacy inventory·invalid/tampered candidate는 fail-closed | Luna max `task_a78c32c7c9c6` / `ctx_7708688f29cf`, report `/tmp/g7-npc-dissolve-item.md`, worker_done `msg_4649aa507ca3`; coordinator가 world `TestNPCCombatRound` race, transport 전체 race, vet, gofmt·diff-check 및 정확한 4파일 범위를 독립 재검증하고 worker release/ack를 완료했다. 커밋 `2e6b483`와 PR #15를 push했으며 PR은 review 대기로 OPEN 유지한다. 전체 world corpus의 기존 room-body 63건 및 NPC admission fixture 2건 실패는 baseline/out-of-scope로 남으며, #7은 OPEN/In Progress/G3이고 전체 NPC tick/AI/리젠·PG/ARM64/browser/deploy 인수는 미완료 |

## 2026-09-15 `command5.c:who` 중앙 라우팅

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command5.c:who` bare/long status | 중앙 `ParseCommand`가 exact `누구`·`누구 l`을 `CommandSocial`로 연결하고, 기존 social handler가 long `종족` projection·read-only receipt/replay를 처리한다. 대소문자·추가 토큰·control/newline·`그룹 l`·`무리 l`은 fail-closed | session/transport targeted race 2회, `go vet`, `gofmt`, `git diff --check`, 범위·보호 hash PASS. 채팅·외침·귓말·게시판·우편·그룹·가족·관리 전체, full command ledger, PG/E2E/browser/mobile/deploy는 미완료 |

## 2026-09-14 display_rom 전투 안내

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `room.c:display_rom` first_enm | list_obj 뒤 first_mon first_enm + find_crt(first_ply,1). 자신=`이/가 당신과 싸우고 있습니다`, 타인=`이/가 님과/와 싸우고 있습니다`. CurrentScene/SceneAt이 look·peek·가 도착에 사용. RoomID look 불변, replay 무중복. nil Enemies·Damage<0·legacy Monsters fail-closed | world/session/transport targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. `나`/first_ply, board/special_obj, consider/equip_list, HP/광채/broadcast, 63방 corpus, 운영 PG는 미완료 |

## 2026-09-14 look find_obj 인벤토리·장비·방 순서

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command2.c:look` find_obj order | find_ext miss 뒤 ply Inventory(OINVIS/PDINVI) → ready[](EQUAL, OINVIS skip 없음) → 방 Inventory. 리스트별 occurrence. RoomID 불변, replay 무중복. 없는 대상·legacy 인벤/바닥/몬스터·special은 fail-closed | world/session/transport targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. `나`/first_ply, board/special_obj, consider/equip_list, HP/광채/broadcast, 63방 corpus, 운영 PG는 미완료 |

## 2026-09-14 look 방 객체·생물 `봐 검`/`늑대 봐`

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command2.c:look` find_obj/find_crt | find_ext miss 뒤 방 first_obj prefix/occurrence·OINVIS/PDINVI, 이어서 first_mon find_crt. 설명/`특별한 점이 없습니다`/`당신은 N을 봅니다`+설명. RoomID 불변, replay 무중복. 없는 대상·legacy floor/monster·special은 fail-closed | world/session/transport targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. 인벤토리/장비/`나`/first_ply, board/special_obj, consider/equip_list, HP/광채/broadcast, 63방 corpus, 운영 PG는 미완료 |

## 2026-09-14 look last-token `동 봐`

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command1.c:parse` last-token verb + `command2.c:look` | `ParseLookLine`/`ParseCommand`가 `동 봐`/`북 보다`를 CommandLook peek로 분류. `ExecuteDirectionalLine`은 look suffix 거절. RoomID 불변, 동일 command ID replay 무중복 | world/session/transport targeted `-race` 2회, session/transport `gofmt`/`go vet`, `git diff --check` PASS. `나`/first_ply, consider/equip_list, 63방 corpus, 운영 PG는 미완료 |

## 2026-09-14 G0 매핑 권위

명령 343행/handler 174개 인수 항목, 계정·월드·전투·NPC·경제·마법·사회·관리·저장 계약, C 근거/fixture/Go 위치/검증/이관/남은 조건은 [2026-09-14 G0 원장 재집계](#2026-09-14-g0-원장-재집계--343-등록-행--174-handler)가 우선한다. 아래 날짜별 슬라이스 노트는 구현 로그이며 전체 G0 분모가 아니다.

## 2026-09-14 소환 — summon bind

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `magic5.c:summon` / `src/global.c` spllist `소환` SSUMMO | CommandCast `주문 소환 <name>`. MP 50/100, 습득 비트, 51% 실패는 항상 -50, find_who 후 CAST MP 차감, dest occupancy/family/level/PNOSUM, source RNOLEA. 성공 occupancy+BeenHere+LT_SPELL. 미이관 점유/NPC active/perm spawn fail-closed. replay 무중복 | world/session targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. transport ExcludeTargetID, 도착 방송, 공격/맵 주문, 운영 PG는 미완료 |

## 2026-09-13 기억 — moon_set bind

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command8.c:moon_set` / `src/global.c` cmdlist-153 `기억` | suffix `기억`/`<물건> [#] 기억`. 광장 1001 거부, value==1001만 기록. canonical 인벤토리 exact 이름, description·key[1]·value=rom_num 원자 receipt. broadcast_rom 2줄은 같은 방·actor 제외, replay 무중복. 미이관 인벤토리는 fail-closed | world/session/transport targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. EQUAL prefix/key/OINVIS, 장비·중첩 조회, 문주 사망 방송, 운영 PG는 미완료 |

## 2026-09-13 `*떨어져라`/`*침공` — dm_moonstone / dm_monster

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `dm5.c:dm_moonstone` / `*떨어져라` | CARETAKER 게이트, object 640, RNOTEL 제외 방 주사위, shotsmax+1..20, 바닥 item graph, PNOBRD broadcast, replay 무중복 | targeted race/vet PASS. 원작 RMAX 전수 파일 load_rom, 짧은 방이름 silent return의 전체 룸 트리 대조는 미완료 |
| `dm5.c:dm_monster` / `*침공` | 10회 방 3601–3630·몬스터 265–299, canonical NPC 원장 필수, 두 broadcast, replay 무중복 | targeted race/vet PASS. 원작 load_rom/load_crt 실패 무시 동작과 대량 원본 몬스터 템플릿 대조는 미완료 |

## 2026-09-13 선전포고 — call_war declare/cancel/accept

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `special1.c:call_war` / `src/global.c` cmdlist-72 `선전포고` | `PlanFamilyWar`/`ApplyFamilyWar`가 PFMBOS·catalog exact-name·온라인 두목 게이트와 `FamilyWar` CALLWAR/AT_WAR 전이를 원자 receipt로 고정. 선언은 broadcast_all, 취소/수락은 PNOBRD broadcast. 미이관 War는 fail-closed. replay는 commit·fan-out을 재실행하지 않는다. | world/session/transport targeted `-race` 2회, `gofmt`/`go vet`, `git diff --check` PASS. 문주 사망 종료와 운영 PG 영속, 전체 C 출력 parity, 패거리 보상은 미완료 |

## 2026-09-13 패거리공지 — family_news view/append/delete

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `post.c:family_news` / `src/global.c` cmdlist-148 `패거리공지` | `FamilyNewsState` canonical `family_news_<n>` 원장, `PlanFamilyNewsView`/`PlanFamilyNewsAppend`/`PlanFamilyNewsDelete`→`ApplyFamilyNews`. PFAMIL·catalog family ID, 두목만 unlink(`d`), 회원 `a` 편집기는 connection-local newsedit(줄마다 79바이트 append, `.` 종료). 미이관 원장·catalog 부재는 fail-closed. 최초 commit만 본문을 바꾸고 동일 command ID replay는 commit·RNG·fan-out을 재실행하지 않는다. | world/session/transport targeted `-race` 2회, 영향 패키지 `gofmt`/`go vet`, `git diff --check` PASS. 전체 C view_file 페이지/PREADI/출력 parity, 패거리 전쟁·보상, 운영 PG import, ARM64/browser E2E는 미완료 |

`패거리공지`는 cmdlist 148의 다음 미구현 player alias였다. 가입/탈퇴/추방 replay 경계는 재구현하지 않고 기존 회귀만 재확인했다.

## 2026-09-10 플레이어 주문 공포·봉합구 — canonical 상태 전이

공포(SFEARS/PFEARS/LT_FEARS)와 봉합구(SSILNC/PSILNC/LT_SILNC)를 player self-cast receipt로
연결했다. 공포는 source 지속시간 난수와 spell-fail의 소비 순서, INT·PRMAGI 보정을 검증하고,
봉합구는 MP 12·SUB_DM gate·고정 3,600초와 PRMAGI 절반 보정을 검증한다. 성공 시 PINVIS·
주문 timer·MP를 원자 반영하고 실패·replay에서는 RNG·출력 fan-out을 반복하지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 실제 PostgreSQL,
ARM64, browser/IME, release/testnet 및 전체 spell parity는 승격 cadence에서 단일 게이트로
수행한다.

## 2026-09-10 플레이어 주문 봉합구 — canonical 상태 전이

봉합구(SSILNC/PSILNC/LT_SILNC)를 player self-cast receipt로 연결했다. source MP 12와
SUB_DM 이상 직업 gate, 고정 3,600초 지속 시간, PRMAGI 절반 보정 및 PINVIS 해제를 검증한다.
성공 시 PSILNC·global LT_SPELL·MP를 원자 반영하고, 실패·replay에서는 상태와 출력이
중복되지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 실제 PostgreSQL,
ARM64, browser/IME, release/testnet 및 전체 spell parity는 승격 cadence에서 단일 게이트로
수행한다.

## 2026-09-10 플레이어 주문 실명 — canonical 상태 전이

실명(SBLIND/PBLIND)을 player self-cast receipt로 연결했다. source MP 15, SUB_DM 이상
직업 gate, spell-fail 경계를 검증하고 성공 시 PBLIND를 켜고 PINVIS를 해제한다. global
LT_SPELL·MP 전이는 원자 적용하며 실패·replay에서는 난수·출력 fan-out을 반복하지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 실제 PostgreSQL,
ARM64, browser/IME, release/testnet 및 전체 spell parity는 승격 cadence에서 단일 게이트로
수행한다.

## 2026-09-10 플레이어 주문 저주해소 — canonical 장착 아이템 정화

저주해소(SREMOV/PFEARS/OCURSE)를 player self-cast receipt로 연결했다. source MP 18과
spell-fail RNG 1회를 검증하고, 성공 시 장착된 ready root의 OCURSE만 제거하며 인벤토리와
컨테이너 항목은 건드리지 않는다. PFEARS 해제·global LT_SPELL·MP 차감과 해제 개수/아이템
projection을 원자적으로 기록하고, canonical item graph가 없는 상태에서는 난수·비용 없이
fail-closed한다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 실제 PostgreSQL,
ARM64, browser/IME, release/testnet 및 전체 spell parity는 승격 cadence에서 단일 게이트로
수행한다.

## 2026-09-10 플레이어 주문 전투 강화 2종 — canonical combat stat 전이

성현진(SBLESS/PBLESS/LT_BLESS)과 수호진(SPROTE/PPROTE/LT_PROTE)을 player
self-cast receipt로 연결했다. source MP 10, spell-fail RNG 1회, INT 기반 지속 시간,
클레릭/팔라딘 레벨 band term, RPMEXT +800을 검증한다. 성공 시 canonical
ItemCollection.CombatStats를 사용해 성현진은 THAC0, 수호진은 방어력을 flag 반영 후
갱신한다. 장비 graph가 없는 legacy-only body는 난수·MP를 소비하지 않고 fail-closed하며,
재생은 RNG와 출력 fan-out을 반복하지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity,
실제 PostgreSQL, ARM64, browser/IME, release/testnet은 승격 cadence에서 단일 종합 게이트로
수행한다.

## 2026-09-10 플레이어 `주문` 발광 — canonical light flag/timer 전이

`발광`(SLIGHT/PLIGHT/LT_LIGHT)을 self-cast receipt로 연결했다. source MP 5와 `spell_fail`
RNG 1회, 레벨 band 기반 interval `300 + band×300`, `RPMEXT` +600을 명시적으로 계산한다.
성공 시 PLIGHT/LT_LIGHT와 global LT_SPELL을 원자 반영해 현재 장면의 어두운 방 판정이
canonical 상태를 사용하고, 실패·재생에서는 RNG·출력을 다시 소비하지 않는다.

검증: world focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity, 실제 PostgreSQL,
ARM64, browser/IME, release/testnet은 승격 cadence에서 단일 종합 게이트로 수행한다.

## 2026-09-10 플레이어 `주문` 자기 대상 정화 3종 — canonical flag 전이

`해독`(SCUREP/PPOISN), `치료`(SRMDIS/PDISEA), `개안술`(SRMBLD/PBLIND)을 self-cast
receipt로 연결했다. source MP 비용(6/12), 직업 gate(치료는 클레릭·상위, 개안술은 클레릭·
팔라딘·상위), 습득 비트와 `spell_fail` 1회 RNG를 검증하고, 성공 시 대상 상태 flag를 지운다.
global LT_SPELL과 MP 차감은 원자 반영되며, 실패·replay에서는 대상 상태와 RNG 소비가 중복되지
않는다. 원작 `cast()`의 PBLIND 선행 gate도 유지한다.

검증: world focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity, 실제 PostgreSQL,
ARM64, browser/IME, release/testnet은 승격 cadence에서 단일 종합 게이트로 수행한다.

## 2026-09-10 플레이어 `주문` 자기 대상 지속 버프 7종 — canonical flag/timer 전이

`부양술`(SLEVIT/PLEVIT/LT_LEVIT), `방열진`(SRFIRE/PRFIRE/LT_RFIRE), `비상술`
(SFLYSP/PFLYSP/LT_FLYSP), `보마진`(SRMAGI/PRMAGI/LT_RMAGI), `방한진`
(SRCOLD/PRCOLD/LT_RCOLD), `수생술`(SBRWAT/PBRWAT/LT_BRWAT), `지방호`
(SSSHLD/PSSHLD/LT_SSHLD)를 player self-cast reducer에 연결했다. source MP 비용, 주문별
기본 interval(부양술 2400, 나머지 1200), INT/`MAX(300, ...)` 규칙과 `RPMEXT` 가산(+600 또는
+800)을 canonical receipt에 고정하고, 7종 모두 `spell_fail` RNG 1회만 계획 단계에서 소비한다.
성공 시 global LT_SPELL과 주문 flag/timer를 함께 원자 반영하며 replay는 RNG·fan-out을
재실행하지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity,
실제 PostgreSQL, ARM64, browser/IME, release/testnet은 승격 cadence에서 단일 종합 게이트로
수행한다.

## 2026-09-10 플레이어 `주문` 자기 대상 지속·감지 주문 — canonical flag/timer 전이

`주문` self-target reducer에 `은둔법`(SINVIS/PINVIS/LT_INVIS), `은둔감지술`(SDINVI/PDINVI/
LT_DINVI), `주문감지술`(SDMAGI/PDMAGI/LT_DMAGI), `선악감지`(SKNOWA/PKNOWA/LT_KNOWA)를
추가했다. source MP 비용과 INT/레벨/Mage/RPMEXT 지속 시간, spell bit 및 timer slot을
receipt에 고정한다. 은둔·감지 3종만 source `spell_fail` RNG 1회를 사용하고 선악감지는
원작처럼 RNG 없이 성공한다. 성공 시 flag/timer·global LT_SPELL·MP를 원자 반영하고, 전투 중
은둔법은 deterministic no-op으로 처리해 replay에서 난수·비용을 재실행하지 않는다.

검증: world/session/transport focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity,
실제 PostgreSQL, ARM64, browser/IME, release/testnet 검증은 승격 cadence에서 단일 종합 게이트로
수행한다.

## 2026-09-10 플레이어 `주문` 자기 대상 회복 — session/transport 연결

원작 `magic1.c:cast`의 self-target 입력 경계를 Go session parser에 추가하고, `주문` prompt와
`회복`(SVIGOR)·`원기회복`(SMENDW)·`완치`(SFHEAL)를 world reducer로 연결했다. source의
MP 비용, 습득/직업/일일 한도/주문 timer, `spell_fail`, INT·신앙·레벨·`RPMEXT` 주사위
순서를 proposal/result receipt에 고정한다. 성공·실패·게이트(주문 이후 PHIDDN 해제 포함)는
원자적으로 적용하며, retry/replay는 RNG·상태·room event를 재실행하지 않는다. 대상 인자를
붙인 주문과 미이관 주문은 fail-closed한다.

검증: world/session/transport focused race 테스트와 영향 패키지 vet, diff check PASS.
전체 spell parity·실제 PostgreSQL·ARM64·browser/IME·release/testnet은 승격 cadence에서
단일 종합 게이트로 검증한다.

## 2026-09-10 NPC 대화 `CAST` 회복 묶음 — canonical HP 전이

`회복`(SVIGOR)·`원기회복`(SMENDW)의 대상 플레이어 분기를 NPC talk reducer에 연결했다.
원작 주문의 MP 비용(2/4), INT·신앙 보너스, 클레릭/팔라딘 레벨 보너스, 1d6/2d6 및
`RPMEXT` 추가 주사위를 source 순서로 계산한다. `회복`은 Barbarian/Fighter, `원기회복`은
Assassin/Barbarian/Fighter만 `spell_fail`을 호출하며, 그 외 직업은 실패 RNG 없이 진행한다.
모든 효과 주사위와 HP delta는 proposal/result에 보존하고 성공·실패·재생을 원자 검증한다.

검증: world NPC talk focused race, 영향 패키지 vet, diff check PASS. 전체 spell parity·
PostgreSQL·ARM64·browser·release는 승격 cadence에서만 실행한다.

## 2026-09-10 NPC 대화 `CAST` 완치 — canonical HP 전이

`완치`(SFHEAL)를 NPC talk receipt 경계에 연결했다. 원작의 클레릭·팔라딘·상위 직업
게이트와 MP 20을 검증하고, 원작의 `heal` 루틴에 `spell_fail`이 없는 점을 반영해 RNG 없이
성공 시 canonical actor의 HP를 HPMax로 설정한다. MP 부족·직업 게이트에서는 대상 HP를
바꾸지 않으며, 성공 출력은 receipt에 고정해 replay에서 중복 실행하지 않는다.

검증: `go test -race ./internal/world -run 'NPCTalkCast(Heal|Detection|TimedUtility)' -count=1`,
영향 패키지 vet, diff check PASS. 전체 spell parity·PostgreSQL·ARM64·browser·release는
승격 cadence에서만 실행한다.

## 2026-09-10 NPC 대화 `CAST` 은둔 계열 — `은둔법`

`은둔법`(SINVIS/PINVIS/LT_INVIS)을 감지 계열과 같은 canonical NPC talk receipt
경계에 추가했다. NPC의 spell bit·MP·`spell_fail`을 확인하고, Mage 지능 보정과
`RPMEXT` +600을 source 계산대로 적용한다. 성공 시 actor의 PINVIS/timer와 NPC MP를
원자 반영하며, 실패·replay에서는 actor 상태와 출력이 중복 변경되지 않는다.

검증: 기존 감지/지속 버프와 함께 world focused race 테스트 및 vet를 통과했다.
전체 spell parity·PostgreSQL·ARM64·browser·release는 승격 cadence에서만 실행한다.

## 2026-09-10 NPC 대화 `CAST` 감지 계열 3종 — canonical 상태 전이

`은둔감지술`(SDINVI/PDINVI/LT_DINVI), `주문감지술`(SDMAGI/PDMAGI/LT_DMAGI),
`선악감지`(SKNOWA/PKNOWA/LT_KNOWA)를 기존 NPC talk receipt 경계에 연결했다. 원작의
NPC spell bit·MP·`spell_fail` RNG를 검증하고, 감지 주문의 지능/직업 보정과 `RPMEXT`
가산을 source slot에 맞게 계산한다. 성공 시 actor flag/timer와 NPC MP를 원자 반영하고,
실패는 NPC MP만 차감한다. room/actor 출력은 receipt에 고정해 replay에서 재방송하지 않는다.

검증: `go test -race ./internal/world -run 'NPCTalkCast(Detection|TimedUtility)' -count=1`
PASS. 전체 spell parity·PostgreSQL·ARM64·browser·release 게이트는 승격 cadence에서만 수행한다.

## 2026-09-10 NPC 대화 `CAST` 지속 버프 6종 — canonical 상태 전이

`command8.c:talk_action`의 비공격 주문 `부양술`(SLEVIT/PLEVIT/LT_LEVIT), `방열진`
(SRFIRE/PRFIRE/LT_RFIRE), `비상술`(SFLYSP/PFLYSP/LT_FLYSP), `보마진`
(SRMAGI/PRMAGI/LT_RMAGI), `방한진`(SRCOLD/PRCOLD/LT_RCOLD), `지방호`
(SSSHLD/PSSHLD/LT_SSHLD)를 동일한 snapshot-bound receipt로 연결했다. 주문별 MP 비용과
`spell_fail` RNG 1회를 기록하고 성공 시 대상 flag/timer를 원자 적용한다. 지속 시간은
원작의 부양술 2400초, 공통 1200초와 `RPMEXT` 가산(비상술 +600, 나머지 +800)을 보존하며,
대상 inventory/equipment가 없는 효과 주문도 허용한다. room/actor 투영은 receipt에만 저장해
재생 시 재방송하지 않는다.

검증: world/session/transport NPCTalk focused race, 영향 패키지 vet, diff check PASS.
전체 spell parity·PostgreSQL·ARM64·browser·release 검사는 승격 cadence에서만 수행한다.

## 2026-09-10 NPC 대화 `CAST` 정화·수생술 — canonical 상태 전이

`command8.c:talk_action`의 비공격 주문 중 `해독`(SCUREP/PPOISN), `치료`(SRMDIS/PDISEA),
`개안술`(SRMBLD/PBLIND)을 canonical body 정화 전이로 연결했다. NPC 주문 bit·직업 gate·
도력·적대 관계를 확인하고 `spell_fail` RNG 1회를 계획 단계에서 기록한다. 성공은 대상
상태 플래그 제거와 NPC MP 6 또는 12 차감을 원자 적용하며, 실패는 MP만 차감한다. 세
주문은 target inventory/equipment가 필요하지 않아 미이관 actor도 안전하게 처리한다.
동일 경계에 `수생술`(SBRWAT/PBRWAT)을 추가해 inventory 없이 LT_BRWAT 1200초 timed flag를
설치한다. room/actor projection은 receipt에 저장하고 replay에서는 재실행하지 않는다.

검증: world/transport NPCTalk focused race PASS. session parser는 기존 CAST 경계를
재사용했다. PostgreSQL/browser/ARM64/release 및 전체 spell parity는 승격 cadence에서만
실행한다.

## 2026-09-10 NPC 대화 `GIVE` — canonical object graph 지급

`command8.c:talk_action`의 `GIVE <object-number>`를 server-owned `SpawnCatalog`와
item-ID allocator 경계에 연결했다. object 번호·이름·무게·quest를 검증하고 `ORENCH`는
계획 단계에서 결정론적 1회 RNG를 소비한다. 대상 actor의 canonical `ItemCollection`이
무게/용량 gate를 통과하면 새 ID로 object subtree를 materialize하고, quest bit·`quest_exp`·
`add_prof`를 같은 snapshot-bound receipt에 반영한다. 성공·quest 보상·room/actor 출력
순서를 receipt에 저장하며 replay에서는 catalog/allocator/RNG를 재호출하지 않는다.

용량 초과와 이미 완료한 quest는 원작의 topic 응답 뒤 actor 거절 메시지만 남기는 durable
rejection으로 처리한다. object catalog, canonical inventory, allocator, malformed tree는
receipt 전에 fail-closed한다. post-state만으로 gift ID/난수/거절 순서를 재구성할 수 없으므로
`RoomNPCTalkEvent`는 receipt projection을 요구한다.

검증: world/session/transport NPCTalk focused race, 영향 패키지 vet, diff check 통과.
ARM64·실제 PostgreSQL·browser/IME·release matrix·testnet은 승격 cadence에서만 실행한다.

## 2026-09-10 NPC 대화 `ACTION` — canonical 감정표현 연결

`TalkCatalog`의 `ACTION` 중 이미 `action.c`와 대조된 닫힌 Go 감정표현 alias만
snapshot-bound NPC action으로 admit했다. `PLAYER` 대상은 대화한 canonical player로
고정하고, 대상 없는 표현은 원작처럼 NPC descriptor만 제외한 room broadcast로 투영한다.
NPC의 `MHIDDN`은 action 호출 순서대로 해제하며 `PSILNC`인 NPC는 topic 응답만 남기고
action projection을 억제한다. room/actor 메시지와 action target ID는 receipt에 저장하고
replay에서는 재선택·재전송하지 않는다. 알 수 없는 alias/target은 계속 fail-closed하며,
`GIVE`는 별도 object catalog/allocator 경계를 통해 연결되어 있다.

검증: `go test -race ./internal/world ./internal/session ./internal/transport -run 'NPCTalk|WorldConnectorSubmitDispatchesNPCTalk' -count=1`, 영향 패키지 `go vet`,
`git diff --check` PASS. 고비용 PostgreSQL/browser/ARM64/release 게이트와 알려진 전체
corpus 회귀는 이번 작은 계약에서 반복하지 않았다.

## 2026-09-10 NPC 대화 `CAST` — 성현진·수호진

`TalkCatalog`의 `CAST` action 중 source `spllist`와 일치하는 `성현진`(SBLESS)·`수호진`(SPROTE)만
admit했다. NPC의 spell bit·MP·적대 관계와 canonical target equipment를 snapshot-bound로
검증하고, `spell_fail`과 동일한 class/지식 확률표의 RNG 1회를 receipt에 기록한다. 성공은
NPC MP 10 차감과 플레이어 효과 bit/timer 및 해당 전투 수치 재계산을 원자 적용하고, 실패는
MP만 차감한다. receipt event는 최초 commit 뒤에만 room/actor cast projection을 전송하며,
post-state에서 outcome을 추측하는 `RoomNPCTalkEvent`는 fail-closed한다. 다른 CAST 주문과
운영 원본 대량 대조·PG/browser/release 검증은 아직 미완료다.

검증: `go test -race ./internal/world ./internal/session ./internal/transport -run 'NPCTalk' -count=1`, 영향 패키지 `go vet` PASS. 전체 세 패키지 실행은 기존 strict room corpus 63건과 family broadcast 회귀로 실패했다.

## 2026-09-10 NPC 대화 `ATTACK` 액션

`TalkCatalog`의 exact topic이 `ATTACK` action을 가질 때 `PlanNPCTalkProposal`/`ApplyNPCTalk`가
원작의 질문·응답 뒤 공격 메시지와 NPC→플레이어 enemy 관계를 하나의 결정론적 receipt
event로 저장한다. actor/room 메시지 순서를 보존하고 replay에서는 재전송하지 않는다.
`성현진`·`수호진` CAST와 `ACTION`·`GIVE`는 canonical effect boundary를 통과했으며, 그
밖의 CAST는 미확인 side effect라 계속 fail-closed한다.

world/session/transport NPCTalk focused race 및 영향 패키지 vet가 통과했다. 실제
PostgreSQL/browser/ARM64/release 검사는 관련 계약이 승격되는 cadence에서만 실행한다.

## 2026-09-10 `직업전환` xterm confirmation

bare `직업전환`을 connection-local `예/아니오` continuation으로 연결했다. 서버가
snapshot-bound gate를 먼저 확인한 뒤 prompt를 표시하고, `예`일 때만 기존
`ExecuteChangeClassLineWithOptions("직업전환 예")` receipt를 생성한다. `아니오`·gate
실패는 receipt-free이며, transient 저장/응답 오류에서는 동일 command ID를 유지해
재시도한다. 직접 `직업전환 예` 형식은 기존 호환 경로로 남긴다.

`go test -race ./internal/session ./internal/transport -run 'ChangeClass|WorldConnectorBareChangeClass' -count=1`와
영향 패키지 `go vet`가 통과했다. 실제 PG/browser/ARM64/release 검사는 관련 계약이
승격되는 cadence에서만 실행한다.

## 2026-09-10 패거리 탈퇴 전역 알림

활성 회원 탈퇴 receipt에 원작의 전역 `### ... 탈퇴` 알림을 추가했다. 알림은 actor ID와
PNOBRD 수신자 정책을 함께 고정하고 최초 commit 뒤에만 fan-out하며 replay에서는 재전송하지
않는다. actor의 탈퇴 fee/member ledger 원자 전이는 기존 reducer를 그대로 사용한다.

`go test -race ./internal/world ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 영향 패키지 `go vet`가 통과했다.

## 2026-09-10 패거리 가입 신청 알림

확인된 `패거리가입` receipt에 원작의 두목 대상 신청 알림을 담고, 첫 commit 뒤에만
정확한 canonical boss connection으로 전달하도록 연결했다. receipt replay에서는 알림을
재전송하지 않으며, 수신자 ID/name은 reducer가 캡처한 값과 post-commit snapshot을 다시
대조한다.

`go test -race ./internal/world ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 영향 패키지 `go vet`가 통과했다.

## 2026-09-10 `패거리탈퇴` 원작 confirmation 연결

활성 패거리원의 bare `패거리탈퇴`를 xterm connection-local 확인 단계로 연결했다.
`예` 전에는 상태·영수증을 변경하지 않고, 확인 시에만 기존 family fee/member ledger를
검증하는 원자 reducer를 호출한다. `아니오`는 즉시 취소하며, pending 신청 취소 경로와
두목/미이관 ledger fail-closed 경계는 기존 계약을 유지한다. 저장 오류에서는 같은 command
ID를 보존해 재시도한다.

`go test -race ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 `go vet ./internal/session ./internal/transport`가 통과했다.

## 2026-09-10 `패거리가입` 원작 continuation 연결

bare `패거리가입`을 웹 xterm의 connection-local selection/confirmation 흐름으로
연결했다. 서버 소유 `FamilyCatalog`을 먼저 목록으로 보여주고, exact family name을
선택한 뒤 `예`를 입력할 때만 기존 `PlanFamilyJoinByName`→`ExecuteGame` receipt를
호출한다. 목록·잘못된 선택·`아니오`는 receipt-free이며, commit 오류는 동일 command ID와
선택을 유지해 재시도한다. actor/boss/catalog 권위는 기존 world reducer가 다시 확인하고
이름으로 ID를 추측하지 않는다.

`go test -race ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 `go vet ./internal/session ./internal/transport`가 통과했다. 실제 PG/browser/release
게이트는 이 변경이 해당 계약에 영향을 주는 cadence에서만 실행한다.

## 2026-09-10 Go + PostgreSQL 브라우저 수직 경로

`bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`를 한 번
실행해 실제 Go 서버와 웹 xterm의 캐릭터 생성→월드 입장→명령→재로그인, canonical
캐릭터 중복 세션 거부, 모바일 viewport 포커스·입력을 검증했다. `3 passed (17.7s)`이며
작업별 PostgreSQL 컨테이너는 종료 시 자신이 만든 것만 제거했다. 로컬 수직 경로는
증명했지만 운영 Supabase/RLS·WSS/Ingress·testnet은 미완료다. 브라우저 게이트는 관련
코드 변경 또는 release cadence에서만 재실행한다.

## 2026-09-10 투표 raw→manifest builder·CLI 경계

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| raw + ISSUE + operator mapping | `cmd/muhan -build-vote-manifest-root`가 `LocateLegacyVoteRawFilesV1`와 명시적 `post/ISSUE` digest를 다시 검증하고, 모든 legacy name→immutable player ID 매핑을 exact하게 결합해 `vote-state-v1` manifest를 만든다. 선택지 bytes/SHA-256은 evidence로 남기되 raw path·metadata·credential은 canonical state로 전달하지 않는다. | `cmd/muhan` race/CLI DB-free dry-run·write·path-only validation·same-byte replay PASS. 실제 원본 대량 수집, operator character 대조·승인, Supabase RLS/운영 복구는 미완료 |
| manifest read/apply | `-import-vote-manifest`는 private 0600 JSON과 ballot/digest/ID를 DB 전에 검증하고, `-import-vote-manifest-apply`만 `Postgres.ImportVoteState`를 호출한다. build/import/runtime mode 조합은 거부한다. | full local integration PASS 후 ARM64 PG apply/replay/restore와 운영 PITR·배포 검증이 남아 있음 |

mapping schema와 실행 절차는 `docs/porting-research/go-vote-state-manifest.md`를 따른다.
builder output은 mapping과 같은 private 0700 디렉터리의 immutable 0600 파일이며, source
root와 겹치거나 변경된 bytes로 재실행하면 fail-closed한다.

## 2026-09-10 투표 canonical 원장 PostgreSQL 경계

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| canonical ballot import | `Postgres.ImportVoteState`가 명시적 `CatalogDigest`와 immutable player-ID keyed `VoteState`를 검증하고, unresolved world snapshot에만 원자적으로 설치한다. `vote_imports`와 `world_commands`를 함께 기록하며 동일 command/aggregate만 replay한다. raw path·bytes·credential·이름 기반 claim은 거부한다. | `TestNormalizeVoteStateImportRejectsSensitiveDuplicateAndDigestMismatch`, ARM64 `postgres:17-alpine` import/replay/foreign-aggregate guard, 전체 local integration PASS. 실제 raw→manifest builder/operator mapping·대량 원본·Supabase RLS/운영 복구는 미완료 |

이 경계는 투표 파일을 직접 읽지 않는다. `LocateLegacyVoteRawFilesV1` 결과와 ISSUE digest를
사람이 검토한 manifest로 결합한 뒤에만 `ImportVoteState`에 제출해야 하며, 운영 전환 전에는
재접속·백업 복원·중복/누락 대조를 별도 증거로 확보한다.

## 2026-09-10 레거시 투표 raw 파일 수집 경계

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `player/vote/<name>_v` 안전 수집 | `LocateLegacyVoteFileV1`가 명시적 absolute root 아래 `player/vote`를 descriptor-anchored no-follow로 열고 0700/euid 디렉터리·0600 regular/nlink=1 파일·64KiB bound·canonical name을 확인한다. fd 전후 stat과 fresh rewalk로 교체를 닫고, `LocateLegacyVoteRawFilesV1`는 `_v` 외 entry를 거부한 뒤 lexical batch와 재열거 결과를 대조한다. 반환값은 raw bytes와 SHA-256/제한 metadata뿐이다. | 전용 race, `bash scripts/run-go-validation.sh integration`, `go vet`, Linux ARM64/Darwin compile PASS. ISSUE 길이·선택지·이름→character ID mapping·manifest/import/apply·운영 원본 대량 대조는 미완료 |

이 경계는 기존 `ReadLegacyVoteFiles`의 일반 `fs.FS` 편의 API를 대체하지 않는다. 운영 이관은
명시적 root 수집 결과를 사람이 검토한 manifest로 만들고 `ImportLegacyVotes`에 제출해야 하며,
locator는 State·계정·credential·DB·runtime을 직접 변경하지 않는다.

## 2026-09-10 레거시 소셜 파일 수집 경계

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `family/family_list` + `family_member_<n>` 수집 | `LocateLegacyFamilyRawFilesV1`가 명시적 absolute root 아래 `family` 디렉터리를 descriptor-anchored no-follow로 열고 0700/euid·0600/regular/nlink=1·64KiB bound·directory entry set·open/read/re-walk 교체를 검증한다. `InspectLegacyFamilyRawFilesV1`은 C의 family-list/member sentinel과 class/name 범위를 재현해 `FamilyCatalog`/`FamilyState`/`BossIDs`로 변환한다. | `go test -race ./internal/world -run 'LegacyFamily'`, `go vet`, Darwin/Linux ARM64 cross-compile PASS. 실제 원본 tree 수집·대량 누락/중복 대조·operator ID mapping과 manifest apply는 미완료 |
| `player/fal/<name>` 메모 수집 | `LegacyMemoFileLocatorV1`가 canonical recipient name만 받아 `player/fal`을 no-follow로 읽고 0700/euid·0600/regular/nlink=1·4MiB·재검사/rewalk를 적용한다. `ParseLegacyMemoFileV1`은 `ctime` 3-line record, UTF-8/80-byte body, timestamp bound와 명시적 sender/recipient ID mapping을 검증해 pointer-free `CharacterMemo`를 만든다. | `go test -race ./internal/world -run 'LegacyMemo'`, `go vet`, Darwin/Linux ARM64 cross-compile PASS. 운영 timezone·원본 대량 수집·sender mapping review·memo manifest apply는 미완료 |

두 collector는 migration-only이며 raw payload를 gameplay state에 넣거나 password·이름 기반
계정 claim·DB/runtime 쓰기를 수행하지 않는다. locator metadata/path와 SHA-256은 별도
migration evidence로만 남고 canonical aggregate에는 들어가지 않는다. 수집 결과는 사람이 승인한 canonical ID mapping과 `family-ledger-v1`/
`character-memos-v1` manifest로 넘긴 뒤에만 기존 `cmd/muhan -import-social-manifest-apply`
경계를 사용할 수 있다.

## 2026-09-10 소셜 raw→manifest builder

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| family raw + operator mapping | `cmd/muhan -build-social-family-root`가 `family_identity`의 모든 boss/member ID를 요구하고 locator/parser 결과를 `family-ledger-v1`로 직렬화한다. 이름으로 ID를 추측하지 않으며 manifest에는 raw path·bytes·credential이 없다. | `go test -race ./cmd/muhan` 및 CLI DB-free/immutable replay PASS. 실제 원본 tree·operator mapping 승인·manifest apply는 미완료 |
| memo raw + operator mapping | `cmd/muhan -build-social-memo-root`가 명시된 `player/fal/<name>` 수신자와 sender ID map을 모두 파싱하고 `character-memos-v1` aggregate를 만든다. ctime timezone은 UTC 기본 또는 명시 IANA location만 사용한다. | malformed/unknown field/duplicate identity/source-output overlap과 dry-run PASS. 실제 대량 수집·timezone 승인·Supabase 운영 import/복구는 미완료 |

builder는 `-build-social-manifest-dry-run`에서 DB/listener 없이 동일 검증만 수행하고,
output은 mapping과 같은 private `0700` 디렉터리의 immutable `0600` 파일로 제한한다.
생성 문서와 mapping schema는 `docs/porting-research/go-social-manifest.md`에 있다.

## 2026-09-10 은행 kind-8 review→import manifest

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| bank review + identity/item mapping | `-build-bank-snapshot-manifest-review`와 `-build-bank-snapshot-manifest-mapping`이 raw conversion review의 canonical digest/크기/root/node/name을 다시 확인하고 명시적 account/player/item ID를 `BankSnapshotV1` import manifest로 결합한다. | `cmd/muhan` race, unknown/path/digest/account mismatch, changed artifact, DB-free dry-run·immutable replay PASS. 실제 account/character 대조·대량 운영 승인은 미완료 |
| manifest read/apply | `-import-bank-snapshot-manifest`는 모든 canonical artifact와 item ID를 DB 전에 검증하고, `-import-bank-snapshot-manifest-apply`만 기존 `Postgres.ImportBankSnapshot` receipt 경계를 호출한다. | 기존 ARM64 PG import/replay/rollback PASS와 CLI path-only 검증 PASS. 운영 Supabase/RLS·중단 batch 복구·live bank/gold parity는 미완료 |

mapping schema와 실행 예는 `docs/porting-research/go-bank-snapshot-manifest.md`를
따른다. raw source digest는 converter review evidence로 유지되고, import request에는
실제로 부착되는 canonical artifact digest만 사용한다.

## 2026-09-10 소셜 aggregate manifest·복구 reader

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| reviewed family/memo manifest | `cmd/muhan -import-social-manifest`가 private 0600 JSON의 kind/version/world/command/expected revision과 pointer-free aggregate를 검증한다. path-only 또는 `-import-social-manifest-dry-run`은 DB/listener 없이 종료하고, `-import-social-manifest-apply`만 명시적 Postgres import를 호출한다. | CLI/storage race·vet·DB-free subprocess·ARM64 PG apply/replay/rollback PASS. 실제 legacy collector·operator mapping·운영 Supabase 권한은 미완료 |
| normalized evidence restart/restore | `ReadFamilyLedgerEvidence`, `ReadCharacterMemosEvidence`, `RestoreSocialState`가 row ordering/count/hash, receipt, expected revision/writer fence와 snapshot authority를 대조하며 tamper/orphan/mismatch를 거부하고 evidence로 snapshot을 덮어쓰지 않는다. | ARM64 PG restore/fence/tamper PASS. 보관·PITR·장애 중 재접속과 전체 데이터 복구 훈련은 미완료 |

## 2026-09-10 패거리 추방·canonical social import

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command12.c:fm_out` | `FamilyMutationExpel`/`PlanFamilyExpulsion`이 exact online canonical target, PFMBOS/catalog boss, 동일 패거리 member ledger row를 확인하고 PFAMIL/DL_EXPND와 원장을 원자적으로 갱신한다. fee·broadcast 없이 target-only post-commit notification을 receipt에 담고 replay에서는 재전송하지 않는다. | world/session/transport race·vet·parser/transport 회귀 PASS. offline legacy `load_ply` 경로, 전체 C 출력 parity와 나머지 family/social 명령은 미완료 |
| `family_member_<n>` canonical import | `Postgres.ImportFamilyLedger`가 명시된 `FamilyState`/`FamilyCatalog`를 world snapshot·`family_imports`·`family_catalog`·`family_members`·`world_commands`에 단일 transaction으로 저장한다. ID/name/class evidence만 사용하며 raw path·credential·이름 기반 claim은 거부한다. | ARM64 PostgreSQL replay/conflict/rollback PASS. 실제 source collector, operator identity review, normalized evidence restore reader, Supabase RLS/운영 권한은 미완료 |
| `player/fal/<name>` memo import | `Postgres.ImportCharacterMemos`가 canonical recipient ID keyed nonnil memo aggregate를 snapshot·`character_memo_imports`·`character_memos`·receipt로 원자 저장하고 timestamp/sender/recipient integrity를 재검증한다. | ARM64 PostgreSQL replay/rollback 및 storage race/vet PASS. legacy file collector, 대량 운영 이관·복구와 전체 출력 parity는 미완료 |

## 2026-09-10 패거리 원장·승인/탈퇴 및 메모 command 경계

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:가입허가`·활성 `패거리탈퇴` | `FamilyState`/`FamilyMember` canonical ledger와 `FamilyCatalog.Fee`를 도입하고, exact target·PFMBOS/catalog boss·online/visibility·fee/gold bound·member identity를 검증한 뒤 승인 시 `family_gold*10000` 이전, 활성 탈퇴 시 `family_gold*20000` 차감과 원장 제거를 하나의 receipt 전이로 처리 | world/session/transport race 및 vet PASS. Supabase/import에서 실제 `family_member_<n>` 원장을 canonical aggregate로 채우는 운영 단계와 원작 broadcast/interactive continuation은 미완료 |
| `command12.c:memo` | `메모 <캐릭터명> <내용>`을 canonical recipient ID 아래 append-only `State.Memos`로 저장. offline recipient 허용, online actor 요구, UTF-8/80바이트/control/name/command-ID 경계와 receipt replay를 적용하고 nil pre-migration은 fail-closed | world/session/transport race 및 vet PASS. legacy `player/fal` batch import·운영 Supabase schema/복구·전체 출력 parity는 미완료 |

`src/frp.new` 및 다른 dirty worktree는 이 배치에서 변경하지 않았다. 전체 `go test -race
./...`는 기존 room body corpus의 검토되지 않은 63건 때문에 계속 실패하며, 이를 새 기능
실패로 숨기지 않는다.

## 2026-09-10 reviewed room manifest seed gate

`cmd/muhan -seed-world`가 이제 `LoadReviewedLegacyRoomCatalog`만 사용한다. 따라서
검토된 3,216개 room source manifest(정규 경로 2,341개·비정규 artifact 875개·body
exception 63개)의 digest가 달라지면 PostgreSQL seed 전에 fail-closed한다. synthetic
fixture와 unit importer는 기존 `LoadLegacyRoomCatalog`를 계속 사용할 수 있지만, 운영
provisioning 경로는 drift가 확인되지 않은 room tree를 권위 snapshot으로 만들지 않는다.

`go test -race ./cmd/muhan -count=1`, `go vet ./cmd/muhan ./internal/world`, `git diff
--check`가 통과했다. 실제 seed/PG 재실행과 63개 body exception의 원본 변환은 별도 G1/G4
승격 조건으로 남아 있다.

## 2026-09-10 legacy bank raw→kind-8 operator conversion

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| raw bank → canonical artifact | `cmd/muhan -convert-bank-raw-root/-convert-bank-raw-player/-convert-bank-raw-abi`가 locator source를 exact ABI로 재검증하고 canonical `BankSnapshotV1` 0600 파일과 `.review.json`을 immutable하게 생성. review에는 source/canonical digest·크기·path·graph count만 있고 identity/credential claim은 없음 | cmd/world race, vet, Linux amd64/arm64 main build·Darwin arm64 compile PASS. 운영 대량 수집·account/character 대조·별도 import manifest는 미완료 |
| conversion safety | private 0700 parent·source root overlap 금지·destination 전체 preflight·same-byte replay만 허용·symlink/권한/변경 output fail-closed. dry-run은 bytes/codec만 검증하고 파일/DB/listener를 만들지 않음 | `TestLegacyBankRawConversionCLIIsDBFreeAndDryRun` 및 immutable/replay/conflict 테스트 PASS. live bank/gold parity·복구·운영 Supabase는 미완료 |

변환 결과는 사람이 source player name과 canonical account/character를 대조하고 item ID
manifest를 작성한 뒤에만 기존 `ImportBankSnapshot` 경계로 넘길 수 있다.

## 2026-09-10 legacy bank file locator·raw 검사 CLI 및 웹 IME Enter

| 이관 경계 | Go/웹 구현 | 검증/남은 조건 |
| --- | --- | --- |
| C `bank_file_locator.c` raw source 수집 | `LocateLegacyBankSnapshotRawV1`가 명시적 absolute root 아래 `player/bank/<canonical-name>`을 descriptor-anchored no-follow로 열고 0700/euid·0600/regular/nlink·4MiB·교체 여부를 검증. 결과는 owned bytes와 SHA-256·filesystem metadata만 반환 | `TestLegacyBankFileLocator` race PASS, Linux amd64/arm64 compile 및 Darwin build 확인. 운영 raw 위치/대량 batch·account/character 대조는 미완료 |
| raw bank review CLI | `-inspect-bank-raw-root` + `-inspect-bank-raw-player`가 exact LP64 ABI를 확인하고 raw→kind-8 parser evidence를 DB/listener 전에 실행. source/canonical digest·크기·graph count·파일 metadata JSON만 출력 | `TestLegacyBankRaw`·`BankSnapshotInspectionCLI` race 및 vet PASS. `ImportBankSnapshot` 연결·라이브 입출금 parity·운영 Supabase는 미완료 |
| xterm IME 제출 경계 | `shouldDeferTerminalSubmission`과 `ClassicTerminal`이 조합 중 CR/LF를 `compositionend` 다음 task로 지연하고 일반 문자/Backspace·focus/reconnect/cleanup을 유지 | `npm test` 58·typecheck·build PASS. 실제 OS IME·iOS/Android 키보드·브라우저 E2E는 미검증 |

raw locator와 검사 CLI는 identity claim이나 gameplay authority를 만들지 않는다. 운영 전환은
사람의 source/name 대조와 명시적 import receipt를 거쳐야 하며, 웹은 여전히 별도 계정 가입
없이 터미널 이름/비밀번호 흐름을 사용한다.

## 2026-09-10 legacy native player raw reader·inspection

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `read_crt_player` native raw stream | `DecodeLegacyPlayerSnapshotRawV1`가 audited little-endian raw-v1 ABI의 `creature`/재귀 `object` bytes를 bounds·EOF·depth·count와 load-time clamp까지 검증하고 pointer-free `PlayerSnapshotV1`로 변환. password/descriptor/pointer는 결과에 포함하지 않음 | `scripts/run-legacy-player-snapshot-v1-differential.sh`에서 C oracle raw fixture와 C/Go CDTO projection byte-for-byte PASS; 운영 ABI 승인·대량 수집/대조·복구는 미완료 |
| raw inspection evidence | `-inspect-player-snapshot-format legacy-player-raw-v1`가 private 0700 tree를 lexical scan하고 raw SHA-256/크기/graph node/parser·ABI/quarantine metadata만 ledger에 기록 | Go cmd race·vet targeted PASS; 운영 Supabase 승인과 raw→manifest operator review는 미완료 |

raw format은 self-describing하지 않으므로 `LegacyPlayerSnapshotRawV1ABI` 일치가 선행되어야
한다. raw bytes를 바로 import하지 않고, 검토된 canonical CDTO와 명시적 account/player/item
manifest를 거친다.

## 2026-09-10 raw→CDTO operator conversion

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| raw source에서 검토 산출물 만들기 | `-convert-player-snapshot-raw-dir`가 명시된 raw-v1 ABI와 private `0700/0600` tree를 전체 검증하고, 별도 private output에 canonical CDTO와 `player-snapshot-review.json`을 atomic immutable write | `cmd/muhan` raw conversion unit test PASS; 같은 bytes replay·changed output conflict·duplicate name·source/output overlap을 확인. exact identity/player/item/credential review와 운영 import는 미완료 |

변환 review는 `suggested_account_name`과 source/canonical SHA-256·크기·graph count만
제공하며 password/native pointer·player ID·item ID·bcrypt hash는 포함하지 않는다. 따라서
자동 claim을 하지 않고, 사람이 대조한 뒤 기존 v1 import manifest를 별도로 작성해야 한다.

## 2026-09-10 review→import manifest builder

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| operator mapping 결합 | `-build-player-snapshot-manifest-review` + `-build-player-snapshot-manifest-mapping`이 사람이 승인한 account/player/item ID·expected revision·bcrypt hash를 review 순서와 매칭하고 CDTO digest·canonical wire·name·graph count를 재검증한 뒤 기존 import manifest를 생성 | `cmd/muhan` race/CLI dry-run PASS; 평문 password·누락 expected revision·경로 traversal·중복 identity/item·변경 output은 fail-closed. 실제 운영 mapping 승인·대량 import/복구는 미완료 |
| DB 경계 분리 | builder는 `-build-player-snapshot-manifest-dry-run` 또는 private `0600` immutable output만 수행하며 DB/listener를 시작하지 않음. 생성물은 별도 `-import-player-snapshot-manifest`에서만 사용 | 운영 Supabase 연결/전체 캐릭터 대조·복구·배포는 미완료 |

mapping schema와 운영 절차는 `docs/porting-research/go-player-snapshot-manifest.md`를
기준으로 한다. raw source digest는 review evidence로 남고, 생성 import manifest의
`source_sha256`은 실제 import 대상 canonical CDTO bytes를 가리킨다.

## 2026-09-10 `PlayerSnapshotV1` operator manifest·read-only inspection

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| 검토된 CDTO batch 전달 | `cmd/muhan -import-player-snapshot-manifest`가 private 0600 JSON v1, source SHA-256, canonical round-trip, bcrypt hash, exact player/item ID를 DB 전에 검증하고 `ImportPlayerSnapshot`을 record 순서대로 호출 | `go test -race ./cmd/muhan`, vet, fast/integration PASS. 운영 대량 이관·전체 계정 대조는 미완료 |
| raw-free snapshot inspection ledger | `-inspect-player-snapshot-dir`가 private 0700 tree를 lexical walk하고 valid/quarantined metadata만 `mud_go.player_snapshot_import_ledger`에 idempotent 저장. payload/password/identity claim 없음 | PG17 ARM64 import harness에서 ledger replay·changed digest·quarantine PASS. C raw player decoder, 운영 보관/복구/승인은 미완료 |

manifest schema와 실행/원자성 규칙은 `docs/porting-research/go-player-snapshot-manifest.md`를
기준으로 한다. 이 경계는 레거시 파일의 gameplay authority를 바꾸지 않는다.

## 2026-09-10 `PlayerSnapshotV1` PostgreSQL import·evidence

| 이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| 검토된 CDTO → account/character/world | `Postgres.ImportPlayerSnapshot`가 caller-owned exact player ID와 item-ID manifest를 사용해 decode/canonicalize·offline admission·계정/linked character·world snapshot을 한 transaction으로 저장하고, command receipt를 재생 | ARM64 `postgres:17-alpine` import/replay/conflict/rollback PASS. 운영 Supabase schema·대량 raw 수집/대조와 account recovery는 미완료 |
| 이관 evidence | `mud_go.character_imports`에 source/canonical SHA-256, source octets, inventory node count, imported revision만 저장. raw CDTO/password는 저장하지 않음 | 운영 보관·암호화·retention·전체 duplicate/loss 대조는 미완료 |

재시도 request hash에는 canonical account name, explicit player ID, raw snapshot digest,
credential digest, item manifest가 포함된다. 따라서 같은 command ID에 다른 source/hash/
manifest를 주면 receipt replay가 아니라 `ErrCommandConflict`로 닫힌다.

## 2026-09-10 `PlayerSnapshotV1` CDTO decoder·offline admission

| 원작/이관 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| C/Rust pointer-free `PlayerSnapshotV1` artifact | `DecodePlayerSnapshotV1`/`EncodePlayerSnapshotV1`가 CDTO v1 envelope·SHA-256·40개 field 계약과 ObjectGraphV1 preorder topology를 검증. `InspectPlayerSnapshotV1`은 raw/SHA evidence를 복사 보존 | C fixture 6종 byte-for-byte round-trip, world race/vet PASS. raw legacy 파일 수집·운영 승인과 Supabase evidence 보관은 미완료 |
| 검증 snapshot → Go world | `ToLegacyMonster`/`ToItemCollection`/`ToPlayerState` 및 `State.AdmitPlayerSnapshot`이 i64 overflow·text/ID 충돌·allocator/room 실패를 fail-closed하고 explicit player ID로 offline clone만 생성 | pure admission TDD PASS. (초기 기록) 이후 PostgreSQL import/receipt/account-link/item-ID manifest 경계는 최신 상단 기록으로 추가됐으며, 운영 대량 이관은 미완료 |

이 경계는 C/Rust runtime 또는 하위 프로세스를 사용하지 않는다. 이름 정규화는 원작
terminal registration과 같은 `CanonicalName`을 사용하지만, source bytes/SHA evidence가
없는 이름 기반 자동 이관은 허용하지 않는다.

## 2026-09-10 `post/ISSUE` raw 카탈로그 파서·서버 주입

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `post/ISSUE` 안건·선택지 파일 | `ParseVoteCatalog`/`LoadVoteCatalogFile`이 raw bytes를 UTF-8/EUC-KR·CRLF·최종 개행 변형까지 검증하고, 안건/선택지 한도·trailing/truncation/malformed를 fail-closed. `cmd/muhan -vote-issue-file`/`MUD_VOTE_ISSUE_FILE`은 `-world`에서만 server-owned catalog를 주입하며 SHA-256/evidence를 보존 | `3a05962` world parser race·vet·diff PASS. 운영 Supabase 대규모 이관, 실제 운영 파일 승인/보관, 전체 투표 출력·기능 parity와 브라우저/배포는 미완료 |

## 2026-09-10 `투표` 이관·continuation·receipt 통합

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:vote`·`vote_cmnd` / `투표` | `VoteCatalog` gate, one-based ISSUE 출력, 연결 로컬 y/n·a..g continuation, `State.Votes` write/rewrite/history receipt를 `PlanLegacyVoteImport`/`ImportLegacyVotes`와 parser→session→transport로 연결. legacy path/raw bytes/name→ID resolver/digest를 all-or-nothing으로 검증하고 disconnect 시 draft를 폐기 | `28938b5`, `3a05962`, `4fd7a8b`: world/session/transport vote·import/parser race/vet, 전체 Go integration, diff check PASS. 실제 운영 Supabase 대규모 이관, 운영 파일 승인/보관, 브라우저/WSS/Ingress·testnet 및 전체 command parity는 미완료 |

투표 원장은 nil이면 이관 미완료로 fail-closed한다. Importer는 source active 파일에 없는
history를 추측해 만들지 않으며, 운영 migration 도구에서 반환된 path/SHA-256 evidence를
별도 검토할 수 있다.

## 2026-09-10 `투표` source gate·ballot authority 경계 (역사적 초기 단계)

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:vote`·`vote_cmnd` / `투표` | `VoteCatalog` ISSUE snapshot과 나이·RELECT 투표소·최대 7개 선택지 gate를 `PlanVote`로 검증. y/n·a..g continuation은 연결 로컬 `VoteContinuation`에서만 진행하고, canonical ballot/history가 없으므로 `ApplyVote`는 쓰기 전에 fail-closed | world/session/transport targeted race·vet·diff PASS. `player/vote/<name>_v` 정규화와 실제 PG receipt 저장/replay, full continuation/출력·운영/브라우저/배포는 미완료 |

투표 안건과 선택지는 client payload가 아니라 서버 소유 catalog에서만 공급된다. 기존
vote 파일 권위가 이관되기 전에는 성공 응답이나 no-op 영수증을 만들지 않는다.

## 2026-09-10 `대답`/`/` reply receipt 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command12.c:resend` / `대답`·`/` | 수신 direct-message event의 sender durable ID/name을 연결 로컬 atomic `replyTarget`에 기록. 서버가 주입한 exact target으로 direct-message reducer를 실행하는 `ExecuteReplyLine` receipt를 추가하고, 최초 commit 뒤에만 reply event를 fan-out | session/transport race·vet·diff PASS. 실제 PG 검증 훅 추가(이번 실행 skip). full resend continuation·C descriptor parity·운영 Supabase·브라우저/배포는 미완료 |

답장 대상은 terminal payload에서 선택할 수 없으며, 연결 종료 시 폐기된다. stale 또는
오프라인 sender는 receipt 전에 fail-closed한다.

## 2026-09-10 배우자 대화 출력·ANSI receipt 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:m_send` / `<메시지> 사랑말` | PMARRI·`m<배우자>` key·온라인 reciprocal identity·255바이트 UTF-8 검증. `crt_str` PINVIS/PDMINV/PDINVI, PANSIC/PBRIGH·`%j`를 proposal에 렌더링하고 actor 응답/배우자 event를 durable receipt로 저장. 첫 commit에서만 정확한 recipient ID/name으로 전송 | session/transport/world race·vet, ARM64 PostgreSQL 저장·replay PASS. 전체 descriptor/title parity, offline `load_ply`, 운영 Supabase·브라우저/배포는 미완료 |

`사랑말`은 C parser의 suffix 명령 순서를 그대로 사용한다. receipt replay에서는
descriptor를 다시 조회하거나 event를 재전송하지 않는다.

## 2026-09-10 이혼·배우자 대화 후속 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:divorce` / `이혼` | 신청 취소·미혼 no-op·이혼 신청 취소·온라인 배우자 신청·상호 수락을 `PlanDivorce`/`ApplyDivorce`로 원자화. `PMARRI`/`PRDMAR`/`PRDDIV`와 `key[2]`를 receipt에 저장하고 parser/session/WorldConnector에서 첫 commit 후 배우자 event·PNOBRD 전역 공지를 fan-out | world/session/transport 후속 race/vet, ARM64 PostgreSQL 17 request/accept/replay PASS. offline/missing legacy `load_ply`, 전체 social parity, 운영 Supabase·브라우저/배포는 미완료 |
| `command11.c:m_send` / `사랑말` | alias와 메시지 UTF-8/255바이트, 기혼·상호 배우자·온라인 canonical identity를 검증하는 경계 추가 | `%C/%M/%j` descriptor와 PLECHO exact echo formatter가 없어 성공 receipt/전송은 명시적 fail-closed; 출력 parity 미완료 |

이혼 수락은 C `broadcast()`와 동일하게 `PNOBRD`를 가진 연결에는 공지를 보내지 않는다.
배우자 대상 event는 receipt에 고정된 durable ID/name이 post-commit snapshot과 일치할 때만
전달하며, replay에서는 재전송하지 않는다.

## 2026-09-10 결혼 신청·수락 receipt 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:marriage` / `결혼` | 결혼식장·25세·canonical online 대상·visibility·성별·중복 pending/active 게이트와 `PRDMAR`·`PMARRI`·`key[2]` 전이를 `PlanMarriage`/`ApplyMarriage`로 원자 적용. 신청·pending 취소·상호 수락을 parser/session/WorldConnector receipt로 연결하고 배우자 대상 event·수락 전역 broadcast를 첫 commit에서만 fan-out | world/session/transport marriage race·vet, ARM64 PostgreSQL 17 저장·replay PASS. `divorce`·`m_send`, 전체 social 출력 parity, 운영 Supabase·브라우저/배포는 미완료 |

수락 결과의 배우자 ID·canonical 이름은 receipt에 고정되어 재접속·이름 충돌로 알림이
다른 플레이어에게 전송되지 않는다. 전역 공지는 원작 `broadcast_all`처럼 두 배우자를
포함하며, replay에서는 중복 전송하지 않는다.

## 2026-09-10 G4 PostgreSQL 백업·복구 증거

| 원작/운영 경계 | Go 검증 | 남은 조건 |
| --- | --- | --- |
| Go `mud_go` world snapshot·command receipt의 backup/restore | `run-go-backup-restore-local.sh`와 `TestPostgresWorldBackupPhysicalRestore`가 ARM64 PostgreSQL custom-format dump/restore 뒤 revision 2·receipt 2개·동일 ID replay·request conflict·writer epoch fencing·후속 revision 3 저장을 확인 | 운영 archive 보관/암호화/retention/PITR, 전체 legacy duplicate/loss 대조, Supabase 운영 복원과 장애 중 재접속은 미완료 |

## 2026-09-10 `정보` 후속 페이지 durable receipt 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command4.c:info_2` / `정보` 뒤 `[엔터]` | `ExecuteInfoContinuation`이 주문·현주문·임무 projection을 canonical snapshot에서 읽어 `ExecuteGame` receipt로 저장. `WorldConnector`는 connection-local command ID를 커밋 성공까지 유지해 재시도 시 같은 response를 replay하고 `.` 취소는 local 처리 | 영향 패키지 race/vet 및 격리 ARM64 PostgreSQL 17 저장·replay PASS. 전체 C 출력/ANSI parity·spell effect·전체 info 인수와 운영 DB는 미완료 |

후속 receipt는 read-only projection이므로 loaded state bytes를 그대로 반환하지만, 현재
`world_commands` revision 규칙에 따라 receipt commit 자체는 world revision을 증가시킨다.
이는 기존 `ExecuteGame` 계약이며 전체 read/write revision 정책을 확정한 것은 아니다.

## 2026-09-10 bounded PostgreSQL receipt/replay 검증

고유 loopback 포트의 ARM64 `postgres:17-alpine`에서
`TestPostgresBoundedLanesPersistAndReplay`를 실행해 alias·burn·study·family-mutation
네 케이스의 최초 저장과 동일 command ID replay를 모두 PASS로 확인했다. 테스트가 만든
컨테이너만 제거했으며, 이 증거는 bounded lane에 한정된다. 전체 PostgreSQL 이관·운영
Supabase·전체 명령 인수는 미완료다.

## 2026-09-10 패거리 가입/탈퇴 세션·전송 경계

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:family`·`add_family`·`out_family` / `패거리가입 <이름>`·`패거리탈퇴`·`가입허가` | `ParseFamilyMutationLine`, `ExecuteFamilyMutationLineWithCatalog`, `PlanFamilyJoinByName`/`PlanFamilyWithdrawal`→`ApplyFamilyMutation`을 parser·WorldConnector에 연결. catalog exact-name, 온라인 canonical boss/PFAMIL·PFMBOS·PRDFML을 확인하고 pending 신청/취소만 원자 receipt로 저장 | session/transport race·vet·parser 회귀 PASS. bare 가입 continuation, 승인/활동 탈퇴의 family fee/member ledger는 fail-closed; 전체 family ledger/공지/전쟁/실제 PG는 미완료 |

같은 command ID는 `ExecuteGame`의 저장 응답을 재생해 membership reducer와 side effect를
중복 실행하지 않는다. `가입허가` 대상은 이름 존재 여부를 권한 증명으로 사용하지 않으며,
원장 없는 경로에서 사용자 상태를 추측하지 않는다.

## 2026-09-10 정리 확인 + 패거리말·주문·가입 경계

Orca 관리 목록에는 주 worktree만 남아 있으며, Git에 남은 예전 `orca/workspaces`
30개는 보존했다. 22개는 `objmon/Celduin_sign` 대소문자 충돌만 남았고, 8개에는
`src/frp.new`·삭제된 파일·Rust·미추적 변경이 있어 확인 없이 삭제하지 않았다. 직접
관리한 Luna max 레인은 종료 후 주 worktree에서 통합했고 `src/frp.new`는 계속 제외한다.

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command11.c:family_talk` / `패거리말`·`]` | `PlanFamilyTalk`, canonical PFAMIL·PSILNC·FamilyCatalog, deterministic recipient event receipt, parser/session/transport 최초 commit fan-out 및 replay 억제 | targeted race/vet 통과; family war/보상과 전체 C 출력 parity는 미완료 |
| `post.c:family_news` / `패거리공지` | `FamilyNewsState` + view/append/delete receipts, PFAMIL/PFMBOS·catalog gate, newsedit 줄 단위 commit, replay 무중복 | targeted race/vet PASS; view_file 페이지 parity·운영 news import는 미완료 |
| `special1.c:call_war` / `선전포고` | `PlanFamilyWar`/`ApplyFamilyWar`, CALLWAR1/2·AT_WAR, 선언 broadcast_all·취소/수락 PNOBRD, replay 무중복 | targeted race/vet PASS; 문주 사망 통합·운영 War 영속·보상은 미완료 |
| `command8.c:moon_set` / `기억` | suffix parse, 광장/value 게이트, inventory exact bind, room broadcast 2줄, replay 무중복 | targeted race/vet PASS; EQUAL prefix/OINVIS·장비/중첩, 문주 사망 방송은 미완료 |
| `command11.c:family`·`add_family`·`out_family` / 가입 신청·취소 | `PlanFamilyJoin`/`PlanFamilyWithdrawal` proposal와 원자 apply, canonical online boss/identity·PFAMIL/PRDFML/PFMBOS 검증 | 승인·활동 회원 탈퇴의 `family_gold`·`family_member_<n>` ledger가 없어 fail-closed; transport 명령 연결은 후속 |
| `command4.c:info_2` 주문 목록 / `주문` | `SpellCatalog` 56 `spllist` + 20 활성 `ospell`, deterministic 이름 정렬 `SpellList` read-only receipt/replay와 session adapter | offensive/targeted/map/미확인 주문 실행, `[엔터]` continuation·전체 spell effect는 미완료 |

검증 명령 `(cd server && go test -race ./internal/world -run 'FamilyTalk|FamilyMutation|SpellCatalog|SpellList' -count=1)`,
session/transport targeted race 및 `go vet`가 통과했다. 전체 world는 기존 strict room
corpus 63개 예외로 실패한다. ARM64/main, 실제 PG/browser·IME/mobile, release,
WSS/Ingress 및 testnet은 cadence 경계에서만 실행한다.

## 2026-09-10 정리 후속: 스크롤·초대·패거리 상태

이전 Orca 정리 요청으로 clean worktree 108개를 제거하고 dirty worktree 30개는
보존했다. 현재 구현은 주 worktree에서만 조립하며, 사용자 소유 `src/frp.new`는
건드리지 않는다. 두 기능 레인은 Luna max로 병렬 처리하고 parser·transport는 메인에서
한 번만 통합했다.

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `magic1.c:readscroll` / `읽어 <두루마리>` | `PlanReadScroll`/`ApplyReadScroll`, canonical root/ready 선택, spell-fail·self-effect RNG replay, 소비·PHIDDN·LT_READS·alignment room 이동, parser/session/transport room event | world/session/transport targeted race 및 `fast`/`integration` PASS; offensive/targeted/map spell, full spell catalog와 legacy item migration은 fail-closed/미완료 |
| `command12.c:invite` / `초대 [이름]` | `PlanPropertyInvite`/`ApplyPropertyInvite`/`ListPropertyInvitations`, RONMAR·DL_MARRI, exact canonical online identity, ordered 10-slot toggle/list, stale atomic apply | world·transport targeted race 및 `fast`/`integration` PASS; family/marriage DB 이관과 전체 social mutation은 미완료 |
| `command11.c:family_who/family_member/list_family` / `패거리누구`·`패거리원`·`모든패거리` | `FamilyCatalog`, deterministic online roster/status projection, PFAMIL/PRDFML/PFMBOS·visibility/blindness gate, parser/session/receipt 연결 | world·session·transport targeted race 및 `fast`/`integration` PASS; family catalog 이관, 가입/탈퇴/승인/공지/전쟁·보상 mutation은 미완료 |

전체 world package 직접 실행은 기존 strict room corpus의 알려진 63개 예외로 실패했다.
이번 batch는 ARM64 main, 실제 PostgreSQL/browser·IME/mobile, release matrix, WSS/Ingress
및 testnet을 반복하지 않았으며, 해당 검사는 각각 `main`/`release` 승격 경계에서 한 번만
수행한다. 전체 C prefix/key/ANSI parity와 full item/spell/social migration은 여전히
승격 조건이다.

작성일: 2026-09-08 (KST) · 상태: **전체 인수 전의 보수적 G0 원장**

이 문서는 `docs/porting-research/go-server-execution-plan.md`의 G0 인벤토리다.
레거시 C의 등록 명령, 월드/전투/저장 코드를 source-backed로 열거하고, 기존
테스트를 Go 이전 작업의 oracle 후보로 매핑한다. 이 문서의 어느 행도 Go 기능이
구현되었거나 인수되었다는 뜻이 아니다.

중요: 이 원장의 `인수 상태=미착수`는 **작업 트리에서 구현물이 사라졌다는 뜻이
아니다**. 전체 명령 집합의 최종 인수 기준을 아직 충족하지 않았기 때문에 명령
행 단위 상태를 보수적으로 유지하는 것이다. 현재 실제 구현·검증된 Go 수직
슬라이스(방향 입력/영속 이동 receipt, 도착 함정, 플레이어 추종 관계 및 재귀 이동,
mixed `first_fol` 순서와 MDMFOL NPC 이동, TRAP_ALARM due catalog spawn, committed
movement room-event fan-out, canonical NPC ID/active 순서, 로그아웃 NPC 적대 정리,
PG replay/race 검증)는
`HANDOFF.md`와 `server/README.md`의 날짜순 실행 기록을 기준으로 확인한다.
추가로 player phase의 `UpdatePlayer` 영속 receipt/replay slice도 같은 실행 기록과
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`에서 확인한다. 이는
전체 `update.c` scheduler 인수가 아니다.
NPC 전투의 첫 slice(`공격` 대상 선택·THAC0/피해·critical·enemy relation·PG replay)와
lethal NPC 전이는 `TestPostgresAttackCommandPersistsAndReplays`/`TestPostgresLethalAttackCommandPersistsNPCDeath`/
`TestPostgresLethalAttackCommandPersistsNPCDrops`로
확인한다. lethal 경계는 NPC 제거, active/enemy/follower 정리, XP/성향·quest/proficiency,
금화·legacy drop graph의 canonical floor 이동, permanent timer를 한 후보로 저장하며,
allocator가 없거나 summon side effect가 필요한 경우 fail-closed한다. PVP·도망·소환
생성·전체 전투 명령 인수로는 아직 승격하지 않는다. 별도 무기 slice는
치명타 파괴, 일반 명중 시 드롭, 다음 swing의 shots 감소와 NPC 피해 비례 숙련도 보상을
`TestPostgresAttackCommandPersistsWeaponDrop` 및 world TDD로 검증했지만, 이것만으로
전체 전투 행을 `완료`로 올리지는 않는다. PUPDMG의 추가 swing 수와 lethal 중단도
`TestPostgresAttackCommandPersistsPowerDamageSequence`로 실제 PG replay를 검증했다.
`건강`/`점수` numeric status receipt도 blind/replay 회귀로 확인하지만, 전체 상태 명령
및 ANSI/title parser 인수로 승격하지 않는다.
`정보`의 첫 페이지 통계(이름·레벨·종족·직업·성향·접속시간·능력치·HP/MP·경험치·돈·
방어력·무게/개수·무기/마법 숙련도)도 `ExecuteInfoLine`과 `PlayerInfo`의 pure
read-only receipt로 연결하고 local race/vet/transport replay를 통과했다. canonical
Items·정의된 class/race/proficiency가 없으면 fail-closed하며, C의 title 계산과
`[엔터]` 후 `info_2` 주문 continuation은 아직 구현하지 않았다. 따라서 이 행의 전체
`info` 인수 상태는 계속 `미구현`이다.
`시간` bare 명령은 게임 시각과 PST wall-clock을 request에 고정한 read-only receipt로
연결했고 동일 command ID replay에서 시계를 다시 읽지 않는다. `도움말`/`?`도
프로세스가 주입한 UTF-8 `help/` 문서를 receipt로 읽는 첫 slice를 추가했다. bare
`helpfile`, `주술`/`정책`, 현재 Go handler가 있는 명령 주제의 `help.<cmdno>`를
지원하고, unknown topic은 C의 고정 no-help 응답을 저장한다. 누락/비 UTF-8 문서는
추정하지 않고 fail-closed하며, 동일 command ID replay와 state purity를 검증한다.
전체 C alias/약어, continuation prompt, title 출력 및 parser parity는 별도 후속이다.
`따라 <플레이어>`와 `내보내` 관계 명령도 same-room exact-name 및 reciprocal replay
회귀를 통과했다. `내보내`의 자기 leader 이탈과 지정 follower 해제 경로까지 연결했지만,
ARM64 PostgreSQL 17 `TestPostgresFollowAndLoseCommandPersistsAndReplays`도 통과했다.
전체 follow/lose/NPC follower 명령 인수로 승격하지 않는다.
`소지품`과 `장비`/`장` read-only receipt도 canonical item/ready 순서 및 blind/invisible
경계를 검증하고 ARM64 PostgreSQL 17 `TestPostgresItemsCommandPersistsAndReplays`를
통과했지만, item mutation과 전체 parser 인수로 승격하지 않는다.
`말`/따옴표 say receipt와 same-room 비동기 fan-out, 침묵·hidden 상태 경계도
`TestPostgresSayCommandPersistsAndReplays` 및 transport 회귀로 확인했지만, 전체
대화/DM/broadcast formatting 인수로 승격하지 않는다.
`누구`/`그룹` read-only receipt도 deterministic online 목록, visibility 및 mixed
`first_fol` 순서와 ARM64 PostgreSQL 17 `TestPostgresSocialCommandPersistsAndReplays`로
확인했지만, C 출력 동등성·group mutation 전체 인수로 승격하지 않는다.
`주워`/`버려` 계열 root transfer mutation도 nested ID 보존·blind/invisible 경계와
ARM64 PostgreSQL 17 `TestPostgresItemMutationCommandPersistsAndReplays`로 확인했지만,
전체 item mutation 및 무게/경비 규칙 인수로 승격하지 않는다.
`입어`/`쥐어`/`무장`/`벗어` 장비 mutation도 canonical inventory root와 20개 ready
slot 사이를 원자적으로 이동하고, 슬롯 충돌·파손·저주·기본 직업/성향/크기/무기 제한과
AC/THAC0 재계산을 적용한다. `TestPostgresEquipmentCommandPersistsAndReplays`가
ARM64 PostgreSQL 17에서 저장·동일 command ID replay를 통과했지만, `모두` 일괄 명령,
전체 C class/race/quest/event/charge 예외, 장비 사용 효과 및 전체 parser 인수로
승격하지 않는다.
`주워 모두`/`버려 모두`도 canonical floor/player root를 가시성·경비·무게·소지 수·
퀘스트/이벤트 보호 규칙과 함께 하나의 후보로 처리한다. 일반 단일 get/drop과 함께
unit/race 및 ARM64 PG17 replay를 통과했지만, container 내부 get/drop, gold/quest
보상, 제물 방·은행·상점 연동은 아직 별도 인수 범위다.
`꺼내 <가방> <물건>`/`넣어 <물건> <가방>` direct container child 이동도 ID/중첩
소유권·용량 counter를 보존하는 receipt로 연결했고 ARM64 PG17 replay를 통과했다.
이 문서는 그 수직 슬라이스를 다시 `완료`로 표시하지 않고, 남은 전체 명령 범위를
추적하는 원장으로만 사용한다.
`잔액`/`입금`/`출금`과 `보관물`/`받아`의 bank slice를 C `RBANK` 방 경계와 3억냥
상한, `모두` 해석, player gold/계정 잔액 원자 변경 및 canonical object-root graph
이동으로 연결했다. 로컬 unit/session/transport 회귀와 ARM64 PostgreSQL 17
`TestPostgresBankMoneyCommandPersistsAndReplays`/`TestPostgresBankItemCommandPersistsAndReplays`의
입금·아이템 보관·동일 command ID replay·출금을 통과했지만, 기존 bank graph 전체
이관과 상점·거래는 아직 별도 인수 범위다.

방 리소스 입장 경계도 추가했다. `AdmitLegacyRoom`은 strict decoder를 약화하지 않고
정책으로 승인된 text/trailing/path-ID issue만 catalog에 넣으며, 원본 bytes/hash/offset을
evidence로 보존한다. `LoadLegacyRoomCatalog`은 C path 규칙을 적용해 3,216개 파일 중
2,341개 canonical room과 875개 noncanonical artifact를 분리하고,
`LegacyRoomCatalog.NewState`가 빈 플레이어 초기 State를 만든다. 이관 예외를 숨기지
않는 구조·정책·catalog TDD는 통과했지만, legacy monster/object의 canonical ID 변환,
PG seed, 전체 room graph 및 runtime admission은 아직 `미구현; G1/G4`다.

## 2026-09-09 시간·수련·상인 선택 bounded lanes 및 cadence 재감사

## 2026-09-10 kind-8 은행 아티팩트 Go codec

`BankSnapshotV1` kind-8 CDTO를 Go에서 직접 encode/decode/inspect/verify한다. C/Rust와
같은 단일 detached ObjectGraphV1 root, canonical re-encode, whole-artifact SHA-256,
4 MiB limit을 적용하고, 결과는 오프라인 이관 증거일 뿐 계정·게임 상태 권한을 만들지
않는다. `Postgres.ImportBankSnapshot`은 이 codec 결과와 명시적인 item ID manifest를
이미 연결된 account/character에만 원자적으로 붙이고 `bank_imports` evidence 및
replayable `world_commands` receipt를 남긴다. DB 없이 private source tree를 검사하는
`InspectBankSnapshotReview(JSON)`과 `-inspect-bank-snapshot-dir`/
`-inspect-bank-snapshot-file` metadata-only CLI, terminal signup/reconnect 계약용
`web/lib/terminal-play-smoke.ts`도 추가했다. 감사된 LP64 `read_obj` raw stream을
kind-8으로 바꾸는 `legacy_bank_raw_v1.go`도 추가했지만, 실제 file-locator·계정 대조·
라이브 bank 명령/잔액 parity·운영 복구는 여전히 P0 후속 항목이다.

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `command8.c:prt_time` / `시간` | `world.ProjectTime`·`session.ExecuteTimeLine`·`WorldConnector` read-only receipt; game hour와 PST 관측값을 request에 고정 | world/session/transport race·replay 통과; 전체 C clock/local-time parity와 운영 시계 이관은 미완료 |
| `command7.c:train` / `수련` | `PlanTraining`/`ApplyTraining`의 RTRAIN/class gate, gold/XP progression, PUPDMG release, class transition | source threshold·level100/127 fixture, atomic stale/family fail-closed와 receipt replay 통과; family edit/broadcast recipient projection은 미확정 |
| `command10.c:selection` / `선택` | canonical `room.NPCIDs`·MPURIT 및 server-owned `MerchantOffers`의 deterministic read-only listing | occurrence/visibility/catalog/legacy-Carry rejection, immutable offer snapshot과 replay 통과; purchase/전체 merchant object migration은 별도 |

세 레인은 파일 소유권을 분리한 Luna max 병렬 작업 후 parser·transport만 메인에서
조립했다. `fast`는 변경된 world/session/transport만 race하고, `integration`은 조립
batch에서 전체 Go race/vet/diff를 한 번 실행한다. Linux ARM64 cross-build는 `main`,
실제 PostgreSQL·브라우저·x64/Windows/macOS matrix는 승인된 `release`에서만 실행한다.
workflow/pre-push를 전수 대조했으며 의도된 migration replay 외에 같은 고비용 검사를
기능 레인마다 반복하는 호출은 없다. 이번 배치에서는 ARM64·DB·browser·strict corpus를
재실행하지 않았다.

## 상태와 범위

2026-09-08 구현 추적 보충(전체 인수와 구분): Go 터미널 가입·로그인과 PG 초안
저장/재로그인은 로컬 DB·브라우저 흐름까지 구현했다. 실제 플레이 상태 초기화,
기존 계정 이관, 이동·전투·게임 저장은 아직 인수되지 않았다. `server/internal/world/`
에는 원본 방 구조 읽기와 조사 API, `display_rom`의 환경/몬스터 설명 출력 부분이
추가됐다. 63개 방의 데이터 예외 처리와 월드 런타임 연결은 미완료다. 아래 G0
표의 미구현 표시는 개별 전체 기능 인수 기준이며 이런 부분 구현을 완료로 세지 않는다.

- 2026-09-14 재집계: 활성 cmdlist 343행/174 handler를 인수 항목으로 연결했다.
  handler 상태는 연결 103 / 부분 19 / 미연결 52다. 이 숫자는 **G0 계약 원장**이며
  게임 전체 인수가 아니다. 개별 행의 최종 게임 인수는 승격 cadence 증거가 있을
  때만 닫는다.
- C/Rust/기존 웹 테스트는 비교·이관용 참고 자산이다. C 동작을 그대로 복제해야
  한다는 뜻이 아니며, 알려진 버그는 differential fixture에서 별도로 판정한다.
- `입력 fixture`/`Go 위치`/`검증`/`이관`/`남은 조건`은 2026-09-14 343/174 표에
  행마다 기록한다. 직접 handler 테스트 파일이 없으면 `CMD-GAP`이다. 게임 전체
  인수는 그 표의 `연결`과 별개다.
- `src/frp.new`와 사용자 소유 변경은 조사하지 않았고 수정하지 않았다.

## C dispatch의 근거와 재현 가능한 집계

실행 경계는 다음과 같다.

1. `src/command1.c:1407-1488`의 `command()`가 입력 한 줄을 받고 `!` 재실행을 처리한다.
2. `src/command1.c:1497-1571`의 `parse()`가 최대 `COMMANDMAX` 토큰/숫자를
   `cmd`에 채운다. 한글 문장 끝 처리, 공백/`#` 처리가 여기에 있다.
3. `src/command1.c:1581-1626`의 `process_cmd()`가 `cmdlist[]`를 순서대로
   exact/약어 비교하고, `*` DM 명령 권한을 확인한 뒤 `cmdfn`을 호출한다.
4. 실제 등록표는 `src/global.c:203-558`의 `cmdlist[]`이며 마지막 `@`가 sentinel이다.
   `눌러`/`밀어`의 `cmdfn=0` 두 행은 `special_cmd` 경로를 가리키는 특수 표식이다.
5. 주문 이름/레벨 표는 `src/global.c:571-635`의 `spllist[]`(활성 주문 56개),
   전투 주문 계수 표는 `src/global.c:637-665`의 `ospell[]`(현재 활성 행 20개)다.

다음은 파일을 변경하지 않고 현재 `cmdlist[]`를 집계하는 명령이다. C 주석과
문자열 내부 escape를 처리하므로, 문서의 수치를 갱신할 때 같은 명령을 다시
실행한다.

```sh
perl -0777 -ne '
  my $s = $_;
  $s =~ s!/\*.*?\*/!!gs;
  my ($b) = $s =~ /}\s*cmdlist\[\]\s*=\s*\{(.*?)\n\s*\};/s;
  my (%aliases, %handlers, %ids, $enabled, $special, $sentinel);
  while ($b =~ /\{\s*"((?:\\.|[^"\\])*)"\s*,\s*(-?\d+)\s*,\s*([A-Za-z_][A-Za-z0-9_]*|0)\s*\}/g) {
    my ($alias, $id, $fn) = ($1, $2, $3);
    $ids{$id}++;
    if ($fn eq "0") {
      $id == 0 ? $sentinel++ : $special++;
      next;
    }
    $enabled++;
    $aliases{$alias} = 1;
    $handlers{$fn} = 1;
  }
  printf "enabled_rows=%d unique_aliases=%d positive_ids=%d handlers=%d special_-2_rows=%d sentinel_rows=%d\n",
    $enabled, scalar(keys %aliases), scalar(grep { $_ > 0 } keys %ids),
    scalar(keys %handlers), $special, $sentinel;
' src/global.c
```

현재 출력:

```text
enabled_rows=343 unique_aliases=341 positive_ids=154 handlers=174 special_-2_rows=2 sentinel_rows=1
```

따라서 “명령 154개”는 numeric `cmdno`의 개수이고, 사용자가 입력할 수 있는
등록 행은 343개다. `북동`과 `남서`는 표에 중복 등록되어 고유 별칭 수와 행 수가
다르다. 깨진 바이트로 보이는 일부 이동 alias도 source에 실제로 등록된 값이므로
UTF-8로 임의 정규화하지 않는다. `은신술`/`가입`/`탈퇴`/`전수`/`변수나한권` 및
`sneak`는 C 주석 안에 있어 위 집계에 포함하지 않았다.

## 2026-09-14 G0 원장 재집계 — 343 등록 행 / 174 handler

G0 조사 재실행. 아래 표는 `src/global.c` 활성 `cmdlist[]`와 현재 작업 트리 Go parser/reducer를 대조한 **계약 원장**이다.
`연결`은 parse→plan/apply 심볼이 있다는 뜻이며 전체 C 출력·운영 PG·ARM64·browser E2E 인수가 아니다.
Go 심볼/분류가 없으면 `미연결`이다. 구현을 지어내지 않았고 `src/frp.new`는 조사·수정하지 않았다.
C source는 `src/Makefile` 운영 객체(`command5.c`)를 우선하고 `comman5_old.c`는 쓰지 않는다.

재현 명령(기존 G0 집계와 동일):

```sh
perl -0777 -ne '
  my $s = $_;
  $s =~ s!/\*.*?\*/!!gs;
  my ($b) = $s =~ /}\s*cmdlist\[\]\s*=\s*\{(.*?)\n\s*\};/s;
  my (%aliases, %handlers, %ids, $enabled, $special, $sentinel);
  while ($b =~ /\{\s*"((?:\\.|[^"\\])*)"\s*,\s*(-?\d+)\s*,\s*([A-Za-z_][A-Za-z0-9_]*|0)\s*\}/g) {
    my ($alias, $id, $fn) = ($1, $2, $3);
    $ids{$id}++;
    if ($fn eq "0") {
      $id == 0 ? $sentinel++ : $special++;
      next;
    }
    $enabled++;
    $aliases{$alias} = 1;
    $handlers{$fn} = 1;
  }
  printf "enabled_rows=%d unique_aliases=%d positive_ids=%d handlers=%d special_-2_rows=%d sentinel_rows=%d\n",
    $enabled, scalar(keys %aliases), scalar(grep { $_ > 0 } keys %ids),
    scalar(keys %handlers), $special, $sentinel;
' src/global.c
```

2026-09-14 출력:

```text
enabled_rows=343 unique_aliases=341 positive_ids=154 handlers=174 special_-2_rows=2 sentinel_rows=1
```

특수 행(343에 미포함): `눌러`/`밀어`는 `cmdfn=0` → `special_cmd`. sentinel `@` 1행.
주석 명령 `은신술`/`가입`/`탈퇴`/`전수`/`변수나한권`/`sneak`는 등록 기능으로 세지 않으며 유지·제외는 사용자 승인 대기.

Handler 합계: 연결 109 / 부분 21 / 미연결 44. 등록 행 합계: 연결 223 / 부분 26 / 미연결 94.
미연결 44 handler는 주로 DM(`dm1.c`–`dm6.c`)이다. `notepad`는 bounded
world/session/transport slice가 연결됐지만 원본 import·운영 경계가 남아 `부분`으로 집계한다.

### 계정·월드·전투·NPC·경제·마법·사회·관리·저장 계약

| 영역 | 확정 계약 | C 근거 | Go 위치 | fixture | 검증 | 이관 | 남은 조건 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 계정 | 웹 가입 없음. xterm 안에서 캐릭터 이름/비밀번호. 내부 ID와 이름 분리. 비밀번호는 출력/로그/receipt에서 redacted. bcrypt 1회. 동일 command ID replay는 해시 재생성 없음. Supabase Auth 비필수 | `command1.c` login/create, `command11.c:passwd`, `player.c` | `session` password/onboarding, `storage` bcrypt/PG | `server/internal/session/password_command_test.go` | 로컬 session/storage race 기록 있음. 이번 레인은 원장만 | 기존 비밀번호 검증 후 이관 | 운영 PG 전 계정 대조, continuation 전 경로, 기존 캐릭터 대량 이관 |
| 월드 | 방/출구/이동/look은 canonical room graph. 입력 7토큰. exact alias만 분류(약어 확장 미구현). 방향 alias는 source 바이트 유지 | `command2.c`/`command6.c`/`room.c`/`global.c:cmdlist` | `world/movement.go`, `session/look_command.go`, `session/directional_command.go` | `server/internal/session/look_command_test.go`, `server/internal/session/directional_command_test.go` | targeted race 기록 있음. 이번 레인은 원장만 | reviewed room manifest seed | 63방 corpus, 깨진 이동 alias 정규화 금지, 전체 display_rom parity |
| 전투 | 공격/도망/기습/교란/맹공/차기와 NPC melee는 snapshot-bound receipt. 사망은 PlanPlayerDeath/PlanNPCPlayerDeath. 문주 AT_WAR 사망 시 broadcast_all 2줄 | `command5.c`/`creature.c:die` | `world/attack.go` `flee.go` `player_death.go` `death_war.go` | `server/internal/session/attack_command_test.go`, `server/internal/world/player_death_test.go` | targeted race 기록 있음. 이번 레인은 원장만 | 전투 상태는 runtime | 전체 hit/armor/RNG parity, 운영 사망 영속 |
| NPC | talk ACTION/ATTACK/CAST/GIVE bounded. tick/chase/maintenance/resource는 scheduler. 미이관 catalog는 fail-closed | `command8.c:talk_action`, `update.c` | `world/npc_talk.go` `npc_combat_round.go` `npc_chase.go` | `server/internal/world/npc_talk_test.go` | focused race 기록 있음. 이번 레인은 원장만 | NPC identity graph import | 원본 talk corpus 전수, 리젠/침공 타이머 운영 연결 |
| 경제 | 상점 품목/구매/판매/가치/수리/교환/상인 구입·선택. 은행은 operator-owned PG import까지. 라이브 gold/graph parity는 별도 | `command7.c`/`bank.c` | `world/bank.go` shop/merchant/trade/repair, `world/forge.go`, `world/newforge.go` | `server/internal/session/bank_command_test.go`, `server/internal/session/forge_command_test.go`, `server/internal/session/newforge_command_test.go` | bank import race 기록 있음. 이번 레인은 원장만 | kind-8/raw locator | 라이브 이체, 대량 계좌 대조, 상점/거래 catalog |
| 마법 | `spllist` 56 + 활성 `ospell` 20. 플레이어 self-cast 다수와 NPC CAST 일부 연결. 대상/맵/공격 주문과 전체 spell effect는 미연결 | `global.c:spllist/ospell`, `magic1.c`–`magic8.c` | `world/cast.go` `spell_catalog.go` | `server/internal/session/cast_command_test.go` | focused race 기록 있음. 이번 레인은 원장만 | spell bit/timer canonical | 대상 주문, `[엔터]` continuation, zap/전주문 DM |
| 사회 | 말/잡담/환호/그룹말/패거리/결혼/투표/우편/게시판/메모/notepad/초대/감정표현. 패거리공지·선전포고·기억 연결 | `command4.c`/`post.c`/`action.c`/`command11.c`/`command12.c` | family_*/marriage/vote/mail/board/emote/notepad | `server/internal/session/family_news_command_test.go`, `server/internal/session/family_war_command_test.go`, `server/internal/world/notepad_test.go`, `server/internal/transport/world_connector_notepad_test.go` | targeted race 기록 있음. 이번 레인은 원장만 | social file → canonical | 패거리 보상, 운영 news/war/notepad import, 전체 C 출력 |
| 관리 | cmdno 101–147 `*` 명령은 클래스 게이트가 계약. 현재 Go는 `*active`/`*활성`, `*enemy`/`*적`, `*charm`/`*최면`, `*떨어져라`/`*침공`을 bounded 연결. 나머지 DM은 미연결 | `dm1.c`–`dm6.c`/`update.c` | `world/dm_active.go`, `world/dm_enemy.go`, `world/dm_charm.go`, `world/dm_family.go` | `server/internal/world/dm_active_test.go`, `server/internal/world/dm_enemy_test.go`, `server/internal/world/dm_charm_test.go`, `server/internal/world/dm_family_test.go` | targeted race 기록 있음. 이번 레인은 원장만 | 특권 명령 별도 suite | NPC producer/tick/AI·공격·추종·리젠, teleport/save/reload/shutdown 등 나머지 DM handler |
| 저장 | PostgreSQL가 영속 권위. command ID+상태 버전 트랜잭션. raw C struct를 DB에 직접 복사하지 않음. runtime descriptor/RNG는 비영속 | `mstruct.h` `player_store.c` `files1.c` | storage/world snapshot import | player/bank snapshot 테스트 | 로컬 PG 선택 실행 기록 있음. 이번 레인은 원장만 | PlayerSnapshotV1/object graph | 운영 백업·복구, 대량 플레이어 대조, live bank |

### 174 handler 인수 항목

| handler | cmdno | alias 수 | C source | Go 상태 | Go 위치 | fixture | 이관/남은 조건 |
| --- | ---: | ---: | --- | --- | --- | --- | --- |
| `absorb` | 89 | 1 | `src/magic3.c:283` | 연결 | CommandAbsorb; world/absorb.go | `server/internal/session/absorb_command_test.go`, `server/internal/world/absorb_test.go` | 흡성대법 bounded |
| `accurate` | 88 | 1 | `src/command9.c:317` | 연결 | CommandPowerAccuracy; world/power_accuracy.go | `server/internal/session/power_accuracy_command_test.go`, `server/internal/world/power_accuracy_test.go` | 살기충전 bounded |
| `action` | 100 | 40 | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| `attack` | 23 | 4 | `src/command5.c:36` | 연결 | CommandAttack; world/attack.go | `server/internal/session/attack_command_test.go`, `server/internal/world/attack_test.go` | 전체 combat parity 미완료 |
| `backstab` | 45 | 1 | `src/command7.c:342` | 연결 | CommandBackstab; world/backstab.go | `server/internal/session/backstab_command_test.go`, `server/internal/world/backstab_test.go` | 은신/위치 전수 미완료 |
| `bank` | 63 | 1 | `src/bank.c:157` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go`, `server/internal/world/bank_test.go` | operator import 있음. 라이브 graph/gold parity 미완료 |
| `bank_inv` | 63 | 1 | `src/bank.c:114` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 보관물 alias는 Bank 경로. bank_inv 심볼 자체는 Go에 없음 |
| `bash` | 51 | 1 | `src/command8.c:489` | 연결 | CommandBash; world/bash.go | `server/internal/session/bash_command_test.go`, `server/internal/world/bash_test.go` | 문/전투 분기 bounded |
| `boss_family` | 148 | 1 | `src/command11.c:597` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go`, `server/internal/session/family_mutation_command_test.go` | 가입허가 bounded |
| `broadsend` | 59 | 2 | `src/command4.c:508` | 연결 | CommandBroadcast; world/broadcast.go | `server/internal/session/broadcast_command_test.go`, `server/internal/world/broadcast_test.go` | 채널/권한 전수 미완료 |
| `broadsend2` | 70 | 1 | `src/command4.c:571` | 연결 | CommandBroadcast; world/broadcast.go | `server/internal/session/broadcast_command_test.go`, `server/internal/world/broadcast_test.go` | 환호 채널 bounded |
| `burn` | 83 | 2 | `src/command2.c:1693` | 연결 | CommandBurn; world/burn.go | `server/internal/session/burn_command_test.go`, `server/internal/world/burn_test.go` | 소각 대상 전수 미완료 |
| `buy` | 42 | 1 | `src/command7.c:169` | 연결 | CommandShopPurchase; world shop/merchant | `server/internal/session/shop_purchase_command_test.go`, `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_purchase_name_test.go` | 가격/재고 전수 미완료 |
| `buy_states` | 149 | 1 | `src/command11.c:954` | 연결 | CommandBuyStates; session/buy_states_command.go + world/buy_states.go + transport route | `server/internal/session/buy_states_command_test.go`, `server/internal/world/buy_states_test.go`, `server/internal/transport/world_connector_buy_states_test.go` | 향상 deterministic apply/receipt/replay bounded. live RNG·전체 G3 미완료 |
| `call_war` | 72 | 1 | `src/special1.c:182` | 연결 | CommandFamilyWar; world/family_war.go | `server/internal/session/family_war_command_test.go`, `server/internal/world/family_war_test.go` | 문주 사망 종료·운영 War 영속·보상 미완료 |
| `cast` | 38 | 1 | `src/magic1.c:23` | 부분 | CommandCast; world/cast.go + spell_catalog.go + summon.go | `server/internal/session/cast_command_test.go`, `server/internal/world/cast_test.go`, `server/internal/world/summon_test.go` | self-cast 다수·천리안·소환 연결. 공격/맵/기타 대상 주문·[엔터] continuation 미완료 |
| `change_class` | 86 | 1 | `src/command7.c:1111` | 연결 | CommandChangeClass; world/change_class.go | `server/internal/session/change_class_command_test.go`, `server/internal/world/change_class_test.go` | confirmation bounded |
| `chg_name` | 95 | 1 | `src/command8.c:1040` | 연결 | CommandItemRename; world/item_rename.go | `server/internal/session/item_rename_command_test.go`, `server/internal/world/item_rename_test.go` | 명명 suffix bounded |
| `circle` | 50 | 1 | `src/command8.c:342` | 연결 | CommandCircle; world/circle.go | `server/internal/session/circle_command_test.go`, `server/internal/world/circle_test.go` | 전투 위치 전수 미완료 |
| `clear` | 28 | 1 | `src/command5.c:983` | 연결 | CommandSettings; world/settings.go | `server/internal/world/settings_test.go` | flag 전수 미완료 |
| `clear_title` | 84 | 1 | `src/alias.c:463` | 연결 | CommandTitle; world/title.go | `server/internal/session/title_command_test.go`, `server/internal/world/title_test.go` | 칭호삭제 bounded. clear_title 심볼은 PlanClearTitle |
| `closeexit` | 32 | 1 | `src/command6.c:419` | 연결 | CommandDoor; world/doors.go | `server/internal/world/doors_test.go` | 열쇠/함정 전수 미완료 |
| `del_board` | 93 | 1 | `src/board.c:390` | 연결 | CommandBoard; world/board.go | `server/internal/session/board_command_test.go`, `server/internal/world/board_test.go` | 글삭제 bounded |
| `deposit` | 63 | 1 | `src/bank.c:338` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 라이브 transfer 미완료 |
| `description` | 73 | 1 | `src/command12.c:488` | 연결 | CommandDescription; world/description.go | `server/internal/session/description_command_test.go`, `server/internal/world/description_test.go` | UTF-8 길이 전수 미완료 |
| `divorce` | 150 | 1 | `src/command11.c:1291` | 연결 | CommandDivorce; world/marriage_followup.go | `server/internal/session/marriage_followup_command_test.go`, `server/internal/world/marriage_followup_test.go` | 이혼 bounded |
| `dm_ac` | 110 | 2 | `src/dm1.c:668` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_add_rom` | 119 | 2 | `src/dm2.c:580` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_append` | 132 | 2 | `src/dm5.c:401` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_attack` | 144 | 2 | `src/dm6.c:163` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_broadecho` | 129 | 2 | `src/dm4.c:127` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_cast` | 134 | 2 | `src/dm4.c:176` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_create_crt` | 117 | 3 | `src/dm1.c:515` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_create_obj` | 105 | 3 | `src/dm1.c:488` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_crt_name` | 139 | 2 | `src/dm4.c:683` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_delete` | 137 | 2 | `src/dm5.c:104` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_dust` | 141 | 2 | `src/dm6.c:22` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_echo` | 112 | 2 | `src/dm1.c:311` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_finger` | 124 | 2 | `src/dm3.c:751` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_flush_crtobj` | 116 | 3 | `src/dm1.c:423` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_flushsave` | 113 | 2 | `src/dm1.c:353` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_follow` | 142 | 2 | `src/dm6.c:86` | 연결 | CommandDMFollow; world/dm_follow.go | `server/internal/world/dm_follow_test.go`, `server/internal/session/dm_follow_command_test.go`, `server/internal/world/logout_test.go` | *따르기. 로그아웃 MDMFOL 정리 bounded |
| `dm_force` | 115 | 3 | `src/dm1.c:704` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_group` | 135 | 2 | `src/dm4.c:419` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_help` | 143 | 2 | `src/dm5.c:660` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_info` | 126 | 2 | `src/dm3.c:852` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_invis` | 107 | 3 | `src/dm1.c:639` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_list` | 125 | 2 | `src/dm3.c:815` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_loadlockout` | 123 | 2 | `src/dm3.c:729` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_log` | 121 | 2 | `src/dm3.c:694` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_monster` | 148 | 1 | `src/dm5.c:632` | 연결 | CommandDMFamily; world/dm_family.go | `server/internal/world/dm_family_test.go`, `server/internal/session/dm_family_command_test.go` | *침공. 원본 몬스터 템플릿 전수 미완료 |
| `dm_moonstone` | 148 | 1 | `src/dm5.c:607` | 연결 | CommandDMFamily; world/dm_family.go | `server/internal/world/dm_family_test.go`, `server/internal/session/dm_family_command_test.go` | *떨어져라. RMAX 전수 load_rom 미완료 |
| `dm_nameroom` | 131 | 2 | `src/dm5.c:356` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_obj_name` | 138 | 2 | `src/dm4.c:546` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_param` | 127 | 2 | `src/dm4.c:19` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_perm` | 106 | 2 | `src/dm1.c:610` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_prepend` | 133 | 2 | `src/dm5.c:512` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_purge` | 109 | 3 | `src/dm1.c:179` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_reload_rom` | 103 | 2 | `src/dm1.c:445` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_replace` | 130 | 2 | `src/dm5.c:26` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_resave` | 104 | 2 | `src/dm1.c:466` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_rmstat` | 102 | 3 | `src/dm1.c:404` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_save_all_ply` | 147 | 1 | `src/dm1.c:13` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_send` | 108 | 3 | `src/dm1.c:134` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_set` | 120 | 1 | `src/dm3.c:20` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_shutdown` | 114 | 2 | `src/dm1.c:381` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_silence` | 128 | 2 | `src/dm4.c:72` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_spy` | 122 | 2 | `src/dm2.c:632` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_stat` | 118 | 2 | `src/dm2.c:24` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_teleport` | 101 | 2 | `src/dm1.c:28` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `dm_users` | 111 | 3 | `src/dm1.c:234` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| `drink` | 58 | 2 | `src/magic1.c:496` | 연결 | CommandDrink; world/drink.go | `server/internal/session/drink_command_test.go`, `server/internal/world/drink_test.go` | 물약/음식 효과 전수 미완료 |
| `drop` | 7 | 2 | `src/command2.c:1263` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| `emote` | 25 | 1 | `src/command11.c:30` | 연결 | CommandEmote; world/emote.go | `server/internal/world/emote_test.go` | NPC 대상·occurrence fail-closed |
| `enemy_status` | 154 | 1 | `src/command8.c:1358` | 연결 | CommandEnemyStatus; session enemy status | `server/internal/session/enemy_status_command_test.go` | 상태 bounded |
| `equipment` | 11 | 2 | `src/command3.c:606` | 연결 | CommandItems; session items/equipment | `server/internal/session/equipment_command_test.go` | 출력 parity 미완료 |
| `family` | 148 | 1 | `src/command11.c:506` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go`, `server/internal/session/family_mutation_command_test.go` | 가입 신청. family_gold ledger 없으면 fail-closed |
| `family_member` | 148 | 1 | `src/command12.c:326` | 연결 | CommandFamilyMember; session family member | `server/internal/session/family_status_command_test.go` | 패거리원 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| `family_news` | 148 | 1 | `src/post.c:290` | 연결 | CommandFamilyNews; world/family_news.go | `server/internal/session/family_news_command_test.go`, `server/internal/world/family_news_test.go` | view_file 페이지/운영 import 미완료 |
| `family_talk` | 148 | 2 | `src/command11.c:741` | 연결 | CommandFamilyTalk; world/family_talk.go | `server/internal/world/family_talk_test.go` | 패거리말/] bounded |
| `family_who` | 148 | 1 | `src/command11.c:785` | 연결 | CommandFamilyWho; session family who | `server/internal/session/family_status_command_test.go` | 온라인 목록 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| `flee` | 37 | 2 | `src/command7.c:19` | 연결 | CommandFlee; world/flee.go | `server/internal/session/flee_command_test.go`, `server/internal/world/flee_test.go` | 전투 중 방향 인자는 fail-closed |
| `fm_out` | 148 | 1 | `src/command12.c:155` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go` | 패거리추방 bounded |
| `follow` | 18 | 1 | `src/command4.c:641` | 부분 | CommandFollow; session follow | `server/internal/session/follow_command_test.go` | 추종자 graph 일부, C follow 전수 미완료 |
| `forge` | 85 | 1 | `src/command7.c:669` | 부분 | CommandForge; world/forge.go | `server/internal/session/forge_command_test.go`, `server/internal/world/forge_test.go`, `server/internal/transport/world_connector_forge_test.go` | 제련 select_arm 1-6 연결. `무기만들기`는 newforge 경로 |
| `get` | 5 | 4 | `src/command2.c:697` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| `give` | 47 | 1 | `src/command8.c:31` | 연결 | CommandGive; world/give.go | `server/internal/session/give_command_test.go`, `server/internal/world/give_test.go` | NPC GIVE talk와 별개 |
| `go` | 30 | 2 | `src/command6.c:76` | 부분 | CommandGo; world/go.go + npc_chase.go | `server/internal/session/go_command_test.go`, `server/internal/world/go_test.go`, `server/internal/world/npc_chase_test.go` | command6 MFOLLO chase(threshold 10, no die_perm_crt). 특수 입구 전수·닫힘/비행/시간/성별 입장 미완료 |
| `group` | 20 | 2 | `src/command4.c:832` | 부분 | CommandSocial; session social/group | `server/internal/session/social_command_test.go`, `server/internal/world/social_group_command_test.go` | NPC PDMINV·mixed display bounded. nil `FollowerRefs` fallback·그룹 구성 전수 미완료 |
| `gtalk` | 57 | 3 | `src/command10.c:244` | 연결 | CommandGroupTalk; world/group_talk.go | `server/internal/world/group_talk_test.go` | 그룹 멤버십 전수 미완료 |
| `haste` | 64 | 1 | `src/command9.c:87` | 연결 | CommandRangerPray/Haste; world/ranger_pray.go | `server/internal/session/ranger_pray_command_test.go`, `server/internal/world/ranger_pray_test.go` | 활보법 bounded |
| `health` | 15 | 2 | `src/command4.c:45` | 부분 | CommandStatus; session status/health | `server/internal/session/status_command_test.go` | 전체 점수 필드 parity 미완료 |
| `help` | 14 | 2 | `src/command4.c:133` | 부분 | CommandHelp; session help/document | `server/internal/session/help_command_test.go` | 정적 docs 연결 일부, 전체 help corpus 미완료 |
| `hide` | 26 | 2 | `src/command5.c:744` | 연결 | CommandHide; world/hide.go | `server/internal/session/hide_command_test.go`, `server/internal/world/hide_test.go` | 바닥 객체/플레이어 분기 bounded |
| `hold` | 12 | 2 | `src/command3.c:860` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | shots/flags 전수 미완료 |
| `ignore` | 68 | 1 | `src/command9.c:564` | 연결 | CommandIgnore; session ignore | `server/internal/session/ignore_command_test.go` | connection-local, PDMINV 검사 |
| `info` | 16 | 1 | `src/command4.c:249` | 부분 | CommandInfo; session info | `server/internal/session/info_command_test.go` | 후속 페이지/주문 목록 일부, 전체 info_2 parity 미완료 |
| `info_obj` | 96 | 1 | `src/command3.c:958` | 연결 | CommandObjectAppraisal; session object_appraisal | `server/internal/session/object_appraisal_command_test.go` | 감정 bounded |
| `inventory` | 6 | 1 | `src/command2.c:1196` | 연결 | CommandItems; session items/equipment | `server/internal/session/items_command_test.go` | 전체 C 출력 parity 미완료 |
| `invite` | 152 | 1 | `src/command12.c:362` | 연결 | CommandPropertyInvite; world/property_invite.go | `server/internal/world/property_invite_test.go` | 초대 bounded |
| `kick` | 97 | 1 | `src/command8.c:1176` | 연결 | CommandKick; world/kick.go | `server/internal/session/kick_command_test.go`, `server/internal/world/kick_test.go` | 차기 bounded |
| `list` | 41 | 1 | `src/command7.c:139` | 연결 | CommandShopList; session shop | `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_marketplace_test.go` | 상점 storage 전수 미완료 |
| `list_act` | 140 | 2 | `src/update.c:1010` | 연결 | CommandDMActive; session/dm_active_command.go + world/dm_active.go + transport route | `server/internal/session/dm_active_command_test.go`, `server/internal/world/dm_active_test.go`, `server/internal/transport/world_connector_dm_active_test.go` | ActiveNPCIDs canonical order·DM gate·read-only receipt/replay·unresolved identity fail-closed bounded. active producer/tick/AI·PG·full G3 미완료 |
| `list_charm` | 146 | 2 | `src/dm6.c:268` | 연결 | CommandDMCharm; session/dm_charm_command.go + world/dm_charm.go + transport route | `server/internal/session/dm_charm_command_test.go`, `server/internal/world/dm_charm_test.go`, `server/internal/transport/world_connector_dm_charm_test.go` | global online player target·CharmRefs canonical order·receipt/replay bounded. Charm producer/spell mutation·expiry·NPC AI/tick·PG/full G3 미완료 |
| `list_enm` | 145 | 2 | `src/dm6.c:226` | 연결 | CommandDMEnemy; session/dm_enemy_command.go + world/dm_enemy.go + transport route | `server/internal/session/dm_enemy_command_test.go`, `server/internal/world/dm_enemy_test.go`, `server/internal/transport/world_connector_dm_enemy_test.go` | same-room NPC enemy list·canonical occurrence/order·receipt/replay bounded. NPC producers/tick/AI·공격·추종·리젠·PG/full G3 미완료 |
| `list_family` | 148 | 1 | `src/command11.c:859` | 연결 | CommandFamilyList; session family list | `server/internal/session/family_status_command_test.go` | 모든패거리 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| `lock` | 34 | 1 | `src/command6.c:553` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 키 객체 graph 전수 미완료 |
| `look` | 2 | 3 | `src/command2.c:37` | 연결 | CommandLook; session/look_command.go + world/look.go | `server/internal/session/look_command_test.go`, `server/internal/world/look_test.go` | 출구 peek + find_obj(인벤→ready→방) + first_mon + display_rom 전투 안내. 닫힘/지도없음/RONMAR·RONFML/PBLIND. `나`/first_ply는 미완료 |
| `look_board` | 94 | 1 | `src/board.c:66` | 연결 | CommandBoard; world/board.go | `server/internal/session/board_command_test.go` | 게시판 조회. look_board 심볼명 없음 |
| `lose` | 19 | 1 | `src/command4.c:743` | 부분 | CommandFollow; session follow | `server/internal/session/follow_command_test.go` | 내보내 전수 미완료 |
| `m_send` | 150 | 1 | `src/command11.c:1247` | 연결 | CommandMarriageSend; world/marriage_followup.go | `server/internal/session/marriage_followup_command_test.go` | 사랑말 bounded |
| `magic_stop` | 91 | 1 | `src/command7.c:1203` | 연결 | CommandMagicStop; world/magic_stop.go | `server/internal/session/magic_stop_command_test.go`, `server/internal/world/magic_stop_test.go` | 혈도봉쇄 bounded |
| `marriage` | 150 | 1 | `src/command11.c:1124` | 연결 | CommandMarriage; world/marriage.go | `server/internal/session/marriage_command_test.go`, `server/internal/world/marriage_test.go` | 신청/수락 bounded |
| `meditate` | 90 | 1 | `src/command9.c:378` | 연결 | CommandMeditate; world/meditate.go | `server/internal/session/meditate_command_test.go`, `server/internal/world/meditate_test.go` | 참선 bounded |
| `memo` | 151 | 1 | `src/command12.c:85` | 연결 | CommandMemo; world/memo.go | `server/internal/session/memo_command_test.go`, `server/internal/world/memo_test.go` | 메모 bounded |
| `moon_set` | 153 | 1 | `src/command8.c:1124` | 연결 | CommandMoonSet; world/moon_set.go | `server/internal/session/moon_set_command_test.go`, `server/internal/world/moon_set_test.go` | EQUAL prefix/OINVIS·장비/중첩 미완료 |
| `move` | 1 | 45 | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| `newforge` | 85 | 1 | `src/command7.c:871` | 부분 | CommandNewForge; world/newforge.go | `server/internal/session/newforge_command_test.go`, `server/internal/world/newforge_test.go`, `server/internal/transport/world_connector_newforge_test.go` | 무기만들기 first prompt/gate + select_newarm case 2-6(900-904, 에메랄드/티타늄/일루션 forge2, 담금질 shots/sum, 이름 3-20바이트, 예 접두 금화 차감·add_obj_crt). 상점/거래 catalog는 미완료 |
| `notepad` | 136 | 2 | `src/post.c:201` | 부분 | CommandNotepad; world/notepad.go + session/notepad_command.go + transport continuation | `server/internal/world/notepad_test.go`, `server/internal/session/notepad_command_test.go`, `server/internal/transport/world_connector_notepad_test.go` | exact alias·CARETAKER·view/append/clear·79-byte/dot·receipt/replay bounded. DM_pad import·운영 PG/복구·전체 C 출력 미완료 |
| `obj_compare` | 96 | 1 | `src/command12.c:21` | 연결 | CommandCompare; session object_compare | `server/internal/session/object_compare_command_test.go` | 비교 bounded |
| `openexit` | 31 | 1 | `src/command6.c:365` | 연결 | CommandDoor; world/doors.go | `server/internal/world/doors_test.go` | 열쇠/함정 전수 미완료 |
| `out_family` | 148 | 1 | `src/command11.c:662` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go` | 패거리탈퇴. out_family 심볼명 없음(PlanFamilyLeave) |
| `output_bank` | 63 | 1 | `src/bank.c:253` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 잔액 출력 bounded. output_bank 심볼 없음 |
| `passwd` | 78 | 1 | `src/command11.c:83` | 부분 | CommandPassword; session password + storage bcrypt | `server/internal/session/password_command_test.go` | 게임 연결 continuation 일부, 운영 계정 이관 미완료 |
| `peek` | 22 | 1 | `src/command4.c:955` | 연결 | CommandPeek; world/peek.go | `server/internal/session/peek_command_test.go`, `server/internal/world/peek_test.go` | OINVIS 전수 미완료 |
| `pfinger` | 80 | 1 | `src/command11.c:400` | 연결 | CommandPlayerLookup; session player lookup | `server/internal/session/player_lookup_command_test.go` | 사용자정보 bounded |
| `picklock` | 35 | 1 | `src/command6.c:641` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 스킬/RNG 전수 미완료 |
| `ply_aliases` | 82 | 2 | `src/alias.c:260` | 연결 | CommandAlias; world/alias.go | `server/internal/session/alias_command_test.go`, `server/internal/world/alias_test.go` | prefix/suffix 둘 다. 영속 전수 미완료 |
| `ply_suicide` | 77 | 1 | `src/command5.c:653` | 연결 | CommandSuicide; session/suicide_command.go + world/suicide.go + transport route | `server/internal/session/suicide_command_test.go`, `server/internal/world/suicide_test.go`, `server/internal/transport/world_connector_suicide_test.go` | 확인/암호 continuation·atomic death·receipt/replay bounded. 운영 계정/전체 C parity 미완료 |
| `poison_mon` | 98 | 1 | `src/command7.c:1308` | 연결 | CommandPoison; world/poison.go | `server/internal/session/poison_command_test.go`, `server/internal/world/poison_test.go` | 독살포 bounded |
| `postdelete` | 55 | 1 | `src/post.c:174` | 연결 | CommandMail; world/mail.go | `server/internal/world/mail_test.go` | 삭제 순서 전수 미완료 |
| `postread` | 54 | 1 | `src/post.c:134` | 연결 | CommandMail; world/mail.go | `server/internal/world/mail_test.go` | 페이지/정렬 전수 미완료 |
| `postsend` | 53 | 1 | `src/post.c:28` | 연결 | CommandMailSend; world/mail_send.go | `server/internal/world/mail_send_test.go` | 우편 파일 이관 전수 미완료 |
| `power` | 87 | 1 | `src/command9.c:259` | 연결 | CommandPowerAccuracy; world/power_accuracy.go | `server/internal/session/power_accuracy_command_test.go`, `server/internal/world/power_accuracy_test.go` | 기공집결 bounded |
| `pray` | 65 | 1 | `src/command9.c:145` | 연결 | CommandRangerPray; world/ranger_pray.go | `server/internal/session/ranger_pray_command_test.go`, `server/internal/world/ranger_pray_test.go` | 신원법 bounded |
| `prepare` | 66 | 1 | `src/command9.c:437` | 연결 | CommandPrepare; world/prepare_updmg.go | `server/internal/world/prepare_updmg_test.go` | 경계 bounded |
| `prt_time` | 49 | 1 | `src/command8.c:311` | 연결 | CommandTime/CommandRead; session time | `server/internal/session/time_command_test.go` | 게임 달력 전수 미완료 |
| `purchase` | 74 | 1 | `src/command10.c:492` | 연결 | CommandShopPurchase/MerchantPurchase; world/merchant_purchase.go | `server/internal/session/merchant_purchase_command_test.go`, `server/internal/world/merchant_purchase_test.go` | 상인 구입 bounded |
| `quit` | 3 | 1 | `src/command5.c:1079` | 부분 | CommandQuit; session quit/save 경로 | `server/internal/session/quit_command_test.go` | 종료 저장·세션 정리의 운영 인수는 미완료 |
| `readscroll` | 40 | 1 | `src/magic1.c:352` | 연결 | CommandReadScroll; world/read_scroll.go | `server/internal/world/read_scroll_test.go` | 게시판 `읽어`와 구분 |
| `ready` | 13 | 1 | `src/command3.c:691` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | 무장 슬롯 전수 미완료 |
| `remove_obj` | 10 | 1 | `src/command3.c:487` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | ready↔inventory 정규화 일부 |
| `repair` | 48 | 1 | `src/command8.c:204` | 연결 | CommandRepair; world/repair.go | `server/internal/session/repair_command_test.go`, `server/internal/world/repair_test.go` | 비용/성공률 전수 미완료 |
| `resend` | 153 | 2 | `src/command12.c:525` | 연결 | CommandReply; session reply | `server/internal/session/reply_command_test.go` | 대답/`/` bounded |
| `return_square` | 81 | 2 | `src/command1.c:1797` | 연결 | CommandReturnSquare; world/return_square.go | `server/internal/session/return_square_command_test.go`, `server/internal/world/return_square_test.go` | 광장 좌표/비용 전수 미완료 |
| `savegame` | 52 | 1 | `src/command8.c:717` | 부분 | CommandSave; session save | `server/internal/session/save_command_test.go` | 운영 PG 자동저장 인수는 미완료 |
| `say` | 4 | 3 | `src/command2.c:642` | 연결 | CommandSay; world/say.go | `server/internal/world/say_test.go` | prefix/occurrence 확장 없음 |
| `search` | 24 | 2 | `src/command5.c:549` | 연결 | CommandSearch; world/search.go | `server/internal/session/search_command_test.go`, `server/internal/world/search_test.go` | 숨김 객체 전수 미완료 |
| `selection` | 75 | 1 | `src/command10.c:597` | 연결 | CommandSelection; world/selection.go | `server/internal/session/selection_command_test.go`, `server/internal/world/selection_test.go` | 상인 선택 bounded |
| `sell` | 43 | 1 | `src/command7.c:227` | 연결 | CommandShopSell; session shop sell | `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_marketplace_test.go` | 가격/재고 전수 미완료 |
| `sendman` | 17 | 2 | `src/command4.c:408` | 연결 | CommandDirectMessage; world/direct_message.go | `server/internal/session/direct_message_command_test.go`, `server/internal/world/direct_message_test.go` | 오프라인/길이 경계 일부 |
| `set` | 27 | 1 | `src/command5.c:895` | 연결 | CommandSettings; world/settings.go | `server/internal/world/settings_test.go` | flag 전수 미완료 |
| `set_title` | 84 | 1 | `src/alias.c:432` | 연결 | CommandTitle; world/title.go | `server/internal/session/title_command_test.go`, `server/internal/world/title_test.go` | 칭호 설정 bounded |
| `steal` | 36 | 1 | `src/command6.c:723` | 연결 | CommandSteal; world/steal.go | `server/internal/session/steal_command_test.go`, `server/internal/world/steal_test.go` | NPC/플레이어 분기 bounded |
| `study` | 39 | 2 | `src/magic1.c:259` | 연결 | CommandStudy; world/study.go | `server/internal/session/study_command_test.go`, `server/internal/world/study_test.go` | 직업/레벨 전수 미완료 |
| `talk` | 56 | 1 | `src/command8.c:817` | 부분 | CommandNPCTalk; world/npc_talk.go | `server/internal/world/npc_talk_test.go` | ATTACK/CAST/GIVE/ACTION 일부. 원본 talk corpus 전수 미완료 |
| `teach` | 71 | 1 | `src/magic1.c:125` | 연결 | CommandTeach; world/teach.go | `server/internal/session/teach_command_test.go`, `server/internal/world/teach_test.go` | 전수 비트 전수 미완료 |
| `track` | 21 | 1 | `src/command4.c:896` | 연결 | CommandTrack; world/track.go | `server/internal/session/track_command_test.go`, `server/internal/world/track_test.go` | RNG/스킬 전수 미완료 |
| `trade` | 76 | 1 | `src/command10.c:664` | 연결 | CommandTrade; session/world trade | `server/internal/session/trade_command_test.go` | 교환 전수 미완료 |
| `train` | 46 | 1 | `src/command7.c:529` | 연결 | CommandTrain; world/training.go | `server/internal/session/training_command_test.go`, `server/internal/world/training_test.go` | stat 한도 전수 미완료 |
| `turn` | 62 | 1 | `src/magic3.c:155` | 연결 | CommandTurn; world/turn.go | `server/internal/session/turn_command_test.go`, `server/internal/world/turn_test.go` | 언데드 대상 전수 미완료 |
| `unlock` | 33 | 1 | `src/command6.c:472` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 키 객체 graph 전수 미완료 |
| `up_dmg` | 99 | 1 | `src/command9.c:200` | 연결 | CommandUpDmg; world/prepare_updmg.go | `server/internal/world/prepare_updmg_test.go` | 잠력격발 bounded |
| `use` | 67 | 1 | `src/command9.c:480` | 연결 | CommandUse; world/use.go | `server/internal/session/use_command_test.go`, `server/internal/world/use_test.go` | 객체 효과 전수 미완료 |
| `value` | 44 | 2 | `src/command7.c:294` | 연결 | CommandValue; session value | `server/internal/session/value_command_test.go` | 감정 전수 미완료 |
| `vote` | 79 | 1 | `src/command11.c:190` | 연결 | CommandVote; world/vote.go | `server/internal/session/vote_command_test.go`, `server/internal/world/vote_test.go` | raw→manifest 있음. 라이브 투표 전수 미완료 |
| `wear` | 9 | 1 | `src/command3.c:21` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | wear restriction 전수 미완료 |
| `welcome` | 61 | 1 | `src/command4.c:227` | 부분 | CommandWelcome; session welcome | `server/internal/session/welcome_command_test.go` | 출력/대상 전수 미완료 |
| `who` | 8 | 1 | `src/command5.c:367` | 부분 | CommandSocial; session social who | `server/internal/session/social_command_test.go` | 누구 출력 전수 미완료 |
| `whois` | 69 | 1 | `src/command5.c:503` | 연결 | CommandPlayerLookup; session player lookup | `server/internal/session/player_lookup_command_test.go` | 오프라인/권한 전수 미완료 |
| `withdraw` | 63 | 1 | `src/bank.c:401` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 라이브 transfer 미완료 |
| `writeboard` | 92 | 1 | `src/board.c:176` | 연결 | CommandBoardWrite; world/board_write.go | `server/internal/world/board_write_test.go` | 써 bounded. writeboard 심볼명 없음 |
| `yell` | 29 | 1 | `src/command6.c:21` | 연결 | CommandYell; world/yell.go | `server/internal/session/yell_command_test.go`, `server/internal/world/yell_test.go` | 권역/방 제한 일부 |
| `zap` | 60 | 1 | `src/magic1.c:673` | 연결 | CommandZap; session/zap_command.go + world/zap.go + transport route | `server/internal/session/zap_command_test.go`, `server/internal/world/zap_test.go`, `server/internal/transport/world_connector_zap_test.go` | wand selector·vigor bounded, direct/Ready EQUAL·receipt/replay 확인. 전체 spell catalog/effect·PG/E2E 미완료 |

### 343 등록 행 인수 항목

| # | cmdno | alias | handler | C source | Go 상태 | Go 위치 | fixture | 이관/남은 조건 |
| ---: | ---: | --- | --- | --- | --- | --- | --- | --- |
| 1 | 1 | `나가는길` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 2 | 1 | `광장` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 3 | 1 | `향로` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 4 | 1 | `수련장` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 5 | 1 | `현감에게` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 6 | 1 | `면회허가` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 7 | 1 | `반하도인` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 8 | 1 | `월광반` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 9 | 1 | `반야바라밀` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 10 | 1 | `8` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 11 | 1 | `북` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 12 | 1 | `ㅂ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 13 | 1 | `�ぃ�` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 14 | 1 | `2` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 15 | 1 | `남` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 16 | 1 | `ㄴ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 17 | 1 | `�ⅲ�` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 18 | 1 | `6` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 19 | 1 | `동` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 20 | 1 | `ㄷ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 21 | 1 | `�┌�` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 22 | 1 | `4` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 23 | 1 | `서` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 24 | 1 | `ㅅ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 25 | 1 | `�В�` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 26 | 1 | `북동` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 27 | 1 | `ㅂㄷ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 28 | 1 | `북서` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 29 | 1 | `북동` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 30 | 1 | `남서` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 31 | 1 | `ㅂㅅ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 32 | 1 | `남동` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 33 | 1 | `ㄴㄷ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 34 | 1 | `남서` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 35 | 1 | `ㄴㅅ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 36 | 1 | `9` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 37 | 1 | `위` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 38 | 1 | `ㅇ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 39 | 1 | `��＂` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 40 | 1 | `3` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 41 | 1 | `밑` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 42 | 1 | `ㅁ` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 43 | 1 | `�ð�` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 44 | 1 | `밖` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 45 | 1 | `나가` | `move` | `src/command2.c:287` | 연결 | CommandDirectional; session/directional_command.go + world/movement.go | `server/internal/session/directional_command_test.go`, `server/internal/world/movement_test.go` | 약어·깨진 바이트 alias는 source 바이트 유지. 함정/추종/입장은 별도 reducer |
| 46 | 2 | `봐` | `look` | `src/command2.c:37` | 연결 | CommandLook; session/look_command.go + world/look.go | `server/internal/session/look_command_test.go`, `server/internal/world/look_test.go` | prefix `봐 동`·last-token `동 봐` peek. find_obj 인벤→ready→방. display_rom 전투 안내. `나`/first_ply는 미완료 |
| 47 | 2 | `보다` | `look` | `src/command2.c:37` | 연결 | CommandLook; session/look_command.go + world/look.go | `server/internal/session/look_command_test.go`, `server/internal/world/look_test.go` | prefix `보다 동`·last-token `북 보다` peek. find_obj 인벤→ready→방. display_rom 전투 안내. `나`/first_ply는 미완료 |
| 48 | 100 | `보아` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 49 | 2 | `조사` | `look` | `src/command2.c:37` | 연결 | CommandLook; session/look_command.go + world/look.go | `server/internal/session/look_command_test.go`, `server/internal/world/look_test.go` | prefix `조사 동굴 2`·last-token `동굴 2 조사` peek. find_obj 인벤→ready→방. display_rom 전투 안내. `나`/first_ply는 미완료 |
| 50 | 3 | `끝` | `quit` | `src/command5.c:1079` | 부분 | CommandQuit; session quit/save 경로 | `server/internal/session/quit_command_test.go` | 종료 저장·세션 정리의 운영 인수는 미완료 |
| 51 | 4 | `말` | `say` | `src/command2.c:642` | 연결 | CommandSay; world/say.go | `server/internal/world/say_test.go` | prefix/occurrence 확장 없음 |
| 52 | 4 | `"` | `say` | `src/command2.c:642` | 연결 | CommandSay; world/say.go | `server/internal/world/say_test.go` | prefix/occurrence 확장 없음 |
| 53 | 4 | `'` | `say` | `src/command2.c:642` | 연결 | CommandSay; world/say.go | `server/internal/world/say_test.go` | prefix/occurrence 확장 없음 |
| 54 | 5 | `주워` | `get` | `src/command2.c:697` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 55 | 5 | `주` | `get` | `src/command2.c:697` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 56 | 5 | `가져` | `get` | `src/command2.c:697` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 57 | 5 | `꺼내` | `get` | `src/command2.c:697` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 58 | 6 | `소지품` | `inventory` | `src/command2.c:1196` | 연결 | CommandItems; session items/equipment | `server/internal/session/items_command_test.go` | 전체 C 출력 parity 미완료 |
| 59 | 7 | `버려` | `drop` | `src/command2.c:1263` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 60 | 7 | `넣어` | `drop` | `src/command2.c:1263` | 연결 | CommandItemMutation; session item mutation + world item graph | `server/internal/session/item_mutation_command_test.go` | 중첩/OINVIS/장비 일부 fail-closed |
| 61 | 8 | `누구` | `who` | `src/command5.c:367` | 부분 | CommandSocial; session social who | `server/internal/session/social_command_test.go` | 누구 출력 전수 미완료 |
| 62 | 9 | `입어` | `wear` | `src/command3.c:21` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | wear restriction 전수 미완료 |
| 63 | 10 | `벗어` | `remove_obj` | `src/command3.c:487` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | ready↔inventory 정규화 일부 |
| 64 | 11 | `장비` | `equipment` | `src/command3.c:606` | 연결 | CommandItems; session items/equipment | `server/internal/session/equipment_command_test.go` | 출력 parity 미완료 |
| 65 | 11 | `장` | `equipment` | `src/command3.c:606` | 연결 | CommandItems; session items/equipment | `server/internal/session/equipment_command_test.go` | 출력 parity 미완료 |
| 66 | 12 | `쥐어` | `hold` | `src/command3.c:860` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | shots/flags 전수 미완료 |
| 67 | 12 | `잡아` | `hold` | `src/command3.c:860` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | shots/flags 전수 미완료 |
| 68 | 13 | `무장` | `ready` | `src/command3.c:691` | 연결 | CommandEquipment; world equipment | `server/internal/session/equipment_command_test.go` | 무장 슬롯 전수 미완료 |
| 69 | 14 | `도움말` | `help` | `src/command4.c:133` | 부분 | CommandHelp; session help/document | `server/internal/session/help_command_test.go` | 정적 docs 연결 일부, 전체 help corpus 미완료 |
| 70 | 14 | `?` | `help` | `src/command4.c:133` | 부분 | CommandHelp; session help/document | `server/internal/session/help_command_test.go` | 정적 docs 연결 일부, 전체 help corpus 미완료 |
| 71 | 15 | `건강` | `health` | `src/command4.c:45` | 부분 | CommandStatus; session status/health | `server/internal/session/status_command_test.go` | 전체 점수 필드 parity 미완료 |
| 72 | 15 | `점수` | `health` | `src/command4.c:45` | 부분 | CommandStatus; session status/health | `server/internal/session/status_command_test.go` | 전체 점수 필드 parity 미완료 |
| 73 | 16 | `정보` | `info` | `src/command4.c:249` | 부분 | CommandInfo; session info | `server/internal/session/info_command_test.go` | 후속 페이지/주문 목록 일부, 전체 info_2 parity 미완료 |
| 74 | 17 | `얘기` | `sendman` | `src/command4.c:408` | 연결 | CommandDirectMessage; world/direct_message.go | `server/internal/session/direct_message_command_test.go`, `server/internal/world/direct_message_test.go` | 오프라인/길이 경계 일부 |
| 75 | 17 | `이야기` | `sendman` | `src/command4.c:408` | 연결 | CommandDirectMessage; world/direct_message.go | `server/internal/session/direct_message_command_test.go`, `server/internal/world/direct_message_test.go` | 오프라인/길이 경계 일부 |
| 76 | 18 | `따라` | `follow` | `src/command4.c:641` | 부분 | CommandFollow; session follow | `server/internal/session/follow_command_test.go` | 추종자 graph 일부, C follow 전수 미완료 |
| 77 | 19 | `내보내` | `lose` | `src/command4.c:743` | 부분 | CommandFollow; session follow | `server/internal/session/follow_command_test.go` | 내보내 전수 미완료 |
| 78 | 20 | `그룹` | `group` | `src/command4.c:832` | 부분 | CommandSocial; session social/group | `server/internal/session/social_command_test.go` | 그룹 구성 전수 미완료 |
| 79 | 20 | `무리` | `group` | `src/command4.c:832` | 부분 | CommandSocial; session social/group | `server/internal/session/social_command_test.go` | 그룹 구성 전수 미완료 |
| 80 | 21 | `추적` | `track` | `src/command4.c:896` | 연결 | CommandTrack; world/track.go | `server/internal/session/track_command_test.go`, `server/internal/world/track_test.go` | RNG/스킬 전수 미완료 |
| 81 | 22 | `엿봐` | `peek` | `src/command4.c:955` | 연결 | CommandPeek; world/peek.go | `server/internal/session/peek_command_test.go`, `server/internal/world/peek_test.go` | OINVIS 전수 미완료 |
| 82 | 23 | `공격` | `attack` | `src/command5.c:36` | 연결 | CommandAttack; world/attack.go | `server/internal/session/attack_command_test.go`, `server/internal/world/attack_test.go` | 전체 combat parity 미완료 |
| 83 | 23 | `공` | `attack` | `src/command5.c:36` | 연결 | CommandAttack; world/attack.go | `server/internal/session/attack_command_test.go`, `server/internal/world/attack_test.go` | 전체 combat parity 미완료 |
| 84 | 23 | `쳐` | `attack` | `src/command5.c:36` | 연결 | CommandAttack; world/attack.go | `server/internal/session/attack_command_test.go`, `server/internal/world/attack_test.go` | 전체 combat parity 미완료 |
| 85 | 23 | `때려` | `attack` | `src/command5.c:36` | 연결 | CommandAttack; world/attack.go | `server/internal/session/attack_command_test.go`, `server/internal/world/attack_test.go` | 전체 combat parity 미완료 |
| 86 | 24 | `검색` | `search` | `src/command5.c:549` | 연결 | CommandSearch; world/search.go | `server/internal/session/search_command_test.go`, `server/internal/world/search_test.go` | 숨김 객체 전수 미완료 |
| 87 | 24 | `찾아` | `search` | `src/command5.c:549` | 연결 | CommandSearch; world/search.go | `server/internal/session/search_command_test.go`, `server/internal/world/search_test.go` | 숨김 객체 전수 미완료 |
| 88 | 25 | `표현` | `emote` | `src/command11.c:30` | 연결 | CommandEmote; world/emote.go | `server/internal/world/emote_test.go` | NPC 대상·occurrence fail-closed |
| 89 | 26 | `숨겨` | `hide` | `src/command5.c:744` | 연결 | CommandHide; world/hide.go | `server/internal/session/hide_command_test.go`, `server/internal/world/hide_test.go` | 바닥 객체/플레이어 분기 bounded |
| 90 | 26 | `숨어` | `hide` | `src/command5.c:744` | 연결 | CommandHide; world/hide.go | `server/internal/session/hide_command_test.go`, `server/internal/world/hide_test.go` | 바닥 객체/플레이어 분기 bounded |
| 91 | 27 | `설정` | `set` | `src/command5.c:895` | 연결 | CommandSettings; world/settings.go | `server/internal/world/settings_test.go` | flag 전수 미완료 |
| 92 | 28 | `해제` | `clear` | `src/command5.c:983` | 연결 | CommandSettings; world/settings.go | `server/internal/world/settings_test.go` | flag 전수 미완료 |
| 93 | 29 | `외쳐` | `yell` | `src/command6.c:21` | 연결 | CommandYell; world/yell.go | `server/internal/session/yell_command_test.go`, `server/internal/world/yell_test.go` | 권역/방 제한 일부 |
| 94 | 30 | `가` | `go` | `src/command6.c:76` | 부분 | CommandGo; world/go.go + npc_chase.go | `server/internal/session/go_command_test.go`, `server/internal/world/go_test.go`, `server/internal/world/npc_chase_test.go` | command6 MFOLLO chase(threshold 10, no die_perm_crt). 특수 입구 전수 미완료 |
| 95 | 30 | `들어가` | `go` | `src/command6.c:76` | 부분 | CommandGo; world/go.go + npc_chase.go | `server/internal/session/go_command_test.go`, `server/internal/world/go_test.go`, `server/internal/world/npc_chase_test.go` | command6 MFOLLO chase(threshold 10, no die_perm_crt). 특수 입구 전수 미완료 |
| 96 | 31 | `열어` | `openexit` | `src/command6.c:365` | 연결 | CommandDoor; world/doors.go | `server/internal/world/doors_test.go` | 열쇠/함정 전수 미완료 |
| 97 | 32 | `닫아` | `closeexit` | `src/command6.c:419` | 연결 | CommandDoor; world/doors.go | `server/internal/world/doors_test.go` | 열쇠/함정 전수 미완료 |
| 98 | 33 | `풀어` | `unlock` | `src/command6.c:472` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 키 객체 graph 전수 미완료 |
| 99 | 34 | `잠궈` | `lock` | `src/command6.c:553` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 키 객체 graph 전수 미완료 |
| 100 | 35 | `따` | `picklock` | `src/command6.c:641` | 연결 | CommandDoorKey; world/door_keys.go | `server/internal/world/door_keys_test.go` | 스킬/RNG 전수 미완료 |
| 101 | 36 | `훔쳐` | `steal` | `src/command6.c:723` | 연결 | CommandSteal; world/steal.go | `server/internal/session/steal_command_test.go`, `server/internal/world/steal_test.go` | NPC/플레이어 분기 bounded |
| 102 | 37 | `도망` | `flee` | `src/command7.c:19` | 연결 | CommandFlee; world/flee.go | `server/internal/session/flee_command_test.go`, `server/internal/world/flee_test.go` | 전투 중 방향 인자는 fail-closed |
| 103 | 37 | `도` | `flee` | `src/command7.c:19` | 연결 | CommandFlee; world/flee.go | `server/internal/session/flee_command_test.go`, `server/internal/world/flee_test.go` | 전투 중 방향 인자는 fail-closed |
| 104 | 38 | `주문` | `cast` | `src/magic1.c:23` | 부분 | CommandCast; world/cast.go + spell_catalog.go + summon.go | `server/internal/session/cast_command_test.go`, `server/internal/world/cast_test.go`, `server/internal/world/summon_test.go` | self-cast 다수·천리안·소환 연결. 공격/맵/기타 대상 주문·[엔터] continuation 미완료 |
| 105 | 39 | `배워` | `study` | `src/magic1.c:259` | 연결 | CommandStudy; world/study.go | `server/internal/session/study_command_test.go`, `server/internal/world/study_test.go` | 직업/레벨 전수 미완료 |
| 106 | 39 | `연마` | `study` | `src/magic1.c:259` | 연결 | CommandStudy; world/study.go | `server/internal/session/study_command_test.go`, `server/internal/world/study_test.go` | 직업/레벨 전수 미완료 |
| 107 | 40 | `읽어` | `readscroll` | `src/magic1.c:352` | 연결 | CommandReadScroll; world/read_scroll.go | `server/internal/world/read_scroll_test.go` | 게시판 `읽어`와 구분 |
| 108 | 41 | `품목` | `list` | `src/command7.c:139` | 연결 | CommandShopList; session shop | `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_marketplace_test.go` | 상점 storage 전수 미완료 |
| 109 | 42 | `사` | `buy` | `src/command7.c:169` | 연결 | CommandShopPurchase; world shop/merchant | `server/internal/session/shop_purchase_command_test.go`, `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_purchase_name_test.go` | 가격/재고 전수 미완료 |
| 110 | 43 | `팔아` | `sell` | `src/command7.c:227` | 연결 | CommandShopSell; session shop sell | `server/internal/session/shop_marketplace_command_test.go`, `server/internal/world/shop_marketplace_test.go` | 가격/재고 전수 미완료 |
| 111 | 44 | `가치` | `value` | `src/command7.c:294` | 연결 | CommandValue; session value | `server/internal/session/value_command_test.go` | 감정 전수 미완료 |
| 112 | 44 | `가격` | `value` | `src/command7.c:294` | 연결 | CommandValue; session value | `server/internal/session/value_command_test.go` | 감정 전수 미완료 |
| 113 | 45 | `기습` | `backstab` | `src/command7.c:342` | 연결 | CommandBackstab; world/backstab.go | `server/internal/session/backstab_command_test.go`, `server/internal/world/backstab_test.go` | 은신/위치 전수 미완료 |
| 114 | 46 | `수련` | `train` | `src/command7.c:529` | 연결 | CommandTrain; world/training.go | `server/internal/session/training_command_test.go`, `server/internal/world/training_test.go` | stat 한도 전수 미완료 |
| 115 | 47 | `줘` | `give` | `src/command8.c:31` | 연결 | CommandGive; world/give.go | `server/internal/session/give_command_test.go`, `server/internal/world/give_test.go` | NPC GIVE talk와 별개 |
| 116 | 48 | `수리` | `repair` | `src/command8.c:204` | 연결 | CommandRepair; world/repair.go | `server/internal/session/repair_command_test.go`, `server/internal/world/repair_test.go` | 비용/성공률 전수 미완료 |
| 117 | 49 | `시간` | `prt_time` | `src/command8.c:311` | 연결 | CommandTime/CommandRead; session time | `server/internal/session/time_command_test.go` | 게임 달력 전수 미완료 |
| 118 | 50 | `교란` | `circle` | `src/command8.c:342` | 연결 | CommandCircle; world/circle.go | `server/internal/session/circle_command_test.go`, `server/internal/world/circle_test.go` | 전투 위치 전수 미완료 |
| 119 | 51 | `맹공` | `bash` | `src/command8.c:489` | 연결 | CommandBash; world/bash.go | `server/internal/session/bash_command_test.go`, `server/internal/world/bash_test.go` | 문/전투 분기 bounded |
| 120 | 52 | `저장` | `savegame` | `src/command8.c:717` | 부분 | CommandSave; session save | `server/internal/session/save_command_test.go` | 운영 PG 자동저장 인수는 미완료 |
| 121 | 53 | `편지보내기` | `postsend` | `src/post.c:28` | 연결 | CommandMailSend; world/mail_send.go | `server/internal/world/mail_send_test.go` | 우편 파일 이관 전수 미완료 |
| 122 | 55 | `편지삭제` | `postdelete` | `src/post.c:174` | 연결 | CommandMail; world/mail.go | `server/internal/world/mail_test.go` | 삭제 순서 전수 미완료 |
| 123 | 54 | `편지받기` | `postread` | `src/post.c:134` | 연결 | CommandMail; world/mail.go | `server/internal/world/mail_test.go` | 페이지/정렬 전수 미완료 |
| 124 | 56 | `대화` | `talk` | `src/command8.c:817` | 부분 | CommandNPCTalk; world/npc_talk.go | `server/internal/world/npc_talk_test.go` | ATTACK/CAST/GIVE/ACTION 일부. 원본 talk corpus 전수 미완료 |
| 125 | 57 | `그룹말` | `gtalk` | `src/command10.c:244` | 연결 | CommandGroupTalk; world/group_talk.go | `server/internal/world/group_talk_test.go` | 그룹 멤버십 전수 미완료 |
| 126 | 57 | `무리말` | `gtalk` | `src/command10.c:244` | 연결 | CommandGroupTalk; world/group_talk.go | `server/internal/world/group_talk_test.go` | 그룹 멤버십 전수 미완료 |
| 127 | 57 | `=` | `gtalk` | `src/command10.c:244` | 연결 | CommandGroupTalk; world/group_talk.go | `server/internal/world/group_talk_test.go` | 그룹 멤버십 전수 미완료 |
| 128 | 58 | `마셔` | `drink` | `src/magic1.c:496` | 연결 | CommandDrink; world/drink.go | `server/internal/session/drink_command_test.go`, `server/internal/world/drink_test.go` | 물약/음식 효과 전수 미완료 |
| 129 | 58 | `먹어` | `drink` | `src/magic1.c:496` | 연결 | CommandDrink; world/drink.go | `server/internal/session/drink_command_test.go`, `server/internal/world/drink_test.go` | 물약/음식 효과 전수 미완료 |
| 130 | 59 | `잡담` | `broadsend` | `src/command4.c:508` | 연결 | CommandBroadcast; world/broadcast.go | `server/internal/session/broadcast_command_test.go`, `server/internal/world/broadcast_test.go` | 채널/권한 전수 미완료 |
| 131 | 59 | `잡` | `broadsend` | `src/command4.c:508` | 연결 | CommandBroadcast; world/broadcast.go | `server/internal/session/broadcast_command_test.go`, `server/internal/world/broadcast_test.go` | 채널/권한 전수 미완료 |
| 132 | 70 | `환호` | `broadsend2` | `src/command4.c:571` | 연결 | CommandBroadcast; world/broadcast.go | `server/internal/session/broadcast_command_test.go`, `server/internal/world/broadcast_test.go` | 환호 채널 bounded |
| 133 | 60 | `zap` | `zap` | `src/magic1.c:673` | 연결 | CommandZap; session/zap_command.go + world/zap.go + transport route | `server/internal/session/zap_command_test.go`, `server/internal/world/zap_test.go`, `server/internal/transport/world_connector_zap_test.go` | wand selector·vigor bounded, direct/Ready EQUAL·receipt/replay 확인. 전체 spell catalog/effect·PG/E2E 미완료 |
| 134 | 61 | `환영` | `welcome` | `src/command4.c:227` | 부분 | CommandWelcome; session welcome | `server/internal/session/welcome_command_test.go` | 출력/대상 전수 미완료 |
| 135 | 62 | `방혼술` | `turn` | `src/magic3.c:155` | 연결 | CommandTurn; world/turn.go | `server/internal/session/turn_command_test.go`, `server/internal/world/turn_test.go` | 언데드 대상 전수 미완료 |
| 136 | 63 | `보관물` | `bank_inv` | `src/bank.c:114` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 보관물 alias는 Bank 경로. bank_inv 심볼 자체는 Go에 없음 |
| 137 | 63 | `잔액` | `bank` | `src/bank.c:157` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go`, `server/internal/world/bank_test.go` | operator import 있음. 라이브 graph/gold parity 미완료 |
| 138 | 63 | `입금` | `deposit` | `src/bank.c:338` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 라이브 transfer 미완료 |
| 139 | 63 | `출금` | `withdraw` | `src/bank.c:401` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 라이브 transfer 미완료 |
| 140 | 63 | `받아` | `output_bank` | `src/bank.c:253` | 부분 | CommandBank; world/bank.go | `server/internal/session/bank_command_test.go` | 잔액 출력 bounded. output_bank 심볼 없음 |
| 141 | 64 | `활보법` | `haste` | `src/command9.c:87` | 연결 | CommandRangerPray/Haste; world/ranger_pray.go | `server/internal/session/ranger_pray_command_test.go`, `server/internal/world/ranger_pray_test.go` | 활보법 bounded |
| 142 | 65 | `신원법` | `pray` | `src/command9.c:145` | 연결 | CommandRangerPray; world/ranger_pray.go | `server/internal/session/ranger_pray_command_test.go`, `server/internal/world/ranger_pray_test.go` | 신원법 bounded |
| 143 | 66 | `경계` | `prepare` | `src/command9.c:437` | 연결 | CommandPrepare; world/prepare_updmg.go | `server/internal/world/prepare_updmg_test.go` | 경계 bounded |
| 144 | 67 | `사용` | `use` | `src/command9.c:480` | 연결 | CommandUse; world/use.go | `server/internal/session/use_command_test.go`, `server/internal/world/use_test.go` | 객체 효과 전수 미완료 |
| 145 | 68 | `듣기거부` | `ignore` | `src/command9.c:564` | 연결 | CommandIgnore; session ignore | `server/internal/session/ignore_command_test.go` | connection-local, PDMINV 검사 |
| 146 | 69 | `사용자검색` | `whois` | `src/command5.c:503` | 연결 | CommandPlayerLookup; session player lookup | `server/internal/session/player_lookup_command_test.go` | 오프라인/권한 전수 미완료 |
| 147 | 73 | `묘사` | `description` | `src/command12.c:488` | 연결 | CommandDescription; world/description.go | `server/internal/session/description_command_test.go`, `server/internal/world/description_test.go` | UTF-8 길이 전수 미완료 |
| 148 | 71 | `가르쳐` | `teach` | `src/magic1.c:125` | 연결 | CommandTeach; world/teach.go | `server/internal/session/teach_command_test.go`, `server/internal/world/teach_test.go` | 전수 비트 전수 미완료 |
| 149 | 72 | `선전포고` | `call_war` | `src/special1.c:182` | 연결 | CommandFamilyWar; world/family_war.go | `server/internal/session/family_war_command_test.go`, `server/internal/world/family_war_test.go` | 문주 사망 종료·운영 War 영속·보상 미완료 |
| 150 | 74 | `구입` | `purchase` | `src/command10.c:492` | 연결 | CommandShopPurchase/MerchantPurchase; world/merchant_purchase.go | `server/internal/session/merchant_purchase_command_test.go`, `server/internal/world/merchant_purchase_test.go` | 상인 구입 bounded |
| 151 | 75 | `선택` | `selection` | `src/command10.c:597` | 연결 | CommandSelection; world/selection.go | `server/internal/session/selection_command_test.go`, `server/internal/world/selection_test.go` | 상인 선택 bounded |
| 152 | 76 | `교환` | `trade` | `src/command10.c:664` | 연결 | CommandTrade; session/world trade | `server/internal/session/trade_command_test.go` | 교환 전수 미완료 |
| 153 | 77 | `목매달기` | `ply_suicide` | `src/command5.c:653` | 연결 | CommandSuicide; session/suicide_command.go + world/suicide.go + transport route | `server/internal/session/suicide_command_test.go`, `server/internal/world/suicide_test.go`, `server/internal/transport/world_connector_suicide_test.go` | 확인/암호 continuation·atomic death·receipt/replay bounded. 운영 계정/전체 C parity 미완료 |
| 154 | 78 | `암호` | `passwd` | `src/command11.c:83` | 부분 | CommandPassword; session password + storage bcrypt | `server/internal/session/password_command_test.go` | 게임 연결 continuation 일부, 운영 계정 이관 미완료 |
| 155 | 79 | `투표` | `vote` | `src/command11.c:190` | 연결 | CommandVote; world/vote.go | `server/internal/session/vote_command_test.go`, `server/internal/world/vote_test.go` | raw→manifest 있음. 라이브 투표 전수 미완료 |
| 156 | 80 | `사용자정보` | `pfinger` | `src/command11.c:400` | 연결 | CommandPlayerLookup; session player lookup | `server/internal/session/player_lookup_command_test.go` | 사용자정보 bounded |
| 157 | 81 | `귀환` | `return_square` | `src/command1.c:1797` | 연결 | CommandReturnSquare; world/return_square.go | `server/internal/session/return_square_command_test.go`, `server/internal/world/return_square_test.go` | 광장 좌표/비용 전수 미완료 |
| 158 | 81 | `귀` | `return_square` | `src/command1.c:1797` | 연결 | CommandReturnSquare; world/return_square.go | `server/internal/session/return_square_command_test.go`, `server/internal/world/return_square_test.go` | 광장 좌표/비용 전수 미완료 |
| 159 | 82 | `줄임말` | `ply_aliases` | `src/alias.c:260` | 연결 | CommandAlias; world/alias.go | `server/internal/session/alias_command_test.go`, `server/internal/world/alias_test.go` | prefix/suffix 둘 다. 영속 전수 미완료 |
| 160 | 82 | `줄` | `ply_aliases` | `src/alias.c:260` | 연결 | CommandAlias; world/alias.go | `server/internal/session/alias_command_test.go`, `server/internal/world/alias_test.go` | prefix/suffix 둘 다. 영속 전수 미완료 |
| 161 | 83 | `태워` | `burn` | `src/command2.c:1693` | 연결 | CommandBurn; world/burn.go | `server/internal/session/burn_command_test.go`, `server/internal/world/burn_test.go` | 소각 대상 전수 미완료 |
| 162 | 83 | `소각` | `burn` | `src/command2.c:1693` | 연결 | CommandBurn; world/burn.go | `server/internal/session/burn_command_test.go`, `server/internal/world/burn_test.go` | 소각 대상 전수 미완료 |
| 163 | 84 | `칭호` | `set_title` | `src/alias.c:432` | 연결 | CommandTitle; world/title.go | `server/internal/session/title_command_test.go`, `server/internal/world/title_test.go` | 칭호 설정 bounded |
| 164 | 84 | `칭호삭제` | `clear_title` | `src/alias.c:463` | 연결 | CommandTitle; world/title.go | `server/internal/session/title_command_test.go`, `server/internal/world/title_test.go` | 칭호삭제 bounded. clear_title 심볼은 PlanClearTitle |
| 165 | 85 | `제련` | `forge` | `src/command7.c:669` | 부분 | CommandForge; world/forge.go | `server/internal/session/forge_command_test.go`, `server/internal/world/forge_test.go` | 제련 select_arm 1-6 연결. `무기만들기`는 newforge 경로 |
| 166 | 85 | `무기만들기` | `newforge` | `src/command7.c:871` | 부분 | CommandNewForge; world/newforge.go | `server/internal/session/newforge_command_test.go`, `server/internal/world/newforge_test.go` | 무기만들기 first prompt/gate + select_newarm case 2-6(900-904, 에메랄드/티타늄/일루션 forge2, 담금질 shots/sum, 이름 3-20바이트, 예 접두 금화 차감·add_obj_crt). 상점/거래 catalog는 미완료 |
| 167 | 86 | `직업전환` | `change_class` | `src/command7.c:1111` | 연결 | CommandChangeClass; world/change_class.go | `server/internal/session/change_class_command_test.go`, `server/internal/world/change_class_test.go` | confirmation bounded |
| 168 | 87 | `기공집결` | `power` | `src/command9.c:259` | 연결 | CommandPowerAccuracy; world/power_accuracy.go | `server/internal/session/power_accuracy_command_test.go`, `server/internal/world/power_accuracy_test.go` | 기공집결 bounded |
| 169 | 88 | `살기충전` | `accurate` | `src/command9.c:317` | 연결 | CommandPowerAccuracy; world/power_accuracy.go | `server/internal/session/power_accuracy_command_test.go`, `server/internal/world/power_accuracy_test.go` | 살기충전 bounded |
| 170 | 89 | `흡성대법` | `absorb` | `src/magic3.c:283` | 연결 | CommandAbsorb; world/absorb.go | `server/internal/session/absorb_command_test.go`, `server/internal/world/absorb_test.go` | 흡성대법 bounded |
| 171 | 90 | `참선` | `meditate` | `src/command9.c:378` | 연결 | CommandMeditate; world/meditate.go | `server/internal/session/meditate_command_test.go`, `server/internal/world/meditate_test.go` | 참선 bounded |
| 172 | 91 | `혈도봉쇄` | `magic_stop` | `src/command7.c:1203` | 연결 | CommandMagicStop; world/magic_stop.go | `server/internal/session/magic_stop_command_test.go`, `server/internal/world/magic_stop_test.go` | 혈도봉쇄 bounded |
| 173 | 92 | `써` | `writeboard` | `src/board.c:176` | 연결 | CommandBoardWrite; world/board_write.go | `server/internal/world/board_write_test.go` | 써 bounded. writeboard 심볼명 없음 |
| 174 | 93 | `글삭제` | `del_board` | `src/board.c:390` | 연결 | CommandBoard; world/board.go | `server/internal/session/board_command_test.go`, `server/internal/world/board_test.go` | 글삭제 bounded |
| 175 | 94 | `게시판` | `look_board` | `src/board.c:66` | 연결 | CommandBoard; world/board.go | `server/internal/session/board_command_test.go` | 게시판 조회. look_board 심볼명 없음 |
| 176 | 95 | `명명` | `chg_name` | `src/command8.c:1040` | 연결 | CommandItemRename; world/item_rename.go | `server/internal/session/item_rename_command_test.go`, `server/internal/world/item_rename_test.go` | 명명 suffix bounded |
| 177 | 96 | `감정` | `info_obj` | `src/command3.c:958` | 연결 | CommandObjectAppraisal; session object_appraisal | `server/internal/session/object_appraisal_command_test.go` | 감정 bounded |
| 178 | 96 | `비교` | `obj_compare` | `src/command12.c:21` | 연결 | CommandCompare; session object_compare | `server/internal/session/object_compare_command_test.go` | 비교 bounded |
| 179 | 97 | `차기` | `kick` | `src/command8.c:1176` | 연결 | CommandKick; world/kick.go | `server/internal/session/kick_command_test.go`, `server/internal/world/kick_test.go` | 차기 bounded |
| 180 | 98 | `독살포` | `poison_mon` | `src/command7.c:1308` | 연결 | CommandPoison; world/poison.go | `server/internal/session/poison_command_test.go`, `server/internal/world/poison_test.go` | 독살포 bounded |
| 181 | 99 | `잠력격발` | `up_dmg` | `src/command9.c:200` | 연결 | CommandUpDmg; world/prepare_updmg.go | `server/internal/world/prepare_updmg_test.go` | 잠력격발 bounded |
| 182 | 100 | `감정표현` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 183 | 100 | `노려봐` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 184 | 100 | `끄덕` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 185 | 100 | `응` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 186 | 100 | `아니` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 187 | 100 | `감` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 188 | 100 | `감사` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 189 | 100 | `미소` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 190 | 100 | `청혼` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 191 | 100 | `떨어` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 192 | 100 | `해` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 193 | 100 | `하품` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 194 | 100 | `웃어` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 195 | 100 | `미안` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 196 | 100 | `악수` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 197 | 100 | `하이파이브` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 198 | 100 | `박수` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 199 | 100 | `흡연` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 200 | 100 | `담배` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 201 | 100 | `절` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 202 | 100 | `찔러` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 203 | 100 | `춤` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 204 | 100 | `노래` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 205 | 100 | `울어` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 206 | 100 | `달래` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 207 | 100 | `당황` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 208 | 100 | `생각` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 209 | 100 | `부끄러` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 210 | 100 | `놀려` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 211 | 100 | `설레` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 212 | 100 | `잘가` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 213 | 100 | `바이` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 214 | 100 | `안녕` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 215 | 100 | `뽀뽀` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 216 | 100 | `윙크` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 217 | 100 | `구걸` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 218 | 100 | `구박` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 219 | 100 | `안아` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 220 | 100 | `껴안아` | `action` | `src/action.c:43` | 연결 | CommandEmote/Express/LookAtTarget; world/emote.go + express + look_at_target | `server/internal/world/emote_test.go`, `server/internal/session/look_at_target_command_test.go` | 감정표현 alias 집합 bounded. NPC 대상 fail-closed |
| 221 | 149 | `향상` | `buy_states` | `src/command11.c:954` | 연결 | CommandBuyStates; session/buy_states_command.go + world/buy_states.go + transport route | `server/internal/session/buy_states_command_test.go`, `server/internal/world/buy_states_test.go`, `server/internal/transport/world_connector_buy_states_test.go` | 향상 deterministic apply/receipt/replay bounded. live RNG·전체 G3 미완료 |
| 222 | 150 | `결혼` | `marriage` | `src/command11.c:1124` | 연결 | CommandMarriage; world/marriage.go | `server/internal/session/marriage_command_test.go`, `server/internal/world/marriage_test.go` | 신청/수락 bounded |
| 223 | 150 | `사랑말` | `m_send` | `src/command11.c:1247` | 연결 | CommandMarriageSend; world/marriage_followup.go | `server/internal/session/marriage_followup_command_test.go` | 사랑말 bounded |
| 224 | 150 | `이혼` | `divorce` | `src/command11.c:1291` | 연결 | CommandDivorce; world/marriage_followup.go | `server/internal/session/marriage_followup_command_test.go`, `server/internal/world/marriage_followup_test.go` | 이혼 bounded |
| 225 | 153 | `기억` | `moon_set` | `src/command8.c:1124` | 연결 | CommandMoonSet; world/moon_set.go | `server/internal/session/moon_set_command_test.go`, `server/internal/world/moon_set_test.go` | EQUAL prefix/OINVIS·장비/중첩 미완료 |
| 226 | 151 | `메모` | `memo` | `src/command12.c:85` | 연결 | CommandMemo; world/memo.go | `server/internal/session/memo_command_test.go`, `server/internal/world/memo_test.go` | 메모 bounded |
| 227 | 152 | `초대` | `invite` | `src/command12.c:362` | 연결 | CommandPropertyInvite; world/property_invite.go | `server/internal/world/property_invite_test.go` | 초대 bounded |
| 228 | 154 | `상태` | `enemy_status` | `src/command8.c:1358` | 연결 | CommandEnemyStatus; session enemy status | `server/internal/session/enemy_status_command_test.go` | 상태 bounded |
| 229 | 148 | `패거리누구` | `family_who` | `src/command11.c:785` | 연결 | CommandFamilyWho; session family who | `server/internal/session/family_status_command_test.go` | 온라인 목록 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| 230 | 148 | `패거리공지` | `family_news` | `src/post.c:290` | 연결 | CommandFamilyNews; world/family_news.go | `server/internal/session/family_news_command_test.go`, `server/internal/world/family_news_test.go` | view_file 페이지/운영 import 미완료 |
| 231 | 148 | `패거리가입` | `family` | `src/command11.c:506` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go`, `server/internal/session/family_mutation_command_test.go` | 가입 신청. family_gold ledger 없으면 fail-closed |
| 232 | 148 | `가입허가` | `boss_family` | `src/command11.c:597` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go`, `server/internal/session/family_mutation_command_test.go` | 가입허가 bounded |
| 233 | 148 | `패거리탈퇴` | `out_family` | `src/command11.c:662` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go` | 패거리탈퇴. out_family 심볼명 없음(PlanFamilyLeave) |
| 234 | 148 | `패거리추방` | `fm_out` | `src/command12.c:155` | 연결 | CommandFamilyMutation; world/family_mutation.go | `server/internal/world/family_mutation_test.go` | 패거리추방 bounded |
| 235 | 148 | `패거리원` | `family_member` | `src/command12.c:326` | 연결 | CommandFamilyMember; session family member | `server/internal/session/family_status_command_test.go` | 패거리원 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| 236 | 148 | `패거리말` | `family_talk` | `src/command11.c:741` | 연결 | CommandFamilyTalk; world/family_talk.go | `server/internal/world/family_talk_test.go` | 패거리말/] bounded |
| 237 | 148 | `]` | `family_talk` | `src/command11.c:741` | 연결 | CommandFamilyTalk; world/family_talk.go | `server/internal/world/family_talk_test.go` | 패거리말/] bounded |
| 238 | 148 | `모든패거리` | `list_family` | `src/command11.c:859` | 연결 | CommandFamilyList; session family list | `server/internal/session/family_status_command_test.go` | 모든패거리 parser/receipt/replay bounded; catalog 이관·mutation 미완료 |
| 239 | 153 | `대답` | `resend` | `src/command12.c:525` | 연결 | CommandReply; session reply | `server/internal/session/reply_command_test.go` | 대답/`/` bounded |
| 240 | 153 | `/` | `resend` | `src/command12.c:525` | 연결 | CommandReply; session reply | `server/internal/session/reply_command_test.go` | 대답/`/` bounded |
| 241 | 101 | `*teleport` | `dm_teleport` | `src/dm1.c:28` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 242 | 101 | `*순간이동` | `dm_teleport` | `src/dm1.c:28` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 243 | 102 | `*방번호` | `dm_rmstat` | `src/dm1.c:404` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 244 | 102 | `*rm` | `dm_rmstat` | `src/dm1.c:404` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 245 | 102 | `*방` | `dm_rmstat` | `src/dm1.c:404` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 246 | 103 | `*reload` | `dm_reload_rom` | `src/dm1.c:445` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 247 | 103 | `*로드` | `dm_reload_rom` | `src/dm1.c:445` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 248 | 104 | `*save` | `dm_resave` | `src/dm1.c:466` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 249 | 104 | `*세이브` | `dm_resave` | `src/dm1.c:466` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 250 | 105 | `*create` | `dm_create_obj` | `src/dm1.c:488` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 251 | 105 | `*뭐든지다만들어` | `dm_create_obj` | `src/dm1.c:488` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 252 | 105 | `*c` | `dm_create_obj` | `src/dm1.c:488` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 253 | 106 | `*perm` | `dm_perm` | `src/dm1.c:610` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 254 | 106 | `*영원` | `dm_perm` | `src/dm1.c:610` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 255 | 107 | `*invis` | `dm_invis` | `src/dm1.c:639` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 256 | 107 | `*바람처럼사라져` | `dm_invis` | `src/dm1.c:639` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 257 | 107 | `*i` | `dm_invis` | `src/dm1.c:639` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 258 | 108 | `*s` | `dm_send` | `src/dm1.c:134` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 259 | 108 | `*send` | `dm_send` | `src/dm1.c:134` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 260 | 108 | `*공지` | `dm_send` | `src/dm1.c:134` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 261 | 109 | `*purge` | `dm_purge` | `src/dm1.c:179` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 262 | 109 | `*청소` | `dm_purge` | `src/dm1.c:179` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 263 | 109 | `*청` | `dm_purge` | `src/dm1.c:179` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 264 | 110 | `*ac` | `dm_ac` | `src/dm1.c:668` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 265 | 110 | `*방어력` | `dm_ac` | `src/dm1.c:668` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 266 | 111 | `*users` | `dm_users` | `src/dm1.c:234` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 267 | 111 | `*누` | `dm_users` | `src/dm1.c:234` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 268 | 111 | `*누구` | `dm_users` | `src/dm1.c:234` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 269 | 112 | `*echo` | `dm_echo` | `src/dm1.c:311` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 270 | 112 | `*말` | `dm_echo` | `src/dm1.c:311` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 271 | 113 | `*flushrooms` | `dm_flushsave` | `src/dm1.c:353` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 272 | 113 | `*모든저장` | `dm_flushsave` | `src/dm1.c:353` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 273 | 114 | `*shutdown` | `dm_shutdown` | `src/dm1.c:381` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 274 | 114 | `*종료` | `dm_shutdown` | `src/dm1.c:381` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 275 | 115 | `*f` | `dm_force` | `src/dm1.c:704` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 276 | 115 | `*force` | `dm_force` | `src/dm1.c:704` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 277 | 115 | `*뭐든지다시켜` | `dm_force` | `src/dm1.c:704` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 278 | 116 | `*flushcrtobj` | `dm_flush_crtobj` | `src/dm1.c:423` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 279 | 116 | `*재설정` | `dm_flush_crtobj` | `src/dm1.c:423` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 280 | 116 | `*재` | `dm_flush_crtobj` | `src/dm1.c:423` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 281 | 117 | `*monster` | `dm_create_crt` | `src/dm1.c:515` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 282 | 117 | `*괴물` | `dm_create_crt` | `src/dm1.c:515` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 283 | 117 | `*괴` | `dm_create_crt` | `src/dm1.c:515` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 284 | 118 | `*status` | `dm_stat` | `src/dm2.c:24` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 285 | 118 | `*상태` | `dm_stat` | `src/dm2.c:24` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 286 | 119 | `*add` | `dm_add_rom` | `src/dm2.c:580` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 287 | 119 | `*방제작` | `dm_add_rom` | `src/dm2.c:580` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 288 | 120 | `*뭐든지다해` | `dm_set` | `src/dm3.c:20` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 289 | 121 | `*log` | `dm_log` | `src/dm3.c:694` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 290 | 121 | `*접속` | `dm_log` | `src/dm3.c:694` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 291 | 122 | `*spy` | `dm_spy` | `src/dm2.c:632` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 292 | 122 | `*뭐든지다엿봐` | `dm_spy` | `src/dm2.c:632` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 293 | 123 | `*lock` | `dm_loadlockout` | `src/dm3.c:729` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 294 | 123 | `*제한` | `dm_loadlockout` | `src/dm3.c:729` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 295 | 124 | `*finger` | `dm_finger` | `src/dm3.c:751` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 296 | 124 | `*핑거` | `dm_finger` | `src/dm3.c:751` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 297 | 125 | `*list` | `dm_list` | `src/dm3.c:815` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 298 | 125 | `*누구든지다봐` | `dm_list` | `src/dm3.c:815` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 299 | 126 | `*info` | `dm_info` | `src/dm3.c:852` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 300 | 126 | `*정보` | `dm_info` | `src/dm3.c:852` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 301 | 127 | `*parameter` | `dm_param` | `src/dm4.c:19` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 302 | 127 | `*수치` | `dm_param` | `src/dm4.c:19` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 303 | 128 | `*silence` | `dm_silence` | `src/dm4.c:72` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 304 | 128 | `*벙어리` | `dm_silence` | `src/dm4.c:72` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 305 | 129 | `*broad` | `dm_broadecho` | `src/dm4.c:127` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 306 | 129 | `*방송` | `dm_broadecho` | `src/dm4.c:127` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 307 | 130 | `*replace` | `dm_replace` | `src/dm5.c:26` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 308 | 130 | `*교체` | `dm_replace` | `src/dm5.c:26` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 309 | 131 | `*name` | `dm_nameroom` | `src/dm5.c:356` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 310 | 131 | `*방이름` | `dm_nameroom` | `src/dm5.c:356` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 311 | 132 | `*append` | `dm_append` | `src/dm5.c:401` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 312 | 132 | `*추가` | `dm_append` | `src/dm5.c:401` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 313 | 133 | `*prepend` | `dm_prepend` | `src/dm5.c:512` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 314 | 133 | `*서언` | `dm_prepend` | `src/dm5.c:512` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 315 | 134 | `*gcast` | `dm_cast` | `src/dm4.c:176` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 316 | 134 | `*전주문` | `dm_cast` | `src/dm4.c:176` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 317 | 135 | `*group` | `dm_group` | `src/dm4.c:419` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 318 | 135 | `*그룹` | `dm_group` | `src/dm4.c:419` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 319 | 136 | `*notepad` | `notepad` | `src/post.c:201` | 부분 | CommandNotepad; world/session/transport notepad | `server/internal/world/notepad_test.go`, `server/internal/session/notepad_command_test.go`, `server/internal/transport/world_connector_notepad_test.go` | bounded 연결. DM_pad import·전체 C 출력 미완료 |
| 320 | 136 | `*메모` | `notepad` | `src/post.c:201` | 부분 | CommandNotepad; world/session/transport notepad | `server/internal/world/notepad_test.go`, `server/internal/session/notepad_command_test.go`, `server/internal/transport/world_connector_notepad_test.go` | bounded 연결. DM_pad import·전체 C 출력 미완료 |
| 321 | 137 | `*delete` | `dm_delete` | `src/dm5.c:104` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 322 | 137 | `*지우기` | `dm_delete` | `src/dm5.c:104` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 323 | 138 | `*oname` | `dm_obj_name` | `src/dm4.c:546` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 324 | 138 | `*뭐든지다바꿔` | `dm_obj_name` | `src/dm4.c:546` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 325 | 139 | `*cname` | `dm_crt_name` | `src/dm4.c:683` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 326 | 139 | `*괴물이름` | `dm_crt_name` | `src/dm4.c:683` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 327 | 140 | `*active` | `list_act` | `src/update.c:1010` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | *active. Go 없음 |
| 328 | 140 | `*활성` | `list_act` | `src/update.c:1010` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | *active. Go 없음 |
| 329 | 141 | `*dust` | `dm_dust` | `src/dm6.c:22` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 330 | 141 | `*나도가끔화낸다` | `dm_dust` | `src/dm6.c:22` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 331 | 142 | `*cfollow` | `dm_follow` | `src/dm6.c:86` | 연결 | CommandDMFollow; world/dm_follow.go | `server/internal/world/dm_follow_test.go`, `server/internal/session/dm_follow_command_test.go`, `server/internal/world/logout_test.go` | *cfollow. 로그아웃 MDMFOL 정리 bounded |
| 332 | 142 | `*따르기` | `dm_follow` | `src/dm6.c:86` | 연결 | CommandDMFollow; world/dm_follow.go | `server/internal/world/dm_follow_test.go`, `server/internal/session/dm_follow_command_test.go`, `server/internal/world/logout_test.go` | *따르기. 로그아웃 MDMFOL 정리 bounded |
| 333 | 148 | `*떨어져라` | `dm_moonstone` | `src/dm5.c:607` | 연결 | CommandDMFamily; world/dm_family.go | `server/internal/world/dm_family_test.go`, `server/internal/session/dm_family_command_test.go` | *떨어져라. RMAX 전수 load_rom 미완료 |
| 334 | 148 | `*침공` | `dm_monster` | `src/dm5.c:632` | 연결 | CommandDMFamily; world/dm_family.go | `server/internal/world/dm_family_test.go`, `server/internal/session/dm_family_command_test.go` | *침공. 원본 몬스터 템플릿 전수 미완료 |
| 335 | 143 | `*dmhelp` | `dm_help` | `src/dm5.c:660` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 336 | 143 | `*도움말` | `dm_help` | `src/dm5.c:660` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 337 | 144 | `*attack` | `dm_attack` | `src/dm6.c:163` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 338 | 144 | `*공격` | `dm_attack` | `src/dm6.c:163` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |
| 339 | 145 | `*enemy` | `list_enm` | `src/dm6.c:226` | 연결 | CommandDMEnemy; session/dm_enemy_command.go + world/dm_enemy.go + transport route | `server/internal/session/dm_enemy_command_test.go`, `server/internal/world/dm_enemy_test.go`, `server/internal/transport/world_connector_dm_enemy_test.go` | same-room NPC enemy list·canonical occurrence/order·receipt/replay bounded. NPC producers/tick/AI·공격·추종·리젠·PG/full G3 미완료 |
| 340 | 145 | `*적` | `list_enm` | `src/dm6.c:226` | 연결 | CommandDMEnemy; session/dm_enemy_command.go + world/dm_enemy.go + transport route | `server/internal/session/dm_enemy_command_test.go`, `server/internal/world/dm_enemy_test.go`, `server/internal/transport/world_connector_dm_enemy_test.go` | same-room NPC enemy list·canonical occurrence/order·receipt/replay bounded. NPC producers/tick/AI·공격·추종·리젠·PG/full G3 미완료 |
| 341 | 146 | `*charm` | `list_charm` | `src/dm6.c:268` | 연결 | CommandDMCharm; session/dm_charm_command.go + world/dm_charm.go + transport route | `server/internal/session/dm_charm_command_test.go`, `server/internal/world/dm_charm_test.go`, `server/internal/transport/world_connector_dm_charm_test.go` | global online player target·CharmRefs canonical order·receipt/replay bounded. Charm producer/spell mutation·expiry·NPC AI/tick·PG/full G3 미완료 |
| 342 | 146 | `*최면` | `list_charm` | `src/dm6.c:268` | 연결 | CommandDMCharm; session/dm_charm_command.go + world/dm_charm.go + transport route | `server/internal/session/dm_charm_command_test.go`, `server/internal/world/dm_charm_test.go`, `server/internal/transport/world_connector_dm_charm_test.go` | global online player target·CharmRefs canonical order·receipt/replay bounded. Charm producer/spell mutation·expiry·NPC AI/tick·PG/full G3 미완료 |
| 343 | 147 | `*사용자저장` | `dm_save_all_ply` | `src/dm1.c:13` | 미연결 | —; — | CMD-GAP (직접 handler 테스트 파일 없음) | 관리자 명령. Go parser/reducer 없음 |

### 이 재집계가 대체하는 G0 서술

- 2026-09-08의 “모든 명령 행 Go 구현=미구현”은 당시 조사 값이다. 2026-09-14부터는 위 343/174 표가 명령 행 상태의 권위다.
- 개별 행의 **게임 전체 인수**는 첫 C fixture와 실패→통과 Go 테스트 및 승격 cadence 증거가 있을 때만 닫는다.
- 주석 명령 유지 여부는 아직 사용자 승인이 없어 제외 처리하지 않는다.

## 등록 명령 ledger

아래 표는 `src/global.c`에서 주석을 제거하고 추출한 **활성 command row 전체**다.
동일 `cmdno`에 여러 handler가 있는 경우(예: 은행, 가족, DM 148)는 C의 실제
dispatch 후보를 모두 적었다. 모든 행에 공통으로 적용되는 G0 상태는 다음과 같다.

| 필드 | 현재 값 |
| --- | --- |
| 입력 fixture | 미정. 최소 exact/약어/숫자/한글/권한/오류 fixture 필요 |
| 예상 출력·상태 | 미정. C 실행 transcript와 before/after 상태 캡처 필요 |
| Go 구현 | 행마다 다름. 2026-09-14 표의 `연결`/`부분`/`미연결`이 권위다. `연결`≠전체 인수 |
| 테스트 명령·결과 | 행마다 다름. 직접 테스트 파일이 없으면 `CMD-GAP`. 이번 레인은 원장 재집계만 실행 |
| 이관 필요 | 필요. C handler와 파일/포인터/시간/RNG 의존성 분해 필요 |
| 인수 상태 | 미착수 |

### 일반 사용자 명령 (cmdno 1–100)

| ID | C handler(s) | source 등록 alias (중복 포함) |
| ---: | --- | --- |
| 1 | `move` | 나가는길, 광장, 향로, 수련장, 현감에게, 면회허가, 반하도인, 월광반, 반야바라밀, 8, 북, ㅂ, �ぃ�, 2, 남, ㄴ, �ⅲ�, 6, 동, ㄷ, �┌�, 4, 서, ㅅ, �В�, 북동, ㅂㄷ, 북서, 북동, 남서, ㅂㅅ, 남동, ㄴㄷ, 남서, ㄴㅅ, 9, 위, ㅇ, ��＂, 3, 밑, ㅁ, �ð�, 밖, 나가 |
| 2 | `look` | 봐, 보다, 조사 |
| 3 | `quit` | 끝 |
| 4 | `say` | 말, ", ' |
| 5 | `get` | 주워, 주, 가져, 꺼내 |
| 6 | `inventory` | 소지품 |
| 7 | `drop` | 버려, 넣어 |
| 8 | `who` | 누구 |
| 9 | `wear` | 입어 |
| 10 | `remove_obj` | 벗어 |
| 11 | `equipment` | 장비, 장 |
| 12 | `hold` | 쥐어, 잡아 |
| 13 | `ready` | 무장 |
| 14 | `help` | 도움말, ? |
| 15 | `health` | 건강, 점수 |
| 16 | `info` | 정보 |
| 17 | `sendman` | 얘기, 이야기 |
| 18 | `follow` | 따라 |
| 19 | `lose` | 내보내 |
| 20 | `group` | 그룹, 무리 |
| 21 | `track` | 추적 |
| 22 | `peek` | 엿봐 |
| 23 | `attack` | 공격, 공, 쳐, 때려 |
| 24 | `search` | 검색, 찾아 |
| 25 | `emote` | 표현 |
| 26 | `hide` | 숨겨, 숨어 |
| 27 | `set` | 설정 |
| 28 | `clear` | 해제 |
| 29 | `yell` | 외쳐 |
| 30 | `go` | 가, 들어가 |
| 31 | `openexit` | 열어 |
| 32 | `closeexit` | 닫아 |
| 33 | `unlock` | 풀어 |
| 34 | `lock` | 잠궈 |
| 35 | `picklock` | 따 |
| 36 | `steal` | 훔쳐 |
| 37 | `flee` | 도망, 도 |
| 38 | `cast` | 주문 |
| 39 | `study` | 배워, 연마 |
| 40 | `readscroll` | 읽어 |
| 41 | `list` | 품목 |
| 42 | `buy` | 사 |
| 43 | `sell` | 팔아 |
| 44 | `value` | 가치, 가격 |
| 45 | `backstab` | 기습 |
| 46 | `train` | 수련 |
| 47 | `give` | 줘 |
| 48 | `repair` | 수리 |
| 49 | `prt_time` | 시간 |
| 50 | `circle` | 교란 |
| 51 | `bash` | 맹공 |
| 52 | `savegame` | 저장 |
| 53 | `postsend` | 편지보내기 |
| 54 | `postread` | 편지받기 |
| 55 | `postdelete` | 편지삭제 |
| 56 | `talk` | 대화 |
| 57 | `gtalk` | 그룹말, 무리말, = |
| 58 | `drink` | 마셔, 먹어 |
| 59 | `broadsend` | 잡담, 잡 |
| 60 | `zap` | zap |
| 61 | `welcome` | 환영 |
| 62 | `turn` | 방혼술 |
| 63 | `bank`, `bank_inv`, `deposit`, `output_bank`, `withdraw` | 보관물, 잔액, 입금, 출금, 받아 |
| 64 | `haste` | 활보법 |
| 65 | `pray` | 신원법 |
| 66 | `prepare` | 경계 |
| 67 | `use` | 사용 |
| 68 | `ignore` | 듣기거부 |
| 69 | `whois` | 사용자검색 |
| 70 | `broadsend2` | 환호 |
| 71 | `teach` | 가르쳐 |
| 72 | `call_war` | 선전포고 |
| 73 | `description` | 묘사 |
| 74 | `purchase` | 구입 |
| 75 | `selection` | 선택 |
| 76 | `trade` | 교환 |
| 77 | `ply_suicide` | 목매달기 |
| 78 | `passwd` | 암호 |
| 79 | `vote` | 투표 |
| 80 | `pfinger` | 사용자정보 |
| 81 | `return_square` | 귀환, 귀 |
| 82 | `ply_aliases` | 줄임말, 줄 |
| 83 | `burn` | 태워, 소각 |
| 84 | `set_title`, `clear_title` | 칭호, 칭호삭제 |
| 85 | `forge`, `newforge` | 제련, 무기만들기 |
| 86 | `change_class` | 직업전환 |
| 87 | `power` | 기공집결 |
| 88 | `accurate` | 살기충전 |
| 89 | `absorb` | 흡성대법 |
| 90 | `meditate` | 참선 |
| 91 | `magic_stop` | 혈도봉쇄 |
| 92 | `writeboard` | 써 |
| 93 | `del_board` | 글삭제 |
| 94 | `look_board` | 게시판 |
| 95 | `chg_name` | 명명 |
| 96 | `info_obj`, `obj_compare` | 감정, 비교 |
| 97 | `kick` | 차기 |
| 98 | `poison_mon` | 독살포 |
| 99 | `up_dmg` | 잠력격발 |
| 100 | `action` | 보아, 감정표현, 노려봐, 끄덕, 응, 아니, 감, 감사, 미소, 청혼, 떨어, 해, 하품, 웃어, 미안, 악수, 하이파이브, 흡연, 담배, 절, 찔러, 춤, 노래, 울어, 달래, 당황, 생각, 부끄러, 놀려, 설레, 잘가, 바이, 안녕, 뽀뽀, 윙크, 구걸, 구박, 안아, 껴안아 |

### 관리자/특수 명령 (cmdno 101–154)

| ID | C handler(s) | source 등록 alias (중복 포함) |
| ---: | --- | --- |
| 101 | `dm_teleport` | *teleport, *순간이동 |
| 102 | `dm_rmstat` | *방번호, *rm, *방 |
| 103 | `dm_reload_rom` | *reload, *로드 |
| 104 | `dm_resave` | *save, *세이브 |
| 105 | `dm_create_obj` | *create, *뭐든지다만들어, *c |
| 106 | `dm_perm` | *perm, *영원 |
| 107 | `dm_invis` | *invis, *바람처럼사라져, *i |
| 108 | `dm_send` | *s, *send, *공지 |
| 109 | `dm_purge` | *purge, *청소, *청 |
| 110 | `dm_ac` | *ac, *방어력 |
| 111 | `dm_users` | *users, *누, *누구 |
| 112 | `dm_echo` | *echo, *말 |
| 113 | `dm_flushsave` | *flushrooms, *모든저장 |
| 114 | `dm_shutdown` | *shutdown, *종료 |
| 115 | `dm_force` | *f, *force, *뭐든지다시켜 |
| 116 | `dm_flush_crtobj` | *flushcrtobj, *재설정, *재 |
| 117 | `dm_create_crt` | *monster, *괴물, *괴 |
| 118 | `dm_stat` | *status, *상태 |
| 119 | `dm_add_rom` | *add, *방제작 |
| 120 | `dm_set` | *뭐든지다해 |
| 121 | `dm_log` | *log, *접속 |
| 122 | `dm_spy` | *spy, *뭐든지다엿봐 |
| 123 | `dm_loadlockout` | *lock, *제한 |
| 124 | `dm_finger` | *finger, *핑거 |
| 125 | `dm_list` | *list, *누구든지다봐 |
| 126 | `dm_info` | *info, *정보 |
| 127 | `dm_param` | *parameter, *수치 |
| 128 | `dm_silence` | *silence, *벙어리 |
| 129 | `dm_broadecho` | *broad, *방송 |
| 130 | `dm_replace` | *replace, *교체 |
| 131 | `dm_nameroom` | *name, *방이름 |
| 132 | `dm_append` | *append, *추가 |
| 133 | `dm_prepend` | *prepend, *서언 |
| 134 | `dm_cast` | *gcast, *전주문 |
| 135 | `dm_group` | *group, *그룹 |
| 136 | `notepad` | *notepad, *메모 |
| 137 | `dm_delete` | *delete, *지우기 |
| 138 | `dm_obj_name` | *oname, *뭐든지다바꿔 |
| 139 | `dm_crt_name` | *cname, *괴물이름 |
| 140 | `list_act` | *active, *활성 |
| 141 | `dm_dust` | *dust, *나도가끔화낸다 |
| 142 | `dm_follow` | *cfollow, *따르기 |
| 143 | `dm_help` | *dmhelp, *도움말 |
| 144 | `dm_attack` | *attack, *공격 |
| 145 | `list_enm` | *enemy, *적 |
| 146 | `list_charm` | *charm, *최면 |
| 147 | `dm_save_all_ply` | *사용자저장 |
| 148 | `family`, `family_news`, `family_who`, `boss_family`, `out_family`, `fm_out`, `family_member`, `family_talk`, `list_family`, `dm_moonstone`, `dm_monster` | 패거리가입, 패거리공지, 패거리누구, 가입허가, 패거리탈퇴, 패거리추방, 패거리원, 패거리말, ], 모든패거리, *떨어져라, *침공 |
| 149 | `buy_states` | 향상 |
| 150 | `marriage`, `m_send`, `divorce` | 결혼, 사랑말, 이혼 |
| 151 | `memo` | 메모 |
| 152 | `invite` | 초대 |
| 153 | `moon_set`, `resend` | 기억, 대답, / |
| 154 | `enemy_status` | 상태 |

### Handler source map

이 표는 위 ID의 source 위치를 좁히는 인덱스다. `src/comman5_old.c`는 같은
역사적 handler의 보존본이며 현재 `src/Makefile`의 `OBJECTS`에는 `command5.o`가
들어간다. 따라서 `comman5_old.c`를 운영 구현으로 읽지 않는다.

| 기능 묶음 | 주요 C source | 관련 ID/계약 |
| --- | --- | --- |
| 입력·로그인·생성·dispatch | `command1.c`, `io.c`, `player.c`, `player_store.c`, `player_path.c` | login/create, parse, alias, process_cmd, quit/return; ID 3, 52, 78, 81, 82 |
| 방·출구·이동·관찰 | `command2.c`, `command6.c`, `room.c`, `files1.c`, `files2.c`, `resource_path.c` | ID 1–2, 29–36, 49, 61–62 |
| 전투·도망·사망·적대 graph | `command5.c`, `command7.c`, `command8.c`, `creature.c`, `player.c` | ID 23, 37, 45, 50–51, 77, 97–99, DM 144–146 |
| NPC/tick/리젠/시간 | `update.c`, `creature.c`, `room.c`, `files2.c`, `main.c` | `update_game`, `update_users`, `update_random`, `update_time`, `update_monster*`, `update_exit` |
| 아이템·인벤토리·장비 | `command2.c`, `command3.c`, `command7.c`, `command8.c`, `command9.c`, `object.c`, `files1.c`, `files3.c` | ID 5–13, 40, 42–48, 58, 67, 83–85, 95–96 |
| 주문·마법·기술 | `magic1.c`–`magic8.c`, `command7.c`, `command8.c`, `command9.c`, `global.c:spllist/ospell` | ID 38–40, 58, 60, 62, 64–67, 87–91, 98–99; 주문 56개 |
| 상점·거래·은행 | `command7.c`, `command8.c`, `command10.c`, `bank.c`, `bank_store.c`, `bank_*` adapters | ID 41–44, 47–48, 63, 74–76 |
| 채팅·사회·게시판·우편 | `command2.c`, `command4.c`, `command10.c`, `command11.c`, `command12.c`, `post.c`, `board.c`, `action.c`, `alias.c` | ID 4, 17–20, 25, 53–57, 59, 68–73, 79–80, 84, 92–94, 100, 148–153 |
| 관리자 | `dm1.c`–`dm6.c`, `docs/dm_cmnd`, `docs/dm.doc` | ID 101–148의 `*` 명령; 권한/영속 side effect 별도 계약 필요 |

## 시스템 기능 ledger (명령 외 상태 전이)

명령 alias만 이관하면 기능 누락을 숨기게 되므로 C의 비명령 tick/저장/자료 형식도
별도 항목으로 기록한다. 아래 `expected`는 향후 fixture가 반드시 관찰해야 할
상태이며, 현재 값은 구현/검증 결과가 아니다.

| 원장 항목 | C 근거 | fixture 및 예상 출력/상태 | 기존 테스트 매핑 | Go 상태 / 인수 |
| --- | --- | --- | --- | --- |
| 세션·접속·입력 lifecycle | `io.c`, `command1.c`, `mstruct.h:iobuf/extra` | connect→prompt→command→disconnect; descriptor/session ownership, prompt/status | `tests/unit/trusted_admission_test.c`, `trusted_admission_file_authority_test.c`, `tests/unit/onboarding_*`, browser/stack onboarding tests | 미구현; 게임 session fixture 미착수 |
| 게임 캐릭터 생성·기존 로그인·비밀번호 | `command1.c:494-1210`, `player.c`, `files1.c` | 캐릭터 이름/비밀번호, 직업/성별/능력치/무기/성향/종족, 저장 후 재로그인; password는 출력/DB evidence에서 redacted | `tests/scenarios/create_and_relogin.json`, `tests/harness/run_onboarding_scenario.py`, `tests/unit/onboarding_credential_lifecycle_test.py`, `tests/unit/player_store_file_test.c` | **부분 규칙 조사만 존재; 전체 가입/로그인 미구현·미인수** |
| 파서·약어·alias | `command1.c:1497-1626`, `alias.c`, `global.c:cmdlist` | exact/약어 우선순위, `!` 재실행, 숫자/한글/공백/문장 끝, alias 저장/재생 | `tests/unit/info_alignment_utf8_test.py`, `tests/unit/alias_title_snapshot_v1_*`, `tests/unit/alias_title_snapshot_manifest_v1_*` (직접 parser oracle은 아님) | 미구현; `CMD-GAP` |
| 방/출구/이동/look | `command2.c`, `command6.c`, `room.c`, `files1.c`, `files2.c`, `mtype.h` flags | room id/name/description/visible exits, exit lock/open state, room constraints, player location and attachments | `tests/unit/files2_load_obj_test.c`, `tests/unit/room_time_test.c`, `tests/unit/resource_tree_manifest_test.py`; direct command test 없음 | 미구현; G1 fixture 필요 |
| 전투·공격·방어·도망·사망 | `command5.c`, `command7.c`, `command8.c`, `creature.c:263-624`, `player.c` | RNG seed/time injection, hit/damage/armor/thaco, enemy list, flee, death/XP/gold/drop/respawn, player/monster side effects | direct combat handler test 없음; `tests/unit/creature_v1_test.c`는 codec only, `tests/unit/update_schedule_test.c`는 schedule only | 미구현; G3 differential 필요 |
| NPC/tick/리젠/시간 | `update.c`, `main.c`, `room.c`, `creature.c`, `files2.c` | 20s user, random interval, active, 150s time, 20,000s moonstone, 4,000/5,000s invasion, timed exit/shutdown | `tests/unit/update_schedule_test.c`, `tests/unit/room_time_test.c` | 미구현; tick clock/RNG contract 필요 |
| 아이템 graph·인벤토리·장비 | `mstruct.h:130-242`, `command2.c`, `command3.c`, `object.c`, `files1.c`, `files3.c` | ordered recursive children, carry/weight, ready↔inventory normalization, wear restrictions, shots/flags, use/burn/repair | `tests/unit/object_v1_test.c`, `object_graph_v1_test.c`, `creature_v1_test.c`, `files1_decoder_test.c`, `files1_serializer_test.c`, `creature_object_layout_contract_test.py` | 미구현; codec tests are not gameplay acceptance |
| 주문·spell list·realm | `global.c:spllist/ospell`, `magic1.c`–`magic8.c`, `command9.c` | known spell/realm, MP cost, level/class/room restriction, duration/timer, combat vs utility, failure and dispel | direct magic tests 없음; `tests/unit/creature_v1_test.c` only serializes spell bytes | 미구현; G3 fixture 필요 |
| NPC 대화/talk files | `files3.c:256-342`, `command8.c:817-1039`, `mstruct.h:ttag`, `docs/crt_talk` | key→response/action/CAST/GIVE/ATTACK, bounded text and deterministic side effects | Go exact topic/no-topic receipt와 `ATTACK`, `CAST(성현진·수호진)` effect/event 경계는 구현·focused race 검증; `ACTION`/`GIVE`·그 밖의 CAST, 원본 대량 대조·전체 출력 parity는 미완료 |
| shop/buy/sell/trade/repair/forge | `command7.c`, `command10.c`, `command8.c`, `docs/rom_stor` | shop storage room/price, item ownership/value, trade quest outputs, repair/forge choices and costs | Go `제련` select_arm 1-6 receipt/replay tests; `무기만들기` first prompt/gate + select_newarm case 2-6; shop/repair/trade have separate suites | **부분 구현**; 제련·무기만들기 확인·금화 차감까지, 상점/거래 catalog는 미완료 |
| bank/inventory transfer | `bank.c`, `bank_store.c`, `bank_money_*`, `docs/porting-research/bank-live-capture-gap-20260907.md` | bank object graph and gold before/after, retry/conflict/legacy fallback, room requirement | `tests/unit/bank_store_test.c`, `bank_legacy_abi_test.c`, `bank_evidence_test.c`, `bank_snapshot_v1_test.c`, `bank_transfer_snapshot_v1_test.c`, `bank_money_*_test.c`; Go raw-bank ABI/graph tests, descriptor locator, kind-8 codec·raw conversion·`ImportBankSnapshot` receipt/replay/rollback tests; live gameplay route remains separate | **부분 구현**; raw locator·변환·artifact 검사·operator-owned PG import/evidence까지 완료, 대량 수집·계정 대조·라이브 graph/gold parity는 미구현 |
| 우편·게시판·메모·공지 | `post.c`, `board.c`, `command11.c`, `command12.c`, `docs/dm.doc` | append/read/delete ordering, board index/file bounds, room/permission requirements, durable text | no direct handler tests; `tests/stack-e2e/*` covers onboarding/evidence, not in-game board | 미구현 |
| alias/title/description/name | `alias.c`, `command2.c`, `command8.c`, `command11.c`, `command12.c` | alias list/order, title mutation, description/name validation, persistence and UTF-8 bounds | `tests/unit/alias_title_snapshot_v1_test.c`, `alias_title_snapshot_manifest_v1_test.c`, related contract/fixture tests | 미구현; snapshot tests are migration contracts, not Go command tests |
| family/kingdom/marriage/vote/invite | `command11.c`, `command12.c`, `post.c`, `player.c`, `mtype.h` | membership/owner permissions, war/reward, family chat/news, marriage/divorce, invite/vote file state | direct feature tests 없음; stack e2e only asserts selected directory cleanup/metadata | 미구현; P1/P0 ownership and transaction contract needed |
| admin/DM world editing | `dm1.c`–`dm6.c`, `docs/dm_cmnd`, `docs/dm.doc` | class gate, room/object/monster creation/edit/delete, force, spy, global cast, shutdown, save/reload | `tests/unit/player_writer_contract_test.py` and static policy tests cover link/authority constraints only; direct DM behavior tests 없음 | 미구현; separate privileged acceptance suite needed |

## 저장 데이터·자원 ledger

`src/mstruct.h`의 `object`, `room`, `creature`, `exit_`, `daily`, `lasttime`와
`src/mtype.h`의 root path/flags가 저장 설계의 출발점이다. raw legacy format은
native struct/pointer/endianness/`long` 폭에 묶여 있으므로 Go DB schema로 직접
복사하지 않는다.

| 저장/자원 영역 | 읽기·쓰기 근거 | 영속해야 할 의미 | 현재 검증 자산 | Go 이관 상태 |
| --- | --- | --- | --- | --- |
| rooms / exits / descriptions | `files1.c:282-395,817-1025`, `files2.c:64-361`, `room.c`, `mtype.h:ROOMPATH` | room identity/name/flags/limits/timers/traffic/visited, ordered exits, descriptions, permanent objects/monsters | `files1_*`, `files2_load_obj_test.c`, `room_time_test.c`, `resource_tree_manifest_test.py`, `docs/porting-research/persistence-supabase.md` | 미구현; P0 |
| object catalog / nested object graph | `files1.c:64-130,411-477`, `files3.c:26-248`, `object.c`, `mstruct.h:object` | fixed strings, value/weight/type/dice/flags, ordered nested children, parent ownership rebuilt not serialized | `object_v1_test.c`, `object_graph_v1_test.c`, decoder/serializer tests | 미구현; P0 |
| monster/NPC catalog and room instances | `files1.c:207-294,478-741`, `files2.c:385-515`, `creature.c`, `docs/crt_flag`, `docs/crt_talk` | stats/flags/spells/AI/talk/quest/carry and room attachment; runtime enemy/follower/talk pointers are not raw DB fields | `creature_v1_test.c`, `creature_object_layout_contract_test.py`, codec harnesses | 미구현; P0 |
| player record and inventory | `player_store.c`, `file_player_store.c`, `player_path.c`, `files1.c:152-206,465-536`, `command8.c:717-805` | identity, password migration policy, stats/HP/MP/XP/gold/quests/timers, ordered inventory; ready items normalized before save | `player_store_*`, `player_record_serializer_test.c`, `player_snapshot_v1_test.c`, `legacy_player_*` harnesses | 미구현; P0 |
| bank files / gold | `bank.c`, `bank_store.c`, `bank_snapshot_v1*`, `bank_money_*` | bank item graph and gold transaction with idempotent retry/conflict semantics | bank unit/C ABI/evidence tests; Go LP64 raw reader + descriptor locator + kind-8 codec/raw conversion + `ImportBankSnapshot` atomic success/replay/conflict/rollback; DB-free metadata inspection/conversion CLI; live capture explicitly gap-documented | **부분 구현**; raw locator·변환·artifact 검사·operator-owned PG import/evidence는 완료, 대량 account 대조·live transfer/gold parity·복구는 미구현 |
| aliases/titles and social files | `alias.c`, `post.c`, `board.c`, `command11.c`, `command12.c`, `mtype.h` roots | aliases/title/description, mail/board/news, family/marriage/vote/invite/memo ownership/order | alias/title contracts; no complete social gameplay fixture | 미구현; P1 |
| runtime/session/tick evidence | `mstruct.h:extra/iobuf`, `io.c`, `update.c`, onboarding/M3 files | descriptor/socket/queues/timers/RNG/session identity are runtime; command id/version/receipt is durable only where contract requires | onboarding/M3/journal tests; these do not implement Go world state | 미구현; T/P2 boundary pending |
| static help/editor/resource files | `docs/*`, `resource_path.c`, `bin/*`, `objmon/*`, `rooms/*` | versioned catalog/resource bytes and path aliases; not normal player DML | `resource_tree_manifest_test.py`, path relocation tools, docs | 미구현; R |

## 기존 테스트와 Go 테스트의 구분

현재 발견한 테스트는 대부분 저장 codec, admission/onboarding, bank adapter,
M3 shadow/journal, web protocol 또는 정적 정책을 검증한다. 특히 다음은 **게임
명령 인수 테스트가 아니다**.

- `tests/unit/creature_v1_test.c`, `object_v1_test.c`, `object_graph_v1_test.c`:
  CDTO/graph codec의 경계만 검증한다.
- `tests/unit/files1_decoder_test.c`, `files1_serializer_test.c`:
  raw file read/write 계약만 검증한다.
- `tests/unit/update_schedule_test.c`, `room_time_test.c`:
  시간/스케줄 일부만 검증하고 tick side effect 전체를 검증하지 않는다.
- `tests/unit/bank_*`:
  legacy bank/adapter/metadata 계약이며 Go `bank` 명령과 동일한 end-to-end가 아니다.
- `tests/unit/onboarding_*`, `tests/browser-e2e/*`, `tests/stack-e2e/*`:
  현재 onboarding/admission/UI evidence 경계이며, 인증 이후 C 게임 명령을 모두
  실행하는 테스트가 아니다.
- `tests/unit/alias_title_*`:
  alias/title snapshot 계약이지만 `process_cmd`의 exact/약어 우선순위 oracle은
  아니다.

검증 명령의 결과를 새로 만들지 않았다. plan이 정한 Go 기준 명령
`cd server && go test ./...`, `go test -race ./...`는 향후 Go package가 추가될
때 실행하고, 이 문서의 C test 목록은 “통과”로 보고하지 않는다. C `src/Makefile`
의 `unit-test` target도 선언된 실행 그래프일 뿐 이 조사에서 실행한 결과가 아니다.

## G0에서 남은 gap과 다음 인수 준비

1. 각 ID/handler에 대해 최소 한 개의 deterministic input fixture와 exact output,
   상태 diff(방/플레이어/NPC/object/경제)를 만들 것.
2. `time`, RNG, UUID/command ID, socket/출력 큐를 주입하고, 순서가 의미 있는
   linked list와 영속 DB row의 ordering을 fixture에 명시할 것.
3. C 명령 handler에 직접 연결된 테스트가 없으므로, C 실행 transcript를 수집하는
   disposable oracle harness를 먼저 만들 것. 파일/운영 데이터와 비밀번호를
   fixture로 복사하지 않는다.
4. `mstruct.h` raw pointer 영역과 `player_snapshot_v1`/ObjectGraph 계약을
   구분하고, 저장 대상·runtime 대상·정적 resource를 Go/Postgres schema 전에
   승인할 것.
5. 등록표의 주석 명령(`은신술`, `가입`, `탈퇴`, `전수`, `변수나한권`)을 유지할지
   제외할지 사용자 승인으로 확정할 것. 현재는 등록 기능으로 세지 않았다.
6. 현재 원장은 전 기능을 나열했지만 완료율/분모/예상 기간을 주장하지 않는다.
   각 행의 인수 상태는 첫 C fixture와 실패→통과 Go 테스트가 생긴 뒤에만 갱신한다.

## 2026-09-08 resource graph admission 후속

`LegacyRoomAdmissionPolicy`는 zeroed creature record를
`empty-monster-placeholder` issue로 기록하고 compatibility policy에서만 런타임
catalog에서 격리한다. strict decoder/corpus는 완화하지 않는다. `ImportNPCs`,
`ImportRoomItems`, `ImportNPCItems`는 각각 NPC identity, room floor, NPC inventory의
one-time canonical ID graph migration이며 room/root/nested 또는 NPC membership 순서를
보존한다. `State.Validate`는 모든 canonical item owner를 함께 검사하고, projection과
NPC death transfer는 기존 ID를 재발급하지 않는다.

검증: `TestLegacyRoomCatalogImportsCanonicalResourceGraphs`,
`TestImportRoomItems*`, `TestImportNPCItems*`, `TestNPCDeathTransfersCanonicalNPCItemsWithoutReallocatingThem`,
`TestPostgresSeedCanonicalRoomGraphs`, ARM64 PostgreSQL 17 CLI canonical seed. 이 결과는
resource seed 경계의 증거이며 전체 아이템 경제·player/bank 이관·scheduler/tick 인수로
승격하지 않는다. 저장 데이터 ledger의 rooms/object/NPC 행은 여전히 전체 gameplay
acceptance가 미완료임을 유지한다.

## 2026-09-08 운영 루프 연결 후속

`RunPlayerVitalScheduler`가 `WorldConnector.RunPlayerVitalPhase`를 `-player-tick`
cadence에 연결했다. `player-vitals-<slot>` deterministic command ID, fixed request
timestamp/hour, pending retry, duplicate-slot skip, connector writer serialization,
shutdown worker cancellation을 `world_tick_test.go`에서 검증했다. 이 항목은 player-vital
부분 루프의 운영 연결 증거이며 `update.c`의 전체 NPC/room spawn/combat/time scheduler나
전체 명령 인수를 완료로 표시하지 않는다. 실제 ARM64 PostgreSQL receipt/replay 증거는
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`와
`TestWorldConnectorPlayerVitalTickReplaysAcrossConnectorRestart`다.

## 2026-09-08 central command parser 후속

`session.ParseCommand`와 `CommandKind`가 현재 Go durable handler의 alias 분류를 한곳으로
모으고 `WorldConnector.Submit`의 순차 fallback을 제거했다. quote-aware tokenization,
7-token bound, direction-first dispatch, unsupported command fail-closed 응답을
`command_parser_test.go`와 transport dispatch 회귀로 검증했다. C의 전체 154 positive
command handler, prefix ambiguity, occurrence 문법, alias persistence는 여전히
미구현 ledger 항목으로 유지한다.

## 2026-09-08 document help 후속

`CommandHelp`를 parser/transport에 추가하고 `ExecuteHelpLine`을 durable receipt 경계에
연결했다. `help/`의 `helpfile`, `spellfile`, `policy`, 현재 Go handler 명령의
`help.<cmdno>`를 읽으며, source가 없거나 UTF-8이 아니면 receipt를 만들지 않는다.
unknown topic은 원본 C와 같은 no-help 응답을 결정론적으로 재생한다. unit/transport
TDD와 실제 ARM64 PostgreSQL 17 + Chromium E2E(`1 passed (11.0s)`)가 통과했지만, 이는
전체 C command table·문서 continuation·info title·약어 동등성 인수가 아니다.

## 2026-09-08 welcome/yell/emote 후속

`환영`은 고정 `help/welcome` 문서를 `fs.FS` 주입 경계에서 읽는 read-only receipt다.
누락·비 UTF-8은 fail-closed하고, 동일 command ID replay는 문서 재읽기와 commit을
재실행하지 않는다. `외쳐`는 `command6.c:yell`의 empty/silent/PHIDDN 순서를 원자
상태 전이로 옮기고, commit 후 current-room named event와 ordered-exit anonymous event를
파생한다. 본인·replay 재방송을 막고, unknown exit topology는 후보 상태를 저장하지
않는다. `TestPostgresYellCommandPersistsAndReplays`가 ARM64 PostgreSQL 17 저장·replay를
검증한다.

`action.c` 일반 플레이어 감정표현은 현재 출력 계약이 확보된 bounded exact alias 집합만
등록했다. `감정표현`, `노려봐`, `끄덕`/`응`, `감`/`감사`, `미소`, `청혼`, `떨어`, `해`,
`하품`, `웃어`, `미안`, `악수`, `하이파이브`, `박수`, `흡연`/`담배`, `절`, `찔러`, `춤`,
`노래`, `울어`, `달래`, `당황`, `생각`, `부끄러`, `놀려`, `설레`, `바이`/`잘가`, `안녕`,
`뽀뽀`, `윙크`, `구걸`, `구박`, `안아`/`껴안아`를 exact command로 처리한다. PHIDDN 해제와
PSILNC ordering을 보존하고, exact same-room online player target에만 target-specific
projection을 허용한다. NPC/prefix/occurrence/미검증 target은 fail-closed다. target·room
fan-out과 actor 제외는 transport에서 committed snapshot으로만 수행한다.
`TestPostgresEmoteCommandPersistsAndReplays`와 `TestWorldConnectorSubmitDispatchesEmoteWithTargetAndRoomProjection`
이 저장·replay·event 경계를 검증한다.

검증 명령과 결과: `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
전용 ARM64 `postgres:17-alpine` PG command tests, 실제 Go+PG+Chromium E2E **1 passed
(10.4s)**, `pnpm test:browser`의 표준/feature-off 각 1 passed. 이는 전체 154 C command,
strict room corpus의 기존 63개 예외, 전체 action alias·모바일/WSS/Ingress·testnet 인수를
완료했다는 의미가 아니다.

## 2026-09-08 express/look-at-target 후속

`표현`(`command11.c:emote`)은 actor response만 receipt에 저장하고, 255 UTF-8 byte
bound·control/invalid UTF-8 fail-closed, empty/silent no-op, non-silent PHIDDN clear와
PLECHO response ordering을 검증한다. committed snapshot에서만 `:이름님이 <text>.` room
event를 파생해 actor/replay 중복 fan-out을 막는다. `보아 <대상>`은 `action.c` explicit
target branch의 bounded slice로, exact same-room canonical target과 NPC-first traversal,
visibility/detect/DM-invisible gate를 적용한다. player target에는 target-specific event,
observer에는 room event를 주며 NPC target은 room projection만 허용한다. bare `보아`,
prefix/occurrence/object inspection과 전체 `조사`는 미구현 ledger 항목으로 유지한다.

`TestPostgresExpressCommandPersistsAndReplays`, `TestPostgresLookAtTargetCommandPersistsAndReplays`,
transport fan-out/receipt tests, full Go race/vet, Linux ARM64 cross-build와 실제
Go+PostgreSQL+Chromium E2E **1 passed (10.1s)**가 통과했다. 이는 전체 명령/alias,
strict room corpus의 기존 63개 예외, testnet 배포 인수를 승격하지 않는다.

## 2026-09-08 strict 방 본문 예외 감사

`TestRoomBodyCorpus`의 기준 실패를 다시 실행한 결과, 원본 `rooms/` 3,216개 중
63개가 계속 strict body decode를 거부했다. `InspectLegacyRoom` 기준 이슈는
`invalid-euc-kr` 80건, `missing-text-terminator` 13건, `trailing-data` 7건으로
총 100건이며, 이슈 offset은 변환 대상 필드의 시작 위치다. 이 기준선은
`server/internal/world/legacy_room_audit_fixture.go`의 63개 path/크기/소비 위치/
SHA-256/이슈 위치 fixture와 `TestRoomBodyCorpusExceptionAudit`가 고정한다.

이번 조사에서는 공통 runtime 변환을 적용하지 않았다.

- `invalid-euc-kr`는 일반 한글 문자열뿐 아니라 원본 바이트에 EUC-KR 표에 없는
  2바이트열(예: `r00/r00100`의 offset 588)과 그래픽성 바이트가 섞인 필드다.
  `resources_utf8/rooms`는 해당 파일의 바이트와 동일하여 독립적인 정정 원천이
  아니며, 대체 문자·삭제·임의 매핑은 원작 출력과 저장 값을 바꾼다.
- `missing-text-terminator`는 고정 폭 텍스트 필드가 폭을 모두 사용한 사례다.
  inspection은 필드 경계에서 읽기를 제한해 원본을 보존하지만, NUL을 합성하거나
  자르는 것은 C의 실제 문자열 사용 계약을 확보하기 전에는 runtime 변환으로
  승격할 수 없다.
- `trailing-data` 7건은 알려진 구조를 `Consumed`까지 읽은 뒤 비영(非零) 데이터가
  남는다. `r00/r00173`의 offset 751처럼 남은 바이트가 몬스터·아이템·타이머처럼
  보이는 경우도 있어, C reader가 EOF를 검사하지 않았다는 사실만으로 폐기 또는
  재해석을 결정할 수 없다. 원본 writer의 파일 길이 축소 보장과 파일별 oracle이
  없으므로 자동 절삭은 금지한다.

fixture는 각 예외의 원본 SHA-256·소비 위치·이슈 순서를 비교하고, caller raw bytes가
  변하지 않는 것과 `DecodeLegacyRoom` 및 zero-value `LegacyRoomAdmissionPolicy`가
  계속 거부하는 것을 확인한다. 따라서 이번 lane의 strict 예외 수는 **63개에서
  63개로 유지**되며, raw corpus와 strict/default 정책을 보존한 감사 증거만 추가됐다.
호환 정책으로 runtime catalog에 넣을 때도 기존 evidence 보존·명시적 검토 경계를
유지해야 한다. 후속 runtime 변환은 C 실행 transcript 또는 파일별 수동 매핑과
상태/출력 fixture가 확보된 뒤 한 issue class 이하의 bounded change로 재검토한다.

## 2026-09-09 NPC combat/death/shop bounded slices

`RunNPCCombatTick`/`RunNPCCombatPhase`는 C `update_active`의 canonical active order와
first enemy/player identity를 durable `npc-combat-<slot>` receipt로 연결한다. 여러
non-lethal `PlanNPCCombatRound` 결과를 하나의 candidate에 순서대로 적용하고,
lethal PLAYER는 `PlanNPCPlayerDeath`를 같은 candidate에 원자적으로 이어 붙인다.
사망 후에는 C의 `first_active` 재시작 경계에 맞춰 같은 tick의 후속 공격을 중단하며,
RNG 기록/재생으로 lethal probe가 원래 공격을 다시 실행하지 않는다. 실제 ARM64
PostgreSQL test는 receipt replay, request conflict, rollback과 RNG 미재실행을 확인했다.
이는 full NPC update/tick/broadcast 완료가 아니다.

`PlanNPCPlayerDeath`는 NPC 공격자의 C PLAYER 사망 branch를 source-backed pure candidate로
추가했다. NPC `MSUMMO`는 PLAYER branch에서 허용되며, room/active/enemy/follower identity,
progression/equipment/timer, floor drop, 1008 respawn, war 결과를 atomic하게 검증한다.
이 reducer는 durable combat tick의 lethal continuation에 연결됐지만, death
broadcast/savegame/summon side effect는 남아 있다.

`QuoteShopPurchase`/`BuyShopItem`과 `RunShopPurchase`는 `RSHOPP`/`RNOTEL` storage의
exact stock/value를 canonical nested graph deep-copy와 durable receipt로 연결한다.
구매 성공 `PHIDDN` 해제, gold/weight/capacity/allocator/temporary flag/stock ownership,
receipt replay/conflict/insert rollback을 실제 PostgreSQL 17에서 검증하지만
parser/list/sell/trade/merchant는 미구현이다.

## 2026-09-09 marketplace·scheduler·backup bounded slices

`ListShopItems`/`SellShopItem`과 `ExecuteShopLine`은 원작 `품목`/`팔아`의
canonical `RSHOPP`/`RPAWNS`·`RNOTEL` 경계를 receipt에 연결했다. list는 저장 순서와
가격을 읽기 전용으로 고정하고, sell은 exact direct-root/explicit occurrence,
visibility, 품질·내용물·event flag, weight/gold overflow, `value/2` payout과
`OPERMT`/`OTEMPP`/`OPERM2` 소유권 전환을 원자 적용한다. 실제 ARM64 PG17 replay/conflict
검증을 통과했지만 prefix/key `find_obj`, double-payout RNG, merchant/repair와 전체
경제 인수는 미완료다.

`NPCCombatScheduler`는 기존 durable combat reducer의 외부 lifecycle 경계다.
`RunOnce`/`Start`/`Run`/`Wait`/`Stop`/`Shutdown`이 고정 cadence와 pending retry/replay,
중복 worker 방지·cancellation을 검증한다. `07f63cb`에서 `-npc-combat-tick`과 main
worker WaitGroup/shutdown에 연결했지만 전체 update cadence와 room broadcast는 남아 있다.

`WorldBackup` envelope는 format/version/world ID/revision/state SHA-256을 포함하고,
unknown field·trailing JSON·checksum·`world.DecodeState` 실패를 거부한다. restore는
expected revision과 receipt 없는 대상만 기본 허용하며 `Force` 시 receipt를 삭제하고
writer epoch을 증가시켜 이전 writer를 fencing한다. `b9b1abf`는 0600·64 MiB·atomic
file export/restore CLI를 추가했고 `c2a0fa5`는 JSONB compacting checksum mismatch를
canonical JSON으로 수정했다. 실제 ARM64 PG17 API/CLI 복구 테스트를 통과했지만 백업
파일 보관·암호화·운영 복원 연습은 G4 인수 조건으로 남긴다.

## 2026-09-09 trade bounded slice update

ledger 76 `trade`는 전체 parity가 아니라 다음 범위의 Go 수직 slice로 갱신됐다.

- 원본 `src/command10.c:trade`의 suffix 입력 `물건 괴물이름 교환`과 명시적 occurrence 확장
- `NPCTradeOffers`의 C `carry` 쌍 import 및 canonical template 검증
- same-room `MTRADE` NPC/직접 inventory root exact selection, named/damaged/key mismatch 거부
- offered subtree 제거, reward subtree deterministic ID 복제, quest/experience/proficiency 반영
- durable receipt replay, request conflict, PostgreSQL 저장/재생 및 room broadcast

prefix/key `find_obj`, merchant NPC, repair/value, 전체 C 경제/ANSI parity는 여전히 미구현이며
이 항목의 전체 인수 상태는 `partial`이다.

## 2026-09-09 가치·수리·개인 메시지 bounded slices

원작 등록표의 서비스/통신 명령을 서로 다른 파일 소유권의 Luna max 레인으로 병렬
구현한 뒤 중앙 parser와 live connector에 통합했다.

- `value` (`가치`/`가격`): RPAWNS 또는 RREPAI에서 exact direct inventory
  name/positive occurrence를 선택해 pawn `value/2`(100,000 cap) 또는 repair
  `value/4`를 read-only typed receipt로 기록한다. migrated canonical item graph,
  visibility와 malformed/nested object fail-closed를 포함한다.
- `repair` (`수리`): source check order를 따라 room/item/type/condition/gold를
  확인하고 injected RNG, piety adjustment, break/refund/remove, successful shots
  restore를 snapshot-bound atomic proposal/apply로 처리한다. replay는 RNG를 재호출하지
  않는다.
- `sendman` (`얘기`/`이야기`): online player exact 우선·prefix fallback과
  PINVIS/PDMINV/PDINVI/PIGNOR/PSILNC를 적용하고, receipt에 저장한 recipient event를
  target 연결에만 보낸다. 모델에 없는 legacy last-message/ignore-list 필드는 만들지
  않는다.

world/session/transport race·vet, live connector, Linux ARM64 build 및 실제 ARM64
PostgreSQL 17 receipt 저장·replay 검증을 통과했다. 세 행은 전체 C prefix/key/ANSI
동등성이나 merchant/전체 경제 인수를 뜻하지 않으며, ledger 전체 상태는 `partial`이다.

## 2026-09-09 NPC 대화·그룹말·상인 구입 bounded slices

다음 세 서비스 lane을 파일 소유권을 분리해 Luna max 에이전트로 병렬 구현한 뒤
중앙 parser와 live connector에 직렬 통합했다.

- `대화`: same-room canonical NPC exact name/positive occurrence를 선택한다. `MTALKS`
  topic loader 계약이 없는 경우 topic 응답은 fail-closed하고, no-topic 응답은 receipt에
  고정한다. 성공 시 `PHIDDN` 해제와 `MTLKAG` enemy 관계를 atomic apply하며, actor 응답과
  room observer event를 replay에서 다시 방송하지 않는다.
- `그룹말`/`무리말`/`=`: C suffix와 terminal prefix 양쪽을 지원하고, authoritative
  mixed `FollowerRefs` 순서로 follower→leader event를 만든다. `PIGNOR`/`PDMINV`/
  `PSILNC`/caretaker 경계는 canonical flag만 사용하며 모델에 없는 eavesdrop/ignore
  영속 필드는 추가하지 않는다.
- `상인 구입`: `<NPC> <item> 구입` suffix와 양수 occurrence를 받는다. MPURIT NPC의
  `MerchantOffers`를 server-owned migration catalog로 분리하고 unresolved `Carry`는
  fail-closed한다. 가격·gold·weight·capacity를 모두 통과한 뒤 nested reward graph를
  deterministic ID로 복제하며, connector는 catalog를 주입받는다.

world/session/transport race·vet와 live connector dispatch/replay 회귀가 통과했다. 실제
ARM64 PostgreSQL 17 receipt 저장·replay와 Linux ARM64 cross-build도 이 lane 통합 후
통과했다. 전체 C command/economy parity, strict room corpus 63건,
NPC full cadence/broadcast, IME/mobile 실기기, WSS/Ingress 및 testnet 배포 인수는 여전히
미완료다.

## 2026-09-09 직접 관리 병렬 레인: 묘사·사용자 조회·귀환

세 개의 파일 소유권이 겹치지 않는 Luna max 레인을 병렬 실행한 뒤, 메인 세션에서
parser·world connector·room fan-out을 직렬 통합했다.

- `묘사`: 원작 `command12.c:description`의 suffix 입력(`<설명> 묘사`)과 bare
  `묘사` 초기화를 연결했다. C의 `strlen(fullstr)` 31바이트 경계, UTF-8/control
  검증, canonical trailing ASCII space, stale proposal 방어와 typed receipt/replay를
  포함한다.
- `사용자검색`/`사용자정보`: online canonical player의 exact-name 조회만 허용한다.
  PINVIS·PDMINV·PBLIND·PDINVI 가시성 게이트를 적용하고, offline/ambiguous/legacy
  file metadata는 existence oracle이 되지 않도록 fail-closed한다. 결과는 read-only
  receipt로 저장·재생한다.
- `귀환`/`귀`: C의 전투→광장→그룹 거부 순서, PFRTUN 목적지, 20레벨 초과 비무적
  도력 소진, 원자적 room membership 이동, PDMINV broadcast 억제와 source/destination
  observer event를 연결했다. denial도 unchanged-state receipt로 고정하고 replay에서는
  이동·방송을 재실행하지 않는다.

검증: 대상 world/session race 테스트, live connector dispatch·fan-out 테스트, 전체
`go test -race ./... -skip '^TestRoomBodyCorpus$'`, `go vet ./...`, Linux ARM64
cross-build, 실제 ARM64 PostgreSQL 17 receipt 저장·재생 테스트가 통과했다. strict room
corpus 63건, 전체 C alias/prefix/key/ANSI parity, NPC full cadence/broadcast,
IME/mobile 실기기, WSS/Ingress와 testnet 배포는 여전히 별도 인수 조건이다.

## 2026-09-09 직접 관리 병렬 레인: 비교·감정·명명

Orca 없이 메인 세션이 Luna max 하위 에이전트를 파일 소유권이 겹치지 않는 세 레인으로
직접 배치하고, 공용 parser·connector만 직렬 통합했다.

- `비교`: 원작 `command12.c:obj_compare`의 bare prompt와 무기/방어구 판정을 canonical
  direct inventory exact name/positive occurrence로 연결했다. BODY 방어구 감쇠, 직업별
  damage 보정, INVINCIBLE 레벨 표, unsupported/missing 응답을 typed read-only receipt로
  고정했으며 nested/equipped/legacy/unmigrated 대상은 fail-closed한다.
- `감정`: 원작 `command3.c:info_obj`의 THIEF 또는 INVINCIBLE 이상 권한, type/shots/
  dice/armor/trait 출력을 canonical root projection으로 옮겼다. invisible gate와
  deterministic response를 포함하며 상태는 변경하지 않는다.
- `명명`: 원작 `command8.c:chg_name` suffix(`<물건> [#] <이름> 명명`)를 canonical
  root mutation으로 옮겼다. 80-byte·UTF-8/control/공백 검증, OCNAME 제거·ONAMED 설정,
  stale proposal 방어, post-commit room announcement와 receipt replay를 포함한다.

검증: 세 레인의 world/session race 테스트, parser·live connector dispatch 테스트,
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`, `git diff --check`, 실제 ARM64
PostgreSQL 17의 세 명령 저장·재생 테스트가 통과했다. strict room corpus 63건, 전체 C
alias/prefix/key/ANSI parity, NPC 전체 cadence/broadcast, IME/mobile 실기기, WSS/Ingress와
testnet 배포는 여전히 별도 인수 조건이다.

## 2026-09-09 직접 관리 병렬 레인: 능력·칭호

Orca 없이 메인 세션이 Luna max 에이전트를 두 레인으로 병렬 배치하고, 공용 parser·
connector·room fan-out은 메인 세션에서 직렬 통합했다.

- `활보법`/`신원법`: 원작 `command9.c`의 직업 권한, 1..100 주사위·레벨/스탯 확률,
  LT_HASTE/LT_PRAYD·PHASTE/PPRAYD 슬롯, 성공 stat 상승과 실패 cooldown을
  snapshot-bound atomic reducer로 옮겼다. 수신자 응답과 committed room event를
  receipt에 고정하고 replay에서는 RNG·방송을 재실행하지 않는다.
- `경계`: PPREPA/LT_PREPA와 PBLIND·DM 예외를 반영한 cooldown/활성/atomic 전환을
  연결했다. `잠력격발`: INVINCIBLE 권한, LT_UPDMG/PUPDMG cooldown, 성공 시 HP/MP/
  dice/armor·효과 timer, 실패 시 원작 cooldown을 typed receipt로 저장한다.
- `칭호`/`칭호삭제`: 웹 계정과 분리된 터미널 명령으로 canonical PlayerState에
  78-byte UTF-8 title을 원자 저장·조회·삭제한다. 빈 값·control·공백 경계를 거부하며
  receipt replay는 상태 변경을 반복하지 않는다.

검증: 능력·칭호 world/session race 테스트, live connector dispatch/fan-out, 전체
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64
cross-build 및 실제 ARM64 PostgreSQL 17 저장·재생을 통과했다. 전체 C alias/prefix/key/
ANSI parity, strict room corpus 63건, NPC full cadence/broadcast, IME/mobile 실기기,
WSS/Ingress와 testnet 배포는 여전히 별도 인수 조건이다.
## 2026-09-09 직접 관리 병렬 후속: 기공집결·살기충전·참선

두 Luna max 레인이 파일 소유권을 분리해 원작 `command9.c`의 세 self-ability를
구현하고, 메인 세션이 parser·connector·room fan-out을 직렬 통합했다.

- `기공집결`: FIGHTER/INVINCIBLE 이상 권한, `PPOWER=52`·`LT_POWER=37`, 600초
  cooldown, DEX 확률, 성공 힘 +3·120초 효과, 실패 `now-590`을 atomic reducer로 고정했다.
- `살기충전`: ASSASSIN/THIEF/INVINCIBLE 이상 권한, `PSLAYE=53`·`LT_SLAYE=38`,
  canonical WIELD gate, 성공 THACO -3·150초 효과와 실패 cooldown을 고정했다.
- `참선`: CLERIC/PALADIN/INVINCIBLE 이상 권한, `PMEDIT=54`·`LT_MEDIT=39`, 700초
  cooldown, PIETY 확률, 성공 지능 +3·150초 효과와 실패 cooldown을 고정했다.

세 명령 모두 server-owned clock/RNG, snapshot-bound proposal/apply, typed result/event,
`ExecuteGame` receipt/replay를 사용한다. 첫 실행만 room observer에 fan-out하고 replay는
RNG·state mutation·방송을 재실행하지 않는다. 정확한 bare alias만 허용하며 웹 계정 가입은
추가하지 않는다.

이번 batch의 검증 비용은 레인별 targeted race/gofmt로 제한하고, 통합 후에만 전체 race·
vet·Linux ARM64 build·격리 PostgreSQL 17 receipt를 batch당 한 번 실행한다. 전체 C
alias/prefix/key/ANSI parity, strict room corpus 63건, NPC full cadence, 실기기 IME/mobile,
WSS/Ingress와 testnet 배포 인수는 여전히 미완료다.

## 2026-09-09 직접 관리 후속: 전역 잡담·환호

원작 `command4.c:broadsend/broadsend2`의 `잡담`/`잡`/`환호`를 Go world/session/
transport 경계에 연결했다. UTF-8·255바이트·제어문자 입력을 먼저 제한하고, PBRSND 일일
사용량, PSILNC·레벨·HP 게이트, C의 31칸 HP 할인표와 INVINCIBLE 보정을 snapshot-bound
reducer로 옮겼다. daily/HP는 Supabase receipt에 저장하고 descriptor-local cooldown과
가시 플레이어 입장 시 global cooldown은 runtime 경계에 남겼다. `PNOBRD`/`PNOBR2` 수신
거부를 적용한 전역 fan-out은 첫 commit에만 실행하며 동일 command ID replay는 재방송하지
않는다. actor도 수신 거부 플래그가 있으면 receipt 응답을 받지 않는다.

검증: world/session/transport `go test -race` 표적 테스트, parser·global fan-out·수신
거부·daily/HP·receipt replay 회귀가 통과했다. `TestRoomBodyCorpus`의 기존 strict 63건
예외는 정책대로 제외했다. 실제 PostgreSQL 17 receipt와 main Linux ARM64 build는 이
기능 lane에서 반복하지 않고 batch/main 경계에서 한 번 실행한다. 전체 C 명령/ANSI parity,
NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet 배포 인수는 여전히 미완료다.

## 2026-09-09 검증 중복 제거와 NPC/xterm bounded 후속

검증 비용을 다시 전수 대조해 `fast`의 깨끗한 작업 트리에서 `HEAD^..HEAD`를 자동으로
재검사하던 경로를 제거했다. `GO_FAST_COMMIT=1` 또는 `GO_FAST_BASE`를 명시한 경우에만
커밋 범위를 검사하며, `integration`에는 ARM64 cross-build가 없고 `main` 기본 브랜치
경계에서만 ARM64 build를 수행한다. 이번 기능 레인에서는 해당 고비용 gate, 실제 PG,
브라우저 및 release matrix를 반복하지 않았다.

- 메모리 WebSocket 회귀 테스트로 원작형 xterm 가입→월드 ID 생성→첫 명령→disconnect
  저장→동일 캐릭터 재로그인/재입장을 고정했다. `CreateInWorld`만 호출되는지와 캐릭터
  ID 보존을 검증하며 운영 코드/parser/connector는 변경하지 않았다.
- `TalkCatalog`가 C `load_crt_tlk`의 canonical file path와 ordered topic pair 및
  `ATTACK`/`ACTION`/`CAST`/`GIVE` descriptor를 읽고, CP949/EUC-KR fallback, duplicate
  first-match, malformed/path traversal/overflow를 fail-closed한다. 실제 `resources_utf8`
  88개 주소 가능 파일 중 1개 CP949 손상 자산 때문에 전체 catalog 연결은 보류했다.
- `PlanNPCMaintenance`/`ApplyNPCMaintenance`와 `ExecuteNPCMaintenanceReceipt`가 C
  `update_active`의 빈 방 정리, confused/charmed 만료, HP/MP 회복, 공격 timer, MWAND
  roll을 snapshot-bound/idempotent receipt로 보존한다. attack/flee/death, scheduler 및
  connector fan-out은 아직 별도 작업이다.

검증: world/session/transport targeted `go test -race` 및 `go vet` 통과, strict room
corpus 63건은 기존 예외 정책으로 제외했다. 전체 C parity, actual PG receipt batch,
Linux ARM64 build, browser IME/mobile, WSS/Ingress와 testnet 배포는 여전히 미완료다.

## 2026-09-09 검증 cadence guard와 NPC/xterm bounded 후속

- **검증 cadence**: `.github/workflows/ci.yml`의 `release-scope-guard`가 release DB/
  compatibility matrix를 기본 브랜치에만 허용한다. 기능 브랜치에서는 checkout·DB·matrix
  fan-out 전에 실패하므로 `fast`/`integration`만 사용한다. ARM64 cross-build는 `main`에서
  한 번만 실행한다.
- **NPC 주제 대화**: 주입형 `TalkCatalog`가 exact canonical file/key 응답과 C의 missing
  key shrug를 receipt projection으로 제공한다. 미지원 action side effect는 상태 변경 없이
  거부한다. parser/connector 기본 경로에는 아직 catalog를 연결하지 않았다.
- **NPC 유지보수**: bounded pre-combat `update_active` prefix를 slot-bound,
  idempotent receipt로 옮겼다. 실패 retry는 동일 slot/now/command ID를 사용하며 프로세스
  scheduler start wiring과 combat 순서는 후속 경계다.
- **xterm secret echo**: password prompt 입력이 xterm 화면·DOM·scrollback에 나타나지 않는
  Chromium 회귀 테스트를 추가했다.

검증: 영향 패키지 race/vet, 전체 Go `integration`, xterm Chromium 4건, web typecheck,
정책·shell·diff 검사를 통과했다. ARM64 cross-build·실제 PG·release matrix·실기기
IME/mobile은 cadence 정책에 따라 이번 기능 레인에서 반복하지 않았다.

## 2026-09-09 검증 중복 감사와 NPC 실행 연결

저장소의 workflow, validation script, pre-push hook, matrix 및 정책 테스트를 전수 대조했다.
자동 push/PR workflow는 없고, 수동 CI의 `fast`/`integration`/`main`/`release` scope가
각각의 비용 경계를 지킨다. `main`만 Linux ARM64 cross-build를 수행하며, `release`는
기본 브랜치의 승인된 호환성·DB·브라우저 검토로 제한한다. `fast` 분류기가 놓치던
`cmd/muhan` 진입점 변경은 `./cmd/muhan`을 추가해 scheduler/flag/listener 변경을 표적
검사하도록 보강했다. GitHub Actions의 clean checkout에서도 fast가 조용히 skip되지 않도록
manual `fast`는 shallow depth 2와 `GO_FAST_COMMIT=1`을 사용한다. 전체 race·vet·ARM64·
실제 PG·브라우저·차트 검증을 각 병렬 레인에서 반복하지 않는 정책은 유지한다.

`TalkCatalog` 주제 대화 주입을 session/transport와 `cmd/muhan`의 선택적
`-npc-talk-dir`/`MUD_NPC_TALK_DIR` 경계까지 연결했다. 경로가 없으면 MTALKS 주제 요청은
fail-closed하고, 지정 경로는 canonical 파일을 시작 시 한 번 읽어 server-owned catalog로
복사한다. 현재 체크인 자산의 CP949 손상 파일 1개는 정정 전까지 전체 로드를 막는 남은
조건이다. NPC 유지보수 bounded prefix는 프로세스 scheduler에 연결했지만 maintenance→
combat strict ordering은 다음 scheduler 통합 범위다. 실제 PG 검증은 opt-in 고유 DB에서만
실행한다.

## 2026-09-09 메일·게시판 bounded slice와 cadence 감사

직접 배치한 Luna max 레인에서 원작 `post.c`/`board.c`의 저장 경계를 분리 구현하고 메인
세션에서 parser·transport를 통합했다.

| 영역 | Go 경계 | 검증/남은 조건 |
| --- | --- | --- |
| 우체국 수신/삭제 | `State.Mailboxes`, `편지받기`, `편지삭제`, RPOSTO(10), ordered sender/body/timestamp, 전체 삭제 atomic receipt/replay | targeted race·integration 통과; interactive `편지보내기` editor와 legacy post 이관은 미구현 |
| 게시판 목록/읽기/삭제 | `BoardState`, `게시판`, `읽어 게시판 <번호>`, `글삭제 게시판 <번호>`, board ID 100–116/120, tombstone·조회수·작성자/DM 권한 | targeted race·integration 통과; `써` editor, 전체 board object/index/body 이관은 미구현 |
| 검증 비용 | fast=영향 race, integration=전체 Go, main=기본 브랜치 ARM64 1회, release=DB/browser/호환성 matrix | 정책·shell·YAML·migration coverage 통과; release job별 Node 초기화 1회로 통합; 중복 실행 결함 없음 |

이번 batch도 전체 C 명령/prefix/key/ANSI parity, strict room corpus 63건, NPC full cadence,
실기기 IME/mobile, WSS/Ingress와 testnet 배포 인수를 완료한 것으로 간주하지 않는다.

## 2026-09-09 xterm compose continuation 통합

앞선 world slice를 `WorldConnector`의 연결별 continuation에 연결했다.

| 영역 | 현재 동작 | 증거/남은 조건 |
| --- | --- | --- |
| `편지보내기 <이름>` | 우체국·canonical recipient를 확인한 뒤 메일 행을 모으고 첫 `.`에서 메일 하나를 원자 append | session/transport/world race, transient commit 재시도 회귀 통과; 실제 PG는 opt-in disposable URL에서만 실행 |
| `써` | 게시판 object/board ID를 확인하고 제목·본문을 모은 뒤 첫 `.`에서 번호/본문을 원자 append, `!!`·빈 제목 취소 | commit 뒤 observer event만 전달하고 replay는 억제; 원작의 제목 직후 빈 본문은 현재 fail-closed 차이로 기록 |
| 입력 경계 | continuation이 history/alias/일반 parser보다 먼저 소비되며 body의 `!`/`!!`는 명령으로 실행되지 않음 | WebSocket line framing은 기존 512-byte 계약 재사용; IME/mobile 실기기와 전체 C 출력 parity는 미완료 |
| 검증 cadence | fast=영향 패키지 race, integration=전체 Go race/vet/diff, main=ARM64 1회, release=PG/browser/호환성 | 이번 batch는 고비용 gate를 반복하지 않음 |

`server/internal/session/mail_board_command_pg_test.go`는 mail read/send/delete와 board
list/read/write 경계를 하나의 opt-in PostgreSQL 실행으로 묶는다. URL이 없으면 명시적으로
skip되며 일반 unit/race 성공을 실제 DB 증거로 해석하지 않는다.

## 2026-09-09 직접 관리 병렬 후속: 듣기거부·훔쳐

서로 다른 파일 소유권의 Luna max 두 레인을 병렬 구현하고, 메인 세션에서 공용 parser와
live connector를 직렬 통합했다.

- `듣기거부`: 원작 `command9.c`의 `first_ignore` 수명을 connection-local
  `IgnoreList`로 옮겼다. 최신 등록 우선·중복 no-op·원자 toggle·256개 상한·14바이트/12
  코드포인트 경계를 고정했으며, 연결 종료 시 목록은 버려지고 world/receipt/PostgreSQL에는
  저장되지 않는다. 대상 추가는 authoritative online exact identity와 `PDMINV`를 다시
  확인하고, 로그아웃 뒤 삭제는 원작 순서를 유지한다.
- 직접 메시지(`얘기`/`이야기`)는 target descriptor의 ignore list를 receipt 전에 검사해
  `is ignoring you` 응답으로 차단한다. 차단된 line은 world commit/event를 만들지 않으며,
  해제 후에만 기존 deterministic DM receipt와 exact target fan-out을 사용한다.
- `훔쳐`: 원작 `command6.c`의 도둑/무적 권한, 5초 `LT_STEAL`, stealth reveal, 안전방·정렬·
  시야·blind gate, chance/RNG, quest/ONEWEV 보호를 snapshot-bound reducer로 옮겼다.
  성공은 canonical root+nested item subtree와 player-kill timer를 atomic transfer하고,
  NPC 실패는 enemy 관계를 추가한다. legacy inventory와 아직 미이관 identity는
  `ErrStealInventoryUnresolved`/fail-closed로 남긴다.
- receipt의 room reveal/failure text와 player warning은 최초 commit 뒤 한 번만 fan-out하며,
  replay에서는 RNG·상태 변경·방송을 다시 실행하지 않는다. bounded 대상은 exact canonical
  이름만 인정하고 C의 prefix/occurrence·전체 ANSI 출력은 후속 differential 범위다.

검증: `go test -race` 대상 world/session/transport와 `go vet` 통과, `scripts/run-go-validation.sh
fast` 및 `integration` 통과. ARM64 cross-build·실제 PostgreSQL·브라우저/IME·release
matrix는 cadence 정책에 따라 이번 기능 레인에서 반복하지 않았다. strict room corpus
63건, 전체 C command/prefix/key/ANSI parity, NPC full cadence, WSS/Ingress와 testnet
배포는 여전히 미완료다.

## 2026-09-09 직접 관리 병렬 후속: 기습·물약·주문 전수

서로 겹치지 않는 신규 world/session 파일 소유권으로 Luna max 세 레인을 병렬 실행하고,
메인 세션에서 중앙 parser와 `WorldConnector` dispatch/room fan-out만 조립했다. 결과는
`c194824`에 기록했다.

- `기습 <대상>`: canonical same-room NPC/player 선택, 도둑·자객·무적 권한, 무기/쿨다운/
  시야/안전방/카오스·가문전쟁 게이트, hidden/invisible 해제, 명중·실패·파손·적대·
  proficiency와 비치명 HP 전이를 `PlanBackstab`/`ApplyBackstab` receipt로 연결했다.
  lethal 결과는 기존 사망 reducer와 원자 조합될 때까지 `ErrBackstabDeathTransitionPending`
  으로 fail-closed하며, player 대상 private warning과 room reveal을 commit 뒤 한 번만
  전달한다. C의 전체 prefix/occurrence와 사망 조합은 후속이다.
- `먹어`/`마셔 <물약>`: canonical inventory/ready root에서 POTION만 선택하고, 잔량·방/
  성향·직업 게이트, self-target 상태효과, OSPECI 1–6, charge/subtree 삭제와 map 이동을
  snapshot-bound receipt로 연결했다. 공격·대상 지정 주문과 C `restore`의 부분 성공/비소비
  경계는 정확한 결과 계약 전까지 소비 없이 fail-closed한다. PPOWER/PMEDIT/PPRAYD의 원본
  플래그 비트를 재대조해 고정했다.
- `가르쳐 <대상> [횟수] <주문>`: C `magic1.c:teach`의 cleric/mage/caretaker 및 주문
  레벨 권한, blind/silence/visibility, canonical same-room player key prefix·occurrence,
  교사 hidden 해제와 대상 spell bit 설정을 deterministic recipient projection으로
  연결했다. NPC fallback과 미확인 주문/문자열은 fail-closed한다.

검증: 세 레인 world/session/transport focused race, parser/connector 회귀와 `go vet`
통과; `scripts/run-go-validation.sh fast`와 조립 후 `scripts/run-go-validation.sh integration`
통과. ARM64 cross-build는 `main`, 실제 PostgreSQL·브라우저·호환성 matrix는 `release`에서
각각 한 번만 실행하므로 이번 기능 레인에서는 반복하지 않았다. strict room corpus 63건,
전체 C command/prefix/key/ANSI parity, NPC full cadence, IME/mobile 실기기, WSS/Ingress와
testnet 배포는 여전히 남은 인수 조건이며, 사용자 소유 `src/frp.new`만 dirty 상태로
보존한다.

## 2026-09-09 직접 관리 병렬 후속: 교란·맹공·혈도봉쇄

서로 겹치지 않는 world/session 파일 경계의 Luna max 세 레인을 병렬 처리한 뒤, 메인에서
중앙 parser·`WorldConnector` dispatch·room/target fan-out을 한 번만 조립했다. 코드 커밋은
`c5609e3` (`기능: 교란·맹공·혈도봉쇄 경계 연결`)이다.

- **교란**: 원작 `command8.c:circle`의 canonical same-room NPC→player 선택, 권한·PVP/
  가문전쟁·안전방·시야·`LT_ATTCK`·stealth 해제·확률/지연·`MUNKIL`·적대·`LT_BEFUD`를
  snapshot-bound proposal/apply와 durable receipt로 옮겼다. 비치명 상태 전이만 허용하고,
  사망·미해결 charm/war는 fail-closed한다.
- **맹공**: 원작 `command8.c:bash`의 fighter/barbarian/invincible gate, canonical 대상과
  무기/내구도, cooldown·stealth·보호 플래그, 명중·damage dice·befuddle·NPC 적대/
  proficiency 및 비치명 HP를 deterministic receipt로 고정했다. `die` 전이와 descriptor
  charm/전쟁 상태가 없는 경우 영수증 없이 거부한다.
- **혈도봉쇄**: 원작 `command7.c:magic_stop`의 NPC-only identity/visibility, occurrence와
  cooldown·MUNKIL·stealth reveal 경계를 먼저 고정했다. C의 `add_enm_crt`와 반 HP damage/
  death/flee 후속을 현재 canonical reducer가 표현하지 못하므로, 일반 대상은 RNG·timer/
  receipt를 만들지 않고 `ErrMagicStopCombatSideEffectPending`으로 fail-closed한다. 이를
  transport에서 unsupported 응답으로 변환해 세션을 끊지 않는다.

검증 결과:

```text
(cd server && go test -race ./internal/world -run 'Circle|Bash|MagicStop' -count=1) PASS
(cd server && go test -race ./internal/session -run 'Circle|Bash|MagicStop' -count=1) PASS
(cd server && go test -race ./internal/transport -run 'Circle|Bash|MagicStop' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
scripts/run-go-validation.sh fast PASS
scripts/run-go-validation.sh integration PASS
git diff --check PASS
```

이번 기능 레인에서는 ARM64 cross-build, 실제 PostgreSQL, 브라우저/IME, release matrix,
strict room corpus 63건, 전체 C prefix/key/ANSI parity, NPC full cadence, WSS/Ingress와
testnet 배포를 반복하지 않았다. ARM64는 `main`, DB/browser/호환성 검증은 `release` 경계에서
한 번만 실행한다. `src/frp.new`는 사용자 소유 dirty 변경으로 계속 보존한다.

## 2026-09-09 직접 관리 병렬 후속: 물품전달·독살포·적상태

파일 소유권이 겹치지 않는 세 Luna max 레인을 병렬 처리하고, 메인 세션에서 중앙
parser·`WorldConnector` dispatch·room/target fan-out을 조립했다. 코드 커밋은
`17c0619` (`기능: 물품전달·독살포·적상태 경계 연결`)이다.

- **물품전달(`줘`)**: 원작 suffix의 item/money source 순서를 bounded parser로 고정하고,
  canonical same-room player identity를 확인한 뒤 `ItemCollection` 전체 subtree 또는
  gold를 하나의 snapshot-bound receipt에서 원자적으로 이동한다. target private 응답과
  observer room event를 분리하며, NPC 수령·legacy inventory·quest/event/nested 보호·
  capacity/overflow는 `ErrGive*`로 영수증 전에 fail-closed한다.
- **독살포**: 자객/무적 권한, exact case-insensitive canonical NPC, visibility·stealth
  reveal·cooldown·`MUNKIL`, 원작 순서의 RNG·poison flag·HP/timer/enemy projection을
  `PlanPoison`/`ApplyPoison` receipt로 연결한다. enemy/death/flee 후속을 현재 canonical
  state와 원자 조합할 수 없는 경우 첫 RNG 전에 거절하고, committed room event는 최초
  실행에서만 fan-out한다.
- **적 상태(`상태`)**: canonical `room.NPCIDs` 순서의 same-room NPC만 읽어
  `display_status` 15칸 의미 바와 blind/visibility를 read-only receipt로 반환한다.
  player/legacy room monster fallback, prefix/occurrence 추측, HPMax 불능 값은 거부한다.

검증: 영향 패키지 및 transport 회귀 `go test -race`, `go vet`,
`scripts/run-go-validation.sh fast`, `scripts/run-go-validation.sh integration`,
`git diff --check` 통과. ARM64 cross-build는 `main`, 실제 PostgreSQL·브라우저·차트와
x64/Windows/macOS 호환 matrix는 승인된 `release` 경계에서 batch당 한 번만 실행하므로
이번 기능 레인에서는 반복하지 않았다. 전체 C prefix/key/ANSI parity, strict room corpus
63건, NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet 배포는 여전히 남은
인수 조건이다.

## 2026-09-09 G3 전투 bounded 후속: 방혼술·흡성대법·차기

| 원작 경계 | Go 구현 | 검증/남은 조건 |
| --- | --- | --- |
| `magic3.c:turn` / `방혼술` | cleric·paladin·invincible gate, canonical same-room NPC/occurrence, undead·visibility·`MUNKIL`, `LT_TURNS`/`LT_ATTCK`, chance·disintegrate/반 HP damage와 enemy 관계를 `PlanTurn`/`ApplyTurn` 및 durable receipt로 연결 | world/session/transport race·replay와 room fan-out 통과; canonical death/drop graph가 완전히 조합되지 않으면 lethal fail-closed |
| `magic3.c:absorb` / `흡성대법` | mage·invincible gate, canonical NPC, stealth reveal·cooldown·`MUNKIL`, source chance/damage, undead MP 소진 또는 HP 흡수·enemy damage를 `PlanAbsorb`/`ApplyAbsorb`로 연결 | focused race·replay·overflow/unresolved side-effect fail-closed 검증; 전체 전투·도주/사망 조합은 미완료 |
| `command8.c:kick` / `차기` | barbarian·invincible 권한, NPC/player canonical target, PVP 안전·전쟁·charm 경계, 무기·명중/damage dice·stealth·`LT_KICK`와 비치명 HP/enemy 전이를 `PlanKick`/`ApplyKick`으로 연결 | focused race·connector event/replay 통과; lethal `die` 전이와 전체 prefix/key/ANSI parity는 별도 승격 |

세 레인은 파일 소유권을 분리한 Luna max 병렬 작업 후 메인 parser·connector를 한 번
조립했다. 기능 레인에서는 `fast`/focused race만 사용하고 조립 후 `integration`을 한 번
실행한다. Linux ARM64 build는 기본 브랜치 `main`, 실제 PostgreSQL·브라우저·x64/Windows/
macOS matrix는 승인된 `release`에서만 실행해 반복 비용을 막는다. strict room corpus
63건, NPC full tick, IME/mobile·WSS/Ingress·testnet 배포는 아직 미검증이다.

## 2026-09-09 직접 관리 병렬 후속: 사용·암호·직업전환

세 Luna max 레인은 각각 별도 world/session 또는 storage 파일만 소유하고 병렬로
실행했다. 메인 세션에서 parser·connector를 한 번 조립한 뒤 전체 Go integration을 한
번만 수행했다.

| 원작 경계 | Go 구현 | 보수적 제한 |
| --- | --- | --- |
| `command9.c:use` / `사용 <아이템>` | canonical inventory/floor root, OUSEFL·SP_WAR, 기존 장비/물약 reducer 위임, PHIDDN·floor transfer·소비·room event 원자 receipt | scroll/wand/key와 미확인 reducer, prefix/모두/다중 occurrence는 fail-closed |
| `command11.c:passwd` / `암호` | current→new→confirm 상태기, bcrypt 1회 생성, secret 비노출, expected/replacement hash 기반 Postgres transaction·idempotent retry | 현재 WorldConnector/xterm continuation에 노출하는 연결은 후속, 실제 PG 실행은 opt-in |
| `command7.c:change_class` / `직업전환` | blind/RTRAIN/class/XP/PFAMIL gate, RTRAIN+1..+3 class fold, XP 100000 차감, `LowerPlayerLevel` stat/vital 보존·receipt | bare form은 typed prompt no-op, `예` one-line만 commit; family roster와 lethal/전역 후속은 fail-closed |

### 검증 비용 경계

현재 workflow는 manual dispatch만 사용한다. 기능 레인은 `fast`(영향 Go package race),
조립 batch는 `integration`(전체 Go race/vet/diff 1회), 기본 브랜치 `main`은 Linux ARM64
cross-build를 한 번만, 명시적 기본 브랜치 `release`는 PostgreSQL/browser와 x64·Windows·
macOS 호환성 matrix를 실행한다. migration을 두 번 적용하는 단계와 command replay는
재실행 안전성 증거라 유지하며, 동일 기능 레인에서 ARM64·DB·browser를 되풀이하지 않는다.

이번 batch의 focused race와 integration은 통과했다. strict room corpus 63건, 전체 C
prefix/key/ANSI parity, NPC full tick, 실기기 IME/mobile, WSS/Ingress·testnet 배포는
여전히 전체 인수 전제다.

## 2026-09-09 터미널 암호·Go 게이트웨이 좌표 및 중복 검증 감사

이번 bounded batch는 게임 연결의 account-only `암호` continuation과 중앙 xterm의 Go
WebSocket 좌표를 연결했다.

- `암호`는 `WorldConnectorConfig.PasswordStore`로 주입된 account credential 경계에서
  current→new→confirm을 수행한다. 새 bcrypt hash는 한 번만 만들고, expected hash를
  조건으로 저장하며, 응답 유실 재시도는 같은 replacement hash를 idempotently 인정한다.
  상태기는 connection-local이고 `history`·alias·world snapshot/receipt/event에는
  자격 증명을 넣지 않는다. WebSocket은 다음 입력의 `secret` boolean만 전달하며 close/
  cancel에서 보류 hash를 지운다. 실제 PostgreSQL 실행은 별도 opt-in 승격 조건이다.
- 웹 루트는 `MUD_GO_GATEWAY_URL`을 우선하고 `MUD_GATEWAY_URL`을 fallback으로 사용한다.
  ws/wss scheme, HTTPS mixed-content, malformed/missing 주소를 순수 helper가 판정한다.
  별도 웹 가입·Supabase Auth 단계는 루트 xterm 경로에 다시 추가하지 않는다.
- workflow/pre-push/matrix 호출 그래프를 재검사한 결과, 기능 레인은 영향 패키지 race,
  조립 후 `integration` 전체 race/vet/diff 1회, 기본 브랜치 `main`에서 ARM64 cross-build
  1회, 승인된 `release`에서 DB/browser/호환성 matrix만 실행한다. migration 2회 적용과
  command replay는 재실행 안전성 증거라 유지하며, 기능 레인마다 같은 고비용 검사를
  반복하는 경로는 확인되지 않았다.

검증: session/storage/transport race, Go `integration`, 웹 테스트 49개와 typecheck,
local-first/self-hosted policy, shell syntax 및 diff check 통과. ARM64 main build, 실제
PostgreSQL, browser/IME 실기기, release matrix와 testnet은 이번 기능 레인에서 반복하지
않았다.
