-- Immutable bank payload evidence; not a live bank loader or authority switch.
create or replace function private.bank_snapshot_v1_payload_nodes(p_payload bytea)
returns jsonb language plpgsql immutable set search_path=pg_catalog,private as $$
declare
  body_length bigint; graph_length bigint; graph bytea; node_count integer;
  i integer; parent bigint; nodes jsonb := '[]'::jsonb;
begin
  if p_payload is null or octet_length(p_payload) not between 55 and 4194304 then
    raise exception using errcode='22023',message='invalid bank payload';
  end if;
  body_length := private.player_snapshot_v1_u32(p_payload,12);
  if substring(p_payload from 1 for 12) <> decode('4d55484344544f0000010008','hex')
     or body_length + 48 <> octet_length(p_payload)
     or private.player_snapshot_v1_u16(p_payload,16) <> 1
     or get_byte(p_payload,18) <> 9 then
    raise exception using errcode='22023',message='invalid bank envelope';
  end if;
  graph_length := private.player_snapshot_v1_u32(p_payload,19);
  if graph_length + 7 <> body_length
     or public.digest(substring(p_payload from 17 for body_length::integer),'sha256')
        <> substring(p_payload from octet_length(p_payload)-31 for 32) then
    raise exception using errcode='22023',message='invalid bank envelope digest';
  end if;
  graph := substring(p_payload from 24 for graph_length::integer);
  if private.player_snapshot_v1_object_graph_valid(graph) is not true then
    raise exception using errcode='22023',message='invalid bank object graph';
  end if;
  node_count := private.player_snapshot_v1_u32(graph,23)::integer;
  if node_count < 1 then
    raise exception using errcode='22023',message='bank must have one root';
  end if;
  for i in 0..node_count-1 loop
    parent := private.player_snapshot_v1_u32(graph,38+i*356);
    if (i=0 and parent<>4294967295) or (i>0 and parent=4294967295) then
      raise exception using errcode='22023',message='bank must have one root';
    end if;
    nodes := nodes || jsonb_build_array(jsonb_build_object(
      'nodeIndex',i,'parentNodeIndex',case when i=0 then null else parent end,
      'siblingOrdinal',private.player_snapshot_v1_u32(graph,42+i*356)));
  end loop;
  return nodes;
end $$;

create table if not exists private.game_character_bank_snapshot_v1_payloads (
  character_id uuid not null,
  command_id uuid not null,
  bank_sha256 text not null check(bank_sha256 ~ '^[0-9a-f]{64}$'),
  payload bytea not null check(octet_length(payload) between 55 and 4194304),
  recorded_at timestamptz not null default clock_timestamp(),
  primary key(character_id,command_id),
  foreign key(character_id,command_id) references
    private.game_character_bank_snapshot_v1_topology_shadows(character_id,command_id) on delete restrict,
  check(encode(public.digest(payload,'sha256'),'hex')=bank_sha256),
  check(private.bank_snapshot_v1_payload_nodes(payload) is not null)
);
alter table private.game_character_bank_snapshot_v1_payloads enable row level security;
drop trigger if exists bank_payload_immutable on private.game_character_bank_snapshot_v1_payloads;
create trigger bank_payload_immutable before update or delete on private.game_character_bank_snapshot_v1_payloads
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();

create or replace function private.record_bank_snapshot_v1_payload_for_receipt(
  p_character_id uuid,p_command_id uuid,p_request_sha256 text,
  p_source_post_sha256 text,p_source_octets bigint,p_bank_sha256 text,p_payload bytea)
returns table(outcome text) language plpgsql security definer
set search_path=pg_catalog,private as $$
declare nodes jsonb; previous bytea;
begin
  if session_user <> 'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='bank payload writer identity is invalid';
  end if;
  if p_character_id is null or p_command_id is null or p_request_sha256 is null
     or p_request_sha256 !~ '^[0-9a-f]{64}$' or p_source_post_sha256 is null
     or p_source_post_sha256 !~ '^[0-9a-f]{64}$' or p_source_octets is null or p_source_octets<1
     or p_bank_sha256 is null or p_bank_sha256 !~ '^[0-9a-f]{64}$' then
    raise exception using errcode='22023',message='invalid bank payload arguments';
  end if;
  nodes := private.bank_snapshot_v1_payload_nodes(p_payload);
  if encode(public.digest(p_payload,'sha256'),'hex')<>p_bank_sha256 then
    raise exception using errcode='22023',message='bank payload digest mismatch';
  end if;
  -- Reuse receipt, writer epoch, M4 manifest and immutable retry checks. Nodes
  -- come from the validated bytes, never a second caller-supplied projection.
  perform private.record_bank_snapshot_v1_topology_shadow_for_receipt(
    p_character_id,p_command_id,p_request_sha256,p_source_post_sha256,p_source_octets,
    p_bank_sha256,octet_length(p_payload)::bigint,nodes);
  insert into private.game_character_bank_snapshot_v1_payloads(character_id,command_id,bank_sha256,payload)
    values(p_character_id,p_command_id,p_bank_sha256,p_payload)
    on conflict do nothing;
  if found then return query select 'RECORDED'::text; return; end if;
  select payload into previous from private.game_character_bank_snapshot_v1_payloads
    where character_id=p_character_id and command_id=p_command_id for share;
  if previous=p_payload then return query select 'EXACT_RETRY'::text; return; end if;
  raise exception using errcode='P0001',message='bank payload conflicts with immutable evidence';
end $$;
revoke all on table private.game_character_bank_snapshot_v1_payloads from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.bank_snapshot_v1_payload_nodes(bytea) from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.record_bank_snapshot_v1_payload_for_receipt(uuid,uuid,text,text,bigint,text,bytea) from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
grant execute on function private.record_bank_snapshot_v1_payload_for_receipt(uuid,uuid,text,text,bigint,text,bytea) to mud_writer;
