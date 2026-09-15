-- 2026-09-05: bind legacy password verification to the exact player file
-- fingerprint imported into the identity index.
--
-- C computes this SHA-256 only after loading the named player and verifying
-- its legacy password. The Gateway forwards it as non-secret evidence; the
-- database must compare it under the character row lock before ownership can
-- move. Passwords, admission tickets, JWTs, and terminal input remain C-only.

-- The original primitive predates terminal onboarding and has no fingerprint
-- argument. Keep it as an internal SECURITY DEFINER implementation detail,
-- but remove the service role's direct bypass path.
revoke all on function public.claim_legacy_game_character(text, text, uuid, uuid)
  from public, anon, authenticated, service_role;

-- Fail closed for an old Gateway during a rolling migration without dropping
-- its signature. The feature is default-off, and only the five-argument
-- fingerprint-bound overload is executable by service_role.
revoke all on function public.claim_legacy_game_character_onboarding(text, text, uuid, uuid)
  from public, anon, authenticated, service_role;

create or replace function public.claim_legacy_game_character_onboarding(
  p_world_id text,
  p_legacy_name_key text,
  p_imported_file_sha256 text,
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  character_id uuid,
  lifecycle public.character_lifecycle,
  owner_user_id uuid,
  claimed_at timestamptz,
  onboarding_status text,
  imported_file_sha256 text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
#variable_conflict use_column
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_character_id uuid;
  v_lifecycle public.character_lifecycle;
  v_owner_user_id uuid;
  v_claimed_at timestamptz;
  v_completed_at timestamptz := now();
begin
  if nullif(btrim(p_world_id), '') is null
     or nullif(btrim(p_legacy_name_key), '') is null
     or p_imported_file_sha256 is null
     or p_imported_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_actor_user_id is null
     or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'onboarding claim requires world, canonical name, file fingerprint, actor, and correlation id';
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found
     or v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> 'claim' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor claim request';
  end if;
  if v_intent.status not in ('started', 'finalized') then
    raise exception using errcode = 'P0001', message = 'invalid onboarding lifecycle transition to claim';
  end if;
  if v_intent.status = 'started' and v_intent.expires_at <= v_completed_at then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired before claim';
  end if;

  -- Hold the same character row lock across fingerprint comparison and the
  -- one-way ownership transition. NULL or a stale import digest is not proof.
  select * into v_character
    from public.game_characters
    where world_id = p_world_id and legacy_name_key = p_legacy_name_key
    for update;
  if not found
     or v_character.imported_file_sha256 is distinct from p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'verified player fingerprint does not match imported character';
  end if;

  select claimed.character_id, claimed.lifecycle,
         claimed.owner_user_id, claimed.claimed_at
    into v_character_id, v_lifecycle, v_owner_user_id, v_claimed_at
    from public.claim_legacy_game_character(
      p_world_id, p_legacy_name_key, p_actor_user_id, p_correlation_id
    ) as claimed;
  if not found
     or v_character_id <> v_character.id
     or v_owner_user_id <> p_actor_user_id
     or v_lifecycle <> 'active'
     or v_claimed_at is null then
    raise exception using errcode = 'P0001', message = 'claimed character result is inconsistent';
  end if;

  if v_intent.status = 'started' then
    update private.game_character_onboarding_intents
      set status = 'finalized', completed_at = v_completed_at
      where correlation_id = p_correlation_id;
  end if;

  return query select
    v_character_id,
    v_lifecycle,
    v_owner_user_id,
    v_claimed_at,
    'finalized'::text,
    v_character.imported_file_sha256;
end;
$$;

revoke all on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  from public, anon, authenticated;
grant execute on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  to service_role;

comment on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid) is
  'Gateway service_role only. After C verifies the legacy password, atomically matches its player SHA-256 to the imported identity row before ownership transfer; never accepts a password, proof, JWT, ticket, or terminal input.';
