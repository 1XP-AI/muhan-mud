\set ON_ERROR_STOP on
begin;
create function pg_temp.reject_paired_baseline_insert() returns trigger language plpgsql as $$
begin raise exception using errcode='P0001',message='injected baseline ledger failure'; end $$;
do $$
declare cid uuid:='a9220000-0000-0000-0000-000000000001'; cmd uuid:='c9220000-0000-0000-0000-000000000001';
  request text; before_state jsonb; result text;
begin
  select request_sha256 into strict request from private.game_character_shadow_receipts where character_id=cid and command_id=cmd;
  begin
    perform private.enroll_paired_snapshot_baseline(cid,cmd,repeat('f',64));
    raise exception 'wrong request accepted';
  exception when sqlstate 'P0001' then
    if sqlerrm='wrong request accepted' then raise; end if;
  end;
  begin
    update private.game_character_legacy_heads set revision=revision+1 where character_id=cid;
    perform private.enroll_paired_snapshot_baseline(cid,cmd,request);
    raise exception 'stale head accepted';
  exception when sqlstate 'P0001' then
    if sqlerrm='stale head accepted' then raise; end if;
  end;
  if exists(select 1 from private.game_character_paired_snapshot_states where character_id=cid) then raise exception 'negative probe wrote state'; end if;
  begin
    perform private.enroll_paired_snapshot_baseline(cid,'c9220000-0000-0000-0000-000000000099',request);
    raise exception 'missing command accepted';
  exception when sqlstate 'P0001' then
    if sqlerrm='missing command accepted' then raise; end if;
  end;
  create trigger injected_baseline_failure before insert on private.game_character_paired_snapshot_baselines
    for each row execute function pg_temp.reject_paired_baseline_insert();
  begin
    perform private.enroll_paired_snapshot_baseline(cid,cmd,request);
    raise exception 'baseline failure not injected';
  exception when sqlstate 'P0001' then
    if sqlerrm<>'injected baseline ledger failure' then raise; end if;
  end;
  drop trigger injected_baseline_failure on private.game_character_paired_snapshot_baselines;
  if exists(select 1 from private.game_character_paired_snapshot_states where character_id=cid)
     or exists(select 1 from private.game_character_paired_snapshot_baselines where character_id=cid) then raise exception 'failed enrollment left partial state'; end if;
  result:=private.enroll_paired_snapshot_baseline(cid,cmd,request);
  if result<>'ENROLLED' then raise exception 'enrollment failed'; end if;
  if not exists(select 1 from private.game_character_paired_snapshot_states s
    join private.game_character_player_snapshot_v1_artifacts p using(character_id)
    join private.game_character_bank_snapshot_v1_payloads b using(character_id)
    where s.character_id=cid and p.command_id=cmd and b.command_id=cmd and s.revision=0
      and s.player_payload=p.payload and s.bank_payload=b.payload) then raise exception 'baseline bytes differ'; end if;
  -- Simulates a later pair revision; enrollment retry must never reset it.
  update private.game_character_paired_snapshot_states set revision=7 where character_id=cid;
  select to_jsonb(s) into before_state from private.game_character_paired_snapshot_states s where character_id=cid;
  if private.enroll_paired_snapshot_baseline(cid,cmd,request)<>'EXACT_RETRY' then raise exception 'retry failed'; end if;
  if before_state is distinct from (select to_jsonb(s) from private.game_character_paired_snapshot_states s where character_id=cid) then raise exception 'retry rewrote pair'; end if;
  if has_function_privilege('mud_writer','private.enroll_paired_snapshot_baseline(uuid,uuid,text)','EXECUTE')
     or has_function_privilege('service_role','private.enroll_paired_snapshot_baseline(uuid,uuid,text)','EXECUTE') then raise exception 'runtime baseline grant leaked'; end if;
end $$;
rollback;
