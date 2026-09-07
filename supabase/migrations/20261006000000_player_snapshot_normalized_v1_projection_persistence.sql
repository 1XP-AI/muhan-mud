-- 2026-10-06: immutable persistence for the Node/Rust reviewed numeric-only
-- PlayerSnapshotNormalizedV1 projection. This is detached evidence, never a
-- loader, relay, or gameplay authority. SQL validates the safe projection
-- contract only; it never decodes PlayerSnapshotV1 artifact bytes.

create table if not exists private.game_character_player_snapshot_normalized_v1_projections (
  character_id uuid not null,
  command_id uuid not null,
  receipt_request_sha256 text not null,
  writer_instance_id uuid not null,
  writer_epoch bigint not null,
  writer_revision bigint not null,
  source_post_sha256 text not null,
  source_octets bigint not null,
  snapshot_sha256 text not null,
  snapshot_octets bigint not null,
  projection_format text not null,
  projection_version integer not null,
  projection_algorithm text not null,
  canonical_digest text not null,
  level smallint not null,
  hp_max smallint not null,
  hp_current smallint not null,
  mp_max smallint not null,
  mp_current smallint not null,
  experience bigint not null,
  gold bigint not null,
  item_count integer not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_player_snapshot_normalized_v1_projections_pk
    primary key (character_id, command_id),
  constraint game_character_player_snapshot_normalized_v1_pro_16d7a2d90b
    unique (character_id, writer_revision),
  constraint game_character_player_snapshot_normalized_v1_pro_3cb94e2e45
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_v1_artifacts(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_normalized_v1_pro_21eb37b10e
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_normalized_v1_pro_0b8fcdcdb0
    check (writer_epoch between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_normalized_v1_pro_ce68cfdeeb
    check (writer_revision between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_normalized_v1_pro_074e4816fe
    check (source_post_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_normalized_v1_pro_2d9f79ca6d
    check (source_octets between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_normalized_v1_pro_02e174aa3b
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_normalized_v1_pro_7614519833
    check (snapshot_octets between 48 and 4194352),
  constraint game_character_player_snapshot_normalized_v1_pro_9bf3fb84b0
    check (projection_format = 'player-snapshot-v1-normalized-projection'),
  constraint game_character_player_snapshot_normalized_v1_pro_08013e274a
    check (projection_version = 1),
  constraint game_character_player_snapshot_normalized_v1_pro_817eeca2da
    check (projection_algorithm = 'sha-256'),
  constraint game_character_player_snapshot_normalized_v1_pro_31b784fa33
    check (canonical_digest ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_normalized_v1_pro_b10b66ac8d
    check (level between 0 and 255),
  constraint game_character_player_snapshot_normalized_v1_pro_56ec87a685
    check (hp_current <= hp_max),
  constraint game_character_player_snapshot_normalized_v1_pro_af1e192fbf
    check (mp_current <= mp_max),
  constraint game_character_player_snapshot_normalized_v1_pro_e2ab36263f
    check (item_count between 0 and 8192)
);

create table if not exists private.game_character_player_snapshot_normalized_v1_projection_daily (
  character_id uuid not null,
  command_id uuid not null,
  slot smallint not null,
  max_value smallint not null,
  current_value smallint not null,
  last_used bigint not null,
  constraint game_character_player_snapshot_normalized_v1_pro_1981f98405
    primary key (character_id, command_id, slot),
  constraint game_character_player_snapshot_normalized_v1_pro_fe5c399f4a
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_normalized_v1_projections(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_normalized_v1_pro_91e5147b46
    check (slot between 0 and 9),
  constraint game_character_player_snapshot_normalized_v1_pro_b35deb9858
    check (max_value between 0 and 255),
  constraint game_character_player_snapshot_normalized_v1_pro_aea689124c
    check (current_value between 0 and max_value)
);

create table if not exists private.game_character_player_snapshot_normalized_v1_projection_timers (
  character_id uuid not null,
  command_id uuid not null,
  slot smallint not null,
  interval_value bigint not null,
  last_used bigint not null,
  misc smallint not null,
  constraint game_character_player_snapshot_normalized_v1_pro_c987ec6bf9
    primary key (character_id, command_id, slot),
  constraint game_character_player_snapshot_normalized_v1_pro_d83738a7d5
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_normalized_v1_projections(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_normalized_v1_pro_35fe37bcdc
    check (slot between 0 and 44)
);

create table if not exists private.game_character_player_snapshot_normalized_v1_projection_items (
  character_id uuid not null,
  command_id uuid not null,
  item_index integer not null,
  parent_index integer,
  child_index integer not null,
  value bigint not null,
  weight smallint not null,
  type_code smallint not null,
  adjustment smallint not null,
  shots_max smallint not null,
  shots_current smallint not null,
  ndice smallint not null,
  sdice smallint not null,
  pdice smallint not null,
  armor smallint not null,
  wear_flag smallint not null,
  magic_power smallint not null,
  magic_realm smallint not null,
  special smallint not null,
  constraint game_character_player_snapshot_normalized_v1_pro_ed2987ff12
    primary key (character_id, command_id, item_index),
  constraint game_character_player_snapshot_normalized_v1_pro_347112ce45
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_normalized_v1_projections(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_normalized_v1_pro_83dff33cc9
    foreign key (character_id, command_id, parent_index)
    references private.game_character_player_snapshot_normalized_v1_projection_items(character_id, command_id, item_index)
    on delete restrict,
  constraint game_character_player_snapshot_normalized_v1_pro_90c285324a
    check (item_index between 0 and 8191),
  constraint game_character_player_snapshot_normalized_v1_pro_7368c16187
    check (parent_index is null or parent_index between 0 and 8191 and parent_index < item_index),
  constraint game_character_player_snapshot_normalized_v1_pro_f59b0dba86
    check (child_index between 0 and 4095),
  constraint game_character_player_snapshot_normalized_v1_pro_af62c7de77
    check (type_code between -128 and 127 and adjustment between -128 and 127
      and armor between -128 and 127 and wear_flag between -128 and 127
      and magic_power between -128 and 127 and magic_realm between -128 and 127),
  constraint game_character_player_snapshot_normalized_v1_pro_3b5dc2da9e
    check (shots_current <= shots_max)
);

alter table private.game_character_player_snapshot_normalized_v1_projections enable row level security;
alter table private.game_character_player_snapshot_normalized_v1_projection_daily enable row level security;
alter table private.game_character_player_snapshot_normalized_v1_projection_timers enable row level security;
alter table private.game_character_player_snapshot_normalized_v1_projection_items enable row level security;

create or replace function private.player_snapshot_normalized_v1_projection_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotNormalizedV1 projections are immutable';
end;
$$;

drop trigger if exists game_character_player_snapshot_normalized_v1_projections_immutable
  on private.game_character_player_snapshot_normalized_v1_projections;
create trigger game_character_player_snapshot_normalized_v1_projections_immutable
  before update or delete on private.game_character_player_snapshot_normalized_v1_projections
  for each row execute function private.player_snapshot_normalized_v1_projection_immutable();
drop trigger if exists game_character_player_snapshot_normalized_v1_projection_daily_immutable
  on private.game_character_player_snapshot_normalized_v1_projection_daily;
create trigger game_character_player_snapshot_normalized_v1_projection_daily_immutable
  before update or delete on private.game_character_player_snapshot_normalized_v1_projection_daily
  for each row execute function private.player_snapshot_normalized_v1_projection_immutable();
drop trigger if exists game_character_player_snapshot_normalized_v1_projection_timers_immutable
  on private.game_character_player_snapshot_normalized_v1_projection_timers;
create trigger game_character_player_snapshot_normalized_v1_projection_timers_immutable
  before update or delete on private.game_character_player_snapshot_normalized_v1_projection_timers
  for each row execute function private.player_snapshot_normalized_v1_projection_immutable();
drop trigger if exists game_character_player_snapshot_normalized_v1_projection_items_immutable
  on private.game_character_player_snapshot_normalized_v1_projection_items;
create trigger game_character_player_snapshot_normalized_v1_projection_items_immutable
  before update or delete on private.game_character_player_snapshot_normalized_v1_projection_items
  for each row execute function private.player_snapshot_normalized_v1_projection_immutable();

create or replace function private.player_snapshot_normalized_v1_projection_closed_object(
  p_value jsonb,
  p_keys text[]
)
returns boolean
language sql
immutable
strict
set search_path = pg_catalog
as $$
  select jsonb_typeof(p_value) = 'object'
    and coalesce((select array_agg(key order by key) from jsonb_object_keys(p_value) as key), array[]::text[]) = p_keys;
$$;

create or replace function private.player_snapshot_normalized_v1_projection_integer(
  p_value jsonb,
  p_lower bigint,
  p_upper bigint
)
returns bigint
language plpgsql
immutable
strict
set search_path = pg_catalog
as $$
declare
  v_text text;
  v_value bigint;
begin
  v_text := p_value #>> '{}';
  if jsonb_typeof(p_value) <> 'number'
     or v_text !~ '^(0|-?[1-9][0-9]*)$' then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;
  begin
    v_value := v_text::bigint;
  exception when numeric_value_out_of_range then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end;
  if v_value not between p_lower and p_upper then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;
  return v_value;
end;
$$;

create or replace function private.record_player_snapshot_normalized_v1_projection_for_receipt(
  p_character_id uuid,
  p_command_id uuid,
  p_receipt_request_sha256 text,
  p_source_post_sha256 text,
  p_source_octets bigint,
  p_projection jsonb
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
  v_existing private.game_character_player_snapshot_normalized_v1_projections%rowtype;
  v_player jsonb;
  v_daily jsonb;
  v_timers jsonb;
  v_items jsonb;
  v_entry jsonb;
  v_index integer;
  v_parent integer;
  v_expected integer;
  v_depth integer;
  v_roots integer := 0;
  v_children integer[] := array_fill(0, array[8192]);
  v_depths integer[] := array_fill(0, array[8192]);
  v_ancestors integer[] := array[]::integer[];
  v_level smallint;
  v_hp_max smallint;
  v_hp_current smallint;
  v_mp_max smallint;
  v_mp_current smallint;
  v_experience bigint;
  v_gold bigint;
  v_digest text;
  v_bytes bytea := decode('6d7568616e2f706c617965722d736e617073686f742d6e6f726d616c697a65642d763100', 'hex');
  v_count integer;
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotNormalizedV1 projection writer session identity is invalid';
  end if;
  if p_character_id is null or p_command_id is null
     or p_receipt_request_sha256 is null or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_post_sha256 is null or p_source_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_octets is null or p_source_octets not between 1 and 9223372036854775807
     or p_projection is null
     or not private.player_snapshot_normalized_v1_projection_closed_object(
       p_projection, array['algorithm', 'canonical_digest', 'format', 'player', 'version']::text[]
     )
     or p_projection->>'format' <> 'player-snapshot-v1-normalized-projection'
     or p_projection->>'algorithm' <> 'sha-256' then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection arguments are invalid';
  end if;
  if private.player_snapshot_normalized_v1_projection_integer(p_projection->'version', 1, 1) <> 1
     or jsonb_typeof(p_projection->'canonical_digest') <> 'string'
     or p_projection->>'canonical_digest' !~ '^[0-9a-f]{64}$' then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;

  v_player := p_projection->'player';
  if not private.player_snapshot_normalized_v1_projection_closed_object(
       v_player, array['daily', 'experience', 'gold', 'hp_current', 'hp_max', 'items', 'level', 'mp_current', 'mp_max', 'timers']::text[]
     ) then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;
  v_level := private.player_snapshot_normalized_v1_projection_integer(v_player->'level', 0, 255)::smallint;
  v_hp_max := private.player_snapshot_normalized_v1_projection_integer(v_player->'hp_max', -32768, 32767)::smallint;
  v_hp_current := private.player_snapshot_normalized_v1_projection_integer(v_player->'hp_current', -32768, 32767)::smallint;
  v_mp_max := private.player_snapshot_normalized_v1_projection_integer(v_player->'mp_max', -32768, 32767)::smallint;
  v_mp_current := private.player_snapshot_normalized_v1_projection_integer(v_player->'mp_current', -32768, 32767)::smallint;
  v_experience := private.player_snapshot_normalized_v1_projection_integer(v_player->'experience', -9223372036854775808, 9223372036854775807);
  v_gold := private.player_snapshot_normalized_v1_projection_integer(v_player->'gold', -9223372036854775808, 9223372036854775807);
  if v_hp_current > v_hp_max or v_mp_current > v_mp_max then
    raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;

  v_daily := v_player->'daily';
  v_timers := v_player->'timers';
  v_items := v_player->'items';
  if jsonb_typeof(v_daily) <> 'array' or jsonb_typeof(v_timers) <> 'array'
     or jsonb_typeof(v_items) <> 'array' then
    raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;
  if jsonb_array_length(v_daily) <> 10 or jsonb_array_length(v_timers) <> 45
     or jsonb_array_length(v_items) > 8192 then
    raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;

  v_bytes := v_bytes || int2send(1::smallint) || set_byte(decode('00', 'hex'), 0, v_level);
  v_bytes := v_bytes || int2send(v_hp_max) || int2send(v_hp_current) || int2send(v_mp_max) || int2send(v_mp_current)
    || int8send(v_experience) || int8send(v_gold);
  for v_entry, v_index in select value, ordinality - 1 from jsonb_array_elements(v_daily) with ordinality loop
    if not private.player_snapshot_normalized_v1_projection_closed_object(v_entry, array['current', 'last_used', 'max']::text[]) then
      raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
    end if;
    if private.player_snapshot_normalized_v1_projection_integer(v_entry->'current', 0, 255)
         > private.player_snapshot_normalized_v1_projection_integer(v_entry->'max', 0, 255) then
      raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
    end if;
    v_bytes := v_bytes
      || set_byte(decode('00', 'hex'), 0, private.player_snapshot_normalized_v1_projection_integer(v_entry->'max', 0, 255)::integer)
      || set_byte(decode('00', 'hex'), 0, private.player_snapshot_normalized_v1_projection_integer(v_entry->'current', 0, 255)::integer)
      || int8send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'last_used', -9223372036854775808, 9223372036854775807));
  end loop;
  for v_entry, v_index in select value, ordinality - 1 from jsonb_array_elements(v_timers) with ordinality loop
    if not private.player_snapshot_normalized_v1_projection_closed_object(v_entry, array['interval', 'last_used', 'misc']::text[]) then
      raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
    end if;
    v_bytes := v_bytes
      || int8send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'interval', -9223372036854775808, 9223372036854775807))
      || int8send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'last_used', -9223372036854775808, 9223372036854775807))
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'misc', -32768, 32767)::smallint);
  end loop;
  v_count := jsonb_array_length(v_items);
  v_bytes := v_bytes || int4send(v_count);
  for v_entry, v_index in select value, ordinality - 1 from jsonb_array_elements(v_items) with ordinality loop
    if not private.player_snapshot_normalized_v1_projection_closed_object(v_entry, array[
      'adjustment', 'armor', 'child_index', 'magic_power', 'magic_realm', 'ndice', 'parent_index', 'pdice',
      'sdice', 'shots_current', 'shots_max', 'special', 'type_code', 'value', 'wear_flag', 'weight'
    ]::text[]) then
      raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
    end if;
    if jsonb_typeof(v_entry->'parent_index') = 'null' then
      v_parent := null;
    else
      v_parent := private.player_snapshot_normalized_v1_projection_integer(v_entry->'parent_index', 0, 8191)::integer;
    end if;
    if v_parent is null then
      v_expected := v_roots;
      v_roots := v_roots + 1;
      v_ancestors := array[]::integer[];
      v_depth := 1;
      if v_roots > 4096 then
        raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
      end if;
    else
      if v_parent >= v_index or array_position(v_ancestors, v_parent) is null then
        raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
      end if;
      v_expected := v_children[v_parent + 1];
      v_children[v_parent + 1] := v_expected + 1;
      v_ancestors := v_ancestors[1:array_position(v_ancestors, v_parent)];
      v_depth := v_depths[v_parent + 1] + 1;
      if v_children[v_parent + 1] > 4096 then
        raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
      end if;
    end if;
    if private.player_snapshot_normalized_v1_projection_integer(v_entry->'child_index', 0, 4095) <> v_expected
       or v_depth > 64
       or private.player_snapshot_normalized_v1_projection_integer(v_entry->'shots_current', -32768, 32767)
          > private.player_snapshot_normalized_v1_projection_integer(v_entry->'shots_max', -32768, 32767) then
      raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
    end if;
    v_ancestors := array_append(v_ancestors, v_index);
    v_depths[v_index + 1] := v_depth;
    v_bytes := v_bytes
      || int4send(coalesce(v_parent, -1))
      || int4send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'child_index', 0, 4095)::integer)
      || int8send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'value', -9223372036854775808, 9223372036854775807))
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'weight', -32768, 32767)::smallint)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'type_code', -128, 127)::smallint) from 2 for 1)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'adjustment', -128, 127)::smallint) from 2 for 1)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'shots_max', -32768, 32767)::smallint)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'shots_current', -32768, 32767)::smallint)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'ndice', -32768, 32767)::smallint)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'sdice', -32768, 32767)::smallint)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'pdice', -32768, 32767)::smallint)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'armor', -128, 127)::smallint) from 2 for 1)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'wear_flag', -128, 127)::smallint) from 2 for 1)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'magic_power', -128, 127)::smallint) from 2 for 1)
      || substring(int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'magic_realm', -128, 127)::smallint) from 2 for 1)
      || int2send(private.player_snapshot_normalized_v1_projection_integer(v_entry->'special', -32768, 32767)::smallint);
  end loop;
  v_digest := encode(public.digest(v_bytes, 'sha256'), 'hex');
  if v_digest <> p_projection->>'canonical_digest' then
    raise exception using errcode = '22023', message = 'PlayerSnapshotNormalizedV1 projection is invalid';
  end if;

  -- Bind to M3/M4 receipt, source, and snapshot metadata. Deliberately do not
  -- call payload validators, digest raw bytes, or otherwise decode the artifact.
  select * into v_receipt from private.game_character_shadow_receipts
   where character_id = p_character_id and command_id = p_command_id for share;
  select * into v_manifest from private.game_character_m4_file_snapshot_manifests
   where character_id = p_character_id and command_id = p_command_id for share;
  select * into v_artifact from private.game_character_player_snapshot_v1_artifacts
   where character_id = p_character_id and command_id = p_command_id for share;
  if v_receipt.character_id is null or v_manifest.character_id is null or v_artifact.character_id is null
     or v_receipt.request_sha256 <> p_receipt_request_sha256 or v_receipt.post_sha256 <> p_source_post_sha256
     or v_manifest.world_id <> v_receipt.world_id or v_manifest.legacy_name_key <> v_receipt.legacy_name_key
     or v_manifest.receipt_request_sha256 <> v_receipt.request_sha256
     or v_manifest.writer_instance_id <> v_receipt.writer_instance_id or v_manifest.writer_epoch <> v_receipt.writer_epoch
     or v_manifest.writer_revision <> v_receipt.writer_revision or v_manifest.file_post_sha256 <> v_receipt.post_sha256
     or v_manifest.storage_format <> v_receipt.storage_format or v_manifest.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_manifest.snapshot_format <> 'legacy-file-manifest-v1' or v_manifest.snapshot_sha256 <> v_receipt.post_sha256
     or v_manifest.snapshot_octets <> p_source_octets
     or v_artifact.world_id <> v_receipt.world_id or v_artifact.legacy_name_key <> v_receipt.legacy_name_key
     or v_artifact.receipt_request_sha256 <> v_receipt.request_sha256
     or v_artifact.writer_instance_id <> v_receipt.writer_instance_id or v_artifact.writer_epoch <> v_receipt.writer_epoch
     or v_artifact.writer_revision <> v_receipt.writer_revision or v_artifact.source_post_sha256 <> v_receipt.post_sha256
     or v_artifact.source_octets <> v_manifest.snapshot_octets or v_artifact.source_octets <> p_source_octets
     or v_artifact.storage_format <> v_receipt.storage_format or v_artifact.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_artifact.snapshot_format <> 'player-snapshot-v1' then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotNormalizedV1 projection has no matching immutable evidence metadata';
  end if;

  insert into private.game_character_player_snapshot_normalized_v1_projections (
    character_id, command_id, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision,
    source_post_sha256, source_octets, snapshot_sha256, snapshot_octets,
    projection_format, projection_version, projection_algorithm, canonical_digest,
    level, hp_max, hp_current, mp_max, mp_current, experience, gold, item_count
  ) values (
    p_character_id, p_command_id, v_receipt.request_sha256,
    v_receipt.writer_instance_id, v_receipt.writer_epoch, v_receipt.writer_revision,
    v_receipt.post_sha256, v_artifact.source_octets, v_artifact.snapshot_sha256, v_artifact.snapshot_octets,
    'player-snapshot-v1-normalized-projection', 1, 'sha-256', v_digest,
    v_level, v_hp_max, v_hp_current, v_mp_max, v_mp_current, v_experience, v_gold, v_count
  ) on conflict do nothing returning * into v_existing;
  if found then
    insert into private.game_character_player_snapshot_normalized_v1_projection_daily (
      character_id, command_id, slot, max_value, current_value, last_used
    )
    select p_character_id, p_command_id, ordinality - 1,
      private.player_snapshot_normalized_v1_projection_integer(value->'max', 0, 255)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'current', 0, 255)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807)
      from jsonb_array_elements(v_daily) with ordinality;
    insert into private.game_character_player_snapshot_normalized_v1_projection_timers (
      character_id, command_id, slot, interval_value, last_used, misc
    )
    select p_character_id, p_command_id, ordinality - 1,
      private.player_snapshot_normalized_v1_projection_integer(value->'interval', -9223372036854775808, 9223372036854775807),
      private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807),
      private.player_snapshot_normalized_v1_projection_integer(value->'misc', -32768, 32767)::smallint
      from jsonb_array_elements(v_timers) with ordinality;
    insert into private.game_character_player_snapshot_normalized_v1_projection_items (
      character_id, command_id, item_index, parent_index, child_index, value, weight, type_code, adjustment,
      shots_max, shots_current, ndice, sdice, pdice, armor, wear_flag, magic_power, magic_realm, special
    )
    select p_character_id, p_command_id, ordinality - 1,
      case when jsonb_typeof(value->'parent_index') = 'null' then null else private.player_snapshot_normalized_v1_projection_integer(value->'parent_index', 0, 8191)::integer end,
      private.player_snapshot_normalized_v1_projection_integer(value->'child_index', 0, 4095)::integer,
      private.player_snapshot_normalized_v1_projection_integer(value->'value', -9223372036854775808, 9223372036854775807),
      private.player_snapshot_normalized_v1_projection_integer(value->'weight', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'type_code', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'adjustment', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'shots_max', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'shots_current', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'ndice', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'sdice', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'pdice', -32768, 32767)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'armor', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'wear_flag', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'magic_power', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'magic_realm', -128, 127)::smallint,
      private.player_snapshot_normalized_v1_projection_integer(value->'special', -32768, 32767)::smallint
      from jsonb_array_elements(v_items) with ordinality;
    return query select 'RECORDED'::text;
    return;
  end if;

  select * into v_existing from private.game_character_player_snapshot_normalized_v1_projections
   where character_id = p_character_id and command_id = p_command_id for share;
  if v_existing.character_id is not null
     and v_existing.receipt_request_sha256 = v_receipt.request_sha256
     and v_existing.writer_instance_id = v_receipt.writer_instance_id and v_existing.writer_epoch = v_receipt.writer_epoch
     and v_existing.writer_revision = v_receipt.writer_revision and v_existing.source_post_sha256 = v_receipt.post_sha256
     and v_existing.source_octets = v_artifact.source_octets and v_existing.snapshot_sha256 = v_artifact.snapshot_sha256
     and v_existing.snapshot_octets = v_artifact.snapshot_octets
     and v_existing.projection_format = 'player-snapshot-v1-normalized-projection' and v_existing.projection_version = 1
     and v_existing.projection_algorithm = 'sha-256' and v_existing.canonical_digest = v_digest
     and v_existing.level = v_level and v_existing.hp_max = v_hp_max and v_existing.hp_current = v_hp_current
     and v_existing.mp_max = v_mp_max and v_existing.mp_current = v_mp_current and v_existing.experience = v_experience
     and v_existing.gold = v_gold and v_existing.item_count = v_count
     and not exists ((select ordinality - 1, private.player_snapshot_normalized_v1_projection_integer(value->'max', 0, 255)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'current', 0, 255)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807) from jsonb_array_elements(v_daily) with ordinality) except (select slot, max_value, current_value, last_used from private.game_character_player_snapshot_normalized_v1_projection_daily where character_id = p_character_id and command_id = p_command_id))
     and not exists ((select slot, max_value, current_value, last_used from private.game_character_player_snapshot_normalized_v1_projection_daily where character_id = p_character_id and command_id = p_command_id) except (select ordinality - 1, private.player_snapshot_normalized_v1_projection_integer(value->'max', 0, 255)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'current', 0, 255)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807) from jsonb_array_elements(v_daily) with ordinality))
     and not exists ((select ordinality - 1, private.player_snapshot_normalized_v1_projection_integer(value->'interval', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'misc', -32768, 32767)::smallint from jsonb_array_elements(v_timers) with ordinality) except (select slot, interval_value, last_used, misc from private.game_character_player_snapshot_normalized_v1_projection_timers where character_id = p_character_id and command_id = p_command_id))
     and not exists ((select slot, interval_value, last_used, misc from private.game_character_player_snapshot_normalized_v1_projection_timers where character_id = p_character_id and command_id = p_command_id) except (select ordinality - 1, private.player_snapshot_normalized_v1_projection_integer(value->'interval', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'last_used', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'misc', -32768, 32767)::smallint from jsonb_array_elements(v_timers) with ordinality))
     and not exists ((select ordinality - 1, case when jsonb_typeof(value->'parent_index') = 'null' then null else private.player_snapshot_normalized_v1_projection_integer(value->'parent_index', 0, 8191)::integer end, private.player_snapshot_normalized_v1_projection_integer(value->'child_index', 0, 4095)::integer, private.player_snapshot_normalized_v1_projection_integer(value->'value', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'weight', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'type_code', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'adjustment', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'shots_max', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'shots_current', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'ndice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'sdice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'pdice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'armor', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'wear_flag', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'magic_power', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'magic_realm', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'special', -32768, 32767)::smallint from jsonb_array_elements(v_items) with ordinality) except (select item_index, parent_index, child_index, value, weight, type_code, adjustment, shots_max, shots_current, ndice, sdice, pdice, armor, wear_flag, magic_power, magic_realm, special from private.game_character_player_snapshot_normalized_v1_projection_items where character_id = p_character_id and command_id = p_command_id))
     and not exists ((select item_index, parent_index, child_index, value, weight, type_code, adjustment, shots_max, shots_current, ndice, sdice, pdice, armor, wear_flag, magic_power, magic_realm, special from private.game_character_player_snapshot_normalized_v1_projection_items where character_id = p_character_id and command_id = p_command_id) except (select ordinality - 1, case when jsonb_typeof(value->'parent_index') = 'null' then null else private.player_snapshot_normalized_v1_projection_integer(value->'parent_index', 0, 8191)::integer end, private.player_snapshot_normalized_v1_projection_integer(value->'child_index', 0, 4095)::integer, private.player_snapshot_normalized_v1_projection_integer(value->'value', -9223372036854775808, 9223372036854775807), private.player_snapshot_normalized_v1_projection_integer(value->'weight', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'type_code', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'adjustment', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'shots_max', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'shots_current', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'ndice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'sdice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'pdice', -32768, 32767)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'armor', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'wear_flag', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'magic_power', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'magic_realm', -128, 127)::smallint, private.player_snapshot_normalized_v1_projection_integer(value->'special', -32768, 32767)::smallint from jsonb_array_elements(v_items) with ordinality)) then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;
  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotNormalizedV1 projection conflicts with immutable evidence';
end;
$$;

grant usage on schema private to mud_writer;
revoke all on table private.game_character_player_snapshot_normalized_v1_projections,
  private.game_character_player_snapshot_normalized_v1_projection_daily,
  private.game_character_player_snapshot_normalized_v1_projection_timers,
  private.game_character_player_snapshot_normalized_v1_projection_items
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login, mud_replay_reader_login;
revoke all on function private.player_snapshot_normalized_v1_projection_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login, mud_replay_reader_login;
revoke all on function private.player_snapshot_normalized_v1_projection_closed_object(jsonb,text[])
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login, mud_replay_reader_login;
revoke all on function private.player_snapshot_normalized_v1_projection_integer(jsonb,bigint,bigint)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login, mud_replay_reader_login;
revoke all on function private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login, mud_replay_reader_login;
grant execute on function private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)
  to mud_writer;

comment on table private.game_character_player_snapshot_normalized_v1_projections is
  'Immutable receipt/artifact-metadata-bound PlayerSnapshotNormalizedV1 evidence; stores only reviewed numeric fields, never raw artifact bytes or gameplay authority.';
comment on function private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb) is
  'mud_writer_login SET ROLE mud_writer only. Validates the Node/Rust canonical numeric projection and records or exactly retries immutable evidence without decoding artifact bytes.';
