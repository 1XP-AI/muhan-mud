"""Check the local runtime builder boundary without Docker or credentials."""
import os
from pathlib import Path
import subprocess
import tempfile
import json

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
    # Exercise the real shell control flow with a recording CLI, not a live daemon.
    fake = Path(directory) / 'docker'
    fake.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
a = sys.argv[1:]
with open(os.environ['BUILD_CALLS'], 'a') as f:
    f.write(json.dumps(a) + '\\n')
if 'buildx' in a and 'inspect' in a:
    print(os.environ.get('TEST_DRIVER', 'docker'))
elif 'buildx' in a and 'build' in a:
    source = pathlib.Path(a[a.index('--build-context') + 1].split('=', 1)[1])
    assert (source / 'src' / 'services' / 'gateway' / 'package.json').is_file()
    assert not (source / 'src' / '.git').exists()
    assert not list(pathlib.Path(a[-1]).iterdir())
    assert '--push' not in a and '--secret' not in a
    assert a[a.index('--platform') + 1] == 'linux/amd64'
    assert a[a.index('--builder') + 1] == 'default'
    assert a[a.index('--target') + 1] == 'runtime'
    sys.exit(int(os.environ.get('TEST_BUILD_EXIT', '0')))
elif 'image' in a and 'inspect' in a:
    if '--format' in a:
        print(os.environ.get('TEST_ARCH', 'amd64') + ' ' + os.environ['TEST_SHA'])
    else:
        print('[]')
else:
    sys.exit(99)
''')
    fake.chmod(0o755)
    calls = Path(directory) / 'calls.jsonl'
    full_env = dict(env, PATH=directory + os.pathsep + env['PATH'],
                    TMPDIR=directory, BUILD_CALLS=str(calls), TEST_SHA=sha)
    def build(**overrides):
        calls.write_text('')
        result = subprocess.run(['bash', str(script), sha, str(recipe)],
                                env=dict(full_env, **overrides), capture_output=True, text=True)
        return result, [json.loads(line) for line in calls.read_text().splitlines()]
    result, recorded = build()
    assert result.returncode == 0, result.stderr
    assert len(recorded) == 4 and 'Built local runtime:' in result.stdout
    result, recorded = build(TEST_DRIVER='cloud')
    assert result.returncode != 0 and len(recorded) == 1
    result, recorded = build(TEST_ARCH='arm64')
    assert result.returncode != 0 and 'identity mismatch' in result.stderr
    result, recorded = build(TEST_BUILD_EXIT='17')
    assert result.returncode == 17 and len(recorded) == 2
    assert 'Built local runtime:' not in result.stdout
text = script.read_text()
assert '--push' not in text and '--secret' not in text
assert '--build-context "source=$scratch/source"' in text
assert 'archive "$revision"' in text
assert 'prune ' not in text.replace('never prune shared caches', '')
print('build_runtime_local_test: source archive, local routing, build failure and image identity gates passed (mock CLI)')
