#!/usr/bin/env python3
import importlib.util
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "export-player-inventory.py"
SPEC = importlib.util.spec_from_file_location("export_player_inventory", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class ExportPlayerInventoryTest(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory(prefix="muhan-inventory-")
        self.home = Path(self.tempdir.name)
        (self.home / "player").mkdir()

        self.write("player/04/타봇", b"valid-player-secret")
        self.write("player/오/오래된", b"old-shard-record")
        self.write("player/04/중복", b"duplicate-a")
        self.write("player/ab/중복", b"duplicate-b")
        self.write("player/04/bad:name", b"invalid-name")
        self.write("player/92/Tester", b"canonical-ascii")
        self.write("player/ab/tester", b"noncanonical-ascii")
        (self.home / "player" / "04" / "not-a-file").mkdir()
        self.write("player/family/family_list", b"not-a-player")
        self.write("player/marriage/list", b"not-a-player")
        self.write("player/README", b"not-a-player")
        self.write("player/04/README", b"not-a-player")
        (self.home / "player" / "04" / "link-to-player").symlink_to("타봇")

    def tearDown(self):
        self.tempdir.cleanup()

    def write(self, relative, data):
        path = self.home / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)

    def run_export(self, strict=False):
        output = io.StringIO()
        errors = io.StringIO()
        argv = ["--mud-home", str(self.home)] + (["--strict"] if strict else [])
        with redirect_stdout(output), redirect_stderr(errors):
            code = MODULE.main(argv)
        records = [json.loads(line) for line in output.getvalue().splitlines()]
        return code, records, errors.getvalue()

    def test_deterministic_metadata_inventory_and_exclusions(self):
        code, records, errors = self.run_export()
        self.assertEqual(code, 0, errors)
        paths = [record["relative_path"] for record in records]
        self.assertEqual(paths, sorted(paths, key=lambda path: (Path(path).name, path)))
        self.assertNotIn("player/family/family_list", paths)
        self.assertNotIn("player/marriage/list", paths)
        self.assertNotIn("player/README", paths)
        self.assertNotIn("player/04/README", paths)
        self.assertNotIn("valid-player-secret", json.dumps(records, ensure_ascii=False))
        self.assertTrue(any(record["name"] == "타봇" and record["status"] == "ok" for record in records))
        ascii_records = [record for record in records if record["canonical_name_key"] == "Tester"]
        self.assertEqual(len(ascii_records), 2)
        self.assertTrue(all(record["duplicate_name"] for record in ascii_records))
        tester = next(record for record in ascii_records if record["name"] == "tester")
        self.assertEqual(tester["status"], "name_not_canonical")
        self.assertTrue(tester["name_not_canonical"])
        self.assertFalse(tester["shard_match"])
        old = next(record for record in records if record["name"] == "오래된")
        self.assertFalse(old["shard_match"])
        self.assertEqual(old["status"], "shard_mismatch")
        self.assertEqual(old["relative_path"], "player/오/오래된")
        link = next(record for record in records if record["name"] == "link-to-player")
        self.assertEqual(link["status"], "non_regular")
        self.assertIsNone(link["sha256"])

    def test_strict_rejects_all_migration_findings_but_keeps_records(self):
        code, records, errors = self.run_export(strict=True)
        self.assertEqual(code, 1, errors)
        duplicate = [record for record in records if record["name"] == "중복"]
        self.assertEqual(len(duplicate), 2)
        self.assertTrue(all(record["duplicate_name"] for record in duplicate))
        self.assertTrue(any(record["status"] == "invalid_name" for record in records))
        self.assertTrue(any(record["status"] == "name_not_canonical" for record in records))
        self.assertTrue(any(record["status"] == "shard_mismatch" for record in records))
        self.assertTrue(any(record["status"] == "non_regular" for record in records))

    def test_sha1_shard_and_output_are_repeatable(self):
        first = self.run_export()[1]
        second = self.run_export()[1]
        self.assertEqual(first, second)
        korean = next(record for record in first if record["name"] == "타봇")
        self.assertEqual(korean["expected_shard"], "04")
        self.assertEqual(korean["byte_size"], len(b"valid-player-secret"))
        self.assertEqual(korean["sha256"], hashlib.sha256(b"valid-player-secret").hexdigest())

    def test_name_validation_matches_c_contract(self):
        self.assertEqual(MODULE.validate_name("가나다"), (True, None))
        self.assertEqual(MODULE.validate_name("가" * 4 + "a"), (True, None))
        self.assertFalse(MODULE.validate_name("가" * 5)[0])  # 15 UTF-8 bytes
        self.assertFalse(MODULE.validate_name("a" * 13)[0])
        for forbidden in ("a/b", "a\\b", "a:b", "a\n", ".", ".."):
            self.assertFalse(MODULE.validate_name(forbidden)[0], forbidden)


if __name__ == "__main__":
    unittest.main()
