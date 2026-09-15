# `편지보내기` bounded slice

상태: Go 순수 world domain 구현 완료 · 터미널 editor/connector 통합 전

## 원작 근거

`src/post.c:28-72`의 `postsend`는 발신자가 `RPOSTO` 방에 있는지 확인한 뒤
대상 캐릭터의 legacy player 파일이 존재하는지 확인하고, 다중 행 `postedit`로
진입한다. `src/post.c:77-126`의 editor는 각 행을 즉시 대상 파일에 append하고,
첫 문자가 `.`인 행을 종료자로 사용한다. 작성 완료 시 발신자에게만
`편지를 보냈습니다.\n`을 출력한다.

Go slice는 이 파일 쓰기/editor 경계를 한 번의 canonical state 후보와 durable
receipt로 바꾸기 위한 domain 계약이다. 원작의 동시 append 섞임과 partial write를
재현하지 않고, 한 명령이 한 메시지를 원자적으로 추가하도록 한다.

## Canonical payload

`world.MailSendPayload`는 다음 세 값만 받는다.

```json
{
  "recipient_id": "canonical-character-id",
  "body": "첫 행\n둘째 행",
  "timestamp": "2026-09-09T01:02:03Z"
}
```

- `recipient_id`는 캐릭터 내부 ID다. legacy 이름을 받는 adapter는
  `State.ResolveMailRecipientID`로 정확히 한 명을 찾은 뒤 ID로 변환한다. 같은
  이름이 둘 이상이면 map 순서에 의존하지 않고 거부한다.
- `body`에는 editor가 받은 행을 넣고 종료자 `.`은 넣지 않는다. 줄마다 UTF-8
  **79바이트**까지 허용한다. 원작의 `strncpy(..., 79)` 경계를 데이터 손실 없이
  보존하기 위해 초과 행은 자르지 않고 거부한다. 마지막 줄바꿈은 domain이
  canonicalize하며, 내용 없이 즉시 종료한 원작 흐름은 `"\n"` 한 줄로 표현한다.
- `timestamp`는 command owner가 주입한다. domain은 Unix epoch 이전/zero를
  거부하고 monotonic 정보를 제거한 UTC로 정규화한다.
- 발신자 ID/이름은 payload에서 받지 않는다. online canonical actor의 ID와 이름을
  server state에서만 채운다.

## Proposal/apply 및 fail-closed 경계

`State.PlanMailSend`는 다음 검사를 모두 통과한 뒤 persistence-owned
`MailMessageIDAllocator`를 정확히 한 번 호출한다.

1. canonical online player가 `RPOSTO` 방에 있는지 확인한다.
2. `Mailboxes != nil`인지 확인한다. nil은 legacy `post/<name>` 이관 미완료
   marker이므로 새 map을 몰래 만들어 기존 편지를 가리지 않는다.
3. 수신자 ID/정확한 이름을 player로 해석하고 NPC·누락·모호한 이름을 거부한다.
4. 본문 UTF-8/control/전체 크기/행 크기와 timestamp를 검증한다.
5. mailbox 개수/수신함 용량과 전역 message ID 충돌을 확인한다.

allocator가 없거나 빈 값·control 문자를 반환하거나 기존 ID를 반환하면 후보를
만들지 않는다. allocator 오류나 위 검사 실패에서는 source `State`와 mailbox를
변경하지 않는다.

`ApplyMailSend`는 allocator·clock·network를 호출하지 않는다. proposal에 묶인
actor/recipient/room flags/mailbox snapshot/message를 다시 비교하고, 달라졌으면
`ErrMailSendStaleProposal`로 거부한다. 통과한 경우에만 recipient mailbox의 끝에
한 메시지를 append한 cloned `State`를 반환한다. 결과는 body를 포함하지 않는
`MailSendResult`로, private mail 본문이 actor receipt/event/audit에 불필요하게
복사되지 않도록 한다.

명령 ID replay/idempotency는 domain이 아니라 `engine.Execute`와 PostgreSQL
receipt가 소유한다. adapter는 payload hash와 command ID를 함께 저장하고, ID가
같은 재시도에서는 기존 receipt를 먼저 반환해야 한다. allocator는 command-scoped
unique ID를 발급하거나, 불확실한 commit을 receipt 조회로 복구할 수 있어야 한다.
동일 proposal을 이미 반영된 state에 다시 적용하는 것은 stale/ID conflict로
거부된다.

## 통합 조건

현재 slice는 의도적으로 parser/transport 파일을 수정하지 않았다. 다음 작업에서
연결할 때는 다음 순서를 지킨다.

1. terminal editor가 `편지보내기 <legacy-name>` 뒤의 행을 수집하고, 첫 문자 `.`을
   제거한다. editor 도중 disconnect/timeout/cancel이면 append를 만들지 않는다.
2. adapter가 legacy 이름을 canonical recipient ID로 해석하고, body/timestamp를
   `MailSendPayload`로 만든다.
3. world writer 안에서 `PlanMailSend` → `ApplyMailSend`를 실행하고, `ExecuteGame`
   receipt에 actor 응답만 넣는다.
4. PostgreSQL에서는 sender/recipient FK, unique message ID, mailbox order를 한
   transaction으로 저장한다. body는 command/audit log로 복제하지 않는다.
5. editor 성공/재접속/불확실한 commit을 실제 PG에서 같은 command ID로 재검증한다.

## 로컬 증거

```text
cd server
go test -race ./internal/world -run 'MailSend|CanonicalizeMailSend|ResolveMailRecipient' -count=1
ok   github.com/1XP-Inc/muhan-mud/server/internal/world  1.470s
```

테스트는 exact/ambiguous recipient, RPOSTO·online·canonical actor gate, nil
mailbox migration marker, empty/multi-line body, 79-byte 행, UTF-8/control/size
오류, timestamp, mailbox full, allocator 오류/ID 충돌, source immutability,
stale/tampered proposal 및 allocator 재호출 없는 재적용을 검증한다.

## 2026-09-09 continuation 통합 후속

`편지보내기 <이름>`은 이제 `WorldConnector`에서 수신자 canonical ID를 먼저 확인한
뒤 connection-local 행 편집을 시작한다. 행의 첫 `.`에서만 `ExecuteMailSendWithOptions`
를 호출하고, `!`·`!!`는 본문으로 보존한다. command ID·메일 ID·timestamp는 초안에
고정하므로 transient 오류 뒤 동일 request를 재시도할 수 있다. 성공/연결 종료 시 초안을
폐기하며, 실제 PostgreSQL send/replay는 `MUHAN_MAIL_BOARD_TEST_DATABASE_URL`을 지정한
하나의 disposable DB 테스트에서만 수행한다.
