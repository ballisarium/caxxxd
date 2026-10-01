package ui

import (
	"os"
	"strconv"
	"strings"
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

func TestMenuDescriptionRemainsReadableAndSearchable(t *testing.T) {
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		console.SetWidth(60)
		return console.SelectItems(in, "Captured media", []MenuItem{
			{ID: "video", Label: "MP4 · 512 MiB", Detail: "1080p · 2:00 · Original player on media.example.test · avc1,mp4a"},
			{ID: "audio", Label: "M4A · 12 MiB", Detail: "Audio only · Второй плеер"},
		}, "video", 8, true)
	})
	run.waitFor("Original player")
	run.waitFor("Ctrl+C")
	run.send("does not exist")
	run.waitFor("No matches")
	run.send("\x15ВТОРОЙ")
	run.waitFor("Второй плеер")
	selected, err := run.finish()
	if err != nil || selected != "audio" {
		t.Fatalf("description search returned (%q, %v), want audio", selected, err)
	}
}

func TestMenuKeepsSummaryAndControlsWithinTheTerminalHeight(t *testing.T) {
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		console.Screen()
		console.Panel("Ready to download", "Title", "Mode", "Quality", "Container", "Range", "Size", "Destination", "Source")
		items := make([]MenuItem, 10)
		for i := range items {
			items[i] = MenuItem{ID: strconv.Itoa(i), Label: "Track " + strconv.Itoa(i), Detail: "A captured stream"}
		}
		return console.SelectItems(in, "Streams", items, "0", 8, false)
	})
	run.waitFor("1/10")
	if lines := strings.Count(run.output(), "\n"); lines > 23 {
		t.Fatalf("menu uses %d lines before its cursor in a 24-line terminal", lines)
	}
	run.send("\x1b[A")
	run.waitFor("Track 9")
	selected, err := run.finish()
	if err != nil || selected != "9" {
		t.Fatalf("scrolled menu returned (%q, %v), want the last item", selected, err)
	}
}
