#!/usr/bin/env python3
"""No daemon required: a name collision must never remove someone else's DB."""
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
harness = root / 'supabase/tests/m3_live_save_route_pg17_integration.sh'
with tempfile.TemporaryDirectory(prefix='muhan-ci-cleanup-') as directory:
    temporary = Path(directory)
    docker = temporary / 'docker'
    docker.write_text('''#!/bin/sh
case "$1" in
  run) if [ "$MOCK_COLLISION" = 1 ]; then exit 1; fi; printf '%064d\\n' 1 ;;
  rm) printf '%s\\n' "$*" >> "$MOCK_LOG" ;;
  *) exit 1 ;;
esac
''')
    docker.chmod(0o755)
    sleep = temporary / 'sleep'
    sleep.write_text('#!/bin/sh\nexit 0\n')
    sleep.chmod(0o755)
    log = temporary / 'removed.log'
    env = {**os.environ, 'PATH': f'{temporary}:{os.environ["PATH"]}',
           'M3_LIVE_SAVE_ROUTE_ALLOW_DISPOSABLE': '1', 'MOCK_LOG': str(log)}
    for collision in ('1', '0'):
        result = subprocess.run(['bash', str(harness)], env={**env, 'MOCK_COLLISION': collision},
                                capture_output=True, text=True, timeout=20)
        assert result.returncode != 0  # Collision or deliberately unavailable DB.
        if collision == '1':
            assert not log.exists(), 'name collision removed an unowned container'
        else:
            assert log.read_text() == f'rm --force {1:064d}\n', 'cleanup must use created ID'
print('ci_container_cleanup_test: collision preserves other containers; owned ID cleaned')
