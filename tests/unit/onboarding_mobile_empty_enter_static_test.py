#!/usr/bin/env python3
"""Keep the mobile onboarding command form able to send a required empty line."""

from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
COMPONENT = (ROOT / "web" / "components" / "onboarding-terminal.tsx").read_text(
    encoding="utf-8"
)


def require(value: bool, message: str) -> None:
    if not value:
        raise SystemExit(message)


require(
    "if (composingRef.current || !mobileLine) return;" not in COMPONENT,
    "mobile onboarding must not reject the legacy empty [enter] input",
)
require(
    "if (composingRef.current) return;" in COMPONENT,
    "only an active IME composition may suppress mobile onboarding submission",
)
require(
    "sendInputRef.current(" + chr(96) + "$" + "{mobileLine}\\n" + chr(96) + ")"
    in COMPONENT,
    "mobile onboarding must keep framing every line, including empty input, with a newline",
)
require(
    "disabled={!ready || !mobileLine}" not in COMPONENT,
    "the mobile send button must remain available for the required empty [enter] input",
)
require(
    COMPONENT.count("disabled={!ready}") >= 2,
    "both mobile input and send button must depend only on onboarding readiness",
)
print("onboarding_mobile_empty_enter_static_test: ok")
