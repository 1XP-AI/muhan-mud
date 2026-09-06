-- 2026-10-03: immutable, receipt/artifact-bound PlayerSnapshotV1 inventory
-- topology evidence. This stores only graph positions; it never copies object
-- attributes and has no effect on the established save authority.

create table if not exists private.game_character_player_snapshot_v1_inventory_graph_shadows (
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
  item_count integer not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_pk
    primary key (character_id, command_id),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_revision_unique
    unique (character_id, writer_revision),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_artifact_fk
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_v1_artifacts(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_request_hash_check
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_writer_epoch_check
    check (writer_epoch between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_writer_revision_check
    check (writer_revision between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_source_hash_check
    check (source_post_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_source_octets_check
    check (source_octets between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_snapshot_hash_check
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_snapshot_octets_check
    check (snapshot_octets between 48 and 4194352),
  constraint game_character_player_snapshot_v1_inventory_graph_shadows_item_count_check
    check (item_count between 0 and 8192)
);

create table if not exists private.game_character_player_snapshot_v1_inventory_graph_shadow_items (
  character_id uuid not null,
  command_id uuid not null,
  node_index integer not null,
  parent_node_index integer,
  sibling_ordinal integer not null,
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_pk
    primary key (character_id, command_id, node_index),
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_shadow_fk
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_v1_inventory_graph_shadows(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_parent_fk
    foreign key (character_id, command_id, parent_node_index)
    references private.game_character_player_snapshot_v1_inventory_graph_shadow_items(
      character_id, command_id, node_index
    )
    on delete restrict,
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_node_index_check
    check (node_index between 0 and 8191),
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_parent_index_check
    check (parent_node_index is null or parent_node_index between 0 and 8191),
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_parent_preorder_check
    check (parent_node_index is null or parent_node_index < node_index),
  constraint game_character_player_snapshot_v1_inventory_graph_shadow_items_sibling_ordinal_check
    check (sibling_ordinal between 0 and 4095)
);

alter table private.game_character_player_snapshot_v1_inventory_graph_shadows
  enable row level security;
alter table private.game_character_player_snapshot_v1_inventory_graph_shadow_items
  enable row level security;

create or replace function private.player_snapshot_v1_inventory_graph_shadow_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotV1 inventory graph shadows are immutable';
end;
$$;

drop trigger if exists game_character_player_snapshot_v1_inventory_graph_shadows_immutable
  on private.game_character_player_snapshot_v1_inventory_graph_shadows;
create trigger game_character_player_snapshot_v1_inventory_graph_shadows_immutable
  before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadows
  for each row execute function private.player_snapshot_v1_inventory_graph_shadow_immutable();

drop trigger if exists game_character_player_snapshot_v1_inventory_graph_shadow_items_immutable
  on private.game_character_player_snapshot_v1_inventory_graph_shadow_items;
create trigger game_character_player_snapshot_v1_inventory_graph_shadow_items_immutable
  before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadow_items
  for each row execute function private.player_snapshot_v1_inventory_graph_shadow_immutable();

-- The canonical payload validator already proves all bounds, node ordering,
-- parent links, and sibling ordinals. This helper intentionally exposes only
-- those topology facts, never object fields.
create or replace function private.player_snapshot_v1_inventory_graph_shadow_nodes(p_payload bytea)
returns table (
  node_index integer,
  parent_node_index integer,
  sibling_ordinal integer
)
language plpgsql
immutable
strict
set search_path = pg_catalog, private
as $$
declare
  v_cursor integer := 0;
  v_field_index integer;
  v_field_length bigint;
  v_graph bytea;
  v_graph_count bigint;
  v_graph_cursor integer := 27;
  v_value_start integer;
  v_parent bigint;
begin
  if not private.player_snapshot_v1_payload_valid(p_payload) then return; end if;

  for v_field_index in 1..40 loop
    v_field_length := private.player_snapshot_v1_u32(p_payload, 19 + v_cursor);
    if v_field_index = 40 then
      v_graph := substring(
        p_payload from 16 + v_cursor + 7 + 1 for v_field_length::integer
      );
      exit;
    end if;
    v_cursor := v_cursor + 7 + v_field_length::integer;
  end loop;

  v_graph_count := private.player_snapshot_v1_u32(v_graph, 23);
  for v_field_index in 0..v_graph_count::integer - 1 loop
    v_value_start := v_graph_cursor + 7;
    node_index := private.player_snapshot_v1_u32(v_graph, v_value_start)::integer;
    v_parent := private.player_snapshot_v1_u32(v_graph, v_value_start + 4);
    parent_node_index := case when v_parent = 4294967295 then null else v_parent::integer end;
    sibling_ordinal := private.player_snapshot_v1_u32(v_graph, v_value_start + 8)::integer;
    return next;
    v_graph_cursor := v_graph_cursor + 356;
  end loop;
end;
$$;

create or replace function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(
  p_character_id uuid,
  p_command_id uuid,
  p_receipt_request_sha256 text,
  p_source_post_sha256 text,
  p_source_octets bigint
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
  v_shadow private.game_character_player_snapshot_v1_inventory_graph_shadows%rowtype;
  v_item_count integer;
  v_inserted_item_count integer;
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotV1 inventory graph shadow writer session identity is invalid';
  end if;
  if p_character_id is null or p_command_id is null
     or p_receipt_request_sha256 is null
     or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_post_sha256 is null
     or p_source_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_octets is null
     or p_source_octets not between 1 and 9223372036854775807 then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotV1 inventory graph shadow arguments are invalid';
  end if;

  -- Lock the complete immutable receipt -> M4 manifest -> artifact chain
  -- before persisting a derived topology row.
  select * into v_receipt from private.game_character_shadow_receipts
   where character_id = p_character_id and command_id = p_command_id for share;
  select * into v_manifest from private.game_character_m4_file_snapshot_manifests
   where character_id = p_character_id and command_id = p_command_id for share;
  select * into v_artifact from private.game_character_player_snapshot_v1_artifacts
   where character_id = p_character_id and command_id = p_command_id for share;
  if v_receipt.character_id is null
     or v_manifest.character_id is null
     or v_artifact.character_id is null
     or v_receipt.request_sha256 <> p_receipt_request_sha256
     or v_receipt.post_sha256 <> p_source_post_sha256
     or v_manifest.world_id <> v_receipt.world_id
     or v_manifest.legacy_name_key <> v_receipt.legacy_name_key
     or v_manifest.receipt_request_sha256 <> v_receipt.request_sha256
     or v_manifest.writer_instance_id <> v_receipt.writer_instance_id
     or v_manifest.writer_epoch <> v_receipt.writer_epoch
     or v_manifest.writer_revision <> v_receipt.writer_revision
     or v_manifest.file_post_sha256 <> v_receipt.post_sha256
     or v_manifest.storage_format <> v_receipt.storage_format
     or v_manifest.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_manifest.snapshot_format <> 'legacy-file-manifest-v1'
     or v_manifest.snapshot_sha256 <> v_receipt.post_sha256
     or v_manifest.snapshot_octets <> p_source_octets
     or v_artifact.world_id <> v_receipt.world_id
     or v_artifact.legacy_name_key <> v_receipt.legacy_name_key
     or v_artifact.receipt_request_sha256 <> v_receipt.request_sha256
     or v_artifact.writer_instance_id <> v_receipt.writer_instance_id
     or v_artifact.writer_epoch <> v_receipt.writer_epoch
     or v_artifact.writer_revision <> v_receipt.writer_revision
     or v_artifact.source_post_sha256 <> v_receipt.post_sha256
     or v_artifact.source_octets <> v_manifest.snapshot_octets
     or v_artifact.source_octets <> p_source_octets
     or v_artifact.storage_format <> v_receipt.storage_format
     or v_artifact.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_artifact.snapshot_format <> 'player-snapshot-v1'
     or v_artifact.snapshot_octets <> octet_length(v_artifact.payload)
     or not private.player_snapshot_v1_payload_valid(v_artifact.payload)
     or v_artifact.snapshot_sha256 <> encode(public.digest(v_artifact.payload, 'sha256'), 'hex') then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotV1 inventory graph shadow has no matching immutable artifact evidence';
  end if;

  select count(*)::integer into v_item_count
    from private.player_snapshot_v1_inventory_graph_shadow_nodes(v_artifact.payload);

  insert into private.game_character_player_snapshot_v1_inventory_graph_shadows (
    character_id, command_id, receipt_request_sha256,
    writer_instance_id, writer_epoch, writer_revision,
    source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, item_count
  ) values (
    p_character_id, p_command_id, v_receipt.request_sha256,
    v_receipt.writer_instance_id, v_receipt.writer_epoch, v_receipt.writer_revision,
    v_receipt.post_sha256, v_artifact.source_octets,
    v_artifact.snapshot_sha256, v_artifact.snapshot_octets, v_item_count
  ) on conflict do nothing returning * into v_shadow;
  if found then
    insert into private.game_character_player_snapshot_v1_inventory_graph_shadow_items (
      character_id, command_id, node_index, parent_node_index, sibling_ordinal
    )
    select p_character_id, p_command_id, n.node_index, n.parent_node_index, n.sibling_ordinal
      from private.player_snapshot_v1_inventory_graph_shadow_nodes(v_artifact.payload) as n
     order by n.node_index;
    get diagnostics v_inserted_item_count = row_count;
    if v_inserted_item_count <> v_item_count then
      raise exception using errcode = 'P0001',
        message = 'PlayerSnapshotV1 inventory graph shadow item insertion is incomplete';
    end if;
    return query select 'RECORDED'::text;
    return;
  end if;

  select * into v_shadow from private.game_character_player_snapshot_v1_inventory_graph_shadows
   where character_id = p_character_id and command_id = p_command_id for share;
  if v_shadow.character_id is not null
     and v_shadow.receipt_request_sha256 = v_receipt.request_sha256
     and v_shadow.writer_instance_id = v_receipt.writer_instance_id
     and v_shadow.writer_epoch = v_receipt.writer_epoch
     and v_shadow.writer_revision = v_receipt.writer_revision
     and v_shadow.source_post_sha256 = v_receipt.post_sha256
     and v_shadow.source_octets = v_artifact.source_octets
     and v_shadow.snapshot_sha256 = v_artifact.snapshot_sha256
     and v_shadow.snapshot_octets = v_artifact.snapshot_octets
     and v_shadow.item_count = v_item_count
     and not exists (
       (select node_index, parent_node_index, sibling_ordinal
          from private.player_snapshot_v1_inventory_graph_shadow_nodes(v_artifact.payload))
       except
       (select node_index, parent_node_index, sibling_ordinal
          from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
         where character_id = p_character_id and command_id = p_command_id)
     )
     and not exists (
       (select node_index, parent_node_index, sibling_ordinal
          from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
         where character_id = p_character_id and command_id = p_command_id)
       except
       (select node_index, parent_node_index, sibling_ordinal
          from private.player_snapshot_v1_inventory_graph_shadow_nodes(v_artifact.payload))
     ) then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;

  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotV1 inventory graph shadow conflicts with immutable evidence';
end;
$$;

create or replace function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(
  p_world_id text,
  p_limit integer default 100
)
returns table (
  character_id uuid,
  command_id uuid,
  legacy_name_key text,
  writer_instance_id uuid,
  writer_epoch bigint,
  writer_revision bigint,
  receipt_request_sha256 text,
  source_post_sha256 text,
  source_octets bigint,
  snapshot_sha256 text,
  snapshot_octets bigint,
  shadow_state text,
  item_count integer,
  shadow_recorded_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotV1 inventory graph shadow writer session identity is invalid';
  end if;
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_limit is null or p_limit not between 1 and 1000 then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotV1 inventory graph shadow reconciliation arguments are invalid';
  end if;

  return query
    select r.character_id, r.command_id, r.legacy_name_key,
      r.writer_instance_id, r.writer_epoch, r.writer_revision,
      r.request_sha256, r.post_sha256, a.source_octets,
      a.snapshot_sha256, a.snapshot_octets,
      case
        when s.character_id is null then 'MISSING'
        when s.receipt_request_sha256 = r.request_sha256
         and s.writer_instance_id = r.writer_instance_id
         and s.writer_epoch = r.writer_epoch
         and s.writer_revision = r.writer_revision
         and s.source_post_sha256 = r.post_sha256
         and s.source_octets = a.source_octets
         and s.snapshot_sha256 = a.snapshot_sha256
         and s.snapshot_octets = a.snapshot_octets
         and s.item_count = (select count(*)::integer
                               from private.player_snapshot_v1_inventory_graph_shadow_nodes(a.payload))
         and not exists (
           (select node_index, parent_node_index, sibling_ordinal
              from private.player_snapshot_v1_inventory_graph_shadow_nodes(a.payload))
           except
           (select node_index, parent_node_index, sibling_ordinal
              from private.game_character_player_snapshot_v1_inventory_graph_shadow_items as i
             where i.character_id = r.character_id and i.command_id = r.command_id)
         )
         and not exists (
           (select node_index, parent_node_index, sibling_ordinal
              from private.game_character_player_snapshot_v1_inventory_graph_shadow_items as i
             where i.character_id = r.character_id and i.command_id = r.command_id)
           except
           (select node_index, parent_node_index, sibling_ordinal
              from private.player_snapshot_v1_inventory_graph_shadow_nodes(a.payload))
         ) then 'EXACT'
        else 'INCONSISTENT'
      end,
      s.item_count, s.recorded_at
    from private.game_character_shadow_receipts as r
    join private.game_character_m4_file_snapshot_manifests as m
      on m.character_id = r.character_id and m.command_id = r.command_id
    join private.game_character_player_snapshot_v1_artifacts as a
      on a.character_id = r.character_id and a.command_id = r.command_id
    left join private.game_character_player_snapshot_v1_inventory_graph_shadows as s
      on s.character_id = r.character_id and s.command_id = r.command_id
   where r.world_id = p_world_id
     and m.world_id = r.world_id
     and m.legacy_name_key = r.legacy_name_key
     and m.receipt_request_sha256 = r.request_sha256
     and m.writer_instance_id = r.writer_instance_id
     and m.writer_epoch = r.writer_epoch
     and m.writer_revision = r.writer_revision
     and m.file_post_sha256 = r.post_sha256
     and m.storage_format = r.storage_format
     and m.receipt_acknowledged_at = r.acknowledged_at
     and m.snapshot_format = 'legacy-file-manifest-v1'
     and m.snapshot_sha256 = r.post_sha256
     and a.world_id = r.world_id
     and a.legacy_name_key = r.legacy_name_key
     and a.receipt_request_sha256 = r.request_sha256
     and a.writer_instance_id = r.writer_instance_id
     and a.writer_epoch = r.writer_epoch
     and a.writer_revision = r.writer_revision
     and a.source_post_sha256 = r.post_sha256
     and a.source_octets = m.snapshot_octets
     and a.storage_format = r.storage_format
     and a.receipt_acknowledged_at = r.acknowledged_at
     and a.snapshot_format = 'player-snapshot-v1'
     and a.snapshot_octets = octet_length(a.payload)
     and private.player_snapshot_v1_payload_valid(a.payload)
     and a.snapshot_sha256 = encode(public.digest(a.payload, 'sha256'), 'hex')
   order by r.character_id, r.writer_revision, r.command_id
   limit p_limit;
end;
$$;

grant usage on schema private to mud_writer;
revoke all on table private.game_character_player_snapshot_v1_inventory_graph_shadows,
  private.game_character_player_snapshot_v1_inventory_graph_shadow_items
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.player_snapshot_v1_inventory_graph_shadow_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.player_snapshot_v1_inventory_graph_shadow_nodes(bytea)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
grant execute on function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)
  to mud_writer;
grant execute on function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)
  to mud_writer;

comment on table private.game_character_player_snapshot_v1_inventory_graph_shadows is
  'Immutable receipt/artifact-bound PlayerSnapshotV1 inventory topology evidence; no object attributes or authority change.';
comment on table private.game_character_player_snapshot_v1_inventory_graph_shadow_items is
  'Immutable minimal graph topology rows: canonical node index, parent index, and sibling ordinal only.';
comment on function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint) is
  'mud_writer_login SET ROLE mud_writer only. Derives or exactly retries immutable topology evidence from a validated receipt-bound artifact.';
comment on function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer) is
  'mud_writer_login SET ROLE mud_writer only. Reports MISSING, EXACT, or INCONSISTENT topology evidence without repair.';
