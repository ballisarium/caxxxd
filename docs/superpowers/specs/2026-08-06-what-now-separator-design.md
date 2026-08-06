# What Now Menu Separator Design

## Goal

Make the question that introduces a choice menu visually distinct from the
currently selected menu row.

## Problem

The terminal menu uses `▸` both for the pterm question line and for the active
choice. On recovery screens this makes `What now?` look like another option
instead of the menu heading.

## Decision

Keep `▸` exclusively for the active choice. Render a short horizontal
separator in the menu accent color above the question, then render the
question in the question accent color:

```text
─────────
What now?:
▸ Try another link    start again from the URL
  Show details        the raw output caxxxd kept
  Quit                leave caxxxd
```

The separator length follows the question's rune length. Text-entry prompts
keep their existing `▸` prompt because they do not render a choice list.
Selection behavior, keyboard controls, wrapping, filtering, and pterm's
delimiter remain unchanged.

## Verification

- Add a focused unit test for the menu-question text.
- Run the focused test, the complete race-enabled Go test suite, vet, build,
  formatting, and diff checks.
- Run the recovery-menu PTY harness and capture the rendered output as a
  screenshot.
- Inspect the screenshot to verify that the separator and choice marker are
  visually distinct at the supported terminal width.
