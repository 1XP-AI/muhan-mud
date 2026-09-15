"""Keep the real Auth test local, explicit, and isolated from other projects."""
from pathlib import Path

root = Path(__file__).resolve().parents[2]
runner = (root / 'scripts/run-real-auth-local-docker.sh').read_text()
probe = (root / 'tests/stack-e2e/real-auth-smoke.mjs').read_text()
bootstrap = (root / 'tests/stack-e2e/real-auth-bootstrap.sql').read_text()
assert '--allow-disposable' in runner
assert '--network none' in runner and '--network "container:$pg"' in runner
assert 'supabase/gotrue:v2.189.0' in runner
assert 'created=("$auth" "${created[@]}")' in runner
assert 'created=("$probe" "${created[@]}")' in runner
assert 'docker rm -f "$id"' in runner
for forbidden in ('--publish', '--privileged', '--volume', 'docker.sock', 'prune', '--push', '--builder'):
    assert forbidden not in runner, forbidden
assert "assert.equal(base, 'http://127.0.0.1:9999')" in probe
assert 'timingSafeEqual' in probe and 'grant_type=refresh_token' in probe
assert 'stage = \'revoked-refresh\'' in probe
assert 'alter role supabase_auth_admin set search_path to auth;' in bootstrap
print('real_auth_local_policy_test: explicit local isolation and auth lifecycle guards pass')
