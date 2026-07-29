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
PROVIDER="${HAZMAT_LIVE_HARNESS_PROVIDER:-auto}"

usage() {
	cat <<'EOF'
Usage:
  bash scripts/check-live-harness-matrix.sh
  bash scripts/check-live-harness-matrix.sh --list-harnesses
  bash scripts/check-live-harness-matrix.sh --validate-contract
  bash scripts/check-live-harness-matrix.sh --emit-skip-evidence --os-lane LANE --output-dir DIR
  bash scripts/check-live-harness-matrix.sh --run --i-understand-this-runs-live-harness-matrix [--harness ID|all] [--provider PROVIDER|auto] [--os-lane LANE] --output-dir DIR

Default mode is disclosure-only. Live mode launches real supported harness CLIs
through Hazmat containment, performs one bounded marker prompt per harness, and
writes redacted artifacts. It is approval-gated and expects direct provider
API keys in HAZMAT_LIVE_PROVIDER_* CI secrets.
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
		--provider)
			shift
			PROVIDER="${1:-}"
			;;
		--output-dir)
			shift
			OUTPUT_DIR="${1:-}"
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
  bash scripts/check-live-harness-matrix.sh --run --i-understand-this-runs-live-harness-matrix --harness all --provider auto --os-lane macos-agent-user --output-dir <absolute-dir>
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

python3 - "$MODE" "$CONTRACT" "$HARNESS" "$OS_LANE" "$DISTRO" "${OUTPUT_DIR:-}" "$REPO_ROOT" "$PROVIDER" <<'PY'
import json
import os
import platform
import re
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

mode, contract_path, selected, os_lane, distro, output_dir, repo_root, provider_filter = sys.argv[1:9]

with open(contract_path, "r", encoding="utf-8") as f:
    contract = json.load(f)

def die(msg):
    sys.stderr.write(f"live-harness-matrix: {msg}\n")
    sys.exit(2)

required_top = ["schema_version", "expected_marker", "artifact_fields", "harnesses"]
for key in required_top:
    if key not in contract:
        die(f"contract missing {key}")
if contract["schema_version"] != 2:
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
    if "direct_provider_tokens" not in row:
        die(f"{row['id']} missing direct_provider_tokens")
    if row["expected_marker"] != contract["expected_marker"]:
        die(f"{row['id']} marker drift")
    if row["live_argv"][0] != "hazmat":
        die(f"{row['id']} live_argv must start with hazmat")
    if not any("{project}" == arg for arg in row["live_argv"]):
        die(f"{row['id']} live_argv missing project placeholder")
    if "{marker}" not in " ".join(row["live_argv"]):
        die(f"{row['id']} live_argv missing marker placeholder")
    mapping = row["auth_token_mapping"]
    if mapping.get("mode") not in ("direct-provider-secret", "contained-harness-auth"):
        die(f"{row['id']} auth_token_mapping mode is unsupported")
    for key in ["materializer", "ci_token_envs", "harness_delivery"]:
        if not mapping.get(key, "").strip():
            die(f"{row['id']} auth_token_mapping missing {key}")
    if mapping["mode"] == "direct-provider-secret" and not row["direct_provider_tokens"]:
        die(f"{row['id']} direct-provider-secret mode needs direct_provider_tokens")
    if mapping["mode"] == "contained-harness-auth" and not row.get("direct_provider_skip_reason", "").strip():
        die(f"{row['id']} contained-harness-auth mode needs direct_provider_skip_reason")
    for token in row["direct_provider_tokens"]:
        for key in ["provider", "ci_env", "session_env", "store_rel_path"]:
            if not token.get(key, "").strip():
                die(f"{row['id']} direct_provider_tokens entry missing {key}")
        if not token["ci_env"].startswith("HAZMAT_LIVE_PROVIDER_"):
            die(f"{row['id']} direct provider ci_env must use HAZMAT_LIVE_PROVIDER_*")
        if not token["store_rel_path"].startswith("providers/") or ".." in Path(token["store_rel_path"]).parts:
            die(f"{row['id']} direct provider store_rel_path must stay under providers/")
    lanes = {entry["lane"]: entry for entry in row["os_support"]}
    for lane in ["macos-agent-user", "docker-sandbox", "macos-current-user", "linux-current-user", "linux-agent-user"]:
        if lane not in lanes:
            die(f"{row['id']} missing os_support lane {lane}")

for row in contract["harnesses"]:
    validate_row(row)

known_lanes = {support["lane"] for row in contract["harnesses"] for support in row["os_support"]}
if mode in ("run", "emit") and os_lane not in known_lanes:
    die(f"unknown OS/provider lane {os_lane!r}")
known_providers = {"auto"}
for row in contract["harnesses"]:
    for token in row.get("direct_provider_tokens", []):
        known_providers.add(token["provider"])
        known_providers.add(token["session_env"])
if provider_filter not in known_providers:
    die(f"unknown provider {provider_filter!r}")

if mode == "validate":
    print(f"live-harness-matrix: contract ok ({len(contract['harnesses'])} harnesses)")
    sys.exit(0)

def utcnow():
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")

def support_for(row):
    for entry in row["os_support"]:
        if entry["lane"] == os_lane:
            return entry
    die(f"{row['id']} contract missing OS/provider lane {os_lane!r}")

def redact(text, values):
    for value in values:
        if value:
            text = text.replace(value, "[redacted-provider-token]")
    text = re.sub(r"(?i)Bearer\s+[A-Za-z0-9._~+/=-]{8,}", "Bearer [redacted]", text)
    text = re.sub(r"sk-[A-Za-z0-9][A-Za-z0-9_-]{8,}", "[redacted-token]", text)
    text = re.sub(r"AIza[A-Za-z0-9_-]{10,}", "[redacted-token]", text)
    text = re.sub(r"caller-[A-Za-z0-9_-]+", "[redacted-caller-token]", text)
    return text

def provider_tokens_for(row):
    tokens = row.get("direct_provider_tokens", [])
    if provider_filter == "auto":
        return tokens
    return [token for token in tokens if token["provider"] == provider_filter or token["session_env"] == provider_filter]

def all_provider_secret_values():
    values = []
    for row in contract["harnesses"]:
        for token in row.get("direct_provider_tokens", []):
            value = os.environ.get(token["ci_env"], "")
            if value:
                values.append(value)
    return values

def select_provider_token(row):
    tokens = provider_tokens_for(row)
    if not tokens:
        if row.get("direct_provider_tokens") and provider_filter != "auto":
            return None, f"provider {provider_filter!r} is not configured for {row['id']}"
        return None, row.get("direct_provider_skip_reason", "direct provider token materializer is not configured")
    for token in tokens:
        value = os.environ.get(token["ci_env"], "").strip()
        if value:
            selected_token = dict(token)
            selected_token["value"] = value
            return selected_token, ""
    envs = ", ".join(token["ci_env"] for token in tokens)
    return None, f"missing direct provider token; set one of: {envs}"

def token_metadata(token):
    if not token:
        return {
            "source": "direct-provider-secret",
            "provider": "",
            "ci_env": "",
            "session_env": "",
            "store_rel_path": "",
            "value": "[redacted]",
        }
    return {
        "source": "direct-provider-secret",
        "provider": token["provider"],
        "ci_env": token["ci_env"],
        "session_env": token["session_env"],
        "store_rel_path": token["store_rel_path"],
        "value": "[redacted]",
    }

def read_os_release():
    facts = {}
    try:
        with open("/etc/os-release", "r", encoding="utf-8") as f:
            for line in f:
                if "=" not in line:
                    continue
                key, value = line.rstrip("\n").split("=", 1)
                facts[key.lower()] = value.strip('"')
    except OSError:
        pass
    return facts

def command_stdout(args):
    try:
        proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=5)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    if proc.returncode != 0:
        return ""
    return proc.stdout.strip()

def environment_facts(row):
    os_release = read_os_release()
    env_version_key = f"HAZMAT_LIVE_HARNESS_{row['id'].upper().replace('-', '_')}_VERSION"
    harness_version = os.environ.get(env_version_key, "").strip()
    return {
        "os_name": platform.system(),
        "os_release": platform.release(),
        "os_version": platform.version(),
        "arch": platform.machine(),
        "linux_id": os_release.get("id", ""),
        "linux_version_id": os_release.get("version_id", ""),
        "linux_pretty_name": os_release.get("pretty_name", ""),
        "macos_product_version": command_stdout(["sw_vers", "-productVersion"]) if platform.system() == "Darwin" else "",
        "macos_build_version": command_stdout(["sw_vers", "-buildVersion"]) if platform.system() == "Darwin" else "",
        "runner": os.environ.get("RUNNER_NAME", "") or os.environ.get("GITHUB_RUNNER_NAME", "") or os.environ.get("HAZMAT_LINUX_VM_RUNNER", ""),
        "github_workflow": os.environ.get("GITHUB_WORKFLOW", ""),
        "github_job": os.environ.get("GITHUB_JOB", ""),
        "github_run_id": os.environ.get("GITHUB_RUN_ID", ""),
        "containment_provider": os_lane,
        "harness_version": harness_version,
        "harness_version_env": env_version_key,
        "harness_version_unavailable_reason": "" if harness_version else "not provided by runner environment",
    }

def write_artifact(row, status, support, command, transcript="", failure_reason="", started_at=None, finished_at=None, token=None):
    out = Path(output_dir) / row["id"]
    out.mkdir(parents=True, exist_ok=True)
    if transcript:
        extra_values = [token["value"]] if token and token.get("value") else []
        (out / "transcript.txt").write_text(redact(transcript, all_provider_secret_values() + extra_values), encoding="utf-8")
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
        "token": token_metadata(token),
        "environment": environment_facts(row),
        "transcript": "transcript.txt" if transcript else "",
        "skip_reason": support.get("skip_reason", "") if status == "skip" else "",
        "failure_reason": failure_reason,
    }
    (out / "metadata.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")

if mode == "emit":
    for row in rows:
        support = support_for(row)
        if support["status"] == "supported":
            if row.get("direct_provider_tokens"):
                status = "pending_live"
                reason = {"reason": "supported lane requires direct-provider live matrix run"}
            else:
                status = "skip"
                reason = {"skip_reason": row.get("direct_provider_skip_reason", "direct provider token materializer is not configured")}
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
    provider_token, provider_error = select_provider_token(row)
    if not provider_token:
        if (not row.get("direct_provider_tokens") or "not configured" in provider_error) and selected == "all":
            write_artifact(row, "skip", {"skip_reason": provider_error}, [])
            continue
        failures += 1
        sys.stderr.write(f"live-harness-matrix: {row['id']}: {provider_error}\n")
        write_artifact(row, "fail", support, [], failure_reason=provider_error)
        continue
    marker = row["expected_marker"]
    with tempfile.TemporaryDirectory(prefix=f"hazmat-live-{row['id']}-") as project:
        Path(project, "README.md").write_text("# Hazmat live harness smoke\n", encoding="utf-8")
        argv = [arg.replace("{project}", project).replace("{marker}", marker) for arg in row["live_argv"]]
        env = os.environ.copy()
        env["HAZMAT_LIVE_HARNESS_EXPECTED_MARKER"] = marker
        env[provider_token["session_env"]] = provider_token["value"]
        started = utcnow()
        try:
            proc = subprocess.run(argv, cwd=repo_root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=int(row["timeout_seconds"]))
            transcript = proc.stdout
            finished = utcnow()
            if proc.returncode != 0:
                failures += 1
                write_artifact(row, "fail", support, argv, transcript, f"exit status {proc.returncode}", started, finished, provider_token)
                continue
            if marker not in transcript:
                failures += 1
                write_artifact(row, "fail", support, argv, transcript, "expected marker missing", started, finished, provider_token)
                continue
            write_artifact(row, "pass", support, argv, transcript, "", started, finished, provider_token)
        except subprocess.TimeoutExpired as exc:
            failures += 1
            transcript = exc.stdout or ""
            write_artifact(row, "fail", support, argv, transcript, "timeout", started, utcnow(), provider_token)

if failures:
    sys.exit(1)
print(f"live-harness-matrix: completed {len(rows)} harness row(s)")
PY
