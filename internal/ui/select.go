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
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// MenuItem is one selectable entry in a menu that may update while open.
type MenuItem struct {
	ID    string
	Label string
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
	reader := bufio.NewReader(interruptReader{in})
	needsDraw := true
	for {
		if needsDraw {
			visibleHeight := min(height, max(len(matches), 1))
			first = clamp(first, 0, max(len(matches)-visibleHeight, 0))
			if selected < first {
				first = selected
			}
			if selected >= first+visibleHeight {
				first = selected - visibleHeight + 1
			}
			rows := make([]string, 0, visibleHeight)
			for index := first; index < min(first+visibleHeight, len(matches)); index++ {
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
				c.drawMenu(in, lines, prompt+": "+query, []string{"  " + c.theme.OnKlein.Sprint("▶") + " " + choice.Target})
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
		labels[index] = item.Label
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
	if reader.Buffered() > 0 {
		return true, nil
	}
	fds := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}}
	for {
		ready, err := unix.Poll(fds, 250)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		if ready == 0 {
			return false, nil
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return false, os.ErrClosed
		}
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			return true, nil
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
