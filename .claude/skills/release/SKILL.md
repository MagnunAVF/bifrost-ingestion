---
name: release
description: Release a finished milestone: release-notes PR into dev, then the dev → main release PR.
argument-hint: "<milestone id> <version>, e.g. M1 v0.1.0"
disable-model-invocation: true
---

Arguments: $ARGUMENTS (milestone id, then version).

Phase 1: release notes into dev

1. Read docs/milestones/<id>.md. For each exit criterion show the evidence (test, file, PR #).
   If any is unmet, stop and list it.
2. `gh issue list --milestone "<title>" --state open` must be empty. If not, stop.
3. `git fetch origin && git checkout -b release/<version> origin/dev`.
4. Write docs/releases/<version>.md: highlights, how to run (Ollama model, make run), known
   limits, then all changes grouped by type from `git log <last-tag>..origin/dev --oneline`
   (whole history for the first release). Issues are written ENG-<n>.
5. Prepend the same notes to CHANGELOG.md and run `make check`.
6. Commit "chore(release): <version>", push, and open a PR into dev with that title.
   STOP until I say it is merged.

Phase 2: promote dev to main 7. `gh pr create --base main --head dev --title "release: <version> (<id>)"` with a body listing
the milestone's ENG issues and linking docs/releases/<version>.md. Print the URL. 8. Print the commands I run after merging it with a MERGE COMMIT (not squash):
git fetch origin && git checkout main && git pull
git tag -a <version> -m "Bifröst <version>: <milestone title>"
git push origin <version>
