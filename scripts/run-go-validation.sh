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
  scripts/run-go-validation.sh integration
  scripts/run-go-validation.sh main

fast  : affected-package/race checks only; no ARM64 build or full repository scan
integration : full Go race/vet/diff gate; no cross-architecture build
main  : the one main-merge gate (integration plus Linux ARM64 cross-build)

The old 'merge' name is intentionally rejected so an ordinary integration check
cannot accidentally spend the main-merge ARM64 budget.

The known 63-room strict corpus exceptions are skipped by default. Set
GO_FAST_INCLUDE_STRICT=1 only when explicitly auditing that corpus.
EOF
}

collect_changed_files() {
	local base="${GO_FAST_BASE:-}"
	if [[ -n "$base" ]] && git -C "$root" rev-parse --verify "$base^{commit}" >/dev/null 2>&1; then
		git -C "$root" diff --name-only "$base"...HEAD
	else
		# Include unstaged lane work so a developer can run fast before committing.
		git -C "$root" diff --name-only HEAD
		git -C "$root" diff --name-only --cached
		git -C "$root" ls-files --others --exclude-standard
		if git -C "$root" rev-parse --verify HEAD^ >/dev/null 2>&1; then
			git -C "$root" diff --name-only HEAD^ HEAD
		fi
	fi
}

append_unique() {
	local candidate="$1"
	local existing
	for existing in "${packages[@]:-}"; do
		[[ "$existing" == "$candidate" ]] && return
	done
	packages+=("$candidate")
}

affected_packages() {
	local file
	local saw_server=0
	while IFS= read -r file; do
		[[ -n "$file" ]] || continue
		case "$file" in
			server/go.mod|server/go.sum|server/internal/engine/*|server/internal/game/*|server/internal/identity/*|server/internal/storage/*|server/internal/terminal/*|server/cmd/*)
				saw_server=1
				;;
			server/internal/world/*)
				append_unique ./internal/world
				append_unique ./internal/session
				append_unique ./internal/transport
				;;
			server/internal/session/*)
				append_unique ./internal/session
				append_unique ./internal/transport
				;;
			server/internal/transport/*)
				append_unique ./internal/transport
				;;
			esac
	done < <(collect_changed_files | sort -u)
	if [[ "$saw_server" == 1 ]]; then
		packages=(./internal/world ./internal/session ./internal/transport)
	fi
}

run_fast() {
	local -a packages=()
	local -a args=(-race -count=1)
	if [[ "${GO_FAST_INCLUDE_STRICT:-0}" != 1 ]]; then
		args+=(-skip '^TestRoomBodyCorpus$')
	fi
	if [[ -n "${GO_FAST_PACKAGES:-}" ]]; then
		if [[ "$GO_FAST_PACKAGES" == all ]]; then
			packages=(./internal/world ./internal/session ./internal/transport)
		else
			read -r -a packages <<<"$GO_FAST_PACKAGES"
		fi
	else
		affected_packages
	fi
	if ((${#packages[@]} == 0)) && [[ -n "${GO_FAST_RUN:-}" ]]; then
		# An explicit test-name filter is an intentional request even when the
		# current diff is documentation-only.
		packages=(./internal/world ./internal/session ./internal/transport)
	fi
	if ((${#packages[@]} == 0)); then
		echo 'No affected Go runtime packages; fast gate skipped (use GO_FAST_PACKAGES=all to override).'
		return 0
	fi
	if [[ -n "${GO_FAST_RUN:-}" ]]; then
		args+=(-run "$GO_FAST_RUN")
	fi
	(
		cd "$server_dir"
		echo "Fast Go packages: ${packages[*]}"
		go test "${args[@]}" "${packages[@]}"
	)
}

run_integration() {
	(
		cd "$server_dir"
		go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1
		go vet ./...
	)
	git -C "$root" diff --check
}

run_main() {
	run_integration
	(
		cd "$server_dir"
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
	)
}

case "$mode" in
	fast)
		run_fast
		;;
	integration)
		run_integration
		;;
	main)
		run_main
		;;
	merge)
		cat >&2 <<'EOF'
'merge' is intentionally disabled because it mixed ordinary integration checks
with the main-merge ARM64 budget. Use 'integration' for full Go race/vet, or
'main' exactly once when the assembled main branch needs the ARM64 cross-build.
EOF
		exit 2
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
