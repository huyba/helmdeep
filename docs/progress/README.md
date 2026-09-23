# Progress reports

Point-in-time snapshots of what was done and what was left, one file per
report, never edited after the fact. `STATUS.md` in the repo root is the
*current* status and gets rewritten in place; this directory is the
history behind it, so a claim made in September can still be checked in
March.

| Report | Commit | Headline |
|---|---|---|
| [2026-09-23 09:29 PDT](2026-09-23-0929.md) | `0354e91` | Tool Gateway working end to end; 5 narrow platform slices delivered (identity, tool registry, egress, model gateway, policy service); 0 of 16 design docs fully implemented, 61 tracked gaps open |

## Writing the next one

- File name is `YYYY-MM-DD-HHMM.md` in local time, so the directory
  sorts chronologically. One report per file — never overwrite or edit a
  past report; correct it in the next one instead, and say what changed.
- Copy the previous report's section order (1–7) so two reports diff
  cleanly.
- Record the commit SHA the report describes, and set "Previous report"
  to the file it follows.
- Metrics come from the repo, not from memory: `git log --oneline | wc -l`,
  `go list ./... | wc -l`, `find . -name '*.go' -not -name '*_test.go' | xargs cat | wc -l`,
  `grep -rh '^func Test' --include='*_test.go' . | wc -l`.
- The open-item count is the sum of the per-area rows in §5. It moves
  when a gap is actually closed, or when a new one is found and written
  down — not when work merely starts on one.
- Add a row to the table above in the same commit.
