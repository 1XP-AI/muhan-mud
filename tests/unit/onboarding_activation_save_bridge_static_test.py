from pathlib import Path


root = Path(__file__).resolve().parents[2]
source = (root / "src" / "onboarding_activation_save_bridge.c").read_text()
header = (root / "src" / "onboarding_activation_save_bridge.h").read_text()

assert "onboarding_activation_save_capability_peek_for_explicit_save" in source
assert "CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED" in source
assert source.index("CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED") < source.index(
    "onboarding_activation_save_capability_consume_published_explicit_save"
)
assert "onboarding_activation_save_bridge_resolve" in header
for forbidden in ("getenv", "onboarding_activation_binding_read", "onboarding_snapshot_command_consumer", "player_store"):
    assert forbidden not in source
