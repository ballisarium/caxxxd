package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEscapeReturnsFromInteractivePrompts(t *testing.T) {
	for _, kind := range []string{"text", "menu", "live menu"} {
		t.Run(kind, func(t *testing.T) {
			run := startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
				if kind == "text" {
					return console.ReadLine(in, "Prompt: ", "draft")
				}
				items := []MenuItem{{ID: "one", Label: "One"}}
				if kind == "live menu" {
					return console.SelectLive(in, "Prompt", func() ([]MenuItem, error) { return items, nil }, "one", 8, false)
				}
				return console.SelectItems(in, "Prompt", items, "one", 8, false)
			})
			run.waitFor("Prompt")
			run.send("\x1b")
			select {
			case result := <-run.result:
				if !result.restored || !errors.Is(result.err, ErrBack) {
					t.Fatalf("Esc returned %v, restored=%v; want Back and restored TTY", result.err, result.restored)
				}
			case <-time.After(750 * time.Millisecond):
				run.send("\x03")
				<-run.result
				t.Fatal("Esc did not return from the prompt")
			}
		})
	}
}

func TestPastedEscapeDoesNotNavigateBack(t *testing.T) {
	run := startReadLine(t, "Prompt: ", "")
	run.waitFor("Prompt")
	run.send("\x1b[200~before \x1b after\x1b[201~")
	run.waitFor("before")
	line, err := run.finish()
	if err != nil || !strings.HasPrefix(line, "before") {
		t.Fatalf("pasted escape canceled the line: (%q, %v)", line, err)
	}
}
