"""Check the local runtime builder boundary without Docker or credentials."""
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
script = root / 'scripts/build-runtime-local.sh'
sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
with tempfile.TemporaryDirectory(prefix='muhan-build-policy-') as directory:
    recipe = Path(directory) / 'Dockerfile'
    recipe.write_text('FROM scratch AS runtime\n')
    env = dict(os.environ, DOCKER_HOST='unix:///nonexistent-local-test.sock')
    def run(revision, endpoint=env['DOCKER_HOST']):
        return subprocess.run(['bash', str(script), revision, str(recipe), '--dry-run'],
                              env=dict(env, DOCKER_HOST=endpoint), capture_output=True, text=True)
    valid = run(sha)
    assert valid.returncode == 0, valid.stderr
    assert 'output=load' in valid.stdout and 'platform=linux/amd64' in valid.stdout
    assert 'builder=default' in valid.stdout
    assert run('HEAD').returncode != 0
    assert run(sha[:12]).returncode != 0
    assert run(sha, 'tcp://remote:2375').returncode != 0
    assert run(sha, '').returncode != 0
text = script.read_text()
assert '--push' not in text and '--secret' not in text
assert '--build-context "source=$scratch/source"' in text
assert 'archive "$revision"' in text
assert 'prune ' not in text.replace('never prune shared caches', '')
print('build_runtime_local_test: immutable source, local-only endpoint, no publish passed')
