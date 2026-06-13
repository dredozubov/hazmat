#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_CACHE_INTEGRATION_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
OLLAMA_BIN="${HAZMAT_OLLAMA_SMOKE_BIN:-ollama}"
MODE="disclose"
TARGET="all"
ACK=0
MISSING_FIXTURES=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-cache-integration-smoke.sh [options]

Guarded live smoke wrapper for cache-only integrations:
  huggingface, ollama, pytorch-torch-hub

By default, this script prints the exact live commands and exits without running
Hazmat. Live mode requires:
  --run --i-understand-this-runs-hazmat-exec

Options:
  --target <name>                 One of: all, huggingface, ollama, torch-hub
  --check-fixtures                Check host-side fixture prerequisites only.
  --skip-if-missing-fixtures      Exit 0 when fixture prerequisites are absent.
  --run                           Run the selected live smoke(s).
  --i-understand-this-runs-hazmat-exec
                                  Required acknowledgement for --run.
  -h, --help                      Show this help.

Fixture environment:
  HAZMAT_HF_SMOKE_MODEL           Required for --target huggingface.
                                  Example: sentence-transformers/all-MiniLM-L6-v2
  HAZMAT_TORCH_HUB_REPO           Required for --target torch-hub.
                                  Example: pytorch/vision
  HAZMAT_TORCH_HUB_MODEL          Required for --target torch-hub.
                                  Example: resnet18
  HAZMAT_OLLAMA_SMOKE_BIN         Ollama executable name or path.

Fixture checks inspect local tool/cache setup. The live run is sudo-adjacent
because it invokes hazmat exec. Agents must ask for explicit approval before
running --check-fixtures, --skip-if-missing-fixtures, or --run.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--target)
			if [ "$#" -lt 2 ]; then
				echo "cache-integration-smoke: --target requires a value" >&2
				exit 2
			fi
			TARGET="$2"
			shift
			;;
		--target=*)
			TARGET="${1#--target=}"
			;;
		--check-fixtures)
			MODE="check"
			;;
		--skip-if-missing-fixtures)
			MODE="skip"
			;;
		--run)
			MODE="run"
			;;
		--i-understand-this-runs-hazmat-exec)
			ACK=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "cache-integration-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

case "$TARGET" in
	all|huggingface|ollama|torch-hub)
		;;
	*)
		echo "cache-integration-smoke: unsupported target: $TARGET" >&2
		exit 2
		;;
esac

selected_targets() {
	case "$TARGET" in
		all)
			printf '%s\n' huggingface ollama torch-hub
			;;
		*)
			printf '%s\n' "$TARGET"
			;;
	esac
}

add_missing_fixture() {
	if [ -z "$MISSING_FIXTURES" ]; then
		MISSING_FIXTURES="- $*"
	else
		MISSING_FIXTURES="$MISSING_FIXTURES
- $*"
	fi
}

require_command() {
	case "$1" in
		*/*)
			if [ ! -x "$1" ]; then
				add_missing_fixture "$1 is missing or not executable"
			fi
			;;
		*)
			if ! command -v "$1" >/dev/null 2>&1; then
				add_missing_fixture "$1 is not on PATH"
			fi
			;;
	esac
}

require_target_command() {
	target="$1"
	command="$2"
	case "$command" in
		*/*)
			if [ ! -x "$command" ]; then
				add_missing_fixture "[$target] $command is missing or not executable"
			fi
			;;
		*)
			if ! command -v "$command" >/dev/null 2>&1; then
				add_missing_fixture "[$target] $command is not on PATH"
			fi
			;;
	esac
}

command_available() {
	case "$1" in
		*/*)
			[ -x "$1" ]
			;;
		*)
			command -v "$1" >/dev/null 2>&1
			;;
	esac
}

add_missing_target_fixture() {
	target="$1"
	shift
	add_missing_fixture "[$target] $*"
}

python_import_available() {
	python3 - "$1" <<'PY' >/dev/null 2>&1
import importlib.util
import sys
raise SystemExit(0 if importlib.util.find_spec(sys.argv[1]) is not None else 1)
PY
}

huggingface_hub_cache_root() {
	if [ -n "${HF_HUB_CACHE:-}" ]; then
		printf '%s\n' "$HF_HUB_CACHE"
	elif [ -n "${HF_HOME:-}" ]; then
		printf '%s/hub\n' "$HF_HOME"
	elif [ -n "${XDG_CACHE_HOME:-}" ]; then
		printf '%s/huggingface/hub\n' "$XDG_CACHE_HOME"
	else
		printf '%s/.cache/huggingface/hub\n' "$HOME"
	fi
}

huggingface_model_cache_dir() {
	printf 'models--%s\n' "$(printf '%s\n' "$1" | sed 's:/:--:g')"
}

huggingface_model_cached() {
	model="$1"
	if [ -e "$model" ]; then
		[ -d "$model" ] && [ -r "$model/config.json" ]
		return
	fi

	root="$(huggingface_hub_cache_root)"
	cache_dir="$root/$(huggingface_model_cache_dir "$model")"
	if [ ! -d "$cache_dir/snapshots" ]; then
		return 1
	fi
	for snapshot in "$cache_dir"/snapshots/*; do
		if [ -r "$snapshot/config.json" ]; then
			return 0
		fi
	done
	return 1
}

torch_hub_cache_root() {
	if [ -n "${TORCH_HOME:-}" ]; then
		printf '%s/hub\n' "$TORCH_HOME"
	elif [ -n "${XDG_CACHE_HOME:-}" ]; then
		printf '%s/torch/hub\n' "$XDG_CACHE_HOME"
	else
		printf '%s/.cache/torch/hub\n' "$HOME"
	fi
}

torch_hub_repo_slug() {
	printf '%s\n' "$1" | sed 's/[\/:]/_/g'
}

torch_hub_repo_base_slug() {
	printf '%s\n' "$1" | sed 's/:.*$//' | sed 's/[\/:]/_/g'
}

torch_hub_repo_cached() {
	[ -n "$(torch_hub_cached_repo_dir "$1")" ]
}

torch_hub_cached_repo_dir() {
	root="$(torch_hub_cache_root)"
	repo_slug="$(torch_hub_repo_slug "$1")"
	base_slug="$(torch_hub_repo_base_slug "$1")"

	if [ ! -d "$root" ]; then
		return
	fi

	for path in "$root/$repo_slug" "$root/$repo_slug"_* "$root/$base_slug" "$root/$base_slug"_*; do
		if [ -d "$path" ]; then
			printf '%s\n' "$path"
			return
		fi
	done
}

torch_hub_model_callable_cached() {
	repo_dir="$(torch_hub_cached_repo_dir "$1")"
	model="$2"
	if [ -z "$repo_dir" ] || [ ! -r "$repo_dir/hubconf.py" ]; then
		return 1
	fi
	case "$model" in
		""|*[!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_]*)
			return 1
			;;
	esac
	grep -Eq "^[[:space:]]*def[[:space:]]+$model[[:space:]]*\\(" "$repo_dir/hubconf.py"
}

check_target_fixtures() {
	case "$1" in
		huggingface)
			require_target_command "$1" python3
			if command -v python3 >/dev/null 2>&1 && ! python_import_available transformers; then
				add_missing_target_fixture "$1" "python3 cannot import transformers"
			fi
			if [ -z "${HAZMAT_HF_SMOKE_MODEL:-}" ]; then
				add_missing_target_fixture "$1" "set HAZMAT_HF_SMOKE_MODEL to a pre-cached Hugging Face model ID or path"
			elif ! huggingface_model_cached "$HAZMAT_HF_SMOKE_MODEL"; then
				add_missing_target_fixture "$1" "no usable local Hugging Face model config or cached snapshot config for $HAZMAT_HF_SMOKE_MODEL under $(huggingface_hub_cache_root); pre-cache before live smoke"
			fi
			;;
		ollama)
			require_target_command "$1" "$OLLAMA_BIN"
			if command_available "$OLLAMA_BIN" && ! "$OLLAMA_BIN" list >/dev/null 2>&1; then
				add_missing_target_fixture "$1" "$OLLAMA_BIN list failed; start the Ollama daemon or check OLLAMA_HOST"
			fi
			;;
		torch-hub)
			require_target_command "$1" python3
			if command -v python3 >/dev/null 2>&1 && ! python_import_available torch; then
				add_missing_target_fixture "$1" "python3 cannot import torch"
			fi
			if [ -z "${HAZMAT_TORCH_HUB_REPO:-}" ]; then
				add_missing_target_fixture "$1" "set HAZMAT_TORCH_HUB_REPO to a pre-cached torch.hub repo"
			elif ! torch_hub_repo_cached "$HAZMAT_TORCH_HUB_REPO"; then
				add_missing_target_fixture "$1" "no cached torch.hub repo matching $HAZMAT_TORCH_HUB_REPO under $(torch_hub_cache_root); pre-cache before live smoke"
			fi
			if [ -z "${HAZMAT_TORCH_HUB_MODEL:-}" ]; then
				add_missing_target_fixture "$1" "set HAZMAT_TORCH_HUB_MODEL to a pre-cached torch.hub callable"
			elif [ -n "${HAZMAT_TORCH_HUB_REPO:-}" ] && torch_hub_repo_cached "$HAZMAT_TORCH_HUB_REPO" &&
				! torch_hub_model_callable_cached "$HAZMAT_TORCH_HUB_REPO" "$HAZMAT_TORCH_HUB_MODEL"; then
				add_missing_target_fixture "$1" "cached torch.hub repo $HAZMAT_TORCH_HUB_REPO does not expose callable $HAZMAT_TORCH_HUB_MODEL in hubconf.py"
			fi
			;;
	esac
}

check_fixtures() {
	MISSING_FIXTURES=""

	if [ ! -x "$HAZMAT" ]; then
		add_missing_fixture "$HAZMAT is missing or not executable; run make first"
	fi
	require_command mktemp

	for target in $(selected_targets); do
		check_target_fixtures "$target"
	done

	if [ -n "$MISSING_FIXTURES" ]; then
		return 1
	fi
	return 0
}

print_missing_fixtures() {
	echo "cache-integration-smoke: missing fixtures:" >&2
	printf '%s\n' "$MISSING_FIXTURES" >&2
}

print_disclosure() {
	cat <<EOF
cache-integration-smoke: dry run only

This script validates cache-only integration manifests with live hazmat exec
sessions. Live mode is sudo-adjacent and requires explicit approval:

  scripts/check-cache-integration-smoke.sh --target $TARGET --run --i-understand-this-runs-hazmat-exec

Selected target(s):
$(selected_targets | sed 's/^/  - /')

Live smoke shape:
  huggingface: HF_HUB_OFFLINE=1 hazmat exec --network none --integration huggingface -- python3 -c 'AutoModel.from_pretrained(..., local_files_only=True)'
  ollama:      hazmat exec --integration ollama -- $OLLAMA_BIN list
               Requires an already-running host Ollama daemon. This target
               intentionally does not force --network none because ollama list
               talks to the local daemon endpoint.
  torch-hub:   hazmat exec --network none --integration pytorch-torch-hub -- python3 -c 'torch.hub.load(...)'

Fixture checks:
  scripts/check-cache-integration-smoke.sh --target $TARGET --check-fixtures
EOF
}

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

prepare_project() {
	if [ -z "$SCRATCH" ]; then
		SCRATCH="$(mktemp -d /tmp/hazmat-cache-integration-smoke.XXXXXX)"
	fi
	PROJECT="$SCRATCH/$1-project"
	mkdir -p "$PROJECT"
	chmod 755 "$SCRATCH" "$PROJECT"
	printf '%s\n' "$PROJECT"
}

run_huggingface() {
	project="$(prepare_project huggingface)"
	HF_HUB_OFFLINE="${HF_HUB_OFFLINE:-1}" "$HAZMAT" exec \
		--docker=none \
		--network none \
		--no-backup \
		--integration huggingface \
		-C "$project" \
		-- python3 -c 'import sys; from transformers import AutoModel; AutoModel.from_pretrained(sys.argv[1], local_files_only=True); print("huggingface cache smoke ok")' \
		"$HAZMAT_HF_SMOKE_MODEL"
}

run_ollama() {
	project="$(prepare_project ollama)"
	"$HAZMAT" exec \
		--docker=none \
		--no-backup \
		--integration ollama \
		-C "$project" \
		-- "$OLLAMA_BIN" list
}

run_torch_hub() {
	project="$(prepare_project torch-hub)"
	"$HAZMAT" exec \
		--docker=none \
		--network none \
		--no-backup \
		--integration pytorch-torch-hub \
		-C "$project" \
		-- python3 -c 'import sys, torch; torch.hub.load(sys.argv[1], sys.argv[2], trust_repo=True, skip_validation=True); print("torch hub cache smoke ok")' \
		"$HAZMAT_TORCH_HUB_REPO" \
		"$HAZMAT_TORCH_HUB_MODEL"
}

run_target() {
	case "$1" in
		huggingface)
			run_huggingface
			;;
		ollama)
			run_ollama
			;;
		torch-hub)
			run_torch_hub
			;;
	esac
}

case "$MODE" in
	disclose)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_fixtures; then
			echo "cache-integration-smoke: fixtures ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ]; then
			echo "cache-integration-smoke: skipped because fixtures are missing" >&2
			print_missing_fixtures
			exit 0
		fi
		print_missing_fixtures
		exit 2
		;;
	run)
		if [ "$ACK" != "1" ]; then
			echo "cache-integration-smoke: refusing live run without --i-understand-this-runs-hazmat-exec" >&2
			exit 2
		fi
		if ! check_fixtures; then
			print_missing_fixtures
			exit 2
		fi
		for target in $(selected_targets); do
			run_target "$target"
		done
		;;
esac
