#!/bin/sh
# check-gitleaks.sh: broader secret-scanning layer alongside
# scripts/check-secret-patterns.sh.
#
# scripts/check-secret-patterns.sh is the fast regex-only first line
# (no dependencies). gitleaks runs as the second line and covers ~100 provider
# patterns plus generic high-entropy detection.
#
# Modes:
#   --staged   scan only staged changes (used by pre-commit)
#   detect     scan working tree (used by check-fast.sh and CI)
#
# Config: .hazmat/hooks/gitleaks.toml in the repo.

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
GITLEAKS_CONFIG="$REPO_ROOT/.hazmat/hooks/gitleaks.toml"
cd "$REPO_ROOT"

if ! command -v gitleaks >/dev/null 2>&1; then
	echo "gitleaks: not installed." >&2
	echo "gitleaks: install with: brew install gitleaks  (or: go install github.com/zricethezav/gitleaks/v8@latest)" >&2
	exit 1
fi

MODE="${1:-detect}"

case "$MODE" in
	--staged|staged)
		echo "gitleaks: scanning staged changes..."
		gitleaks protect --staged --redact -v --no-banner --config "$GITLEAKS_CONFIG"
		;;
	detect|--detect)
		# Scan via git (tracked files + history), NOT --no-git. The working tree
		# carries multi-GB untracked artefacts (Dolt DB at .beads/, TLC state files
		# under tla/states/, built binaries) that --no-git would traverse needlessly.
		echo "gitleaks: scanning git history..."
		gitleaks detect --redact -v --no-banner --config "$GITLEAKS_CONFIG"
		;;
	*)
		echo "usage: $0 [--staged|detect]" >&2
		exit 2
		;;
esac

echo "gitleaks: no secrets found"
