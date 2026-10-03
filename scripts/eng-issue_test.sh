#!/usr/bin/env bash
# Tests for scripts/eng-issue.sh. A fake `gh` on PATH serves canned issue lists, so this never
# talks to GitHub. Run: bash scripts/eng-issue_test.sh (also part of `make test`).
set -u

here=$(cd "$(dirname "$0")" && pwd)
script="$here/eng-issue.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
cat >"$tmp/bin/gh" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_GH_LOG"
[ -n "${FAKE_GH_FAIL:-}" ] && { echo "gh: HTTP 401: Bad credentials" >&2; exit 1; }
cat "$FAKE_GH_JSON"
EOF
chmod +x "$tmp/bin/gh"

# Mirrors the real repo on 2026-10-03: titles carry the plan-order id, numbers differ.
cat >"$tmp/issues.json" <<'EOF'
[
  {"number": 1, "title": "ENG-1: [1.0] Repository, tooling and guardrails"},
  {"number": 2, "title": "ENG-3: [1.2] Defensive JSON parsing and deep sanitization"},
  {"number": 3, "title": "ENG-4: [1.3] Local embedding engine integration (Ollama)"},
  {"number": 7, "title": "ENG-2: [1.1] Database bootstrapping and schema remediation"},
  {"number": 8, "title": "ci: add Claude Code GitHub Workflow"},
  {"number": 9, "title": "ENG-13: [2.1] Go MCP server"}
]
EOF
cat >"$tmp/dup.json" <<'EOF'
[
  {"number": 2, "title": "ENG-3: first"},
  {"number": 4, "title": "ENG-3: second"}
]
EOF

pass=0 fail=0
# check <name> <want-exit> <want-stdout> <want-stderr-substring> -- <args...>
check() {
  local name=$1 want_code=$2 want_out=$3 want_err=$4
  shift 5
  local out err code
  out=$(PATH="$tmp/bin:$PATH" bash "$script" "$@" 2>"$tmp/stderr")
  code=$?
  err=$(cat "$tmp/stderr")
  if [ "$code" = "$want_code" ] && [ "$out" = "$want_out" ] &&
    { [ -z "$want_err" ] && [ -z "$err" ] || [ -n "$want_err" ] && [[ "$err" == *"$want_err"* ]]; }; then
    pass=$((pass + 1))
  else
    fail=$((fail + 1))
    printf 'FAIL %s\n  exit:   got %s want %s\n  stdout: got %q want %q\n  stderr: got %q want *%s*\n' \
      "$name" "$code" "$want_code" "$out" "$want_out" "$err" "$want_err"
  fi
}

export FAKE_GH_JSON="$tmp/issues.json" FAKE_GH_LOG="$tmp/gh.log"

check "id to number"                 0 "2"     "" -- ENG-3
check "id equal to number"           0 "1"     "" -- ENG-1
check "id far from number"           0 "7"     "" -- ENG-2
check "ENG-1 does not match ENG-13"  0 "9"     "" -- ENG-13
check "number to id"                 0 "ENG-3" "" -- 2
check "number to id (7)"             0 "ENG-2" "" -- 7
check "unknown id"                   1 ""      "no issue titled ENG-99" -- ENG-99
check "unknown number"               1 ""      "issue 99 not found or has no ENG-<n> title" -- 99
check "number without ENG title"     1 ""      "issue 8 not found or has no ENG-<n> title" -- 8
check "no args"                      2 ""      "usage:" --
check "two args"                     2 ""      "usage:" -- ENG-1 ENG-2
check "lowercase id"                 2 ""      "usage:" -- eng-3
check "hash form"                    2 ""      "usage:" -- "#2"
check "ENG-#n form"                  2 ""      "usage:" -- "ENG-#2"
check "id without digits"            2 ""      "usage:" -- ENG-x
check "id with suffix"               2 ""      "usage:" -- ENG-3-foo

FAKE_GH_JSON="$tmp/dup.json" \
  check "ambiguous id"               1 ""      "ambiguous: ENG-3 is in the titles of issues 2 4" -- ENG-3
FAKE_GH_FAIL=1 \
  check "gh failure is reported"     1 ""      "Bad credentials" -- ENG-3

# Closed issues must resolve too (close-issue runs after merge; old ids stay valid).
if grep -q -- "--state all" "$FAKE_GH_LOG"; then pass=$((pass + 1)); else
  fail=$((fail + 1)); echo "FAIL gh must be called with --state all"; fi

echo "eng-issue: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
