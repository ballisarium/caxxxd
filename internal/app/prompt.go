package app

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/pterm/pterm"

	"github.com/ballisarium/caxxxd/internal/ui"
)

// ErrInterrupted is what a prompt returns when the user presses Ctrl+C.
var ErrInterrupted = errors.New("interrupted")

// errNoSuchChoice means the menu returned something that was never offered.
var errNoSuchChoice = errors.New("the menu returned an unknown option")

// Choice is one option in a menu. Detail is the quiet half of the row.
type Choice struct {
	Label  string
	Detail string
}

// Prompter asks the user for one decision at a time.
//
// Everything caxxxd needs to know is either a line of text or a pick from a
// list, and a pick is made with the arrow keys. There is no letter shortcut
// anywhere in the flow, so no keyboard layout can leave a step unreachable —
// on a Cyrillic layout the D key reports "в", which is exactly the kind of
// thing a menu never has to care about.
type Prompter interface {
	// Text asks for one line. initial prefills the field; hint, when set, is
	// printed above the prompt.
	Text(question, hint, initial string) (string, error)

	// Choose shows a menu and returns the index that was picked. initial is
	// the option the cursor starts on.
	Choose(question string, choices []Choice, initial int) (int, error)
}

// filterThreshold is the menu size past which type-to-search is worth showing.
// The stream tables are the only lists that ever reach it.
const filterThreshold = 9

// filterable reports whether a menu of this many options will offer
// type-to-search, so a hint can promise it only when it is true.
func filterable(options int) bool { return options >= filterThreshold }

// menuHeight is the tallest a menu is allowed to be before it starts scrolling.
const menuHeight = 12

// selectorColumns is what the menu spends before a row. While choosing, that
// is two: the selector, or the blank standing in for it. The line it leaves
// behind once a choice is made is indented by four, and the budget has to
// cover the wider of the two or that last line wraps.
const selectorColumns = 4

// TerminalPrompter asks through the terminal: menus with pterm's interactive
// select, and text with a line editor.
//
// The text half is not pterm's. Its input offers no way to clear a line or
// delete a word, and it places the cursor by a width that counts some of its
// own escape codes as visible characters, which leaves the cursor sitting a
// few columns past what was typed.
type TerminalPrompter struct {
	console *ui.Console
	in      *os.File
}

// NewTerminalPrompter returns a Prompter styled to match console.
func NewTerminalPrompter(console *ui.Console) TerminalPrompter {
	return TerminalPrompter{console: console, in: os.Stdin}
}

// Text asks for one line of input.
func (p TerminalPrompter) Text(question, hint, initial string) (string, error) {
	if hint != "" {
		p.console.Hint(hint)
	}

	value, err := p.console.ReadLine(p.in, p.askLine(question), initial)
	p.console.Blank()

	if err != nil {
		// The editor reports both Ctrl+C and a closed input as the end of it.
		if errors.Is(err, io.EOF) {
			return "", ErrInterrupted
		}
		return "", err
	}
	return value, nil
}

// Choose shows a menu of choices.
func (p TerminalPrompter) Choose(question string, choices []Choice, initial int) (int, error) {
	if len(choices) == 0 {
		return 0, errNoSuchChoice
	}

	labels := menuLabels(choices, p.console.Width()-selectorColumns)
	if initial < 0 || initial >= len(labels) {
		initial = 0
	}

	// The selector's look is set field by field: pterm exposes builders for
	// the menu's behaviour but not for its styling.
	interrupted := false
	p.console.Hint("↑ / ↓  Move   Enter  Select   Ctrl+C  Quit")
	menu := pterm.DefaultInteractiveSelect
	menu.Options = labels
	menu.DefaultOption = labels[initial]
	menu.MaxHeight = min(len(labels), menuHeight)
	menu.Filter = filterable(len(labels))
	menu.TextStyle = pterm.NewStyle()
	menu.OptionStyle = pterm.NewStyle()
	menu.SelectorStyle = pterm.NewStyle(pterm.Bold)
	menu.Selector = p.console.Theme().OnKlein.Sprint("▶")
	menu.OnInterruptFunc = func() { interrupted = true }

	selected, err := menu.Show(p.ask(question))
	p.console.Blank()

	switch {
	case err != nil:
		return 0, err
	case interrupted:
		return 0, ErrInterrupted
	}

	for index, label := range labels {
		if label == selected {
			return index, nil
		}
	}
	return 0, errNoSuchChoice
}

// ask styles a question for pterm's interactive menu. The menu adds its own
// delimiter after the text; the line editor is handed the whole prompt and
// needs one written in.
func (p TerminalPrompter) ask(question string) string {
	parts := strings.SplitN(menuQuestionText(question), "\n", 2)
	// pterm appends its delimiter after the full prompt text. Keeping the rule
	// above the question leaves that delimiter attached to the question rather
	// than drawing it onto the separator.
	return p.console.Theme().Lift.Sprint(parts[0]) + "\n" + p.console.Theme().Glow.Sprint(parts[1])
}

func menuQuestionText(question string) string {
	width := max(len([]rune(question)), 1)
	return strings.Repeat("─", width) + "\n" + question
}

// askLine is the prompt a line editor draws, delimiter and all.
func (p TerminalPrompter) askLine(question string) string {
	return p.console.Theme().OnKlein.Sprint("▶") + " " + p.console.Theme().Ink.Sprint(question+": ")
}

// menuLabels lays the choices out as aligned rows, none wider than room.
//
// A row that does not fit wraps, and a wrapped row breaks the menu rather than
// merely looking untidy: pterm redraws by counting newlines, not by asking
// where the terminal folded a line, so the overflow stays on screen as the
// selection moves. The detail is the half that gets shortened, because the
// label is what is being chosen.
//
// The rows stay free of colour on purpose: pterm matches the type-to-search
// filter against these exact strings, and escape sequences inside them would
// make a search for "1080" miss the row that shows it.
func menuLabels(choices []Choice, room int) []string {
	// Only the rows that carry a detail are aligned against each other. A menu
	// can hold both a table of streams, whose rows are one long label, and a
	// way back with a few words after it; aligning those together would push
	// the few words off to where the table ends.
	label := 0
	for _, choice := range choices {
		if choice.Detail == "" {
			continue
		}
		if width := len([]rune(choice.Label)); width > label {
			label = width
		}
	}

	labels := make([]string, 0, len(choices))
	seen := make(map[string]bool, len(choices))
	for _, choice := range choices {
		row := choice.Label
		if choice.Detail != "" {
			row = ui.Pad(row, label) + "   " + choice.Detail
		}
		row = ui.Truncate(row, room)

		// pterm identifies a choice by its text, so two identical rows would
		// be the same option. Trailing space is invisible and breaks the tie.
		for seen[row] {
			row += " "
		}
		seen[row] = true

		labels = append(labels, row)
	}
	return labels
}

// backChoice is the row every menu that can go back ends with.
var backChoice = Choice{Label: "‹ Back", Detail: "return to the previous step"}

// quitChoice leaves caxxxd.
var quitChoice = Choice{Label: "Quit", Detail: "leave caxxxd"}
