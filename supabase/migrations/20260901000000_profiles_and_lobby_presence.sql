-- 2026-09-01: web MUD identity and private lobby presence.
-- This migration deliberately grants no browser write path for game state.

create table if not exists public.profiles (
  id uuid primary key references auth.users(id) on delete cascade,
  display_name text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint profiles_display_name_length check (display_name is null or char_length(display_name) between 1 and 80)
);

create or replace function public.handle_new_user_profile()
returns trigger
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  candidate text;
begin
  candidate := nullif(btrim(new.raw_user_meta_data ->> 'display_name'), '');
  insert into public.profiles (id, display_name)
  values (new.id, left(candidate, 80))
  on conflict (id) do nothing;
  return new;
end;
$$;

drop trigger if exists on_auth_user_created_profile on auth.users;
create trigger on_auth_user_created_profile
  after insert on auth.users
  for each row execute function public.handle_new_user_profile();

create or replace function public.touch_profile_updated_at()
returns trigger
language plpgsql
security invoker
set search_path = public, pg_temp
as $$
begin
  new.updated_at := now();
  return new;
end;
$$;

drop trigger if exists profiles_touch_updated_at on public.profiles;
create trigger profiles_touch_updated_at
  before update on public.profiles
  for each row execute function public.touch_profile_updated_at();

alter table public.profiles enable row level security;
revoke all on table public.profiles from anon;
revoke all on table public.profiles from authenticated;
grant select, insert, update on table public.profiles to authenticated;

drop policy if exists profiles_select_own on public.profiles;
create policy profiles_select_own on public.profiles
  for select to authenticated using ((select auth.uid()) = id);

drop policy if exists profiles_insert_own on public.profiles;
create policy profiles_insert_own on public.profiles
  for insert to authenticated with check ((select auth.uid()) = id);

drop policy if exists profiles_update_own on public.profiles;
create policy profiles_update_own on public.profiles
  for update to authenticated
  using ((select auth.uid()) = id)
  with check ((select auth.uid()) = id);

-- A private Realtime channel checks read authorization for both Broadcast and
-- Presence while joining. Broadcast remains receive-only because the only
-- INSERT policy below is restricted to Presence.
drop policy if exists mud_lobby_presence_select on realtime.messages;
drop policy if exists mud_lobby_read on realtime.messages;
create policy mud_lobby_read on realtime.messages
  for select to authenticated
  using (
    (select realtime.topic()) = 'mud:lobby'
    and extension in ('broadcast', 'presence')
  );

drop policy if exists mud_lobby_presence_insert on realtime.messages;
create policy mud_lobby_presence_insert on realtime.messages
  for insert to authenticated
  with check ((select realtime.topic()) = 'mud:lobby' and extension = 'presence');
