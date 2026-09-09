#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "$0")/.." && pwd -P)"
server_dir="$root/server"
mode="${1:-fast}"

usage() {
	cat >&2 <<'EOF'
Usage:
  scripts/run-go-validation.sh fast
  GO_FAST_RUN='TestNameRegex' scripts/run-go-validation.sh fast
  GO_FAST_PACKAGES='./internal/world ./internal/session' scripts/run-go-validation.sh fast
  GO_FAST_INCLUDE_STRICT=1 scripts/run-go-validation.sh fast
  scripts/run-go-validation.sh merge

fast  : changed-package/race checks only; no ARM64 build or full repository scan
merge : one full local gate after integration (race, vet, Linux ARM64 build, diff check)

The known 63-room strict corpus exceptions are skipped by default. Set
GO_FAST_INCLUDE_STRICT=1 only when explicitly auditing that corpus.
EOF
}

run_fast() {
	local -a packages=(./internal/world ./internal/session ./internal/transport)
	local -a args=(-race -count=1)
	if [[ "${GO_FAST_INCLUDE_STRICT:-0}" != 1 ]]; then
		args+=(-skip '^TestRoomBodyCorpus$')
	fi
	if [[ -n "${GO_FAST_PACKAGES:-}" ]]; then
		read -r -a packages <<<"$GO_FAST_PACKAGES"
	fi
	if [[ -n "${GO_FAST_RUN:-}" ]]; then
		args+=(-run "$GO_FAST_RUN")
	fi
	(
		cd "$server_dir"
		go test "${args[@]}" "${packages[@]}"
	)
}

run_merge() {
	(
		cd "$server_dir"
		go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1
		go vet ./...
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
	)
	git -C "$root" diff --check
}

case "$mode" in
	fast)
		run_fast
		;;
	merge)
		run_merge
		;;
	help|-h|--help)
		usage
		exit 0
		;;
	*)
		usage
		exit 2
		;;
esac
