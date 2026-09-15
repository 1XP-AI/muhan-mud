#!/usr/bin/env python3
"""Build a deterministic, metadata-only imported-unclaimed review manifest.

This is intentionally a dry-run-only transform.  It reads exporter JSONL and
prints a closed manifest; it has no database, network, player-file, or apply
mode.  The manifest omits source paths and every raw player-file field.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
from typing import Any, Dict, Iterable, List, Optional, Tuple


FORMAT = "muhan.imported_unclaimed_manifest"
FORMAT_VERSION = 1
MAX_INPUT_BYTES = 64 * 1024 * 1024
MAX_RECORDS = 100_000
MAX_NAME_CODEPOINTS = 12
MAX_NAME_BYTES = 14
MAX_SOURCE_BYTES = 64 * 1024 * 1024
SHA256_HEX_LENGTH = 64

EXPORTER_FIELDS = {
    "name",
    "canonical_name_key",
    "name_not_canonical",
    "relative_path",
    "observed_shard",
    "expected_shard",
    "shard_match",
    "valid_name",
    "byte_size",
    "sha256",
    "status",
    "duplicate_name",
}


def canonical_name_key(name: str) -> str:
    """Match C lowercize(name, 1) and the game-identity canonicalizer."""
    folded = "".join(
        chr(ord(character) + 32) if "A" <= character <= "Z" else character
        for character in name
    )
    if folded and "a" <= folded[0] <= "z":
        return chr(ord(folded[0]) - 32) + folded[1:]
    return folded


def expected_shard(name: str) -> str:
    return hashlib.sha1(canonical_name_key(name).encode("utf-8")).hexdigest()[:2]


def valid_name(name: str) -> bool:
    try:
        encoded = name.encode("utf-8")
    except UnicodeEncodeError:
        return False
    if not 1 <= len(name) <= MAX_NAME_CODEPOINTS or len(encoded) > MAX_NAME_BYTES:
        return False
    if name in {".", ".."}:
        return False
    return all(ord(character) >= 32 and ord(character) != 127 and character not in "/\\:" for character in name)


def valid_sha256(value: Any) -> bool:
    return isinstance(value, str) and len(value) == SHA256_HEX_LENGTH and all(character in "0123456789abcdef" for character in value)


def _read_inventory(path: Path) -> List[str]:
    """Read one bounded, regular, UTF-8 inventory without following symlinks."""
    descriptor = -1
    try:
        descriptor = os.open(os.fspath(path), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_size < 0 or info.st_size > MAX_INPUT_BYTES:
            raise ValueError("invalid inventory input")
        data = bytearray()
        while len(data) <= MAX_INPUT_BYTES:
            chunk = os.read(descriptor, min(1024 * 1024, MAX_INPUT_BYTES + 1 - len(data)))
            if not chunk:
                break
            data.extend(chunk)
        if len(data) > MAX_INPUT_BYTES:
            raise ValueError("invalid inventory input")
        after = os.fstat(descriptor)
        if (after.st_size, after.st_ino, after.st_dev, after.st_mtime_ns, after.st_ctime_ns) != (
            info.st_size, info.st_ino, info.st_dev, info.st_mtime_ns, info.st_ctime_ns
        ):
            raise ValueError("invalid inventory input")
        text = bytes(data).decode("utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        raise ValueError("invalid inventory input") from exc
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    lines = [line for line in text.splitlines() if line]
    if len(lines) > MAX_RECORDS:
        raise ValueError("invalid inventory input")
    return lines


def _rejection(legacy_name_key: Optional[str], reasons: Iterable[str]) -> Dict[str, Any]:
    record: Dict[str, Any] = {"reasons": sorted(set(reasons))}
    if legacy_name_key is not None:
        record["legacy_name_key"] = legacy_name_key
    return record


def _assess_line(line: str) -> Tuple[Optional[Dict[str, Any]], Dict[str, Any], Optional[str]]:
    """Return a candidate or a safe rejection; never retain raw source data."""
    try:
        value = json.loads(line)
    except json.JSONDecodeError:
        return None, _rejection(None, ["invalid_entry"]), None
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        return None, _rejection(None, ["invalid_entry"]), None

    name = value.get("name")
    if not isinstance(name, str) or not valid_name(name):
        reasons = ["invalid_entry"] if not isinstance(name, str) else ["invalid_name"]
        if isinstance(value.get("status"), str) and value["status"] != "ok":
            reasons.append("source_status_not_ok")
        if isinstance(name, str) and valid_name(name):
            reasons.append("noncanonical_name")
        shard = value.get("observed_shard")
        if isinstance(name, str) and not valid_name(name) and isinstance(shard, str):
            try:
                if shard != expected_shard(name):
                    reasons.append("shard_mismatch")
            except UnicodeEncodeError:
                pass
        return None, _rejection(None, reasons), None

    canonical = canonical_name_key(name)
    reasons: List[str] = []
    missing = EXPORTER_FIELDS.difference(value)
    unknown = set(value).difference(EXPORTER_FIELDS)
    if unknown or (missing - {"sha256"}):
        reasons.append("invalid_entry")
    if name != canonical:
        reasons.append("noncanonical_name")

    # The source fields are an admission boundary.  They are inspected but
    # never copied to the result except through the four closed candidate keys.
    required_boolean_fields = ("name_not_canonical", "shard_match", "valid_name", "duplicate_name")
    if any(not isinstance(value.get(field), bool) for field in required_boolean_fields):
        reasons.append("invalid_entry")
    elif value["name_not_canonical"] != (name != canonical) or not value["valid_name"]:
        reasons.append("invalid_entry")
    elif value["duplicate_name"]:
        reasons.append("duplicate_canonical_name")

    if value.get("canonical_name_key") != canonical:
        reasons.append("invalid_entry")
    if value.get("status") != "ok":
        reasons.append("source_status_not_ok")
    actual_shard = expected_shard(name)
    if value.get("observed_shard") != actual_shard or value.get("expected_shard") != actual_shard or value.get("shard_match") is not True:
        reasons.append("shard_mismatch")
    if value.get("relative_path") != f"player/{actual_shard}/{name}":
        reasons.append("invalid_entry")

    source_size = value.get("byte_size")
    if not isinstance(source_size, int) or isinstance(source_size, bool) or not 0 <= source_size <= MAX_SOURCE_BYTES:
        reasons.append("invalid_source_size")
    source_sha256 = value.get("sha256")
    if source_sha256 is None or "sha256" not in value:
        reasons.append("missing_source_digest")
    elif not valid_sha256(source_sha256):
        reasons.append("invalid_source_digest")

    rejection = _rejection(canonical, reasons)
    if rejection["reasons"]:
        return None, rejection, canonical
    return {
        "legacy_name_key": canonical,
        "legacy_shard": actual_shard,
        "source_sha256": source_sha256,
        "source_size": source_size,
    }, rejection, canonical


def build_manifest(lines: Iterable[str]) -> Dict[str, Any]:
    assessed = [_assess_line(line) for line in lines]
    names: Dict[str, int] = {}
    for _, _, key in assessed:
        if key is not None:
            names[key] = names.get(key, 0) + 1

    candidates: List[Dict[str, Any]] = []
    rejections: List[Dict[str, Any]] = []
    for candidate, rejection, key in assessed:
        if key is not None and names[key] > 1:
            rejection = _rejection(key, [*rejection["reasons"], "duplicate_canonical_name"])
            candidate = None
        if candidate is None:
            rejections.append(rejection)
        else:
            candidates.append(candidate)
    return {
        "format": FORMAT,
        "format_version": FORMAT_VERSION,
        "dry_run": True,
        "candidates": sorted(candidates, key=lambda record: (record["legacy_name_key"], record["legacy_shard"], record["source_sha256"], record["source_size"])),
        "rejections": sorted(rejections, key=lambda record: (0 if "legacy_name_key" in record else 1, record.get("legacy_name_key", ""), record["reasons"])),
    }


def _write_new_manifest(path: Path, encoded: str) -> None:
    """Publish one complete owner-only manifest without replacing an entry.

    The parent directory is a trusted, access-controlled deployment boundary.
    A private temporary regular file in that directory is fully written and
    synced before a hard link atomically claims the previously absent output
    name.  Link creation fails if another entry won the name race.
    """
    data = encoded.encode("ascii")
    descriptor = -1
    temporary: Optional[Path] = None
    try:
        descriptor, temporary_name = tempfile.mkstemp(
            prefix=f".{path.name}.", suffix=".tmp", dir=os.fspath(path.parent)
        )
        temporary = Path(temporary_name)
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            raise OSError("unsafe manifest output")
        os.fchmod(descriptor, stat.S_IRUSR | stat.S_IWUSR)
        while data:
            written = os.write(descriptor, data)
            if written <= 0:
                raise OSError("failed to write manifest output")
            data = data[written:]
        os.fsync(descriptor)
        if descriptor >= 0:
            os.close(descriptor)
            descriptor = -1
        os.link(os.fspath(temporary), os.fspath(path), follow_symlinks=False)
        os.unlink(os.fspath(temporary))
        temporary = None
    except BaseException:
        if temporary is not None:
            try:
                os.unlink(os.fspath(temporary))
            except OSError:
                pass
        raise
    finally:
        if descriptor >= 0:
            try:
                os.close(descriptor)
            except OSError:
                pass


def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", required=True, type=Path, help="metadata-only JSONL emitted by export-player-inventory.py")
    parser.add_argument("--output", type=Path, default=None, help="manifest destination (default: stdout)")
    return parser.parse_args(argv)


def main(argv: Optional[List[str]] = None) -> int:
    args = parse_args(argv)
    try:
        manifest = build_manifest(_read_inventory(args.inventory))
        encoded = json.dumps(manifest, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n"
        if args.output is None or str(args.output) == "-":
            sys.stdout.write(encoded)
        else:
            _write_new_manifest(args.output, encoded)
    except (OSError, ValueError):
        print("build-imported-unclaimed-manifest: failed safely", file=sys.stderr)
        return 2
    return 1 if manifest["rejections"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
