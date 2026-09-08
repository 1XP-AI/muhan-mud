# 작업 기준

## 2026-09-08 사용자 결정: Go 서버로 전환

- 현재 실행 기준은 `docs/porting-research/go-server-execution-plan.md`다. 먼저 이 문서와 `HANDOFF.md` 상단을 읽는다.
- 최종 게임 런타임은 Go, 영속 상태는 Supabase PostgreSQL이다. 가입·로그인은 원작처럼 xterm 안에서 게임 캐릭터 이름/비밀번호로 수행한다. 별도 웹 가입·Supabase 웹 로그인은 요구하지 않는다. Supabase Auth 사용 여부는 게임 인증 계약 검토 후 결정하며 필수 의존성이 아니다.
- 웹은 새롬 데이터맨에서 영감을 받은 중앙 xterm 단일 화면이다. `docs/web-mud/terminal-only-ui-plan.md`를 따른다. 터미널 기본 포커스를 유지하되 텍스트 선택·IME·모바일 키보드·접근성을 방해하지 않는다.
- C→DB 어댑터 확장과 Rust 게임 런타임 개발을 중단한다. C/Rust 코드는 비교 테스트·데이터 변환의 참고 자산으로 보존하며 Go 운영 서버의 FFI/하위 프로세스로 사용하지 않는다.
- 과거 목표에 남은 “Rust 포팅 경계”, dual-write 확장 등의 문구는 새 구현 지시가 아니다. 충돌하면 새 계획을 기준으로 사용자에게 알린다.
- 2026-09-08 새 Go 목표 생성 및 실행 지시가 확인됐다. G0부터 구현과 로컬 검증을 진행한다. CI·push·배포는 아래 제한과 사용자 지시 범위를 따른다.

## 개발 재개 후

- TDD로 작은 기능 단위를 완성한다. 진행률은 승인된 전체 기능 목록의 인수 기준 통과로 계산하며 코드량·테스트 개수를 게임 완성률로 환산하지 않는다.
- 최신 비용 정책: 모든 하위 에이전트는 난도와 무관하게 Luna max (`gpt-5.6-luna`, reasoning `max`)만 사용한다. Astra/Terra 등으로 자동 승격하지 않는다. 과거 목표 본문의 혼합 모델 배정보다 이 사용자 결정이 우선한다. 의존성 계약을 먼저 확정하고 파일 소유권이 겹치지 않는 작업만 병렬화한다. 불필요한 에이전트 생성·중복 검증을 피한다. 메인 세션 모델 변경은 앱 설정에서 별도로 확인하며 변경되지 않은 모델을 Luna라고 보고하지 않는다.
- 검증은 로컬 우선이다. 자동 push, GitHub Actions 반복 실행, 클라우드 빌드, 운영 전환은 하지 않는다. 별도 사용자 지시의 범위를 따른다.
- 공유 Docker의 다른 작업 컨테이너·볼륨·캐시를 삭제하지 않는다. 작업별 고유 Compose 프로젝트와 임시 경로를 사용하고 자신이 만든 리소스만 정리한다.
- `src/frp.new` 및 기존 사용자 변경은 수정·stage·되돌리기 금지. 완료 작업의 worktree도 병합과 미보존 변경 여부 확인 없이 삭제하지 않는다.
- push가 요청되면 `private` 원격만 사용한다. 커밋 메시지는 한국어로 작성한다.

## 목표 연동 GitHub Projects (2026-09-08)

- 사용자 요청으로 만든 비공개 모니터링 보드: https://github.com/orgs/1XP-Inc/projects/1
- 저장소는 `1XP-Inc/muhan-mud`만 사용한다. 전체 목표 이슈: https://github.com/1XP-Inc/muhan-mud/issues/1
- 목표 작업 시작 시 관련 이슈와 Project의 Status/Stage/Evidence를 읽고 현재 코드와 대조한다. 해당 작업을 시작하면 In Progress로 표시한다.
- 작업 종료 시 실제 변경, 검증 명령·결과(실행/skip 구분), 미검증·실패·남은 조건을 해당 이슈 댓글과 Evidence 필드에 갱신한다. 의미 있는 진전만 기록하고 동일 상태 댓글을 반복하지 않는다.
- 목표와 보드의 네이티브 자동 동기화는 없다. 위 시작/종료 갱신을 실행 프로토콜로 사용한다. 이 규칙은 코드 push/CI/배포 또는 별도 주기 자동화를 허용하지 않는다.
- Done/이슈 닫기는 해당 이슈의 전체 인수 조건이 증거로 충족된 경우만 한다. 로컬 미커밋 구현은 로컬 증거로 표시하고 원격 재현/배포 완료로 쓰지 않는다. 닫힌 이슈 비율을 게임 완성률로 환산하지 않는다.
- 갱신 실패 시 이슈를 중복 생성하지 말고 미반영 사실을 보고한 뒤 다음 실행에서 재조회한다.
- 이슈 매핑: #2 G0 전체 계약/원장, #3 G1 월드/이동, #4 G2 인증/캐릭터 이관, #5 G2 xterm/IME/모바일, #6 G3 전투/성장, #7 G3 NPC/tick/관계, #8 G3 아이템/경제, #9 G3 기술/마법/퀘스트, #10 G3 사회/관리, #11 G4 데이터/복구, #12 G5 패키징/배포.
- Project ID: `PVT_kwDOCOMKYc4Biw7Q`. Status field: `PVTSSF_lADOCOMKYc4Biw7QzhhntwQ` (Todo `f75ad846`, In Progress `47fc9ee4`, Done `98236657`). Stage field: `PVTSSF_lADOCOMKYc4Biw7Qzhhnt2Y`. Evidence field: `PVTF_lADOCOMKYc4Biw7Qzhhnt2U`. 항목 ID는 실행 시 `gh project item-list 1 --owner 1XP-Inc --format json`으로 조회한다.
