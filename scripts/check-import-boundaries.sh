#!/bin/bash
#
# Guard: package-split architecture boundaries must stay structural.
#
# The Go test owns the policy because it can inspect go list output and parse
# Go files directly. This script is the stable CI/local entry point.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT/hazmat"

go test ./... -run 'TestImportBoundaries|TestPackageSplitDependencyGraph|TestImportBoundaryScript|TestVerifiedLedgerGovernedFunctionsExist'
