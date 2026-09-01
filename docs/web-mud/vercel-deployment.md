# Vercel 웹 클라이언트 배포

> 보관된 초기 배포안입니다. 현재 운영 기준은 자체 호스팅 Kubernetes이며 `kubernetes-deployment.md`를 따릅니다.

Vercel에는 `web/`의 Next.js 클라이언트만 배포한다. 기존 C MUD와 WebSocket 게이트웨이는 영속 디스크와 상시 프로세스를 제공하는 별도 런타임에 둔다. 브라우저가 도달하는 경로는 Vercel의 HTTPS와 게이트웨이의 WSS뿐이며, MUD TCP 4000은 공개하지 않는다.

## 프로젝트 생성

저장소를 Vercel에 연결하고 다음 값을 지정한다.

- Framework Preset: `Next.js`
- Root Directory: `web`
- Install Command: 기본값(pnpm workspace 자동 감지)
- Build Command: `pnpm build`
- Output Directory: 기본값(`.next`)

저장소 루트의 `pnpm-lock.yaml`과 `packageManager` 버전을 사용하므로 CI에서 lockfile을 임의 갱신하지 않는다. 첫 배포 로그에서 pnpm 10.15.1과 Next.js 빌드가 선택됐는지 확인한다.

## 환경변수

Production과 필요한 Preview 환경에 아래 공개 변수 세 개를 설정한다.

```dotenv
NEXT_PUBLIC_SUPABASE_URL=https://your-project.supabase.co
NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=sb_publishable_replace_me
NEXT_PUBLIC_MUD_GATEWAY_URL=wss://gateway.example.com/ws
```

이 값은 브라우저 번들에 포함된다. `service_role`, secret key, JWT signing secret은 절대 `NEXT_PUBLIC_` 변수로 만들지 않는다. `NEXT_PUBLIC_MUD_GATEWAY_URL`은 `next.config.ts`의 Content Security Policy에도 빌드 시 포함되므로 값을 바꾼 뒤에는 반드시 다시 배포한다.

Supabase Auth의 Site URL은 안정적인 Vercel production URL로 설정한다. 이메일 확인을 사용하는 경우 허용 Redirect URL에는 실제 production URL과 의도적으로 사용할 preview URL만 등록한다.

## 게이트웨이 origin 연동

게이트웨이의 `ALLOWED_ORIGINS`에는 정확한 origin만 쉼표로 나열한다.

```dotenv
NODE_ENV=production
ALLOWED_ORIGINS=https://mud.example.com,https://muhan-web.vercel.app
```

와일드카드 `*.vercel.app`은 사용하지 않는다. 임의의 Preview 배포를 실서버 게이트웨이에 연결해야 한다면 배포 자동화가 해당 Preview origin을 일시적으로 allow-list에 추가하고 배포 종료 후 제거하도록 만든다. 그렇지 않으면 Preview는 별도 스테이징 게이트웨이를 사용한다.

TLS를 reverse proxy에서 종료한다면 인터넷에서 게이트웨이 컨테이너 포트로 직접 우회할 수 없게 하고, 신뢰한 proxy만 `X-Forwarded-Proto`를 주입하도록 네트워크를 제한한다. 외부 health probe는 `GET /healthz`, 브라우저 접속은 `WSS /ws`와 `muhan.v1` subprotocol을 사용한다.

## 배포 후 확인

1. 배포 URL을 열어 보안 헤더와 로그인 화면이 표시되는지 확인한다.
2. 새 Supabase 테스트 계정으로 가입·로그인한다.
3. 브라우저 Network에서 JWT가 URL query나 로그에 나타나지 않고 첫 WebSocket frame으로만 전달되는지 확인한다.
4. 터미널에서 기존 MUD 환영 문구를 보고 새 캐릭터가 아닌 disposable 테스트 캐릭터로 로그인한다.
5. 한글 명령, 백스페이스, 비밀번호 no-echo, 창 resize, 네트워크 재연결을 확인한다.
6. 게이트웨이 로그에서 해당 세션에 TCP 연결 하나만 만들어졌고 MUD 4000이 외부 스캔에 닫혀 있는지 확인한다.
7. 런타임 재시작 뒤 테스트 캐릭터 파일이 영속 볼륨에 남아 있는지 확인한다.

브라우저에서 `Content-Security-Policy`의 `connect-src` 오류가 보이면 WSS 변수를 고친 뒤 재배포한다. 403 upgrade는 대개 origin, `muhan.v1`, 또는 production WSS 판정 실패다. 4001 close는 JWT 만료이므로 새 Supabase 세션으로 재연결한다.

## 롤백

웹은 Vercel의 이전 검증된 deployment로 promotion한다. 게이트웨이는 먼저 새 연결을 drain하고 이전 이미지로 되돌리되, C MUD 데이터 볼륨은 교체하거나 초기화하지 않는다. Supabase migration은 기록을 수정하지 말고 `supabase-setup.md`의 권한 회수 migration을 적용한다. 롤백 중에도 TCP 4000 공개나 인증 비활성화로 우회하지 않는다.
