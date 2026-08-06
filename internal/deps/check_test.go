package deps_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ballisarium/caxxxd/internal/deps"
)

func TestCheckReportsMissingDependency(t *testing.T) {
	checker := deps.Checker{
		LookPath: func(name string) (string, error) {
			if name == "yt-dlp" {
				return "", errors.New("missing")
			}
			return "/opt/homebrew/bin/ffmpeg", nil
		},
		Version: func(context.Context, string) (string, error) {
			return "7.1", nil
		},
	}

	status := checker.Check(context.Background())
	if status.YTDLP.Found {
		t.Fatal("yt-dlp unexpectedly found")
	}
	if !status.FFmpeg.Found {
		t.Fatal("ffmpeg should be found")
	}
	if status.Ready() {
		t.Fatal("status must not be ready when yt-dlp is missing")
	}
}

func TestCheckReportsBothDependencies(t *testing.T) {
	checker := deps.Checker{
		LookPath: func(name string) (string, error) {
			return "/opt/homebrew/bin/" + name, nil
		},
		Version: func(_ context.Context, path string) (string, error) {
			if path == "/opt/homebrew/bin/yt-dlp" {
				return "2026.01.01", nil
			}
			return "ffmpeg version 7.1", nil
		},
	}

	status := checker.Check(context.Background())
	if !status.Ready() {
		t.Fatal("status should be ready when both tools resolve")
	}
	if status.YTDLP.Version != "2026.01.01" {
		t.Fatalf("yt-dlp version = %q", status.YTDLP.Version)
	}
	if status.FFmpeg.Path != "/opt/homebrew/bin/ffmpeg" {
		t.Fatalf("ffmpeg path = %q", status.FFmpeg.Path)
	}
	if status.YTDLP.Name != "yt-dlp" || status.FFmpeg.Name != "ffmpeg" {
		t.Fatalf("dependency names not populated: %#v %#v", status.YTDLP, status.FFmpeg)
	}
}

func TestCheckTreatsVersionFailureAsNotFound(t *testing.T) {
	wantErr := errors.New("exec format error")
	checker := deps.Checker{
		LookPath: func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		},
		Version: func(context.Context, string) (string, error) {
			return "", wantErr
		},
	}

	status := checker.Check(context.Background())
	if status.Ready() {
		t.Fatal("a binary that cannot report its version is not usable")
	}
	if !errors.Is(status.YTDLP.Err, wantErr) {
		t.Fatalf("YTDLP.Err = %v, want %v", status.YTDLP.Err, wantErr)
	}
}

func TestMissingReportsUnusableTools(t *testing.T) {
	checker := deps.Checker{
		LookPath: func(name string) (string, error) {
			if name == "ffmpeg" {
				return "", errors.New("missing")
			}
			return "/opt/homebrew/bin/" + name, nil
		},
		Version: func(context.Context, string) (string, error) { return "1", nil },
	}

	missing := checker.Check(context.Background()).Missing()
	if len(missing) != 1 || missing[0] != "ffmpeg" {
		t.Fatalf("Missing() = %v, want [ffmpeg]", missing)
	}
}

func TestNewCheckerHasRealHooks(t *testing.T) {
	checker := deps.NewChecker()
	if checker.LookPath == nil || checker.Version == nil {
		t.Fatal("NewChecker must populate both hooks")
	}
}

// fakeTool writes an executable that answers only the given version flag,
// which is how ffmpeg behaves: it rejects the GNU-style double dash.
func fakeTool(t *testing.T, name string, acceptedFlag string, version string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	body := fmt.Sprintf(`#!/bin/sh
if [ "$1" = %q ]; then
  echo %q
  echo "build configuration nobody needs to read"
  exit 0
fi
echo "Unrecognized option '$1'." >&2
exit 8
`, acceptedFlag, version)

	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	return path
}

func TestCheckAcceptsToolsThatOnlyKnowSingleDashVersion(t *testing.T) {
	ffmpeg := fakeTool(t, "ffmpeg", "-version", "ffmpeg version 8.1.2")
	ytdlp := fakeTool(t, "yt-dlp", "--version", "2026.07.04")

	checker := deps.NewChecker()
	checker.LookPath = func(name string) (string, error) {
		if name == "ffmpeg" {
			return ffmpeg, nil
		}
		return ytdlp, nil
	}

	status := checker.Check(context.Background())
	if !status.Ready() {
		t.Fatalf("status = %#v, want ready", status)
	}
	if status.FFmpeg.Version != "ffmpeg version 8.1.2" {
		t.Fatalf("ffmpeg version = %q", status.FFmpeg.Version)
	}
	if status.YTDLP.Version != "2026.07.04" {
		t.Fatalf("yt-dlp version = %q", status.YTDLP.Version)
	}
}

func TestCheckReportsToolsThatAnswerNoVersionFlag(t *testing.T) {
	broken := fakeTool(t, "ffmpeg", "-nonsense", "never printed")

	checker := deps.NewChecker()
	checker.LookPath = func(string) (string, error) { return broken, nil }

	if status := checker.Check(context.Background()); status.Ready() {
		t.Fatalf("status = %#v, want not ready", status)
	}
}
