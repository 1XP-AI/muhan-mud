#!/usr/bin/env python3
"""Guard the Git-index resource tree against macOS path collisions.

The source tree is the authoritative resource archive.  Check its index rather
than the checkout so this test is deterministic on both case-sensitive and
case-insensitive filesystems (and in sparse checkouts).
"""

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unicodedata


ROOT = pathlib.Path(__file__).resolve().parents[2]
MANIFEST_DIR = ROOT / "resources_manifest"


def git(*args: str) -> bytes:
    return subprocess.run(
        ["git", *args], cwd=ROOT, check=True, stdout=subprocess.PIPE
    ).stdout


def index_blobs() -> dict[str, str]:
    blobs: dict[str, str] = {}
    stages: list[str] = []

    for entry in git("ls-files", "--stage", "-z").split(b"\0"):
        if not entry:
            continue
        metadata, raw_path = entry.split(b"\t", 1)
        _mode, blob, stage = metadata.split()
        path = raw_path.decode("utf-8", "surrogateescape")
        if stage != b"0":
            stages.append(path)
            continue
        if path in blobs:
            raise AssertionError(f"duplicate index entry: {path}")
        blobs[path] = blob.decode("ascii")

    if stages:
        raise AssertionError("unmerged index entries: " + ", ".join(stages))
    return blobs


def manifest_aliases() -> dict[str, tuple[str, str]]:
    aliases: dict[str, tuple[str, str]] = {}
    lines = (MANIFEST_DIR / "path-alias.v1.tsv").read_text(encoding="utf-8").splitlines()
    expected_header = "legacy_path_hex\tlegacy_path_cp949\tnormalized_utf8_path\tblob_sha1"
    if not lines or lines[0] != expected_header:
        raise AssertionError("unexpected path alias manifest header")

    for line_no, line in enumerate(lines[1:], start=2):
        fields = line.split("\t")
        if len(fields) != 4:
            raise AssertionError(f"path alias line {line_no} must have four fields")
        legacy_hex, _legacy_path, normalized, blob = fields
        if legacy_hex in aliases:
            raise AssertionError(f"duplicate legacy alias: {legacy_hex}")
        aliases[legacy_hex] = (normalized, blob)
    return aliases


def available_git_blobs(blob_ids: set[str]) -> set[str]:
    if not blob_ids:
        return set()
    requested = sorted(blob_ids)
    result = subprocess.run(
        ["git", "cat-file", "--batch-check=%(objectname) %(objecttype)"],
        cwd=ROOT,
        check=True,
        input="".join(f"{blob}^{{blob}}\n" for blob in requested).encode("ascii"),
        stdout=subprocess.PIPE,
    )
    available: set[str] = set()
    for requested_blob, line in zip(requested, result.stdout.decode("ascii").splitlines()):
        fields = line.split()
        if len(fields) == 2 and fields[0] == requested_blob and fields[1] == "blob":
            available.add(requested_blob)
    return available


def regenerated_aliases() -> dict[str, tuple[str, str]]:
    def make_tree(entries: list[tuple[str, str, str, str]]) -> str:
        payload = b"".join(
            f"{mode} {kind} {object_id}\t{name}".encode("ascii") + b"\0"
            for mode, kind, object_id, name in entries
        )
        return subprocess.run(
            ["git", "mktree", "-z"],
            cwd=ROOT,
            input=payload,
            stdout=subprocess.PIPE,
            check=True,
        ).stdout.decode("ascii").strip()

    # Exercise relocation semantics with just the two source blobs.  The full
    # index is checked independently above; a synthetic tree keeps this
    # regeneration contract fast enough to run in every unit-test invocation.
    objmon_tree = make_tree([
        ("100644", "blob", "49b3a1975def2762e68f2663351ee55ffb387e61", "Celduin_sign"),
        ("100644", "blob", "4eb75435475df4df6d4cb20050754c1ab09baefe", "celduin_sign__4eb75435"),
    ])
    source_tree = make_tree([("040000", "tree", objmon_tree, "objmon")])
    with tempfile.TemporaryDirectory(prefix="muhan-resource-regeneration-") as temp_dir:
        temp_root = pathlib.Path(temp_dir)
        env = os.environ.copy()
        env["RESOURCE_SOURCE_TREE"] = source_tree
        subprocess.run(
            [
                str(ROOT / "tools/revive/extract-legacy-blobs.sh"),
                str(temp_root / "resources_utf8"),
                str(temp_root / "resources_manifest"),
            ],
            cwd=ROOT,
            env=env,
            check=True,
            stdout=subprocess.PIPE,
        )
        generated = json.loads(
            (temp_root / "resources_manifest/resource-map.v1.json").read_text(encoding="utf-8")
        )
    return {
        row["legacy_path_hex"]: (row["normalized_utf8_path"], row["blob_sha1"])
        for row in generated
    }


def main() -> int:
    errors: list[str] = []
    blobs = index_blobs()

    casefolded: dict[str, list[str]] = {}
    for path in blobs:
        key = unicodedata.normalize("NFD", path).casefold()
        casefolded.setdefault(key, []).append(path)
    for paths in casefolded.values():
        if len(paths) > 1:
            errors.append("NFD/casefold collision: " + " | ".join(sorted(paths)))

    aliases = manifest_aliases()
    resource_map = json.loads((MANIFEST_DIR / "resource-map.v1.json").read_text(encoding="utf-8"))
    mapped = {
        row["legacy_path_hex"]: (row["normalized_utf8_path"], row["blob_sha1"])
        for row in resource_map
    }
    if len(mapped) != len(resource_map):
        errors.append("resource-map.v1.json contains duplicate legacy aliases")
    if aliases != mapped:
        errors.append("path-alias.v1.tsv and resource-map.v1.json disagree")

    available = available_git_blobs({blob for _normalized, blob in aliases.values()})
    for legacy_hex, (normalized, blob) in aliases.items():
        if len(blob) != 40 or any(ch not in "0123456789abcdef" for ch in blob):
            errors.append(f"{legacy_hex}: blob_sha1 is not a lowercase Git SHA-1: {blob}")
            continue
        target = f"resources_utf8/{normalized}"
        if blobs.get(target) != blob:
            errors.append(
                f"{legacy_hex}: {target} has {blobs.get(target)!r}, expected manifest blob {blob}"
            )
        if blob not in available:
            errors.append(f"{legacy_hex}: {blob} is not an available Git blob")

    # These aliases also exist as package seed files in objmon/.  Keep the
    # source archive casefold-safe while preserving both historical blobs.
    canonical_objmon = {
        "objmon/Celduin_sign": "49b3a1975def2762e68f2663351ee55ffb387e61",
        "objmon/celduin_sign__4eb75435": "4eb75435475df4df6d4cb20050754c1ab09baefe",
    }
    for path, blob in canonical_objmon.items():
        if blobs.get(path) != blob:
            errors.append(f"{path} has {blobs.get(path)!r}, expected {blob}")
    if "objmon/celduin_sign" in blobs:
        errors.append("legacy collision path remains: objmon/celduin_sign")

    regenerated = regenerated_aliases()
    for legacy_hex, expected in {
        "6F626A6D6F6E2F43656C6475696E5F7369676E": (
            "objmon/Celduin_sign",
            "49b3a1975def2762e68f2663351ee55ffb387e61",
        ),
        "6F626A6D6F6E2F63656C6475696E5F7369676E": (
            "objmon/celduin_sign__4eb75435",
            "4eb75435475df4df6d4cb20050754c1ab09baefe",
        ),
    }.items():
        if regenerated.get(legacy_hex) != expected:
            errors.append(
                f"regeneration lost {legacy_hex}: {regenerated.get(legacy_hex)!r}, expected {expected!r}"
            )

    if errors:
        print("resource tree manifest test failed:", file=sys.stderr)
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        return 1

    print(f"resource tree manifest ok: {len(blobs)} index paths, {len(aliases)} aliases")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
