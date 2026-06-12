#!/bin/sh
# Hermetic fake-service smoke tests for future service harness adapters.

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT/hazmat"
go test ./internal/serviceharness
