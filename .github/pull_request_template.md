Issue: ENG-

## What

<one paragraph: what changed and why>

## How

- <key decisions; link docs/plans/ENG-<n>-\*.md if there is one>

## Tests

- <new or changed tests / table rows>

## Checklist

- [ ] Base branch is `dev` (or `main` only for a release/hotfix)
- [ ] Title ends with `(ENG-<n>)`; issues are written ENG-<n>, never #<n>
- [ ] `make check` passes
- [ ] Fixtures in testdata/ untouched; no existing tests modified
- [ ] sqlc code regenerated if queries or migrations changed
- [ ] Decisions recorded in docs/milestones/M1.md (and an ADR if significant)
