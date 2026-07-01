#!/usr/bin/env bash

set -euo pipefail

ISSUE=0
ACK=0
OUTPUT_ENV=""

usage() {
	cat <<'EOF'
Usage:
  bash scripts/mint-live-harness-token.sh
  bash scripts/mint-live-harness-token.sh --issue-token --i-understand-this-mints-live-harness-token --output-env PATH

Default mode is disclosure-only. Issue mode writes a 0600 env file containing a
short-lived Muginn caller token for live harness smokes. It never prints the
token value.

Token sources:
  MUGINN_LIVE_HARNESS_TOKEN_CMD
      Command that prints JSON with token, ttl_seconds, caller_id, and optional
      expires_at/model/budget_class. This is the preferred CI path.

  MUGINN_LIVE_HARNESS_CALLER_TOKEN
      Manual fallback token. Set MUGINN_LIVE_HARNESS_TOKEN_TTL_SECONDS to the
      known remaining TTL. CI refuses this fallback unless
      MUGINN_LIVE_HARNESS_ALLOW_STATIC_CI_TOKEN=1 is set.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--issue-token)
			ISSUE=1
			;;
		--i-understand-this-mints-live-harness-token)
			ACK=1
			;;
		--output-env)
			shift
			OUTPUT_ENV="${1:-}"
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "live-harness-token: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$ISSUE" -ne 1 ]; then
	cat <<'EOF'
live-harness-token: disclosure-only

This broker writes a temporary env file for live supported-harness smokes. It
does not run by default, does not print token material, and rejects missing or
overlong TTL metadata before writing anything.

Live mode requires:
  bash scripts/mint-live-harness-token.sh --issue-token --i-understand-this-mints-live-harness-token --output-env <absolute-path>
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	echo "live-harness-token: refusing token issue without --i-understand-this-mints-live-harness-token" >&2
	exit 2
fi

if [ -z "$OUTPUT_ENV" ]; then
	echo "live-harness-token: --output-env is required" >&2
	exit 2
fi

case "$OUTPUT_ENV" in
	/*) ;;
	*)
		echo "live-harness-token: --output-env must be absolute" >&2
		exit 2
		;;
esac

mkdir -p "$(dirname "$OUTPUT_ENV")"

python3 - "$OUTPUT_ENV" <<'PY'
import json
import os
import shlex
import subprocess
import sys
from datetime import datetime, timezone

output_env = sys.argv[1]
max_ttl = int(os.environ.get("MUGINN_LIVE_HARNESS_MAX_TOKEN_TTL_SECONDS", "3600"))
token_cmd = os.environ.get("MUGINN_LIVE_HARNESS_TOKEN_CMD", "").strip()
payload = {}
source = ""

if token_cmd:
    proc = subprocess.run(token_cmd, shell=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode != 0:
        sys.stderr.write("live-harness-token: token command failed\n")
        if proc.stderr:
            sys.stderr.write(proc.stderr)
        sys.exit(2)
    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        sys.stderr.write(f"live-harness-token: token command did not print JSON: {exc}\n")
        sys.exit(2)
    source = "command"
else:
    if os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("MUGINN_LIVE_HARNESS_ALLOW_STATIC_CI_TOKEN") != "1":
        sys.stderr.write("live-harness-token: CI requires MUGINN_LIVE_HARNESS_TOKEN_CMD; refusing static token fallback\n")
        sys.exit(2)
    token = os.environ.get("MUGINN_LIVE_HARNESS_CALLER_TOKEN", "")
    if not token:
        sys.stderr.write("live-harness-token: set MUGINN_LIVE_HARNESS_TOKEN_CMD or MUGINN_LIVE_HARNESS_CALLER_TOKEN\n")
        sys.exit(2)
    payload = {
        "token": token,
        "ttl_seconds": os.environ.get("MUGINN_LIVE_HARNESS_TOKEN_TTL_SECONDS", ""),
        "caller_id": os.environ.get("MUGINN_LIVE_HARNESS_CALLER_ID", "hazmat-live-harness"),
        "model": os.environ.get("MUGINN_LIVE_HARNESS_MODEL", ""),
        "budget_class": os.environ.get("MUGINN_LIVE_HARNESS_BUDGET_CLASS", "live-harness-smoke"),
    }
    source = "env"

token = str(payload.get("token", "")).strip()
if not token:
    sys.stderr.write("live-harness-token: token payload is missing token\n")
    sys.exit(2)

try:
    ttl = int(payload.get("ttl_seconds", 0))
except (TypeError, ValueError):
    ttl = 0
if ttl <= 0 or ttl > max_ttl:
    sys.stderr.write(f"live-harness-token: ttl_seconds must be 1..{max_ttl}, got {payload.get('ttl_seconds')!r}\n")
    sys.exit(2)

caller_id = str(payload.get("caller_id", "hazmat-live-harness")).strip() or "hazmat-live-harness"
expires_at = str(payload.get("expires_at", "")).strip()
if not expires_at:
    expires_at = datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
model = str(payload.get("model", "")).strip()
budget_class = str(payload.get("budget_class", "live-harness-smoke")).strip() or "live-harness-smoke"

lines = [
    f"MUGINN_TOKEN={shlex.quote(token)}",
    f"MUGINN_CALLER_TOKEN={shlex.quote(token)}",
    f"HAZMAT_LIVE_HARNESS_TOKEN_SOURCE={shlex.quote(source)}",
    f"HAZMAT_LIVE_HARNESS_TOKEN_TTL_SECONDS={ttl}",
    f"HAZMAT_LIVE_HARNESS_TOKEN_EXPIRES_AT={shlex.quote(expires_at)}",
    f"HAZMAT_LIVE_HARNESS_CALLER_ID={shlex.quote(caller_id)}",
    f"HAZMAT_LIVE_HARNESS_BUDGET_CLASS={shlex.quote(budget_class)}",
]
if model:
    lines.append(f"HAZMAT_LIVE_HARNESS_MODEL={shlex.quote(model)}")

tmp = output_env + ".tmp"
old_umask = os.umask(0o177)
try:
    with open(tmp, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    os.chmod(tmp, 0o600)
    os.replace(tmp, output_env)
finally:
    os.umask(old_umask)
    try:
        if os.path.exists(tmp):
            os.unlink(tmp)
    except OSError:
        pass

print(f"live-harness-token: wrote redacted env file to {output_env} ttl_seconds={ttl} source={source}")
PY
