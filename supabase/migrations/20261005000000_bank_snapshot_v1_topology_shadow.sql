-- Detached audit evidence only.  It is deliberately not a bank loader, writer,
-- or gameplay authority; M3 has already published the legacy file before any call here.
create table if not exists private.game_character_bank_snapshot_v1_topology_shadows (
  character_id uuid not null, command_id uuid not null, receipt_request_sha256 text not null,
  writer_instance_id uuid not null, writer_epoch bigint not null, writer_revision bigint not null,
  source_post_sha256 text not null, source_octets bigint not null, bank_sha256 text not null,
  bank_octets bigint not null, item_count integer not null, recorded_at timestamptz not null default clock_timestamp(),
  primary key(character_id, command_id), unique(character_id, writer_revision),
  foreign key(character_id, command_id) references private.game_character_m4_file_snapshot_manifests(character_id, command_id) on delete restrict,
  check(receipt_request_sha256 ~ '^[0-9a-f]{64}$'), check(writer_epoch between 1 and 9223372036854775807), check(writer_revision between 1 and 9223372036854775807),
  check(source_post_sha256 ~ '^[0-9a-f]{64}$'), check(source_octets between 1 and 9223372036854775807), check(bank_sha256 ~ '^[0-9a-f]{64}$'), check(bank_octets between 48 and 4194304), check(item_count between 0 and 8192)
);
create table if not exists private.game_character_bank_snapshot_v1_topology_shadow_items (
  character_id uuid not null, command_id uuid not null, node_index integer not null, parent_node_index integer, sibling_ordinal integer not null,
  primary key(character_id,command_id,node_index), foreign key(character_id,command_id) references private.game_character_bank_snapshot_v1_topology_shadows(character_id,command_id) on delete restrict,
  check(node_index between 0 and 8191), check(parent_node_index is null or parent_node_index between 0 and 8191), check(parent_node_index is null or parent_node_index < node_index), check(sibling_ordinal between 0 and 8191)
);
alter table private.game_character_bank_snapshot_v1_topology_shadows enable row level security;
alter table private.game_character_bank_snapshot_v1_topology_shadow_items enable row level security;
create or replace function private.bank_snapshot_v1_topology_shadow_immutable() returns trigger language plpgsql security invoker set search_path=pg_catalog as $$ begin raise exception using errcode='P0001',message='BankSnapshotV1 topology shadows are immutable'; end $$;
drop trigger if exists game_character_bank_snapshot_v1_topology_shadows_immutable on private.game_character_bank_snapshot_v1_topology_shadows;
create trigger game_character_bank_snapshot_v1_topology_shadows_immutable before update or delete on private.game_character_bank_snapshot_v1_topology_shadows for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();
drop trigger if exists game_character_bank_snapshot_v1_topology_shadow_items_immutable on private.game_character_bank_snapshot_v1_topology_shadow_items;
create trigger game_character_bank_snapshot_v1_topology_shadow_items_immutable before update or delete on private.game_character_bank_snapshot_v1_topology_shadow_items for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();

create or replace function private.record_bank_snapshot_v1_topology_shadow_for_receipt(p_character_id uuid,p_command_id uuid,p_receipt_request_sha256 text,p_source_post_sha256 text,p_source_octets bigint,p_bank_sha256 text,p_bank_octets bigint,p_nodes jsonb)
returns table(outcome text) language plpgsql security definer set search_path=pg_catalog,private as $$
declare r private.game_character_shadow_receipts%rowtype; m private.game_character_m4_file_snapshot_manifests%rowtype; s private.game_character_bank_snapshot_v1_topology_shadows%rowtype; n jsonb; i integer:=0; parent integer; sibling integer; children integer[]:=array_fill(0,array[8192]); depths integer[]:=array_fill(0,array[8192]); ancestors integer[]:=array[]::integer[]; expected integer; depth integer; roots integer:=0; count integer;
begin
  if session_user <> 'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then raise exception using errcode='P0001',message='BankSnapshotV1 writer session identity is invalid'; end if;
  if p_character_id is null or p_command_id is null or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$' or p_source_post_sha256 !~ '^[0-9a-f]{64}$' or p_bank_sha256 !~ '^[0-9a-f]{64}$' or p_source_octets not between 1 and 9223372036854775807 or p_bank_octets not between 48 and 4194304 or p_nodes is null or jsonb_typeof(p_nodes) <> 'array' or jsonb_array_length(p_nodes) not between 1 and 8192 then raise exception using errcode='22023',message='BankSnapshotV1 topology arguments are invalid'; end if;
  select * into r from private.game_character_shadow_receipts where character_id=p_character_id and command_id=p_command_id for share;
  select * into m from private.game_character_m4_file_snapshot_manifests where character_id=p_character_id and command_id=p_command_id for share;
  if not found or r.character_id is null or r.request_sha256<>p_receipt_request_sha256 or r.post_sha256<>p_source_post_sha256 or m.world_id<>r.world_id or m.legacy_name_key<>r.legacy_name_key or m.receipt_request_sha256<>r.request_sha256 or m.writer_instance_id<>r.writer_instance_id or m.writer_epoch<>r.writer_epoch or m.writer_revision<>r.writer_revision or m.file_post_sha256<>r.post_sha256 or m.storage_format<>r.storage_format or m.receipt_acknowledged_at<>r.acknowledged_at or m.snapshot_format<>'legacy-file-manifest-v1' or m.snapshot_sha256<>r.post_sha256 or m.snapshot_octets<>p_source_octets then raise exception using errcode='P0001',message='BankSnapshotV1 topology shadow has no matching M3 receipt and M4 manifest evidence'; end if;
  for n in select value from jsonb_array_elements(p_nodes) loop
    if jsonb_typeof(n)<>'object' or not (n ?& array['nodeIndex','parentNodeIndex','siblingOrdinal']) or (n - 'nodeIndex' - 'parentNodeIndex' - 'siblingOrdinal') <> '{}'::jsonb or jsonb_typeof(n->'nodeIndex')<>'number' or jsonb_typeof(n->'siblingOrdinal')<>'number' or jsonb_typeof(n->'parentNodeIndex') not in ('null','number') or (n->>'nodeIndex') !~ '^[0-9]{1,4}$' or (n->>'siblingOrdinal') !~ '^[0-9]{1,4}$' or (jsonb_typeof(n->'parentNodeIndex')='number' and (n->>'parentNodeIndex') !~ '^[0-9]{1,4}$') then raise exception using errcode='22023',message='BankSnapshotV1 topology nodes are invalid'; end if;
    if (n->>'nodeIndex')::integer<>i or (n->>'nodeIndex')::integer not between 0 and 8191 or (n->>'siblingOrdinal')::integer not between 0 and 8191 then raise exception using errcode='22023',message='BankSnapshotV1 topology preorder bounds are invalid'; end if;
    parent:=case when n->>'parentNodeIndex' is null then null else (n->>'parentNodeIndex')::integer end; sibling:=(n->>'siblingOrdinal')::integer;
    if parent is null then expected:=roots; roots:=roots+1; ancestors:=array[]::integer[]; depth:=1; else if parent<0 or parent>=i or not parent=any(ancestors) then raise exception using errcode='22023',message='BankSnapshotV1 topology parent preorder bounds are invalid'; end if; expected:=children[parent+1]; children[parent+1]:=expected+1; ancestors:=ancestors[1:array_position(ancestors,parent)]; depth:=depths[parent+1]+1; end if;
    if sibling<>expected then raise exception using errcode='22023',message='BankSnapshotV1 topology sibling bounds are invalid'; end if; ancestors:=array_append(ancestors,i); i:=i+1;
    if depth>64 then raise exception using errcode='22023',message='BankSnapshotV1 topology depth exceeds ObjectGraphV1 limit'; end if; depths[i]:=depth;
  end loop; count:=i;
  if roots<>1 then raise exception using errcode='22023',message='BankSnapshotV1 topology must have exactly one root'; end if;
  insert into private.game_character_bank_snapshot_v1_topology_shadows(character_id,command_id,receipt_request_sha256,writer_instance_id,writer_epoch,writer_revision,source_post_sha256,source_octets,bank_sha256,bank_octets,item_count) values(p_character_id,p_command_id,r.request_sha256,r.writer_instance_id,r.writer_epoch,r.writer_revision,r.post_sha256,p_source_octets,p_bank_sha256,p_bank_octets,count) on conflict do nothing returning * into s;
  if found then insert into private.game_character_bank_snapshot_v1_topology_shadow_items select p_character_id,p_command_id,(value->>'nodeIndex')::integer,case when value->>'parentNodeIndex' is null then null else (value->>'parentNodeIndex')::integer end,(value->>'siblingOrdinal')::integer from jsonb_array_elements(p_nodes); return query select 'RECORDED'::text; return; end if;
  select * into s from private.game_character_bank_snapshot_v1_topology_shadows where character_id=p_character_id and command_id=p_command_id for share;
  if s.receipt_request_sha256=r.request_sha256 and s.writer_instance_id=r.writer_instance_id and s.writer_epoch=r.writer_epoch and s.writer_revision=r.writer_revision and s.source_post_sha256=r.post_sha256 and s.source_octets=p_source_octets and s.bank_sha256=p_bank_sha256 and s.bank_octets=p_bank_octets and s.item_count=count and (select count(*) from private.game_character_bank_snapshot_v1_topology_shadow_items where character_id=p_character_id and command_id=p_command_id)=count and not exists ((select (value->>'nodeIndex')::integer,case when value->>'parentNodeIndex' is null then null else (value->>'parentNodeIndex')::integer end,(value->>'siblingOrdinal')::integer from jsonb_array_elements(p_nodes)) except (select node_index,parent_node_index,sibling_ordinal from private.game_character_bank_snapshot_v1_topology_shadow_items where character_id=p_character_id and command_id=p_command_id)) then return query select 'EXACT_RETRY'::text; return; end if;
  raise exception using errcode='P0001',message='BankSnapshotV1 topology shadow conflicts with immutable evidence';
end $$;
revoke all on table private.game_character_bank_snapshot_v1_topology_shadows,private.game_character_bank_snapshot_v1_topology_shadow_items from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb) from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
grant execute on function private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb) to mud_writer;
