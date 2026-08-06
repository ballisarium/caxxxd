package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

type fakeRunner struct {
	payload []byte
	err     error
	name    string
	args    []string
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.name = name
	f.args = args
	return f.payload, f.err
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return payload
}

func TestFetchParsesMetadata(t *testing.T) {
	runner := &fakeRunner{payload: fixture(t, "video.json")}
	client := ytdlp.Client{Binary: "yt-dlp", Runner: runner}

	info, err := client.Fetch(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if info.Title != "Example Video" {
		t.Fatalf("Title = %q", info.Title)
	}
	if info.Uploader != "Example Channel" {
		t.Fatalf("Uploader = %q", info.Uploader)
	}
	if info.Platform != "Youtube" {
		t.Fatalf("Platform = %q", info.Platform)
	}
	if info.Duration != 125.4 {
		t.Fatalf("Duration = %v", info.Duration)
	}
	if info.WebpageURL != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("WebpageURL = %q", info.WebpageURL)
	}
	if info.ThumbnailURL != "https://i.example/abc123.jpg" {
		t.Fatalf("ThumbnailURL = %q", info.ThumbnailURL)
	}
	if len(info.Formats) != 5 {
		t.Fatalf("len(Formats) = %d, want 5", len(info.Formats))
	}
}

func TestFetchPassesStructuredFlags(t *testing.T) {
	runner := &fakeRunner{payload: fixture(t, "video.json")}
	client := ytdlp.Client{Binary: "/opt/homebrew/bin/yt-dlp", Runner: runner}

	if _, err := client.Fetch(context.Background(), "https://example.test/watch?v=a&b=c"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if runner.name != "/opt/homebrew/bin/yt-dlp" {
		t.Fatalf("binary = %q", runner.name)
	}
	assertContainsSequence(t, runner.args, "--dump-single-json")
	assertContainsSequence(t, runner.args, "--no-playlist")
	if runner.args[len(runner.args)-1] != "https://example.test/watch?v=a&b=c" {
		t.Fatalf("URL must be the final argument, got %q", runner.args[len(runner.args)-1])
	}
}

func TestFetchHandlesAudioOnlySources(t *testing.T) {
	client := ytdlp.Client{Binary: "yt-dlp", Runner: &fakeRunner{payload: fixture(t, "audio_only.json")}}

	info, err := client.Fetch(context.Background(), "https://soundcloud.example/example/pod777")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, format := range info.Formats {
		if format.Kind != ytdlp.FormatKindAudio {
			t.Fatalf("format %q kind = %q, want audio", format.ID, format.Kind)
		}
	}
}

func TestFetchRejectsInvalidURL(t *testing.T) {
	runner := &fakeRunner{payload: fixture(t, "video.json")}
	client := ytdlp.Client{Binary: "yt-dlp", Runner: runner}

	for _, rawURL := range []string{"", "not a url", "youtube.com/watch", "ftp:///nohost"} {
		if _, err := client.Fetch(context.Background(), rawURL); err == nil {
			t.Fatalf("Fetch(%q) succeeded, want error", rawURL)
		}
	}
	if runner.name != "" {
		t.Fatal("invalid URLs must not reach the runner")
	}
}

func TestFetchReportsRunnerFailure(t *testing.T) {
	wantErr := errors.New("exit status 1")
	client := ytdlp.Client{Binary: "yt-dlp", Runner: &fakeRunner{err: wantErr}}

	if _, err := client.Fetch(context.Background(), "https://example.test/video"); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch error = %v, want wrapped %v", err, wantErr)
	}
}

func TestFetchReportsMalformedJSON(t *testing.T) {
	client := ytdlp.Client{Binary: "yt-dlp", Runner: &fakeRunner{payload: []byte("{not json")}}

	if _, err := client.Fetch(context.Background(), "https://example.test/video"); err == nil {
		t.Fatal("Fetch accepted malformed JSON")
	}
}

func TestFetchReportsMissingFormats(t *testing.T) {
	client := ytdlp.Client{Binary: "yt-dlp", Runner: &fakeRunner{payload: fixture(t, "no_formats.json")}}

	if _, err := client.Fetch(context.Background(), "https://example.test/video"); !errors.Is(err, ytdlp.ErrNoFormats) {
		t.Fatalf("Fetch error = %v, want ErrNoFormats", err)
	}
}

func TestNewClientUsesExecRunner(t *testing.T) {
	client := ytdlp.NewClient("yt-dlp")
	if client.Binary != "yt-dlp" || client.Runner == nil {
		t.Fatalf("NewClient = %#v", client)
	}
}
