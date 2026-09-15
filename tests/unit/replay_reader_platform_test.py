"""Reject unsupported host before any Docker or database work."""
from pathlib import Path
import os
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='muhan-replay-platform-') as directory:
    node = Path(directory) / 'node'
    node.write_text('#!/bin/sh\nprintf "darwin\\n"\n')
    node.chmod(0o755)
    result = subprocess.run(['bash', str(root / 'supabase/tests/player_snapshot_v1_replay_reader_pg17_integration.sh')],
                            env=dict(os.environ, PATH=directory + os.pathsep + os.environ['PATH'],
                                     PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE='1'),
                            text=True, capture_output=True)
    assert result.returncode == 2, result.stderr
    assert 'requires Linux Node' in result.stderr, result.stderr
print('replay_reader_platform_test: non-Linux rejected before database setup')
