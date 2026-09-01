"""Static contract for the MUD1O claim credential lifetime.

The C unit exercises the wipe primitive byte-for-byte.  This contract keeps
the two ownership edges that are otherwise difficult to invoke in isolation:
the local command buffer is wiped after the callback, and central disconnect
wipes claim state before releasing io/extr/creature memory.
"""

from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
COMMAND1 = (ROOT / "src" / "command1.c").read_text(encoding="utf-8")
IO = (ROOT / "src" / "io.c").read_text(encoding="utf-8")


def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def test_claim_password_is_wiped_before_each_free_path() -> None:
    expect(
        "onboarding_session_zeroize_claim_memory" in COMMAND1,
        "MUD1O claim must use the explicit zeroization hook",
    )
    expect(
        "password_ok = Ply[fd].ply && strcmp" in COMMAND1
        and "onboarding_zero_claim_credentials(fd, str);" in COMMAND1,
        "claim must wipe password and transient input immediately after compare",
    )
    expect(
        "onboarding_zero_claim_credentials(fd, 0);\n\t\tdisconnect(fd);" in COMMAND1,
        "successful MUD1O claim must wipe before disconnect",
    )


def test_disconnect_has_claim_fail_safe_before_releasing_memory() -> None:
    marker = "Ply[fd].extr->onboarding_mode == ONBOARDING_ADMISSION_MODE_CLAIM"
    expect(marker in IO, "disconnect must identify the MUD1O claim lane")
    save_guard = IO.index("Ply[fd].ply->fd = -1", IO.index(marker))
    wipe = IO.index("onboarding_session_zeroize_claim_memory(", IO.index(marker))
    release = IO.index("free(Ply[fd].io);", IO.index(marker))
    expect(
        save_guard < wipe < release,
        "claim must become non-saveable and wipe credentials before disconnect frees io",
    )
    expect(
        "claim_input" in IO and "buf, sizeof(buf)" in IO,
        "handle_commands must wipe its local stack command buffer after callback",
    )


if __name__ == "__main__":
    test_claim_password_is_wiped_before_each_free_path()
    test_disconnect_has_claim_fail_safe_before_releasing_memory()
    print("onboarding_credential_lifecycle_test: ok")
