#!/usr/bin/env bash
# Resolve between an ENG-<n> id and its GitHub issue number.
#
# ENG-<n> is the id in the issue *title* ("ENG-3: [1.2] ..."). It is the id used in branches,
# commits, PR titles and docs. It is NOT always the GitHub issue number (ENG-3 is issue 2), so
# anything that calls `gh issue ...` must resolve it first. GitHub titles are the only source of
# truth; there is no local copy to keep in sync.
#
#   scripts/eng-issue.sh ENG-3   # prints 2
#   scripts/eng-issue.sh 2       # prints ENG-3
#
# Exit codes: 0 resolved, 1 not found / ambiguous / gh failed, 2 usage error.
# Needs gh (authenticated) and jq. Set GH_REPO to target a repo other than the current one.
set -euo pipefail

die() { echo "eng-issue: $1" >&2; exit "${2:-1}"; }
usage() { die "usage: $(basename "$0") ENG-<n> | <issue-number>" 2; }

[ $# -eq 1 ] || usage
arg=$1

if [[ "$arg" =~ ^ENG-([0-9]+)$ ]]; then
  mode=id
elif [[ "$arg" =~ ^[0-9]+$ ]]; then
  mode=number
else
  usage
fi

issues=$(gh issue list --state all --limit 1000 --json number,title) || die "gh issue list failed"

if [ "$mode" = id ]; then
  numbers=$(jq -r --arg id "$arg" \
    '[.[] | select(.title | startswith($id + ":")) | .number] | sort | map(tostring) | join(" ")' \
    <<<"$issues")
  case "$numbers" in
    "") die "no issue titled $arg: ..." ;;
    *" "*) die "ambiguous: $arg is in the titles of issues $numbers" ;;
    *) echo "$numbers" ;;
  esac
else
  id=$(jq -r --argjson n "$arg" \
    'first(.[] | select(.number == $n) | .title | capture("^(?<id>ENG-[0-9]+):").id) // empty' \
    <<<"$issues")
  [ -n "$id" ] || die "issue $arg not found or has no ENG-<n> title"
  echo "$id"
fi
