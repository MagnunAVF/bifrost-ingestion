---
name: ship
description: Commit the current work, push the branch, and open a pull request into dev for an ENG issue.
argument-hint: "ENG-<n>"
disable-model-invocation: true
---

Ship the work for $ARGUMENTS. The GitHub issue number is the digits after "ENG-".

1. Run `make check`. If it fails, stop and report.
2. The current branch must contain "ENG-<n>" and must not be dev or main. Otherwise stop.
3. Review `git status` and `git diff --stat origin/dev...HEAD`. testdata/catalog.db and
   testdata/ProductEntry.json must be unchanged. List any file unrelated to the issue and ask
   before including it.
4. Commit with conventional messages ending in "(ENG-<n>)" (the PR is squashed into dev).
5. `git push -u origin HEAD`.
6. `gh pr create --base dev --milestone "<current milestone title>" --title "<type>(<scope>): <summary> (ENG-<n>)"`
   with a body following .github/pull_request_template.md, starting with "Issue: ENG-<n>".
   Never write "#<n>" for an issue and don't use closing keywords.
7. Print the PR URL.
