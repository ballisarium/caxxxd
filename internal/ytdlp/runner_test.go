package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// fakeBinary writes an executable script and returns its path.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

// drain collects every event until the channel closes.
func drain(t *testing.T, events <-chan ytdlp.RunEvent, timeout time.Duration) []ytdlp.RunEvent {
	t.Helper()
	var collected []ytdlp.RunEvent
	deadline := time.After(timeout)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return collected
			}
			collected = append(collected, event)
		case <-deadline:
			t.Fatalf("timed out after %s with %d events", timeout, len(collected))
		}
	}
}

func TestDownloaderStreamsStructuredEvents(t *testing.T) {
	binary := fakeBinary(t, `#!/bin/sh
printf '[youtube] abc123: Downloading webpage\n'
printf '__CAXXXD_PROGRESS__downloading\t50\t100\tNA\t25\t2\n'
printf '__CAXXXD_POSTPROCESS__started\n'
printf '__CAXXXD_FILE__/tmp/output.mkv\n'
printf 'WARNING: something cosmetic\n' >&2
`)

	events, err := ytdlp.Downloader{Binary: binary}.Start(context.Background(), []string{"--no-playlist"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var kinds []ytdlp.EventKind
	var logs []string
	var terminal *ytdlp.RunEvent
	for _, event := range drain(t, events, 10*time.Second) {
		switch {
		case event.Done:
			terminal = &event
		case event.Log != "":
			logs = append(logs, event.Log)
		default:
			kinds = append(kinds, event.Parsed.Kind)
			switch event.Parsed.Kind {
			case ytdlp.EventProgress:
				if event.Parsed.Progress.DownloadedBytes != 50 || event.Parsed.Progress.TotalBytes != 100 {
					t.Fatalf("progress = %#v", event.Parsed.Progress)
				}
			case ytdlp.EventCompletedFile:
				if event.Parsed.FilePath != "/tmp/output.mkv" {
					t.Fatalf("FilePath = %q", event.Parsed.FilePath)
				}
			}
		}
	}

	want := []ytdlp.EventKind{ytdlp.EventProgress, ytdlp.EventPostProcess, ytdlp.EventCompletedFile}
	if len(kinds) != len(want) {
		t.Fatalf("parsed kinds = %v, want %v", kinds, want)
	}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("parsed kinds = %v, want %v", kinds, want)
		}
	}

	if len(logs) != 2 {
		t.Fatalf("logs = %q, want the webpage line and the warning", logs)
	}
	if terminal == nil {
		t.Fatal("stream ended without a terminal event")
	}
	if terminal.Err != nil {
		t.Fatalf("terminal error = %v, want nil", terminal.Err)
	}
}

func TestDownloaderParsesMarkersOnStderr(t *testing.T) {
	// yt-dlp reports post-processing progress on stderr, so markers have to be
	// recognized there too, not just on stdout.
	binary := fakeBinary(t, `#!/bin/sh
printf '__CAXXXD_POSTPROCESS__started\n' >&2
printf '__CAXXXD_POSTPROCESS__finished\n' >&2
printf 'WARNING: cosmetic\n' >&2
`)

	events, err := ytdlp.Downloader{Binary: binary}.Start(context.Background(), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	stages := 0
	logs := 0
	for _, event := range drain(t, events, 10*time.Second) {
		switch {
		case event.Done:
		case event.Log != "":
			logs++
		case event.Parsed.Kind == ytdlp.EventPostProcess:
			stages++
		}
	}

	if stages != 2 {
		t.Fatalf("parsed %d post-process events, want 2", stages)
	}
	if logs != 1 {
		t.Fatalf("kept %d diagnostic lines, want only the warning", logs)
	}
}

func TestDownloaderReportsProcessFailure(t *testing.T) {
	binary := fakeBinary(t, `#!/bin/sh
printf 'ERROR: Video unavailable\n' >&2
exit 1
`)

	events, err := ytdlp.Downloader{Binary: binary}.Start(context.Background(), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	collected := drain(t, events, 10*time.Second)
	terminal := collected[len(collected)-1]
	if !terminal.Done || terminal.Err == nil {
		t.Fatalf("terminal event = %#v, want a failure", terminal)
	}

	var sawError bool
	for _, event := range collected {
		if event.Log == "ERROR: Video unavailable" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("stderr diagnostics missing from %#v", collected)
	}
}

func TestDownloaderCancelsPromptly(t *testing.T) {
	// The heartbeat child keeps running until something stops it, and it keeps
	// the output pipe open, which is exactly the shape of a stray ffmpeg.
	binary := fakeBinary(t, `#!/bin/sh
printf '__CAXXXD_PROGRESS__downloading\t1\t100\tNA\t1\t30\n'
while true; do
  printf 'x' >> "$1"
  sleep 0.1
done &
wait
`)
	heartbeat := filepath.Join(t.TempDir(), "heartbeat")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := ytdlp.Downloader{Binary: binary}.Start(ctx, []string{heartbeat})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	first := <-events
	if first.Parsed.Kind != ytdlp.EventProgress {
		t.Fatalf("first event = %#v, want progress", first)
	}

	started := time.Now()
	cancel()

	collected := drain(t, events, 10*time.Second)
	terminal := collected[len(collected)-1]
	if !terminal.Done {
		t.Fatalf("last event = %#v, want terminal", terminal)
	}
	if !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("terminal error = %v, want context.Canceled", terminal.Err)
	}
	// Well under the runtime's pipe-closing backstop: the process group itself
	// has to go down, not just the command caxxxd launched.
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s, want a prompt process-group termination", elapsed)
	}

	assertHeartbeatStopped(t, heartbeat)
}

// assertHeartbeatStopped fails if the fake binary's child is still writing.
func assertHeartbeatStopped(t *testing.T, path string) {
	t.Helper()

	size := func() int64 {
		info, err := os.Stat(path)
		if err != nil {
			return 0
		}
		return info.Size()
	}

	time.Sleep(300 * time.Millisecond)
	before := size()
	time.Sleep(600 * time.Millisecond)
	if after := size(); after != before {
		t.Fatalf("child process survived cancellation: heartbeat grew from %d to %d bytes", before, after)
	}
}

func TestDownloaderReportsStartFailure(t *testing.T) {
	downloader := ytdlp.Downloader{Binary: filepath.Join(t.TempDir(), "does-not-exist")}

	if _, err := downloader.Start(context.Background(), nil); err == nil {
		t.Fatal("Start succeeded for a missing binary")
	}
}
