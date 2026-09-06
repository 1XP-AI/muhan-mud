\set ON_ERROR_STOP on
-- PG17-only disposable contract: apply through 20261005000000, then run.
begin;
create or replace function pg_temp.assert_true(v boolean, m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
select pg_temp.assert_true(to_regprocedure('private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb)') is not null,'bank topology recorder signature exists');
select pg_temp.assert_true((select relrowsecurity from pg_class where oid='private.game_character_bank_snapshot_v1_topology_shadows'::regclass) and (select relrowsecurity from pg_class where oid='private.game_character_bank_snapshot_v1_topology_shadow_items'::regclass),'bank topology relations are private RLS evidence');
select pg_temp.assert_true(not exists(select 1 from pg_attribute where attrelid in ('private.game_character_bank_snapshot_v1_topology_shadows'::regclass,'private.game_character_bank_snapshot_v1_topology_shadow_items'::regclass) and atttypid='bytea'::regtype and attnum>0 and not attisdropped),'bank topology shadow stores no bank payload');
select pg_temp.assert_true((select prosecdef and proconfig=array['search_path=pg_catalog, private']::text[] from pg_proc where oid='private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb)'::regprocedure),'bank topology writer is a pinned security definer');
rollback;
