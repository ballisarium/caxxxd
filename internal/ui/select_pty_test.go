package ui

import (
	"os"
	"strconv"
	"testing"
)

func TestMenuReadsFragmentedAndBatchedNavigation(t *testing.T) {
	options := make([]string, 14)
	for index := range options {
		options[index] = "Track " + strconv.Itoa(index)
	}
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		selected, err := console.Select(in, "Choose a track", options, 0, 12, true)
		return strconv.Itoa(selected), err
	})
	run.waitFor("Choose a track")
	// A split arrow wraps up to the last row and scrolls it into view.
	run.send("\x1b")
	run.send("[")
	run.send("A")
	run.waitFor("Track 13")
	// Several arrows in one read must remain separate keys.
	run.send("\x1b[B\x1b[B")
	selected, err := run.finish()
	if err != nil || selected != "1" {
		t.Fatalf("menu returned (%q, %v), want Track 1", selected, err)
	}
}

func TestMenuSearchPreservesSelectionAndPaste(t *testing.T) {
	options := []string{"one", "two", "three", "four", "five", "второй поток", "seven", "eight", "nine"}
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		selected, err := console.Select(in, "Choose a track", options, 8, 12, true)
		return strconv.Itoa(selected), err
	})
	run.waitFor("Choose a track")
	run.send("no match\r")
	run.waitFor("no match")
	// Enter with no matches stays in the menu. Clear and paste a Cyrillic
	// search with a newline, which must not select until Enter is pressed.
	run.send("\x15\x1b[200~ВТОРОЙ\r\n\x1b[201~")
	run.waitFor("ВТОРОЙ")
	select {
	case <-run.result:
		t.Fatal("a pasted newline selected a menu option")
	default:
	}
	selected, err := run.finish()
	if err != nil || selected != "5" {
		t.Fatalf("menu returned (%q, %v), want the matching option's original index", selected, err)
	}
}
