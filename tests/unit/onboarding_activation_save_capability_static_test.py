#!/usr/bin/env python3
"""The activation capability remains a session-only, explicit save seam."""

from pathlib import Path
import re


ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "src" / "onboarding_activation_save_capability.c").read_text(encoding="utf-8")
HEADER = (ROOT / "src" / "onboarding_activation_save_capability.h").read_text(encoding="utf-8")
BRIDGE = (ROOT / "src" / "onboarding_activation_save_bridge.c").read_text(encoding="utf-8")
COMMAND = (ROOT / "src" / "command1.c").read_text(encoding="utf-8")
MAKEFILE = (ROOT / "src" / "Makefile").read_text(encoding="utf-8")


def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def main() -> None:
    compact = re.sub(r"/\*.*?\*/", "", SOURCE, flags=re.DOTALL).lower()
    forbidden = ("save_ply", "load_ply", "player_store", "file_player_store",
                 "character_player_snapshot", "character_save_journal", "opendir",
                 "readdir", "scandir", "glob", "find", "mkdir", "open(", "write(")
    expect(not [token for token in forbidden if token in compact],
           "capability must not scan, persist, or automatically save")
    expect('getenv("MUD_M3_MODE")' in SOURCE and
           'getenv("MUD_M3_PLAYER_SNAPSHOT_V1")' in SOURCE and
           '"shadow"' in SOURCE and '"handoff"' in SOURCE,
           "capability requires both literal shadow and handoff opt-ins")
    for function in ("onboarding_provision", "onboarding_claim"):
        start = COMMAND.index("void " + function)
        end = COMMAND.find("\nvoid ", start + 1)
        section = COMMAND[start:end if end >= 0 else len(COMMAND)]
        capture = section.index("onboarding_capture_activation_save_capability")
        finish = section.index("onboarding_finish_activation")
        expect(capture < finish, function + " captures before onboarding tuple clearing")
    expect("onboarding_activation_save_capability.o" in MAKEFILE,
           "the session seam is linked only into the legacy live object graph")
    legacy_consume = "onboarding_activation_save_capability_consume_for_explicit_save"
    published_consume = "onboarding_activation_save_capability_consume_published_explicit_save"
    expect(legacy_consume not in HEADER and legacy_consume not in SOURCE,
           "no command-id-only consuming API may remain production-callable")
    expect(published_consume in HEADER and BRIDGE.count(published_consume + "(") == 1,
           "the bridge is the sole published-capability consumer")
    finish = BRIDGE[BRIDGE.index("onboarding_activation_save_bridge_finish"):]
    expect(finish.index("CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED") <
           finish.index(published_consume + "("),
           "the bridge must consume only after V4 PUBLISHED")
    print("onboarding_activation_save_capability_static_test: ok")


if __name__ == "__main__":
    main()
