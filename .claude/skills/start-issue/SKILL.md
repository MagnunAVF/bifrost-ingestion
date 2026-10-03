---
name: start-issue
description: Start work on an ENG issue: read it, branch from dev, and propose a test-first plan. Use when beginning any issue.
argument-hint: "ENG-<n>"
---

Start work on $ARGUMENTS. The GitHub issue number is the digits after "ENG-".

1. Run `gh issue view <n> --comments`. Read docs/milestones/M1.md and docs/data-notes.md.
   If an issue listed under "Depends on" is still open, stop and tell me.
2. `git fetch origin`, then `git checkout -b <type>/ENG-<n>-<short-slug> origin/dev`
   (type: feat, fix, chore or docs).
3. Read the packages the issue touches and their nested CLAUDE.md files.
4. Propose, without writing code: public API (types and signatures), the test table (happy path
   - one row per acceptance criterion and error case), files to create or change, and decisions
     that need my input. If the issue says "write the plan first", write ONLY
     docs/plans/ENG-<n>-<slug>.md instead. STOP and wait for my approval.
5. After approval: tests first (show them failing for the right reason), then the implementation,
   then `make check` until green.
6. Summarize what changed and what I should double-check. Don't ship; I'll run /ship.
