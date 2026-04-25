#!/bin/bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHECK_SCRIPT="$REPO_ROOT/scripts/check-secret-patterns.sh"
PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { FAIL=$((FAIL + 1)); TOTAL=$((TOTAL + 1)); printf "  \033[31m✗\033[0m %s\n" "$1"; }
phase() { printf "\n\033[1m── %s ──\033[0m\n\n" "$1"; }

make_repo() {
	tmp="$(mktemp -d)"
	git -C "$tmp" init -q
	git -C "$tmp" config user.name "Hazmat Test"
	git -C "$tmp" config user.email "hazmat@example.com"
	printf 'seed\n' >"$tmp/README.md"
	git -C "$tmp" add README.md
	git -C "$tmp" commit -qm "init"
	printf '%s\n' "$tmp"
}

assert_fails_with() {
	label="$1"
	expected="$2"
	shift 2

	output=""
	status=0
	set +e
	output=$("$@" 2>&1)
	status=$?
	set -e

	if [ "$status" -eq 0 ]; then
		fail "$label: command unexpectedly succeeded"
		printf '%s\n' "$output" >&2
		return
	fi

	if printf '%s' "$output" | grep -Fq "$expected"; then
		pass "$label"
	else
		fail "$label: expected output containing '$expected'"
		printf '%s\n' "$output" >&2
	fi
}

assert_succeeds() {
	label="$1"
	shift

	output=""
	status=0
	set +e
	output=$("$@" 2>&1)
	status=$?
	set -e

	if [ "$status" -eq 0 ]; then
		pass "$label"
	else
		fail "$label: command unexpectedly failed"
		printf '%s\n' "$output" >&2
	fi
}

run_in_repo() {
	repo="$1"
	mode="$2"
	(
		cd "$repo"
		sh "$CHECK_SCRIPT" "$mode"
	)
}

phase "Staged detection"

repo="$(make_repo)"
trap 'rm -rf "$repo" "${repo2:-}" "${repo3:-}" "${repo4:-}" "${repo5:-}"' EXIT INT TERM HUP
printf 'AIza12345678901234567890123456789012345\n' >"$repo/google.txt"
git -C "$repo" add google.txt
assert_fails_with \
	"staged Google key is rejected" \
	"Google API key:" \
	run_in_repo "$repo" staged
rm -rf "$repo"

repo2="$(make_repo)"
printf 'sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890\n' >"$repo2/anthropic.txt"
git -C "$repo2" add anthropic.txt
assert_fails_with \
	"staged Anthropic key is rejected" \
	"Anthropic API key:" \
	run_in_repo "$repo2" staged
rm -rf "$repo2"

repo3="$(make_repo)"
printf 'ghp_abcdefghijklmnopqrstuvwxyz1234567890\n' >"$repo3/github.txt"
git -C "$repo3" add github.txt
assert_fails_with \
	"staged GitHub token is rejected" \
	"GitHub token:" \
	run_in_repo "$repo3" staged
rm -rf "$repo3"

phase "Tracked detection"

repo4="$(make_repo)"
printf 'AKIA1234567890ABCDEF\n' >"$repo4/aws.txt"
git -C "$repo4" add aws.txt
git -C "$repo4" commit -qm "add aws fixture"
assert_fails_with \
	"tracked AWS access key is rejected" \
	"AWS access key ID:" \
	run_in_repo "$repo4" repo
rm -rf "$repo4"

phase "Safe fixtures"

repo5="$(make_repo)"
cat >"$repo5/examples.txt" <<'EOF'
example-anthropic-key
example-google-api-key
example-github-pat
example-aws-access-key-id
example-openrouter-key
example-context7-key
EOF
git -C "$repo5" add examples.txt
assert_succeeds \
	"example-* fixtures are allowed in staged content" \
	run_in_repo "$repo5" staged

printf "\n"
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
if [ "$FAIL" -eq 0 ]; then
	printf "\033[32m  All %d checks passed.\033[0m\n" "$TOTAL"
else
	printf "\033[31m  %d/%d checks failed.\033[0m\n" "$FAIL" "$TOTAL"
fi
printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

exit "$FAIL"
