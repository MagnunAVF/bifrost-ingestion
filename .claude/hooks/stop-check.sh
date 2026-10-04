#!/usr/bin/env bash
cd "$CLAUDE_PROJECT_DIR" || exit 0
input=$(cat)
[ "$(echo "$input" | jq -r '.stop_hook_active')" = "true" ] && exit 0

if ! git diff --quiet -- testdata/catalog.db testdata/ProductEntry.json; then
  echo "A read-only fixture in testdata/ was modified. Restore it with:" >&2
  echo "git checkout -- testdata/catalog.db testdata/ProductEntry.json" >&2
  exit 2
fi

git diff --quiet && git diff --cached --quiet && exit 0

if ! out=$(make -s check 2>&1); then
  echo "make check is failing. Fix it before finishing:" >&2
  echo "$out" | tail -40 >&2
  exit 2
fi
