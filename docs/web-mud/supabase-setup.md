# Supabase 설정 (MVP)

이 문서는 `20260901000000_profiles_and_lobby_presence.sql` 적용 절차다. Supabase는 웹 사용자 인증, 자기 프로필, `mud:lobby` 접속 Presence만 담당한다. 게임 명령·전투·이동·월드 상태를 브라우저가 Postgres에 쓰는 정책은 만들지 않는다. 계정과 MUD character의 ownership index/claim migration은 별도 [Game identity migration 실행 가이드](game-identity-migration.md)를 따른다.

## 적용

필수 도구는 Supabase CLI와 로그인된 프로젝트다. 저장소 루트에서 프로젝트를 link한 뒤 migration을 적용한다.

```sh
supabase login
supabase link --project-ref "$SUPABASE_PROJECT_REF"
supabase db push
```

CI에서는 `SUPABASE_ACCESS_TOKEN`을 secret으로 주입하고 `SUPABASE_PROJECT_REF`를 명시한다. 적용 전 `supabase db diff`로 이미 존재하는 `profiles`/정책과 충돌이 없는지 확인한다. 로컬 검증은 `supabase start` 후 `supabase db reset`으로 실행할 수 있으며, 기존 로컬 DB를 공유한다면 reset 전에 백업한다.

## Dashboard 보안 설정

Dashboard → **Project Settings → Realtime Settings**에서 **Allow public access to channels**를 끈다. 이 설정은 모든 채널 join에 `realtime.messages` RLS를 적용하고 `config: { private: true }`가 없는 클라이언트를 거부한다. 웹 클라이언트는 반드시 private `mud:lobby` 채널을 사용한다.

Auth → Providers에서 실제 사용할 provider만 켠다. 브라우저에는 Project URL과 publishable/anon key만 넣고 `service_role` 또는 secret key는 절대 넣지 않는다.

Auth JWT 설정에서 가능하면 비대칭 signing key(예: ES256/RS256)를 사용한다. 게이트웨이는 Supabase JWKS endpoint(`https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json`)를 캐시해 `iss`, `aud`, `exp`, `sub`와 서명을 검증한다. 비대칭 키로 전환할 때는 기존 토큰의 만료 및 키 rotation 기간을 고려한다. JWKS를 사용할 수 없는 레거시 프로젝트는 게이트웨이가 임의로 secret을 브라우저에 노출하지 말고, 서버 측 Auth `/user` 검증 폴백만 허용한다.

## 정책 범위

Migration은 `authenticated`에 자기 `profiles` 행의 SELECT/INSERT/UPDATE만 허용한다. `anon`은 테이블 권한이 없고 DELETE 정책도 없다. Realtime v2의 private 채널 입장 검사는 Broadcast와 Presence의 읽기 권한을 함께 요구하므로 `mud:lobby`에서 두 extension의 SELECT를 허용하되, INSERT는 `extension = 'presence'`에만 허용한다. 따라서 클라이언트 Broadcast 전송, Postgres Changes, 다른 topic, 게임-state 테이블 쓰기 권한은 의도적으로 없다.

## 수동 확인 SQL

Dashboard SQL Editor 또는 `supabase db remote commit`이 아닌 안전한 read-only 세션에서 확인한다.

```sql
select schemaname, tablename, rowsecurity
from pg_tables
where (schemaname, tablename) in (('public', 'profiles'), ('realtime', 'messages'));

select grantee, table_schema, table_name, privilege_type
from information_schema.role_table_grants
where table_schema = 'public' and table_name = 'profiles'
order by grantee, privilege_type;

select schemaname, tablename, policyname, roles, cmd, qual, with_check
from pg_policies
where (schemaname, tablename) in (('public', 'profiles'), ('realtime', 'messages'))
order by schemaname, tablename, policyname;

select tgname, tgrelid::regclass, tgenabled
from pg_trigger
where not tgisinternal
  and tgrelid in ('auth.users'::regclass, 'public.profiles'::regclass);
```

Expected: `profiles` RLS is enabled; only authenticated SELECT/INSERT/UPDATE grants appear; the three profile policies and two `mud_lobby_presence_*` policies exist; both profile triggers exist. The Dashboard Realtime setting is not represented reliably in these catalog queries, so verify **Allow public access to channels = Disabled** in the UI (or via the project settings API used by your deployment automation).

## Rollback

Do not casually drop `profiles`: it is tied to `auth.users` and may contain user edits. To disable the feature, first remove client subscriptions and revoke browser access, then use a reviewed migration to drop the two Realtime policies and profile grants. Keep the table and trigger for recoverability:

```sql
drop policy if exists mud_lobby_presence_select on realtime.messages;
drop policy if exists mud_lobby_read on realtime.messages;
drop policy if exists mud_lobby_presence_insert on realtime.messages;
revoke select, insert, update on table public.profiles from authenticated;
```

If the migration itself must be reverted in a disposable environment, restore from the database backup/snapshot rather than editing migration history. After rollback, repeat the read-only checks above and confirm a test authenticated user cannot read/update `profiles` and cannot join `mud:lobby`.

## Manual smoke test

Create a test Auth user, sign in through the client, and confirm the trigger creates exactly one matching profile. Update only `display_name`; verify `updated_at` changes. Subscribe using a private channel:

```ts
const channel = supabase.channel('mud:lobby', { config: { private: true } })
  .on('presence', { event: 'sync' }, () => {})
await channel.subscribe()
await channel.track({ status: 'online' })
```

An anonymous client, a public channel, or a different topic must be rejected. Presence is for slow connection state only, never per-keystroke input or authoritative game state.
