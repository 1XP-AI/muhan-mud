# Muhan MUD 포팅 핸드오프

## 최신 구현·검증 체크포인트 — 2026-09-10 (`직업전환` xterm 확인)

bare `직업전환`을 원작처럼 xterm connection-local `예/아니오` 흐름으로 연결했다.
게이트 통과 전에는 prompt만 표시하고 receipt를 만들지 않으며, `아니오`는
`직업전환이 되지 않았습니다`로 취소한다. `예`일 때만 기존 snapshot-bound
change-class reducer를 동일 command ID로 호출하고, 저장/응답이 불확실하면 draft와
ID를 유지해 재시도한다. 기존 `직업전환 예` one-line 입력도 그대로 동작한다.

검증: `go test -race ./internal/session ./internal/transport -run 'ChangeClass|WorldConnectorBareChangeClass' -count=1`
및 `go vet ./internal/session ./internal/transport` PASS. 기능 레인에서는 PG/browser/
ARM64/release 게이트를 반복하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (패거리 탈퇴 전역 알림)

활성 회원 탈퇴 receipt에 전역 탈퇴 알림을 추가했다. actor는 제외하고 PNOBRD를 존중하며
최초 commit 뒤에만 전송, replay는 재전송하지 않는다.

검증: world/session/transport family race 및 영향 패키지 vet PASS. 이번 변경에서는 전체
PG/browser/ARM64/release 게이트를 반복하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (패거리 가입 알림)

가입 신청 receipt에 원작의 두목 알림을 추가했다. 알림은 canonical boss ID/name에
고정되고 최초 commit 뒤에만 전달되며 replay에서는 재전송하지 않는다.

검증: world/session/transport family race 및 영향 패키지 vet PASS. 이번 변경에서는 전체
PG/browser/ARM64/release 게이트를 반복하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (`패거리탈퇴` confirmation)

활성 패거리원의 bare `패거리탈퇴`를 xterm 안의 `예/아니오` 확인 흐름으로 연결했다.
확인 전에는 상태·receipt를 변경하지 않으며, `예`일 때만 기존 fee/member ledger 원자
reducer를 실행한다. pending 신청 취소와 미이관 ledger fail-closed는 유지한다.

검증: `go test -race ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 `go vet ./internal/session ./internal/transport` PASS. 이번 변경에서는 전체 PG/browser/
ARM64/release 게이트를 반복하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (`패거리가입` continuation)

bare `패거리가입`을 xterm 안의 원작형 목록→이름 선택→`예/아니오` 확인 흐름으로
연결했다. 목록과 취소/잘못된 선택은 저장하지 않고, 확인된 exact 이름만 기존 canonical
family mutation receipt로 넘긴다. 저장이 불확실하면 같은 command ID로 재시도할 수 있다.

검증: `go test -race ./internal/session ./internal/transport -run 'FamilyMutation|FamilyApplication'`
및 `go vet ./internal/session ./internal/transport` PASS. 이 변경에서는 고비용 PG/browser/
ARM64/release 검사를 반복하지 않았다.

## 최신 검증 체크포인트 — 2026-09-10 (Go + PostgreSQL 브라우저 수직 경로)

반복 실행하지 않던 실제 브라우저 게이트를 이번 기능 묶음의 종료 시점에 한 번 실행했다.
`bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`가 작업별
PostgreSQL 17 컨테이너를 만들고 Go 서버·웹 xterm을 연결해 다음 3개를 모두 통과했다.

- 브라우저에서 원작 방식 캐릭터 생성 → 월드 입장 → 명령 실행 → 재로그인
- 사전 이관 canonical 캐릭터 1회 입장 및 중복 세션 거부
- 모바일 viewport 변경 뒤 xterm 포커스·입력 유지

결과: `3 passed (17.7s)`, 종료 시 자신이 만든 PostgreSQL 컨테이너만 제거됐다. 운영
Supabase 권한/RLS, WSS/Ingress와 testnet 배포 증거는 아직 없으므로 이번 결과를 운영
승격으로 해석하지 않는다. 다음 기능 레인에서는 이 브라우저 게이트를 반복하지 않고,
코드 변경이 실제 브라우저 계약에 영향을 줄 때만 release cadence에서 재실행한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (vote raw→manifest builder)

레거시 `player/vote/<name>_v` collector와 canonical PostgreSQL import 사이에 DB 없는
`cmd/muhan` 승격 경계를 추가했다. `-build-vote-manifest-root`는 명시적 ISSUE digest와
raw 선택지 bytes를 다시 검증하고 operator의 exact name→immutable player ID mapping을
결합해 `vote-state-v1` manifest를 만든다. output은 mapping 옆 private immutable 0600이며
source root overlap, unknown mapping field, 누락/추가 파일, malformed choice/digest,
변경 output은 fail-closed한다. raw path/metadata/credential은 manifest와 storage request에
들어가지 않는다.

`-build-vote-manifest-dry-run`과 path-only `-import-vote-manifest`는 PostgreSQL/listener
없이 종료하고, 실제 변경은 `-import-vote-manifest-apply`에서만 기존
`Postgres.ImportVoteState` transaction/receipt를 호출한다. vote manifest mode는 다른
import/seed/backup/world mode와 섞이지 않는다. schema와 실행 예는
`docs/porting-research/go-vote-state-manifest.md`다.

검증:

```text
(cd server && go test -race ./cmd/muhan -count=1) PASS
(cd server && go vet ./cmd/muhan) PASS
CLI vote build dry-run/write, path-only validation, same-byte replay without DATABASE_URL PASS
```

이번 배치는 로컬에만 있으며 push/CI/배포는 실행하지 않았다. 실제 원본 대량 수집,
operator character 대조·승인, Supabase RLS/PITR·운영 복구와 browser/deploy 검증은 남아
있다. `src/frp.new`와 변경이 남은 기존 Orca worktree는 보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (vote canonical PostgreSQL import)

`Postgres.ImportVoteState`와 `mud_go.vote_imports`를 추가했다. 명시적 ISSUE
`CatalogDigest`와 immutable player-ID keyed `VoteState`만 받아 unresolved world snapshot에
원자적으로 설치하며, raw path/bytes·credential·이름 기반 claim·foreign actor·이미 import된
snapshot·stale revision을 거부한다. `vote_imports` evidence와 `world_commands` receipt를
같은 transaction에 기록하고 같은 command/aggregate만 replay한다.

검증: `TestNormalizeVoteStateImportRejectsSensitiveDuplicateAndDigestMismatch`,
`bash scripts/run-go-social-import-local.sh --allow-disposable` (ARM64
`postgres:17-alpine` import/replay/foreign aggregate),
`bash scripts/run-go-validation.sh integration`(race/vet/diff) PASS. raw→manifest
builder·운영 원본 대조·Supabase RLS/PITR·브라우저/배포는 미완료다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (vote raw collector)

`server/internal/world/legacy_vote_file_locator_v1.go`에 레거시
`player/vote/<name>_v` 수집기를 추가했다. 명시적 absolute root, descriptor-anchored
no-follow walk, 0700/euid 디렉터리·0600 regular/nlink=1 파일, 64KiB bound, canonical
이름, fd 전후 stat와 fresh rewalk, `_v` 외 entry 및 batch 변경 감지를 적용한다.
`LocateLegacyVoteRawFilesV1`는 lexical order의 owned bytes/SHA-256/제한 metadata만 반환하며
계정·character claim·VoteState·DB/runtime 쓰기는 하지 않는다. ISSUE 길이/선택지와 명시적
name→ID mapping은 기존 `vote_import.go` 및 후속 검토 manifest에서 처리한다.

검증: `(cd server && go test -race ./internal/world -run 'LegacyVoteFileLocator' -count=1)` PASS,
`bash scripts/run-go-validation.sh integration` PASS. 전체 world corpus에서 기존 방 본문
63건 예외는 별도 skip 대상이다. 아직 실제 원본 대량 수집·operator mapping·manifest/apply·
운영 Supabase 권한은 미완료다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (bank snapshot review→import manifest)

은행 raw→kind-8 변환 review와 `Postgres.ImportBankSnapshot` 사이를 잇는 DB-free CLI를
추가했다. `-build-bank-snapshot-manifest-review`와 `-build-bank-snapshot-manifest-mapping`은
canonical digest/크기/root/node, review source player name, operator account/player/item ID와
expected revision을 다시 대조해 private immutable import manifest를 만든다. 이름 기반
claim, 평문 credential, raw path/bytes는 manifest에 들어가지 않는다. malformed/unknown
field/path traversal/digest·graph·account mismatch/중복 identity·item은 fail-closed한다.

`-build-bank-snapshot-manifest-dry-run`과 path-only `-import-bank-snapshot-manifest`는
DATABASE_URL/listener 없이 검증만 수행한다. 실제 DB 변경은 명시적인
`-import-bank-snapshot-manifest-apply`에서만 기존 atomic bank import/receipt를 호출하며,
동일 command ID/revision으로 중단 batch를 재개할 수 있다. schema는
`docs/porting-research/go-bank-snapshot-manifest.md`다.

검증:

```text
(cd server && go test -race ./cmd/muhan -count=1) PASS
(cd server && go vet ./cmd/muhan) PASS
CLI bank build dry-run/write, path-only validation, same-byte replay without DATABASE_URL PASS
git diff --check PASS
```

이번 배치는 로컬에만 있으며 push/CI/배포는 실행하지 않았다. 실제 account/character 대조,
대량 운영 이관, Supabase RLS/apply·복구/PITR와 live bank/gold parity는 남아 있다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (social raw→manifest builder)

## 최신 구현·검증 체크포인트 — 2026-09-10 (social raw→manifest builder)

collector와 explicit import 사이를 잇는 DB-free `cmd/muhan` builder를 추가했다.
`-build-social-family-root`는 family raw locator/parser와 `family_identity`의 explicit
boss/member ID map을 결합해 `family-ledger-v1` manifest를 만든다. `-build-social-memo-root`는
operator가 열거한 recipient 및 sender ID map으로 `player/fal/<name>`을 파싱해
`character-memos-v1`를 만든다. 두 경로 모두 malformed/unknown mapping/duplicate identity/
source-output overlap을 fail-closed하고, raw path·bytes·password·credential은 manifest나
storage request로 넘기지 않는다.

`-build-social-manifest-dry-run`은 DB/listener/output 없이 같은 검증을 수행한다. 일반 output은
mapping과 같은 private `0700` 디렉터리의 immutable `0600` 파일이며, 같은 bytes 재실행만
허용한다. 생성물은 사람 검토 후 `-import-social-manifest` 재검증과 별도
`-import-social-manifest-apply`에서만 DB 변경에 사용한다. mapping schema/example은
`docs/porting-research/go-social-manifest.md`다.

검증:

```text
(cd server && go test -race ./cmd/muhan -count=1) PASS
(cd server && go vet ./cmd/muhan) PASS
CLI family/memo dry-run·write·same-byte replay without DATABASE_URL PASS
git diff --check PASS
```

이번 배치는 로컬에만 있으며 push/CI/배포는 실행하지 않았다. 실제 원본 tree 대량 수집,
operator ID/timezone 승인, Supabase apply/RLS/PITR와 전체 social parity는 남아 있다.
`src/frp.new`와 미병합/미커밋 기존 Orca worktree는 보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (legacy social raw collector)

레거시 `family/family_list`·`family_member_<n>` 원장을 읽는
`LegacyFamilyFileLocatorV1`/`InspectLegacyFamilyRawFilesV1`와 `player/fal/<name>`을 읽는
`LegacyMemoFileLocatorV1`/`ParseLegacyMemoFileV1`를 추가했다. 두 collector는 명시된
absolute root, no-follow descriptor walk, private 0700/0600·euid·nlink=1·크기 bound·교체
재검사를 적용한다. family는 C sentinel/row 순서와 class/name을 재현하고, memo는 C
`ctime`/header/body 3-line format·UTF-8·80바이트·timestamp bound를 재현한다. 이름은
무결성 evidence일 뿐이며, caller가 공급한 immutable character ID mapping 없이는 결과를
만들지 않는다. locator path/SHA는 migration evidence로만 남고 canonical state·runtime에는
raw path·password·DB 쓰기가 들어가지 않는다.

검증:

```text
(cd server && go test -race ./internal/world -run 'Legacy(Family|Memo)' -count=1) PASS
(cd server && go test -race ./internal/world ./internal/storage ./cmd/muhan -run 'Legacy(Family|Memo)|Social' -count=1) PASS
(cd server && go vet ./internal/world ./internal/storage ./cmd/muhan) PASS
(cd server && GOOS=darwin GOARCH=arm64 go test -c ./internal/world) PASS
(cd server && GOOS=linux GOARCH=amd64 go test -c ./internal/world) PASS
git diff --check PASS
```

collector는 기존 social manifest CLI와 분리된 migration-only 입력 단계다. 실제 원본 tree
대량 수집, operator account/character mapping·timezone 승인, manifest 생성/apply, Supabase
운영 권한·PITR·전체 social parity는 남아 있다. 이번 배치는 로컬에만 있고 push/CI/배포를
실행하지 않았다. `src/frp.new`와 미병합/미커밋 변경이 있는 기존 Orca worktree는 보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (소셜 manifest CLI·복구 reader)

검토된 `family-ledger-v1`/`character-memos-v1` JSON manifest를 읽는 운영 경계를
추가했다. 기본 경로 또는 `-import-social-manifest-dry-run`은 private 0600·크기·형식·
unknown field·canonical ID/name/class/timestamp/fee를 검증한 뒤 `DATABASE_URL`,
listener, writer를 열지 않고 종료한다. `-import-social-manifest-apply`를 명시한 경우에만
`ImportFamilyLedger` 또는 `ImportCharacterMemos`를 호출하며, 다른 import/seed/backup/
inspection/world 모드와 섞이지 않는다. 평문 비밀번호·credential·raw/source path·이름
기반 claim은 manifest 스키마에서 거부한다.

재시작 경계에는 `ReadFamilyLedgerEvidence`, `ReadCharacterMemosEvidence`,
`RestoreSocialState`를 추가했다. PostgreSQL normalized rows의 count/hash/receipt/row
ordering을 읽어 현재 world snapshot과 대조하고, expected revision과 writer fence를
확인한다. evidence가 snapshot을 덮어쓰거나 누락 identity를 복구하지 않으며, tamper·
orphan·불일치는 fail-closed한다.

반복 검증은 `scripts/run-go-social-import-local.sh --allow-disposable`로 ARM64
`postgres:17-alpine` 한 개만 만들고 CLI apply·import replay/rollback·restore·fence를
실행한 뒤 자신이 만든 컨테이너만 삭제한다.

검증:

```text
(cd server && go test -race ./cmd/muhan -run 'Social' -count=1) PASS
(cd server && go test -race ./internal/storage -run 'Social' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/storage) PASS
bash scripts/run-go-social-import-local.sh --allow-disposable PASS
bash scripts/run-go-validation.sh integration PASS
git diff --check PASS
```

이번 배치는 로컬 작업 tree에만 있으며 push/CI/배포는 실행하지 않았다. 실제 legacy
`family_member_*`/`player/fal` 수집기와 operator 승인 mapping, 전체 social/family
parity, normalized evidence 보관·PITR, room body 63건, 브라우저·모바일·WSS/Ingress·
testnet 승격은 남아 있다. `src/frp.new`와 변경이 남은 기존 Orca worktree는 계속
보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (패거리 추방·canonical 소셜 원장 이관)

이번 배치에서 `command12.c:fm_out`의 온라인 패거리 추방 경계를 Go
`State → session receipt → WorldConnector`까지 연결했다. 온라인 canonical
PFMBOS 두목과 정확한 가시성 대상, 동일 패거리의 `FamilyState` member row를 모두
확인한 뒤 대상의 PFAMIL/DL_EXPND를 지우고 원장에서 제거한다. 원작 계약대로 fee는
0이고 패거리 전체 방송은 만들지 않으며, 대상이 아직 같은 canonical 이름인 경우에만
commit 후 대상 전용 알림을 한 번 보낸다. self/hidden/ambiguous/missing-ledger/
stale/replay는 commit 전에 닫힌다.

`Postgres.ImportFamilyLedger`와 `ImportCharacterMemos`는 검토된 pointer-free
aggregate만 받아 world snapshot, normalized family/memo evidence, `world_commands`
receipt를 한 transaction으로 갱신한다. command ID replay와 aggregate conflict,
expected revision/writer fence, canonical ID/name/class/timestamp/fee, nil pre-migration
aggregate, credential/raw-path 입력을 fail-closed한다. legacy raw 파일이나 이름 기반
계정 claim은 수행하지 않는다.

검증:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Family|Memo|ParseCommand' -count=1) PASS
(cd server && go test -race ./internal/storage -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport ./internal/storage ./cmd/muhan) PASS
ARM64 postgres:17-alpine에서 social import/replay/rollback 테스트 PASS
bash scripts/run-go-validation.sh fast PASS
bash scripts/run-go-validation.sh integration PASS (전체 Go race/vet/diff gate)
git diff --check PASS
```

이번 배치는 로컬 작업 tree에만 있으며 push/CI/배포는 실행하지 않았다. 실제 legacy
`family_member_*`/`player/fal` 수집기와 operator 승인 manifest, normalized evidence
복구 reader, Supabase RLS/운영 권한, 전체 social/family 출력 parity, room body 63건,
브라우저·모바일·WSS/Ingress·testnet 승격은 남아 있다. `src/frp.new`와 변경이 남은
기존 Orca worktree는 삭제하거나 stage하지 않는다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (패거리 원장·메모 command vertical slice)

이번 배치에서 패거리 승인/활성 탈퇴와 `메모 <캐릭터명> <내용>`을 canonical
State→session receipt→WorldConnector 경계까지 연결했다. 패거리에는
`FamilyState`/`FamilyMember` 원장을 추가해 정확한 canonical target·PFMBOS와
catalog boss 권한·fee/gold overflow·멤버 중복·stale/replay를 검증한다. 활성
탈퇴는 `family_gold*20000`을 차감하고 원장에서 제거하며, 가입허가는
`family_gold*10000`을 두목에게서 신청자에게 이전하고 pending→member로 전이한다.
대화형 bare continuation과 `fm_out`의 미검증 부작용은 계속 fail-closed다.

메모는 canonical recipient ID 아래 append-only aggregate로 저장하고, offline recipient는
허용하되 actor는 online이어야 한다. UTF-8·80바이트·control 문자·정확한 이름·중복
command ID를 검증하며, `State.Memos == nil`인 pre-migration snapshot은 receipt 전에
거부한다. 비밀번호와 raw player path는 State·request·receipt에 포함하지 않는다.

검증:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Family|Memo|ParseCommand' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport ./cmd/muhan) PASS
bash scripts/run-go-validation.sh fast PASS (재실행; 첫 실행은 기존 시간 출력 flaky test로 실패)
go test -race ./... EXPECTED FAIL: 기존 room body corpus의 unsupported 63건
bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable PASS (3/3, real Go + ARM64 PostgreSQL + Chromium)
git diff --check PASS
```

이번 변경은 로컬 작업 tree에만 있으며 push/CI/배포를 실행하지 않았다. 실제 Supabase
import에서 `FamilyState`/`Memos`를 검토된 canonical aggregate로 채우는 단계, 전체
family/social command parity, legacy room body 63건 변환, live gold/bank parity,
운영 Supabase·WSS/Ingress·testnet 검증은 남아 있다. 기존 사용자 변경 `src/frp.new`와
dirty Orca worktree는 삭제하거나 stage하지 않는다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (legacy bank raw 변환 산출물)

`cmd/muhan`에 `-convert-bank-raw-root`, `-convert-bank-raw-player`,
`-convert-bank-raw-abi`, `-convert-bank-cdto-output` 및 명시적 dry-run을 추가했다.
locator가 읽은 raw LP64 bank stream을 exact ABI로 다시 검증하고 canonical kind-8
`BankSnapshotV1` 파일과 `.review.json`을 만든다. review에는 source/canonical SHA-256·크기·
root/node count·원본 player path만 있고 world/player ID·item ID·계정 claim·credential은
없다. 출력 parent는 private 0700, 산출물은 immutable 0600이며 source root 내부 출력,
symlink/권한 오류, 변경된 재실행은 fail-closed한다. dry-run은 canonical bytes를 검증하지만
파일·DB·listener를 만들지 않는다.

검증:

```text
(cd server && go test -race ./cmd/muhan ./internal/world -run 'BankRawConversion|LegacyBankSnapshotRaw|LegacyBankFileLocator|BankSnapshotInspectionCLI' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/world) PASS
GOOS=linux GOARCH=amd64/arm64 go build ./cmd/muhan PASS
GOOS=darwin GOARCH=arm64 go test -c ./cmd/muhan PASS
git diff --check PASS
```

이 산출물은 사람의 source/name 대조와 별도 `ImportBankSnapshot` manifest 없이는 운영
계정이나 gameplay authority를 만들지 않는다. 대량 raw 수집·account/character 대조·live
bank/gold parity·실제 Supabase import/복구·브라우저/모바일·WSS/Ingress·testnet은 남아 있다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (legacy bank locator·raw 검사 및 IME Enter 경계)

`server/internal/world/legacy_bank_file_locator_v1*.go`에 C
`src/bank_file_locator.c`의 `MUHAN_HOME/player/bank/<name>` 레이아웃을 따르는
descriptor-anchored raw 파일 수집 경계를 추가했다. 명시적인 absolute root와 canonical
player name만 받고, root/player/bank 0700·euid 소유, 파일 0600·regular·nlink=1·4MiB
bound를 no-follow syscall로 확인한다. open/stat/read 후 재검사와 root tree rewalk로 교체를
감지하며, 반환값은 소유한 bytes와 SHA-256·inode/mode/time metadata뿐이다. 이 단계는 raw
object/account/password를 해석하거나 DB/runtime에 쓰지 않는다.

`cmd/muhan`에는 `-inspect-bank-raw-root` + `-inspect-bank-raw-player` (+ 명시적
`-inspect-bank-raw-dry-run`)을 연결했다. exact `LegacyBankSnapshotRawV1ABI` 검증 후
raw→kind-8 parser evidence를 실행하고, stdout에는 source/canonical digest·크기·root/node·
파일 metadata만 JSON으로 출력한다. canonical BankSnapshot 검사와 seed/import/world/backup
모드는 상호 배타적이며 `DATABASE_URL`·listener 초기화 전에 종료한다. object text·balance·
credential은 출력하지 않는다.

웹 `ClassicTerminal`은 IME 조합 중 xterm이 먼저 내보내는 CR/LF를 조합 종료 다음 task로
미루어 한글 입력을 중복 제출하지 않도록 했다. 일반 문자/Backspace·재접속/cleanup 경계는
그대로 유지한다. `terminal-play-smoke`에 이 불변식을 추가했다.

검증:

```text
(cd server && go test -race ./cmd/muhan ./internal/world -run 'LegacyBankSnapshotRaw|LegacyBankFileLocator|BankSnapshotInspectionCLI' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/world) PASS
(cd web && npm test) PASS (58 tests)
(cd web && npm run typecheck) PASS
(cd web && npm run build) PASS
```

실제 운영 raw 경로 수집/계정·캐릭터 대조/`ImportBankSnapshot`, 라이브 입출금 parity,
실제 OS IME·모바일 키보드, 운영 Supabase·WSS/Ingress·testnet 배포는 아직 남아 있다.
`src/frp.new` 및 변경이 남은 기존 worktree는 계속 보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (레거시 은행 raw 이관·검사 CLI)

`server/internal/world/legacy_bank_raw_v1.go`에 C `read_obj`가 저장한 LP64
`object=376` + little-endian `int` child-count 재귀 스트림을 읽어 pointer-free
`BankSnapshotV1`로 바꾸는 migration-only 경계를 추가했다. ABI 문자열을 명시적으로
고정하고 포인터·padding은 해석하지 않으며, fixed-string NUL tail·shots current clamp,
negative/trailing/truncated 입력, 4MiB·4096 list·64 depth·8192 node 한계를 fail-closed로
검사한다. canonical kind-8 CDTO와 source SHA-256 evidence를 별도로 소유하고, 계정 매핑
및 DB/runtime 쓰기는 수행하지 않는다. C `bank_evidence_test.c`의 zeroed object fixture와
교차 테스트를 유지한다.

`cmd/muhan`에는 `-inspect-bank-snapshot-dir`/`-inspect-bank-snapshot-file`을 연결했다.
검사 모드는 다른 seed/import/world/backup 모드와 섞을 수 없고 `DATABASE_URL`·listener
초기화 전에 종료하며, stdout에는 digest/size/root/node 등 metadata-only JSON만 출력한다.
`-inspect-bank-snapshot-dry-run`은 소스 지정 여부를 다시 확인하는 명시적 안전 표식이다.

검증:

```text
(cd server && go test -race ./cmd/muhan ./internal/world -run '^(TestBankSnapshotInspectionCLI|TestLegacyBankSnapshotRawV1|TestBankSnapshotV1)' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/world) PASS
make -C src bank-store-test bank-evidence-test PASS
```

남은 조건은 raw 은행 파일을 운영 경로에서 안전하게 수집하는 file-locator/대량 batch,
실제 account/character 대조와 `ImportBankSnapshot` 연결, 라이브 입출금 parity·복구 및
전체 G3/G4/G5 인수다. `src/frp.new`와 기존 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (Go BankSnapshotV1 codec)

`server/internal/world/bank_snapshot_v1.go`에 C/Rust kind-8 `BankSnapshotV1`과 동일한
canonical ObjectGraphV1 wrapper를 추가했다. 단일 detached root, envelope/digest/4 MiB
bound, fixed-string/shots/preorder graph를 검증하고 `Inspect`/`Verify`는 source bytes와
독립 SHA-256을 소유 복사로 보존한다. 이 코드는 offline migration/recovery evidence
경계라서 계정·세션·PostgreSQL을 변경하지 않는다.

검증:

```text
(cd server && go test -race ./internal/world -run '^TestBankSnapshotV1' -count=1) PASS
(cd server && go vet ./internal/world) PASS
bash scripts/run-cdto-differential.sh PASS
```

남은 조건은 legacy bank 파일 parser/계정 매핑, gold·nested graph 실제 import receipt,
복구 연습과 전체 G3 기능 인수다. `src/frp.new` 및 예전 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (은행 import receipt·터미널 플레이 smoke)

`server/internal/storage/Postgres.ImportBankSnapshot`가 검토된 kind-8 은행 CDTO를
이미 연결된 account/character에만 붙인다. canonical graph와 caller-owned item ID
manifest를 먼저 확인하고, world state·`mud_go.bank_imports` evidence·`world_commands`
receipt를 한 transaction으로 갱신한다. 같은 command ID는 metadata-only 결과를 재생하고,
중복 계정/캐릭터·revision/writer·manifest 충돌은 fail-closed한다. raw graph·비밀번호·
credential은 receipt/evidence에 저장하지 않는다. `scripts/run-go-bank-snapshot-import-local.sh
--allow-disposable`는 ARM64 `postgres:17-alpine`에서 성공·replay·rollback을 검증하며,
자신이 만든 컨테이너만 종료한다.

`cmd/muhan`에는 DB/runtime을 열지 않는 `InspectBankSnapshotReview(JSON)` 경계와
`-inspect-bank-snapshot-dir`/`-inspect-bank-snapshot-file` 실행 플래그를 추가해
private `0700` 디렉터리의 `0600` `.bin` 파일을 lexical 순서로 읽고 digest/size/root/node
metadata만 생성한다. 웹에는
`web/lib/terminal-play-smoke.ts` 계약 harness를 추가해 terminal signup→world command→
reconnect→same-character relogin, secret frame, malformed/mixed gateway, focus/IME 규칙을
브라우저·DB 없이 결정론적으로 검증한다.

검증:

```text
(cd server && go test -race ./internal/storage -count=1) PASS
(cd server && go test -race ./cmd/muhan -run '^Test(ValidateBankSnapshot|InspectBankSnapshot|PlayerSnapshot)' -count=1) PASS
(cd server && go vet ./internal/storage ./cmd/muhan ./internal/world) PASS
bash scripts/run-go-bank-snapshot-import-local.sh --allow-disposable PASS
(cd web && npm test) PASS (57 tests)
(cd web && npm run typecheck) PASS
```

실제 legacy bank 파일 수집/계정 대조, 라이브 bank 명령 parity, 실제
브라우저·IME/mobile·WSS/Ingress·Supabase 운영 이관/복구는 아직 남아 있다. `src/frp.new`
와 기존 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (review→import manifest builder)

`cmd/muhan`에 `-build-player-snapshot-manifest-review`와
`-build-player-snapshot-manifest-mapping`을 연결하는 DB-독립 builder를 추가했다. raw 변환이
만든 `player-snapshot-review.json`과 사람이 승인한 private identity mapping을 review 순서로
매칭하고, CDTO canonical SHA-256·round-trip·snapshot name·inventory graph count를 다시
검증한 뒤 기존 `-import-player-snapshot-manifest`가 소비하는 immutable `0600` manifest를
생성한다. `-build-player-snapshot-manifest-dry-run`은 파일 검증만 수행하며 DB/listener를
시작하지 않는다. mapping은 exact account/player/item ID·expected revision·Base64 bcrypt
hash를 모두 요구하고, 평문 password·누락 필드·경로 traversal·중복 identity/item을
fail-closed한다. 출력은 review와 같은 private `0700` 디렉터리에만 기록되며 다른 bytes로
덮어쓰지 않는다.

검증:

```text
(cd server && go test -race ./cmd/muhan -run 'PlayerSnapshotManifest(Build|DryRun)|BuildPlayerSnapshot|WritePlayerSnapshotManifest' -count=1) PASS
(cd server && go vet ./cmd/muhan) PASS
```

이 단계도 실제 Supabase import를 수행하지 않는다. 운영 raw 수집·사람의 계정/캐릭터 대조·
mapping 승인·대량 import/복구·전체 command parity·브라우저/IME/mobile·WSS/Ingress·testnet
승격은 남아 있으며, `src/frp.new`와 예전 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (native raw→CDTO review conversion)

`cmd/muhan -convert-player-snapshot-raw-dir`를 추가했다. 명시된
`LegacyPlayerSnapshotRawV1ABI`와 private `0700` source/`0600` files를 먼저 전부 검증하고,
DB·게임 런타임·identity claim 없이 별도 private output에 canonical CDTO와
`player-snapshot-review.json`을 만든다. output은 nested shard directory를 `0700`으로
만들고 CDTO/review를 atomic immutable write하며 동일 bytes replay만 허용한다. duplicate
name, source/output overlap, malformed raw, permission, changed output은 fail-closed한다.

review에는 source/canonical SHA-256, 크기, graph node 수와 suggested account name만 있고
password/native pointer/player ID/item ID/bcrypt hash는 없다. 사람 검토 후 기존
`-import-player-snapshot-manifest`에 exact mapping과 별도 credential hash를 추가해야 한다.

검증:

```text
(cd server && go test ./cmd/muhan -run 'PlayerSnapshotRawConversion' -count=1) PASS
```

전체 이관·identity binding·운영 Supabase·복구/배포·전체 기능 parity는 미완료이며,
`src/frp.new`와 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (legacy native player raw reader)

`server/internal/world/legacy_player_raw_v1.go`에 C `read_crt_player`의 native player
파일을 audited little-endian raw-v1 ABI로 읽어 pointer-free `PlayerSnapshotV1`로 바꾸는
migration-only reader를 추가했다. `creature=1952`, `object=376`, 8/16/32/64-bit scalar,
pointer=64 계약을 자동 추정하지 않고 고정하며, root/child count·depth·전체 object bound와
truncated/trailing EOF를 fail-closed한다. HP/MP·shots current clamp와 NUL 문자열 tail
정규화를 재현하고, fd/ready/native pointer/password는 결과에 넣지 않는다.

`cmd/muhan -inspect-player-snapshot-format legacy-player-raw-v1`로 private raw tree를
read-only 수집할 수 있다. `mud_go.player_snapshot_import_ledger`에는 raw source SHA-256,
크기, parser/ABI, 결과·quarantine 사유·inventory node 수만 저장하며 payload/credential/
identity claim은 없다. raw는 operator 검토 후 canonical CDTO와 account/player/item manifest를
만들어야 import할 수 있다.

검증:

```text
(cd server && go test -race ./internal/world -run 'LegacyPlayerSnapshotRawV1' -count=1) PASS
(cd server && go test -race ./cmd/muhan -run 'PlayerSnapshot(Inspection|Manifest)|InspectPlayerSnapshotDirectoryAcceptsAuditedLegacyRawFormat' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/world) PASS
bash scripts/run-legacy-player-snapshot-v1-differential.sh PASS (C raw fixture ↔ Go/C CDTO byte comparison)
```

운영 raw player 수집·ABI 승인·전체 캐릭터 대조/복구·대량 import·account recovery와 전체
게임 기능 인수는 미완료다. `src/frp.new` 및 예전 dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (PlayerSnapshotV1 operator manifest CLI)

`cmd/muhan -import-player-snapshot-manifest`와
`-import-player-snapshot-manifest-dry-run`을 추가했다. manifest는 정확한 `0600` 정규
파일의 JSON v1이며, record별 expected revision·canonical account name·exact player ID·
item-ID 순서·source SHA-256·Base64 bcrypt hash·private snapshot path를 요구한다.
모든 record와 snapshot을 PostgreSQL에 연결하기 전에 읽고 CDTO canonical round-trip,
digest, graph count, duplicate/path/permission/unknown-field을 검증한다. 평문 비밀번호
필드는 거부하고, dry-run은 DB 환경변수 없이도 성공한다. 실제 모드는 record 순서대로
`ImportPlayerSnapshot`을 호출하고 각 원자 transaction의 결과를 hash/count만 출력한다.
동일 command ID 재실행은 기존 receipt replay에 맡긴다.

계약과 운영 예시는 `docs/porting-research/go-player-snapshot-manifest.md`에 있다.

검증:

```text
(cd server && go test ./cmd/muhan -count=1) PASS
(cd server && go vet ./cmd/muhan) PASS
```

이 CLI는 한 snapshot 묶음의 재현 가능한 전달 경계만 완성한다. 운영 Supabase 승인,
legacy raw player 자동 수집·대량 대조/복구, 전체 command parity, 실제 브라우저/IME/mobile,
WSS/Ingress·testnet 승격은 여전히 미완료다. `src/frp.new`와 dirty worktree는 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (PostgreSQL PlayerSnapshotV1 import)

`Postgres.ImportPlayerSnapshot`와 `mud_go.character_imports` schema를 추가했다. 호출자가
검토한 raw CDTO, account name/password hash, exact world player ID, deterministic item-ID
manifest를 제출하면, snapshot decode/canonical digest·legacy name normalization·State
offline admission을 거친 뒤 account/linked character/world JSON/evidence/command receipt를
하나의 PostgreSQL transaction으로 저장한다. raw payload나 password는 receipt/evidence에
넣지 않고 source/canonical SHA-256, source octets, inventory node count, imported revision만
보존한다. same command ID 재시도는 저장 응답만 재생하며, request/hash·revision·writer
fencing·중복 이름/ID·item manifest mismatch에서 fail-closed한다.

`scripts/run-go-player-snapshot-import-local.sh --allow-disposable`는 고유 loopback
ARM64 `postgres:17-alpine`만 만들고 종료 시 자기 컨테이너만 제거한다.

검증:

```text
(cd server && MUHAN_PLAYER_SNAPSHOT_IMPORT_TEST_DATABASE_URL='postgresql://...' go test -race ./internal/storage -run '^TestPostgres.*PlayerSnapshot' -count=1 -v) PASS
bash scripts/run-go-player-snapshot-import-local.sh --allow-disposable PASS
```

이 단계는 한 snapshot의 승인·저장·재생 경계까지만 완료했다. 운영 Supabase schema
승인/백업·대량 raw player 수집/manifest 생성·전체 데이터 대조·account recovery/브라우저
플레이·전체 command parity·WSS/Ingress·testnet 전환은 미완료다. `src/frp.new`와 예전
dirty worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (PlayerSnapshotV1 read-only inspection ledger)

`-inspect-player-snapshot-dir`/`-inspect-player-snapshot-world`와 dry-run을 추가했다.
정확한 `0700` root 및 deterministic lexical walk를 사용해 private 파일은 CDTO canonical
검증·source SHA-256·octets·inventory node 수를, malformed/public/symlink 파일은 raw 없이
quarantine reason을 만든다. DB에는 `mud_go.player_snapshot_import_ledger`의 metadata만
idempotent하게 기록하며, 동일 path+digest의 다른 metadata는 conflict, 같은 path의 변경
digest는 새 evidence revision으로 남긴다. payload·비밀번호·자동 identity claim은 없다.

검증:

```text
(cd server && go test -race ./cmd/muhan -run 'PlayerSnapshot(Inspection|Manifest)' -count=1) PASS
(cd server && go test -race ./internal/storage -run 'PlayerSnapshotInspection' -count=1) PASS
(cd server && go vet ./cmd/muhan ./internal/storage) PASS
bash scripts/run-go-player-snapshot-import-local.sh --allow-disposable PASS (PG17 ledger idempotency 포함)
```

이 수집기는 C raw player 파일을 자동 해석하거나 계정을 claim하지 않는다. 운영 raw 수집·
대량 대조·복구·Supabase 승인과 전체 게임 인수는 미완료이며, `src/frp.new`와 dirty
worktree는 계속 보호한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (Go PlayerSnapshotV1 이관 경계)

`server/internal/world/player_snapshot_v1.go`에 C/Rust와 동일한 pointer-free CDTO
`PlayerSnapshotV1` decoder/encoder를 추가했다. magic·wire version·kind·payload 한도,
필드 순서/타입/길이·SHA-256·trailing/truncation을 검증하고, player type·HP/MP·daily
상한과 preorder object graph의 parent/sibling/depth/node·fixed-string·shots 규칙을
fail-closed로 적용한다. `InspectPlayerSnapshotV1`은 원본 바이트와 SHA-256 evidence를
복사 보존하며, C/Rust/하위 프로세스를 호출하지 않는다.

`ToLegacyMonster`/`ToItemCollection`/`ToPlayerState`는 명시적 변환 단계로 분리했다.
legacy int32 범위를 벗어난 i64 값, 잘못된 text, 중복/누락 item ID는 거부하며, 빈
inventory도 nonnil canonical `ItemCollection` marker로 변환한다. `State.AdmitPlayerSnapshot`
은 operator가 제공한 명시적 world player ID만 사용하고 이름에서 ID를 추측하지 않으며,
오프라인 상태로만 clone에 삽입한다. 이름은 원작과 같은 `CanonicalName` 정규화를 적용하고
기존 ID/이름 충돌·방 부재·allocator 실패 시 원본 State를 변경하지 않는다.

검증:

```text
(cd server && go test -race ./internal/world -run 'PlayerSnapshotV1|AdmitPlayerSnapshot' -count=1) PASS
(cd server && go test ./... -count=1) 실행: world strict room corpus의 기존 invalid 63건으로 전체 명령 FAIL; 나머지 패키지는 PASS
(cd server && go test -race ./internal/session ./internal/transport -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
```

정상 C fixture 6종은 Go 재인코딩 결과가 원본과 byte-for-byte 일치하며, rich/minimal/
persisted graph와 one-item/tree inventory를 포함한다. 이 경계는 이관 artifact 검증과
순수 State admission까지만 완료한 것이다. 아직 raw legacy player 파일 수집/운영 승인,
PostgreSQL import receipt/evidence table, account link orchestration, full C output parity,
실제 Supabase·browser/IME/mobile·WSS/Ingress·testnet 배포는 남아 있다. `src/frp.new`와
예전 dirty worktree는 수정·stage하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (ISSUE 카탈로그 파서·작업공간 정리)

`3a05962`에서 원작 `post/ISSUE` raw 바이트를 서버 소유 `VoteCatalog`로 파싱하는
경계를 추가했다. UTF-8/EUC-KR·CRLF·최종 개행 변형, 안건/선택지 수·크기, trailing/
truncation/malformed 입력을 fail-closed로 검증하고 raw bytes/SHA-256/evidence를 보존한다.
`cmd/muhan`은 `-vote-issue-file` 또는 `MUD_VOTE_ISSUE_FILE`을 `-world` 모드에서만 받아
catalog를 주입하며 backup/restore/seed 모드와 섞이지 않는다. `4fd7a8b`는 alias 통합
테스트의 시계를 고정해 integration gate를 결정론적으로 만들었다.

검증 결과:

```text
(cd server && go test -race ./internal/world -run 'VoteCatalog|VoteIssue' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
(cd server && scripts/run-go-validation.sh integration) PASS
git diff --check PASS
```

MUD Orca에는 메인 워크트리만 등록되고 하위 터미널은 0개다. 대소문자 충돌만 있던
예전 Git worktree 22개는 삭제했으며, `src/frp.new`, Rust 수정, 삭제/미추적 파일이
남은 8개는 유실 방지를 위해 보존했다. 해당 브랜치 ref와 다른 저장소의 작업공간은
삭제하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (투표 이관·연결 로컬 continuation 통합)

이번 배치에서 완료된 하위 에이전트 세션은 모두 종료했다. 메인 워크트리에는
`28938b5` 투표 legacy importer와 canonical receipt continuation, `3a8bb8d` `Ballots`/
append-only `History` 원장, `ccd149c` 메일·게시판 격리 PostgreSQL receipt/replay 회귀,
`0c6f7d6` 중앙 xterm 모바일 IME·focus/viewport 경계를 보존했다. `src/frp.new`는
사용자 변경으로 계속 미수정·미stage다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Vote|LegacyVote' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
(cd server && scripts/run-go-validation.sh integration) PASS
(cd web && npm test) PASS (51 tests)
(cd web && npm run typecheck) PASS
git diff --check PASS
```

메일·게시판 실제 ARM64 `postgres:17-alpine` 실행은 에이전트가 disposable DB에서
4개 시나리오를 통과시켰다. 기본 환경에서는 해당 테스트가 명시적 DB URL이 없으면
skip된다. 투표는 명시적 legacy importer·원장 연결·raw ISSUE parser 및 서버 주입까지
완료했지만, 운영 Supabase 대규모 이관 실행·전체 기능·실제 OS IME/mobile·WSS/Ingress·
testnet 인수는 아직 남아 있다.

Orca는 MUD 메인 워크트리만 등록하고 하위 터미널은 0개다. 예전 `/Users/jjangg96/orca`
연결 worktree 중 대소문자 충돌만 남은 22개는 정리했고, `src/frp.new`·Rust·삭제/미추적
변경이 있는 8개와 브랜치 ref는 보존했다. 다른 저장소의 Orca 터미널은 정리 범위가
아니므로 건드리지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (투표 source gate·ballot fail-closed, 역사적 초기 단계)

`command11.c:vote`의 원본 게이트를 Go에 연결했다. `투표` exact alias와 server-owned
`VoteCatalog`(ISSUE의 안건 수·질문·최대 7개 선택지)를 검증하고, 21세 미만 일반
캐릭터·비투표소·음수 시각·잘못된/누락 catalog를 거절한다. `VoteContinuation`은
`vote_cmnd`의 y/n 및 a..g 입력을 연결 로컬에서만 결정론적으로 진행하며 snapshot이나
receipt에 사용자 선택을 저장하지 않는다. 현재 `State`에 `player/vote/<name>_v`에
해당하는 canonical ballot/history가 없으므로 실제 투표 쓰기(`case 3`)는 중복 투표와
유실을 막기 위해 receipt 전에 명시적으로 fail-closed한다. terminal 요청이 issue나
ballot ID를 주입할 수 없고, 이미 저장된 command ID replay는 reducer보다 먼저 재생된다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Vote|Reply|DirectMessage|ParseCommand' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
(cd server && scripts/run-go-validation.sh integration) PASS
git diff --check PASS
```

실제 PostgreSQL 투표 저장은 canonical ballot schema가 확정될 때까지 실행 대상이
아니며, 운영 Supabase·전체 vote continuation/파일 이관·브라우저/IME/mobile·WSS/Ingress·
testnet은 미완료다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (대답·`/` reply receipt 경계)

`command12.c:resend`의 `대답`/`/`을 연결 로컬 마지막 수신자 경계로 연결했다.
직접 메시지 event가 수신자의 atomic `talksend` 대체 상태에 정확한 sender ID/name을
기록하고, 답장 입력은 클라이언트가 대상 ID를 보낼 수 없도록 그 값을 서버에서만
`ExecuteGame` request에 주입한다. `PlanDirectMessage`의 visibility·ignore·silent·
UTF-8/255바이트 규칙과 동일한 reducer를 재사용하며, 대상이 stale/logged-out이면
receipt 전에 fail-closed한다. 최초 commit 뒤에만 event를 전송하고 receipt replay에서는
재전송하지 않는다.

검증 결과:

```text
(cd server && go test -race ./internal/session ./internal/transport -run 'Reply|DirectMessage|ParseCommand' -count=1) PASS
(cd server && go vet ./internal/session ./internal/transport) PASS
git diff --check PASS
```

실제 PostgreSQL reply 저장/replay 테스트는 `MUHAN_SERVICE_COMMAND_TEST_DATABASE_URL`
설정 시 실행하도록 추가했지만 이번 로컬 실행에서는 별도 DB를 재기동하지 않았다.
전체 resend continuation parity·legacy descriptor 순서·운영 Supabase·브라우저/IME/mobile·
WSS/Ingress·testnet 인수는 남아 있다. 현재 변경은 아직 메인 브랜치에 push하지 않았다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (배우자 대화 출력·ANSI 경계)

`command11.c:m_send`의 suffix 입력(`<메시지> 사랑말`)을 Go에 연결했다. `PMARRI`,
`m<배우자>` `key[2]`, 온라인 canonical 상호 배우자와 255바이트 UTF-8 메시지를
검증하고, C `crt_str`의 PINVIS/PDMINV/PDINVI 및 PANSIC/PBRIGH 색상·`%j` 조사
규칙을 proposal에 렌더링해 actor 응답과 배우자 event를 하나의 `ExecuteGame` receipt로
저장한다. 배우자 event는 최초 commit 뒤 정확한 durable ID/name에만 전달하며 replay에서는
재전송하지 않는다. 구현 커밋은 `77bc622`다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Divorce|MarriageSend|MarriageFollowup|Marriage|ParseCommand' -count=1) PASS
(cd server && go test -race ./internal/session ./internal/transport -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
(cd server && MUHAN_DIVORCE_TEST_DATABASE_URL='postgresql://...' go test -race ./internal/session -run '^TestPostgres(DivorceRequestAccept|MarriageSend)' -count=1 -v) PASS (ARM64 postgres:17-alpine)
git diff --check PASS
```

현재 `사랑말`은 원작 suffix 형태와 visibility/ANSI 출력까지 연결됐지만, 전체
descriptor/title parity·legacy offline 배우자 `load_ply` 구분·전체 social 명령,
운영 Supabase·브라우저/IME/mobile·WSS/Ingress·testnet 인수는 남아 있다. world 전체
race의 기존 strict room corpus 63건 예외도 계속 기록한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (이혼 후속 영수증·전송 경계)

`command11.c:divorce`의 온라인 canonical 경계를 Go parser→session→WorldConnector에
연결했다. `이혼`은 결혼 신청 취소·미혼 no-op·이혼 신청 취소·온라인 배우자 신청·상호
수락의 source 순서를 유지하고, `PMARRI`/`PRDMAR`/`PRDDIV`와 `key[2]`를 한
`ExecuteGame` receipt로 원자 저장한다. 배우자 알림과 수락 전역 공지는 첫 commit 뒤에만
fan-out하며, 전역 공지는 원작 `broadcast()`의 `PNOBRD`를 존중한다. 동일 command ID
재시도는 저장된 응답만 재생한다. 구현 파일은 `marriage_followup.go`,
`marriage_followup_command.go`와 대응 session/transport TDD다.

`command11.c:m_send`의 기혼·배우자·UTF-8/255바이트 입력 검증과 parser 경계도 추가했지만,
원작 `%C/%M/%j` descriptor와 `PLECHO` exact echo formatter가 아직 Go canonical 상태에
없어 성공 전이는 명시적으로 fail-closed한다. 추측한 출력이나 알림은 저장하지 않는다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Divorce|MarriageSend|MarriageFollowup|Marriage|ParseCommand' -count=1) PASS
(cd server && go test -race ./internal/session ./internal/transport -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
(cd server && MUHAN_DIVORCE_TEST_DATABASE_URL='postgresql://...' go test -race ./internal/session -run '^TestPostgresDivorceRequestAcceptPersistsAndReplays$' -count=1 -v) PASS (ARM64 postgres:17-alpine)
git diff --check PASS
```

직접 world 전체 race는 기존 strict room corpus의 63개 invalid EUC-KR/trailing-data
예외로 실패한다(`TestRoomBodyCorpus`); 이번 결혼 후속 회귀와 무관한 승격 조건이다.
배우자 offline/missing의 legacy `load_ply` 구분, 실제 Supabase 운영 연결, m_send 출력
formatter, 전체 social parity, 브라우저/IME/mobile, WSS/Ingress와 testnet 승격은 남아 있다.
테스트 컨테이너는 이번 실행에서 만든 것만 종료·삭제했으며 `src/frp.new`와 예전 dirty
worktree는 보존한다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (결혼 신청·수락 영수증)

`command11.c:marriage`의 canonical하게 증명 가능한 경계를 Go에 연결했다.
`결혼 <이름>`/`<이름> 결혼`은 결혼식장·25세·온라인 canonical 대상·시야·성별·중복
신청 게이트를 검증하고, 신청/취소/상호 수락을 하나의 `ExecuteGame` receipt로 원자
저장한다. 원작의 `PRDMAR`/`PMARRI`와 `key[2]` 배우자 값을 기존 player snapshot에
보존하며, 수락 시 배우자 알림과 전체 접속자 결혼 공지는 첫 커밋 뒤에만 전달한다.
동일 command ID 재시도는 저장된 결과만 재생한다. 구현 커밋은 `22d1e8e`다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Marriage|ParseCommand' -count=1) PASS
(cd server && go test -race ./internal/session -run '^TestPostgresMarriageRequestAcceptPersistsAndReplays$' -count=1 -v) PASS (ARM64 postgres:17-alpine)
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
git diff --check PASS
```

영향 패키지 전체 race 실행에서 world의 기존 strict room corpus 63건 예외가 재현되어
실패했지만 session/transport는 통과했다. 이 변경은 신청·취소·수락만 포함하며 이혼
(`divorce`)과 배우자 대화(`m_send`), 전체 사회/공지 출력 parity는 미완료다. 실제
Supabase 운영 연결, 브라우저·IME/mobile, WSS/Ingress와 testnet 승격도 남아 있다.

## 최신 구현·검증 체크포인트 — 2026-09-10 (`정보` 후속 페이지 영수증)

`command4.c:info_2`의 `[엔터]` 후속 페이지를 연결 로컬 snapshot 조회에서
`ExecuteInfoContinuation` 영수증 경계로 올렸다. 주문·현주문·임무 projection은 현재
canonical snapshot을 한 번 읽어 response에 고정하고, world state bytes는 그대로
보존한다. 커밋/응답이 불확실하면 connection-local command ID를 유지해 같은 입력의
재시도에서 receipt replay만 수행하며 reducer나 projection을 다시 실행하지 않는다.
`.` 취소는 기존처럼 영수증 없이 처리하고, 실패한 커밋 뒤 pending 상태와 command ID도
검증한다. 구현 커밋은 `251b500`이다.

검증 결과:

```text
(cd server && go test -race ./internal/session ./internal/transport -run 'Info(Continuation|Line)|WorldConnectorInfo' -count=1) PASS
(cd server && go test -race ./internal/session ./internal/transport -count=1) PASS
(cd server && go vet ./internal/session ./internal/transport) PASS
isolated postgres:17-alpine ARM64 + TestPostgresInfoCommandPersistsAndReplaysWithoutNewStateVersion PASS
git diff --check PASS
```

실제 PostgreSQL 테스트는 이 실행에서 만든 컨테이너만 종료·삭제했다. 전체 info
페이지의 C 출력/ANSI parity, 전체 spell effect, 실제 브라우저·IME/mobile, WSS/Ingress,
strict room corpus 63건과 testnet 배포는 여전히 미완료다. `src/frp.new`와 예전 dirty
worktree는 보존한다.

## 최신 G4 검증 체크포인트 — 2026-09-10 (Go PostgreSQL 백업·복구)

Go `mud_go` 스키마에 대해 `scripts/run-go-backup-restore-local.sh`를 추가했다.
격리된 ARM64 `postgres:17-alpine`에서 source DB에 revision 2와 receipt 2개를 기록한
뒤 PostgreSQL custom-format `pg_dump`/`pg_restore`로 target DB를 복원하고, receipt
replay·request conflict·writer epoch fencing·복원 후 revision 3 쓰기를 확인한다.
`TestPostgresWorldBackupRestoreFencesReceipts`는 receipt가 있는 restore의 expected
revision 경계와 Force 복원 후 구 writer 거부도 검증한다. 구현 커밋은 `357220b`다.

검증:

```text
bash scripts/run-go-backup-restore-local.sh --allow-disposable PASS
(cd server && go test -race ./internal/storage -run 'Test(WorldBackup|PostgresWorldBackup)' -count=1) PASS
(cd server && go vet ./internal/storage) PASS
bash -n scripts/run-go-backup-restore-local.sh && git diff --check PASS
```

스크립트는 자신이 만든 컨테이너와 임시 archive만 제거했다. 운영 보관·암호화·키 회전·
retention, PostgreSQL 장애/PITR, 전체 레거시 중복·유실 대조, Supabase 운영 복원,
WSS/Ingress·testnet은 미완료다.

## 최신 검증 체크포인트 — 2026-09-10 (실제 PG·브라우저 모바일 viewport)

실제 PostgreSQL 17 ARM64, Go `-race`, Chromium을 연결한
`bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`에
모바일 viewport 회귀를 추가해 **3 passed (17.4s)**를 확인했다. 신규 terminal-only 가입·
재로그인, 기존 `LinkExistingWorldCharacter` 캐릭터의 단일 입장·중복 세션 거부, iPhone
크기 viewport에서 resize 후 xterm focus 유지와 `봐` 제출을 검증했다. 테스트 소유 DB
컨테이너는 종료 후 제거했다. 이는 모바일 viewport 에뮬레이션과 synthetic IME 경계의
증거이며 실제 모바일 OS 키보드/IME, 운영 WSS/Ingress 및 전체 기능 인수는 미완료다.

## 최신 검증 체크포인트 — 2026-09-10 (bounded PostgreSQL receipt/replay)

고유 loopback 포트와 임시 데이터 디렉터리를 사용하는 ARM64 `postgres:17-alpine`
컨테이너에서 `MUHAN_BOUNDED_LANES_TEST_DATABASE_URL`을 설정하고
`go test -race ./internal/session -run '^TestPostgresBoundedLanesPersistAndReplay$' -count=1 -v`를
실행했다. alias·burn·study·family-mutation 네 케이스 모두 저장 후 같은 command ID
replay까지 **PASS**했으며 테스트 소유 컨테이너는 종료 후 제거했다. 다른 Docker 자원은
건드리지 않았다. 이는 bounded lane 증거이며 전체 PostgreSQL 이관·전체 명령·운영
Supabase 검증을 의미하지 않는다.

## 최신 검증 체크포인트 — 2026-09-10 (실제 PG·브라우저 기존 캐릭터 경로)

격리된 ARM64 `postgres:17-alpine` 컨테이너에서 Go `-race` 서버, xterm 브라우저와
PostgreSQL을 함께 기동해 `bash scripts/run-go-process-postgres-browser-e2e-local.sh
--allow-disposable`를 실행했다. 가입→월드 입장→재로그인 시나리오와, 명시적
`LinkExistingWorldCharacter` 이관 캐릭터의 로그인·단일 입장·중복 세션 거부 시나리오가
각각 통과해 **2 passed (16.2s)**였다. 암호가 터미널 출력에 나타나지 않는 것도 확인했고,
래퍼가 자신이 만든 PostgreSQL 컨테이너만 종료했다. 실제 OS IME 조합기·모바일 키보드,
운영 WSS/Ingress 및 전체 명령 인수는 이 테스트의 범위를 넘으므로 미완료로 유지한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-10 (패거리 가입/탈퇴 세션·전송 연결)

`command11.c:family`·`add_family`·`out_family`의 보수적인 실행 경계를 메인
parser→session→WorldConnector에 연결했다. `패거리가입 <패거리명>`은 immutable
`FamilyCatalog`의 exact 이름과 온라인 canonical boss/PFAMIL·PFMBOS를 증명한 뒤
PRDFML 신청을 하나의 `ExecuteGame` receipt로 저장하고, `패거리탈퇴`는 pending 신청
취소만 저장한다. 동일 command ID 재시도는 저장된 응답을 재생하며 reducer를 다시
실행하지 않는다. bare `패거리가입`의 목록/선택/확인 continuation, `가입허가 [대상]`,
활동 회원 탈퇴는 family fee/member ledger가 현재 canonical State에 없으므로
영수증 전에 fail-closed한다. `가입허가` 대상 이름은 권한 증명에 사용하지 않는다.

메인 통합 파일은 `server/internal/session/command_parser.go`와
`server/internal/transport/world_connector.go`이며, 세션 어댑터·TDD는
`family_mutation_command.go`와 대응 테스트, transport 회귀는
`world_connector_family_mutation_test.go`에 있다. 검증:

```text
(cd server && go test -race ./internal/session ./internal/transport -run 'FamilyMutation|FamilyTalk|FamilyStatus|ParseCommand' -count=1) PASS
(cd server && go vet ./internal/session ./internal/transport) PASS
git diff --check PASS
```

통합 영속성 harness `TestPostgresBoundedLanesPersistAndReplay`에도 동일 family
신청 케이스와 replay 검증을 포함했다. 이번 로컬 실행은 전용
`MUHAN_BOUNDED_LANES_TEST_DATABASE_URL`이 없어 **SKIP**되었으며, 실제 PostgreSQL
증거로 기록하지 않는다.

전체 명령/interactive continuation, 실제 PostgreSQL·브라우저·IME/mobile, strict room
corpus 63건, ARM64/release/WSS/Ingress/testnet 인수는 여전히 미완료다. `src/frp.new`와
예전 dirty worktree는 보존한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-10 (정리 확인 + 패거리말·주문·가입 경계)

정리 상태를 다시 확인했다. Orca가 관리하는 worktree 목록에는 주 worktree만 남아
있고, 예전 `orca/workspaces` 경로의 Git worktree 30개는 보존했다. 그중 22개는
`objmon/Celduin_sign` 대소문자 충돌만 남았고, 나머지 8개에는 `src/frp.new`·삭제된
파일·Rust 변경·미추적 파일이 섞여 있다. 이 변경을 확인 없이 버리면 원본 파일을
잃을 수 있어 삭제하지 않았다. 직접 하위 레인은 Luna max로
소유 파일을 마친 뒤 중단/종료했고, 현재 남은 작업은 주 worktree에서만 통합한다.

이번 후속은 다음 경계를 추가했다.

- `패거리말`/`]`: `PFAMIL`·`PSILNC`와 server-owned `FamilyCatalog`를 검증하는
  read-only family event receipt를 만들고, room order 후 sorted residual online ID로
  deterministic fan-out한다. 최초 commit에서만 actor를 포함한 recipient 이벤트를
  전송하고 replay에서는 재방송하지 않는다.
- 패거리 가입 신청·신청 취소의 canonical online boss/identity와 PFAMIL·PRDFML·
  PFMBOS 상태를 원자 proposal로 검증한다. 승인과 활동 회원 탈퇴는 C의
  `family_gold`·`family_member_<n>` 원장이 아직 Go State에 없어 명시적으로
  fail-closed한다.
- `주문` 목록의 `spllist` 56개와 활성 `ospell` 20개를 source-backed catalog로
  검증하고, 이름 정렬 read-only receipt/replay를 연결했다. offensive/targeted/map/
  미확인 주문은 실행하지 않고 fail-closed한다.

검증:

```text
(cd server && go test -race ./internal/world -run 'FamilyTalk|FamilyMutation|SpellCatalog|SpellList' -count=1) PASS
(cd server && go test -race ./internal/session ./internal/transport -run 'FamilyTalk|SpellList|ParseCommand' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
git diff --check PASS
```

전체 world package는 기존 strict room corpus의 알려진 63개 예외로 실패하며, 이는
이번 변경의 회귀가 아니다. ARM64/main, 실제 PostgreSQL, 브라우저/IME·모바일,
release matrix, WSS/Ingress 및 testnet 배포는 cadence 승격 경계에서만 실행한다.
`src/frp.new`는 계속 사용자 소유 dirty 변경으로 수정·stage하지 않는다.

## 최신 오케스트레이션 체크포인트 — 2026-09-10 (정리·스크롤·초대·패거리 상태)

이전 Orca 정리 요청에 따라 무변경(clean) worktree 108개를 일반 worktree 제거로
정리하고, 변경이 남은 30개는 보존했다. 현재 Orca MUD 저장소에는 주 작업 worktree만
남아 있으며, 제거된 Luna 레인의 브랜치 참조는 보존을 위해 다시 생성했다. 현재 직접
관리 중인 하위 레인은 종료되었고, `src/frp.new` 사용자 변경은 계속 수정·stage하지
않는다.

이번 Go 기능 batch는 파일 소유권이 겹치지 않는 두 Luna max 레인을 병렬 처리한 뒤
메인 세션에서 parser·WorldConnector를 조립했다.

- `읽어 <두루마리>`: `magic1.c:readscroll`의 canonical inventory root→ready 슬롯
  선택, blindness/type/charge/level/alignment/class/no-magic/cooldown 게이트와
  `drinkSpells` 기반 self-target 효과를 `PlanReadScroll`/`ApplyReadScroll` receipt로
  연결했다. spell-fail/effect RNG를 proposal에 고정하고, 성공·실패 소비, PHIDDN 해제,
  LT_READS 기록, alignment 거부 시 방 이동을 원자 적용한다. targeted/offensive/map/
  미확인 효과와 legacy inventory는 영수증 전에 fail-closed한다. `읽어 게시판 <번호>`는
  기존 게시판 parser로 유지한다.
- `초대`: `command12.c:invite`의 RONMAR·DL_MARRI 권한, exact canonical online
  identity, self/ambiguous/missing/invisible 차단, 10명 ordered toggle/list와 nil
  invitation migration 경계를 `PlanPropertyInvite`/`ApplyPropertyInvite`로 연결했다.
  저장된 초대는 ID 순서를 보존하고 이름은 canonical player에서만 렌더링하며, 마지막
  제거는 key를 삭제한다.
- `패거리누구`/`패거리원`/`모든패거리`: `PFAMIL`/`PRDFML`/`PFMBOS`, DL_EXPND family
  ID, visibility·blindness와 server-owned `FamilyCatalog`를 사용한 deterministic
  read-only projection을 추가했다. catalog가 없거나 canonical identity가 해소되지
  않으면 추측하지 않고 닫는다. roster는 room order 후 sorted residual online ID이며,
  pending 신청은 `(-)`로 표시한다.
- 세 명령군 모두 central parser→session `ExecuteGame`→PostgreSQL receipt/replay→
  WebSocket 응답 경계를 통과하고, 스크롤 성공의 room event는 최초 commit에서만
  fan-out한다.

검증 결과:

```text
(cd server && go test -race ./internal/world -run 'FamilyStatus|PropertyInvite|ReadScroll' -count=1) PASS
(cd server && go test -race ./internal/session -run 'ParseCommandClassifies|ReadScroll|PropertyInvite|Family' -count=1) PASS
(cd server && go test -race ./internal/transport -run 'ReadScroll|PropertyInvite|FamilyStatus' -count=1) PASS
scripts/run-go-validation.sh fast PASS
scripts/run-go-validation.sh integration PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
git diff --check PASS
```

전체 world package를 직접 실행한 명령은 원본 strict room corpus의 알려진 63개 예외로
실패했으며, 이는 이번 기능 회귀가 아니라 기존 `TestRoomBodyCorpus` 승격 조건이다.
ARM64 main build, 실제 PostgreSQL/browser·IME/mobile, release matrix, WSS/Ingress와
testnet 배포는 cadence 정책에 따라 이번 기능 레인에서 반복하지 않았다. 전체 C
prefix/key/ANSI parity, strict corpus 63건, family mutation/chat/war와 scroll의
offensive spell·full item migration은 여전히 전체 인수 조건이다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (터미널 암호·Go 게이트웨이 좌표·검증 비용 감사)

이번 배치는 파일 소유권이 겹치지 않는 두 Luna max 레인을 병렬 처리한 뒤 메인 세션에서
전송 경계만 한 번 조립했다.

- **터미널 `암호`**: 게임 연결 안에서 현재 암호→새 암호→확인 순서를 소비하는
  connection-local continuation을 연결했다. 계정 credential store는 `PasswordStore`로
  별도 주입하고, expected/replacement bcrypt hash 조건부 UPDATE와 idempotent retry를
  사용한다. 암호 입력은 history·alias·world receipt·이벤트·로그에 들어가지 않으며,
  WebSocket `secret` 플래그가 다음 한 줄에만 전달된다. 연결 종료·취소 시 보류 hash를
  지운다. 저장소 계정 이름은 world/player ID와 분리해 인증 결과에 보존한다.
- **xterm 게이트웨이 좌표**: 웹 루트가 런타임의 `MUD_GO_GATEWAY_URL`을 우선하고
  `MUD_GATEWAY_URL`을 명시적 fallback으로 사용한다. ws/wss·HTTPS mixed-content·누락/
  malformed 주소 검증을 순수 helper로 분리했으며, 기존 Supabase 웹 가입 화면을 루트
  경로의 필수 단계로 되살리지 않았다.
- **중복 검증 전수 감사**: workflow/pre-push/validation script/matrix를 다시 대조했다.
  CI는 `workflow_dispatch`만 사용하고, 기능 레인은 영향 패키지 race, 조립 batch는
  `integration` 전체 Go race/vet/diff 1회, 기본 브랜치 `main`에서만 Linux ARM64
  cross-build 1회, 승인된 `release`에서만 PostgreSQL·브라우저·x64/Windows/macOS
  호환성 matrix를 실행한다. release의 migration 2회 적용과 command replay는 재실행
  안전성 증거인 의도된 반복이라 유지하며, 기능 레인마다 ARM64/DB/browser를 반복하는
  호출은 발견되지 않았다.

검증 결과:

```text
(cd server && go test -race -count=1 ./internal/session ./internal/storage ./internal/transport) PASS
scripts/run-go-validation.sh integration PASS
(cd web && npm test) PASS (49 tests)
(cd web && npm run typecheck) PASS
python3 tests/unit/local_first_policy_test.py PASS
python3 tests/unit/self_hosted_ci_policy_test.py PASS
bash -n scripts/run-go-validation.sh scripts/check-local-before-push.sh .githooks/pre-push PASS
git diff --check PASS
```

실제 PostgreSQL·ARM64 main cross-build·browser/IME 실기기·release matrix·WSS/Ingress와
testnet 배포는 이 기능 배치에서 반복하지 않았다. 실제 PG 암호 저장 경로와 운영 도메인은
해당 승격 경계에서 별도 증거를 추가해야 한다. `src/frp.new`는 사용자 소유 dirty 변경으로
계속 보존하며 수정·stage하지 않는다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (검증 cadence 전수 감사 + 시간·수련·선택)

검증 호출 그래프를 다시 전수 대조했다. `.github/workflows/ci.yml`는
`workflow_dispatch`만 사용하고, 기능 레인은 `fast`(영향 Go 패키지 race), 조립 batch는
`integration`(전체 Go race/vet/diff 1회), 기본 브랜치 병합은 `main`(integration + Linux
ARM64 cross-build 1회), 승인된 기본 브랜치의 `release`만 PostgreSQL·브라우저·
x64/Windows/macOS 호환성 matrix를 실행한다. pre-push는 push 전체를 다시 빌드하지 않고
변경된 migration/stack 계약만 검사한다. release 내부의 migration 2회 실행은 CI 재실행
안전성을 검증하는 의도된 replay이며, 같은 검사가 기능 레인과 중복 호출되는 경로는
발견되지 않았다. 따라서 이번 기능 배치에서 ARM64·실제 DB·브라우저·호환성 검사를
반복하지 않았다.

파일 소유권이 겹치지 않는 Luna max 세 레인을 병렬 완료하고 메인 세션에서 parser와
`WorldConnector`를 한 번 조립했다.

- `시간`: `command8.c:prt_time`의 게임 시각(`now % 24`)과 고정 PST wall-clock을
  request에 묶은 read-only receipt/replay를 추가했다. connector는 기존 `Clock`의
  이미 투영된 game hour를 전달해 `0시→12시` 출력 호환성을 보존한다.
- `수련`: `command7.c:train`의 RTRAIN/class-bit·blind/caretaker gate, 경험치·금화
  다중 레벨 상승, PUPDMG 해제와 INVINCIBLE/CARETAKER 전환을 snapshot-bound atomic
  reducer로 연결했다. family edit와 global broadcast 수신자 원장이 없는 경우
  영수증 전에 fail-closed하며 `BroadcastPending`으로 남긴다.
- `선택 <NPC> [occurrence]`: `command10.c:selection`의 canonical same-room NPC와
  MPURIT merchant catalog를 사용해 deterministic stock 목록을 read-only receipt로
  반환한다. legacy `Carry` fallback·prefix 추측·catalog 누락은 허용하지 않는다.

이번 변경 검증은 `scripts/run-go-validation.sh fast`, 조립 후
`scripts/run-go-validation.sh integration`, 정책·shell·migration coverage 및
`git diff --check`를 통과했다. strict room corpus 63건, ARM64 cross-build, 실제
PostgreSQL/browser·release matrix, IME/mobile 실기기와 testnet 배포는 cadence/승격
경계 밖이라 실행하지 않았다. `src/frp.new`는 사용자 소유 dirty 변경으로 계속
수정·stage·되돌리지 않는다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (줘·독살포·상태 + 검증 비용 경계)

직접 관리한 Luna max 세 레인을 병렬 완료한 뒤 메인 세션에서 공용 parser와
`WorldConnector`를 통합했다. 코드 커밋은 `17c0619` (`기능: 물품전달·독살포·적상태 경계 연결`)이다.

- `줘`: 원작 suffix 형식 `<물건|금액냥> <대상> 줘`를 canonical same-room player와
  `ItemCollection` root subtree/gold 원자 전이로 연결했다. 대상 private 응답과 방 관찰자
  응답을 분리하고, NPC 수령·legacy inventory·quest/event/nested 보호·capacity/overflow는
  영수증 전에 fail-closed한다.
- `독살포`: 자객/무적 권한, exact case-insensitive canonical NPC, 시야·은신 해제·쿨다운·
  `MUNKIL`, deterministic RNG/poison flag/HP/timer/enemy projection을 receipt에 연결했다.
  적대 관계·치명 사망·도주 후속이 현재 원자 조합되지 않으면 RNG와 커밋 전에 거절하며,
  최초 성공의 방 이벤트만 전달하고 replay에서는 재방송하지 않는다.
- `상태`: 같은 방 canonical NPC의 `display_status` 15칸 의미 바와 blindness/visibility를
  read-only receipt로 연결했다. player/legacy room monster fallback과 prefix/occurrence
  추측은 허용하지 않는다.

검증 결과: 영향 패키지 race, transport 회귀, `go vet`,
`scripts/run-go-validation.sh fast`, `scripts/run-go-validation.sh integration`,
`git diff --check`가 통과했다. ARM64 cross-build는 기본 브랜치 `main`에서 한 번,
실제 PostgreSQL·브라우저·x64/Windows/macOS 호환 matrix는 승인된 `release`에서 한 번만
실행하도록 분리했으므로 이번 기능 레인에서는 반복하지 않았다. `src/frp.new`는 사용자
소유 dirty 변경으로 수정·stage하지 않았다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (NPC phase ordering + 저장 명령)

이번 병렬 Luna max 후속은 세 레인을 파일 소유권으로 분리해 통합했다.

- NPC maintenance/resource/combat scheduler를 하나의 `NPCWorldScheduler`로 묶었다.
  매 wake-up은 maintenance → resource → combat 순서이며 각 phase의 원래 cadence는
  유지한다(최소 cadence로 wake한 뒤 durable slot suppression 적용). 중간 phase 오류는
  뒤 phase를 실행하지 않고 다음 cadence에 같은 pending request를 재시도한다. 프로세스
  시작/종료 로그와 drain도 이 단일 worker를 기준으로 바꿨다.
- 원작 `저장`(`src/global.c` cmdno 52 / `command8.c:savegame`)을 Go의 canonical
  state가 이미 receipt로 저장된다는 권위 모델에 맞춘 no-state durable receipt로 연결했다.
  xterm에서 `저장`과 명시적 호환 alias `save`를 dispatch하며 offline·인자 추가·제어문자는
  fail-closed하고 동일 command ID replay는 commit 1회/응답 불변을 보장한다.
- `아파트_수위_아저씨-127` talk 자산은 manifest와 Git blob이 byte-for-byte 동일하지만
  CP949 offset 216의 standalone `0xBA`로 strict decode가 실패한다. 정확한 원본을 찾지
  못했으므로 임의 보정하지 않고 provenance와 fail-closed 회귀를 추가했다.

이 batch의 새 검증은 scheduler/session/connector targeted race와 `cmd/muhan` 프로세스
테스트(격리 DB 미설정 시 의도적 skip)다. 전체 integration/ARM64/release/browser는
앞선 batch의 증거를 재사용하고 이번 기능 레인에서 반복하지 않는다. `src/frp.new`는
사용자 소유 dirty 변경으로 계속 보존한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (검증 중복 감사 + NPC 실행 연결)

개발 속도 전수 점검 결과, 기능 레인은 `fast`, 조립 batch는 `integration`, 기본 브랜치
병합 시에만 `main`, 호환성·DB·브라우저·차트는 승인된 `release`로 고정했다. `fast`는
현재 작업 트리의 영향 패키지만 검사하고, `server/cmd/muhan/` 변경은 `./cmd/muhan`을
추가한다. 따라서 main flag/scheduler wiring을 놓치지 않으면서도 ARM64 cross-build와
전체 저장소 gate를 매 레인에 반복하지 않는다. 수동 CI의 `release-scope-guard`와
`workflow_dispatch` 전용 정책은 그대로 유지한다. CI의 clean checkout에서 fast가 조용히
skip되지 않도록 해당 job만 depth 2와 `GO_FAST_COMMIT=1`을 사용한다.

이번 bounded 후속의 구현 상태:

- `TalkCatalog`를 session→transport→`cmd/muhan`까지 연결했다. 운영 실행 시
  `-npc-talk-dir` 또는 `MUD_NPC_TALK_DIR`로 canonical `<name>-<level>` 디렉터리를
  명시해야 하며, 미지정이면 주제 대화는 fail-closed한다. 현재 체크인된 전체
  `resources_utf8/objmon/talk`는 CP949 손상 파일 1개가 있어 그 자산을 정정하기 전에는
  전체 카탈로그를 시작 시 로드하지 않는다.
- C `update_active`의 bounded NPC 유지보수 prefix를 프로세스 scheduler에 연결했다.
  (이후 최신 체크포인트에서 maintenance→resource→combat strict ordering을 단일 worker로
  보강했다.)
- 격리 PostgreSQL 유지보수 receipt/replay 테스트는
  `MUHAN_NPC_MAINTENANCE_TEST_DATABASE_URL`이 있을 때만 실행하고, 기본 로컬 실행은
  환경변수 부재로 skip한다. 공유 DB를 사용하거나 정리하지 않는다.

검증: 영향 패키지 race/vet, cmd 프로세스 테스트(격리 DB 미설정으로 의도적 skip),
`scripts/run-go-validation.sh fast`, 조립 batch의 `scripts/run-go-validation.sh integration`
1회, 정책·shell·diff 검사를 실행했다. 이 batch에서는 ARM64 cross-build, 실제 PostgreSQL,
브라우저/차트·release matrix를 반복하지 않는다. `src/frp.new`는 사용자 소유 dirty
변경으로 계속 보존한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (검증 cadence guard + NPC/xterm 후속)

검증 경로를 다시 대조한 결과, 기능 레인은 `fast`, 조립 batch는 `integration`, 기본
브랜치 병합 시에만 `main`을 사용한다. `main`만 Linux ARM64 cross-build를 수행하며,
`release`는 DB·브라우저·x64/Windows/macOS 호환 matrix를 포함하는 승인된 기본 브랜치
검토 전용이다. 실수로 feature branch에서 `release`를 선택하면
`release-scope-guard`가 checkout·PostgreSQL·matrix 시작 전에 중단한다. 정책 테스트와
YAML/shell 검증으로 이 경계를 고정했다.

이번 병렬 Luna max 후속은 다음을 추가했다.

- `TalkCatalog` 주입형 NPC 주제 대화: canonical `<name>-<level>` exact topic 응답,
  파일/키 누락 shrug·fail-closed, `ATTACK/ACTION/CAST/GIVE` 미지원 side effect 거부.
- pre-combat NPC 유지보수 tick: 빈 방 정리·상태 만료·HP/MP 회복·공격 timer·MWAND
  wander를 결정론적 slot과 idempotent receipt로 실행하며, 실패 시 동일 request를 재시도한다.
  프로세스 scheduler wiring과 combat 순서는 다음 경계로 남겼다.
- xterm secret prompt 회귀: 비밀번호가 화면·DOM·scrollback에 echo되지 않는 브라우저 테스트.

검증: 영향 패키지 race/vet, 전체 `scripts/run-go-validation.sh integration`, xterm
Chromium 4건, 웹 typecheck, 정책·shell·diff 검사가 통과했다. ARM64 cross-build,
disposable PostgreSQL, release matrix, 실기기 IME/mobile, WSS/Ingress와 testnet 배포는
중복 실행하지 않았고 해당 경계에 남아 있다. `src/frp.new`는 사용자 dirty 변경으로
수정·stage하지 않는다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (검증 중복 제거 + xterm/시뮬레이션 레인)

검증 경로를 다시 전수 점검해 `fast`가 깨끗한 작업 트리에서 직전 커밋을 암묵적으로
반복하지 않도록 고쳤다. 현재 변경만 표적 race로 검사하고, 커밋 자체를 다시 볼 때만
`GO_FAST_COMMIT=1` 또는 `GO_FAST_BASE=<commit>`를 지정한다. `integration`은 전체
Go race/vet/diff만, `main`은 기본 브랜치에서만 Linux ARM64 cross-build까지 실행한다.
feature branch에서 `main` CI scope를 선택하면 checkout/setup 전에 즉시 거부하며, release
matrix는 계속 수동 승인 시에만 실행한다.

병렬 Luna max 레인 결과도 직렬 통합 전 검증했다.

- 메모리 저장소/WebSocket 회귀가 xterm 캐릭터 생성→`CreateInWorld`→월드 입장→첫 `봐`→
  durable disconnect→동일 캐릭터 재로그인/재입장을 검증한다. draft-only 등록 경로와
  캐릭터 ID 교체를 감시하며 PostgreSQL 없이 결정론적으로 실행된다.
- C `load_crt_tlk` 경계를 정적 `TalkCatalog`로 옮겼다. canonical `<name>-<level>` 경로,
  UTF-8/CP949 decode, key/response와 `ATTACK`/`ACTION`/`CAST`/`GIVE`, first duplicate,
  malformed/overflow/path traversal fail-closed를 테스트한다. 현재 `resources_utf8`에는
  주소 가능한 88개 중 CP949 손상 파일 1개가 있어 실제 전체 catalog 연결은 그 자산을
  정정하기 전까지 fail-closed이며, 아직 parser/connector에 연결하지 않았다.
- C `update_active`의 비전투 NPC 유지보수 prefix(빈 방 제거, 상태 만료, HP/MP 회복,
  공격 cooldown, MWAND 방황)를 snapshot-bound proposal/apply와 idempotent receipt로
  옮겼다. RNG는 첫 commit에서만 사용하고 replay는 재호출하지 않는다. attack/flee/death,
  scheduler, parser/connector 연결은 아직 남아 있다.

검증: `scripts/run-go-validation.sh fast`, world/session/transport targeted race,
`go vet ./internal/world ./internal/session ./internal/transport`, 정책·shell·diff 검사
통과. ARM64 cross-build, disposable PostgreSQL, browser/release matrix는 이번 레인에서
반복하지 않았다. `src/frp.new`는 사용자 dirty 변경으로 계속 보존한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (전역 잡담·환호)

원작 `command4.c:broadsend/broadsend2`의 `잡담`/`잡`/`환호`를 Go world/session/
transport로 옮겼다. 255바이트·UTF-8/control 입력 경계, PBRSND 일일 quota, PSILNC·레벨·
HP gate, 31칸 할인표와 INVINCIBLE 보정을 durable reducer에 고정했다. Supabase receipt에는
daily/HP 결과와 event를 저장하고 descriptor-local cooldown/global admission timestamp는
runtime에만 둔다. PNOBRD/PNOBR2 수신 거부 fan-out은 첫 commit에만 실행하고 replay에는
재실행하지 않는다.

검증: `go test -race ./internal/world ./internal/session ./internal/transport -skip
'^TestRoomBodyCorpus$'` 및 parser/connector/session replay 회귀 통과. 이번 lane에서는
실제 PostgreSQL·browser·ARM64 cross-build를 반복하지 않았고, 각각 통합 batch·main/release
경계에서만 실행한다. `src/frp.new`는 사용자 소유 dirty 변경이라 계속 보존하며, issue #1과
Project 항목은 전체 인수까지 In Progress다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (반복 검증 비용 재분리)

저장소의 워크플로·로컬 hook·검증 스크립트·문서를 전수 대조해 같은 고비용 검증이
기능 레인마다 반복될 수 있는 경로를 제거했다. `fast`는 변경 패키지 targeted race,
`integration`은 필요할 때 전체 Go race/vet/diff만 실행하며 Linux ARM64 cross-build는
실행하지 않는다. 실제 main 병합 경계에서만 `scripts/run-go-validation.sh main`을 한 번
실행해 ARM64 cross-build를 추가한다. 모호한 예전 `merge` 명령은 실수로 ARM64 비용을
소비하지 않도록 거부한다.

수동 CI도 `fast`/`integration`/`main`/`release`로 분리했다. `release`만 DB·브라우저·
x64/Windows/macOS 호환 matrix를 실행하며 자동 push/PR trigger는 추가하지 않았다.
pre-push는 기존처럼 Go 전체 gate를 호출하지 않고 경로 영향이 있는 migration/stack
검사만 수행한다. 정적 정책·shell 검증과 `integration` 전체 Go gate를 변경 후 실행했고,
main ARM64 cross-build와 release matrix는 이 비용 분리 변경 자체로 재실행하지 않았다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (검증 비용 최적화 + bounded lanes)

반복 검증 전수 점검을 반영했다. `scripts/run-go-validation.sh fast`는 변경 경로를
자동 분류해 world 변경 시 session/transport 소비자까지, session 변경 시 transport까지,
transport-only 변경은 transport만 race 검사한다. 문서·명령만 바뀌면 Go 표적 검사를
건너뛰고 `GO_FAST_PACKAGES=all`로만 명시적 전체 표적 검사를 요청한다. `.githooks/pre-push`는
다중 커밋 push의 remote tip을 한 번만 기준으로 삼고, migration/stack-runner 또는
gateway/web/stack contract 변경에 해당하는 검사만 실행한다. 전체 race/vet/Linux ARM64
cross-build는 조립 batch의 `integration`과 실제 main 병합의 `main`으로 분리하고, DB receipt는
`TestPostgresBoundedLanesPersistAndReplay`를 하나의 disposable PostgreSQL에서 한 번만
실행한다. CI는 계속 수동 `workflow_dispatch`이며 ARM64 build와 release matrix를 기능
레인마다 재실행하지 않는다.

이번 bounded batch는 C 원본과 대조한 `줄임말`(목록/추가/삭제, 단일 명령 `$1..$16`/`$*`
치환, 다중 queue fail-closed),
`태워`/`소각`(직접 inventory root, 보호 규칙, cooldown/reward/jackpot), `배워`/`연마`
(scroll level/alignment/class gate, spell bit, room 이동)를 Go world/session receipt와
WorldConnector parser/dispatch/room event까지 연결했다. `go test -race` targeted 및
connector 회귀, `scripts/run-go-validation.sh fast`, 통합 `scripts/run-go-validation.sh
integration` 및 정책 검사를 통과했다. ARM64 PostgreSQL 17의 3-lane 저장/replay batch를
통과했다. 새 검증 harness는 환경변수 없이는 skip되며, 운영 DB를 사용하지 않는다.

커밋 전 보존 조건: `src/frp.new`는 사용자 소유 변경으로 수정·stage·되돌리지 않는다.
전체 게임 parity, strict room corpus 63건, NPC full tick, 브라우저 실기기 IME/mobile,
WSS/Ingress·testnet 배포와 전체 legacy data migration은 아직 남아 있다. 목표 Project
항목과 issue #1은 전체 인수 조건 전까지 `In Progress`를 유지한다.

## 최신 오케스트레이션 체크포인트 — 2026-09-09 (원작 `!` 재실행)

`src/command1.c`의 연결별 `lastcommand` 경계를 Go `WorldConnector`에 연결했다. `!`은
직전 명령을, `!suffix`는 직전 명령에 suffix를 붙여 기존 parser/reducer로 전달한다.
선행 공백과 79바이트 UTF-8 history 예산을 처리하고 빈 확장에서는 이전 history를
보존한다. history는 연결 로컬 상태이므로 `State`·계정·PostgreSQL receipt에 저장하지
않으며 재접속 시 초기화된다. session pure TDD와 live connector `go test -race`가
통과했다. 전체 C 약어 우선순위·다중 alias command queue·출력 parity는 아직 남아 있다.

전용 ARM64 PostgreSQL 17과 Chromium을 함께 사용하는 로컬 E2E도 재실행했다. 가입→월드
입장→줄임말 등록·실행→`!` 재실행→재로그인 및 기존 캐릭터 중복 세션 거절의 2개 테스트가
16.0초에 통과했다. 테스트는 기존 화면에 남은 문자열을 재사용하지 않도록 `!` 응답의
새 발생 횟수를 기다린다. 전체 명령 parity·실기기 IME/mobile·운영 배포는 여전히 남아 있다.

## 현재 오케스트레이션 체크포인트 — 2026-09-09

개발 속도 최적화를 전수 점검해 검증 cadence를 코드화했다. 병렬 Luna max 레인은 담당
파일의 gofmt와 변경 패키지 targeted race만 실행하고, 전체 race·vet·Linux ARM64
cross-build·격리 PostgreSQL·브라우저·차트는 메인 통합 또는 승인된 릴리스 경계에서
batch당 한 번만 실행한다. `scripts/run-go-validation.sh fast`가 레인용이고,
`integration`은 ARM64 build 없는 조립 batch gate, `main`은 실제 main 병합 때의 ARM64
포함 gate다. 기존 `.github/workflows/ci.yml`는 `workflow_dispatch` 전용이며
`fast`(기본 Go 표적 검사), `integration`(전체 Go race/vet), `main`(Linux ARM64 build 포함),
`release`(기존 DB·브라우저·x64/Windows/macOS matrix) 선택으로
비용 경계를 명시한다. Linux ARM64, x64/Windows/macOS 호환 검증 자체는 보존하되 release
게이트에서만 실행한다.
역사 문서에 반복된 전체 검증 명령은 실행 hook이 아니라 과거 증거이므로 매 레인마다
재실행하지 않는다. `.githooks/pre-push`는 Go 전체 gate를 자동 호출하지 않으며 기존
웹/정책 fast check와 명시적 `MUHAN_LOCAL_FULL_STACK=1` opt-in만 유지한다.
전수 정책 검사에서 발견한 stack E2E migration coverage 누락 12개(20261016~27)는
runner에 시간순으로 연결했고 정적 coverage·shell 검사를 통과했다. 실제 full stack은
재실행하지 않았다.

최신 bounded slice는 `뇌물`/`숨겨`/`도망`이다. `MTRADE` threshold·gold debit·NPC
정리, `OHIDDN` object stealth, arrival trap의 dart/pit/alarm/death 처리를 각각
snapshot-bound proposal/apply와 durable receipt/replay로 옮겼고, parser·WorldConnector·
room event까지 연결했다. targeted session/world/transport race와 parser/live connector
회귀, 통합 ARM64 PG17 receipt와 merge gate를 batch당 한 번 실행해 통과했다. 전체 C parity,
strict room corpus 63건, NPC full cadence, 실기기 IME/mobile, WSS/Ingress·testnet 배포는
여전히 남아 있다. `src/frp.new`는 사용자 소유 변경으로 계속 보존한다.

이전 bounded slice는 `기공집결`/`살기충전`/`참선`이다. `PPOWER`/`PSLAYE`/`PMEDIT`
flag와 원본 timer·cooldown·권한·성공 stat/THACO·실패 cooldown을 snapshot-bound
proposal/apply와 durable receipt/replay로 옮겼고, parser·WorldConnector·room event까지
연결했다. targeted session/world/transport race와 parser/live connector 회귀, 통합
ARM64 PG17 receipt와 merge gate를 batch당 한 번 실행해 통과했다. 전체 C parity,
strict room corpus 63건, NPC full cadence, 실기기 IME/mobile, WSS/Ingress·testnet 배포는
여전히 남아 있다. `src/frp.new`는 사용자 소유 변경으로 계속 보존한다.

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
격리 `postgres:17-alpine`의 `TestPostgresLookAtTargetOccurrencePersistsAndReplays`는
추가로 통과했지만, 전체 PostgreSQL+Chromium 재실행, C command/tick/경제, WSS/Ingress와
testnet 인수는 여전히 미완료다.
인프라 저장소의 `scripts/docker-source-paths.test.mjs` reviewed source pin은 새 root
`f09b0a4a606f61d0ffb8505f660d421d31bda030`을 가리키도록 갱신했으며, infra 테스트 재실행과
commit `ae3f6769`까지 완료했다. `node --test scripts/docker-source-paths.test.mjs`는 2/2
통과했다. `src/frp.new`는 계속 사용자 dirty 변경으로 보존한다.

## 2026-09-09 메일·게시판 bounded 후속 및 검증 감사

이번 로컬 batch에서 Luna max 세 레인을 직접 병렬 실행하고 공용 parser/connector를 메인
세션에서 통합했다.

- `편지받기`/`편지삭제`: `State.Mailboxes` ordered canonical mailbox, sender/body/timestamp
  검증, RPOSTO(10) 우체국 방 게이트, 전체 mailbox 원자 삭제와 receipt replay 경계를 추가했다.
  `편지보내기` interactive multi-line editor는 명시적 unsupported로 남겼다.
- `게시판`/`읽어 게시판 <번호>`/`글삭제 게시판 <번호>`: `BoardState`와 State 연결, board_dir
  허용 ID(100–116, 120), canonical 게시판 object 해석, newest-first 목록, 삭제/복구 권한과
  non-owner 조회수 증가를 추가했다. `써` editor 및 전체 board data migration은 후속이다.
- CI 비용 감사: fast/integration/main/release 및 pre-push 호출 그래프에 중복 ARM64·DB·browser
  실행 결함이 없음을 확인했다. ARM64는 main 병합에서 한 번, DB/browser/호환성 matrix는
  release에서만 실행하며, 기능 레인은 영향 패키지 race와 조립 integration만 사용한다.
  같은 release job 안에서 반복되던 Node toolchain 초기화 5회는 job당 1회로 통합했다.

검증 결과: 영향 패키지 `scripts/run-go-validation.sh fast` PASS, 전체
`scripts/run-go-validation.sh integration` PASS, `go test -race` board/mail/parser/connector
표적 PASS, `python3 tests/unit/local_first_policy_test.py` PASS,
`python3 tests/unit/self_hosted_ci_policy_test.py` PASS,
`python3 tests/unit/stack_e2e_migration_coverage_test.py` PASS, shell/YAML/diff 검사 PASS.
이번 batch에서는 ARM64 cross-build, 실제 PostgreSQL/browser/release matrix, strict room corpus
63건, full board editor/mail send, NPC full parity, IME/mobile 실기기, WSS/Ingress와 testnet
배포를 반복하지 않았다. 사용자 소유 `src/frp.new`만 dirty 상태로 보존한다.

## 2026-09-09 xterm compose continuation 연결 체크포인트

`편지보내기 <이름>`과 `써`의 multiline continuation을 메인 connector에 통합했다.
`worldConnection`은 connection-local draft와 stable command ID를 보유하고, 작성 중인
행을 history/alias/parser로 넘기지 않는다. 메일은 첫 `.`에서 canonical mailbox에 한
번 append하고, 게시판은 제목·본문·번호를 한 번 append한다. 게시판 `!!`/빈 제목은
receipt 없이 취소되고, `Close`/성공 시 draft references를 버린다. transient commit 오류
뒤에는 같은 command ID와 메일 ID로 재시도하며, board room event는 최초 commit에만
전달한다.

변경 파일은 `server/internal/session/compose_command.go`,
`server/internal/transport/world_connector.go`, `server/internal/world/mail_send.go`,
`server/internal/world/board_write.go`와 각 race/PG 회귀 테스트·문서다. 현재 원작의
제목 직후 `.` 빈 게시글은 fail-closed 차이로 명시했다.

검증: targeted `go test -race`(world/session/transport), `scripts/run-go-validation.sh
fast`, 전체 `scripts/run-go-validation.sh integration`, `go vet`, policy 테스트 통과.
`MUHAN_MAIL_BOARD_TEST_DATABASE_URL`이 없어서 opt-in PostgreSQL test는 실행하지 않았으며,
ARM64 cross-build/browser/release/testnet은 이번 레인에서 반복하지 않았다. `src/frp.new`는
사용자 소유 dirty binary로 계속 보존한다. 다음 작업은 실제 disposable PG에서 새 send/write
receipt를 함께 검증하고, 빈 게시글·legacy post/board 이관 차이를 differential 결정하는
것이다.

## 2026-09-09 `듣기거부`·`훔쳐` 통합 체크포인트

이번 작업은 검증 중복 제거 cadence를 유지하면서 두 개의 독립 Luna max 레인을 병렬 처리하고
메인에서 parser/connector만 조립했다.

- `server/internal/session/ignore_command.go`, `server/internal/transport/ignore_list.go`:
  `듣기거부`의 connection-local 목록·정규화·상한·동시성 계약을 추가했다.
- `server/internal/transport/world_connector_ignore.go`:
  authoritative online exact target/PDMINV 확인, 목록 출력·toggle, 대상 descriptor의
  ignore list를 이용한 직접 메시지 차단을 연결했다. ignore는 world/receipt/DB에 저장하지
  않는다.
- `server/internal/world/steal.go`, `server/internal/session/steal_command.go`:
  `훔쳐 <물건> <대상>`의 권한·쿨다운·시야/보호·확률·canonical root subtree transfer,
  NPC 실패 적대화와 player-kill timer를 typed receipt로 구현했다.
- `server/internal/transport/world_steal_events.go`:
  commit 뒤 reveal/failure room event와 player target warning을 한 번만 전송한다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Steal|Ignore|DirectMessage' -count=1)  PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport)  PASS
scripts/run-go-validation.sh fast  PASS
scripts/run-go-validation.sh integration  PASS
git diff --check  PASS
```

이번 기능 레인에서는 ARM64 cross-build, 실제 PostgreSQL, 브라우저/IME, release matrix를
반복하지 않았다. 해당 검증은 `main`/`release` 경계에서만 실행한다. strict room corpus
63건, C 전체 prefix/occurrence/ANSI parity, NPC full cadence, WSS/Ingress와 testnet 배포는
여전히 남은 인수 조건이다. `src/frp.new`는 사용자 dirty 변경으로 계속 보존한다.

## 2026-09-09 기습·물약·주문 전수 통합 체크포인트

직접 관리한 Luna max 세 레인을 병렬로 완료한 뒤 메인에서 parser와 connector를 통합했다.
코드 커밋은 `c194824` (`기능: 기습·물약·주문 전수 Go 경계 연결`)이다.

- world: `backstab.go`, `drink.go`, `teach.go`와 각 TDD가 canonical State의 proposal/apply
  경계를 사용한다. 세션: `*_command.go`가 표시 이름/주문/물약 선택자만 받고 `ExecuteGame`
  receipt·command-ID replay를 보장한다.
- transport: `CommandBackstab`·`CommandDrink`·`CommandTeach`를 중앙 parser에 등록하고,
  committed result의 room/target projection을 최초 실행에서만 전달한다. actor는 typed
  receipt 응답을 받고 replay에서는 RNG·mutation·event가 반복되지 않는다.
- 범위: backstab은 사망 reducer 조합 전까지 lethal fail-closed, drink은 self-target으로
  표현 가능한 효과와 OSPECI만 허용하며 C restore의 부분 성공은 보류, teach는 online
  same-room player와 원본 권한/visibility를 적용한다. 전체 C prefix/ANSI parity와 NPC 전투
  full cadence는 미완료다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Backstab|Drink|Teach|WorldConnectorSubmitDispatches(Teach|Backstab|Drink)' -count=1) PASS
(cd server && go vet ./internal/world ./internal/session ./internal/transport) PASS
scripts/run-go-validation.sh fast PASS
scripts/run-go-validation.sh integration PASS
git diff --check PASS
```

ARM64 cross-build·실제 PostgreSQL·브라우저/IME·release matrix는 cadence 정책대로 이번
기능 레인에서 반복하지 않았다. strict room corpus 63건, 전체 C command/prefix/key/ANSI
parity, NPC full cadence, WSS/Ingress와 testnet 배포는 남은 조건이다. `src/frp.new`는
사용자 소유 dirty binary로 stage/수정하지 않았다.

## 2026-09-09 교란·맹공·혈도봉쇄 통합 체크포인트

세 개의 독립 Luna max 레인을 병렬 실행하고 메인에서 parser·connector·room/target event를
조립했다. 코드 커밋은 `c5609e3` (`기능: 교란·맹공·혈도봉쇄 경계 연결`)이다.

- `교란`: canonical same-room NPC→player 선택, 권한/PVP·전쟁·안전방·시야, stealth/
  `LT_ATTCK`, 확률·befuddle·적대 상태를 `PlanCircle`/`ApplyCircle` receipt로 연결했다.
- `맹공`: fighter/barbarian/invincible 권한, canonical 무기·내구도·명중·damage dice,
  befuddle·NPC 적대/proficiency와 비치명 HP를 `PlanBash`/`ApplyBash`로 연결했다. lethal
  `die`/도주와 descriptor charm/전쟁 상태는 fail-closed다.
- `혈도봉쇄`: NPC-only lookup/visibility/occurrence, reveal·cooldown·`MUNKIL` 순서를
  고정했다. 원작의 적대 추가·반 HP damage/death/flee 후속은 canonical reducer 조합 전까지
  `ErrMagicStopCombatSideEffectPending`으로 영수증 없이 거부하며, transport는 세션을
  끊지 않고 unsupported 응답을 돌려준다.

검증은 새 world/session/transport focused race, `go vet`, `scripts/run-go-validation.sh fast`,
`scripts/run-go-validation.sh integration`, `git diff --check`를 통과했다. ARM64는 `main`,
실제 PostgreSQL·브라우저·호환성 matrix는 `release`에서 한 번만 실행한다. strict room corpus,
전체 C prefix/key/ANSI parity, NPC full cadence, IME/mobile 실기기, WSS/Ingress와 testnet
배포는 아직 남은 승격 조건이다. `src/frp.new`는 사용자 소유 dirty 변경으로 보존한다.

## 2026-09-09 직접 관리 병렬 후속: 방혼술·흡성대법·차기 및 검증 비용 경계

세 Luna max 레인을 서로 겹치지 않는 world/session 파일로 병렬 처리한 뒤, 메인에서
공용 parser·`WorldConnector`·room/target fan-out만 한 번 조립했다.

- `방혼술`: cleric/paladin/invincible 권한, canonical same-room NPC와 occurrence, undead/
  visibility·`MUNKIL`, `LT_TURNS`/`LT_ATTCK`, 확률·소멸/반 HP damage 및 비치명 enemy 관계를
  snapshot-bound receipt로 연결했다. NPC death graph가 allocator와 함께 완전히 조합되지
  않으면 `ErrTurnDeathTransitionPending`으로 fail-closed한다.
- `흡성대법`: mage/invincible gate, exact canonical NPC, stealth reveal·cooldown·`MUNKIL`,
  source chance/damage, undead MP 소진 또는 HP 흡수·enemy damage를 deterministic receipt로
  고정했다. unresolved combat/death/overflow는 RNG와 commit 전에 거부한다.
- `차기`: barbarian/invincible 권한, NPC 우선 및 player PVP 안전/war/charm 경계, 무기·명중/
  damage dice·stealth·`LT_KICK`와 비치명 HP/enemy projection을 연결했다. lethal 전이는
  `ErrKickDeathTransitionPending`으로 보류하며 target private event는 room 관찰자와 분리한다.

각 레인은 world/session focused race를 수행했고, 메인 통합 후 connector 회귀와 Go 통합
gate를 한 번만 실행한다. `fast`는 변경 패키지 표적 검사, `integration`은 전체 Go
race/vet/diff, ARM64 cross-build는 기본 브랜치 `main`, 실제 PostgreSQL·브라우저·x64/
Windows/macOS는 승인된 `release`에서만 실행한다. 이번 batch에서는 ARM64·DB·browser·strict
room corpus를 반복하지 않았다. 전체 C prefix/key/ANSI parity, NPC full tick, IME/mobile,
WSS/Ingress와 testnet 배포는 계속 남은 승격 조건이며 `src/frp.new`는 수정·stage하지 않는다.

## 2026-09-09 직접 관리 병렬 후속: 사용·암호·직업전환 및 검증 비용 재감사

서로 겹치지 않는 세 Luna max 레인을 병렬 처리한 뒤 메인에서 공용 parser와
`WorldConnector`만 조립했다. 완료된 레인은 끝나는 즉시 중단해 유휴 에이전트와 중복
호출을 남기지 않았다.

- **사용**(`command9.c:use`): canonical inventory/floor root를 이름+occurrence로
  해석하고 OUSEFL·특수 SP_WAR를 확인한다. 무기/갑옷/광원은 기존 `ReadyItem`의
  무장·착용·휴대 reducer로, 물약은 `PlanDrink`/`ApplyDrink`로 위임한다. floor 이동,
  PHIDDN 해제, 소비와 room event를 하나의 proposal/apply receipt로 묶고, scroll/wand/
  key/미확인 분기는 RNG·변경 전에 `ErrUseUnsupported`로 닫았다.
- **암호**(`command11.c:passwd`): 현재→새 암호→확인 상태기를 connection-local로
  추가하고, 출력·로그·영수증에는 평문/해시를 넣지 않는다. 새 bcrypt hash는 한 번만
  만들며 저장 응답 유실 시 expected/replacement hash를 묶은 Postgres transaction이
  동일 의도를 idempotent 재시도한다. `취소`와 잘못된 입력은 저장하지 않는다.
- **직업전환**(`command7.c:change_class/chg_class_main`): blind/RTRAIN/class/XP/PFAMIL
  게이트, RTRAIN+1..+3 destination class fold, XP 100000 차감과 `LowerPlayerLevel`의
  stat/vital 변경을 snapshot-bound receipt로 구현했다. bare `직업전환`은 현재 원작
  확인 문구를 기록하는 typed no-op이며, `직업전환 예`만 변경을 커밋한다. PFAMIL의
  family roster와 lethal/전역 후속이 없는 상태에서는 fail-closed한다.

검증 비용도 다시 전수 대조했다. workflow는 manual dispatch만 가지며 `fast`는 변경된 Go
  패키지 race, 조립 후 `integration`은 전체 Go race/vet/diff 1회, 기본 브랜치 `main`만
  Linux ARM64 cross-build, 명시적 기본 브랜치 `release`만 DB/browser/x64/Windows/macOS
  호환성 matrix를 실행한다. release 안의 ARM64 native/runtime smoke는 운영 호환성
  checkpoint이고 일반 기능 레인에서 재호출하지 않는다. migration 2회 적용과 replay
  검사는 재실행 안전성을 위한 의도된 중복이며 제거하지 않았다. pre-push도 remote tip을
  한 번만 기준으로 migration/stack 계약만 검사한다.

검증 결과:

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/storage \
  -run 'Use|ChangeClass|ClassChange|Password|ParseUseAndChangeClass' -count=1) PASS
(cd server && go test -race ./internal/transport \
  -run 'Turn|Absorb|Kick|Use|ChangeClass|WorldConnectorSubmitDispatches' -count=1) PASS
scripts/run-go-validation.sh integration PASS
git diff --check PASS
```

strict room corpus(63건), 실제 PostgreSQL password adapter, 브라우저/IME·모바일,
ARM64 main gate, release matrix, 전체 C prefix/ANSI parity, NPC full tick, WSS/Ingress와
testnet 배포는 이번 기능 레인에서 반복하지 않았다. `src/frp.new`는 사용자 소유 dirty
변경으로 수정·stage하지 않았다.
