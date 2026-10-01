package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/term"
)

// MenuItem is one selectable entry in a menu that may update while open.
type MenuItem struct {
	ID     string
	Label  string
	Detail string
}

// SelectItems keeps complete descriptions separate from the compact list rows.
func (c *Console) SelectItems(in *os.File, prompt string, items []MenuItem, initialID string, height int, filter bool) (string, error) {
	if in == nil || len(items) == 0 {
		return "", io.EOF
	}
	return c.selectMenu(in, prompt, func() ([]MenuItem, error) { return items, nil }, initialID, height, filter, false)
}

// Select reads keys as a stream, independently of terminal read boundaries.
// Ctrl+C shares the line editor's interruption boundary, so neither a partial
// escape sequence nor several keys arriving together can swallow it.
func (c *Console) Select(in *os.File, prompt string, options []string, initial, height int, filter bool) (int, error) {
	if in == nil || len(options) == 0 {
		return 0, io.EOF
	}
	items := make([]MenuItem, len(options))
	for index, option := range options {
		items[index] = MenuItem{ID: strconv.Itoa(index), Label: option}
	}
	initial = clamp(initial, 0, len(options)-1)
	selected, err := c.selectMenu(in, prompt, func() ([]MenuItem, error) {
		return items, nil
	}, strconv.Itoa(initial), height, filter, false)
	if err != nil {
		return 0, err
	}
	index, err := strconv.Atoi(selected)
	if err != nil {
		return 0, err
	}
	return index, nil
}

// SelectLive redraws a menu as items change, retaining the selected item by
// ID when the provider reorders or replaces entries. The provider is checked
// while waiting for input, so returning from this method leaves no input
// reader running in the background.
func (c *Console) SelectLive(in *os.File, prompt string, items func() ([]MenuItem, error), initialID string, height int, filter bool) (string, error) {
	if in == nil || items == nil {
		return "", io.EOF
	}
	return c.selectMenu(in, prompt, items, initialID, height, filter, true)
}

func (c *Console) selectMenu(in *os.File, prompt string, provideItems func() ([]MenuItem, error), initialID string, height int, filter, live bool) (string, error) {
	items, err := provideItems()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", io.EOF
	}
	items = append([]MenuItem(nil), items...)
	if term.IsTerminal(int(in.Fd())) {
		state, err := term.MakeRaw(int(in.Fd()))
		if err != nil {
			return "", err
		}
		c.setRaw(true)
		defer func() {
			_ = term.Restore(int(in.Fd()), state)
			c.setRaw(false)
		}()
	}
	c.hideCursor()
	defer c.showCursor()
	if c.clears {
		_, _ = io.WriteString(c.out, "\x1b[?2004h")
		defer io.WriteString(c.out, "\x1b[?2004l")
	}

	height = max(1, height)
	matches := rankMenuItems("", items)
	selected, first, lines := menuIndexForID(matches, items, initialID, 0), 0, 0
	query := ""
	pasting := false
	reader := bufio.NewReader(newPromptReader(in))
	needsDraw := true
	for {
		if needsDraw {
			description := []string{}
			if len(matches) > 0 {
				item := items[matches[selected].OriginalIndex]
				if item.Detail != "" && !strings.Contains(item.Label, item.Detail) {
					wrapped := wrap(item.Detail, max(c.menuWidth(in)-2, 1))
					description = append(description, "")
					for i := 0; i < min(len(wrapped), 3); i++ {
						line := wrapped[i]
						if i == 2 && len(wrapped) > 3 {
							line = Truncate(line, c.menuWidth(in)-4) + " …"
						}
						description = append(description, c.theme.Mist.Sprint("  "+line))
					}
				}
			}
			visibleHeight := min(height, max(len(matches), 1))
			if _, terminalRows, err := term.GetSize(int(in.Fd())); err == nil && terminalRows > 0 && c.clears {
				// Reserve the heading, description, controls and the final cursor
				// line so redrawing a menu cannot scroll away the summary above it.
				bodyRows := c.screenRows - lines
				room := terminalRows - bodyRows - len(strings.Split(prompt, "\n")) - len(description) - 5
				visibleHeight = min(visibleHeight, max(room, 1))
			}
			first = clamp(first, 0, max(len(matches)-visibleHeight, 0))
			if selected < first {
				first = selected
			}
			if selected >= first+visibleHeight {
				first = selected - visibleHeight + 1
			}
			rows := make([]string, 0, visibleHeight)
			for index := first; index < min(first+visibleHeight, len(matches)); index++ {
				label := items[matches[index].OriginalIndex].Label
				if index == selected {
					width := c.menuWidth(in)
					row := Truncate("▶ "+label, width)
					rows = append(rows, c.theme.OnKlein.Sprint(Pad(row, width)))
				} else {
					rows = append(rows, "  "+label)
				}
			}
			heading := prompt + ":"
			if filter && query != "" {
				heading += " " + query
			}
			if len(matches) == 0 {
				rows = append(rows, c.theme.Mist.Sprint("  No matches. Clear the filter."))
			}
			rows = append(rows, description...)
			rows = append(rows, "", c.theme.Slate.Sprint("  ↑↓ Move  Enter Select"), c.theme.Slate.Sprint("  Esc Back  Ctrl+C Exit"))
			position := fmt.Sprintf("  %d/%d", selected+1, len(matches))
			if len(matches) == 0 {
				position = "  0 matches"
			}
			if filter {
				if query == "" {
					position += " · Type to filter"
				} else {
					position += " · Ctrl+U Clear filter"
				}
			}
			rows = append(rows, c.theme.Slate.Sprint(position))
			lines = c.drawMenu(in, lines, heading, rows)
			needsDraw = false
		}

		if live {
			ready, err := waitMenuInput(in, reader)
			if err != nil {
				return "", err
			}
			if !ready {
				updated, err := provideItems()
				if err != nil {
					return "", err
				}
				updated = append([]MenuItem(nil), updated...)
				if !slices.Equal(items, updated) {
					selectedID := ""
					if len(matches) > 0 {
						selectedID = items[matches[selected].OriginalIndex].ID
					}
					items = updated
					matches = rankMenuItems(query, items)
					selected = menuIndexForID(matches, items, selectedID, selected)
					needsDraw = true
				}
				continue
			}
		}

		key, err := readMenuKey(reader)
		if err != nil {
			return "", err
		}
		if key == menuPasteStart || key == menuPasteEnd {
			pasting = key == menuPasteStart
			continue
		}
		if pasting && !unicode.IsPrint(key) {
			continue
		}
		previous := query
		switch key {
		case '\x04':
			return "", io.EOF
		case '\r', '\n':
			if len(matches) > 0 {
				choice := matches[selected]
				c.drawMenu(in, lines, prompt+": "+query, []string{"  " + c.theme.OnKlein.Sprint("▶") + " " + items[choice.OriginalIndex].Label})
				return items[choice.OriginalIndex].ID, nil
			}
		case menuUp, '\x10':
			if len(matches) > 0 {
				selected = (selected + len(matches) - 1) % len(matches)
			}
		case menuDown, '\x0e':
			if len(matches) > 0 {
				selected = (selected + 1) % len(matches)
			}
		case '\x7f', '\b':
			if query != "" {
				query = string([]rune(query)[:len([]rune(query))-1])
			}
		case '\x15':
			query = ""
		default:
			if filter && unicode.IsPrint(key) {
				query += string(key)
			}
		}
		if query != previous {
			matches = rankMenuItems(query, items)
			selected, first = 0, 0
		}
		needsDraw = true
	}
}

func rankMenuItems(query string, items []MenuItem) fuzzy.Ranks {
	labels := make([]string, len(items))
	for index, item := range items {
		labels[index] = item.Label + " " + item.Detail
	}
	matches := fuzzy.RankFindFold(query, labels)
	if len(matches) != len(items) {
		sort.Sort(matches)
	}
	return matches
}

func menuIndexForID(matches fuzzy.Ranks, items []MenuItem, id string, fallback int) int {
	if len(matches) == 0 {
		return 0
	}
	for index, match := range matches {
		if items[match.OriginalIndex].ID == id {
			return index
		}
	}
	return clamp(fallback, 0, len(matches)-1)
}

// waitMenuInput bounds a live menu's idle wait without adding a reader
// goroutine that could continue consuming terminal input after return.
func waitMenuInput(in *os.File, reader *bufio.Reader) (bool, error) {
	return waitPromptInput(in, reader, 250)
}

// drawMenu keeps each row on one physical terminal line so a redraw erases
// exactly the previous menu, including when a filter leaves no matches.
func (c *Console) drawMenu(in *os.File, previous int, heading string, rows []string) int {
	if previous > 0 && c.clears {
		_, _ = fmt.Fprintf(c.out, "\x1b[%dA\r\x1b[J", previous)
		c.screenRows -= previous
	}
	width := c.menuWidth(in)
	lines := append(strings.Split(heading, "\n"), rows...)
	for _, line := range lines {
		c.line(truncateVisible(line, width))
	}
	return len(lines)
}

func (c *Console) menuWidth(in *os.File) int {
	width := c.Width()
	if columns, _, err := term.GetSize(int(in.Fd())); err == nil && columns > 1 {
		width = min(width, columns-1)
	}
	return width
}

const (
	menuUp rune = -1 - iota
	menuDown
	menuPasteStart
	menuPasteEnd
	menuIgnore
)

func readMenuKey(reader *bufio.Reader) (rune, error) {
	key, _, err := reader.ReadRune()
	if err != nil || key != '\x1b' {
		return key, err
	}
	key, _, err = reader.ReadRune()
	if err != nil || (key != '[' && key != 'O') {
		return menuIgnore, err
	}
	// CSI / SS3 keys may arrive one byte at a time. A bounded sequence also
	// keeps malformed input from growing an unbounded buffer.
	var sequence strings.Builder
	for range 32 {
		key, _, err = reader.ReadRune()
		if err != nil {
			return menuIgnore, err
		}
		sequence.WriteRune(key)
		if key >= '@' && key <= '~' {
			switch key {
			case 'A':
				return menuUp, nil
			case 'B':
				return menuDown, nil
			}
			switch sequence.String() {
			case "200~":
				return menuPasteStart, nil
			case "201~":
				return menuPasteEnd, nil
			}
			break
		}
	}
	return menuIgnore, nil
}
