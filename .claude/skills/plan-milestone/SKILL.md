---
name: plan-milestone
description: Break a roadmap milestone into a milestone plan file and ENG issues.
argument-hint: "<milestone id, e.g. M2>"
disable-model-invocation: true
---

Plan milestone $ARGUMENTS.

1. Read docs/roadmap.md (the $ARGUMENTS section), CLAUDE.md, the previous milestone file and its
   release notes.
2. Write docs/milestones/$ARGUMENTS.md: Goal, Exit criteria (testable), Work breakdown
   (Plan id, Issue, Depends on, Lane, Estimate), Schedule, Risks, Open questions, Decisions.
   Each issue = one PR into dev, at most 1-2 days. Different lanes must not touch the same files.
   Use "new" in the Issue column. STOP for my review.
3. After approval, for each item: `gh issue create --milestone "<title>" --label <labels>
--title "[<plan id>] <title>"` with acceptance criteria; read the number from the returned URL
   and rename it: `gh issue edit <n> --title "ENG-<n>: [<plan id>] <title>"`.
   Then replace dependency placeholders in bodies with ENG-<n> and fill the Issue column.
4. Commit on `docs/plan-$ARGUMENTS` and open a PR into dev.
