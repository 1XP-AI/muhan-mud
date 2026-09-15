#!/usr/bin/env python3
"""Keep the MUD1 file-authority harness on the real PlayerStore facade."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
TEST = (ROOT / "tests/unit/trusted_admission_file_authority_test.c").read_text(
    encoding="utf-8"
)
MAKEFILE = (ROOT / "src/Makefile").read_text(encoding="utf-8")


class TrustedAdmissionFileAuthorityStaticTest(unittest.TestCase):
    def test_harness_binds_the_production_player_store_facade(self) -> None:
        self.assertNotIn("int load_ply(", TEST)
        self.assertIn("return player_store_bind(&operations, binding);", TEST)
        self.assertIn("return player_store_unbind(binding)", TEST)

    def test_target_links_the_facade_implementation(self) -> None:
        target = MAKEFILE.split(
            "trusted-admission-file-authority-test:", 1
        )[1].split("\n\n", 1)[0]
        self.assertIn("player_store.c", target)


if __name__ == "__main__":
    result = unittest.main(verbosity=2, exit=False).result
    sys.exit(0 if result.wasSuccessful() else 1)
