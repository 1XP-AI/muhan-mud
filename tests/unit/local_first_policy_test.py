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
print('local_first_policy_test: all workflows are manual-only')
