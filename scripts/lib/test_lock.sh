#!/bin/bash

HAZMAT_TEST_SUITE_LOCKDIR="${TMPDIR:-/tmp}/hazmat-test-suite.lock"

acquire_hazmat_test_suite_lock() {
    local owner="${1:-unknown}"
    local holder_pid=""
    local holder_owner=""

    if mkdir "$HAZMAT_TEST_SUITE_LOCKDIR" 2>/dev/null; then
        :
    else
        if [ -f "$HAZMAT_TEST_SUITE_LOCKDIR/pid" ]; then
            holder_pid=$(cat "$HAZMAT_TEST_SUITE_LOCKDIR/pid" 2>/dev/null || true)
        fi
        if [ -f "$HAZMAT_TEST_SUITE_LOCKDIR/owner" ]; then
            holder_owner=$(cat "$HAZMAT_TEST_SUITE_LOCKDIR/owner" 2>/dev/null || true)
        fi
        if [ -n "$holder_pid" ] && kill -0 "$holder_pid" 2>/dev/null; then
            echo "error: another Hazmat host-side test is already running." >&2
            echo "holder: ${holder_owner:-unknown} (pid $holder_pid)" >&2
            echo "Run host-side test entrypoints one at a time." >&2
            exit 1
        fi
        rm -rf "$HAZMAT_TEST_SUITE_LOCKDIR"
        mkdir "$HAZMAT_TEST_SUITE_LOCKDIR"
    fi

    printf '%s\n' "$$" > "$HAZMAT_TEST_SUITE_LOCKDIR/pid"
    printf '%s\n' "$owner" > "$HAZMAT_TEST_SUITE_LOCKDIR/owner"
    trap 'rm -rf "$HAZMAT_TEST_SUITE_LOCKDIR"' EXIT INT TERM HUP
}
