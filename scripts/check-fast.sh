#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
exec "$REPO_ROOT/.hazmat/hooks/pre-push.sh" "$@"
