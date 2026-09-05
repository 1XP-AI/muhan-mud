#!/usr/bin/env python3
"""Focused contract tests for the metadata-only imported-unclaimed manifest."""

import hashlib
import importlib.util
import io
import json
import os
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "build-imported-unclaimed-manifest.py"
FIXTURES = Path(__file__).with_name("fixtures")


def load_module():
    spec = importlib.util.spec_from_file_location("build_imported_unclaimed_manifest", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    assert spec and spec.loader
    spec.loader.exec_module(module)
    return module


class ImportedUnclaimedManifestTest(unittest.TestCase):
    def setUp(self):
        self.module = load_module()
        self.tempdir = tempfile.TemporaryDirectory(prefix="muhan-manifest-")
        self.inventory = Path(self.tempdir.name) / "inventory.jsonl"

    def tearDown(self):
        self.tempdir.cleanup()

    def write_lines(self, lines):
        self.inventory.write_text("\n".join(lines) + ("\n" if lines else ""), encoding="utf-8")

    def run_manifest(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = self.module.main(["--inventory", str(self.inventory)])
        return code, json.loads(stdout.getvalue()), stderr.getvalue(), stdout.getvalue()

    def run_manifest_to(self, output):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = self.module.main(["--inventory", str(self.inventory), "--output", str(output)])
        return code, stderr.getvalue(), stdout.getvalue()

    def record(self, name, payload=b"fixture", **overrides):
        shard = self.module.expected_shard(name)
        record = {
            "name": name,
            "canonical_name_key": name,
            "name_not_canonical": False,
            "relative_path": f"player/{shard}/{name}",
            "observed_shard": shard,
            "expected_shard": shard,
            "shard_match": True,
            "valid_name": True,
            "byte_size": len(payload),
            "sha256": hashlib.sha256(payload).hexdigest(),
            "status": "ok",
            "duplicate_name": False,
        }
        record.update(overrides)
        return json.dumps(record, sort_keys=True, separators=(",", ":"))

    def test_zero_one_and_multiple_candidates_have_versioned_stable_output(self):
        self.write_lines([])
        code, manifest, errors, serialized = self.run_manifest()
        self.assertEqual((code, errors), (0, ""))
        self.assertEqual(manifest["format_version"], 1)
        self.assertEqual(manifest["candidates"], [])
        self.assertEqual(manifest["rejections"], [])
        self.assertTrue(serialized.endswith("\n"))

        self.write_lines([self.record("Alice", b"alice")])
        code, manifest, errors, _ = self.run_manifest()
        self.assertEqual((code, errors), (0, ""))
        self.assertEqual(manifest["candidates"], [{
            "legacy_name_key": "Alice",
            "legacy_shard": "35",
            "source_sha256": hashlib.sha256(b"alice").hexdigest(),
            "source_size": 5,
        }])

        self.write_lines([self.record("Bob", b"bob"), self.record("Alice", b"alice")])
        first = self.run_manifest()
        self.write_lines([self.record("Alice", b"alice"), self.record("Bob", b"bob")])
        second = self.run_manifest()
        self.assertEqual(first[0:2], second[0:2])
        self.assertEqual(first[3], second[3])
        self.assertEqual([item["legacy_name_key"] for item in first[1]["candidates"]], ["Alice", "Bob"])

    def test_fixture_rejects_are_closed_and_never_leak_payload_or_paths(self):
        self.inventory.write_bytes((FIXTURES / "imported_unclaimed_manifest_inventory.jsonl").read_bytes())
        code, manifest, errors, serialized = self.run_manifest()
        expected = json.loads((FIXTURES / "imported_unclaimed_manifest_expected.json").read_text(encoding="utf-8"))
        self.assertEqual((code, errors), (1, ""))
        self.assertEqual(manifest, expected)
        self.assertNotIn("must-not-appear", serialized)
        self.assertNotIn("relative_path", serialized)
        self.assertNotIn("player/", serialized)
        self.assertNotIn("password", serialized)

    def test_boundaries_and_digest_failures_fail_closed(self):
        twelve_ascii = "Abcdefghijkl"
        fourteen_bytes = "가나다라ab"
        self.write_lines([
            self.record("A", b"one"),
            self.record(twelve_ascii, b"twelve"),
            self.record(fourteen_bytes, b"fourteen"),
            self.record("Baddigest", b"bad", sha256="A" * 64),
            self.record("Nodigest", b"none", sha256=None),
            self.record("Abcdefghijklm", b"x"),
        ])
        code, manifest, errors, _ = self.run_manifest()
        self.assertEqual((code, errors), (1, ""))
        self.assertEqual([item["legacy_name_key"] for item in manifest["candidates"]], ["A", twelve_ascii, fourteen_bytes])
        rejected = {item.get("legacy_name_key"): item["reasons"] for item in manifest["rejections"]}
        self.assertEqual(rejected["Baddigest"], ["invalid_source_digest"])
        self.assertEqual(rejected["Nodigest"], ["missing_source_digest"])
        self.assertIn(["invalid_name"], [item["reasons"] for item in manifest["rejections"] if "legacy_name_key" not in item])

    def test_duplicate_canonical_name_never_emits_a_candidate(self):
        upper = self.record("Alice", b"upper")
        lower_record = json.loads(self.record("alice", b"lower"))
        lower_record["canonical_name_key"] = "Alice"
        lower_record["name_not_canonical"] = True
        lower_record["relative_path"] = "player/35/alice"
        lower_record["expected_shard"] = "35"
        lower_record["observed_shard"] = "35"
        lower_record["shard_match"] = True
        lower_record["status"] = "name_not_canonical"
        self.write_lines([upper, json.dumps(lower_record, sort_keys=True, separators=(",", ":"))])
        code, manifest, errors, _ = self.run_manifest()
        self.assertEqual((code, errors), (1, ""))
        self.assertEqual(manifest["candidates"], [])
        self.assertEqual(manifest["rejections"], [
            {"legacy_name_key": "Alice", "reasons": ["duplicate_canonical_name"]},
            {"legacy_name_key": "Alice", "reasons": ["duplicate_canonical_name", "noncanonical_name", "source_status_not_ok"]},
        ])

    def test_output_refuses_existing_regular_file_without_changing_it(self):
        self.write_lines([self.record("Alice", b"alice")])
        output = Path(self.tempdir.name) / "credential-like-output.json"
        original = "must-stay-unchanged\n"
        output.write_text(original, encoding="utf-8")

        code, errors, stdout = self.run_manifest_to(output)

        self.assertEqual(code, 2)
        self.assertEqual(output.read_text(encoding="utf-8"), original)
        self.assertEqual(stdout, "")
        self.assertEqual(errors, "build-imported-unclaimed-manifest: failed safely\n")
        self.assertNotIn(str(output), errors)
        self.assertNotIn(str(self.inventory), errors)

    def test_output_refuses_symlink_without_changing_link_or_target(self):
        self.write_lines([self.record("Alice", b"alice")])
        target = Path(self.tempdir.name) / "target.json"
        target.write_text("target-must-stay-unchanged\n", encoding="utf-8")
        output = Path(self.tempdir.name) / "manifest-link.json"
        os.symlink(target, output)

        code, errors, stdout = self.run_manifest_to(output)

        self.assertEqual(code, 2)
        self.assertTrue(output.is_symlink())
        self.assertEqual(os.readlink(output), str(target))
        self.assertEqual(target.read_text(encoding="utf-8"), "target-must-stay-unchanged\n")
        self.assertEqual(stdout, "")
        self.assertEqual(errors, "build-imported-unclaimed-manifest: failed safely\n")

    def test_output_write_fsync_and_encoding_failures_leave_no_partial_final_or_temp_artifact(self):
        self.write_lines([self.record("Alice", b"alice")])
        for failure, patch_target in ((OSError("disk full"), "write"), (OSError("fsync failed"), "fsync")):
            with self.subTest(patch_target=patch_target):
                output = Path(self.tempdir.name) / f"{patch_target}-failure.json"
                with patch.object(self.module.os, patch_target, side_effect=failure):
                    code, errors, stdout = self.run_manifest_to(output)

                self.assertEqual((code, stdout), (2, ""))
                self.assertEqual(errors, "build-imported-unclaimed-manifest: failed safely\n")
                self.assertFalse(output.exists())
                self.assertEqual(list(output.parent.glob(f".{output.name}.*.tmp")), [])

        output = Path(self.tempdir.name) / "encoding-failure.json"
        with self.assertRaises(UnicodeEncodeError):
            self.module._write_new_manifest(output, "not-ascii: \ud800")
        self.assertFalse(output.exists())
        self.assertEqual(list(output.parent.glob(f".{output.name}.*.tmp")), [])

    def test_output_publication_race_preserves_competing_final_entries(self):
        self.write_lines([self.record("Alice", b"alice")])
        real_link = os.link

        for entry_type in ("regular", "symlink", "hardlink", "directory"):
            with self.subTest(entry_type=entry_type):
                output = Path(self.tempdir.name) / f"raced-{entry_type}.json"
                target = Path(self.tempdir.name) / f"raced-{entry_type}-target.json"

                def race(source, destination, *args, **kwargs):
                    destination = Path(destination)
                    if entry_type == "regular":
                        destination.write_text("racer regular\n", encoding="utf-8")
                    elif entry_type == "symlink":
                        target.write_text("racer symlink target\n", encoding="utf-8")
                        os.symlink(target, destination)
                    elif entry_type == "hardlink":
                        target.write_text("racer hardlink target\n", encoding="utf-8")
                        real_link(target, destination)
                    else:
                        destination.mkdir()
                    return real_link(source, destination, *args, **kwargs)

                with patch.object(self.module.os, "link", side_effect=race):
                    code, errors, stdout = self.run_manifest_to(output)

                self.assertEqual((code, stdout), (2, ""))
                self.assertEqual(errors, "build-imported-unclaimed-manifest: failed safely\n")
                if entry_type == "regular":
                    self.assertEqual(output.read_text(encoding="utf-8"), "racer regular\n")
                elif entry_type == "symlink":
                    self.assertTrue(output.is_symlink())
                    self.assertEqual(os.readlink(output), str(target))
                    self.assertEqual(target.read_text(encoding="utf-8"), "racer symlink target\n")
                elif entry_type == "hardlink":
                    self.assertEqual(output.read_text(encoding="utf-8"), "racer hardlink target\n")
                    self.assertEqual(os.stat(output).st_ino, os.stat(target).st_ino)
                else:
                    self.assertTrue(output.is_dir())
                    self.assertEqual(list(output.iterdir()), [])
                self.assertEqual(list(output.parent.glob(f".{output.name}.*.tmp")), [])

    def test_output_creates_only_new_closed_manifest_with_owner_only_mode(self):
        self.write_lines([self.record("Alice", b"alice")])
        output = Path(self.tempdir.name) / "new-manifest.json"

        code, errors, stdout = self.run_manifest_to(output)

        self.assertEqual((code, errors, stdout), (0, "", ""))
        self.assertFalse(output.is_symlink())
        self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
        expected = json.dumps(self.module.build_manifest(self.module._read_inventory(self.inventory)), ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n"
        self.assertEqual(output.read_text(encoding="ascii"), expected)
        manifest = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(set(manifest), {"candidates", "dry_run", "format", "format_version", "rejections"})
        self.assertEqual(set(manifest["candidates"][0]), {"legacy_name_key", "legacy_shard", "source_sha256", "source_size"})


if __name__ == "__main__":
    unittest.main()
