package ui

import (
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// Tagline is the one-line description under the logo.
const Tagline = "media downloads without the flag maze"

// The layout is drawn to the terminal, but never narrower than MinWidth or
// wider than MaxWidth: a full-width panel on a maximised window reads as a
// stretched banner rather than as a card.
const (
	MinWidth = 60
	MaxWidth = 104
)

// Field is one label/value pair inside a panel.
type Field struct {
	Label string
	Value string
}

// Status is the state the status bar reports. It outlives any single step,
// which is what makes the bar worth looking at.
type Status struct {
	Version     string
	Tools       string
	Destination string
	Step        int
	Steps       int
	Title       string
}

// Console draws every part of caxxxd that is not a prompt. All output goes
// through its writer, so a test can render the whole flow into a buffer.
type Console struct {
	theme       Theme
	out         io.Writer
	width       int
	widthPinned bool
	status      Status
	clears      bool
	animates    bool
	raw         bool

	// lastBar is the status the bar currently on screen was drawn from, so a
	// step that changes nothing does not draw it again.
	lastBar  Status
	barDrawn bool
}

// NewConsole returns a Console drawing to out at the terminal's width.
func NewConsole(out io.Writer) *Console {
	terminal := isTerminal(out)
	return &Console{
		theme: NewTheme(),
		out:   out,
		width: terminalWidth(),
		// Both are the same question asked twice: is there a screen to draw
		// on. A file keeps every frame an animation ever drew, so in one it
		// draws none of them.
		clears:   terminal,
		animates: terminal,
	}
}

// MatchOutput adjusts styling to what out can actually show, and honours
// NO_COLOR. Colour is for a screen: redirected into a file or a pipe, the same
// run should read as plain text rather than as a transcript peppered with
// escape codes. It affects pterm globally, so it belongs to the program's
// start-up, not to a package that happens to draw.
func MatchOutput(out *os.File) {
	if _, quiet := os.LookupEnv("NO_COLOR"); quiet || !isTerminal(out) {
		pterm.DisableStyling()
	}
}

// Restore puts back whatever this program turned off on the terminal. It is
// deferred by the program's entry point, so an ordinary exit — or a panic that
// unwinds through it — never leaves a hidden cursor behind.
func (c *Console) Restore() {
	c.showCursor()
	if c.clears {
		// Bracketed paste is a terminal mode; nothing about exiting turns it off.
		_, _ = io.WriteString(c.out, "\x1b[?2004l")
	}
}

// isTerminal reports whether out is something a screen can be cleared on. A
// pipe or a file keeps everything that was written to it, and clearing it
// would only mean writing escape codes into the middle of the text.
func isTerminal(out io.Writer) bool {
	file, ok := out.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// Screen starts a new screen. On a terminal it clears what the previous step
// left behind, so each decision is looked at on its own rather than at the
// bottom of everything that led to it.
func (c *Console) Screen() {
	if c.clears {
		_, _ = io.WriteString(c.out, "\x1b[H\x1b[2J")
	}
	// A screen is also the moment to notice that the window changed size: the
	// next one is about to be drawn, and it may as well be drawn to fit.
	if !c.widthPinned {
		c.width = terminalWidth()
	}
	c.barDrawn = false
}

// SetClearScreens overrides the detection above. Tests use it to render the
// flow either way without needing a real terminal.
func (c *Console) SetClearScreens(clears bool) { c.clears = clears }

// SetAnimated overrides whether the live views redraw in place. Tests use it
// to read what a spinner or a progress bar would have shown.
func (c *Console) SetAnimated(animates bool) { c.animates = animates }

func terminalWidth() int {
	return clamp(pterm.GetTerminalWidth(), MinWidth, MaxWidth)
}

// Theme exposes the palette for the few callers that colour their own text.
func (c *Console) Theme() Theme { return c.theme }

// Width is the column count the chrome is drawn to.
func (c *Console) Width() int { return c.width }

// SetWidth pins the layout to a width. Tests use it to render a stable layout
// no matter what terminal they run under; a pinned width is never replaced by
// a later reading of the real one.
func (c *Console) SetWidth(width int) {
	c.width = clamp(width, MinWidth, MaxWidth)
	c.widthPinned = true
}

// SetStatus replaces the state the status bar reports from now on.
func (c *Console) SetStatus(status Status) { c.status = status }

// SetDestination updates just the download folder the status bar shows.
func (c *Console) SetDestination(destination string) { c.status.Destination = destination }

// Logo prints the product name as block letters shaded across the Klein ramp,
// with the tagline and version beneath it.
func (c *Console) Logo(version string) {
	letters := make(pterm.Letters, 0, len("caxxxd"))
	name := []rune("caxxxd")
	for index, character := range name {
		// The ramp runs across the word so the logo reads as one gradient
		// rather than six separately coloured letters.
		shade := c.theme.Lift.Fade(0, float32(len(name)-1), float32(index), c.theme.Glow)
		letters = append(letters, pterm.Letter{
			String: string(character),
			RGB:    shade,
			Style:  pterm.NewStyle(pterm.FgLightBlue),
		})
	}

	logo, err := pterm.DefaultBigText.WithLetters(letters).Srender()
	if err != nil {
		// Block letters are decoration; the name still has to appear.
		logo = "caxxxd\n"
	}

	c.line("")
	for _, row := range strings.Split(strings.TrimRight(logo, "\n"), "\n") {
		c.line(" " + row)
	}

	subtitle := c.theme.Mist.Sprint(Tagline)
	if version != "" {
		subtitle += c.theme.Slate.Sprint("  ·  " + version)
	}
	c.line(" " + subtitle)
	c.line("")
}

// Step prints the status bar with the step it is about to run. The bar is the
// one solid run of International Klein Blue on the screen.
//
// Several questions can belong to the same step — a quality, then a container,
// then an exact stream — and repeating an identical bar between them would be
// a rule drawn for its own sake, so it is only drawn when it has news.
func (c *Console) Step(number int, title string) {
	c.status.Step = number
	c.status.Title = title

	if c.barDrawn && c.lastBar == c.status {
		return
	}
	c.statusBar()
}

// statusBar prints the current status without advancing the step.
func (c *Console) statusBar() {
	left := " ▌ "
	if c.status.Steps > 0 {
		left += pterm.Sprintf("%d/%d  ", c.status.Step, c.status.Steps)
	}
	left += strings.ToUpper(c.status.Title)

	// Two columns of breathing room between the step and the state, plus the
	// closing mark and the space before it.
	const trailing = 2
	right := c.statusRight(c.width - visibleWidth(left) - trailing - 2)
	if right != "" {
		right += " "
	}
	right += "▐"

	gap := c.width - visibleWidth(left) - visibleWidth(right)
	if gap < 0 {
		left = truncateVisible(left, max(c.width-visibleWidth(right), 0))
		gap = 0
	}

	c.line(c.theme.OnKlein.Sprint(left + strings.Repeat(" ", gap) + right))
	c.line("")

	c.lastBar = c.status
	c.barDrawn = true
}

// statusRight is the live state on the right of the bar, fitted into budget.
//
// The tools are what a step can fail on, so they are the last thing dropped.
// A destination that will not fit is shortened from its front: the end of a
// path is the part that says which folder it is.
func (c *Console) statusRight(budget int) string {
	tools := c.status.Tools
	destination := c.status.Destination
	if destination != "" {
		destination = "⬇ " + destination
	}

	const separator = "  ·  "
	switch {
	case budget <= 0:
		return ""

	case tools == "":
		return truncatePath(destination, budget)

	case destination == "":
		return truncateVisible(tools, budget)

	case visibleWidth(tools)+len(separator)+visibleWidth(destination) <= budget:
		return tools + separator + destination
	}

	// Both are wanted but do not fit: keep the tools whole and give whatever
	// is left to the path, unless what is left is too little to read.
	const minimumPath = 14
	remaining := budget - visibleWidth(tools) - len(separator)
	if remaining >= minimumPath {
		return tools + separator + truncatePath(destination, remaining)
	}
	return truncateVisible(tools, budget)
}

// truncatePath shortens a path to width columns by dropping its front.
//
// The tail is measured in columns and taken rune by rune. Slicing by rune
// count against a column budget reads before the start of the string as soon
// as the path holds anything wider than one column.
func truncatePath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(path) <= width {
		return path
	}
	if width == 1 {
		return "…"
	}

	runes := []rune(pterm.RemoveColorFromString(path))
	budget := width - 1
	kept := len(runes)
	for used := 0; kept > 0; kept-- {
		size := runewidth.RuneWidth(runes[kept-1])
		if used+size > budget {
			break
		}
		used += size
	}
	return "…" + string(runes[kept:])
}

// Panel draws a titled card the full width of the layout.
func (c *Console) Panel(title string, body ...string) {
	c.frame(title, c.theme.Klein, body)
}

// Success, Notice, and Alert are panels that carry the outcome of a step in
// their colour, so a result never has to be read to be recognised.
func (c *Console) Success(title string, body ...string) {
	c.frame("✔ "+title, c.theme.Success, body)
}

func (c *Console) Notice(title string, body ...string) {
	c.frame("• "+title, c.theme.Warning, body)
}

func (c *Console) Alert(title string, body ...string) {
	c.frame("✖ "+title, c.theme.Danger, body)
}

// frame draws the card itself: a rounded border in accent, the title sitting
// in the top edge, and the body padded one column in from each side.
func (c *Console) frame(title string, accent pterm.RGB, body []string) {
	inner := c.width - 4
	rule := strings.Repeat("─", max(inner+2-visibleWidth(title)-3, 0))

	c.line(accent.Sprint("╭─ ") + c.theme.Ink.Sprint(title) + accent.Sprint(" "+rule+"╮"))
	for _, row := range body {
		for _, wrapped := range wrap(row, inner) {
			padding := strings.Repeat(" ", max(inner-visibleWidth(wrapped), 0))
			c.line(accent.Sprint("│") + " " + wrapped + padding + " " + accent.Sprint("│"))
		}
	}
	c.line(accent.Sprint("╰" + strings.Repeat("─", inner+2) + "╯"))
	c.line("")
}

// Fields renders label/value pairs as panel body lines with aligned values.
//
// A value that will not fit is shortened here rather than wrapped, so the
// column stays a column. Paths lose their front, because the end of a path is
// the part that says which file it is.
func (c *Console) Fields(fields []Field) []string {
	label := 0
	for _, field := range fields {
		label = max(label, visibleWidth(field.Label))
	}

	const gutter = 3
	room := c.width - 4 - label - gutter

	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		value := field.Value
		if visibleWidth(value) > room {
			if strings.Contains(value, "/") {
				value = truncatePath(value, room)
			} else {
				value = truncateVisible(value, room)
			}
		}

		padded := field.Label + strings.Repeat(" ", label-visibleWidth(field.Label))
		lines = append(lines, c.theme.Mist.Sprint(padded)+strings.Repeat(" ", gutter)+c.theme.Ink.Sprint(value))
	}
	return lines
}

// Bullets renders a list through pterm's bullet list printer.
func (c *Console) Bullets(items []string) {
	if len(items) == 0 {
		return
	}

	entries := make([]pterm.BulletListItem, 0, len(items))
	for _, item := range items {
		entries = append(entries, pterm.BulletListItem{
			Level:       1,
			Text:        c.theme.Slate.Sprint(truncateVisible(item, c.width-4)),
			TextStyle:   pterm.NewStyle(),
			Bullet:      c.theme.Lift.Sprint("·"),
			BulletStyle: pterm.NewStyle(),
		})
	}

	_ = pterm.DefaultBulletList.WithWriter(c.out).WithItems(entries).Render()
	c.line("")
}

// Text prints a plain paragraph in the body colour.
func (c *Console) Text(text string) {
	for _, row := range wrap(c.theme.Ink.Sprint(text), c.width-2) {
		c.line(" " + row)
	}
}

// Hint prints a quiet aside: something worth knowing, never a decision.
func (c *Console) Hint(text string) {
	c.line(" " + c.theme.Slate.Sprint(truncateVisible(text, c.width-2)))
}

// Accent prints a line in the accent colour, used for step-level headings.
func (c *Console) Accent(text string) {
	c.line(" " + c.theme.Glow.Sprint(text))
}

// Blank prints one empty line.
func (c *Console) Blank() { c.line("") }

func (c *Console) line(text string) {
	_, _ = io.WriteString(c.out, text+c.newline())
}

// newline is what ends a line. Raw mode turns off the translation that makes a
// bare newline return to the first column, so anything drawn while a line
// editor holds the terminal has to carry the carriage return itself — without
// it every following line starts where the last one ended.
func (c *Console) newline() string {
	if c.raw {
		return "\r\n"
	}
	return "\n"
}

// setRaw records that the terminal is in raw mode.
func (c *Console) setRaw(raw bool) { c.raw = raw }

// rewrite redraws a single line in place, without ending it. Trailing spaces
// clear whatever the previous, possibly longer, version of the line left
// behind. Whoever opens a rewritten line owes it a newline before printing
// anything else.
func (c *Console) rewrite(text string) {
	padding := max(c.width-visibleWidth(text)-1, 0)
	_, _ = io.WriteString(c.out, "\r"+text+strings.Repeat(" ", padding))
}

// clearLine erases the line the cursor sits on and returns to its start, so a
// transient line can be replaced by something permanent.
func (c *Console) clearLine() {
	_, _ = io.WriteString(c.out, "\r"+strings.Repeat(" ", c.width)+"\r")
}

// hideCursor and showCursor bracket a line that redraws itself. Without them
// the terminal leaves its cursor parked at the end of the padding, blinking
// away in the middle of a progress bar that is not asking for input.
func (c *Console) hideCursor() {
	if c.clears {
		_, _ = io.WriteString(c.out, "\x1b[?25l")
	}
}

func (c *Console) showCursor() {
	if c.clears {
		_, _ = io.WriteString(c.out, "\x1b[?25h")
	}
}

// visibleWidth is the number of terminal columns a styled string occupies.
func visibleWidth(text string) int {
	return runewidth.StringWidth(pterm.RemoveColorFromString(text))
}

// truncateVisible shortens styled text to width columns. Colour sequences are
// kept, so a truncated string still ends in the reset its style opened with.
func truncateVisible(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(text) <= width {
		return text
	}

	var (
		builder strings.Builder
		used    int
		inCode  bool
	)
	for _, character := range text {
		switch {
		case character == '\x1b':
			inCode = true
		case inCode:
			if character == 'm' {
				inCode = false
			}
		default:
			size := runewidth.RuneWidth(character)
			if used+size > width-1 {
				builder.WriteString("…\x1b[0m")
				return builder.String()
			}
			used += size
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

// wrap breaks styled text into lines of at most width columns, splitting on
// spaces. A single word longer than the line is truncated rather than broken,
// because the things that reach this are titles and paths — and a line wider
// than the panel would push its border off the edge.
func wrap(text string, width int) []string {
	if width <= 0 || visibleWidth(text) <= width {
		return []string{text}
	}

	words := strings.Split(text, " ")
	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if visibleWidth(candidate) > width {
			if current == "" {
				lines = append(lines, truncateVisible(word, width))
				continue
			}
			lines = append(lines, current)
			current = truncateVisible(word, width)
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, truncateVisible(current, width))
	}
	return lines
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
