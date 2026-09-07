#!/usr/bin/env python3
"""Small regression guard for the shared-runner routing/isolation contract."""
from pathlib import Path

root = Path(__file__).resolve().parents[2]
workflow = (root / '.github/workflows/ci.yml').read_text()
makefile = (root / 'src/Makefile').read_text()
assert 'runs-on: [self-hosted, Linux, ARM64]' in workflow
assert 'labels: [self-hosted, Linux, ARM64]' in workflow
assert 'labels: [self-hosted, macOS, ARM64]' in workflow
assert 'runs-on: ${{ matrix.labels }}' in workflow
assert 'labels: windows-latest' in workflow
assert 'labels: ubuntu-latest' in workflow  # native x64/pinned amd64 evidence
assert '5432:5432' not in workflow
assert '127.0.0.1:5432/' not in workflow
assert "job.services.postgres.ports['5432']" in workflow
assert 'log_file="/tmp/frp.log"' not in workflow
assert 'mktemp -d "$RUNNER_TEMP/muhan-ci.XXXXXX"' in workflow
assert 'MUHAN_BROWSER_PORT=' in workflow
for setting in ('TMPDIR', 'XDG_CACHE_HOME', 'npm_config_cache',
                'npm_config_store_dir', 'PLAYWRIGHT_BROWSERS_PATH'):
    assert workflow.count(f'{setting}=%s/') == 2, setting
assert workflow.count('${{ github.run_attempt }}') == 2
assert 'dtolnay/rust-toolchain@1.90.0' in workflow
assert 'pnpm/action-setup@v4' in workflow
assert 'python-is-python3' in workflow and 'build-essential' in workflow
assert workflow.count('Acquire::Retries=3 update --error-on=any') == 2
assert 'sudo apt-get update\n' not in workflow
assert workflow.count('sudo -n true ||') == 2
assert makefile.count('/tmp/muhan-unit') == 1  # local default only
assert 'MUHAN_UNIT_DIR ?= /tmp/muhan-unit' in makefile
for config in ('playwright.config.ts', 'playwright.feature-off.config.ts'):
    assert 'process.env.MUHAN_BROWSER_PORT' in (root / 'tests/browser-e2e' / config).read_text()
print('self_hosted_ci_policy_test: routing, explicit tools, isolated outputs and ports passed')
