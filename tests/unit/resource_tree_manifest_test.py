#!/usr/bin/env python3
"""Guard the Git-index resource tree against macOS path collisions.

The source tree is the authoritative resource archive.  Check its index rather
than the checkout so this test is deterministic on both case-sensitive and
case-insensitive filesystems (and in sparse checkouts).
"""

import json
import os
import pathlib
import hashlib
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


def relocation_sources() -> dict[str, tuple[bytes, str, str]]:
    lines = (ROOT / "tools/revive/path-relocations.v1.tsv").read_text(encoding="utf-8").splitlines()
    expected_header = "source_path\tlegacy_path_hex\tnormalized_utf8_path\tblob_sha1"
    if not lines or lines[0] != expected_header:
        raise AssertionError("unexpected path relocation source header")
    rows: dict[str, tuple[bytes, str, str]] = {}
    for line_no, line in enumerate(lines[1:], start=2):
        source_path, legacy_hex, normalized, blob = line.split("\t")
        if source_path in rows:
            raise AssertionError(f"duplicate relocation source on line {line_no}: {source_path}")
        rows[source_path] = (bytes.fromhex(legacy_hex), normalized, blob)
    return rows


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


def make_raw_tree(entries: list[tuple[str, str, str, bytes]], env: dict[str, str]) -> str:
    """Create a tree whose entry names are passed to Git as literal bytes."""
    payload = b"".join(
        f"{mode} {kind} {object_id}\t".encode("ascii") + name + b"\0"
        for mode, kind, object_id, name in entries
    )
    return subprocess.run(
        ["git", "mktree", "-z"],
        cwd=ROOT,
        env=env,
        input=payload,
        stdout=subprocess.PIPE,
        check=True,
        timeout=5,
    ).stdout.decode("ascii").strip()


def make_temporary_blob(data: bytes, env: dict[str, str]) -> str:
    return subprocess.run(
        ["git", "hash-object", "-w", "--stdin"],
        cwd=ROOT,
        env=env,
        input=data,
        stdout=subprocess.PIPE,
        check=True,
        timeout=5,
    ).stdout.decode("ascii").strip()


def classify_blob(data: bytes) -> tuple[str, str]:
    if b"\x00" in data:
        return "binary", "binary"
    ctrl = sum(1 for byte in data if (byte < 9) or (13 < byte < 32))
    if data and (ctrl / len(data)) > 0.02:
        return "binary", "binary"
    try:
        text = data.decode("utf-8")
        return ("text", "ascii") if all(ord(char) < 128 for char in text) else ("text", "utf-8")
    except UnicodeDecodeError:
        pass
    try:
        data.decode("cp949")
        return "text", "cp949"
    except UnicodeDecodeError:
        return "binary", "binary"


def regenerate(
    source_tree: str,
    relocation_tsv: str | None = None,
    git_env: dict[str, str] | None = None,
) -> tuple[list[dict[str, object]], list[list[str]], dict[str, bytes]]:
    with tempfile.TemporaryDirectory(prefix="muhan-resource-regeneration-") as temp_dir:
        temp_root = pathlib.Path(temp_dir)
        env = os.environ.copy()
        if git_env is not None:
            env.update(git_env)
        env["RESOURCE_SOURCE_TREE"] = source_tree
        if relocation_tsv is not None:
            relocation_path = temp_root / "path-relocations.v1.tsv"
            relocation_path.write_text(relocation_tsv, encoding="utf-8")
            env["RESOURCE_PATH_RELOCATIONS"] = str(relocation_path)
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
            timeout=5,
        )
        resource_map = json.loads(
            (temp_root / "resources_manifest/resource-map.v1.json").read_text(encoding="utf-8")
        )
        alias_lines = (
            temp_root / "resources_manifest/path-alias.v1.tsv"
        ).read_text(encoding="utf-8").splitlines()
        if not alias_lines or alias_lines[0] != "legacy_path_hex\tlegacy_path_cp949\tnormalized_utf8_path\tblob_sha1":
            raise AssertionError("generated path alias header is invalid")
        aliases = [line.split("\t") for line in alias_lines[1:]]
        if any(len(row) != 4 for row in aliases):
            raise AssertionError("generated path alias row is invalid")
        extracted = {
            row["normalized_utf8_path"]: (
                temp_root / "resources_utf8" / row["normalized_utf8_path"]
            ).read_bytes()
            for row in resource_map
        }
        return resource_map, aliases, extracted


def assert_cp949_raw_path_regeneration() -> None:
    """A non-UTF-8 Git name is decoded and copied without touching the checkout."""
    blob_data = b"\xb0\xa1\xb0\xa2\n"
    raw_path = b"legacy/\xb0\xa1.txt"
    legacy_hex = "6C65676163792FB0A12E747874"
    normalized_path = "legacy/가.txt"
    blob_sha1 = "5fe20951640354f7de767d4a862231bf142f4a1c"
    expected_row: dict[str, object] = {
        "legacy_path_hex": legacy_hex,
        "legacy_path_cp949": normalized_path,
        "normalized_utf8_path": normalized_path,
        "blob_sha1": blob_sha1,
        "sha256": "68daf2a48cc572e46ea0d12c5e3cb34720fbaf8714321dba8b0ff148181331a9",
        "size": 5,
        "kind": "text",
        "text_encoding": "cp949",
    }
    empty_relocations = "source_path\tlegacy_path_hex\tnormalized_utf8_path\tblob_sha1\n"

    with tempfile.TemporaryDirectory(prefix="muhan-raw-path-objects-") as object_dir:
        git_env = {"GIT_OBJECT_DIRECTORY": object_dir}
        blob = make_temporary_blob(blob_data, git_env)
        leaf_tree = make_raw_tree(
            [("100644", "blob", blob, raw_path.rsplit(b"/", 1)[1])], git_env
        )
        source_tree = make_raw_tree(
            [("040000", "tree", leaf_tree, raw_path.split(b"/", 1)[0])], git_env
        )
        generated, aliases, extracted = regenerate(
            source_tree, empty_relocations, git_env
        )

    if blob != blob_sha1:
        raise AssertionError(f"temporary blob was {blob!r}, expected {blob_sha1}")
    if generated != [expected_row]:
        raise AssertionError(f"raw CP949 fixture regenerated {generated!r}, expected {[expected_row]!r}")
    expected_aliases = [[legacy_hex, normalized_path, normalized_path, blob_sha1]]
    if aliases != expected_aliases:
        raise AssertionError(f"raw CP949 aliases were {aliases!r}, expected {expected_aliases!r}")
    if extracted != {normalized_path: blob_data}:
        raise AssertionError("raw CP949 blob bytes were not copied unchanged")


def assert_non_cp949_raw_path_regeneration() -> None:
    """An undecodable name uses the deterministic raw-hex fallback, not locale decoding."""
    blob_data = b"\xb0\xa1\xb0\xa2\n"
    raw_path = b"legacy/\x80.txt"
    legacy_hex = "6C65676163792F802E747874"
    normalized_path = "legacy/__legacy_hex_802E747874"
    blob_sha1 = "5fe20951640354f7de767d4a862231bf142f4a1c"
    expected_row: dict[str, object] = {
        "legacy_path_hex": legacy_hex,
        "legacy_path_cp949": "",
        "normalized_utf8_path": normalized_path,
        "blob_sha1": blob_sha1,
        "sha256": "68daf2a48cc572e46ea0d12c5e3cb34720fbaf8714321dba8b0ff148181331a9",
        "size": 5,
        "kind": "text",
        "text_encoding": "cp949",
    }
    empty_relocations = "source_path\tlegacy_path_hex\tnormalized_utf8_path\tblob_sha1\n"

    with tempfile.TemporaryDirectory(prefix="muhan-raw-path-objects-") as object_dir:
        git_env = {"GIT_OBJECT_DIRECTORY": object_dir}
        blob = make_temporary_blob(blob_data, git_env)
        leaf_tree = make_raw_tree(
            [("100644", "blob", blob, raw_path.rsplit(b"/", 1)[1])], git_env
        )
        source_tree = make_raw_tree(
            [("040000", "tree", leaf_tree, raw_path.split(b"/", 1)[0])], git_env
        )
        generated, aliases, extracted = regenerate(
            source_tree, empty_relocations, git_env
        )

    if blob != blob_sha1:
        raise AssertionError(f"temporary blob was {blob!r}, expected {blob_sha1}")
    if generated != [expected_row]:
        raise AssertionError(
            f"non-CP949 raw fixture regenerated {generated!r}, expected {[expected_row]!r}"
        )
    expected_aliases = [[legacy_hex, "", normalized_path, blob_sha1]]
    if aliases != expected_aliases:
        raise AssertionError(
            f"non-CP949 raw aliases were {aliases!r}, expected {expected_aliases!r}"
        )
    if extracted != {normalized_path: blob_data}:
        raise AssertionError("non-CP949 raw blob bytes were not copied unchanged")


def regenerated_aliases() -> dict[str, tuple[str, str]]:
    # Exercise relocation semantics with just the two source blobs.  The full
    # index is checked independently above; a synthetic tree keeps this
    # regeneration contract fast enough to run in every unit-test invocation.
    objmon_tree = make_tree([
        ("100644", "blob", "49b3a1975def2762e68f2663351ee55ffb387e61", "Celduin_sign"),
        ("100644", "blob", "4eb75435475df4df6d4cb20050754c1ab09baefe", "celduin_sign__4eb75435"),
    ])
    source_tree = make_tree([("040000", "tree", objmon_tree, "objmon")])
    generated, _aliases, _extracted = regenerate(source_tree)
    return {
        row["legacy_path_hex"]: (row["normalized_utf8_path"], row["blob_sha1"])
        for row in generated
    }


def assert_three_way_same_blob_collision() -> None:
    blob = "49b3a1975def2762e68f2663351ee55ffb387e61"
    source_tree = make_tree([
        ("100644", "blob", blob, "FOO"),
        ("100644", "blob", blob, "Foo"),
        ("100644", "blob", blob, "foo"),
    ])
    generated, _aliases, _extracted = regenerate(
        source_tree,
        "source_path\tlegacy_path_hex\tnormalized_utf8_path\tblob_sha1\n",
    )
    normalized = [row["normalized_utf8_path"] for row in generated]
    fs_keys = {unicodedata.normalize("NFD", path).casefold() for path in normalized}
    if len(normalized) != 3 or len(set(normalized)) != 3 or len(fs_keys) != 3:
        raise AssertionError(f"three-way same-blob collision was not uniquely relocated: {normalized!r}")
    expected = ["FOO", "Foo__49b3a197", "foo__49b3a197_2"]
    if normalized != expected:
        raise AssertionError(f"three-way relocation was not deterministic: {normalized!r}")


def assert_regeneration_artifacts() -> None:
    source_entries = [
        ("objmon/Celduin_sign", "49b3a1975def2762e68f2663351ee55ffb387e61"),
        ("objmon/celduin_sign__4eb75435", "4eb75435475df4df6d4cb20050754c1ab09baefe"),
    ]
    objmon_tree = make_tree([
        ("100644", "blob", blob, source_path.rsplit("/", 1)[1])
        for source_path, blob in source_entries
    ])
    source_tree = make_tree([("040000", "tree", objmon_tree, "objmon")])
    generated, aliases, extracted_files = regenerate(source_tree)
    relocations = relocation_sources()
    expected_aliases = []
    expected_by_hex = {}
    for source_path, blob in source_entries:
        relocation = relocations.get(source_path)
        if relocation and relocation[2] != blob:
            raise AssertionError(
                f"{source_path}: relocation blob {relocation[2]!r} does not match fixture blob {blob!r}"
            )
        source_bytes = relocation[0] if relocation else source_path.encode("utf-8")
        legacy_hex = source_bytes.hex().upper()
        data = git("cat-file", "-p", blob)
        kind, encoding = classify_blob(data)
        expected_by_hex[legacy_hex] = {
            "legacy_path_hex": legacy_hex,
            "legacy_path_cp949": source_bytes.decode("cp949"),
            "blob_sha1": blob,
            "sha256": hashlib.sha256(data).hexdigest(),
            "size": len(data),
            "kind": kind,
            "text_encoding": encoding,
            "bytes": data,
            "normalized_utf8_path": relocation[1] if relocation else source_path,
        }

    generated_hexes = {row["legacy_path_hex"] for row in generated}
    if generated_hexes != set(expected_by_hex):
        raise AssertionError(
            f"regenerated legacy aliases were {sorted(generated_hexes)!r}, "
            f"expected {sorted(expected_by_hex)!r}"
        )
    for row in generated:
        expected = expected_by_hex.get(row["legacy_path_hex"])
        if expected is None:
            raise AssertionError(f"unexpected regenerated legacy alias: {row['legacy_path_hex']}")
        for field in (
            "legacy_path_hex",
            "legacy_path_cp949",
            "normalized_utf8_path",
            "blob_sha1",
            "sha256",
            "size",
            "kind",
            "text_encoding",
        ):
            if row[field] != expected[field]:
                raise AssertionError(f"{row['legacy_path_hex']}: {field} was {row[field]!r}, expected {expected[field]!r}")
        if extracted_files.get(row["normalized_utf8_path"]) != expected["bytes"]:
            raise AssertionError(f"{row['legacy_path_hex']}: extracted bytes differ from source blob")
        expected_aliases.append([
            expected["legacy_path_hex"],
            expected["legacy_path_cp949"],
            row["normalized_utf8_path"],
            expected["blob_sha1"],
        ])
    if sorted(aliases) != sorted(expected_aliases):
        raise AssertionError(f"generated aliases differ from source paths: {aliases!r}")


def main() -> int:
    errors: list[str] = []
    try:
        assert_cp949_raw_path_regeneration()
    except (AssertionError, subprocess.TimeoutExpired, subprocess.CalledProcessError) as exc:
        errors.append(f"raw CP949 path regeneration: {exc}")

    try:
        assert_non_cp949_raw_path_regeneration()
    except (AssertionError, subprocess.TimeoutExpired, subprocess.CalledProcessError) as exc:
        errors.append(f"raw non-CP949 path regeneration: {exc}")

    try:
        assert_three_way_same_blob_collision()
    except (AssertionError, subprocess.TimeoutExpired) as exc:
        errors.append(f"three-way relocation regression: {exc}")

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

    try:
        assert_regeneration_artifacts()
    except AssertionError as exc:
        errors.append(f"regeneration artifact verification: {exc}")

    if errors:
        print("resource tree manifest test failed:", file=sys.stderr)
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        return 1

    print(f"resource tree manifest ok: {len(blobs)} index paths, {len(aliases)} aliases")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
