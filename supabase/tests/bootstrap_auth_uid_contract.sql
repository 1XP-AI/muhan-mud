\set ON_ERROR_STOP on
-- Disposable bootstrap regression: PostgREST JSON claims and legacy SQL fixtures.
begin;
create function pg_temp.check_uid(expected uuid) returns void language plpgsql as $$
begin
  if auth.uid() is distinct from expected then
    raise exception 'bootstrap auth.uid claim mismatch';
  end if;
end $$;
select pg_temp.check_uid(null);
select set_config('request.jwt.claim.sub', '', true);
select set_config('request.jwt.claims', '{"sub":"10000000-0000-0000-0000-000000000001"}', true);
select pg_temp.check_uid('10000000-0000-0000-0000-000000000001');
create table public.bootstrap_uid_probe(owner_id uuid);
insert into public.bootstrap_uid_probe values
  ('10000000-0000-0000-0000-000000000001'),
  ('10000000-0000-0000-0000-000000000002');
alter table public.bootstrap_uid_probe enable row level security;
create policy own_row on public.bootstrap_uid_probe for select to authenticated
  using (owner_id = (select auth.uid()));
grant usage on schema public to authenticated;
grant select on public.bootstrap_uid_probe to authenticated;
set local role authenticated;
do $$ begin
  if (select count(*) from public.bootstrap_uid_probe) <> 1 or
     (select owner_id from public.bootstrap_uid_probe) <>
       '10000000-0000-0000-0000-000000000001'::uuid then
    raise exception 'JSON claims must expose only the owner row';
  end if;
end $$;
select set_config('request.jwt.claims', '{}', true);
do $$ begin
  if exists (select 1 from public.bootstrap_uid_probe) then
    raise exception 'missing subject must expose no rows';
  end if;
end $$;
reset role;
select pg_temp.check_uid(null);
select set_config('request.jwt.claims', '', true);
select pg_temp.check_uid(null);
select set_config('request.jwt.claim.sub', '10000000-0000-0000-0000-000000000002', true);
select pg_temp.check_uid('10000000-0000-0000-0000-000000000002');
rollback;
