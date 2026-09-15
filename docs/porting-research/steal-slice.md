# `훔쳐` bounded slice

상태: world/session reducer와 live connector 통합 완료 · 전체 C occurrence/parity는 후속

## Go 계약

- `훔쳐 <물건> <대상>`의 두 개 one-token selector만 받는다. 대상은 현재 같은 방의
  canonical NPC 또는 온라인 플레이어 display name으로 다시 확인하며, legacy `Body.Inventory`
  만 남은 NPC/플레이어는 추측 변환하지 않고 `ErrStealInventoryUnresolved`로 중단한다.
- 도둑/INVINCIBLE 권한, `LT_STEAL` 5초 cooldown, `PHIDDN`/`PINVIS` reveal, `PBLIND`,
  `RNOKIL`, `PCHAOS`, `MUNKIL`/`MUNSTL`, `ONEWEV`·quest 보호, 레벨·DEX 확률을
  snapshot-bound `PlanSteal`/`ApplySteal`에서 검증한다.
- 성공 시 `TransferItemRoots`로 root와 nested subtree를 한 receipt에서 이동하고,
  플레이어 대상이면 `LT_PLYKL` 7–10일 timer를 함께 기록한다. 실패한 NPC 대상은
  canonical enemy 관계를 추가한다. 동일 command ID 재시도는 receipt를 그대로 반환하며
  RNG·아이템 이동·적대화·방송을 반복하지 않는다.
- 실패/reveal room text와 플레이어 대상 private warning은 commit 뒤 한 번만 connector가
  전송한다. actor는 receipt 응답으로 받으므로 room fan-out에서 제외한다.

## 원작과의 경계

원작 `command6.c:steal`의 `find_crt` prefix/occurrence, descriptor별 object search와
직접 player-file 저장은 아직 canonical ID/import 계약이 없어서 이 slice에 포함하지 않았다.
운영 admission은 canonical inventory와 NPC relationship가 모두 이관된 상태에서만 허용한다.
이름·출력 ANSI 차이는 differential fixture로 별도 확정한다.

## 검증

```text
(cd server && go test -race ./internal/world ./internal/session ./internal/transport -run 'Steal' -count=1)
(cd server && go vet ./internal/world ./internal/session ./internal/transport)
scripts/run-go-validation.sh fast
scripts/run-go-validation.sh integration
```

이번 기능 레인에서는 ARM64 cross-build, 실제 PostgreSQL, 브라우저/IME, release matrix를
반복하지 않는다. ARM64는 `main`, DB·브라우저·호환성 검증은 `release` 경계에서만 실행한다.
