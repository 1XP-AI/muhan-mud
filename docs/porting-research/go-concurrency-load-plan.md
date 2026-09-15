# Go connector concurrency/load plan

상태: 2026-09-08 로컬 전용 harness 추가. 운영 성능 승인이나 배포 증거가 아니다.

## 목적과 경계

현재 Go `WorldConnector`의 명령 경계를 기준으로 두 비용 모델을 비교한다.

1. `rest-like`: 테스트 전용 `httptest` 요청 하나가 `Open → Submit → Close`를
   수행한다. 운영 REST API를 추가하거나 현재 프로토콜을 바꾸지 않는다.
2. `persistent`: 테스트 전용 로컬 HTTP 요청으로 세션을 한 번 열고 명령을
   제출한 뒤 닫는다. 실제 운영 WebSocket의 대체 구현이 아니라, 연결 수명과
   명령 처리 비용을 분리해 보는 connector capacity probe다. target이 32를
   넘으면 32개만 실제로 admission하고 한 개 overflow 요청으로 `503` hard-limit
   경계를 확인한 뒤 나머지는 virtual-client accounting으로 계산한다.

두 모델 모두 loopback `httptest`와 합성된 메모리 `CommandStore`만 사용한다. 외부
네트워크, 실데이터, Supabase/PostgreSQL, Docker, k6가 필요하지 않다. 따라서 이
harness의 수치는 DB/게이트웨이/브라우저 성능이나 운영 SLO로 해석하지 않는다.

소유 범위는 `tests/load/`와 이 문서뿐이다. 서버 프로토콜/런타임 제한/웹/인프라를
수정하지 않는다.

## 현재 고정 제한

| 경계 | 현재 값 | 의미 |
| --- | ---: | --- |
| Go connector `MaxSessions` | 32 | `cmd/muhan`이 `WorldConnector`에 전달하는 동시 게임 세션 상한. persistent target이 32를 넘으면 초과 admission은 실패해야 한다. |
| Go SQL pool `SetMaxOpenConns` | 16 | `cmd/muhan`의 PostgreSQL 연결 상한. 메모리 harness에는 적용되지 않으며 DB 용량 측정으로 오인하면 안 된다. |
| connector `commandMu` | 전역 단일 `sync.Mutex` | 한 `WorldConnector`의 admission/command 경로가 같은 writer 구간을 공유한다. CPU worker를 늘려도 이 직렬화 경계는 사라지지 않는다. |
| chart gateway `maxConnections` | 200 | 배포 차트의 gateway 동시 연결 제한으로 취급하는 외부 경계. 이 저장소에는 차트 소스가 없으므로 배포 검증 때 실제 chart 값을 재확인해야 한다. |

현재 코드 기준의 첫 두 값은 `server/cmd/muhan/main.go`의 DB pool과 connector
생성 경계, `server/internal/transport/world_connector.go`의 `MaxSessions`와
`commandMu`에서 확인한다. gateway 값은 사용자 승인된 운영 차트 제한을 기록한
것이며, 이 로컬 harness가 이를 증명하지 않는다.

## 안전한 staged targets

허용 target은 `100`, `250`, `500`, `1000`뿐이다. target은 REST-like에서는 총
시도 요청 수, persistent에서는 총 virtual session 수다. 기본 worker 수는 32이고
검증 범위도 1..32로 제한한다. 이 guard는 실수로 무제한 local stress를 실행하는
것을 막는다.

예상 결과:

| mode | target 100 | target 250/500/1000 |
| --- | --- | --- |
| REST-like | 100개 요청 모두 성공해야 함(각 요청이 session을 반환) | target 개수만큼 성공해야 함; failure는 stage/actor/status로 즉시 보고 |
| persistent | 32개 admission 성공, 68개 `503` 거절, 32개 명령 완료 | 32개 admission 성공, `target-32`개 `503` 거절, 32개 명령 완료 |

`503`은 persistent capacity probe에서 예상되는 hard-limit 결과다. 예상보다 적은
admission, admitted command 실패, close 후 pending cleanup 잔류는 harness 실패다.
실패 보고에는 다음 조치가 포함된다: 먼저 `stage`의 첫 오류를 고치고, 그 뒤
target을 올린다. `cleanup_pending > 0`이면 새 target을 실행하지 말고 connector
종료/cleanup 경계를 조사한다.

## 로컬 측정 baseline

다음은 2026-09-08, `4ce00ad5f6c24ae93cf3b23bee1eab14d22fee97` 기준으로 실행한
단일 staged sweep이다. 환경은 Darwin 25.6.0 arm64, Go toolchain 1.27.1
(`GOTOOLCHAIN=auto`), `workers=32`, 60초 timeout이다. duration은 ms로 바꿔
기록했으며, 이 값은 성능 방향성 자료이지 SLO가 아니다.

| mode | target | completed/accepted | rejected | duration | req/s | p50 | p95 | cleanup |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| rest-like | 100 | 100/100 | 0 | 306.848 ms | 325.9 | 90.301 ms | 134.360 ms | 0 |
| persistent | 100 | 32/32 | 68 | 124.356 ms | 257.3 | 24.298 ms | 43.851 ms | 0 |
| rest-like | 250 | 250/250 | 0 | 998.140 ms | 250.5 | 123.543 ms | 168.881 ms | 0 |
| persistent | 250 | 32/32 | 218 | 244.855 ms | 130.7 | 76.733 ms | 114.109 ms | 0 |
| rest-like | 500 | 500/500 | 0 | 1,928.950 ms | 259.2 | 121.123 ms | 150.572 ms | 0 |
| persistent | 500 | 32/32 | 468 | 203.391 ms | 157.3 | 35.300 ms | 79.662 ms | 0 |
| rest-like | 1000 | 1000/1000 | 0 | 3,721.606 ms | 268.7 | 112.609 ms | 163.791 ms | 0 |
| persistent | 1000 | 32/32 | 968 | 123.034 ms | 260.1 | 24.256 ms | 43.821 ms | 0 |

`persistent` duration covers the real 32-session admission/command/cleanup probe;
the `target-32` rejected count includes virtual clients beyond the one overflow
request. REST-like completes every staged request with at most 32 local workers.

### 기록된 1000 실패와 조치

초기 구현의 2026-09-08 REST-like `target=1000 -timeout 60s`는 `context deadline
exceeded`로 실패했다. 당시 report는 `accepted=808`, `completed=808`,
`rejected=192`, `cleanup_pending=0`이었고 첫 오류 stage는 `rest-dispatch`였다.
원인은 connector 제한이 아니라 매 worker가 1000개 synthetic player를 가진 JSON
snapshot을 매 admission/command/close마다 복사·검증·직렬화해 작업량이
불필요하게 증가한 harness fixture였다. 수정 후 REST-like는 worker 수만큼(32)의
재사용 actor만 fixture에 두고, persistent는 32개 hard-limit actor와 overflow
probe actor만 둔다. 위 표의 최신 1000 결과(1000/1000, 3.722초)는 같은 실패가
재발하지 않는 것을 확인한 값이다. 실제 DB/운영 트래픽의 1000 실패 원인을
추론하는 증거로 사용하지 않는다.

## 실행 방법

작은 smoke:

```sh
cd tests/load
go test -run 'Test(PersistentCapacityReportsMaxSessions|RequestIsolationAcrossLocalRuntimes)$' -count=1
go run ./cmd/muhan-load -mode rest-like -target 100 -timeout 30s
go run ./cmd/muhan-load -mode persistent -target 100 -timeout 30s
```

benchmark:

```sh
go test -run '^$' -bench 'Benchmark(RESTLike|PersistentSession)Command$' -benchtime=3s -count=1
```

전체 staged sweep은 각 target을 별도 실행한다.

```sh
for target in 100 250 500 1000; do
  go run ./cmd/muhan-load -mode rest-like -target "$target" -timeout 60s
  go run ./cmd/muhan-load -mode persistent -target "$target" -timeout 60s
done
```

이름/포트 충돌 회피는 `httptest.NewServer`가 매 실행 loopback ephemeral port를
선택하는 방식으로 보장한다. harness는 container를 만들지 않지만, 선택적 향후
container adapter가 생기더라도 `CollisionNamespace(seed)`처럼 매 run마다
고유한 project/container namespace를 먼저 생성해야 한다. 공유 Compose 프로젝트,
고정 host port, 공유 volume을 사용하지 않는다.

## 검증과 해석

테스트는 요청 격리(두 runtime/두 store가 섞이지 않음), cleanup(세션/connector
pending이 0으로 복귀), loopback port와 resource namespace 충돌 회피, staged target
guard, hard-limit 결과를 고정한다. benchmark의 p50/p95와 requests/sec는 현재
호스트에서의 재현 가능한 방향성 자료일 뿐이다. 메모리 store라서 `DB max 16`의
대기/포화, gateway 200의 소켓 수, 실제 TLS/브라우저 backpressure, PostgreSQL
transaction latency는 측정하지 않는다.

측정 결과를 기록할 때는 mode, target, workers, OS/arch, Go version, command, raw
JSON/benchmark output, failure/cleanup 상태를 함께 보존한다. 이 harness만으로
MaxSessions/DB/gateway 제한을 상향하거나 배포 준비 완료를 선언하지 않는다.
