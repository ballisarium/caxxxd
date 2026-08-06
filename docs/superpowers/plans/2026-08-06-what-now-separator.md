# What Now Menu Separator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate menu headings from active choices without changing menu behavior.

**Architecture:** Keep pterm as the interactive-select engine. Change only the
question string supplied by `TerminalPrompter.Choose`, using a pure helper for
the separator text so the visual contract is unit-testable.

**Tech Stack:** Go 1.26, pterm 0.12.83, existing PTY/terminal tests, macOS
terminal screenshot capture.

## Global Constraints

- Keep `▸` only for the selected menu row.
- Keep text-entry prompts unchanged.
- Do not add a production dependency.
- Preserve the existing Klein-blue palette and pterm keyboard behavior.
- Verify the live PTY rendering before committing.

---

### Task 1: Lock the menu-heading text contract

**Files:**
- Create: `internal/app/prompt_test.go`
- Modify: none

**Interfaces:**
- Test the package-local `menuQuestionText(question string) string` contract
  before production code exists.

- `Step 1: Write the failing test`

```go
func TestMenuQuestionTextSeparatesTheHeadingFromChoices(t *testing.T) {
    got := menuQuestionText(`What now?`)
    want := `─────────\nWhat now?`
    if got != want {
        t.Fatalf(`menuQuestionText() = %q, want %q`, got, want)
    }
    if strings.Contains(got, `▸`) {
        t.Fatalf(`menu question must not contain the choice selector: %q`, got)
    }
}
```

- `Step 2: Run the focused test to verify it fails`

Run:

```bash
go test ./internal/app -run '^TestMenuQuestionTextSeparatesTheHeadingFromChoices$' -count=1
```

Expected: compilation fails because `menuQuestionText` does not exist yet.

### Task 2: Render the separated menu heading

**Files:**
- Modify: `internal/app/prompt.go`
- Test: `internal/app/prompt_test.go`

**Interfaces:**
- `menuQuestionText(question string) string` returns the plain separator and
  question text.
- `TerminalPrompter.ask(question string) string` styles that text for pterm.

- `Step 1: Implement the smallest helper`

```go
func menuQuestionText(question string) string {
    width := max(len([]rune(question)), 1)
    return strings.Repeat(`─`, width) + `\n` + question
}
```

- `Step 2: Use the existing theme without changing menu behavior`

Style the separator with `Theme.Lift`, the question with `Theme.Glow`, and
leave `menu.Selector = "▸"` unchanged.

- `Step 3: Run the focused test`

Run the same focused command from Task 1. Expected: PASS.

- `Step 4: Run the complete automated checks`

```bash
gofmt -l .
go vet ./...
go test ./... -race -count=1
go build ./cmd/caxxxd
git diff --check
```

Expected: formatting prints nothing and every command exits 0.

### Task 3: Verify the real terminal rendering

**Files:**
- Create: `.scratch/what-now-menu.png`
- Modify: none

- `Step 1: Run the recovery-menu PTY harness`

Run:

```bash
go run .scratch/what_now_demo.go
```

The harness uses the real `TerminalPrompter`, a deterministic unavailable-media
runner, and the same recovery path that displays `What now?`. Capture the
fixed-width terminal output at the supported terminal width. The existing
`TestUXDemo` remains useful for the happy path, but it does not enter this
recovery menu.

- `Step 2: Inspect the screenshot`

Confirm that the question has a separator and no `▸`, while exactly one `▸`
marks the active choice. Compare the captured screen with the supplied
reference and record any mismatch before committing.

### Task 4: Commit and publish

**Files:**
- All files from Tasks 1–3 that are intentional.

- `Step 1: Review the diff and staged file list`

```bash
git status --short
git diff --check
```

- `Step 2: Commit one logical UI change`

```bash
git add internal/app/prompt.go internal/app/prompt_test.go docs/superpowers/
git commit -m "Separate menu headings from active choices"
```

- `Step 3: Push the current main branch`

```bash
git push origin main
```

- `Step 4: Verify the remote commit`

```bash
git status --short --branch
git ls-remote origin refs/heads/main
```
