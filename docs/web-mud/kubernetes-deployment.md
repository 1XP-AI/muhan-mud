# testnet-1xp 배포 경계

운영 배포의 기준 저장소는 private `1XP-Inc/testnet`이며 프로젝트 경로는 `muhan-mud/`다. 애플리케이션 소스인 이 저장소에는 브라우저·게이트웨이·레거시 C 런타임과 로컬 Dockerfile만 둔다. 인프라 저장소의 Dockerfile이 `1XP-Inc/muhan-mud`의 `main`을 BuildKit secret으로 clone해 `tech1xp/muhan-mud-testnet` 단일 이미지를 만든다.

Helm release `muhan-mud-testnet`은 같은 애플리케이션 이미지를 서로 다른 command로 실행한다.

- web: Next.js standalone server, 공개 경로 `/`
- gateway: JWT 인증 WebSocket↔TCP bridge, 공개 경로 `/ws`
- mud: 단일 C 서버, 내부 ClusterIP 4000만 사용
- postgres: `supabase/postgres`와 보존 PVC
- auth: `supabase/gotrue`, 공개 경로 `/auth/v1`
- realtime: `supabase/realtime`, 공개 경로 `/realtime/v1`

브라우저에는 공개 anon JWT만 전달한다. JWT signing secret, Postgres password, service-role key, Realtime 암호화 키는 사전 생성한 Kubernetes Secret에만 둔다. 게이트웨이는 외부 issuer `https://muhan.1xp.vc/auth/v1`를 검증하되 사용자 조회는 내부 GoTrue ClusterIP로 수행한다.

Postgres와 MUD PVC에는 `helm.sh/resource-policy: keep`을 적용한다. C 서버는 raw struct 파일을 단일 writer로 저장하므로 replicas는 항상 1이고 Deployment 전략은 `Recreate`다. Helm rollback이나 재배포에서 PVC를 초기화하지 않는다.

Vercel 배포 문서는 초기 제품 조사 기록으로만 남는다. 현재 운영 경로에는 Vercel managed runtime이나 hosted Supabase project를 사용하지 않는다.
