#!/usr/bin/env python3
"""Keep MUD1O provisioning behind the legacy creation confirmation gates."""

from pathlib import Path
import re


ROOT = Path(__file__).resolve().parents[2]
COMMAND = (ROOT / "src" / "command1.c").read_text(encoding="utf-8")


def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)


def function_body(name: str) -> str:
    start = COMMAND.index(name)
    opening = COMMAND.index("{", start)
    depth = 0
    for position in range(opening, len(COMMAND)):
        if COMMAND[position] == "{":
            depth += 1
        elif COMMAND[position] == "}":
            depth -= 1
            if depth == 0:
                return COMMAND[opening + 1:position]
    raise SystemExit(f"unterminated function: {name}")


def case_body(body: str, number: int) -> str:
    match = re.search(rf"^\s*case {number}:", body, re.MULTILINE)
    if not match:
        raise SystemExit(f"missing case {number}")
    following = re.search(r"^\s*(?:case \d+|default):", body[match.end():], re.MULTILINE)
    end = match.end() + following.start() if following else len(body)
    return body[match.start():end]


provision = function_body("void onboarding_provision")
name = case_body(provision, 2)
confirmation = case_body(provision, 3)
enter = case_body(provision, 4)
reserved = case_body(provision, 5)
commit = case_body(provision, 6)
activated = case_body(provision, 7)
create = function_body("void create_ply")
password = case_body(create, 8)
wizard_controls = function_body("int onboarding_control_during_wizard")

require("하시겠습니까(예/아니오)" in name,
        "MUD1O name acceptance must present the legacy confirmation")
require("ONBOARDING_CONTROL_RESERVE" not in name,
        "MUD1O must not reserve while the name is merely proposed")
require("strcmp((char *)str,\"예\")" in confirmation and
        "tempstr[0][0] = 0" in confirmation and
        "RETURN(fd, onboarding_provision, 2)" in confirmation,
        "declining the name must clear it and return to the name prompt")
require("[엔터]를 누르십시요" in confirmation and
        "RETURN(fd, onboarding_provision, 4)" in confirmation,
        "accepting the name must stop at the legacy [enter] gate")
require("ONBOARDING_CONTROL_RESERVE" in enter and
        "RETURN(fd, onboarding_provision, 5)" in enter,
        "only [enter] may advance MUD1O to RESERVE")
require("ONBOARDING_CONTROL_RESERVED" in reserved and "create_ply(fd, 1, 0)" in reserved,
        "the unchanged wizard must start only after RESERVE is acknowledged")
require("ONBOARDING_CONTROL_COMMIT" in commit and
        "RETURN(fd, onboarding_provision, 7)" in commit and
        "ONBOARDING_CONTROL_ACTIVATED" in activated,
        "the post-wizard provision lifecycle must retain COMMIT then ACTIVATED")
require("RETURN(fd, onboarding_provision, 6)" in password,
        "the existing gender/class/stats/weapon/alignment/race/password wizard must return to COMMIT")
require(all(f"Ply[fd].io->fnparam == {value}" in wizard_controls for value in (5, 6, 7)),
        "only post-[enter] gateway controls may bypass wizard input protection")
print("onboarding_provision_confirmation_static_test: ok")
