"""Keep compiled runtime output in pnpm deploy despite the dist gitignore."""
import json
from pathlib import Path

root = Path(__file__).resolve().parents[2]
package = json.loads((root / 'services/onboarding-reconciler/package.json').read_text())
assert 'dist' in package.get('files', []), 'pnpm deploy must include compiled runtime output'
assert 'pg' in package['dependencies'], 'Postgres adapters require pg at runtime'
print('reconciler_package_policy_test: compiled output and runtime dependency declared')
