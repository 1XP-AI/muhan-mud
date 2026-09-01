#!/usr/bin/env python3
"""Freeze the legacy struct declaration order used by files1.c raw prefixes."""

from __future__ import annotations

import re
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
MSTRUCT = ROOT / "src/mstruct.h"
FILES1 = ROOT / "src/files1.c"

OBJECT_FIELDS = [
    "name", "description", "key", "use_output", "value", "weight", "type",
    "adjustment", "shotsmax", "shotscur", "ndice", "sdice", "pdice", "armor",
    "wearflag", "magicpower", "magicrealm", "special", "flags", "questnum",
    "first_obj", "parent_obj", "parent_rom", "parent_crt",
]

CREATURE_FIELDS = [
    "name", "description", "talk", "password", "key", "fd", "level", "type",
    "class", "race", "numwander", "alignment", "strength", "dexterity",
    "constitution", "intelligence", "piety", "hpmax", "hpcur", "mpmax", "mpcur",
    "armor", "thaco", "experience", "gold", "ndice", "sdice", "pdice", "special",
    "proficiency", "realm", "spells", "flags", "quests", "questnum", "carry",
    "rom_num", "ready", "daily", "lasttime", "following", "first_fol", "first_obj",
    "first_enm", "first_tlk", "parent_rom",
]


def struct_body(source: str, name: str) -> str:
    match = re.search(
        rf"typedef\s+struct\s+{re.escape(name)}\s*\{{(?P<body>.*?)\}}\s*{re.escape(name)}\s*;",
        source,
        flags=re.DOTALL,
    )
    if not match:
        raise AssertionError(f"could not find typedef struct {name}")
    return match.group("body")


def declaration_names(body: str) -> list[str]:
    body = re.sub(r"/\*.*?\*/", "", body, flags=re.DOTALL)
    body = re.sub(r"//.*", "", body)
    body = re.sub(r"^\s*#.*$", "", body, flags=re.MULTILINE)
    names: list[str] = []
    for statement in body.split(";"):
        statement = statement.strip()
        if not statement or statement.startswith("#"):
            continue
        # The final identifier before array suffixes is the field name.  This
        # handles scalar, pointer, fixed-array, and nested struct declarations.
        match = re.search(r"([A-Za-z_]\w*)\s*(?:\[[^\]]*\]\s*)*$", statement)
        if match:
            names.append(match.group(1))
    return names


class CreatureObjectLayoutContractTest(unittest.TestCase):
    def test_object_declaration_order_is_frozen(self) -> None:
        source = MSTRUCT.read_text(encoding="utf-8")
        self.assertEqual(declaration_names(struct_body(source, "object")), OBJECT_FIELDS)

    def test_creature_declaration_order_is_frozen(self) -> None:
        source = MSTRUCT.read_text(encoding="utf-8")
        self.assertEqual(declaration_names(struct_body(source, "creature")), CREATURE_FIELDS)

    def test_raw_codec_still_uses_struct_prefix_then_count(self) -> None:
        source = FILES1.read_text(encoding="utf-8")
        self.assertRegex(source, r"write_full\(fd,\s*\(char \*\)obj_ptr,\s*sizeof\(object\)\)")
        self.assertRegex(source, r"write_full\(fd,\s*\(char \*\)crt_ptr,\s*sizeof\(creature\)\)")
        self.assertRegex(source, r"read\(fd,\s*obj_ptr,\s*sizeof\(object\)\)")
        self.assertRegex(source, r"read\(fd,\s*crt_ptr,\s*sizeof\(creature\)\)")

    def test_parser_rejects_declaration_order_drift(self) -> None:
        source = MSTRUCT.read_text(encoding="utf-8")
        body = struct_body(source, "object")
        drifted = re.sub(
            r"char\s+description\[80\];",
            "char drift_marker[80];",
            body,
            count=1,
        )
        self.assertNotEqual(declaration_names(drifted), OBJECT_FIELDS)


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(CreatureObjectLayoutContractTest)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)
