package ytdlp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ballisarium/caxxxd/internal/process"
)

// eventBuffer keeps the output writers from blocking behind a slow UI frame.
const eventBuffer = 64

// maxLineBytes caps how much output is buffered while waiting for a newline.
const maxLineBytes = 1 << 20

// escalationGrace is how long the process group gets to exit after SIGTERM
// before caxxxd escalates to SIGKILL. Well-behaved children stop instantly;
// this only matters for one that ignores SIGTERM.
const escalationGrace = time.Second

// waitGrace is the final backstop: after it, the runtime closes the command's
// pipes so a surviving grandchild holding them cannot stall Wait forever.
const waitGrace = 5 * time.Second

// RunEvent is one thing that happened while yt-dlp ran: a structured event, a
// diagnostic line, or the terminal result.
type RunEvent struct {
	Parsed Event
	Log    string
	Err    error
	Done   bool
}

// Downloader runs yt-dlp and streams its structured output.
type Downloader struct {
	Binary string
}

// Start launches yt-dlp with args and returns a channel of events. The channel
// always ends with exactly one event where Done is true, then closes.
// Cancelling ctx terminates the whole process group, including ffmpeg.
func (d Downloader) Start(ctx context.Context, args []string) (<-chan RunEvent, error) {
	binary := d.Binary
	if binary == "" {
		binary = "yt-dlp"
	}

	// Both streams are scanned for markers: yt-dlp writes download progress to
	// stdout but post-processing progress to stderr. Anything without a marker
	// stays a diagnostic line either way.
	events := make(chan RunEvent, eventBuffer)
	stdout := &lineWriter{events: events, parse: true}
	stderr := &lineWriter{events: events, parse: true}

	finished := make(chan struct{})

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	process.ConfigureGroup(cmd)
	cmd.Cancel = func() error {
		err := process.TerminateGroup(cmd)
		go escalate(cmd, finished)
		return err
	}
	cmd.WaitDelay = waitGrace

	if err := cmd.Start(); err != nil {
		close(events)
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}

	go func() {
		defer close(events)

		// Wait joins the goroutines copying stdout and stderr, so no further
		// writes can happen once it returns.
		err := cmd.Wait()
		close(finished)
		stdout.flush()
		stderr.flush()

		if ctxErr := ctx.Err(); ctxErr != nil {
			// A cancelled download is a user decision, not a tool failure.
			err = ctxErr
		}
		events <- RunEvent{Done: true, Err: err}
	}()

	return events, nil
}

// escalate SIGKILLs the process group if it is still alive after the grace
// period. It returns as soon as the command has been reaped, so the signal is
// never sent to a group id that could already have been recycled.
func escalate(cmd *exec.Cmd, finished <-chan struct{}) {
	timer := time.NewTimer(escalationGrace)
	defer timer.Stop()

	select {
	case <-finished:
	case <-timer.C:
		_ = process.KillGroup(cmd)
	}
}

// lineWriter splits process output into lines and turns each one into an event.
// Lines that carry a caxxxd marker become parsed events; the rest are kept as
// diagnostics.
type lineWriter struct {
	events chan<- RunEvent
	parse  bool
	buffer []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buffer = append(w.buffer, p...)
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		w.emit(string(w.buffer[:index]))
		w.buffer = w.buffer[index+1:]
	}

	// A stream that never emits a newline must not grow without bound.
	if len(w.buffer) > maxLineBytes {
		w.flush()
	}
	return len(p), nil
}

// flush emits whatever is left after the process stops writing.
func (w *lineWriter) flush() {
	if len(w.buffer) == 0 {
		return
	}
	w.emit(string(w.buffer))
	w.buffer = nil
}

func (w *lineWriter) emit(line string) {
	line = strings.TrimRight(line, "\r")
	if w.parse {
		if parsed, ok := ParseOutputLine(line); ok {
			w.events <- RunEvent{Parsed: parsed}
			return
		}
	}
	// A diagnostic line is the one thing here written to be read by a person,
	// which is exactly why it must not be able to redraw the screen it lands on.
	if trimmed := SafeDiagnostic(line); trimmed != "" {
		w.events <- RunEvent{Log: trimmed}
	}
}
