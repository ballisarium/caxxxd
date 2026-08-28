package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestTranscriptConverterWritesPlainUTF8Text(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.ru.srt")
	destination := filepath.Join(dir, "captions.ru.txt")
	input := "\ufeff1\r\n00:00:00,000 --> 00:00:02,000\r\n<i>Привет</i>, мир!\r\n\r\n" +
		"2\r\n00:00:02,500 --> 00:00:04,000\r\nTom &amp; Jerry\r\n[Музыка]\r\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, false, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	want := "Привет, мир!\nTom & Jerry [Музыка]\n"
	if string(got) != want {
		t.Fatalf("transcript = %q, want %q", got, want)
	}
}

func TestTranscriptConverterKeepsOnlyCuesInTheSelectedRange(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.srt")
	destination := filepath.Join(dir, "captions.txt")
	input := "1\n00:00:00,000 --> 00:00:02,000\nBefore\n\n" +
		"2\n00:00:02,000 --> 00:00:03,000\nInside\n\n" +
		"3\n00:00:04,000 --> 00:00:05,000\nAfter\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	section := domain.TimeRange{Start: 2, End: 4}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, false, &section); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Inside\n" {
		t.Fatalf("transcript = %q, want only the overlapping cue", got)
	}
}

func TestInvalidSubtitlesLeaveAnExistingTranscriptIntact(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.srt")
	destination := filepath.Join(dir, "captions.txt")
	if err := os.WriteFile(source, []byte("not an SRT file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, false, nil); err == nil {
		t.Fatal("Convert accepted an invalid subtitle file")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep me\n" {
		t.Fatalf("existing transcript was changed to %q", got)
	}
}

func TestAutomaticTranscriptRemovesRollingCaptionRepeats(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.en.srt")
	destination := filepath.Join(dir, "captions.en.txt")
	input := "1\n00:00:00,000 --> 00:00:01,000\nHello\n\n" +
		"2\n00:00:00,500 --> 00:00:02,000\nHello world\n\n" +
		"3\n00:00:01,500 --> 00:00:03,000\nworld again.\n\n" +
		"4\n00:00:04,000 --> 00:00:05,000\nA new thought.\n\n" +
		"5\n00:00:05,000 --> 00:00:06,000\nthought. Still here.\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, true, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello world again.\nA new thought.\nthought. Still here.\n"
	if string(got) != want {
		t.Fatalf("transcript = %q, want %q", got, want)
	}
}

func TestTranscriptConverterRejectsPartiallyMalformedSRT(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.srt")
	destination := filepath.Join(dir, "captions.txt")
	input := "1\n00:00:00,000 --> 00:00:01,000\nValid\n\n" +
		"2\n00:61:00,000 --> 00:61:01,000\nInvalid timestamp\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, false, nil); err == nil {
		t.Fatal("Convert accepted a partially malformed subtitle file")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial transcript exists: %v", err)
	}
}

func TestTranscriptConverterRemovesDecodedMarkupAndTerminalEscapes(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.srt")
	destination := filepath.Join(dir, "captions.txt")
	input := "1\n00:00:00,000 --> 00:00:01,000\n&lt;i&gt;Safe&lt;/i&gt; &#x1b;[2J text\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (ytdlp.TranscriptConverter{}).Convert(context.Background(), source, destination, false, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "Safe text\n" {
		t.Fatalf("sanitized transcript = %q", payload)
	}
}

func TestCancelledConversionLeavesAnExistingTranscriptIntact(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "captions.srt")
	destination := filepath.Join(dir, "captions.txt")
	if err := os.WriteFile(source, []byte("1\n00:00:00,000 --> 00:00:01,000\nNew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("Old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := (ytdlp.TranscriptConverter{}).Convert(ctx, source, destination, false, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Convert error = %v, want context.Canceled", err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "Old\n" {
		t.Fatalf("cancelled conversion replaced transcript with %q", payload)
	}
}
