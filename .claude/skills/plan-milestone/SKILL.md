---
name: plan-milestone
description: Break a roadmap milestone into a milestone plan file and ENG issues.
argument-hint: "<milestone id, e.g. M2>"
disable-model-invocation: true
---

Plan milestone $ARGUMENTS.

1. Read docs/plans/ROADMAP.md (the $ARGUMENTS section), CLAUDE.md, the previous milestone file and its
   release notes.
2. Write docs/milestones/$ARGUMENTS.md: Goal, Exit criteria (testable), Work breakdown
   (Plan id, Issue, Depends on, Lane, Estimate), Schedule, Risks, Open questions, Decisions.
   Each issue = one PR into dev, at most 1-2 days. Different lanes must not touch the same files.
   Use "new" in the Issue column and add a "GitHub" column. STOP for my review.
3. After approval, assign ENG ids in plan order, starting after the highest ENG-<n> in any issue
   title (ENG ids are title ids and are NOT GitHub numbers). For each item, in plan order:
   `gh issue create --milestone "<title>" --label <labels> --title "ENG-<n>: [<plan id>] <title>"`
   with acceptance criteria, and note the GitHub number from the returned URL. Write
   dependencies and plan file names in bodies as ENG ids, never as GitHub numbers. Fill the Issue
   and GitHub columns, then check every row with `scripts/eng-issue.sh ENG-<n>`.
4. Commit on `docs/plan-$ARGUMENTS` and open a PR into dev.
