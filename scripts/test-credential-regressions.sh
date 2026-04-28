#!/bin/bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHECK_SCRIPT="$REPO_ROOT/scripts/check-credential-regressions.sh"
PASS=0
FAIL=0
TOTAL=0
TMP_ROOTS=""

cleanup() {
	for root in $TMP_ROOTS; do
		rm -rf "$root"
	done
}
trap cleanup EXIT INT TERM HUP

pass() {
	PASS=$((PASS + 1))
	TOTAL=$((TOTAL + 1))
	printf "  PASS %s\n" "$1"
}

fail() {
	FAIL=$((FAIL + 1))
	TOTAL=$((TOTAL + 1))
	printf "  FAIL %s\n" "$1"
}

phase() {
	printf "\n-- %s --\n\n" "$1"
}

make_root() {
	root="$(mktemp -d)"
	TMP_ROOTS="$TMP_ROOTS $root"
	mkdir -p "$root/hazmat/integrations" "$root/scripts" "$root/.hazmat/hooks"
	printf '%s\n' "$root"
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

write_allowed_tree() {
	root="$1"
	cat >"$root/hazmat/credential_registry.go" <<'EOF'
package main

func registryOwnedWrite(home string, raw []byte) error {
	return os.WriteFile(secretStoreDirForHome(home)+"/providers/example", raw, 0600)
}
EOF
	cat >"$root/hazmat/secret_store.go" <<'EOF'
package main

func storeOwnedWrite(home string, raw []byte) error {
	return os.WriteFile(home+"/.hazmat/secrets/providers/example", raw, 0600)
}
EOF
	cat >"$root/hazmat/config_agent.go" <<'EOF'
package main

func legacyGitHelper(agentHome string) {
	// credential-regression: allow legacy fixture until the Git HTTPS broker lands.
	helper := "store --file " + agentHome + "/.config/git/credentials"
	_ = helper
}
EOF
	cat >"$root/hazmat/integration_manifest.go" <<'EOF'
package main

var safeEnvKeys = map[string]bool{
	"GOPATH": true,
}
EOF
	cat >"$root/hazmat/integrations/go.yaml" <<'EOF'
session:
  env_passthrough: [GOPATH]
EOF
}

phase "Allowed registry-owned surfaces"

root="$(make_root)"
write_allowed_tree "$root"
assert_succeeds \
	"registry/store owners and documented legacy exception are allowed" \
	sh "$CHECK_SCRIPT" --root "$root"

phase "Rejected ad hoc credential surfaces"

root="$(make_root)"
cat >"$root/hazmat/config_agent.go" <<'EOF'
package main

func adHocGitHelper(agentHome string) {
	helper := "store --file " + agentHome + "/.config/git/credentials"
	_ = helper
}
EOF
assert_fails_with \
	"new Git credential.helper store is rejected" \
	"durable Git credential store paths" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/bootstrap.go" <<'EOF'
package main

func adHocSSHWrite(agentHome string, raw []byte) error {
	return agentWriteFile(agentHome+"/.ssh/id_ed25519", raw, 0600)
}
EOF
assert_fails_with \
	"new durable agent credential path write is rejected" \
	"durable /Users/agent credential-path writes" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/config_import.go" <<'EOF'
package main

func adHocHostStore(home string, raw []byte) error {
	return os.WriteFile(secretStoreDirForHome(home)+"/providers/example", raw, 0600)
}
EOF
assert_fails_with \
	"new host secret-store write outside owners is rejected" \
	"host secret-store writes must be owned" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/config.go" <<'EOF'
package main

func adHocLiteralWrite(home string, raw []byte) error {
	return os.WriteFile(home+"/.hazmat/secrets/providers/example", raw, 0600)
}
EOF
assert_fails_with \
	"new hard-coded host secret-store write is rejected" \
	"host secret-store writes must be owned" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/integration_manifest.go" <<'EOF'
package main

var safeEnvKeys = map[string]bool{
	"GITHUB_TOKEN": true,
}
EOF
assert_fails_with \
	"credential-shaped safeEnvKeys entry is rejected" \
	"credential-shaped env keys need SecretRef-backed delivery" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/integrations/bad-inline.yaml" <<'EOF'
session:
  env_passthrough: [AWS_SECRET_ACCESS_KEY]
EOF
assert_fails_with \
	"credential-shaped inline env_passthrough is rejected" \
	"credential-shaped integration env_passthrough" \
	sh "$CHECK_SCRIPT" --root "$root"

root="$(make_root)"
cat >"$root/hazmat/integrations/bad-block.yaml" <<'EOF'
session:
  env_passthrough:
    - GH_TOKEN
EOF
assert_fails_with \
	"credential-shaped block env_passthrough is rejected" \
	"credential-shaped integration env_passthrough" \
	sh "$CHECK_SCRIPT" --root "$root"

printf "\n"
if [ "$FAIL" -eq 0 ]; then
	printf "All %d credential-regression checks passed.\n" "$TOTAL"
else
	printf "%d/%d credential-regression checks failed.\n" "$FAIL" "$TOTAL"
fi

exit "$FAIL"
