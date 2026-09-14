# STOP — 사용자가 재개하기 전까지 오케스트레이션 금지

**STOP.** 새 Orca 워커를 시작하지 마라. `worker-start` / `check --wait` / 새 dispatch 금지. 사용자가 이 세션에서 명시적으로 재개하라고 하기 전에는 이슈를 닫지도, 커밋/푸시도, Helm 변경도 하지 마라.

권위: 이 파일 + 저장소 루트 `HANDOFF.md` 맨 위 「STOP」 섹션.

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
