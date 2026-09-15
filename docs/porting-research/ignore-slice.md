# `듣기거부` 연결 로컬 slice

상태: 세션/transport 순수 경계 구현 완료 · connector 통합 대기

## 원작 기준

`src/command9.c:ignore`는 인자 없이 실행하면 `extr->first_ignore`를 출력하고,
온라인 플레이어 이름을 인자로 받으면 이미 목록에 있으면 삭제하고 없으면 목록 머리에
추가한다. `src/global.c`의 명령어는 `듣기거부`이며 `first_ignore`는 `extra`에만 있고
`src/io.c:disconnect`에서 연결 종료 때 해제된다. 즉 캐릭터 파일·DB에 이 목록을 저장하지
않는다. 대상은 `find_who`로 현재 접속 중인 플레이어만 찾고, `PDMINV` 대상은 거부한다.

## Go 계약

- `session.ParseIgnoreLine`은 정확한 `듣기거부` bare 목록 또는 한 개의 bounded 대상
  selector만 인정한다. 제어문자, 잘못된 UTF-8, 두 번째 명령 토큰, 공백이 포함된 대상,
  경로 구분자, `.`/`..`, legacy 14바이트·12코드포인트 경계 초과는 fail-closed다.
- `session.CanonicalIgnoreTargetName`은 C의 `up(cmnd->str[1][0])`에 맞춰 첫 ASCII
  소문자만 대문자로 바꾼다. 이는 display-name selector 정규화일 뿐 player ID 조회나
  권한 판정이 아니다.
- `transport.IgnoreList`는 connection-owned zero-value-safe 구조체다. 최신 항목을
  앞에 넣고(`first_ignore` head insertion), 삭제 시 나머지 순서는 유지한다. `List`는
  owned snapshot을 반환하며 `Add` 중복은 no-op, `Toggle`은 단일 lock 아래에서 삭제 또는
  head insertion을 원자적으로 수행한다.
- 목록은 최대 `MaxIgnoredPlayers`(256)개, 각 이름은 14바이트·12코드포인트로 제한한다.
  모든 메서드는 자체 mutex를 사용하므로 connector는 connection mutex와 함께 사용하되
  목록 내부 mutex를 직접 노출하거나 외부에서 목록 slice를 보관하지 않는다.

## 부모 통합 단계의 필수 재검증

파서의 `Target`과 목록의 문자열은 권위 identity가 아니다. connector가 dispatch할 때
다음 순서를 지켜야 한다.

1. 현재 lease의 actor가 여전히 온라인인 canonical player인지 확인한다.
2. 현재 authoritative world/connection registry에서 대상 이름을 exact-match로 다시 찾는다.
3. 대상이 온라인이며 source의 `PDMINV`/관리자 visibility 경계를 통과하는지 확인한다.
4. 검증된 canonical display name만 해당 connection의 `IgnoreList.Toggle`에 전달한다.
5. 결과 응답/목록은 receipt나 world state에 넣지 말고 현재 연결에만 출력한다. 연결 종료 시
   `IgnoreList`를 버리고 새 연결에는 새 zero-value 목록을 만든다.

현재 slice는 `WorldConnector`, `CommandKind`, `world.State`, engine receipt, PostgreSQL을
수정하지 않는다. 따라서 parser가 실제 게임 명령으로 분류되거나 DM/잡담 fan-out에서
`IgnoreList.Contains`가 적용되는 것은 부모 통합 후속 작업이다. 그 통합에서는 대상이
로그아웃한 뒤에도 목록 문자열이 남는 원작 동작과, 다시 같은 이름이 접속했을 때 해당
문자열이 적용되는 수명 의미를 differential test로 명시해야 한다.

검증:

```text
(cd server && go test -race ./internal/session ./internal/transport -run 'Ignore')
```

위 targeted race 테스트는 session parser와 transport 목록의 TDD, capacity, stable order,
동시 호출을 검사한다. 전체 integration·ARM64·PostgreSQL·브라우저 검증은 이 독립 slice에서
반복하지 않는다.
