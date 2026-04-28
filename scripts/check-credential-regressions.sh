#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
MODE="${1:-repo}"
ROOT=""

usage() {
	echo "usage: $0 [repo|--staged|staged|--root DIR]" >&2
	exit 2
}

case "$MODE" in
	repo)
		;;
	--staged|staged)
		MODE="staged"
		;;
	--root)
		[ "${2:-}" != "" ] || usage
		MODE="root"
		ROOT="$2"
		;;
	*)
		usage
		;;
esac

TMPDIR_CREDENTIAL_SCAN="$(mktemp -d)"
REPORT="$TMPDIR_CREDENTIAL_SCAN/report.txt"

cleanup() {
	rm -rf "$TMPDIR_CREDENTIAL_SCAN"
}
trap cleanup EXIT INT TERM HUP

touch "$REPORT"

is_candidate_path() {
	case "$1" in
		.hazmat/hooks/*.sh|\
		.github/workflows/*.yml|\
		.github/workflows/*.yaml|\
		hazmat/*.go|\
		hazmat/*/*.go|\
		hazmat/*/*/*.go|\
		hazmat/integrations/*.yaml|\
		hazmat/integrations/*.yml|\
		scripts/*.sh|\
		scripts/pre-commit|\
		scripts/pre-push)
			return 0
			;;
	esac
	return 1
}

is_skipped_path() {
	case "$1" in
		scripts/check-credential-regressions.sh|\
		scripts/test-credential-regressions.sh)
			return 0
			;;
	esac
	return 1
}

scan_file() {
	path="$1"
	display="$2"
	status=0
	output="$(
		awk -v file="$display" '
function fail(msg) {
	printf "%s:%d: %s\n", file, NR, msg
	bad = 1
}

function is_secret_store_owner(path) {
	return path ~ /(^|\/)hazmat\/(secret_store|credential_registry)\.go$/
}

function credential_key(line) {
	return line ~ /(TOKEN|SECRET|API_KEY|PASSWORD|PRIVATE_KEY|ACCESS_KEY|AWS_[A-Z_]*KEY|GH_TOKEN|GITHUB_TOKEN)/
}

function agent_credential_path(line) {
	return line ~ /(\.config\/git\/credentials|\.ssh\/|\.aws\/|\.gnupg\/|\.config\/gh|\.config\/gcloud|\.claude\/\.credentials\.json|\.codex\/auth\.json|\.gemini\/oauth_creds\.json|\.local\/share\/opencode\/auth\.json)/
}

function agent_write_call(line) {
	return line ~ /(os\.WriteFile|agentWriteFile|SudoWriteFile|writeAgentSecretFile)/
}

function host_secret_write(line) {
	return line ~ /(os\.WriteFile|writeHostStoredSecretFile|SudoWriteFile)/
}

function host_secret_path(line) {
	return line ~ /(\.hazmat\/secrets|secretStoreDirForHome)/
}

BEGIN {
	bad = 0
	allow_next = 0
	in_safe_env_keys = 0
	in_yaml_env_passthrough = 0
}

{
	allowed = allow_next || $0 ~ /credential-regression: allow/
	if ($0 ~ /credential-regression: allow/) {
		allow_next = 1
	} else {
		allow_next = 0
	}
	if (allowed) {
		next
	}

	if ($0 ~ /credential\.helper/ && $0 ~ /store/) {
		fail("git credential.helper store must be declared as a credential registry or brokered credential")
	}
	if ($0 ~ /store --file/ && $0 ~ /\.config\/git\/credentials/) {
		fail("durable Git credential store paths must not be added ad hoc")
	}
	if (agent_write_call($0) && agent_credential_path($0)) {
		fail("durable /Users/agent credential-path writes must go through registry-backed session materialization")
	}
	if (host_secret_write($0) && host_secret_path($0) && !is_secret_store_owner(file)) {
		fail("host secret-store writes must be owned by secret_store.go or credential_registry.go")
	}

	if (file ~ /(^|\/)hazmat\/integration_manifest\.go$/) {
		if ($0 ~ /var safeEnvKeys[[:space:]]*=/) {
			in_safe_env_keys = 1
		} else if (in_safe_env_keys && $0 ~ /^}/) {
			in_safe_env_keys = 0
		}
		if (in_safe_env_keys && credential_key($0)) {
			fail("credential-shaped env keys need SecretRef-backed delivery instead of safeEnvKeys passthrough")
		}
	}

	if (file ~ /(^|\/)hazmat\/integrations\/.*\.ya?ml$/) {
		if ($0 ~ /env_passthrough:/) {
			in_yaml_env_passthrough = 1
			if (credential_key($0)) {
				fail("credential-shaped integration env_passthrough needs SecretRef-backed delivery")
			}
		} else if (in_yaml_env_passthrough && $0 ~ /^[^[:space:]-]/) {
			in_yaml_env_passthrough = 0
		} else if (in_yaml_env_passthrough && credential_key($0)) {
			fail("credential-shaped integration env_passthrough needs SecretRef-backed delivery")
		}
	}
}

END {
	exit bad
}
' "$path"
	)" || status=$?

	if [ -n "$output" ]; then
		printf '%s\n' "$output" >>"$REPORT"
	elif [ "$status" -ne 0 ]; then
		printf '%s: credential regression scan failed with status %s\n' "$display" "$status" >>"$REPORT"
	fi
}

scan_root() {
	root="$1"
	find "$root" -type f \( -name '*.go' -o -name '*.sh' -o -name '*.yaml' -o -name '*.yml' -o -name pre-commit -o -name pre-push \) | sort | while IFS= read -r path; do
		rel=${path#"$root"/}
		if is_candidate_path "$rel" && ! is_skipped_path "$rel"; then
			scan_file "$path" "$rel"
		fi
	done
}

scan_repo() {
	cd "$REPO_ROOT"
	git ls-files -- \
		'.hazmat/hooks/*.sh' \
		'.github/workflows/*.yml' \
		'.github/workflows/*.yaml' \
		'hazmat/*.go' \
		'hazmat/**/*.go' \
		'hazmat/integrations/*.yaml' \
		'hazmat/integrations/*.yml' \
		'scripts/*.sh' \
		'scripts/pre-commit' \
		'scripts/pre-push' | while IFS= read -r path; do
			if is_candidate_path "$path" && ! is_skipped_path "$path"; then
				scan_file "$REPO_ROOT/$path" "$path"
			fi
		done
}

scan_staged() {
	cd "$REPO_ROOT"
	staged_root="$TMPDIR_CREDENTIAL_SCAN/staged"
	mkdir -p "$staged_root"

	git diff --cached --name-only --diff-filter=ACMR | while IFS= read -r path; do
		if is_candidate_path "$path" && ! is_skipped_path "$path"; then
			dest="$staged_root/$path"
			mkdir -p "$(dirname "$dest")"
			git show ":$path" >"$dest"
		fi
	done

	scan_root "$staged_root"
}

case "$MODE" in
	repo)
		echo "credential-regression: tracked structural scan..."
		scan_repo
		;;
	staged)
		echo "credential-regression: staged structural scan..."
		scan_staged
		;;
	root)
		echo "credential-regression: structural scan under $ROOT..."
		scan_root "$ROOT"
		;;
esac

if [ -s "$REPORT" ]; then
	echo "credential-regression: found ad hoc credential handling:" >&2
	cat "$REPORT" >&2
	echo "credential-regression: route credentials through credential_registry.go/secret_store.go or add a reviewed credential-regression allow comment with the backing issue." >&2
	exit 1
fi

echo "credential-regression: no ad hoc credential handling found"
