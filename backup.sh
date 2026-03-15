#!/usr/bin/env bash
#
# Backup ~/workspace with rsync
#
# Usage:
#   ./backup.sh /Volumes/BACKUP/workspace
#   ./backup.sh user@host:/backup/workspace
#
# Excludes build artifacts and large re-cloneable repos.

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <destination>"
  echo "  e.g. $0 /Volumes/BACKUP/workspace"
  echo "  e.g. $0 user@nas:/backup/workspace"
  exit 1
fi

DEST="$1"
SRC="$HOME/workspace/"

if [[ ! -d "$SRC" ]]; then
  echo "Error: source directory '$SRC' not found." >&2
  exit 1
fi

echo "Source:      $SRC"
echo "Destination: $DEST"
echo "Note: --delete is active — files in DEST not present in SRC will be removed."
echo ""

rsync -aHAX --progress --delete \
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
  --exclude='/gurufocus-data/' \
  --exclude='/flux2.c/' \
  --exclude='/iTerm2/' \
  --exclude='/nixpkgs/' \
  --exclude='/postiz-app/' \
  --exclude='/dsss17/' \
  --exclude='/dsss17-nix/' \
  --exclude='/SillyTavern/' \
  --exclude='/emacs/' \
  --exclude='/bitcoin/' \
  --exclude='/MatchingCompressor/' \
  --exclude='/zcash/' \
  --exclude='/transmission/' \
  --exclude='/moltbot/' \
  --exclude='/agent-browser/' \
  --exclude='/darkfi/' \
  --exclude='/24slash6/' \
  --exclude='/urweb/' \
  --exclude='/bitcoinbook/' \
  --exclude='/nix-config/' \
  --exclude='/dotfiles/' \
  "$SRC" "$DEST"
