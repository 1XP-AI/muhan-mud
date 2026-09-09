"""Prevent accidental restoration of automatic paid CI triggers."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
for workflow in (root / '.github/workflows').iterdir():
    if workflow.suffix not in {'.yml', '.yaml'}:
        continue
    text = workflow.read_text()
    trigger = re.search(r'^on:\n((?:[ \t].*\n|\n)+)', text, re.M)
    assert trigger, f'{workflow.name}: use an explicit block-form manual trigger'
    trigger_body = trigger.group(1).strip()
    assert trigger_body.startswith('workflow_dispatch:'), (
        f'{workflow.name}: hosted CI must remain manual-only; obtain user approval for changes'
    )
    assert not re.search(r'^\s+(push|pull_request|schedule):', trigger_body, re.M), (
        f'{workflow.name}: automatic hosted CI triggers are disabled'
    )

go_validation = (root / 'scripts/run-go-validation.sh').read_text()
assert 'affected_packages()' in go_validation
assert 'GO_FAST_PACKAGES=all' in go_validation
assert 'CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...' in go_validation
local_hook = (root / 'scripts/check-local-before-push.sh').read_text()
assert 'MUHAN_DIFF_BASE' in local_hook
assert 'run_migration_contract=0' in local_hook
assert 'run_stack_contracts=0' in local_hook
print('local_first_policy_test: all workflows are manual-only')
