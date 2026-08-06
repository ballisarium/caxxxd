package ui

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// ReadLine asks for one line and lets the terminal's own editing keys work on
// it: Ctrl+U to clear the line, Ctrl+W to drop a word, Ctrl+A and Ctrl+E to
// reach its ends, the arrows to move inside it. A URL is long enough that
// having to hold Backspace through it is a real cost.
//
// initial prefills the field by arriving as if it had been typed, so the
// cursor lands after it and every editing key applies to it too.
//
// Paste arrives in one piece rather than character by character, which is what
// keeps a pasted URL from being read as a line of separate keystrokes.
func (c *Console) ReadLine(in *os.File, prompt, initial string) (string, error) {
	// Editing in place needs both halves of the terminal: keys to read, and a
	// screen to draw the line back onto. With either one redirected, the
	// escape codes would go into a file and the user would be typing blind.
	if in == nil || !c.clears || !term.IsTerminal(int(in.Fd())) {
		return readPlainLine(c, in, prompt, initial)
	}

	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return readPlainLine(c, in, prompt, initial)
	}
	c.setRaw(true)
	defer func() {
		_ = term.Restore(int(in.Fd()), state)
		c.setRaw(false)
	}()

	// Said here rather than by the caller: these keys exist because the editor
	// below is running, and only while it is.
	c.Hint("Ctrl+U clears the line, Ctrl+W the word before the cursor.")

	editor := term.NewTerminal(readWriter{
		// The prefill is fed in ahead of the keyboard, which is what makes it
		// an editable answer rather than a label the user has to retype.
		reader: io.MultiReader(strings.NewReader(initial), in),
		writer: c.out,
	}, prompt)

	editor.SetBracketedPasteMode(true)
	// Bracketed paste is a mode set on the terminal, not on the file
	// descriptor, so restoring the descriptor does not turn it off. Left on,
	// it would follow caxxxd out and into the shell.
	defer editor.SetBracketedPasteMode(false)

	stopResize := watchResize(in, editor)
	defer stopResize()

	line, err := editor.ReadLine()
	if errors.Is(err, term.ErrPasteIndicator) {
		// A line that was pasted whole comes back with this alongside the text
		// itself. Pasting a URL is the ordinary way to use caxxxd: it is the
		// most expected answer there is, not a failure.
		return line, nil
	}
	return line, err
}

// watchResize keeps the editor's idea of the terminal in step with the window
// while a line is being typed. Without it a resize mid-URL leaves the editor
// wrapping and placing the cursor by the old width.
func watchResize(in *os.File, editor *term.Terminal) func() {
	resize := func() {
		width, height, err := term.GetSize(int(in.Fd()))
		if err != nil || width <= 0 || height <= 0 {
			// A terminal that does not report its size keeps the editor's own
			// default. Handing it a zero would make it wrap after every single
			// character, which is worse than a guess that is merely wrong.
			return
		}
		_ = editor.SetSize(width, height)
	}
	resize()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			case <-signals:
				resize()
			}
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
		<-finished
	}
}

// readPlainLine is the fallback for input that cannot be edited in place. The
// prompt is still shown, because something has to say what is being asked.
func readPlainLine(c *Console, in *os.File, prompt, initial string) (string, error) {
	_, _ = io.WriteString(c.out, prompt)
	if in == nil {
		c.line("")
		return initial, nil
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	switch {
	case err != nil && line == "":
		return "", err
	case line == "":
		return initial, nil
	default:
		return line, nil
	}
}

// readWriter joins the keyboard and the screen into the one stream a line
// editor needs.
type readWriter struct {
	reader io.Reader
	writer io.Writer
}

func (rw readWriter) Read(p []byte) (int, error)  { return rw.reader.Read(p) }
func (rw readWriter) Write(p []byte) (int, error) { return rw.writer.Write(p) }
