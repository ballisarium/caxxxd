package ui

import (
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// The line editor only exists on a real terminal: raw mode, escape sequences,
// bracketed paste. A buffer cannot exercise any of that, so these tests give
// it an actual pty and type into it.

// editorRun is one ReadLine running against a pty, with the keyboard on one
// end and everything it drew collected at the other.
type editorRun struct {
	t      *testing.T
	master *os.File
	result chan lineResult

	mutex sync.Mutex
	drawn strings.Builder
}

type lineResult struct {
	line     string
	err      error
	restored bool
}

func startReadLine(t *testing.T, prompt, initial string) *editorRun {
	t.Helper()
	return startTerminalPrompt(t, func(console *Console, in *os.File) (string, error) {
		return console.ReadLine(in, prompt, initial)
	})
}

func startTerminalPrompt(t *testing.T, prompt func(*Console, *os.File) (string, error)) *editorRun {
	t.Helper()

	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	// A freshly opened pty reports no size at all, which is not what a
	// terminal someone is typing into looks like.
	if err := pty.Setsize(master, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatalf("size the terminal: %v", err)
	}

	console := NewConsole(slave)
	console.SetWidth(80)
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}

	run := &editorRun{t: t, master: master, result: make(chan lineResult, 1)}

	// The pty has a finite buffer, so what the editor draws has to be read as
	// it is written or the editor would block against a full pipe.
	go func() {
		buffer := make([]byte, 4096)
		for {
			read, err := master.Read(buffer)
			if read > 0 {
				run.mutex.Lock()
				run.drawn.Write(buffer[:read])
				run.mutex.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		line, err := prompt(console, slave)
		after, stateErr := term.GetState(int(slave.Fd()))
		run.result <- lineResult{line: line, err: err, restored: stateErr == nil && reflect.DeepEqual(before, after)}
		_ = slave.Close()
	}()

	t.Cleanup(func() { _ = master.Close() })
	return run
}

// output is everything the editor has drawn so far.
func (r *editorRun) output() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.drawn.String()
}

// waitFor blocks until the editor has drawn text, so keys are only sent once
// there is something listening for them.
func (r *editorRun) waitFor(text string) {
	r.t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.output(), text) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("the editor never drew %q, it drew:\n%q", text, r.output())
}

func (r *editorRun) send(keys string) {
	r.t.Helper()
	if _, err := io.WriteString(r.master, keys); err != nil {
		r.t.Fatalf("write to the terminal: %v", err)
	}
}

// finish presses Enter and reports what ReadLine returned.
func (r *editorRun) finish() (string, error) {
	r.t.Helper()
	r.send("\r")

	select {
	case result := <-r.result:
		if !result.restored {
			r.t.Fatal("prompt did not restore terminal settings")
		}
		return result.line, result.err
	case <-time.After(5 * time.Second):
		r.t.Fatal("ReadLine never returned")
		return "", nil
	}
}

func TestTypedTextIsReturned(t *testing.T) {
	run := startReadLine(t, "▸ Paste a media URL: ", "")
	run.waitFor("Paste a media URL")

	run.send("https://example.test/clip")
	run.waitFor("example.test/clip")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if line != "https://example.test/clip" {
		t.Fatalf("ReadLine() = %q", line)
	}
}

func TestAPastedLineIsAnAnswerAndNotAnError(t *testing.T) {
	// x/term reports a line that arrived entirely as a paste by returning
	// ErrPasteIndicator alongside the text. Pasting a URL is how caxxxd is
	// normally used, so treating that as a failure would break the app for
	// its most ordinary input.
	run := startReadLine(t, "▸ Paste a media URL: ", "")
	run.waitFor("Paste a media URL")

	run.send("\x1b[200~https://www.youtube.com/watch?v=abc123\x1b[201~")
	run.waitFor("watch?v=abc123")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("a pasted line came back as an error: %v", err)
	}
	if line != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("ReadLine() = %q", line)
	}
}

func TestThePrefillIsEditable(t *testing.T) {
	run := startReadLine(t, "▸ Download folder: ", "~/Downloads/caxxxd")
	run.waitFor("~/Downloads/caxxxd")

	// The cursor sits after the prefill, so backspaces eat into it.
	run.send(strings.Repeat("\x7f", len("caxxxd")))
	run.send("Movies")
	run.waitFor("Movies")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if line != "~/Downloads/Movies" {
		t.Fatalf("ReadLine() = %q, want the edited prefill", line)
	}
}

func TestCtrlWDeletesBackToWhitespace(t *testing.T) {
	// A word here is what a terminal means by one: text back to the previous
	// space. On a path without spaces that is the whole path, which is worth
	// knowing rather than guessing at.
	run := startReadLine(t, "▸ Paste a media URL: ", "one two three")
	run.waitFor("one two three")

	run.send("\x17")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if line != "one two " {
		t.Fatalf("ReadLine() = %q, want the last word gone", line)
	}
}

func TestCtrlUClearsTheWholeLine(t *testing.T) {
	run := startReadLine(t, "▸ Paste a media URL: ", "https://example.test/the-wrong-one")
	run.waitFor("the-wrong-one")

	run.send("\x15")
	run.send("https://example.test/right")
	run.waitFor("example.test/right")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if line != "https://example.test/right" {
		t.Fatalf("ReadLine() = %q, want only what was typed after Ctrl+U", line)
	}
}

func TestEnterOnAPrefillAcceptsIt(t *testing.T) {
	run := startReadLine(t, "▸ Download folder: ", "~/Movies")
	run.waitFor("~/Movies")

	line, err := run.finish()
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if line != "~/Movies" {
		t.Fatalf("ReadLine() = %q", line)
	}
}

func TestCtrlCEndsTheEditor(t *testing.T) {
	run := startReadLine(t, "▸ Paste a media URL: ", "")
	run.waitFor("Paste a media URL")

	run.send("\x03")

	select {
	case result := <-run.result:
		// io.EOF is what the caller turns into "the user left".
		if !result.restored {
			t.Fatal("Ctrl+C did not restore terminal settings")
		}
		if !errors.Is(result.err, io.EOF) {
			t.Fatalf("Ctrl+C returned %v, want io.EOF", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C did not end the editor")
	}
}

func TestCtrlCAfterEscapeEndsTheEditor(t *testing.T) {
	run := startReadLine(t, "▸ Paste a media URL: ", "")
	run.waitFor("Paste a media URL")

	// Escape leaves the key decoder waiting for more bytes. Ctrl+C must
	// still interrupt, even when it arrives in the same terminal read.
	run.send("\x1b\x03")
	select {
	case result := <-run.result:
		if !result.restored {
			t.Fatal("Ctrl+C after Escape did not restore terminal settings")
		}
		if !errors.Is(result.err, io.EOF) {
			t.Fatalf("Ctrl+C after Escape returned %v, want io.EOF", result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ctrl+C after Escape did not end the editor")
	}
}

func TestBracketedPasteIsTurnedOffOnTheWayOut(t *testing.T) {
	// It is a mode set on the terminal, not on the file descriptor, so
	// restoring the descriptor does not turn it off. Left on, it follows
	// caxxxd out into the shell.
	run := startReadLine(t, "▸ Paste a media URL: ", "")
	run.waitFor("Paste a media URL")

	if _, err := run.finish(); err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(run.output(), "\x1b[?2004l") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("bracketed paste was left enabled:\n%q", run.output())
}
