# Interactive Time Range Design

## Goal

Let users choose a whole video or a clipped time range from inside caxxxd,
while keeping the existing `--section` command-line flow unchanged.

## User flow

After metadata is fetched and before the media mode choice, show a compact
range menu on the existing Media progress step:

```text
Time range

▸ Whole video       download all 2:05
  Choose a range    download only part of this video
  ‹ Back             return to the URL
```

Choosing `Choose a range` opens one text prompt. The prompt accepts the same
syntax as `--section` and shows the known duration as a hint:

```text
Time range: 00:30-01:20
Use SS, MM:SS, or HH:MM:SS. Video duration: 2:05.
```

The range is normalized after parsing, so `90-120` is shown as
`1:30-2:00` in the review. Invalid input stays in the range flow, carries a
short explanation to the next redraw, and never replaces a previously valid
range with invalid state.

The review includes `Change time range` when the range was selected inside the
app. That action returns directly to the range menu and then back to the same
review after a new valid choice, without making the user repeat format
selection. `‹ Back` from the initial range menu returns to the URL prompt.

## State and CLI compatibility

The fetched media's effective range remains the single value consumed by the
review, yt-dlp command builder, and local normalization step. Because
`--download-sections` can leave the first packet after container time zero,
the trimmer probes that packet before seeking with ffmpeg. Audio-only files
drop attached thumbnails during stream-copy clipping when their container
cannot accept a copied picture stream.

- A CLI-provided `--section` is validated after metadata lookup exactly as it
  is now and skips the interactive range menu.
- An interactive choice is stored only for the current item and is cleared
  when the user starts another URL.
- `Whole video` clears an interactive range and leaves the existing full-file
  behavior unchanged.
- The five-step status bar remains unchanged; the range menu shares the Media
  step rather than adding a sixth progress step.

## Error handling

Use the existing domain parser and duration validator. Explain malformed
syntax, reversed or equal endpoints, and endpoints beyond the media duration
using the existing error text. A metadata duration that is unavailable makes
an interactive range invalid because the application cannot safely verify it.
Ctrl+C during the text prompt keeps its current meaning and exits the session;
the menu's `‹ Back` is the navigation path before entering text.

## Verification

- Add app-level tests for whole-video selection, valid interactive ranges,
  invalid range retry, duration overflow, back navigation, review editing, and
  CLI `--section` compatibility.
- Run focused red/green tests, the full race-enabled Go suite, vet, build,
  formatting, and diff checks.
- Run a live PTY scenario with a deterministic metadata fixture, choose a
  range, and inspect the terminal output and review summary before committing.
