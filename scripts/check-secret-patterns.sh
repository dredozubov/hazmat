#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

MODE="${1:-repo}"
PATTERNS='
Google API key|AIza[0-9A-Za-z_-]{35}
Anthropic API key|sk-ant-(api[0-9]{2}-)?[A-Za-z0-9_-]{20,}
GitHub token|gh[pousr]_[A-Za-z0-9]{36}
GitHub fine-grained PAT|github_pat_[A-Za-z0-9_]{82,}
AWS access key ID|AKIA[0-9A-Z]{16}
OpenRouter API key|sk-or-v1-[0-9a-f]{64}
Context7 API key|ctx7sk-[0-9a-fA-F-]{36}
'

scan_repo_pattern() {
	regex="$1"
	git grep -n -I -E "$regex" -- .
}

scan_staged_pattern() {
	regex="$1"
	STAGED_FILES="$(git diff --cached --name-only --diff-filter=ACMR)"
	if [ -z "$STAGED_FILES" ]; then
		return 1
	fi
	git grep --cached -n -I -E "$regex" -- $STAGED_FILES
}

run_scan() {
	scope_label="$1"
	scan_fn="$2"
	found=0

	printf 'secret-check: %s secret-pattern scan...\n' "$scope_label"

	old_ifs="$IFS"
	IFS='
'
	for entry in $PATTERNS; do
		label=${entry%%|*}
		regex=${entry#*|}
		if [ -z "$label" ]; then
			continue
		fi
		if matches="$($scan_fn "$regex")"; then
			if [ "$found" -eq 0 ]; then
				echo "secret-check: found provider-shaped credential material:" >&2
				found=1
			fi
			printf '%s\n' "$matches" | sed "s/^/$label: /" >&2
		fi
	done
	IFS="$old_ifs"

	if [ "$found" -ne 0 ]; then
		echo "secret-check: replace it with an obviously fake example-* fixture before committing." >&2
		exit 1
	fi
}

case "$MODE" in
	repo)
		run_scan "tracked" scan_repo_pattern
		;;
	--staged|staged)
		run_scan "staged" scan_staged_pattern
		;;
	*)
		echo "usage: $0 [repo|--staged]" >&2
		exit 2
		;;
esac

echo "secret-check: no provider-shaped credential patterns found"
