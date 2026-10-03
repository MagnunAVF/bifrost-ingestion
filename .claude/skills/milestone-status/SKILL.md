---
name: milestone-status
description: Report a milestone's progress against its plan and recommend what to do next. Use when asked about progress or what to work on next.
argument-hint: "<milestone id, e.g. M1>"
---

1. Read docs/milestones/$ARGUMENTS.md (plan ids, ENG numbers, dependencies, lanes, schedule).
2. Run `gh issue list --milestone "<title>" --state all --json number,title,state` and
   `gh pr list --base dev --state open --json number,title,headRefName`.
3. Report per lane, writing issues as ENG-<n> and pull requests as PR #<n>:
   done / in review / not started, and time used vs the schedule.
4. List exit criteria still unmet.
5. Recommend the next 1-2 issues whose dependencies are done; say which can run in parallel.
6. Flag scope creep: issues in the GitHub milestone that aren't in the plan, and vice versa.
   Don't edit anything.
