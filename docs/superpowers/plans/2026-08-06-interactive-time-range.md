# Interactive Time Range Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a polished in-app whole-video or time-range choice that shares the existing clipping pipeline.

**Architecture:** Add a range stage to the existing linear app workflow and keep it on the current Media progress step. Use `domain.ParseTimeRange` and `TimeRange.ValidateDuration` for all interactive validation, keep CLI-provided ranges fixed and non-interactive, and add a review route that returns to the range stage without losing format choices.

**Tech Stack:** Go 1.26, existing `Prompter` interface, pterm menus, existing domain time-range parser, yt-dlp `--download-sections`, ffprobe packet inspection, ffmpeg stream-copy trimmer, scripted app tests, macOS PTY live test.

## Global Constraints

- Keep `--section` parsing and validation behavior unchanged.
- Do not add a production dependency.
- Use one effective `*domain.TimeRange` for review, download arguments, and trimming.
- Keep the progress bar at five conceptual steps.
- Keep `Ctrl+C` during text entry as session interruption.
- Use existing menu, notice, hint, and text-prompt primitives.
- Reject invalid syntax, reversed/equal endpoints, unknown durations, and ranges beyond the fetched duration.

---

### Task 1: Lock interactive range behavior with app tests

**Files:**
- Modify: `internal/app/app_test.go`
- Modify: `internal/app/selection_test.go`
- Modify: `internal/app/screen_test.go`
- Modify: `internal/app/helpers_test.go` only if a shared test assertion is needed

**Interfaces:**
- The existing scripted prompter records menu labels, question order, text prefills, drawn notices, and downloader arguments.
- Tests use `internal/app/testdata/video.json`, whose duration is `125.4` seconds.

- [x] **Step 1: Add the whole-video and interactive-range flows**

Add app tests that expect a new `Time range` menu after metadata and before
the media mode. Cover these scripts:

```go
text(link),
pick("Whole video"),
pick("Video"),
pick("Best available"),
pick("MKV"),
pick("Download"),
pick("Quit"),
```

and:

```go
text(link),
pick("Choose a range"),
text("00:30-01:20"),
pick("Video"),
pick("Best available"),
pick("MKV"),
pick("Download"),
pick("Quit"),
```

Assert that whole-video review has no time-range field, while the clipped
review shows `Time range` and the downloader receives
`--download-sections`, `*0:30-1:20`.

- [x] **Step 2: Add validation and navigation cases**

Add tests for malformed input followed by a valid range, an end beyond the
fixture duration followed by a valid range, `‹ Back` from the initial range
menu, and `Change time range` from review. Assert that invalid input redraws a
clear notice, does not start the downloader, and does not leave the invalid
value in review.

Update existing scripted happy-path and back-navigation flows to select
`Whole video` where the new menu is now part of the normal path. Add a CLI
section test that proves the new menu is skipped and the existing section
still reaches review and the downloader unchanged.

- [x] **Step 3: Run the focused tests and verify RED**

Run:

```bash
go test ./internal/app -run 'Test(Interactive|WholeVideo|Range|Back|Section)' -count=1
```

The focused tests initially failed because the `Time range` stage and review
action did not exist yet. Existing tests also identified the back-navigation
paths that needed explicit range handling.

### Task 2: Add the range stage and effective session state

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/steps.go`

**Interfaces:**
- `App` keeps `sectionFixed` for a CLI-supplied range and a return-stage field
  for the range editor.
- `askRange() (stage, error)` owns the menu, text input, validation, and
  return routing.

- [x] **Step 1: Add the Media-stage range state**

Add a `stageRange` stage that maps to step `2`, include it in `showsMedia`, and
dispatch it from `step`. Record whether `Options.Section` was supplied before
the UI starts so a CLI range can skip the interactive menu. Default the range
editor's next stage to `stageMode` and allow the review to temporarily set it
to `stageReview`.

- [x] **Step 2: Route metadata and back transitions through the range stage**

After successful metadata and manual-selection reset, return `stageMode` for
a fixed CLI range and `stageRange` otherwise. Make `askMode`'s Back choice
return to `stageRange`. Clear an interactive range before fetching a different
URL and when resetting for the next item; never clear a fixed CLI range.

- [x] **Step 3: Implement the range menu and text input**

Use the following choices and existing duration formatter:

```go
[]Choice{
    {Label: "Whole video", Detail: "download all " + ui.FormatDuration(a.info.Duration)},
    {Label: "Choose a range", Detail: "download only part of this video"},
    backChoice,
}
```

For `Choose a range`, call `Prompter.Text` with question `Time range`, a hint
that names all accepted formats and the duration, and the previous valid range
as the initial value when one exists. Parse with `domain.ParseTimeRange`, then
validate with `ValidateDuration`. On failure, carry `That time range is not
valid`, the domain error, and the concrete example `00:30-01:20` before
returning to `stageRange`. Assign the parsed value only after both checks pass.

- [x] **Step 4: Add the review editor route**

Add `Change time range` to the review choices only when the range is not CLI
fixed. Route it to `stageRange`; after a valid whole-video or clipped choice,
return to `stageReview` so mode, quality, container, and manual stream choices
remain intact. Keep the existing review Back and Quit behavior for all other
choices.

- [x] **Step 5: Run the focused tests and verify GREEN**

Run the focused command from Task 1. Expected: all new and updated app tests
pass, including the invalid-input redraw and review edit cases.

### Task 3: Clarify the user-facing review and documentation

**Files:**
- Modify: `internal/app/steps.go`
- Modify: `README.md`
- Modify: `internal/ytdlp/trim.go`
- Modify: `internal/ytdlp/trim_test.go`

- [x] **Step 1: Use user-facing time-range copy in the review**

Render the selected value under `Time range`, not the implementation-oriented
`Section` label. Omit the field for whole-video downloads. Keep the CLI flag
name and command examples unchanged elsewhere in the README.

- [x] **Step 2: Document the in-app flow**

Extend the README time-ranges section with a short example explaining that a
user can choose `Whole video` or `Choose a range` after pasting a URL, then
enter `START-END` in `SS`, `MM:SS`, or `HH:MM:SS` form. Keep the existing
`--section` examples and explain that both paths use the same clipping logic.

- [x] **Step 3: Run formatting and documentation checks**

Run:

```bash
gofmt -w internal/app/app.go internal/app/steps.go internal/app/app_test.go internal/app/selection_test.go internal/app/screen_test.go internal/app/helpers_test.go
gofmt -l .
git diff --check
```

Expected: no unformatted Go files and no whitespace errors.

- [x] **Step 4: Keep local clipping correct for sectioned streams**

Probe the first local media packet before the stream-copy pass. This avoids
applying the original URL timestamp a second time after yt-dlp has already
seeked a remote stream. When an audio file contains an attached thumbnail,
map only the audio stream if the target container cannot accept the picture.
Cover both cases with trimmer tests.

### Task 4: Verify the whole feature and live terminal UX

**Files:**
- Use ignored files under `.scratch/` for the
  deterministic PTY harness and screenshot.

- [x] **Step 1: Run the complete automated suite**

Run:

```bash
go test ./... -race -count=1
go vet ./...
go build ./cmd/caxxxd
```

All packages must pass with zero race reports, vet findings, or build errors.

- [x] **Step 2: Run a real PTY scenario**

Run a live harness with a known public test URL and actual
`TerminalPrompter`, then enter `Choose a range` and `00:30-01:20`. Confirm
the terminal visibly shows the range menu, the duration hint, the range in the
review, and the `Change time range` action. Capture the tested screen under
`.scratch/interactive-time-range.png` and inspect it with the image viewer.

- [x] **Step 3: Compare the live output with the design**

Confirm that the range menu is visually separated from the media card, the
range input explains its syntax, errors do not accumulate across redraws, and
the review keeps the existing layout while exposing the selected range.

- [x] **Step 4: Review the final diff**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Only the range implementation, local clipping fix, tests, README, and the
approved design/plan documents may be included. `.scratch/` artifacts must
remain ignored.

### Task 5: Commit and publish

- [ ] **Step 1: Stage the intentional files**

```bash
git add internal/app/app.go internal/app/steps.go internal/app/app_test.go internal/app/helpers_test.go internal/app/selection_test.go internal/ytdlp/trim.go internal/ytdlp/trim_test.go README.md docs/superpowers/specs/2026-08-06-interactive-time-range-design.md docs/superpowers/plans/2026-08-06-interactive-time-range.md
git diff --cached --check
```

- [ ] **Step 2: Create one logical commit**

```bash
git commit -m "Add interactive time range selection"
```

- [ ] **Step 3: Push the current branch**

```bash
git push origin main
```

- [ ] **Step 4: Verify the remote commit**

```bash
git status --short --branch
git rev-parse HEAD
git ls-remote origin refs/heads/main
```

The local and remote SHA must match, and the worktree must be clean apart from
ignored `.scratch/` artifacts.
