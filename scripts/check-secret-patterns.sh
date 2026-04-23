#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

MODE="${1:-repo}"
PATTERN='AIza[0-9A-Za-z_-]{35}'

scan_repo() {
	git grep -n -I -E "$PATTERN" -- .
}

scan_staged() {
	STAGED_FILES="$(git diff --cached --name-only --diff-filter=ACMR)"
	if [ -z "$STAGED_FILES" ]; then
		return 1
	fi
	git grep --cached -n -I -E "$PATTERN" -- $STAGED_FILES
}

case "$MODE" in
	repo)
		echo "secret-check: tracked Google API key scan..."
		if matches="$(scan_repo)"; then
			echo "secret-check: found provider-shaped Google API key material:" >&2
			printf '%s\n' "$matches" >&2
			echo "secret-check: replace it with an obviously fake fixture before committing." >&2
			exit 1
		fi
		;;
	--staged|staged)
		echo "secret-check: staged Google API key scan..."
		if matches="$(scan_staged)"; then
			echo "secret-check: found provider-shaped Google API key material in staged content:" >&2
			printf '%s\n' "$matches" >&2
			echo "secret-check: replace it with an obviously fake fixture before committing." >&2
			exit 1
		fi
		;;
	*)
		echo "usage: $0 [repo|--staged]" >&2
		exit 2
		;;
esac

echo "secret-check: no provider-shaped Google API keys found"
