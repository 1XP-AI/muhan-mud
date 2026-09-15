# 로컬 Go 터미널 브라우저 검증

2026-09-08 · Playwright CLI, Chromium headed · 실제 Go 프로세스/폐기용 PostgreSQL 17/Next dev.

확인된 결과:

- 웹 가입 페이지 없이 xterm 이름 프롬프트, 초기 입력 포커스 확인.
- 영문 이름 Alice로 원작 선택 질문 진행, 한글 선택은 브라우저 paste 이벤트 사용.
- 캐릭터 저장 완료 출력 및 PG 캐릭터 1행 확인.
- 새로고침 후 같은 이름/암호로 재로그인 성공.
- 출력 DOM에서 테스트 암호 미노출, 콘솔 오류/경고 0.
- 데스크톱과 390×844 viewport의 스크린샷을 직접 확인. 후자는 실제 모바일 기기 테스트가 아니다.

증거 이미지: `output/playwright/go-terminal-desktop.png`, `output/playwright/go-terminal-mobile.png`.
개발 도구 표시 버튼은 Next dev 환경의 요소다. 이미지에 로그인 후 월드 미구현 안내가
명시되어 있으며 게임 플레이 완료를 뜻하지 않는다.

발견/한계:

- Playwright 일반 `keyboard.type`의 한글 입력은 이번 xterm에서 전달되지 않았다.
  붙여넣기는 전달됐으나 이를 IME 정상 동작의 증거로 대신하지 않는다. 실제 OS IME
  조합/확정/Backspace/Enter 및 모바일 키보드는 별도 확인이 필요하다.
- 서버의 프롬프트를 관찰하며 한 줄씩 입력했다. 다중 줄 붙여넣기/긴 줄 wrap/
  선택·스크롤백 보존의 실제 브라우저 회귀는 추가 필요하다.
- 로컬 WS이며 운영 WSS/Ingress, 전체 Supabase 권한, 장애복구 및 월드 플레이는 미검증이다.

테스트 데이터와 서버는 이 검증에만 사용한 폐기용 자원이다. 기존 운영 데이터는 사용하지 않았다.
