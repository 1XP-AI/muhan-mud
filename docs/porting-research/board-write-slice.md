# `써` 게시판 글쓰기 Go bounded slice

상태: 2026-09-09 로컬 순수 domain/TDD 구현 · session/transport/PG receipt 연결 전

## 원작 경계

`src/board.c:176-321`의 `writeboard`/`write_board`는 현재 방의 `게시판`을
찾은 뒤 제목을 한 줄로 받고, 본문을 여러 줄로 수집한다. 본문 입력은
`!!`로 취소할 수 있고, 행 첫 글자가 `.`이면 제출한다. 제출 시
`board_index` 끝에 `num = index_count + 1`, 작성자, localtime, 줄 수,
`readnum = 0`, 제목을 한 번 append하고 임시 본문 파일을 같은 번호로
이동한다. Go는 임시 파일을 만들지 않고 이 두 결과를 하나의 `BoardPost`
append 후보로 만든다.

## canonical submit payload

`world.BoardWritePayload`가 interactive continuation과 world reducer 사이의
유일한 submit 계약이다.

```text
BoardID: 0 또는 canonical room object에서 확인한 board_dir 번호
Title:   UTF-8 한 줄 제목
Body:    UTF-8 본문, editor 각 행 뒤 LF가 하나씩 붙은 문자열
```

`BoardWritePayloadFromLines`를 사용하면 session이 수집한 `[]string`을
`strings.Join(lines, "\n") + "\n"`으로 변환한다. `.`와 `!!`은 이 함수에
전달하지 않는다. 작성자와 권한 주체는 payload에 넣지 않는다. reducer가
온라인 canonical `PlayerState.Body.Name`에서 작성자를 파생하므로 client가
다른 캐릭터 이름을 위조할 수 없다.

제목·본문·각 본문 행은 invalid UTF-8, terminal/control character, 고정
바이트 한도를 자르지 않고 거부한다. 본문은 최소 한 개의 유효한
non-whitespace 내용과 terminal LF를 요구하며, 빈 행은 다른 내용이 있는
경우 보존한다. 이 bounded 정책은 원작의 빈 게시물(제목 뒤 바로 `.`)을
운영 데이터로 받아들이지 않는 명시적 차이다.

## proposal/apply 계약

- `State.PlanBoardWrite(actorID, payload, now)`는 deterministic clock을 받고
  `BoardContext`를 통해 현재 방의 canonical `SP_BOARD` object와 닫힌
  board ID(100–116, 120)를 확인한다. payload의 nonzero `BoardID`는 이
  object의 ID와 일치해야 한다.
- `BoardWriteOptions.AllocateNumber`는 모든 검증 뒤 한 번 호출된다. 인자는
  `(boardID, len(posts)+1)`이고 반환 값은 반드시 제안된 다음 번호와 같아야
  한다. nil이면 `len(posts)+1`을 사용한다. 이는 외부 DB sequence가
  임의 번호를 만들어 contiguous legacy index를 깨는 것을 막는다. 외부
  allocator는 receipt commit 전 부작용을 만들지 않거나 동일 command ID에
  대해 재시도 가능한 예약이어야 한다.
- `now`는 `Round(0).UTC()`로 정규화한다. reducer가 `time.Now()`를 호출하지
  않으므로 timestamp를 fixture와 receipt에서 재현할 수 있다.
- `State.ApplyBoardWrite(proposal)`는 actor body, room item graph, board
  snapshot, next sequence를 다시 확인한다. 어느 하나라도 바뀌면
  `ErrBoardWriteStaleProposal`/fail-closed 오류를 반환하고 index 또는 body를
  부분 저장하지 않는다.
- 성공 시 `BoardState`의 해당 `Posts` 끝에 새 `BoardPost`를 append하고
  `ReadCount=0`, `Deleted=false`를 고정한다. `BoardWriteResult`는 actor
  응답, 번호/줄 수, post, 그리고 commit 후에만 발행할
  `BoardWriteEvent`를 함께 제공한다. receipt replay에서는 event를 다시
  fan-out하지 않아야 한다.

## session 통합 포인트

1. `써` line을 parser가 받으면 connection-local continuation mode로
   전환한다. 첫 입력은 제목, 이후 입력은 본문 행이다.
2. 제목이 비어 있으면 원작처럼 취소한다. 본문 행 첫 두 글자가 `!!`이면
   payload를 만들지 않고 continuation을 버린다. 첫 글자가 `.`이면
   `BoardWritePayloadFromLines`를 호출하고 `ExecuteGame` 요청에 payload만
   실어 보낸다.
3. receipt reducer 안에서 `PlanBoardWriteWithOptions`와
   `ApplyBoardWrite`를 호출하고 resulting state를 Supabase/PostgreSQL에
   저장한다. 응답 전송은 commit 뒤, event fan-out은 commit 성공 및
   non-replay일 때만 수행한다.

이번 파일은 공용 parser/transport를 수정하지 않는다. 이 slice가 제공하는
통합 API는 `BoardWritePayloadFromLines`, `PlanBoardWrite*`,
`ApplyBoardWrite`, `BoardWriteResult.Event`이다.

## 검증

```text
cd server
gofmt -w internal/world/board_write.go internal/world/board_write_test.go
go test -race ./internal/world -run '^TestBoardWrite' -count=1
```

테스트는 LF/줄 수와 control 입력, actor에서 파생한 author, board object 및
board ID mismatch, allocator 호출 순서/실패/sequence, timestamp 정규화,
append/receipt event, source 불변성, stale board/actor/object와 proposal
replay를 고정한다.

아직 필요한 작업은 `써`의 connection continuation/parser/transport 연결,
Supabase durable receipt 및 실제 PostgreSQL replay, 전체 legacy board
index/body 이관, 브라우저 xterm 입력·모바일 IME 검증이다. 이 레인의
targeted race만 실행했으며 ARM64 cross-build·release matrix·testnet 배포는
승인된 main/release 경계에서 실행한다.

## 2026-09-09 continuation 통합 후속

`써`는 이제 `WorldConnector`의 연결별 제목/본문 continuation으로 연결되어 있다.
본문 행은 메모리에만 쌓고 첫 `.`에서 `PlanBoardWrite` → `ApplyBoardWrite`를 하나의
receipt로 실행한다. 제목의 빈 줄과 본문의 `!!`는 receipt 없이 취소하며, transient
commit 오류에는 같은 command ID·payload를 재사용한다. commit 뒤 room event만 한 번
전송하고 replay에서는 억제한다. 제목 직후 `.`로 빈 게시글을 등록하는 원작 예외는
현재 fail-closed 운영 차이로 남아 있다.

`MUHAN_MAIL_BOARD_TEST_DATABASE_URL`을 지정하면 `mail_board_command_pg_test.go`가
게시판 글쓰기 receipt/replay까지 같은 disposable PostgreSQL에서 검증한다. ARM64
cross-build·browser/IME·release matrix·testnet 배포는 기능 레인에서 반복하지 않는다.
