# AGENTS.md

These rules apply to the whole `caxxxd` repository.

## Project

`caxxxd` is a macOS terminal frontend for `yt-dlp` and `ffmpeg`, written in
Go. The supported runtime is macOS 13 or newer, with Go 1.26 and the external
commands `yt-dlp` and `ffmpeg` available on `PATH`.

The user-facing interface is an interactive terminal flow:

1. accept one media URL;
2. fetch structured metadata;
3. choose video or audio and resolve a format;
4. review the request;
5. download, post-process, and report the resulting file.

The project is intentionally small and opinionated. Keep the common path
clear, predictable, and safe.

## Repository map

- `cmd/caxxxd/` contains the executable entry point and CLI argument parsing.
- `internal/app/` owns the workflow, prompts, state transitions, errors, and
  download lifecycle.
- `internal/domain/` owns shared media vocabulary, presets, and time ranges.
- `internal/ytdlp/` owns metadata parsing, format normalization, command
  construction, structured process output, and local media trimming.
- `internal/ui/` owns terminal rendering, prompts, menus, progress, and
  terminal cleanup.
- `internal/deps/` checks the external tools.
- `internal/config/` stores remembered local preferences.
- `internal/process/` owns Unix process-group termination.
- `docs/` contains repository documentation assets such as screenshots.
- `.github/` contains CI and release workflows.

## Non-negotiable rules

- Never print, commit, or place secrets, tokens, credentials, raw `.env`
  values, signed media URLs, or private user data in source, fixtures, logs,
  screenshots, test output, or documentation.
- Do not add production dependencies without explicit approval. Prefer the
  standard library and the dependencies already present in `go.mod`.
- Do not hand-edit generated release artifacts. Change their declarative
  source and regenerate them when generation is part of the requested task.
- Preserve existing user changes. Do not reset, revert, reformat, delete, or
  overwrite unrelated work.
- Do not stage, commit, amend, rebase, stash, push, or create a release unless
  the user explicitly asks for that operation.
- Keep temporary files, live-test outputs, downloaded media, and diagnostic
  artifacts under `.scratch/`. Never put them in the repository root or
  `docs/`.
- Use `apply_patch` for source and documentation edits. Do not write project
  files with shell redirection or ad-hoc file-generation commands.
- Do not build shell command strings. Pass URLs, paths, and flags as argument
  slices to `exec.Command` or the relevant runner.
- Do not weaken validation, permissions, cancellation, or error handling to
  make a test pass.

## Implementation rules

### Workflow and state

- Fix behavior at the owning layer. Do not hide a parent state-transition bug
  with a child-side fallback.
- When a shared boundary changes, update both sides: producer and consumer,
  success and error paths, retry behavior, and user-visible state.
- A failed metadata lookup must not leave the rejected URL prefilled in the
  next URL prompt.
- Keep one-item-per-run behavior and the explicit `--no-playlist` guard unless
  the user requests a different product contract.
- Every external operation must have a clear cancellation and failure path.
- Cancellation must not be reported as an ordinary tool failure.
- Do not claim a file was delivered, replaced, or completed until the relevant
  filesystem or process result proves it.

### `yt-dlp` and `ffmpeg`

- Read metadata from structured JSON and parse caxxxd's structured progress
  markers. Do not scrape human-readable output for normal control flow.
- Keep URLs and user paths as separate process arguments.
- Video downloads must preserve source codecs. Use remuxing or stream copy
  where possible; do not silently transcode video.
- `--section START-END` accepts `SS`, `MM:SS`, and `HH:MM:SS`. Validate the
  relationship between endpoints before invoking `yt-dlp`, then validate the
  end against the fetched media duration.
- Section downloads must use `yt-dlp --download-sections` when the source
  supports seeking. The local normalization pass must use `ffmpeg -c copy`
  and replace the result atomically only after success.
- If local clipping fails, keep the original file intact and show an actionable
  `ffmpeg`/processing error.
- Process groups must include child `ffmpeg` processes so `Ctrl+C` does not
  leave a downloader running in the background.

### UI and text

- Follow the existing Klein-blue terminal language, screen-per-step layout,
  menus, progress views, and terminal cleanup behavior.
- Reuse existing prompts, labels, and error categories before inventing new
  ones.
- User-facing copy should be plain, concise, and honest. Do not expose raw
  diagnostics until the user asks for details.
- Keep the interface usable with English, Cyrillic, and other keyboard layouts;
  do not rely on letter shortcuts for menu actions.
- Update `README.md` when a flag, workflow, output contract, dependency, or
  release behavior changes.

## Compatibility guard

dont request the admin list for non-admin communities (stock bug)

Treat this as a hard compatibility rule. Do not introduce an admin-list
request into a flow where the community is not known to be administered.

## Test-first workflow

For behavior changes, start with the smallest user-visible or contract-level
failing test:

1. reproduce the bug or specify the new behavior;
2. add the focused failing test;
3. implement the smallest coherent fix;
4. run the focused test;
5. run the wider checks before claiming completion.

Prefer fakes and checked-in JSON fixtures for ordinary tests. Do not make the
normal suite depend on a network account or a live media site. Use a real PTY
when raw terminal behavior, bracketed paste, or escape sequences are the thing
under test.

## Required verification

Run the cheapest relevant checks first, then the full set for release-facing
changes:

```bash
gofmt -l .
go vet ./...
go test ./... -race -count=1
go build ./cmd/caxxxd
git diff --check
```

`gofmt -l .` must print nothing. A non-zero exit, race, panic, unhandled
rejection, failed assertion, or build error is a failed check and must be
reported honestly.

For a live test, keep local automated checks, the real `yt-dlp`/`ffmpeg`
execution, and the final output inspection as separate evidence. Verify the
result with `ffprobe` or an equivalent tool, and do not expose signed URLs in
the report.

Tags matching `v*` trigger the release workflow. Release work must also
respect `.goreleaser.yaml`, the macOS arm64/amd64 targets, and the Homebrew
cask configuration. Do not run a release merely because the build passes.

## Git hygiene

Only source and project assets belong in the repository:

- keep Go source, tests, fixtures, `README.md`, `LICENSE`, `docs/`, workflows,
  `go.mod`, `go.sum`, and release configuration when they are intentional;
- keep local binaries, `dist/`, `.scratch/`, downloaded media, `.part` files,
  logs, coverage output, editor state, and OS metadata out of Git;
- do not use `.gitignore` as a way to hide a source change or a file that
  should be reviewed;
- remember that `.gitignore` does not remove files already tracked by Git.

## Commit messages

Commit messages are written in English and follow this format.

### Subject

- One line, imperative mood, capitalized, with no trailing period.
- Describe the change and its purpose at a meaningful altitude.
- Aim for roughly 50–72 characters.
- Start with a strong verb such as `Add`, `Fix`, `Refine`, `Restructure`,
  `Clarify`, `Simplify`, `Remove`, or `Restore`.
- Keep one logical change per commit. Do not bundle unrelated cleanup.

Good subjects:

```text
Add section clipping with timestamp normalization
Fix blank URL field after metadata failure
Refine download errors with actionable recovery
Update README with release installation steps
```

### Body

Add a body when the change needs context:

- leave one blank line after the subject;
- wrap lines at roughly 72 columns;
- use present tense;
- explain the problem, why the change exists, and the user-visible effect;
- mention notable tests, compatibility constraints, or side effects;
- do not paste a raw diff or write filler such as `various changes`.

Example:

```text
Fix clipped media timestamps with local stream copy

Independent DASH streams can retain different timestamps after
yt-dlp seeks and merges a requested section. Normalize the short
merged file with ffmpeg stream copy before reporting success, while
leaving the original intact if the local pass fails.
```

Before committing, inspect the diff, run the required verification, and make
sure no ignored or sensitive artifact is being added intentionally.
