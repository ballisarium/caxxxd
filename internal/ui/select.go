package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/term"
)

// Select reads keys as a stream, independently of terminal read boundaries.
// Ctrl+C shares the line editor's interruption boundary, so neither a partial
// escape sequence nor several keys arriving together can swallow it.
func (c *Console) Select(in *os.File, prompt string, options []string, initial, height int, filter bool) (int, error) {
	if in == nil || len(options) == 0 {
		return 0, io.EOF
	}
	if term.IsTerminal(int(in.Fd())) {
		state, err := term.MakeRaw(int(in.Fd()))
		if err != nil {
			return 0, err
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

	height = max(1, min(height, len(options)))
	matches := fuzzy.RankFindFold("", options)
	selected, first, lines := clamp(initial, 0, len(options)-1), 0, 0
	query := ""
	pasting := false
	reader := bufio.NewReader(interruptReader{in})
	for {
		first = max(0, min(first, selected))
		if selected >= first+height {
			first = selected - height + 1
		}
		rows := make([]string, 0, height)
		for index := first; index < min(first+height, len(matches)); index++ {
			prefix := "  "
			if index == selected {
				prefix = c.theme.OnKlein.Sprint("▶") + " "
			}
			rows = append(rows, prefix+matches[index].Target)
		}
		heading := prompt + ":"
		if filter {
			heading = prompt + " [type to search]: " + query
		}
		lines = c.drawMenu(in, lines, heading, rows)

		key, err := readMenuKey(reader)
		if err != nil {
			return 0, err
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
			return 0, io.EOF
		case '\r', '\n':
			if len(matches) > 0 {
				choice := matches[selected]
				c.drawMenu(in, lines, prompt+": "+query, []string{"  " + c.theme.OnKlein.Sprint("▶") + " " + choice.Target})
				return choice.OriginalIndex, nil
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
			matches = fuzzy.RankFindFold(query, options)
			if len(matches) != len(options) {
				sort.Sort(matches)
			}
			selected, first = 0, 0
		}
	}
}

// drawMenu keeps each row on one physical terminal line so a redraw erases
// exactly the previous menu, including when a filter leaves no matches.
func (c *Console) drawMenu(in *os.File, previous int, heading string, rows []string) int {
	if previous > 0 && c.clears {
		_, _ = fmt.Fprintf(c.out, "\x1b[%dA\r\x1b[J", previous)
	}
	width := c.Width()
	if columns, _, err := term.GetSize(int(in.Fd())); err == nil && columns > 1 {
		width = min(width, columns-1)
	}
	lines := append(strings.Split(heading, "\n"), rows...)
	for _, line := range lines {
		c.line(truncateVisible(line, width))
	}
	return len(lines)
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
