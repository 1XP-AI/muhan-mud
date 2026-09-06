-- 2026-10-13: private-schema USAGE is deliberately granted to a small number
-- of constrained logins. PostgreSQL functions otherwise default to PUBLIC
-- EXECUTE, which would turn schema lookup into execution authority. Remove
-- that ambient path without changing any deliberately named function grants.

-- Cover every function that exists when this forward-only migration is
-- applied, including functions whose ACL is still NULL and therefore resolves
-- to PostgreSQL's implicit PUBLIC EXECUTE default.
revoke execute on all functions in schema private from public;

-- Defaults are per creating role. Omitting FOR ROLE intentionally changes
-- only the migration owner, so this is safe for a managed migration runner
-- and prevents its future private functions from acquiring PUBLIC EXECUTE.
alter default privileges in schema private
  revoke execute on functions from public;

-- The restricted list login retains exactly its explicit bounded RPC after
-- the schema-wide revoke. Named grants for every other private function are
-- intentionally left untouched.
grant execute on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  to onboarding_snapshot_eligibility_login;
