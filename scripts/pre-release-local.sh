#!/bin/sh
# Local pre-release gate for checks that are too stateful for normal pre-push.

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

echo "pre-release-local: fast repository gate..."
bash "$REPO_ROOT/scripts/pre-push"

echo "pre-release-local: hermetic all-harness e2e smoke..."
bash "$REPO_ROOT/scripts/e2e-harness-smoke.sh"

echo "pre-release-local: all checks passed"
