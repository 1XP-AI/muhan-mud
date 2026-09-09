# 게시판 Go 순수 도메인 수직 슬라이스

상태: 2026-09-09 로컬 bounded slice · `State`, 세션/transport, Supabase receipt
경계까지 연결됨. 게시판 작성 editor와 전체 legacy 데이터 이관은 후속 범위임.

## 원작 근거

`src/board.c`의 `BOARD_INDEX`는 게시물 번호, 올린이, 날짜/시간, 줄 수, 조회
카운터, 제목을 고정 폭 raw index에 저장한다 (`18-31`). 게시판 디렉터리는
`100`~`116` 및 `120`으로 닫혀 있다 (`36-56`). Go는 이 raw 구조체를 memcpy하지
않고 `BoardPost`로 정규화한다.

| C 동작 | 근거 | Go 계약 |
| --- | --- | --- |
| 게시판 식별자 탐색 | `board.c:36-56` | `ValidateBoardID`; 100–116, 120 이외는 거부 |
| 목록은 index 끝에서 역방향으로 읽음 | `board.c:140-160` | `Board.Posts`는 번호 오름차순으로 보존하고 `BoardState.List`는 newest-first 반환 |
| 일반 사용자는 삭제 행을 건너뜀, DM은 표시 | `board.c:153-154` | `List(boardID, admin)`의 `admin` 분기 |
| 상세 읽기 번호는 1-based 범위 확인 | `board.c:346-359` | `ValidateBoardPostNumber` 및 state의 contiguous 번호 검증 |
| 삭제 글은 DM 이외에 읽기 거부 | `board.c:360-364` | `PlanRead`/`ApplyRead`가 `ErrBoardDeleted` 반환 |
| 글 작성자 자신은 조회 수를 올리지 않음 | `board.c:376-383` | `PlanRead`가 exact author 비교를 캡처하고, non-owner만 `ReadCount` 증가 |
| 목록/상세 출력 뒤 index를 read-modify-write | `board.c:151-153`, `376-383` | snapshot-bound `BoardReadProposal`; stale proposal은 재적용 거부 |
| 제목 입력은 고정 title 버퍼에 복사 | `board.c:256-263`, `304-305` | `ValidateBoardTitle`는 UTF-8 경계를 자르지 않고 39바이트까지 허용 |
| 본문은 한 줄씩 body 파일에 기록 | `board.c:267-318` | `Body`는 bounded UTF-8 text이며 `\n`만 줄 구분자로 허용 |
| 글 번호는 index 길이에서 1을 더해 발급 | `board.c:285-304` | state는 1부터 연속한 번호만 수용하고 중복 번호를 거부 |
| 작성자 또는 DM만 삭제/복구 | `board.c:390-443` | `PlanDelete`가 권한을 확인하고 `ApplyDelete`가 tombstone를 toggle |
| readnum 부호를 삭제 tombstone으로 사용 | `board.c:445-452` | Go에서는 `Deleted bool`과 non-negative `ReadCount`를 분리해 보존 |

## Go 경계

`server/internal/world/board.go`는 `BoardState{Boards map[int]Board}`와
`BoardPost{Number, Author, Title, Body, CreatedAt, ReadCount, Deleted}`만
소유한다. `Clone`은 map/slice를 깊게 복사하고, `Validate`는 다음을 fail-closed로
검사한다.

- 게시판 번호, 1-based 게시물 번호, 게시물 번호의 중복/비연속 상태
- 작성자/제목/본문의 빈 값, UTF-8, 바이트 길이, control/terminal separator
- zero timestamp, 음수 조회 수

본문에서는 C가 저장하는 줄 구분자 `\n`만 예외로 허용한다. 작성자 한도는 legacy
character name 경계인 14바이트, 제목은 `BOARD_INDEX.title[40]`의 NUL 공간을
보존하기 위해 39바이트, 본문은 1 MiB로 제한한다. 임의의 입력을 잘라 저장하지
않는다.

`List`는 defensive copy를 newest-first로 반환한다. `PlanRead`/`ApplyRead`와
`PlanDelete`/`ApplyDelete`는 exact post snapshot을 proposal에 묶는다. 따라서
동일 proposal을 다시 적용하면 조회 수 이중 증가나 삭제 상태의 이중 toggle 없이
`ErrBoardStaleProposal`로 거부된다. `PlanDeleteRestore`/`ApplyDeleteRestore`와
`ListBoard`/`ListPosts`는 같은 순수 경계의 descriptive aliases다.

삭제 상태는 원작의 음수 `readnum`을 Go의 명시적 `Deleted`로 정규화한다. 원작이
삭제 시 `0`을 `-1`로 보정하던 표현상의 세부를 그대로 저장하지 않고, 실제 조회
수와 tombstone을 분리하므로 삭제/복구 뒤에도 조회 수를 잃지 않는다. 세션 명령은
canonical room item의 `SP_BOARD`/board ID로 대상 게시판을 해석하고, `ExecuteGame`
receipt를 통해 목록·상세 읽기·삭제/복구를 Supabase/PostgreSQL snapshot에 연결한다.
읽기는 작성자 본인일 때 조회 수를 유지하고 그 외에는 한 번만 증가한다. command ID
replay는 engine receipt 경계가 담당한다. 게시판 작성 editor, 전체 board index/body
이관, 별도 조회수 테이블 분리는 후속 범위다.

## 로컬 검증

- `gofmt -w server/internal/world/board.go server/internal/world/board_test.go`
  — 통과
- `cd server && go test -race ./internal/world/board.go ./internal/world/board_test.go -run '^TestBoard'`
  — 통과 (게시판 순수 domain race 포함)
- `cd server && go test -race ./internal/world -run '^TestBoard'` — 현재 작업 트리의
  게시판 테스트 통과
- `cd server && go test -race ./internal/world` — 기존 `TestRoomBodyCorpus`가
  63개의 legacy room resource를 지원하지 못해 실패. 게시판 테스트와 무관한
  기존 corpus gate이며, 이번 변경은 그 리소스나 해당 테스트를 수정하지 않았다.
