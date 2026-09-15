\set ON_ERROR_STOP on
-- Separate valid backup fixture. No trigger/constraint suppression.
begin;
insert into public.game_characters
  (id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format)
values ('a9500000-0000-0000-0000-000000000001', 'backup-contract', 'Pvahero', 'Pvahero',
  substr(encode(public.digest(convert_to('Pvahero', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1);
insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9500000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch('backup-contract',
  'b9500000-0000-0000-0000-000000000001'::uuid, clock_timestamp() + interval '3 minutes');
select private.game_character_shadow_request_sha256('backup-contract',
  'a9500000-0000-0000-0000-000000000001'::uuid, 'Pvahero',
  substr(encode(public.digest(convert_to('Pvahero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9500000-0000-0000-0000-000000000001'::uuid, 'b9500000-0000-0000-0000-000000000001'::uuid,
  1::bigint, 1::bigint, 'absent', null::text, repeat('a',64), 1::smallint) as request \gset
select private.record_legacy_published_receipt('backup-contract', 'Pvahero',
  'a9500000-0000-0000-0000-000000000001'::uuid, 'c9500000-0000-0000-0000-000000000001'::uuid,
  'b9500000-0000-0000-0000-000000000001'::uuid, :'request',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a',64), 1::smallint);
-- Nonempty nested bank fixture, generated with canonical CDTO envelopes.
create function pg_temp.bank_envelope(kind integer,body bytea) returns bytea language sql as $$
  select decode('4d55484344544f000001','hex') || int2send(kind::smallint) || int4send(octet_length(body)) || body || public.digest(body,'sha256')
$$;
select pg_temp.bank_envelope(6,decode('0001030000000400000003','hex') ||
  (select string_agg(int2send((i+2)::smallint)||decode('090000015d','hex')||int4send(i)||int4send(case when i=0 then -1 else 0 end)||int4send(case when i=2 then 1 else 0 end)||decode(repeat('00',300),'hex')||int8send(case when i=0 then '-9223372036854775808'::bigint else i::bigint end)||decode(repeat('00',29),'hex'),''::bytea order by i) from generate_series(0,2) s(i))) as bank_graph \gset
select pg_temp.bank_envelope(8,decode('000109','hex')||int4send(octet_length(:'bank_graph'::bytea))||:'bank_graph'::bytea) as bank_payload \gset
set session authorization mud_writer_login;
set local role mud_writer;
select * from private.record_m4_file_snapshot_manifest_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', 'legacy-file-manifest-v1', repeat('a',64), 9);
select * from private.record_player_snapshot_v1_artifact_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', repeat('a',64), 9, 'player-snapshot-v1',
  encode(public.digest(decode(:'fixture_hex','hex'), 'sha256'), 'hex'),
  octet_length(decode(:'fixture_hex','hex')), decode(:'fixture_hex','hex'));
select * from private.record_player_snapshot_v1_level_projection_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', repeat('a',64), 9);
select * from private.record_bank_snapshot_v1_payload_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', repeat('a',64), 9, encode(public.digest(:'bank_payload'::bytea,'sha256'),'hex'), :'bank_payload'::bytea);
commit;
