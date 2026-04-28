#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
GITLEAKS_CONFIG="$SCRIPT_DIR/gitleaks.toml"

cd "$REPO_ROOT"

echo "pre-commit: staged diff sanity..."
git diff --cached --check

STAGED_FILES="$(git diff --cached --name-only --diff-filter=ACMR)"
if [ -z "$STAGED_FILES" ]; then
	echo "pre-commit: no staged files"
	exit 0
fi

sh "$REPO_ROOT/scripts/check-secret-patterns.sh" --staged

echo "pre-commit: gitleaks scan..."
gitleaks protect --staged --redact -v --no-banner --config "$GITLEAKS_CONFIG"

GO_FILES=""
SHELL_FILES=""
for path in $STAGED_FILES; do
	case "$path" in
		hazmat/*.go)
			GO_FILES="$GO_FILES $path"
			;;
		.beads/hooks/*|.hazmat/hooks/*.sh|scripts/*.sh|scripts/pre-commit|scripts/pre-push)
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
