# 오케스트레이터 재개 카드 — 2026-09-14

다음 에이전트는 이 파일과 저장소 루트 `HANDOFF.md` 맨 위 섹션을 먼저 읽는다.

## 한 줄

라이브 트리는 `/Users/jjangg96/Documents/1xp/muhan-mud.nosync` 브랜치 `codex/mud-identity-foundation`. GitHub #2만 CLOSED. #1,#3–#12는 OPEN. `src/frp.new`는 만지지 않는다. 라이브 Helm은 아직 C.

## 명령

```text
cd /Users/jjangg96/Documents/1xp/muhan-mud.nosync
git status -sb
git log -1 --oneline
orca skills get orchestration
orca orchestration run-use --id run_80efe6288c63 --json
```

워커 시작 예:

```text
orca orchestration worker-start \
  --spec "<self-contained spec>" \
  --task-title "<title>" \
  --run run_80efe6288c63 \
  --from term_d96bf1ac-3e2a-4d14-9b89-1a4b35c801ba \
  --worktree path:/Users/jjangg96/Documents/1xp/muhan-mud.nosync \
  --agent grok \
  --timeout-ms 180000 \
  --json
```

`check --wait`에는 `--terminal`을 붙이지 않는다. 동시 waiter 두 개 금지.

## 남은 우선순위

1. in-flight 리뷰 `ctx_3c9b17707cf8` (simultaneous-val) 수거·release. 구현자 `ctx_a15aa1f892bc` release.
2. #8 인벤토리·은행·상점 catalog. 아이템 get/drop nested/occurrence 슬라이스는 착륙.
3. #3 실접속 E2E 인수, look extra-token P3.
4. #11 backup/restore·전 데이터 dry-run (63방 Admit 정책 3종은 착륙).
5. #5 실기기 IME. 불가 시 unverifiable.
6. #12 testnet Go cutover + rollback. 라이브 C 유지.
7. #1 마지막.

상세 계약·리뷰 결과·C 오라클은 `HANDOFF.md` 상단 「오케스트레이터 핸드오프」를 따른다.
