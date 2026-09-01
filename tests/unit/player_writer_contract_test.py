#!/usr/bin/env python3
"""Static guard for the M3 character/bank persistence boundary.

This is intentionally a source contract, not a C integration test.  It keeps
new canonical-player writes behind PlayerStore while documenting the two
existing archive moves and the bank direct-write bypass as explicit legacy
baselines.
"""

from __future__ import annotations

import re
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SRC = ROOT / "src"

PLAYER_STORE = SRC / "player_store.c"
PLAYER_FILE_STORE = SRC / "file_player_store.c"

# These are deliberately source-level exceptions.  They are not approved for
# the target design; changing or removing them is expected during later M3
# slices, while adding a new one should require a reviewed baseline update.
ARCHIVE_BYPASS_BASELINE = {
    SRC / "command5.c": "suicide shell archive move",
    SRC / "comman5_old.c": "inactive legacy suicide shell archive copy",
    SRC / "dm6.c": "DM lightning shell archive move",
}
BANK_BYPASS_BASELINE = SRC / "bank.c"


def c_sources() -> list[Path]:
    return sorted(SRC.glob("*.c"))


def direct_player_path_mutations_in_source(
    path: Path, text: str,
) -> list[tuple[Path, str]]:
    """Find raw writes whose first path is produced by player_path_from_name.

    This deliberately catches only operations on the canonical path variable;
    PLAYERPATH subtrees such as alias, mail, and family remain separate
    aggregates and are inventoried in the M3 document.
    """

    findings: list[tuple[Path, str]] = []
    assignment = re.compile(
        r"player_path_from_name\([^;\n]*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*,"
    )
    write_call = re.compile(
        r"\b(?:open|fopen|rp_open|rename|unlink|system)\s*\([^;\n]*"
        r"\b{path}\b[^;\n]*"
    )
    write_flags = re.compile(r"\bO_(?:WRONLY|RDWR|CREAT|TRUNC)\b|[\"']w\+?[\"']")

    for match in assignment.finditer(text):
        variable = match.group(1)
        # Limit the inspection to the function-ish source window.  This
        # avoids pairing an existence check with an unrelated writer in a
        # later function while remaining intentionally conservative.
        window = text[match.start() : match.start() + 1800]
        operation = write_call.pattern.format(path=re.escape(variable))
        for call in re.finditer(operation, window):
            snippet = call.group(0)
            if variable in snippet and (
                write_flags.search(snippet)
                or re.match(r"\s*(?:rename|unlink|system)\s*\(", snippet)
            ):
                findings.append((path, snippet.strip()))
        # The suicide path stores the canonical filename in a shell command
        # before invoking system(temp), so the path is not an argument to
        # system() itself.  Keep that archive move in the explicit baseline.
        archive = re.search(
            rf'sprintf\(\s*temp\s*,\s*"mv[^;\n]*\b{re.escape(variable)}\b'
            rf'[\s\S]{{0,500}}\bsystem\(\s*temp\s*\)',
            window,
        )
        if archive:
            findings.append((path, archive.group(0).strip()))
    return findings


def direct_player_path_mutations() -> list[tuple[Path, str]]:
    findings: list[tuple[Path, str]] = []
    for path in c_sources():
        findings.extend(
            direct_player_path_mutations_in_source(
                path, path.read_text(encoding="utf-8", errors="replace")
            )
        )
    return findings


class PlayerWriterContractTest(unittest.TestCase):
    def test_player_store_is_the_only_canonical_serializer_writer(self) -> None:
        source = PLAYER_STORE.read_text(encoding="utf-8")
        file_source = PLAYER_FILE_STORE.read_text(encoding="utf-8")

        self.assertIn("file_player_store_save", source)
        self.assertIn("file_player_store_load", source)
        self.assertIn("write_crt(", file_source)
        self.assertIn("mkstemp", file_source)
        self.assertIn("fsync", file_source)
        self.assertIn("rename(temp, file)", file_source)

        # A new direct serializer call is the regression this guard prevents.
        for path in c_sources():
            if path in {PLAYER_FILE_STORE, SRC / "files1.c"}:
                continue
            text = path.read_text(encoding="utf-8", errors="replace")
            self.assertNotRegex(
                text,
                r"\bfile_player_store_(?:save|load)\s*\(",
                f"{path.relative_to(ROOT)} bypasses PlayerStore",
            )
            self.assertNotRegex(
                text,
                r"\bwrite_crt\s*\(",
                f"{path.relative_to(ROOT)} directly serializes a player",
            )

    def test_raw_canonical_player_mutations_are_explicit_baselines(self) -> None:
        findings = direct_player_path_mutations()
        baseline_paths = set(ARCHIVE_BYPASS_BASELINE) | {PLAYER_FILE_STORE}
        unexpected = [
            (path, snippet)
            for path, snippet in findings
            if path not in baseline_paths
        ]
        self.assertEqual(
            unexpected,
            [],
            "new raw canonical player mutation; route it through PlayerStore "
            "or deliberately review/update the M3 baseline",
        )

        # Keep the current exceptions visible: a future cleanup must remove
        # them, not silently make the scanner less strict.
        for path, reason in ARCHIVE_BYPASS_BASELINE.items():
            text = path.read_text(encoding="utf-8", errors="replace")
            self.assertIn("player_path_from_name", text, reason)
            self.assertIn("system(", text, reason)

    def test_bank_direct_write_is_a_named_legacy_bypass(self) -> None:
        source = BANK_BYPASS_BASELINE.read_text(encoding="utf-8", errors="replace")
        self.assertRegex(source, r'sprintf\(file,\s*"%s/bank/%s"')
        self.assertIn("rp_open(file, O_RDWR | O_BINARY", source)
        self.assertIn("O_RDWR | O_CREAT | O_TRUNC", source)
        self.assertIn("write_obj(fd, obj_ptr, 0)", source)
        self.assertGreaterEqual(len(re.findall(r"\bload_bank\s*\(", source)), 1)
        self.assertGreaterEqual(len(re.findall(r"\bsave_bank\s*\(", source)), 1)

    def test_negative_fixture_would_fail_for_a_new_direct_writer(self) -> None:
        """The policy must reject a new path-derived O_TRUNC operation."""
        fake = """
        player_path_from_name(name, file, sizeof(file));
        fd = open(file, O_WRONLY | O_CREAT | O_TRUNC, 0600);
        """
        findings = direct_player_path_mutations_in_source(Path("fake.c"), fake)
        self.assertEqual(len(findings), 1)
        self.assertIn("O_TRUNC", findings[0][1])

        direct_serializer = "write_crt(fd, player, 0);"
        self.assertRegex(direct_serializer, r"\bwrite_crt\s*\(")


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(PlayerWriterContractTest)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)
