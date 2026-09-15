\set ON_ERROR_STOP on
create function pg_temp.assert_true(value boolean) returns void language plpgsql as $$
begin
  if value is not true then raise exception 'restored database contract failed'; end if;
end $$;
select receipt_request_sha256 as request from private.game_character_player_snapshot_v1_artifacts
where character_id = 'a9500000-0000-0000-0000-000000000001' \gset
select payload as bank_payload, bank_sha256 as bank_hash from private.game_character_bank_snapshot_v1_payloads
where character_id='a9500000-0000-0000-0000-000000000001' \gset
set session authorization mud_full_payload_rehearsal_reader_login;
select pg_temp.assert_true(
  current_user = session_user
  and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'INSERT')
  and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'UPDATE')
  and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'DELETE')
  and (select count(*) = 1 from private.game_character_player_snapshot_v1_artifacts)
);
reset session authorization;
set session authorization mud_replay_reader_login;
select pg_temp.assert_true(
  not has_column_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'payload', 'SELECT')
  and (select count(command_id) = 1 from private.game_character_player_snapshot_v1_artifacts)
);
reset session authorization;
set session authorization mud_writer_login;
set role mud_writer;
select pg_temp.assert_true(not has_table_privilege(current_user,'private.game_character_bank_snapshot_v1_payloads','SELECT'));
select pg_temp.assert_true((select outcome='EXACT_RETRY' from private.record_bank_snapshot_v1_payload_for_receipt(
  'a9500000-0000-0000-0000-000000000001','c9500000-0000-0000-0000-000000000001',
  :'request',repeat('a',64),9,:'bank_hash',:'bank_payload'::bytea)));
select pg_temp.assert_true((select outcome = 'EXACT_RETRY' from private.record_m4_file_snapshot_manifest_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', 'legacy-file-manifest-v1', repeat('a',64), 9)));
select pg_temp.assert_true((select outcome = 'EXACT_RETRY' from private.record_player_snapshot_v1_artifact_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', repeat('a',64), 9, 'player-snapshot-v1',
  encode(public.digest(decode(:'fixture_hex','hex'), 'sha256'), 'hex'),
  octet_length(decode(:'fixture_hex','hex')), decode(:'fixture_hex','hex'))));
select pg_temp.assert_true((select outcome = 'EXACT_RETRY' from private.record_player_snapshot_v1_level_projection_for_receipt(
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'request', repeat('a',64), 9)));
reset role;
reset session authorization;
select pg_temp.assert_true(private.enroll_paired_snapshot_baseline(
  'a9500000-0000-0000-0000-000000000001','c9500000-0000-0000-0000-000000000001',:'request')='EXACT_RETRY');
select pg_temp.assert_true((select revision=1 from private.game_character_paired_snapshot_states
  where character_id='a9500000-0000-0000-0000-000000000001'));
select pg_temp.assert_true((select outcome='EXACT_RETRY' and committed_revision=1
  from private.game_character_paired_snapshot_states s,
  lateral private.commit_paired_snapshot_candidate(s.character_id,'d9500000-0000-0000-0000-000000000001',0,s.player_payload,s.bank_payload)
  where s.character_id='a9500000-0000-0000-0000-000000000001'));
select pg_temp.assert_true(not exists(
  select 1 from unnest(array['anon','authenticated','service_role','mud_writer','mud_writer_login']) r(role_name)
  where has_function_privilege(role_name,'private.enroll_paired_snapshot_baseline(uuid,uuid,text)','EXECUTE')
     or has_table_privilege(role_name,'private.game_character_paired_snapshot_states','INSERT')
     or has_table_privilege(role_name,'private.game_character_paired_snapshot_states','UPDATE')
     or has_table_privilege(role_name,'private.game_character_paired_snapshot_baselines','UPDATE')
));
