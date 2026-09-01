#!/usr/bin/env python3
"""Export a deterministic, metadata-only inventory of MUHAN player files."""

from __future__ import annotations

import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
from typing import Any, Dict, Iterable, List, Optional, Tuple


MIN_NAME_CODEPOINTS = 1
MAX_NAME_CODEPOINTS = 12
MAX_NAME_BYTES = 14
NON_CHARACTER_DIRS = {
    "alias",
    "bank",
    "fal",
    "family",
    "invite",
    "marriage",
    "simul",
    "suic",
    "vote",
}
NON_CHARACTER_FILES = {"README"}


def canonical_name_key(name: str) -> str:
    """Match C lowercize(name, 1): ASCII-fold, then capitalize first ASCII."""
    folded = "".join(
        chr(ord(character) + 32) if "A" <= character <= "Z" else character
        for character in name
    )
    if folded and "a" <= folded[0] <= "z":
        folded = chr(ord(folded[0]) - 32) + folded[1:]
    return folded


def expected_shard(name: str) -> str:
    return hashlib.sha1(canonical_name_key(name).encode("utf-8")).hexdigest()[:2]


def validate_name(name: str) -> Tuple[bool, Optional[str]]:
    try:
        encoded = name.encode("utf-8")
    except UnicodeEncodeError:
        return False, "filename is not valid UTF-8"
    if not MIN_NAME_CODEPOINTS <= len(name) <= MAX_NAME_CODEPOINTS:
        return False, "filename must contain 1..12 Unicode codepoints"
    if len(encoded) > MAX_NAME_BYTES:
        return False, "filename exceeds 14 UTF-8 bytes"
    if name in (".", ".."):
        return False, "dot filename is not a character name"
    for character in name:
        codepoint = ord(character)
        if codepoint < 32 or codepoint == 127:
            return False, "filename contains a control character"
        if character in "/\\:":
            return False, "filename contains a forbidden separator"
    return True, None


def relative_path(path: Path, mud_home: Path) -> str:
    return path.relative_to(mud_home).as_posix()


class NonRegularFileError(Exception):
    """Raised when an entry is not a regular file at the protected open."""


def metadata(path: Path) -> Tuple[int, str]:
    digest = hashlib.sha256()
    size = 0
    descriptor = -1
    try:
        # O_NONBLOCK keeps a FIFO/device from stalling an inventory run before
        # fstat can reject it; it is a no-op for regular files.
        descriptor = os.open(os.fspath(path), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        file_stat = os.fstat(descriptor)
        if not stat.S_ISREG(file_stat.st_mode):
            raise NonRegularFileError("entry is not a regular file")
        with os.fdopen(descriptor, "rb") as stream:
            descriptor = -1
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                size += len(chunk)
                digest.update(chunk)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    return size, digest.hexdigest()


def candidate_record(path: Path, observed: str, mud_home: Path) -> Dict[str, Any]:
    name = path.name
    canonical = canonical_name_key(name)
    valid, reason = validate_name(name)
    try:
        # Keep a migration hint even for a UTF-8 name rejected for length or
        # forbidden characters.  Names containing undecodable filesystem
        # bytes have no safe SHA-1 input and therefore retain null here.
        expected = expected_shard(name)
    except UnicodeEncodeError:
        expected = None
    record: Dict[str, Any] = {
        "name": name,
        "canonical_name_key": canonical,
        "name_not_canonical": name != canonical,
        "relative_path": relative_path(path, mud_home),
        "observed_shard": observed,
        "expected_shard": expected,
        "shard_match": bool(valid and expected is not None and observed == expected),
        "valid_name": valid,
        "byte_size": None,
        "sha256": None,
    }
    if reason:
        record["validation_error"] = reason

    if path.is_symlink():
        record["status"] = "non_regular"
        record["file_error"] = "symlink is not a regular player file"
        return record

    try:
        record["byte_size"], record["sha256"] = metadata(path)
    except NonRegularFileError as exc:
        record["status"] = "non_regular"
        record["file_error"] = str(exc)
        return record
    except OSError as exc:
        if exc.errno == errno.ELOOP:
            record["status"] = "non_regular"
            record["file_error"] = "symlink is not a regular player file"
            return record
        record["status"] = "unreadable"
        record["file_error"] = str(exc)
        return record

    if not valid:
        record["status"] = "invalid_name"
    elif name != canonical:
        record["status"] = "name_not_canonical"
    elif observed != expected:
        record["status"] = "shard_mismatch"
    else:
        record["status"] = "ok"
    return record


def iter_candidates(player_root: Path, mud_home: Path) -> Iterable[Dict[str, Any]]:
    try:
        children = sorted(player_root.iterdir(), key=lambda path: path.name)
    except OSError as exc:
        raise RuntimeError(f"cannot read player root {player_root}: {exc}") from exc

    for shard_dir in children:
        if shard_dir.name in NON_CHARACTER_DIRS or shard_dir.name in NON_CHARACTER_FILES:
            continue
        if shard_dir.is_symlink():
            # A symlink at this level is in the candidate namespace.  Preserve
            # it as a fail-closed record without traversing outside MUHAN_HOME.
            yield candidate_record(shard_dir, shard_dir.name, mud_home)
            continue
        if not shard_dir.is_dir():
            # Unexpected files/FIFOs at player/ are not silently ignored: they
            # are retained as fail-closed records for strict inventory runs.
            yield candidate_record(shard_dir, shard_dir.name, mud_home)
            continue
        try:
            entries = sorted(shard_dir.iterdir(), key=lambda path: path.name)
        except OSError as exc:
            raise RuntimeError(f"cannot read shard directory {shard_dir}: {exc}") from exc
        for path in entries:
            if path.name in NON_CHARACTER_FILES:
                continue
            yield candidate_record(path, shard_dir.name, mud_home)


def inventory(mud_home: Path) -> List[Dict[str, Any]]:
    player_root = mud_home / "player"
    if not player_root.is_dir() or player_root.is_symlink():
        raise RuntimeError(f"MUHAN_HOME/player is not a regular directory: {player_root}")
    records = list(iter_candidates(player_root, mud_home))
    by_name: Dict[str, List[Dict[str, Any]]] = {}
    for record in records:
        by_name.setdefault(record["canonical_name_key"], []).append(record)
    for name_records in by_name.values():
        if len(name_records) > 1:
            for record in name_records:
                record["duplicate_name"] = True
                if record["status"] == "ok":
                    record["status"] = "duplicate"
        else:
            name_records[0]["duplicate_name"] = False
    return sorted(records, key=lambda record: (record["name"], record["relative_path"]))


def has_strict_findings(records: Iterable[Dict[str, Any]]) -> bool:
    return any(
        record["status"] in {"shard_mismatch", "invalid_name", "name_not_canonical", "duplicate", "non_regular", "unreadable"}
        or not record["valid_name"]
        or not record["shard_match"]
        or record.get("name_not_canonical", False)
        or record.get("duplicate_name", False)
        for record in records
    )


def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mud-home", type=Path, default=None, help="MUHAN_HOME (default: $MUHAN_HOME)")
    parser.add_argument("--output", type=Path, default=None, help="JSONL destination (default: stdout)")
    parser.add_argument("--strict", action="store_true", help="return nonzero for mismatch, invalid, duplicate, or non-regular entries")
    return parser.parse_args(argv)


def main(argv: Optional[List[str]] = None) -> int:
    args = parse_args(argv)
    mud_home_value = args.mud_home or (Path(os.environ["MUHAN_HOME"]) if os.environ.get("MUHAN_HOME") else None)
    if mud_home_value is None:
        print("export-player-inventory: --mud-home or MUHAN_HOME is required", file=sys.stderr)
        return 2
    mud_home = mud_home_value.expanduser().resolve()
    try:
        records = inventory(mud_home)
    except (OSError, RuntimeError, ValueError) as exc:
        print(f"export-player-inventory: {exc}", file=sys.stderr)
        return 2

    stream = sys.stdout
    close_stream = False
    if args.output is not None and str(args.output) != "-":
        try:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            stream = args.output.open("w", encoding="utf-8", newline="\n")
            close_stream = True
        except OSError as exc:
            print(f"export-player-inventory: cannot open output: {exc}", file=sys.stderr)
            return 2
    try:
        for record in records:
            # ASCII escaping keeps JSON valid even when the filesystem
            # exposes a filename containing undecodable bytes (represented by
            # Python surrogate codepoints).  It does not expose file bytes.
            stream.write(json.dumps(record, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n")
    except OSError as exc:
        print(f"export-player-inventory: cannot write output: {exc}", file=sys.stderr)
        return 2
    finally:
        if close_stream:
            stream.close()
    return 1 if args.strict and has_strict_findings(records) else 0


if __name__ == "__main__":
    raise SystemExit(main())
