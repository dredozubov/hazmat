#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
GITLEAKS_CONFIG="$SCRIPT_DIR/gitleaks.toml"
GOOGLE_API_KEY_PATTERN='AIza[0-9A-Za-z_-]{35}'

cd "$REPO_ROOT"

scan_staged_google_api_keys() {
	STAGED_FILES="$(git diff --cached --name-only --diff-filter=ACMR)"
	if [ -z "$STAGED_FILES" ]; then
		return 1
	fi
	git grep --cached -n -I -E "$GOOGLE_API_KEY_PATTERN" -- $STAGED_FILES
}

echo "pre-commit: staged diff sanity..."
git diff --cached --check

STAGED_FILES="$(git diff --cached --name-only --diff-filter=ACMR)"
if [ -z "$STAGED_FILES" ]; then
	echo "pre-commit: no staged files"
	exit 0
fi

echo "pre-commit: staged Google API key scan..."
if matches="$(scan_staged_google_api_keys)"; then
	echo "pre-commit: found provider-shaped Google API key material in staged content:" >&2
	printf '%s\n' "$matches" >&2
	echo "pre-commit: replace it with an obviously fake fixture before committing." >&2
	exit 1
fi

echo "pre-commit: gitleaks scan..."
gitleaks protect --staged --redact -v --no-banner --config "$GITLEAKS_CONFIG"

GO_FILES=""
SHELL_FILES=""
for path in $STAGED_FILES; do
	case "$path" in
		hazmat/*.go)
			GO_FILES="$GO_FILES $path"
			;;
		.hazmat/hooks/*.sh|scripts/*.sh|scripts/pre-commit|scripts/pre-push)
			SHELL_FILES="$SHELL_FILES $path"
			;;
	esac
done

if [ -n "$GO_FILES" ]; then
	echo "pre-commit: gofmt..."
	UNFORMATTED="$(gofmt -l $GO_FILES)"
	if [ -n "$UNFORMATTED" ]; then
		echo "pre-commit: gofmt required for staged files:" >&2
		for file in $UNFORMATTED; do
			echo "  $file" >&2
		done
		exit 1
	fi
fi

if [ -n "$SHELL_FILES" ]; then
	echo "pre-commit: shell syntax..."
	for file in $SHELL_FILES; do
		bash -n "$file"
	done
fi

echo "pre-commit: all checks passed"
