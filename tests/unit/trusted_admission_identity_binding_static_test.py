#!/usr/bin/env python3
"""Freeze MUD1's descriptor-only identity binding at the legacy load edge."""

from __future__ import annotations

import re
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
COMMAND = (ROOT / "src/command1.c").read_text(encoding="utf-8")
MSTRUCT = (ROOT / "src/mstruct.h").read_text(encoding="utf-8")
FILES1 = (ROOT / "src/files1.c").read_text(encoding="utf-8")


def function_body(source: str, name: str) -> str:
    match = re.search(rf"void\s+{name}\s*\(", source)
    if not match:
        raise AssertionError(f"could not find {name}")
    start = source.find("{", match.end())
    if start < 0:
        raise AssertionError(f"could not find {name} body")
    depth = 0
    for offset, character in enumerate(source[start:], start=start):
        if character == "{":
            depth += 1
        elif character == "}":
            depth -= 1
            if depth == 0:
                return source[start + 1:offset]
    raise AssertionError(f"could not close {name} body")


def struct_body(source: str, name: str) -> str:
    match = re.search(
        rf"typedef\s+struct\s+{re.escape(name)}\s*\{{(?P<body>.*?)\}}\s*{re.escape(name)}\s*;",
        source,
        flags=re.DOTALL,
    )
    if not match:
        raise AssertionError(f"could not find typedef struct {name}")
    return match.group("body")


class TrustedAdmissionIdentityBindingStaticTest(unittest.TestCase):
    def test_ticket_name_is_the_exact_loaded_player_name(self) -> None:
        login = function_body(COMMAND, "trusted_admission_login")
        self.assertEqual(login.count("load_ply(ticket.name, &ply_ptr)"), 2)
        self.assertEqual(login.count("strcmp(ply_ptr->name, ticket.name) != 0"), 2)
        self.assertLess(
            login.index("load_ply(ticket.name, &ply_ptr)"),
            login.index("Ply[fd].ply = ply_ptr;"),
        )

    def test_identity_metadata_is_descriptor_only_not_a_player_record(self) -> None:
        extra = struct_body(MSTRUCT, "extra")
        creature = struct_body(MSTRUCT, "creature")
        login = function_body(COMMAND, "trusted_admission_login")

        for field in ("auth_user_id", "character_id", "admission_nonce"):
            self.assertIn(field, extra)
            self.assertNotIn(field, creature)
            self.assertIn(f"Ply[fd].extr->{field}", login)
            self.assertNotIn(field, FILES1)

        self.assertRegex(FILES1, r"write_full\(fd,\s*\(char \*\)crt_ptr,\s*sizeof\(creature\)\)")


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(TrustedAdmissionIdentityBindingStaticTest)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)
