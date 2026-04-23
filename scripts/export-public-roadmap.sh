#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
config_path="$root_dir/docs/public-roadmap.config.json"
output_path="$root_dir/docs/public-roadmap.md"

if ! command -v bd >/dev/null 2>&1; then
  echo "bd is required" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

tmp_output="$(mktemp)"
trap 'rm -f "$tmp_output"' EXIT

issue_field() {
  local issue_json="$1"
  local jq_expr="$2"
  jq -r "(if type==\"array\" then .[0] else . end) | $jq_expr" <<<"$issue_json"
}

{
  echo "# Public Roadmap"
  echo
  echo "_Generated from bd issues via \`scripts/export-public-roadmap.sh\`._"
  echo
  echo "This is a curated subset of Hazmat work. It is meant to make real contribution opportunities visible without dumping the entire local issue database into the repo."
  echo

  while IFS= read -r section; do
    title="$(jq -r '.title' <<<"$section")"
    intro="$(jq -r '.intro' <<<"$section")"

    echo "## $title"
    echo
    echo "$intro"
    echo

    while IFS= read -r issue_id; do
      issue_json="$(bd show "$issue_id" --json)"
      issue_title="$(issue_field "$issue_json" '.title')"
      issue_status="$(issue_field "$issue_json" '.status')"
      issue_priority="$(issue_field "$issue_json" '.priority')"
      issue_type="$(issue_field "$issue_json" '.issue_type')"
      issue_assignee="$(issue_field "$issue_json" '.assignee // empty')"
      issue_desc="$(issue_field "$issue_json" '.description')"

      echo "### $issue_title (\`$issue_id\`)"
      echo
      echo "- Status: \`$issue_status\`"
      echo "- Priority: \`P$issue_priority\`"
      echo "- Type: \`$issue_type\`"
      if [[ -n "$issue_assignee" ]]; then
        echo "- Assignee: \`$issue_assignee\`"
      fi
      echo "- Summary: $issue_desc"
      echo
    done < <(jq -r '.issues[]' <<<"$section")
  done < <(jq -c '.sections[]' "$config_path")
} >"$tmp_output"

mv -f "$tmp_output" "$output_path"
echo "Wrote $output_path"
