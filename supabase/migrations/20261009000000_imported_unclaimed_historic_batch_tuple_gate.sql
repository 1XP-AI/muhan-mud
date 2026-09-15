-- 2026-10-09: historic ledger batches predate private per-record tuple
-- evidence. Backfill only a complete, still-imported-unclaimed member set;
-- every other historical batch remains unverifiable and must fail closed.
--
-- A batch can be reconstructed only while every admitted member still exposes
-- the exact immutable import shape. In particular, this never guesses a tuple
-- from a claimed/owned/reformatted row, and it never partially populates a
-- batch whose ledger count cannot be proven by its member links.

create table if not exists private.game_imported_unclaimed_batch_tuple_policy (
  policy_key boolean primary key default true check (policy_key),
  historic_cutoff timestamptz not null,
  backfill_complete boolean not null default false
);

alter table private.game_imported_unclaimed_batch_tuple_policy enable row level security;

revoke all on table private.game_imported_unclaimed_batch_tuple_policy
  from public, anon, authenticated, service_role;

-- Keep the first installation instant. Replays must not move the boundary and
-- retroactively classify a newer batch as historic.
insert into private.game_imported_unclaimed_batch_tuple_policy (
  policy_key, historic_cutoff
) values (true, clock_timestamp())
on conflict (policy_key) do nothing;

-- The one-shot block is atomic: after the first installation no later replay
-- can treat a mutable row that was changed-and-restored as original evidence.
do $$
begin
  if exists (
    select 1 from private.game_imported_unclaimed_batch_tuple_policy
     where policy_key and not backfill_complete
  ) then
    with qualified_batches as (
      select batch.world_id, batch.stream_id, batch.batch_sequence
        from private.game_imported_unclaimed_batches as batch
        cross join private.game_imported_unclaimed_batch_tuple_policy as policy
        join private.game_imported_unclaimed_batch_members as member
          on member.world_id = batch.world_id
         and member.stream_id = batch.stream_id
         and member.batch_sequence = batch.batch_sequence
        join public.game_characters as character on character.id = member.character_id
       where not policy.backfill_complete
         and batch.recorded_at < policy.historic_cutoff
         and not exists (
               select 1
                 from private.game_imported_unclaimed_batch_member_identities as identity
                where identity.world_id = batch.world_id
                  and identity.stream_id = batch.stream_id
                  and identity.batch_sequence = batch.batch_sequence
             )
       group by batch.world_id, batch.stream_id, batch.batch_sequence, batch.record_count
      having count(*) = batch.record_count
         and count(distinct character.legacy_name_key) = batch.record_count
         and bool_and(
           character.world_id = batch.world_id
           and character.legacy_name = character.legacy_name_key
           and character.legacy_name_key = private.game_identity_canonical_legacy_name(character.legacy_name_key)
           and character.legacy_shard = substr(
             encode(digest(convert_to(character.legacy_name_key, 'UTF8'), 'sha1'), 'hex'), 1, 2
           )
           and character.lifecycle = 'imported_unclaimed'
           and character.owner_user_id is null
           and character.claimed_at is null
           and character.storage_format = 1
           and character.imported_file_sha256 ~ '^[0-9a-f]{64}$'
         )
    )
    insert into private.game_imported_unclaimed_batch_member_identities (
      world_id, stream_id, batch_sequence, canonical_legacy_name,
      legacy_shard, imported_file_sha256, storage_format
    )
    select member.world_id, member.stream_id, member.batch_sequence,
           character.legacy_name_key, character.legacy_shard,
           character.imported_file_sha256, character.storage_format
      from qualified_batches as batch
      join private.game_imported_unclaimed_batch_members as member
        on member.world_id = batch.world_id
       and member.stream_id = batch.stream_id
       and member.batch_sequence = batch.batch_sequence
      join public.game_characters as character on character.id = member.character_id;

    update private.game_imported_unclaimed_batch_tuple_policy
       set backfill_complete = true
     where policy_key and not backfill_complete;
  end if;
end;
$$;
