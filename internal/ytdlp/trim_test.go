package ytdlp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

const fakeTrimScript = `#!/bin/sh
input=""
output=""
previous=""
for argument in "$@"; do
  printf '%s\n' "$argument" >> "$CAXXXD_TRIM_ARGS"
  if [ "$previous" = "-i" ]; then
    input="$argument"
  fi
  output="$argument"
  previous="$argument"
done
if [ "${CAXXXD_TRIM_MODE:-success}" = "fail" ]; then
  printf 'fake ffmpeg failure\n' >&2
  exit 1
fi
printf 'trimmed' > "$output"
`

const fakeProbeScript = `#!/bin/sh
case "$*" in
  *"stream=codec_type"*) printf 'video,0\naudio,0\n' ;;
  *) printf '8.899000\n' ;;
esac
`

const fakeAudioProbeScript = `#!/bin/sh
case "$*" in
  *"stream=codec_type"*) printf 'audio,0\nvideo,1\n' ;;
  *) printf '0.000000\n' ;;
esac
`

func TestTrimmerNormalizesTheFileWithStreamCopyAndReplacesIt(t *testing.T) {
	binary := fakeBinary(t, fakeTrimScript)
	probe := fakeBinary(t, fakeProbeScript)
	input := filepath.Join(t.TempDir(), "clip.mkv")
	if err := os.WriteFile(input, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed input: %v", err)
	}
	argsPath := filepath.Join(t.TempDir(), "ffmpeg-args")
	t.Setenv("CAXXXD_TRIM_ARGS", argsPath)

	err := (ytdlp.Trimmer{Binary: binary, Probe: probe}).Trim(context.Background(), input, domain.TimeRange{Start: 5, End: 12})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}

	content, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read replaced input: %v", err)
	}
	if string(content) != "trimmed" {
		t.Fatalf("input content = %q, want trimmed output", content)
	}

	args := readLines(t, argsPath)
	assertContainsSequence(t, args, "-ss", "8.899", "-i", input, "-t", "7", "-map", "0", "-c", "copy", "-avoid_negative_ts", "make_zero")
	output := args[len(args)-1]
	if output == input {
		t.Fatal("ffmpeg output must be a temporary path, not the input")
	}
	if filepath.Dir(output) != filepath.Dir(input) || filepath.Ext(output) != filepath.Ext(input) {
		t.Fatalf("temporary output = %q, want same directory and extension as %q", output, input)
	}

	entries, err := os.ReadDir(filepath.Dir(input))
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "caxxxd-trim") {
			t.Fatalf("temporary trim file was left behind: %q", entry.Name())
		}
	}
}

func TestTrimmerKeepsTheOriginalWhenFfmpegFails(t *testing.T) {
	binary := fakeBinary(t, fakeTrimScript)
	probe := fakeBinary(t, fakeProbeScript)
	input := filepath.Join(t.TempDir(), "clip.mkv")
	if err := os.WriteFile(input, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed input: %v", err)
	}
	argsPath := filepath.Join(t.TempDir(), "ffmpeg-args")
	t.Setenv("CAXXXD_TRIM_ARGS", argsPath)
	t.Setenv("CAXXXD_TRIM_MODE", "fail")

	err := (ytdlp.Trimmer{Binary: binary, Probe: probe}).Trim(context.Background(), input, domain.TimeRange{Start: 5, End: 12})
	if err == nil {
		t.Fatal("Trim succeeded, want ffmpeg failure")
	}

	content, readErr := os.ReadFile(input)
	if readErr != nil {
		t.Fatalf("read original input: %v", readErr)
	}
	if string(content) != "original" {
		t.Fatalf("input content = %q, want original file preserved", content)
	}
}

func TestTrimmerDropsAttachedPictureFromAudioOutput(t *testing.T) {
	binary := fakeBinary(t, fakeTrimScript)
	probe := fakeBinary(t, fakeAudioProbeScript)
	input := filepath.Join(t.TempDir(), "clip.opus")
	if err := os.WriteFile(input, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed input: %v", err)
	}
	argsPath := filepath.Join(t.TempDir(), "ffmpeg-args")
	t.Setenv("CAXXXD_TRIM_ARGS", argsPath)

	err := (ytdlp.Trimmer{Binary: binary, Probe: probe}).Trim(context.Background(), input, domain.TimeRange{Start: 5, End: 12})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}

	args := readLines(t, argsPath)
	assertContainsSequence(t, args, "-map", "0:a:0", "-c", "copy")
}

func TestTrimmerRejectsAnInvalidSectionBeforeStartingFfmpeg(t *testing.T) {
	input := filepath.Join(t.TempDir(), "clip.mkv")
	if err := os.WriteFile(input, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed input: %v", err)
	}

	err := (ytdlp.Trimmer{Binary: "/does/not/exist"}).Trim(context.Background(), input, domain.TimeRange{Start: 12, End: 5})
	if err == nil {
		t.Fatal("Trim succeeded for an invalid section")
	}
	if !strings.Contains(err.Error(), "section start must be before section end") {
		t.Fatalf("Trim error = %v, want invalid time range", err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}
