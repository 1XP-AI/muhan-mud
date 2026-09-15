#!/usr/bin/env python3
"""Keep the raw legacy PlayerSnapshotV1 probe test-only and fail closed."""

from pathlib import Path


root = Path(__file__).resolve().parents[2]
harness = (root / "tests" / "harness" / "legacy_player_snapshot_v1_oracle.c").read_text(
    encoding="utf-8"
)
runner = (root / "scripts" / "run-legacy-player-snapshot-v1-differential.sh").read_text(
    encoding="utf-8"
)
makefile = (root / "src" / "Makefile").read_text(encoding="utf-8")

for required in (
    "LEGACY_PLAYER_SNAPSHOT_V1_RAW_ABI_CONTRACT",
    "legacy_raw_abi_contract",
    "raw_legacy_abi_supported",
    '"abi-fingerprint"',
    '"abi-check"',
):
    if required not in harness:
        raise SystemExit(f"raw legacy ABI contract is missing {required}")

for required in (
    "expected_abi=",
    '"$oracle" abi-fingerprint',
    '"$oracle" abi-check "$expected_abi"',
    '"$oracle" abi-check "$unsupported_abi"',
):
    if required not in runner:
        raise SystemExit(f"differential runner is missing closed ABI check {required}")

if runner.index("cargo test") > runner.index('"$oracle" abi-check "$expected_abi"'):
    raise SystemExit("portable Rust CDTO fixture validation must run before ABI admission")

for production_source in (root / "src").glob("*.c"):
    if "legacy_player_snapshot_v1_oracle" in production_source.read_text(
        encoding="utf-8"
    ):
        raise SystemExit(f"test oracle leaked into production source: {production_source}")

if "legacy-player-snapshot-v1-abi-static-test:" not in makefile:
    raise SystemExit("Makefile must expose the CI-safe ABI isolation check")

if "legacy-player-snapshot-v1-oracle-static-test: legacy-player-snapshot-v1-abi-static-test" not in makefile:
    raise SystemExit("oracle test must depend on the ABI isolation check")

print("legacy_player_snapshot_v1_abi_static_test: ok")
