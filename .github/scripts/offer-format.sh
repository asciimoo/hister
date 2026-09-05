#!/usr/bin/env bash
set -euo pipefail

MARKER='<!-- hister-format-prompt -->'
BODY="$MARKER
Formatting check failed. Comment \`@github-actions format\` to auto-fix and push to this PR.

- Only the repo owner or the PR author can trigger this.
- For PRs from forks, **Allow edits from maintainers** must be enabled."

EXISTING=$(gh api "repos/$REPO/issues/$PR/comments" --paginate \
  --jq ".[] | select(.body | startswith(\"$MARKER\")) | .id" | head -n1)

if [ -n "$EXISTING" ]; then
  gh api -X PATCH "repos/$REPO/issues/comments/$EXISTING" -f body="$BODY" >/dev/null
else
  gh api -X POST "repos/$REPO/issues/$PR/comments" -f body="$BODY" >/dev/null
fi
