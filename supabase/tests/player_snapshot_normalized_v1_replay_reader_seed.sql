\set ON_ERROR_STOP on

-- Disposable database owner only. Required psql variables:
-- pvi_tree_payload: decode('<checked-in tree fixture hex>', 'hex') expression
-- normalized_projection: exact JSON line emitted by Rust for that fixture
-- This deliberately commits one fixed fixture for a SEPARATE reader session.
-- A second run fails on its fixed character ID; it never deletes/replaces data.
-- Define psql variable receipt_only to seed just identity/head/receipt for the
-- manifest-first CLI integration. No manifest/artifact/projection is prefilled.
\if :{?pvi_tree_payload}
\else
  \quit 2
\endif
\if :{?receipt_only}
\else
  \if :{?normalized_projection}
  \else
    \quit 2
  \endif
\endif

begin;
create or replace function pg_temp.require_recorded(value text)
returns void language plpgsql as $$
begin
  if value is distinct from 'RECORDED' then
    raise exception 'normalized reader seed recorder did not record';
  end if;
end;
$$;

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format
) values (
  'a9140000-0000-0000-0000-000000000001', 'normalized-reader-test', 'Nprhero', 'Nprhero',
  substr(encode(public.digest(convert_to('Nprhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1
);
insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9140000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch(
  'normalized-reader-test', 'b9140000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);
-- Source post metadata is synthetic fixture evidence, not a live legacy save.
select private.game_character_shadow_request_sha256(
  'normalized-reader-test', 'a9140000-0000-0000-0000-000000000001'::uuid, 'Nprhero',
  substr(encode(public.digest(convert_to('Nprhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9140000-0000-0000-0000-000000000001'::uuid,
  'b9140000-0000-0000-0000-000000000001'::uuid, 1::bigint, 1::bigint,
  'absent', null::text, repeat('a', 64), 1::smallint
) as request_sha256 \gset npr_
select private.record_legacy_published_receipt(
  'normalized-reader-test', 'Nprhero', 'a9140000-0000-0000-0000-000000000001'::uuid,
  'c9140000-0000-0000-0000-000000000001'::uuid,
  'b9140000-0000-0000-0000-000000000001'::uuid, :'npr_request_sha256',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a', 64), 1::smallint
);
\if :{?receipt_only}
commit;
\quit
\endif
select octet_length(:pvi_tree_payload) as snapshot_octets,
  encode(public.digest(:pvi_tree_payload, 'sha256'), 'hex') as snapshot_sha256,
  encode(:pvi_tree_payload, 'hex') as snapshot_hex
\gset npr_

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.require_recorded((select outcome
from private.record_m4_file_snapshot_manifest_for_receipt(
  'a9140000-0000-0000-0000-000000000001'::uuid,
  'c9140000-0000-0000-0000-000000000001'::uuid,
  :'npr_request_sha256', 'legacy-file-manifest-v1', repeat('a', 64), 9
)));
select pg_temp.require_recorded((select outcome
from private.record_player_snapshot_v1_artifact_for_receipt(
  'a9140000-0000-0000-0000-000000000001'::uuid,
  'c9140000-0000-0000-0000-000000000001'::uuid,
  :'npr_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1',
  :'npr_snapshot_sha256', :'npr_snapshot_octets', decode(:'npr_snapshot_hex', 'hex')
)));
select pg_temp.require_recorded((select outcome
from private.record_player_snapshot_normalized_v1_projection_for_receipt(
  'a9140000-0000-0000-0000-000000000001'::uuid,
  'c9140000-0000-0000-0000-000000000001'::uuid,
  :'npr_request_sha256', repeat('a', 64), 9, :'normalized_projection'::jsonb
)));
reset role;
reset session authorization;
commit;
