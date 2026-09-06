#!/bin/sh
# Opt-in disposable PostgreSQL 17 contract runner.
#
# This script intentionally has no default database URL and is never invoked by
# the repository's normal test commands.  It neither creates nor drops a
# database: the caller must supply an already-created, empty disposable one.

set -eu

if [ "${PG17_CONTRACT_RUN:-}" != "1" ]; then
    echo "Refusing to run: set PG17_CONTRACT_RUN=1 to opt in." >&2
    exit 64
fi

if [ -z "${PG17_CONTRACT_URL:-}" ]; then
    echo "Refusing to run: PG17_CONTRACT_URL must name a caller-provided disposable database." >&2
    exit 64
fi

case "$PG17_CONTRACT_URL" in
    postgresql://*|postgres://*) ;;
    *)
        echo "Refusing to run: PG17_CONTRACT_URL must be a PostgreSQL connection URL." >&2
        exit 64
        ;;
esac

if ! command -v psql >/dev/null 2>&1; then
    echo "Refusing to run: psql is required for the explicit PG17 contract harness." >&2
    exit 69
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# The schema file commits its detached contract.  The assertion file opens one
# transaction and rolls back its role/grant/test-data setup before it exits.
psql "$PG17_CONTRACT_URL" -X -v ON_ERROR_STOP=1 \
    -f "$script_dir/alias_title_snapshot_manifest_v1.sql"
psql "$PG17_CONTRACT_URL" -X -v ON_ERROR_STOP=1 \
    -f "$script_dir/pg17_alias_title_snapshot_manifest_v1_contract.sql"
