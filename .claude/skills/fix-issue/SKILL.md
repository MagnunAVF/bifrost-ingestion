---
name: fix-issue
description: Fix a bug reported as an ENG issue, starting from a failing regression test.
argument-hint: "ENG-<n>"
---

Fix $ARGUMENTS. The GitHub issue number is the digits after "ENG-".

1. `gh issue view <n> --comments`.
2. `git fetch origin && git checkout -b fix/ENG-<n>-<short-slug> origin/dev`.
3. Find the root cause and explain it in 2-3 sentences.
4. Add a table row or test that reproduces the bug; show it failing with `go test -run`.
5. Fix the root cause, not the symptom. Run `make check`.
6. Stop and tell me it's ready for /ship.
