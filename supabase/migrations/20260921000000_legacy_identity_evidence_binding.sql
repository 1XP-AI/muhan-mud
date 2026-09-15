-- 2026-09-21: bind V1 legacy identity evidence to the one-way onboarding
-- completion transaction. This stores only canonical metadata from the closed
-- C wire contract; it never stores a password, proof, ticket, JWT, or player
-- bytes, and it never makes Postgres an authority for legacy file inspection.

create table if not exists private.game_character_legacy_identity_evidence (
  correlation_id uuid primary key
    references private.game_character_onboarding_intents(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  mode text not null,
  world_id text not null,
  canonical_legacy_name text not null,
  legacy_shard char(2) not null,
  player_file_sha256 text not null,
  outcome text not null,
  evidence_version smallint not null,
  storage_format text not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_legacy_identity_evidence_mode check (mode in ('claim', 'provision')),
  constraint game_character_legacy_identity_evidence_name check (
    canonical_legacy_name = private.game_identity_canonical_legacy_name(canonical_legacy_name)
    and char_length(canonical_legacy_name) between 1 and 12
    and octet_length(canonical_legacy_name) <= 14
    and canonical_legacy_name !~ E'[[:cntrl:]/\\\\:]'
    and canonical_legacy_name not in ('.', '..')
  ),
  constraint game_character_legacy_identity_evidence_world check (
    char_length(world_id) between 1 and 64 and world_id !~ '[[:cntrl:]]'
  ),
  constraint game_character_legacy_identity_evidence_shard check (
    legacy_shard ~ '^[0-9a-f]{2}$'
    and legacy_shard = substr(
      encode(public.digest(convert_to(canonical_legacy_name, 'UTF8'), 'sha1'), 'hex'), 1, 2
    )
  ),
  constraint game_character_legacy_identity_evidence_sha256 check (
    player_file_sha256 ~ '^[0-9a-f]{64}$'
  ),
  constraint game_character_legacy_identity_evidence_outcome check (
    outcome in ('ok', 'not_found', 'corrupt', 'io_error', 'invalid_input')
  ),
  constraint game_character_legacy_identity_evidence_version check (evidence_version = 1),
  constraint game_character_legacy_identity_evidence_storage check (storage_format = 'player-v1')
);

alter table private.game_character_legacy_identity_evidence enable row level security;

revoke all on table private.game_character_legacy_identity_evidence
  from public, anon, authenticated, service_role;

-- This shard-aware overload is additive. The prior evidence RPC remains as a
-- rolling-compatible wrapper below; older finalize and claim RPCs also remain.
create or replace function public.finalize_game_character_legacy_identity_evidence(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_character_id uuid,
  p_outcome text,
  p_canonical_legacy_name text,
  p_player_file_sha256 text,
  p_evidence_version smallint,
  p_storage_format text,
  p_legacy_shard text
)
returns table (
  character_id uuid,
  actor_user_id uuid,
  mode text,
  lifecycle public.character_lifecycle,
  world_id text,
  canonical_legacy_name text,
  legacy_shard char(2),
  player_file_sha256 text,
  evidence_version smallint,
  storage_format text,
  recorded_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_evidence private.game_character_legacy_identity_evidence%rowtype;
  v_provisioning private.game_character_provisioning_requests%rowtype;
  v_claim_character_id uuid;
  v_claim_lifecycle public.character_lifecycle;
  v_claim_actor_user_id uuid;
  v_claimed_at timestamptz;
  v_now timestamptz;
begin
  if p_actor_user_id is null
     or p_correlation_id is null
     or p_character_id is null
     or p_outcome is distinct from 'ok'
     or p_evidence_version is distinct from 1::smallint
     or p_storage_format is distinct from 'player-v1'
     or p_player_file_sha256 is null
     or p_player_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_canonical_legacy_name is null
     or p_legacy_shard is null
     or p_legacy_shard !~ '^[0-9a-f]{2}$'
     or p_canonical_legacy_name <> private.game_identity_canonical_legacy_name(p_canonical_legacy_name) then
    raise exception using errcode = '22023',
      message = 'legacy identity evidence must be an exact successful V1 player-v1 tuple';
  end if;

  -- Intent is the correlation lock. Holding it through the ledger insert makes
  -- an exact retry observe the completed tuple instead of racing a duplicate.
  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence correlation is not owned by actor';
  end if;

  -- Claim completion must retain the established intent -> target advisory
  -- lock -> character lock order. Read only enough target metadata to derive
  -- the advisory key first; the authoritative row is locked immediately after.
  if v_intent.mode = 'claim' then
    select * into v_character
      from public.game_characters
      where id = p_character_id;
    if not found
       or v_character.legacy_name_key <> p_canonical_legacy_name
       or v_character.legacy_name <> p_canonical_legacy_name
       or v_character.legacy_shard <> p_legacy_shard then
      raise exception using errcode = 'P0001', message = 'legacy identity evidence does not match canonical character';
    end if;
    perform pg_advisory_xact_lock(
      hashtextextended(
        jsonb_build_array(v_character.world_id, p_canonical_legacy_name)::text,
        870254061
      )
    );
  end if;

  select * into v_character
    from public.game_characters
    where id = p_character_id
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence character is unavailable';
  end if;

  if v_character.legacy_name_key <> p_canonical_legacy_name
     or v_character.legacy_name <> p_canonical_legacy_name
     or v_character.legacy_shard <> p_legacy_shard then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence does not match locked character';
  end if;

  select * into v_evidence
    from private.game_character_legacy_identity_evidence
    where correlation_id = p_correlation_id
    for update;

  -- Authorization decisions happen only after every blocking lock above.
  v_now := clock_timestamp();

  if found then
    if v_evidence.actor_user_id <> p_actor_user_id
       or v_evidence.character_id <> p_character_id
       or v_evidence.mode <> v_intent.mode
       or v_evidence.world_id <> v_character.world_id
       or v_evidence.canonical_legacy_name <> p_canonical_legacy_name
       or v_evidence.legacy_shard <> p_legacy_shard
       or v_evidence.player_file_sha256 <> p_player_file_sha256
       or v_evidence.outcome <> p_outcome
       or v_evidence.evidence_version <> p_evidence_version
       or v_evidence.storage_format <> p_storage_format then
      raise exception using errcode = 'P0001', message = 'legacy identity evidence correlation conflicts with immutable tuple';
    end if;

    if v_character.world_id <> v_evidence.world_id
       or v_character.legacy_name <> v_evidence.canonical_legacy_name
       or v_character.legacy_name_key <> v_evidence.canonical_legacy_name
       or v_character.legacy_shard <> v_evidence.legacy_shard
       or v_character.storage_format <> 1 then
      raise exception using errcode = 'P0001', message = 'finalized evidence character identity is inconsistent';
    end if;

    if v_evidence.mode = 'provision' then
      select * into v_provisioning
        from private.game_character_provisioning_requests
        where correlation_id = p_correlation_id
        for update;
      if not found
         or v_provisioning.actor_user_id <> p_actor_user_id
         or v_provisioning.character_id <> p_character_id
         or v_provisioning.world_id <> v_evidence.world_id
         or v_provisioning.legacy_name_key <> v_evidence.canonical_legacy_name
         or v_provisioning.status <> 'finalized'
         or v_provisioning.saved_file_sha256 <> p_player_file_sha256
         or v_provisioning.storage_format <> 1
         or v_provisioning.finalized_at is null
         or v_provisioning.finalized_via not in ('finalize', 'reconcile')
         or v_character.lifecycle <> 'active'
         or v_character.owner_user_id <> p_actor_user_id
         or v_intent.status <> 'finalized' then
        raise exception using errcode = 'P0001', message = 'finalized provision evidence state is inconsistent';
      end if;
    elsif v_evidence.mode = 'claim' then
      if v_character.lifecycle <> 'active'
         or v_character.owner_user_id <> p_actor_user_id
         or v_character.imported_file_sha256 is distinct from p_player_file_sha256
         or v_intent.status <> 'finalized' then
        raise exception using errcode = 'P0001', message = 'finalized claim evidence state is inconsistent';
      end if;
    else
      raise exception using errcode = 'P0001', message = 'legacy identity evidence mode is inconsistent';
    end if;

    return query select
      v_evidence.character_id, v_evidence.actor_user_id, v_evidence.mode,
      v_character.lifecycle, v_evidence.world_id, v_evidence.canonical_legacy_name,
      v_evidence.legacy_shard,
      v_evidence.player_file_sha256, v_evidence.evidence_version,
      v_evidence.storage_format, v_evidence.recorded_at;
    return;
  end if;

  if v_intent.mode not in ('claim', 'provision')
     or v_character.legacy_name_key <> p_canonical_legacy_name
     or v_character.legacy_name <> p_canonical_legacy_name
     or v_character.legacy_shard <> p_legacy_shard
     or v_character.storage_format <> 1 then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence does not match canonical character';
  end if;

  if v_intent.mode = 'provision' then
    select * into v_provisioning
      from private.game_character_provisioning_requests
      where correlation_id = p_correlation_id
      for update;
    -- The provisioning request lock may have waited; never authorize an old
    -- intent expiry based on transaction-start time.
    v_now := clock_timestamp();
    if not found
       or v_intent.status <> 'provisioning'
       or v_intent.expires_at <= v_now
       or v_provisioning.actor_user_id <> p_actor_user_id
       or v_provisioning.character_id <> p_character_id
       or v_provisioning.world_id <> v_character.world_id
       or v_provisioning.legacy_name_key <> p_canonical_legacy_name
       or v_provisioning.status <> 'reserved'
       or v_character.lifecycle <> 'provisioning'
       or v_character.owner_user_id <> p_actor_user_id then
      raise exception using errcode = 'P0001', message = 'provision evidence is not authorized for current lifecycle';
    end if;

    perform 1
      from private.complete_game_character_provisioning(
        p_actor_user_id, p_correlation_id, p_player_file_sha256, 1::smallint, false
      );
  else
    -- Claim SHA is the imported-file fingerprint, not a newly saved file.
    if v_intent.status <> 'started'
       or v_intent.expires_at <= v_now
       or v_character.lifecycle <> 'imported_unclaimed'
       or v_character.owner_user_id is not null
       or v_character.storage_format <> 1
       or v_character.imported_file_sha256 is distinct from p_player_file_sha256 then
      raise exception using errcode = 'P0001', message = 'claim evidence is not authorized for imported character';
    end if;

    select claimed.character_id, claimed.lifecycle, claimed.owner_user_id, claimed.claimed_at
      into v_claim_character_id, v_claim_lifecycle, v_claim_actor_user_id, v_claimed_at
      from public.claim_legacy_game_character_onboarding(
        v_character.world_id, p_canonical_legacy_name, p_player_file_sha256,
        p_actor_user_id, p_correlation_id
      ) as claimed;
    if not found
       or v_claim_character_id <> p_character_id
       or v_claim_lifecycle <> 'active'
       or v_claim_actor_user_id <> p_actor_user_id
       or v_claimed_at is null then
      raise exception using errcode = 'P0001', message = 'claim evidence finalization result is inconsistent';
    end if;
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id;
  select * into v_character
    from public.game_characters
    where id = p_character_id;
  if not found
     or v_character.lifecycle <> 'active'
     or v_character.owner_user_id <> p_actor_user_id
     or v_character.legacy_name_key <> p_canonical_legacy_name
     or v_character.legacy_name <> p_canonical_legacy_name
     or v_character.legacy_shard <> p_legacy_shard
     or v_character.storage_format <> 1
     or v_intent.status <> 'finalized'
     or (v_intent.mode = 'claim' and v_character.imported_file_sha256 is distinct from p_player_file_sha256) then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence lifecycle transition is inconsistent';
  end if;

  if v_intent.mode = 'provision' then
    select * into v_provisioning
      from private.game_character_provisioning_requests
      where correlation_id = p_correlation_id
      for update;
    if not found
       or v_provisioning.actor_user_id <> p_actor_user_id
       or v_provisioning.character_id <> p_character_id
       or v_provisioning.world_id <> v_character.world_id
       or v_provisioning.legacy_name_key <> p_canonical_legacy_name
       or v_provisioning.status <> 'finalized'
       or v_provisioning.saved_file_sha256 <> p_player_file_sha256
       or v_provisioning.storage_format <> 1
       or v_provisioning.finalized_at is null
       or v_provisioning.finalized_via not in ('finalize', 'reconcile') then
      raise exception using errcode = 'P0001', message = 'provision evidence finalization receipt is inconsistent';
    end if;
  end if;

  insert into private.game_character_legacy_identity_evidence (
    correlation_id, actor_user_id, character_id, mode, world_id,
    canonical_legacy_name, legacy_shard, player_file_sha256, outcome,
    evidence_version, storage_format, recorded_at
  ) values (
    p_correlation_id, p_actor_user_id, p_character_id, v_intent.mode,
    v_character.world_id, p_canonical_legacy_name, p_legacy_shard,
    p_player_file_sha256, p_outcome, p_evidence_version, p_storage_format,
    clock_timestamp()
  ) returning * into v_evidence;

  return query select
    v_evidence.character_id, v_evidence.actor_user_id, v_evidence.mode,
    v_character.lifecycle, v_evidence.world_id, v_evidence.canonical_legacy_name,
    v_evidence.legacy_shard,
    v_evidence.player_file_sha256, v_evidence.evidence_version,
    v_evidence.storage_format, v_evidence.recorded_at;
end;
$$;

revoke all on function public.finalize_game_character_legacy_identity_evidence(
  uuid, uuid, uuid, text, text, text, smallint, text, text
) from public, anon, authenticated;
grant execute on function public.finalize_game_character_legacy_identity_evidence(
  uuid, uuid, uuid, text, text, text, smallint, text, text
) to service_role;

-- Keep the already-published eight-argument signature executable during a
-- rolling Gateway deployment. It cannot carry the wire shard, so it obtains
-- a target shard before the new overload takes and verifies its character lock.
create or replace function public.finalize_game_character_legacy_identity_evidence(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_character_id uuid,
  p_outcome text,
  p_canonical_legacy_name text,
  p_player_file_sha256 text,
  p_evidence_version smallint,
  p_storage_format text
)
returns table (
  character_id uuid,
  actor_user_id uuid,
  mode text,
  lifecycle public.character_lifecycle,
  canonical_legacy_name text,
  player_file_sha256 text,
  evidence_version smallint,
  storage_format text,
  recorded_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_legacy_shard char(2);
begin
  select c.legacy_shard into v_legacy_shard
    from public.game_characters c
    where c.id = p_character_id;
  if not found then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence character is unavailable';
  end if;

  return query select
    finalized.character_id, finalized.actor_user_id, finalized.mode,
    finalized.lifecycle, finalized.canonical_legacy_name,
    finalized.player_file_sha256, finalized.evidence_version,
    finalized.storage_format, finalized.recorded_at
  from public.finalize_game_character_legacy_identity_evidence(
    p_actor_user_id, p_correlation_id, p_character_id, p_outcome,
    p_canonical_legacy_name, p_player_file_sha256, p_evidence_version,
    p_storage_format, v_legacy_shard::text
  ) as finalized;
end;
$$;

revoke all on function public.finalize_game_character_legacy_identity_evidence(
  uuid, uuid, uuid, text, text, text, smallint, text
) from public, anon, authenticated;
grant execute on function public.finalize_game_character_legacy_identity_evidence(
  uuid, uuid, uuid, text, text, text, smallint, text
) to service_role;

comment on table private.game_character_legacy_identity_evidence is
  'Private immutable V1 legacy identity evidence ledger keyed by onboarding correlation and locked world/name/shard identity. Service_role has no direct table CRUD; all writes are lifecycle-bound security-definer RPC transactions.';
comment on function public.finalize_game_character_legacy_identity_evidence(uuid, uuid, uuid, text, text, text, smallint, text, text) is
  'Gateway service_role only. Accepts only canonical LegacyIdentityEvidenceV1 ok/player-v1 evidence, validates the lowercase two-hex wire shard against the locked character, and atomically binds actor/correlation/world/character/name/shard/SHA semantics in an immutable ledger row.';
comment on function public.finalize_game_character_legacy_identity_evidence(uuid, uuid, uuid, text, text, text, smallint, text) is
  'Gateway service_role rolling-compatibility wrapper. It derives the shard from the character row, then delegates to the shard-aware evidence finalizer; new evidence-aware callers must use the nine-argument overload.';
