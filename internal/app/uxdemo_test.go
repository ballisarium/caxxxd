package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pterm/pterm"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// slowDownloader plays a download at human speed so the live view can be
// looked at rather than only asserted on.
type slowDownloader struct{ dir string }

func (d slowDownloader) Start(ctx context.Context, args []string) (<-chan ytdlp.RunEvent, error) {
	events := make(chan ytdlp.RunEvent, 64)

	go func() {
		defer close(events)
		total := int64(103_285_760)

		for step := 0; step <= 10; step++ {
			downloaded := total * int64(step) / 10
			events <- ytdlp.RunEvent{Parsed: ytdlp.Event{
				Kind: ytdlp.EventProgress,
				Progress: ytdlp.Progress{
					Status:          "downloading",
					DownloadedBytes: downloaded,
					TotalBytes:      total,
					SpeedBytes:      7_549_747,
					ETASeconds:      int64(10 - step),
					ETAKnown:        true,
				},
			}}
			time.Sleep(250 * time.Millisecond)
		}

		events <- ytdlp.RunEvent{Parsed: ytdlp.Event{
			Kind:     ytdlp.EventProgress,
			Progress: ytdlp.Progress{Status: "finished", DownloadedBytes: total, TotalBytes: total},
		}}
		time.Sleep(400 * time.Millisecond)

		events <- ytdlp.RunEvent{Parsed: ytdlp.Event{Kind: ytdlp.EventPostProcess, Stage: "started"}}
		time.Sleep(4 * time.Second)

		events <- ytdlp.RunEvent{Parsed: ytdlp.Event{
			Kind:     ytdlp.EventCompletedFile,
			FilePath: filepath.Join(d.dir, "deep house for long coding sessions [h0dRXiGf9nU].opus"),
		}}
		events <- ytdlp.RunEvent{Done: true}
	}()

	return events, nil
}

// TestUXDemo is not an assertion; it draws the flow onto the real terminal so
// the live parts can be watched. Run it under a pty:
//
//	CAXXXD_UX_DEMO=1 go test ./internal/app -run TestUXDemo -v
func TestUXDemo(t *testing.T) {
	if os.Getenv("CAXXXD_UX_DEMO") == "" {
		t.Skip("set CAXXXD_UX_DEMO=1 to draw the flow")
	}
	pterm.EnableStyling()
	defer pterm.DisableStyling()

	dir := t.TempDir()
	console := ui.NewConsole(os.Stdout)
	console.SetClearScreens(true)

	session := app.New(app.Options{
		InitialURL:  "https://www.youtube.com/watch?v=h0dRXiGf9nU",
		Version:     "demo",
		Checker:     readyChecker(),
		Client:      ytdlp.Client{Binary: "yt-dlp", Runner: stubRunner{payload: fixture(t, "video.json")}},
		Downloader:  slowDownloader{dir: dir},
		ConfigStore: writtenConfig(t, config.Config{OutputDir: dir, AudioFormat: domain.AudioFormatSource}),
		RevealFile:  func(string) error { return nil },
		Home:        dir,
		Console:     console,
		Prompter:    app.NewTerminalPrompter(console),
	})

	if err := session.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	time.Sleep(time.Second)
}
