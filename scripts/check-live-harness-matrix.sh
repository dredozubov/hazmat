#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONTRACT="$REPO_ROOT/docs/live-harness-smoke-contract.json"
MODE="disclosure"
ACK=0
HARNESS="all"
OS_LANE="${HAZMAT_LIVE_HARNESS_OS_LANE:-macos-agent-user}"
DISTRO="${HAZMAT_LIVE_HARNESS_DISTRO:-}"
OUTPUT_DIR=""
TOKEN_ENV=""

usage() {
	cat <<'EOF'
Usage:
  bash scripts/check-live-harness-matrix.sh
  bash scripts/check-live-harness-matrix.sh --list-harnesses
  bash scripts/check-live-harness-matrix.sh --validate-contract
  bash scripts/check-live-harness-matrix.sh --emit-skip-evidence --os-lane LANE --output-dir DIR
  bash scripts/check-live-harness-matrix.sh --run --i-understand-this-runs-live-harness-matrix [--harness ID|all] [--os-lane LANE] --output-dir DIR

Default mode is disclosure-only. Live mode launches real supported harness CLIs
through Hazmat containment, performs one bounded marker prompt per harness, and
writes redacted artifacts. It is approval-gated and expects a short-lived
Muginn caller token from scripts/mint-live-harness-token.sh or --token-env.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--list-harnesses)
			MODE="list"
			;;
		--validate-contract)
			MODE="validate"
			;;
		--emit-skip-evidence)
			MODE="emit"
			;;
		--run)
			MODE="run"
			;;
		--i-understand-this-runs-live-harness-matrix)
			ACK=1
			;;
		--harness)
			shift
			HARNESS="${1:-}"
			;;
		--os-lane)
			shift
			OS_LANE="${1:-}"
			;;
		--distro)
			shift
			DISTRO="${1:-}"
			;;
		--output-dir)
			shift
			OUTPUT_DIR="${1:-}"
			;;
		--token-env)
			shift
			TOKEN_ENV="${1:-}"
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "live-harness-matrix: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$MODE" = "disclosure" ]; then
	cat <<'EOF'
live-harness-matrix: disclosure-only

This wrapper validates and, with explicit approval, runs the live real-harness
matrix for Claude, Codex, OpenCode, Antigravity, Hermes, Qwen, Cursor Agent,
and Pi. Live mode uses one short marker prompt per harness and writes redacted
metadata/transcript artifacts.

To run live, ask for exact approval for:
  bash scripts/check-live-harness-matrix.sh --run --i-understand-this-runs-live-harness-matrix --harness all --os-lane macos-agent-user --output-dir <absolute-dir>
EOF
	exit 0
fi

if [ ! -r "$CONTRACT" ]; then
	echo "live-harness-matrix: missing contract $CONTRACT" >&2
	exit 2
fi

if [ "$MODE" = "run" ]; then
	if [ "$ACK" -ne 1 ]; then
		echo "live-harness-matrix: refusing live run without --i-understand-this-runs-live-harness-matrix" >&2
		exit 2
	fi
fi

if { [ "$MODE" = "run" ] || [ "$MODE" = "emit" ]; } && [ -z "$OUTPUT_DIR" ]; then
	echo "live-harness-matrix: --output-dir is required" >&2
	exit 2
fi

case "$MODE" in
	list|validate)
		;;
	run|emit)
		case "$OUTPUT_DIR" in
			/*) mkdir -p "$OUTPUT_DIR" ;;
			*)
				echo "live-harness-matrix: --output-dir must be absolute" >&2
				exit 2
				;;
		esac
		;;
	*)
		echo "live-harness-matrix: internal mode error: $MODE" >&2
		exit 2
		;;
esac

if [ "$MODE" = "run" ] && [ -z "$TOKEN_ENV" ]; then
	NEEDS_TOKEN="$(python3 - "$CONTRACT" "$HARNESS" "$OS_LANE" <<'PY'
import json
import sys

contract_path, selected, os_lane = sys.argv[1:4]
with open(contract_path, "r", encoding="utf-8") as f:
    rows = json.load(f)["harnesses"]
if selected != "all":
    rows = [row for row in rows if row["id"] == selected]
needs = False
for row in rows:
    for support in row["os_support"]:
        if support["lane"] == os_lane and support["status"] == "supported":
            needs = True
print("1" if needs else "0")
PY
)"
	if [ "$NEEDS_TOKEN" = "1" ]; then
		TOKEN_ENV="$(mktemp "${TMPDIR:-/tmp}/hazmat-live-harness-token.XXXXXX.env")"
		bash "$SCRIPT_DIR/mint-live-harness-token.sh" \
			--issue-token \
			--i-understand-this-mints-live-harness-token \
			--output-env "$TOKEN_ENV"
	fi
fi

if [ -n "$TOKEN_ENV" ]; then
	case "$TOKEN_ENV" in
		/*) ;;
		*) echo "live-harness-matrix: --token-env must be absolute" >&2; exit 2 ;;
	esac
	if [ ! -r "$TOKEN_ENV" ]; then
		echo "live-harness-matrix: token env file is not readable: $TOKEN_ENV" >&2
		exit 2
	fi
	set -a
	# shellcheck disable=SC1090
	. "$TOKEN_ENV"
	set +a
fi

python3 - "$MODE" "$CONTRACT" "$HARNESS" "$OS_LANE" "$DISTRO" "${OUTPUT_DIR:-}" "$REPO_ROOT" <<'PY'
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

mode, contract_path, selected, os_lane, distro, output_dir, repo_root = sys.argv[1:8]

with open(contract_path, "r", encoding="utf-8") as f:
    contract = json.load(f)

def die(msg):
    sys.stderr.write(f"live-harness-matrix: {msg}\n")
    sys.exit(2)

required_top = ["schema_version", "expected_marker", "max_token_ttl_seconds", "artifact_fields", "harnesses"]
for key in required_top:
    if key not in contract:
        die(f"contract missing {key}")
if contract["schema_version"] != 1:
    die("unsupported schema_version")

rows = contract["harnesses"]
if selected != "all":
    rows = [row for row in rows if row["id"] == selected]
    if not rows:
        die(f"unknown harness {selected!r}")

if mode == "list":
    for row in contract["harnesses"]:
        print(row["id"])
    sys.exit(0)

def validate_row(row):
    for key in ["id", "display_name", "launch_command", "bootstrap_command", "inference_shape", "live_argv", "timeout_seconds", "expected_marker", "auth_token_mapping", "state_roots", "skip_conditions", "os_support"]:
        if key not in row or row[key] in ("", [], {}):
            die(f"{row.get('id', '<unknown>')} missing {key}")
    if row["expected_marker"] != contract["expected_marker"]:
        die(f"{row['id']} marker drift")
    if row["live_argv"][0] != "hazmat":
        die(f"{row['id']} live_argv must start with hazmat")
    if not any("{project}" == arg for arg in row["live_argv"]):
        die(f"{row['id']} live_argv missing project placeholder")
    if "{marker}" not in " ".join(row["live_argv"]):
        die(f"{row['id']} live_argv missing marker placeholder")
    mapping = row["auth_token_mapping"]
    if mapping.get("caller_token_env") != "MUGINN_TOKEN":
        die(f"{row['id']} caller_token_env must be MUGINN_TOKEN")
    lanes = {entry["lane"]: entry for entry in row["os_support"]}
    for lane in ["macos-agent-user", "docker-sandbox", "macos-current-user", "linux-current-user", "linux-agent-user"]:
        if lane not in lanes:
            die(f"{row['id']} missing os_support lane {lane}")

for row in contract["harnesses"]:
    validate_row(row)

if mode == "validate":
    print(f"live-harness-matrix: contract ok ({len(contract['harnesses'])} harnesses)")
    sys.exit(0)

def utcnow():
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")

def support_for(row):
    for entry in row["os_support"]:
        if entry["lane"] == os_lane:
            return entry
    return {"lane": os_lane, "status": "typed_skip", "skip_reason": f"unknown OS lane {os_lane}"}

def redact(text, token):
    if token:
        text = text.replace(token, "[redacted-muginn-token]")
    text = re.sub(r"(?i)Bearer\s+[A-Za-z0-9._~+/=-]{8,}", "Bearer [redacted]", text)
    text = re.sub(r"sk-[A-Za-z0-9][A-Za-z0-9_-]{8,}", "[redacted-token]", text)
    text = re.sub(r"AIza[A-Za-z0-9_-]{10,}", "[redacted-token]", text)
    text = re.sub(r"caller-[A-Za-z0-9_-]+", "[redacted-caller-token]", text)
    return text

def write_artifact(row, status, support, command, transcript="", failure_reason="", started_at=None, finished_at=None):
    out = Path(output_dir) / row["id"]
    out.mkdir(parents=True, exist_ok=True)
    token = os.environ.get("MUGINN_TOKEN", "")
    if transcript:
        (out / "transcript.txt").write_text(redact(transcript, token), encoding="utf-8")
    metadata = {
        "schema_version": contract["schema_version"],
        "harness": row["id"],
        "display_name": row["display_name"],
        "os_lane": os_lane,
        "distro": distro,
        "hazmat_commit": subprocess.run(["git", "-C", repo_root, "rev-parse", "HEAD"], text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL).stdout.strip(),
        "status": status,
        "started_at": started_at or utcnow(),
        "finished_at": finished_at or utcnow(),
        "command": command,
        "expected_marker": row["expected_marker"],
        "timeout_seconds": row["timeout_seconds"],
        "token": {
            "source": os.environ.get("HAZMAT_LIVE_HARNESS_TOKEN_SOURCE", ""),
            "ttl_seconds": os.environ.get("HAZMAT_LIVE_HARNESS_TOKEN_TTL_SECONDS", ""),
            "expires_at": os.environ.get("HAZMAT_LIVE_HARNESS_TOKEN_EXPIRES_AT", ""),
            "caller_id": os.environ.get("HAZMAT_LIVE_HARNESS_CALLER_ID", ""),
            "budget_class": os.environ.get("HAZMAT_LIVE_HARNESS_BUDGET_CLASS", ""),
            "value": "[redacted]"
        },
        "transcript": "transcript.txt" if transcript else "",
        "skip_reason": support.get("skip_reason", "") if status == "skip" else "",
        "failure_reason": failure_reason,
    }
    (out / "metadata.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")

if mode == "emit":
    for row in rows:
        support = support_for(row)
        if support["status"] == "supported":
            status = "pending_live"
            reason = {"reason": "supported lane requires live matrix run"}
        else:
            status = "skip"
            reason = support
        write_artifact(row, status, reason, [])
    print(f"live-harness-matrix: wrote {len(rows)} evidence row(s) to {output_dir}")
    sys.exit(0)

if mode != "run":
    die(f"unsupported mode {mode}")

failures = 0
for row in rows:
    support = support_for(row)
    if support["status"] != "supported":
        write_artifact(row, "skip", support, [])
        continue
    if not os.environ.get("MUGINN_TOKEN", ""):
        die("MUGINN_TOKEN is required for supported live run")
    marker = row["expected_marker"]
    with tempfile.TemporaryDirectory(prefix=f"hazmat-live-{row['id']}-") as project:
        Path(project, "README.md").write_text("# Hazmat live harness smoke\n", encoding="utf-8")
        argv = [arg.replace("{project}", project).replace("{marker}", marker) for arg in row["live_argv"]]
        env = os.environ.copy()
        env["HAZMAT_LIVE_HARNESS_EXPECTED_MARKER"] = marker
        started = utcnow()
        try:
            proc = subprocess.run(argv, cwd=repo_root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=int(row["timeout_seconds"]))
            transcript = proc.stdout
            finished = utcnow()
            if proc.returncode != 0:
                failures += 1
                write_artifact(row, "fail", support, argv, transcript, f"exit status {proc.returncode}", started, finished)
                continue
            if marker not in transcript:
                failures += 1
                write_artifact(row, "fail", support, argv, transcript, "expected marker missing", started, finished)
                continue
            write_artifact(row, "pass", support, argv, transcript, "", started, finished)
        except subprocess.TimeoutExpired as exc:
            failures += 1
            transcript = exc.stdout or ""
            write_artifact(row, "fail", support, argv, transcript, "timeout", started, utcnow())

if failures:
    sys.exit(1)
print(f"live-harness-matrix: completed {len(rows)} harness row(s)")
PY
