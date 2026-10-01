package ui

import (
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestLiveMenuPreservesSelectedIDWhenItemsMove(t *testing.T) {
	items := []MenuItem{
		{ID: "one", Label: "One"},
		{ID: "two", Label: "Two"},
		{ID: "three", Label: "Three"},
	}
	var itemsMu sync.Mutex
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		selected, err := console.SelectLive(in, "Choose a track", func() ([]MenuItem, error) {
			itemsMu.Lock()
			defer itemsMu.Unlock()
			return append([]MenuItem(nil), items...), nil
		}, "two", 4, false)
		return selected, err
	})
	run.waitFor("Choose a track")

	output := run.output()
	run.send("\x1b[B")
	output = waitForOutputGrowth(t, run, len(output))

	itemsMu.Lock()
	items = []MenuItem{
		{ID: "three", Label: "Three"},
		{ID: "one", Label: "One"},
		{ID: "two", Label: "Two"},
	}
	itemsMu.Unlock()
	output = waitForOutputGrowth(t, run, len(output))

	// The selected row is now first after the reorder. Moving down should pick
	// One; keeping the old numeric position would wrap back to Three.
	run.send("\x1b[B")
	_ = waitForOutputGrowth(t, run, len(output))
	selected, err := run.finish()
	if err != nil {
		t.Fatalf("SelectLive() returned %q, %v", selected, err)
	}
	if selected != "one" {
		t.Fatalf("SelectLive() returned %q, want stable ID one", selected)
	}
}

func waitForOutputGrowth(t *testing.T, run *editorRun, previous int) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		output := run.output()
		if len(output) > previous {
			return output
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the menu did not redraw after its state changed; output was:\n%q", run.output())
	return ""
}

func TestLiveMenuReturnsEOFAndRestoresTTYOnInterrupt(t *testing.T) {
	run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		return console.SelectLive(in, "Choose a track", func() ([]MenuItem, error) {
			return []MenuItem{{ID: "one", Label: "One"}}, nil
		}, "one", 4, false)
	})
	run.waitFor("Choose a track")
	run.send("\x1b")
	time.Sleep(50 * time.Millisecond)
	run.send("\x03")

	select {
	case result := <-run.result:
		if !result.restored {
			t.Fatal("SelectLive() did not restore terminal settings")
		}
		if result.err != io.EOF {
			t.Fatalf("SelectLive() returned %v, want io.EOF", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C after a partial escape did not end the menu")
	}
}
