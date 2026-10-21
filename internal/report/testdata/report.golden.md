## xis provenance report

**Agent:** claude-code 2.1.304 · **Models:** claude-sonnet-5-5 · **Fidelity:** live
**Sessions:** 1 · **Steps:** 5 · **Duration:** 3m12s
**Change:** 3f786850e387 → worktree · 3 files · +7 −1 · contributors: ai 2, human 1 hunks

### Intent

- ✅ Fix the Add function (addressed)
- ❌ Add a test for Sub (unaddressed)

### Flags

| Severity | Rule | Location | Message |
| --- | --- | --- | --- |
| high | `TEST_EDIT_AFTER_FAILURE` | `step st3` | The agent edited calc_test.go after a failing test run. |

Suppressed:
- `TEST_FILE_MODIFIED` at `file calc_test.go`: the old test was wrong

### Review units

1. **Fix addition** ✔
   Add now adds instead of subtracting [hunk:h1].
   Files: `calc.go`
2. **Tests: calc_test.go**
   1 hunk from steps #3 (1 ai).
   Files: `calc_test.go`
3. **Docs: README.md** ❓
   1 hunk from steps #5 (1 human).
   Files: `README.md`

### Verification

- step 3 `go test ./...` → **fail**
- step 4 `go test ./...` → **pass**

Last test run: **pass**.

Unverified agent claims:
- `calc.go`: "The bug is in Add: it subtracts."

---
<sub>xis v0.1.0 · sessions 01M5CTQV4E00 · note `refs/notes/xis`</sub>
