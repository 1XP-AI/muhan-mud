# RESUMED — 2026-09-15 사용자 명시적 재개

## CURRENT CHECKPOINT — 2026-09-15

현재 `codex/mud-identity-foundation`의 push된 브랜치 tip이 권위이며 정확한 SHA는 `git rev-parse HEAD`로 확인한다. PR #13은 Project #1 `In Progress` 항목으로 연결되고 `Refs #1`, `Refs #3`–`Refs #12`를 추적한다. #12 Go terminal-only lane은 Luna Max worker_done·coordinator 독립 검증·release 후 네 소유 파일을 commit/push했으며, shell/Playwright-list/scope/protected-file 검증은 PASS했다. Docker BuildKit은 호스트 파일시스템 100%와 metadata I/O 오류로 막혀 #12는 OPEN이다. 현재 active/reclaimable worker는 0개이며 `src.frp.new`는 기존 dirty 해시를 보존한다.

**이 아래의 기존 재개 기록과 현재 값이 충돌하면 이 CURRENT CHECKPOINT를 따른다.**

현재 정책 보정: 검증이 끝난 독립 slice는 파일 소유 범위를 확인한 뒤 로컬 checkpoint commit과 `private/codex/mud-identity-foundation` push로 보존한다. 기존 dirty 변경과 `src/frp.new`는 별도로 보호하며, 아래의 과거 capacity 대기·commit/push 미수행 문구는 이 보정 이후의 현재 작업에는 적용하지 않는다. HEAD는 `30740f0`이고 upstream 차이는 `0/0`이다. PR #13 (`https://github.com/1XP-Inc/muhan-mud/pull/13`)은 Project #1 `In Progress` 항목으로 추가되어 `Refs #1`, `Refs #3`–`Refs #12`를 추적하며 현재 `REVIEW_REQUIRED`다. #11 actual rooms/NPC/item dry-run `ctx_1c5d3f67da39` / `task_94a473d75945`는 worker_done·coordinator 검증·release 완료했다. Astra는 비용 정책상 사용하지 않는다.

현재 active task: #12 실제 Go terminal-only disposable browser lane `task_a332fb7991c8` / `ctx_b6f398bc2185` (gpt-5.6-luna max). 소유 범위는 local Docker runner/Dockerfile/script/docs이며, legacy Node/C lane과 제품·Go·migration 코드는 수정하지 않는다. 진단 선행 task `task_734330383ef2` / `ctx_eeb48d78446b`는 no-source-change 보고서 검증 및 release 완료했다.

최신 live checkpoint: #7 NPC ordinary combat `MPOISS`→`PPOISN` slice `ctx_8a120016169c` / `task_b67992572ae2`는 worker_done `msg_596526509b82`, coordinator 독립 검증, release 완료했다. `src/update.c:471-490`의 성공 hit 뒤 정확한 poison roll·threshold·durable tick/probe-replay를 네 파일에 반영했고, Project #7 Evidence와 issue comment를 갱신했다. #3/#7/#12는 Project `In Progress`, 이슈 `OPEN`이며 #11 `ctx_43c752e2ffb3` / `task_94a473d75945`는 Luna max capacity로 대기 중이다.

2026-09-15 최신 정착: #10 `command5.c:who` 중앙 라우팅 `ctx_e584eec17a21` / `task_57f83a285f8a` (report `/tmp/g10-who-routing-task_57f83a285f8a.md`, worker_done `msg_8437e1c3bedd`)는 exact `누구`·`누구 l`을 `CommandSocial`로 연결하고 `L`/`x`/추가 토큰/control·`그룹 l`·`무리 l`은 fail-closed로 유지했다. 기존 social handler의 long `종족` projection, read-only receipt와 replay 무중복을 WorldConnector 회귀 테스트로 확인했고, worker·coordinator race 2회·vet·gofmt·diff-check·scope/hash PASS 후 release했다. #10 Project는 `In Progress`, 이슈는 `OPEN`이며 chat/board/mail/group/family/admin 전체·full ledger/PG/E2E/browser/mobile/deploy가 남아 있다.

사용자가 GitHub Project 기반 goal 생성과 Luna max 오케스트레이션 재개를 명시했다. 현재 live 루트는 `/Users/jjangg96/Documents/1xp/muhan-mud.nosync`, 브랜치는 `codex/mud-identity-foundation`, HEAD는 `4e6055e`다. Orca Run은 `run_80efe6288c63`, coordinator는 `term_929886c4-466e-4f7d-a8ab-36a9cd747d15`, goal은 `01a0a045-fc22-7071-8752-614f31b7dfdb`다. 서브에이전트는 `gpt-5.6-luna` / `max`만 사용하며, 기존 dirty 파일과 `src/frp.new`는 보존한다. #7 worker는 release됐고 #11은 같은 정책으로 capacity 대기 중이다.

정착한 #10 who mode `ctx_2c795061f5c8` / `task_640072d3980c`(`/tmp/g10-who-mode.md`)는 worker_done·coordinator 독립 검증·release 완료했고, #10 issue comment `comment-5676165923`를 REST로 기록했다. #11 full-data dry-run `ctx_893d7c03076a` / `task_5ecd6d1e7e5c`(`/tmp/g4-full-data-dry-run.md`)는 worker_done·coordinator 검증·release 완료했다. #3 missing-destination 검증과 #4 duplicate-login/reconnect 회귀 테스트 worker는 ack/release 완료했고, #4는 부분 증거만 Project Evidence에 기록한 채 OPEN으로 유지했다. 이번 재개에서 #3 follow/lose occurrence·`나` alias slice(`ctx_1011bd560abb` / `task_208726132104`, `/tmp/g1-follow-occurrence.md`)와 #6 `buy_states` deterministic apply slice(`ctx_8e6aba74d1b8` / `task_831b2f6534d0`, `/tmp/g3-buy-states-apply.md`)도 Luna max로 정착하여 ack/release 완료했다. #7 C `display_rom` 전투 알림 parity slice(`ctx_5a84d3b6cb75` / `task_9557eba1bd0d`, `/tmp/g4-npc-display-combat.md`)는 표시 경로의 `Damage < 0` 오거부만 제거하고 nil Enemies·미해결 identity·legacy room monsters·실제 combat/tick guard를 유지했으며 worker_done·ack/release와 coordinator positive race 2회·vet·gofmt·diff-check를 완료했다. #7은 NPC tick/AI/공격·추종·리젠 전체가 남아 OPEN 유지한다. #9 reducer `ctx_54c92fc70cc2` / `task_78b6413b8708`(`/tmp/g3-recall-target.md`), C `add_ply_rom` destination-arrival follow-up `ctx_df5ceb4901eb` / `task_059cfc77ea2c`(`/tmp/g3-recall-target-arrival.md`), targeted positive occurrence `ctx_701c6210a39c` / `task_1309c925aad9`(`/tmp/g3-recall-occurrence.md`)도 worker_done·ack/release 또는 release 완료했다. targeted recall parser/atomic transfer/messages/source·target·destination fan-out/PHIDDN·PDMINV/replay와 C `find_crt` 1-based occurrence 증거를 Project에 반영했으나 전체 기술·퀘스트·PG/E2E·ARM64/browser/deploy acceptance가 남아 #9 OPEN 유지한다. 과거 아래 STOP 스냅샷의 stuck/unacked 항목은 위조·변경하지 않는다. commit/push/issue close/Helm/deploy는 수행하지 않았다.

- 2026-09-15 추가 정착: #8 shop C `EQUAL`/`find_obj` prefix/key selector slice `ctx_7a3dce18448b` / `task_f670f9284bd3` (report `/tmp/g3-shop-prefix-key.md`)와 #10 `moon_set` selector slice `ctx_c19cfca2eb1d` / `task_cabef61e4304` (report `/tmp/g10-moon-set-equal.md`)를 서로 비중복으로 병렬 수행했다. 각 worker와 coordinator의 anchored race 2회·vet·gofmt·diff-check PASS, 소유 범위 확인 후 release 완료. #8/#10 issue comment는 REST 등록했지만 Project v2 Evidence mutation은 GraphQL rate-limit 오류로 보류 중이며 두 이슈 모두 전체 인수 미충족으로 OPEN 유지한다.
- 2026-09-15 추가 정착: #8 `command7.c:value`·`command8.c:repair` C `EQUAL`/`find_obj` display-name·key[0..2] prefix selector `ctx_8228b9f9cd38` / `task_477997272cdf` (report `/tmp/g3-value-repair-equal.md`)를 #8 shop 파일과 겹치지 않게 수행했다. canonical direct Inventory 순서·positive occurrence·중복 필드의 root 1회 계산·OINVIS/PDINVI와 value nested-parent/repair parent-subtree·PHIDDN/RNG/cost/mutation/replay 보존을 검증했고 지정 8개 파일만 변경했다. coordinator anchored race 2회·vet·gofmt·diff-check PASS, `src/frp.new` SHA-256 보존. worker succeeded/settled이나 release request `6d40dcf7-6342-44ee-a8ee-c838f8e09e66`가 런타임 연결 종료 뒤 `release_pending`이며 동일 request로 복구 중이다. #8 issue comment `comment-5674298222`는 REST 등록했고 #8은 OPEN 유지한다.
- 2026-09-15 추가 정착: #8 `command10.c:trade` C `find_crt/find_obj/EQUAL` selector/parity `ctx_bd53e4152083` / `task_18cd2d7e5a4c` (report `/tmp/g3-trade-equal.md`)를 shop/value/repair 파일과 겹치지 않게 수행했다. room NPC·canonical direct Inventory 순서·positive occurrence·name/key prefix·MINVIS/OINVIS/PDINVI·unresolved preflight와 post-selection exact catalog name/key[0], atomic consume/reward/quest/replay를 검증했고 지정 4개 trade world/session/transport test 파일만 변경했다. coordinator trade race 2회·vet·gofmt·diff-check PASS 후 worker release 완료. real PG는 `MUHAN_TRADE_TEST_DATABASE_URL` 미설정으로 주장하지 않았다. #8 issue comment `comment-5674482676`는 REST 등록했고 #8은 OPEN 유지한다.
- 2026-09-15 추가 정착: #10 `command5.c:set/clear` settings parity `ctx_2d70877ca549` / `task_8f62312510e5` (report `/tmp/g3-settings-parity.md`)를 독립 파일 소유로 수행했다. C parser의 생략 `도망수치` 기본값 1, 1→10, 그 외 MAX(value,2), PWIMPY 유지 경계를 수정하고 ordinary/special/family/clear/flag-list/stale/replay를 보강했으며 `server/internal/world/settings.go`, `settings_test.go`, `server/internal/session/settings_command_test.go`만 변경했다. worker race 2회·vet·gofmt·diff-check와 coordinator settings race/scope PASS 후 release 완료, PG는 환경변수 미설정으로 skip했다. issue comment `comment-5674620085`를 REST 등록했고 #10은 OPEN 유지한다.
- 2026-09-15 추가 정착: #9 `magic1.c:study` C EQUAL selector/Ready fallback `ctx_8a1cabe509cf` / `task_1772927cc901` (report `/tmp/g3-study-equal.md`)를 study 전용 파일로 수행했다. direct Inventory 우선, display/key prefix·OINVIS/PDINVI·positive occurrence, Ready slot 독립 fallback, Ready root/subtree 학습 제거와 alignment room transfer를 proposal location/slot로 원자 처리했고 `server/internal/world/study.go`, `study_test.go`만 변경했다. worker race 2회·vet·gofmt·diff-check와 coordinator study race/vet/gofmt/diff-check/scope PASS 후 release 완료, issue comment `comment-5674657723`를 REST 등록했다. 전체 기술·퀘스트·PG/E2E·ARM64/browser/deploy가 남아 #9는 OPEN 유지한다.
- 2026-09-15 추가 정착: #9 training/level-stat parity `ctx_d9fb99d741a5` / `task_5d9001b153e3` (report `/tmp/g3-training-stat-parity.md`)를 training/level-up 전용 파일로 수행했다. C `command7.c:train`의 PUPDMG source order와 `player.c:up_level` signed legacy stat 경계를 맞추고 raw -1/-128 stat 증가 및 범위 밖 fail-closed를 보강했으며 4개 파일만 변경했다. worker race 2회·RaiseLevel race 2회·vet·gofmt·diff-check와 coordinator training/RaiseLevel race·vet·gofmt·diff-check/scope PASS 후 release 완료, issue comment `comment-5674678721`를 REST 등록했다. family edit/broadcast와 전체 #9 인수가 남아 OPEN 유지한다.
- 2026-09-15 추가 정착: #9 `magic1.c:zap` selector parity `ctx_312350c02cc4` / `task_158139d17eaf` (report `/tmp/g3-zap-selector.md`)를 zap 전용 파일로 수행했다. C `magic1.c:zap`, `object.c:find_obj`, `mtype.h:EQUAL`에 맞춰 direct Inventory의 display/key[0..2] case-insensitive prefix·OINVIS/PDINVI·positive occurrence와 direct miss 뒤 Ready fallback의 독립 counter를 적용했으며 `server/internal/world/zap.go`, `zap_test.go` 2개만 변경했다. worker race 2회·vet·gofmt·diff-check와 coordinator zap/parser race·vet·gofmt·diff-check/scope PASS 후 release 완료, issue comment `comment-5674806143`를 REST 등록했다. 전체 주문 catalog/퀘스트/PG/E2E/ARM64/browser/deploy 인수가 남아 #9는 OPEN 유지한다.
- 2026-09-15 추가 정착: #9 `magic1.c:drink` selector parity `ctx_3a60788174cf` / `task_e1c03fb93250` (report `/tmp/g3-drink-selector.md`)를 drink 전용 파일로 수행했다. C `EQUAL` display/key[0..2] case-insensitive prefix와 Inventory 실패 뒤 독립 Ready occurrence counter를 `selectDrinkRoot`에 적용했으며 `server/internal/world/drink.go`와 기존 dirty `drink_test.go`만 변경했다. TDD red→green, coordinator exact selector race·world/session/transport drink race·vet·gofmt·diff-check 및 `src/frp.new` SHA 보존 PASS 후 worker release 완료, issue comment `comment-5675031350`를 REST PATCH로 기록했다. 전체 기술·퀘스트·효과/출력·PG/E2E·ARM64/browser/deploy 인수가 남아 #9는 OPEN 유지한다.

현재 추가 정착한 slice: #8 equipment selector parity `ctx_68c6862a2533` / `task_6ac5b01084b5` (report `/tmp/g3-equipment-selector.md`). C `command3.c` wear/ready/hold의 Inventory-only 및 remove의 Ready-only 경계를 보존하면서 display/key[0..2] case-insensitive EQUAL prefix·positive occurrence·OINVIS/PDINVI를 장비 전용 selector에 적용했고 `server/internal/world/equipment_mutation.go`, `equipment_mutation_test.go`만 변경했다. worker·coordinator focused race/vet/gofmt/diff-check/scope PASS 후 release 완료, issue comment `comment-5675211450`, #8 OPEN 유지.
현재 추가 정착한 slice: #9 use selector parity `ctx_8eb67a16abce` / `task_ffc866df4b39` (report `/tmp/g3-use-selector.md`). C `command9.c:480-513`의 Inventory 우선·floor 독립 fallback과 OUSEFL/direct-root/no-Ready 경계를 보존하면서 display/key[0..2] case-insensitive EQUAL prefix·root 1회 occurrence를 적용했고 `server/internal/world/use.go`, `use_test.go`만 변경했다. worker·coordinator focused/boundary race·vet/gofmt/diff-check/scope PASS 후 release 완료, issue comment `comment-5675211488`, 전체 magic/quest/effect/output/운영 인수가 남아 #9 OPEN 유지.
2026-09-15 coordinator-only verification: 기존 구현의 #3 last-token look 경계와 #8 live item catalog Submit 회귀를 별도 race로 재실행해 PASS했다. 새 worker/코드 변경은 없고 #3 comment `comment-5675223848`, #8 comment `comment-5675223845`에 명령과 결과를 기록했으며 두 이슈 모두 OPEN 유지한다.

- 2026-09-15 정착한 Luna max slice: #8 `command2.c:burn` selector parity `ctx_47a7e10510c8` / `task_e47141e91778` (report `/tmp/g3-burn-equal.md`). direct Inventory-only C `EQUAL` display/key[0..2] prefix·root 1회 occurrence·OINVIS/PDINVI를 적용하고 기존 burn 보호·RNG/timer·atomic receipt/replay를 보존했다. `burn.go`, `burn_test.go`만 변경, focused world/session/transport race·vet·gofmt·diff-check·scope/hash PASS, release 완료. #8 comment `comment-5675417355`, OPEN 유지.
- 2026-09-15 정착한 Luna max slice: #9 `magic1.c:readscroll` selector parity `ctx_ed2192541449` / `task_a3e9a1d50892` (report `/tmp/g3-readscroll-equal.md`). direct Inventory 우선·display/key[0..2] prefix·OINVIS/PDINVI·root 1회 occurrence·Ready 독립 counter reset을 반영하고 기존 spell/admission/PHIDDN/atomic/replay 경계를 보존했다. 실제 변경은 `read_scroll.go`, `read_scroll_test.go`이며 coordinator가 worker payload 경로 불일치를 actual diff로 확인·보정했다. focused race·gofmt·diff-check·protected hash 및 정착 후 vet PASS, #9 comment `comment-5675417354`, OPEN 유지.
- 2026-09-15 정착한 Luna max slice: #9 `magic3.c:turn` selector parity `ctx_527376c45179` / `task_5bd56d0fd620` (report `/tmp/g3-turn-equal.md`). direct room NPC display/Keys[0..2] prefix·stored order·root 1회 occurrence와 MINVIS/PDINVI·PDMINV/validation을 보존했다. 실제 변경은 `turn.go`, `turn_test.go`, focused Turn 및 world/session/transport race·vet·gofmt·diff-check·protected hash PASS 후 release 완료. 초기 공유 burn-test compile obstruction은 burn owner가 해결했고, #9 comment `comment-5675417371`, OPEN 유지.
- 현재 `gpt-5.6-luna` / `max` active worker는 0개다. #10 `command5.c:who` mode parity `ctx_2c795061f5c8` / `task_640072d3980c` (report `/tmp/g10-who-mode.md`)는 worker_done·coordinator 독립 race/vet/gofmt/diff 검증·release 완료했고 issue comment `comment-5676165923`를 기록했다. #8 공통 Inventory EQUAL selector `ctx_d394dd99dc2d` / `task_caa0f6fd2878` (report `/tmp/g3-inventory-equal.md`)와 #12 ARM64 report-only `ctx_26ac84424e59` / `task_dbbc06845e53` (report `/tmp/g5-arm64-smoke.md`)는 worker_done·coordinator 검증·release 완료했다. Project #8/#9/#10/#12는 `In Progress`, 이슈는 OPEN이다. GraphQL reset 후 #8/#9/#10/#12 Evidence를 갱신하고 item-list로 상태·내용을 재확인했으며, #11 Evidence는 기존 최신 근거가 이미 반영되어 있었다. HEAD `4e6055e`, `src.frp.new` SHA-256 `d6c16b0ec01a1a4e07391c838b8ee63819e7353b0ece86d293d92373c30906eb` 유지.
- #11 disposable PostgreSQL `pg_dump/pg_restore` `ctx_a05309d87e17` / `task_9b6ae6d3e12b`와 #9 `command8.c:circle` `ctx_e30e083c50ea` / `task_e3b340f9e964`는 worker_done·coordinator 검증·release 완료로 정착했다.

- 2026-09-15 정착한 Luna max slice: #10 family-status direct handler acceptance `ctx_ee7b16d4f094` / `task_29483576b1b3` (report `/tmp/g10-family-status-handler.md`, worker_done `msg_43583e592e43`)는 `server/internal/session/family_status_command_test.go` 한 파일만 추가했다. `패거리누구`·`패거리원`·`모든패거리`의 `ParseFamilyLine`/중앙 `ParseCommand`, deterministic who/member/list receipt·replay·불변 snapshot·pre-receipt fail-closed를 검증했으며 worker/coordinator race 2회·vet·gofmt·diff-check PASS 후 release했다. #10 issue comment `comment-5676471061`와 Project Evidence를 갱신했고, chat/board/mail/group/family/admin 전체·PG/E2E/browser/mobile/deploy는 남아 OPEN 유지한다.
- 2026-09-15 정착한 Luna max slice: #7 `list_act`/`*active`·`*활성` canonical active-NPC projection `ctx_e6d3f48ed5bb` / `task_a298cbebd2c0` (report `/tmp/g7-dm-active.md`, worker_done `msg_17e67fea4050`)는 `dm_active.go`, `dm_active_command.go`, 중앙 parser/transport route와 각 focused test를 포함한 7개 소유 파일만 변경했다. ActiveNPCIDs canonical order, DM gate, unresolved identity fail-closed, read-only receipt/replay·fan-out 억제를 검증했고 worker/coordinator focused race 2회·vet·gofmt·diff-check·scope/hash PASS 후 release했다. `src/frp.new`와 HEAD `4e6055e`는 보존했으며 #7 issue/Project Evidence 동기화 후에도 active producer/tick/AI·공격·추종·리젠·NPC corpus·PG/ARM64 및 전체 G3 인수가 남아 OPEN 유지한다. 현재 Luna max active worker와 reclaimable worker는 0개다.

- 2026-09-15 정착한 Luna max slice: #7 `list_enm`/`*enemy`·`*적` canonical same-room enemy projection `ctx_315a85602863` / `task_db94f012dbf7` (report `/tmp/g7-dm-enemy.md`, worker_done `msg_35a9e20e5faf`)는 `dm_enemy.go`, `dm_enemy_command.go`, 중앙 parser/transport route와 각 focused test를 포함한 7개 소유 파일만 변경했다. `[발생번호]` suffix, RoomState.NPCIDs canonical occurrence/order, player/NPC enemy references, SUB_DM gate, missing/unresolved fail-closed, read-only receipt/replay·fan-out 억제를 검증했고 worker/coordinator DMEnemy race 2회·DMActive/DMFollow regression race·vet·gofmt·diff-check·scope/hash PASS 후 release했다. `src.frp.new`와 HEAD `4e6055e`는 보존했으며 #7 issue/Project Evidence를 동기화했다. NPC producer/tick/AI·공격·추종·리젠·NPC corpus·list_charm·PG/ARM64 및 전체 G3 인수가 남아 OPEN 유지한다. 현재 Luna max active worker와 reclaimable worker는 0개다.

- 2026-09-15 정착한 Luna max slice: #7 `list_charm`/`*charm`·`*최면` canonical charm projection `ctx_81513103a8e4` / `task_6ca9f199b428` (report `/tmp/g7-dm-charm.md`, worker_done `msg_115b6265efb6`)는 `dm_charm.go`, `dm_charm_command.go`, `state.go`, 중앙 parser/transport route와 각 focused test를 포함한 8개 소유 파일만 변경했다. `CharmRefs`의 nil/unresolved 대 nonnil-empty/known-empty 구분, 글로벌 canonical online player target, player/NPC charmer identity와 C head-insertion order, SUB_DM gate, no-receipt typed fail-closed, read-only receipt/replay·fan-out 억제를 검증했고 worker/coordinator race 2회·DMActive/DMEnemy/DMCharm regression race·vet·repo-root gofmt·diff-check·scope/hash PASS 후 release했다. `src.frp.new`와 HEAD `4e6055e`는 보존했으며 #7 issue/Project Evidence를 동기화한다. Charm producer/spell mutation·expiry, NPC producer/tick/AI·공격·추종·리젠·NPC corpus, PG/ARM64 및 전체 G3 인수가 남아 OPEN 유지한다. 현재 Luna max active worker와 reclaimable worker는 0개다.

이하 2026-09-14 STOP 스냅샷은 기록 보존용이며 현재 재개 지시보다 우선하지 않는다.

## Historical STOP — 사용자가 재개하기 전까지 오케스트레이션 금지

**Historical only.** 아래의 당시 금지 지시는 사용자의 2026-09-15 명시적 재개로 해제되었다.

## 스냅샷 (2026-09-14)

### Git
- 트리: `/Users/jjangg96/Documents/1xp/muhan-mud.nosync`
- 브랜치: `codex/mud-identity-foundation` tracking `private/codex/mud-identity-foundation`
- **HEAD: `c46579a3d02c74df39c3fad0c47b5bc92486f914`** (`c46579a`) 기능: G1 보기·이동, G3 상점·아이템, G4 방 corpus 예외 정책
- 원격 `private` = `https://github.com/1XP-Inc/muhan-mud.git`
- **`src/frp.new` dirty 바이너리 — checkout/commit/reset 금지. 아래 dirty 목록에서 제외함.**

HEAD `c46579a` 대비 dirty (`src/frp.new` 제외):

```
HANDOFF.md
docs/porting-research/orchestrator-resume.md
scripts/run-go-backup-restore-local.sh
server/cmd/muhan/backup_cli_test.go
server/internal/engine/command_test.go
server/internal/session/bank_command.go
server/internal/session/bank_command_test.go
server/internal/session/command_parser.go
server/internal/session/command_parser_test.go
server/internal/session/directional_command.go
server/internal/session/directional_command_test.go
server/internal/session/item_mutation_command_test.go
server/internal/session/items_command.go
server/internal/session/items_command_test.go
server/internal/session/look_command.go
server/internal/session/look_command_test.go
server/internal/storage/backup.go
server/internal/storage/backup_test.go
server/internal/storage/writer_test.go
server/internal/transport/world_connector_item_mutation_test.go
server/internal/world/container_mutation.go
server/internal/world/container_mutation_test.go
server/internal/world/look.go
server/internal/world/player_items.go
server/internal/world/player_items_test.go
```

### Orca (건드리지 말 것)
- Run: `run_80efe6288c63`
- Coordinator: `term_d96bf1ac-3e2a-4d14-9b89-1a4b35c801ba`
- **in-flight 리뷰:** `ctx_579d4464699d` / `task_99bc07fe3614` / `term_29a68935-ead3-4d68-bb07-fadf602ef207`
  - 제목: Independent review G3 directional 소지품 reject
  - 구현 `ctx_eac502525303` / `task_acb3221e265b`는 이미 succeeded + released
  - 리뷰 파일 존재: `{SCRATCH}/pr-g3-dir-items-reject-review.txt` (no P0/P1; leftover P3 mid-verb `동 소지품 extra`)
  - worker nextAction: `worker-release --dispatch ctx_579d4464699d` — **STOP 동안 실행하지 말 것**
- **unacked inbox:** `delivery_b984e9df5ff3` heartbeat `msg_267e4c654027` (replayed). **ack 하지 말 것**
- **stuck G0:** `task_7a36af246be8` / `ctx_583060c51aa5` dispatched, liveness unverifiable/stale. **`worker_done` 위조 금지**

### GitHub `1XP-Inc/muhan-mud`
- CLOSED: #2 only
- **OPEN: #1, #3, #4, #5, #6, #7, #8, #9, #10, #11, #12**
- 인수 체크박스+Evidence 없이 CLOSE 금지

## 사용자가 재개하기 전까지

하지 말 것: `worker-start`, `check --wait`, `check --ack`, `worker-release` (위 리뷰 포함), 이슈 close, helm-upgrade, `src/frp.new` 터치, clone/reset/rebase.

재개 지시가 오면 그때 `orchestrator-resume.md`의 이 STOP 블록을 읽고 `delivery_b984e9df5ff3` ack → `ctx_579d4464699d` 수거/release부터.

스크래치: `/var/folders/7s/1pkt8kzx41zg5k2ffpkz_zpr0000gn/T/grok-goal-9d493c8e45f1/implementer`
