#!/usr/bin/env bash
#
# Backup the shared workspace (/Users/Shared/workspace) with rsync.
#
# Usage:
#   ./backup.sh [--show-scope]
#   ./backup.sh /Volumes/BACKUP/workspace
#   ./backup.sh user@host:/backup/workspace
#
#   # Destructive mirror (removes destination-only files):
#   ./backup.sh --sync /Volumes/BACKUP/workspace
#
# By default, backup is ADDITIVE: no files are deleted from the destination.
# Use --sync for a full mirror.  Local --sync destinations must contain a
# .backup-target sentinel file to prove they are initialized backup targets:
#   touch /Volumes/BACKUP/workspace/.backup-target
#
# Exclude rules come from two sources (printed before each run):
#   1. Built-in excludes: universal build artifacts (node_modules/, .venv/, etc.)
#   2. User excludes:     $SHARED_WORKSPACE/.backup-excludes
#      Edit this file to add or remove repos from backup scope.
#
# Use --show-scope to inspect effective excludes without running rsync.

set -euo pipefail

# Canonical shared workspace — must match the sharedWorkspace constant in
# sandbox/main.go.
SHARED_WORKSPACE="/Users/Shared/workspace"
BACKUP_TARGET_MARKER=".backup-target"
BACKUP_EXCLUDES_FILE="${SHARED_WORKSPACE}/.backup-excludes"

SYNC_MODE=false
SHOW_SCOPE=false

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --sync)
      SYNC_MODE=true
      shift
      ;;
    --show-scope)
      SHOW_SCOPE=true
      shift
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
    *)
      break
      ;;
  esac
done

# --show-scope: print effective excludes and exit without running rsync.
if [[ "$SHOW_SCOPE" == true ]]; then
  echo "Backup source: ${SHARED_WORKSPACE}/"
  echo ""
  echo "Built-in excludes (always applied — universal build artifacts):"
  for pat in 'node_modules/' '.venv/' 'venv/' '__pycache__/' '.next/' 'dist/' \
             'build/' 'target/' '.nix-*' '.DS_Store' '*.pyc'; do
    echo "  --exclude=${pat}"
  done
  echo ""
  echo "User excludes file: ${BACKUP_EXCLUDES_FILE}"
  if [[ -f "$BACKUP_EXCLUDES_FILE" ]]; then
    count=0
    while IFS= read -r line; do
      [[ "$line" =~ ^[[:space:]]*# ]] && continue
      [[ -z "${line// }" ]] && continue
      echo "  --exclude=${line}"
      ((count++)) || true
    done < "$BACKUP_EXCLUDES_FILE"
    if [[ $count -eq 0 ]]; then
      echo "  (file exists but contains no active exclude patterns)"
    fi
  else
    echo "  (file not found — no user-specific excludes)"
    echo "  To create it, run: sandbox setup"
    echo "  Or manually: touch '${BACKUP_EXCLUDES_FILE}'"
  fi
  exit 0
fi

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 [--sync] <destination>"
  echo "       $0 --show-scope"
  echo "  e.g. $0 /Volumes/BACKUP/workspace"
  echo "  e.g. $0 user@nas:/backup/workspace"
  echo "  e.g. $0 --sync /Volumes/BACKUP/workspace   # mirror mode (deletes dest-only files)"
  exit 1
fi

DEST="$1"
SRC="${SHARED_WORKSPACE}/"

if [[ ! -d "$SRC" ]]; then
  echo "Error: source directory '$SRC' not found." >&2
  exit 1
fi

# Validate --sync destination (local paths only; remote paths skip local checks)
if [[ "$SYNC_MODE" == true ]] && [[ "$DEST" != *:* ]]; then
  if [[ ! -d "$DEST" ]]; then
    echo "Error: --sync destination '$DEST' does not exist or is not mounted." >&2
    echo "Mount the volume or create the directory, then initialize it:" >&2
    echo "  mkdir -p '$DEST' && touch '$DEST/$BACKUP_TARGET_MARKER'" >&2
    exit 1
  fi
  MARKER="$DEST/$BACKUP_TARGET_MARKER"
  if [[ ! -f "$MARKER" ]]; then
    echo "Error: --sync destination '$DEST' is not an initialized backup target." >&2
    echo "Missing sentinel file: $MARKER" >&2
    echo "To initialize this destination:" >&2
    echo "  touch '$MARKER'" >&2
    exit 1
  fi
fi

echo "Source:      $SRC"
echo "Destination: $DEST"
if [[ "$SYNC_MODE" == true ]]; then
  echo "Mode:        SYNC — destination-only files will be deleted"
else
  echo "Mode:        safe (additive, no deletions)"
fi
if [[ -f "$BACKUP_EXCLUDES_FILE" ]]; then
  echo "Scope file:  ${BACKUP_EXCLUDES_FILE}"
else
  echo "Scope file:  ${BACKUP_EXCLUDES_FILE} (not found — only built-in excludes applied)"
fi
echo ""

RSYNC_EXTRA_FLAGS=()
if [[ "$SYNC_MODE" == true ]]; then
  RSYNC_EXTRA_FLAGS+=("--delete")
fi
if [[ -f "$BACKUP_EXCLUDES_FILE" ]]; then
  RSYNC_EXTRA_FLAGS+=("--exclude-from=${BACKUP_EXCLUDES_FILE}")
fi

rsync -aHAX --progress "${RSYNC_EXTRA_FLAGS[@]}" \
  --exclude='node_modules/' \
  --exclude='.venv/' \
  --exclude='venv/' \
  --exclude='__pycache__/' \
  --exclude='.next/' \
  --exclude='dist/' \
  --exclude='build/' \
  --exclude='target/' \
  --exclude='.nix-*' \
  --exclude='.DS_Store' \
  --exclude='*.pyc' \
  "$SRC" "$DEST"
