# Game identity migration 실행 가이드

이 문서는 [`20260902000000_game_identity.sql`](../../supabase/migrations/20260902000000_game_identity.sql)의 적용·검증 범위를 설명한다. 이 migration은 **계정 소유권 index만** 만든다. C MUD의 plaintext password, Gateway→MUD trusted admission, player-file backfill, DB game-state cutover는 이 migration에 포함되지 않는다.

## 적용 전제와 범위

- self-hosted Supabase의 표준 `auth`, `anon`, `authenticated`, `service_role` database role을 전제로 한다. migration은 `service_role`이 없으면 중단한다.
- migration runner는 `pgcrypto` extension과 `auth.users` FK를 만들 수 있어야 한다. Supabase CLI의 migration role 또는 동등한 관리자 connection을 사용한다.
- Gateway만 service-role credential로 `public.claim_legacy_game_character`, `begin_game_character_session`, `renew_game_character_session`, `end_game_character_session` RPC를 호출한다. browser, edge bundle, Realtime client에는 service-role key를 넣지 않는다. service role에는 이 RPC들의 execute만 부여되며 직접 table read/write와 private schema 접근은 없다.
- challenge/final claim RPC 입력은 `world_id`, 공유 canonicalizer가 만든 `legacy_name_key`, C가 관찰한 exact player-file SHA-256, 검증된 Auth user id, 고정 correlation UUID뿐이다. SHA-256은 password proof가 아니며, DB의 90초 challenge ledger와 C의 password 1회 검증·검증 직후 SHA 재확인을 함께 통과해야 한다. session begin RPC는 owner actor id, character id, random session UUID, bounded gateway id, short expiry만 받고, 같은 row lock 안에서 검증한 owner/lifecycle/canonical name을 반환한다. Gateway는 같은 session/gateway에 대해서만 lease를 주기적으로 renew하고, 종료 시 exact session id로 end한다. legacy password, proof, JWT, ticket은 RPC 인수·table column·audit payload에 없다.

`game_characters`의 `legacy_shard`는 `SHA-1(UTF-8 legacy_name_key)` 첫 바이트의 lowercase hex 두 글자다. 이는 C player path의 shard와 맞아야 한다. canonicalizer 불일치·payload/file mismatch와 예약 이름 `.`/`..`는 거부되며, backfill 도구가 위반 row를 quarantine해야 한다. migration은 이를 자동 보정하지 않는다.

## imported_unclaimed 검토 manifest (dry-run 전용)

DB backfill이나 claim/link 전에, 격리된 legacy host에서 만든 metadata-only inventory JSONL을 다음 도구로 검토용 manifest로 바꾼다.

```sh
python3 scripts/build-imported-unclaimed-manifest.py \
  --inventory /secure/player-inventory.jsonl \
  --output /secure/imported-unclaimed-manifest.json
```

이 도구는 DB·Supabase·network·player file을 열거나 변경하지 않으며 `--apply` 옵션도 없다. Manifest v1에는 canonical `legacy_name_key`, 계산된 shard, source SHA-256/size만 candidate로 남고, path·raw payload·password·credential·game state는 포함하지 않는다. 중복 canonical key, 이름/shard/digest/metadata 오류는 candidate 대신 deterministic rejection record로 분리하며, rejection이 하나라도 있으면 exit code 1이다.

`--output`은 아직 존재하지 않는 최종 파일명에만 사용할 수 있다. 도구는 지정한 trusted parent 안에 owner read/write 전용(`0600`) temporary regular file을 만들고, deterministic closed JSON을 모두 write·`fsync`·close한 뒤 hard-link no-replace로 최종 이름을 원자적으로 publish한다. 따라서 write/encoding/`fsync` 실패나 publish 경쟁 시 기존 일반 파일·symlink·hardlink·directory를 truncate·replace·follow하지 않으며, 최종 이름이 없던 경우에는 불완전 manifest가 최종 이름으로 나타나지 않는다. 오류 때 temporary artifact는 최선으로 삭제하지만, 프로세스 강제 종료·storage/cleanup 오류 뒤에는 trusted parent에 `.final-name.*.tmp`가 남을 수 있으므로 운영자가 검토·삭제해야 한다; link가 이미 성공한 뒤 cleanup 오류가 나면 최종 manifest는 완전한 상태로 남을 수 있다. 지정한 parent directory는 사전에 존재하고 운영자가 신뢰·접근 통제해야 하며, 실행 중 비신뢰 주체가 parent 또는 그 경로 구성요소를 쓰거나 rename할 수 없어야 한다. 도구는 parent를 만들거나 보호하지 않는다.

## 적용

배포 전 production DB snapshot을 만든 뒤 staging에서 먼저 실행한다.

```sh
supabase link --project-ref "$SUPABASE_PROJECT_REF"
supabase db push
```

CLI를 쓰지 않는 self-hosted 운영자는 migration runner로 동일 SQL을 한 번 적용한다. `service_role` 누락, FK 권한 부족, 기존 데이터가 새 shard/lifecycle check를 위반하는 경우에는 오류를 해결하기 전 진행하지 않는다. migration 파일을 수정하거나 down migration으로 claim/audit row를 삭제하지 않는다.

## contract 검증

적용한 **일회용** database에서 아래 psql script를 실행한다. fixture auth user, character, claim, audit row는 transaction 마지막의 rollback으로 남지 않는다.

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -f supabase/tests/game_identity_contract.sql
```

검증 항목은 다음과 같다.

- browser `authenticated` role은 unclaimed character를 읽지 못하고, active owner만 select한다.
- browser는 character/audit/lease/snapshot을 mutate하거나 claim RPC를 호출하지 못한다.
- service role의 첫 claim은 `imported_unclaimed → claiming → active`를 한 transaction으로 완료하고 audit을 한 번 남긴다.
- 같은 correlation UUID 재시도는 한 row를 반환하며 audit을 중복하지 않는다.
- 다른 actor의 claim/session lease, active lease 경쟁, wrong-gateway/expired renewal, invalid/reserved C name, nested camelCase `accessToken` audit payload은 거부된다. same-session begin은 같은 Gateway에서만 idempotent이고, exact live session/gateway만 renew하며, stale/wrong-gateway end는 replacement lease를 지우지 않는다.

동시 A/B claim은 RPC의 character row lock과 `private.game_character_claim_requests.correlation_id` primary key로 직렬화된다. 운영 배포 gate에서는 이 SQL contract 외에도 두 독립 Gateway request로 경쟁 claim integration test를 실행해 정확히 하나만 commit되는지 확인해야 한다.

## Gateway 호출 규칙

Gateway는 C의 `CHALLENGE`를 받은 뒤 service-only challenge RPC가 exact actor/correlation/character/SHA에 90초 `allowed`를 기록한 경우에만 C로 `ALLOW`를 보낸다. C는 그 전에는 password prompt를 내지 않고, old password를 정확히 한 번 비교한 직후 같은 파일의 SHA를 다시 확인해 `VERIFIED`를 보낸다. Gateway는 같은 allow가 살아 있을 때만 final claim RPC를 호출하며, transport 결과가 불확정인 경우에만 같은 correlation/payload를 한 번 재시도한다. 운영자가 challenge ledger를 우회해 RPC를 임의 호출해서는 안 된다. 성공 결과의 `character_id`/owner/lifecycle만 audit log에 남기고 password/proof/JWT/ticket 전문은 로그·DB·snapshot에 기록하지 않는다.

RPC가 `P0001`을 반환하면 이미 claim됨, correlation conflict, 또는 claim 불가 상태이므로 사용자에게 재시도 가능한 일반 오류만 보여 주고 운영 audit을 확인한다. `22023`은 Gateway input/actor configuration 오류다. claim DB가 실패하면 trusted admission을 열지 않는다.

## Rollback

이 단계의 rollback은 이전 검증 image/chart revision으로 되돌리고 이미 생성된 ownership/audit/request rows를 보존하는 것이다. migration을 되돌려 table을 drop하거나 claim을 unclaim하지 않는다. 운영 MUD는 `MUD_REQUIRE_TRUSTED_ADMISSION=1`을 유지하며, 인터넷에서 legacy password route를 다시 여는 것을 rollback으로 사용하지 않는다.
