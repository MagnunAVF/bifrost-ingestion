---
name: start-issue
description: Start work on an ENG issue: read it, branch from dev, and propose a test-first plan. Use when beginning any issue.
argument-hint: "ENG-<n>"
---

Start work on $ARGUMENTS. ENG-<n> is the id in the issue title, not the GitHub issue number:
resolve it first with `scripts/eng-issue.sh $ARGUMENTS` (prints <number>).

1. Run `gh issue view <number> --comments`. Read the milestone plan named in CLAUDE.md
   "Current focus" (docs/milestones/M<k>.md) and docs/data-notes.md.
   Take dependencies from the milestone table (plan ids → ENG ids), resolve each ENG id with
   scripts/eng-issue.sh, and if one is still open, stop and tell me.
2. `git fetch origin`, then `git checkout -b <type>/ENG-<n>-<short-slug> origin/dev`
   (type: feat, fix, chore or docs).
3. Read the packages the issue touches and their nested CLAUDE.md files.
4. Propose, without writing code: public API (types and signatures), the test table (happy path
   plus one row per acceptance criterion and error case), files to create or change, and
   decisions that need my input. If the issue says "write the plan first", write ONLY
     docs/plans/ENG-<n>-<slug>.md instead. STOP and wait for my approval.
5. After approval: save the approved proposal as docs/plans/ENG-<n>-<slug>.md (always, even if
   the issue didn't ask for a plan; same layout as the existing plans: status line with the
   approval date, goal, public API, tests, tasks, approved decisions) and keep it in sync if the
   API changes during implementation; commit it with the issue's docs task. Then tests first
   (show them failing for the right reason), then the implementation, then `make check` until
   green.
6. Summarize what changed and what I should double-check. Don't ship; I'll run /ship.
