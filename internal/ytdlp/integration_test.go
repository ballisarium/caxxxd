package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/deps"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// fakeYTDLPScript is a stand-in for the real binary. It answers the two
// structured requests caxxxd makes and replays a download, with the behaviour
// selected by CAXXXD_FAKE_DOWNLOAD.
const fakeYTDLPScript = `#!/bin/sh
case "$1" in
  --version)
    echo "2026.01.01"
    exit 0
    ;;
esac

output_dir=""
previous=""
for argument in "$@"; do
  case "$previous" in
    -P) output_dir="$argument" ;;
  esac
  case "$argument" in
    --dump-single-json)
      cat "$CAXXXD_FAKE_METADATA"
      exit 0
      ;;
  esac
  previous="$argument"
done

case "${CAXXXD_FAKE_DOWNLOAD:-normal}" in
  fail)
    printf 'ERROR: Video unavailable\n' >&2
    exit 1
    ;;
  slow)
    printf '__CAXXXD_PROGRESS__downloading\t1048576\t104857600\tNA\t524288\t180\n'
    sleep 30 &
    wait
    ;;
  *)
    printf '__CAXXXD_PROGRESS__downloading\t1048576\t10485760\tNA\t524288\t18\n'
    printf '__CAXXXD_PROGRESS__downloading\t10485760\t10485760\tNA\t524288\t0\n'
    printf '__CAXXXD_POSTPROCESS__started\n'
    : > "$output_dir/Example Video [abc123].mkv"
    printf '__CAXXXD_FILE__%s/Example Video [abc123].mkv\n' "$output_dir"
    ;;
esac
`

// fakeFFmpegScript only has to exist and report a version.
const fakeFFmpegScript = `#!/bin/sh
echo "ffmpeg version 8.1.2"
`

// installFakeBinaries puts fake yt-dlp and ffmpeg executables first on PATH.
func installFakeBinaries(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	for name, body := range map[string]string{
		"yt-dlp": fakeYTDLPScript,
		"ffmpeg": fakeFFmpegScript,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}

	t.Setenv("CAXXXD_FAKE_METADATA", filepath.Join("testdata", "video.json"))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return directory
}

func TestIntegrationDependencyCheckFindsInstalledTools(t *testing.T) {
	directory := installFakeBinaries(t)

	status := deps.NewChecker().Check(context.Background())
	if !status.Ready() {
		t.Fatalf("status = %#v, want ready", status)
	}
	if status.YTDLP.Path != filepath.Join(directory, "yt-dlp") {
		t.Fatalf("yt-dlp path = %q", status.YTDLP.Path)
	}
	if status.YTDLP.Version != "2026.01.01" {
		t.Fatalf("yt-dlp version = %q", status.YTDLP.Version)
	}
	if !strings.HasPrefix(status.FFmpeg.Version, "ffmpeg version") {
		t.Fatalf("ffmpeg version = %q", status.FFmpeg.Version)
	}
}

func TestIntegrationFetchReadsMetadataFromTheBinary(t *testing.T) {
	installFakeBinaries(t)

	info, err := ytdlp.NewClient("yt-dlp").Fetch(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if info.Title != "Example Video" {
		t.Fatalf("Title = %q", info.Title)
	}
	if len(info.Formats) != 5 {
		t.Fatalf("len(Formats) = %d, want 5", len(info.Formats))
	}
	if len(ytdlp.FormatsByKind(info.Formats, ytdlp.FormatKindAudio)) != 2 {
		t.Fatalf("audio streams = %#v", ytdlp.FormatsByKind(info.Formats, ytdlp.FormatKindAudio))
	}
}

func TestIntegrationDownloadRunsTheBuiltCommand(t *testing.T) {
	installFakeBinaries(t)
	outputDir := t.TempDir()

	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://www.youtube.com/watch?v=abc123",
		Mode:      domain.MediaModeVideo,
		Container: domain.VideoContainerMKV,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	assertContainsSequence(t, args, "-f", "bv*+ba/b")

	events, err := ytdlp.Downloader{Binary: "yt-dlp"}.Start(context.Background(), args)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var samples []ytdlp.Progress
	var stages []string
	completed := ""
	var terminal error
	for _, event := range drain(t, events, 15*time.Second) {
		switch {
		case event.Done:
			terminal = event.Err
		case event.Log != "":
		default:
			switch event.Parsed.Kind {
			case ytdlp.EventProgress:
				samples = append(samples, event.Parsed.Progress)
			case ytdlp.EventPostProcess:
				stages = append(stages, event.Parsed.Stage)
			case ytdlp.EventCompletedFile:
				completed = event.Parsed.FilePath
			}
		}
	}

	if terminal != nil {
		t.Fatalf("terminal error = %v", terminal)
	}
	if len(samples) != 2 {
		t.Fatalf("progress samples = %#v, want two", samples)
	}
	if samples[1].Fraction() != 1 {
		t.Fatalf("final fraction = %v, want 1", samples[1].Fraction())
	}
	if len(stages) != 1 || stages[0] != "started" {
		t.Fatalf("post-process stages = %#v", stages)
	}

	wantPath := filepath.Join(outputDir, "Example Video [abc123].mkv")
	if completed != wantPath {
		t.Fatalf("completed path = %q, want %q", completed, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("the reported file does not exist: %v", err)
	}
}

func TestIntegrationDownloadFailureSurfacesDiagnostics(t *testing.T) {
	installFakeBinaries(t)
	t.Setenv("CAXXXD_FAKE_DOWNLOAD", "fail")

	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://www.youtube.com/watch?v=abc123",
		Mode:      domain.MediaModeAudio,
		OutputDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}

	events, err := ytdlp.Downloader{Binary: "yt-dlp"}.Start(context.Background(), args)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	collected := drain(t, events, 15*time.Second)
	terminal := collected[len(collected)-1]
	if terminal.Err == nil {
		t.Fatal("a failing run must report an error")
	}

	var sawDiagnostic bool
	for _, event := range collected {
		if strings.Contains(event.Log, "Video unavailable") {
			sawDiagnostic = true
		}
	}
	if !sawDiagnostic {
		t.Fatalf("diagnostics missing from %#v", collected)
	}
}

func TestIntegrationCancellationStopsTheProcess(t *testing.T) {
	installFakeBinaries(t)
	t.Setenv("CAXXXD_FAKE_DOWNLOAD", "slow")

	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://www.youtube.com/watch?v=abc123",
		Mode:      domain.MediaModeVideo,
		Container: domain.VideoContainerMKV,
		OutputDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := ytdlp.Downloader{Binary: "yt-dlp"}.Start(ctx, args)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	first := <-events
	if first.Parsed.Kind != ytdlp.EventProgress {
		t.Fatalf("first event = %#v", first)
	}

	started := time.Now()
	cancel()

	collected := drain(t, events, 15*time.Second)
	terminal := collected[len(collected)-1]
	if !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("terminal error = %v, want context.Canceled", terminal.Err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}
