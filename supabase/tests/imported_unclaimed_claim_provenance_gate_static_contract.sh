#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20260930000000_imported_unclaimed_claim_provenance_gate.sql'
contract='supabase/tests/imported_unclaimed_claim_provenance_gate_contract.sql'

fail() {
  printf 'imported-unclaimed claim provenance gate static contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" >/dev/null || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing executable SQL coverage: $1"
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'create or replace function public.challenge_legacy_game_character_onboarding('
require_source 'create or replace function public.claim_legacy_game_character_onboarding('
require_source '  p_world_id text,'
require_source '  p_imported_file_sha256 text,'
require_source '  p_actor_user_id uuid,'
require_source '  p_correlation_id uuid'
require_source '  challenge_status text,'
require_source 'onboarding_status text, imported_file_sha256 text'
require_source 'from private.game_imported_unclaimed_batch_members as member'
require_source 'where member.character_id = v_character.id'
require_source "message = 'claim challenge unavailable'"
require_source "message = 'onboarding claim unavailable'"
require_source "set lifecycle = 'handoff_pending'"
require_source 'grant execute on function public.challenge_legacy_game_character_onboarding(text, text, text, uuid, uuid)'
require_source 'grant execute on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid)'
require_contract "\\ir ../migrations/20260930000000_imported_unclaimed_claim_provenance_gate.sql"
require_contract 'missing-member challenge leaves intent and attempt state unchanged'
require_contract 'missing-member claim leaves every ownership and handoff state unchanged'
require_contract "'claim challenge unavailable'"
require_contract "'onboarding claim unavailable'"
require_contract 'exact completed retry remains valid after challenge expiry'
require_contract 'competing actor can hold its pre-ownership challenge'
require_contract 'concurrent/ownership contender conflict'
require_contract 'SHA/name mismatch creates no challenge attempt'
require_contract 'evidence finalizer uses the member-positive claim route'
require_contract 'legacy active no-member row retains normal session lease behavior'
require_contract 'rollback;'
