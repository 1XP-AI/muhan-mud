-- 2026-10-10: expose a bounded service-only work list for the already-bound
-- onboarding snapshot fulfillment seam. This is a selector, never a save,
-- artifact writer, candidate resolver, or gameplay reader.

create or replace function private.list_pending_game_character_onboarding_snapshot_eligibility(
  p_limit integer default 100
)
returns table (
  correlation_id uuid,
  actor_user_id uuid,
  character_id uuid,
  mode text,
  command_id uuid
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if p_limit is null or p_limit not between 1 and 1000 then
    raise exception using errcode = '22023',
      message = 'pending onboarding snapshot eligibility list limit is invalid';
  end if;

  return query
    select o.correlation_id, o.actor_user_id, o.character_id, o.mode, b.command_id
      from private.game_character_onboarding_snapshot_eligibility_outbox o
      join private.game_character_onboarding_snapshot_command_bindings b
        on b.correlation_id = o.correlation_id
       and b.actor_user_id = o.actor_user_id
       and b.character_id = o.character_id
       and b.mode = o.mode
     where o.status = 'pending'
       and o.fulfilled_at is null
     order by o.enqueued_at, o.correlation_id
     limit p_limit;
end;
$$;

revoke all on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  to service_role;

comment on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer) is
  'service_role only. Lists at most the requested pending eligibility rows with an exact immutable actor/correlation/character/mode/command binding in enqueued-at/correlation order; it never selects artifact candidates or mutates gameplay state.';
