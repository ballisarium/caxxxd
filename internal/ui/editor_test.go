package ui

import (
	"os"
	"strings"
	"testing"
)

// A pipe is not a terminal, so the editor falls back to reading whole lines.
// That path is what keeps `caxxxd < answers.txt` and a CI run from hanging.
func TestReadLineFallsBackToWholeLines(t *testing.T) {
	console, _ := newTestConsole(t)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()

	go func() {
		_, _ = writer.WriteString("https://example.test/clip\n")
		_ = writer.Close()
	}()

	value, err := console.ReadLine(reader, "▸ Paste a media URL: ", "")
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if value != "https://example.test/clip" {
		t.Fatalf("ReadLine() = %q", value)
	}
}

func TestAnEmptyAnswerKeepsWhatWasOffered(t *testing.T) {
	console, _ := newTestConsole(t)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()

	go func() {
		_, _ = writer.WriteString("\n")
		_ = writer.Close()
	}()

	value, err := console.ReadLine(reader, "▸ Download folder: ", "~/Movies")
	if err != nil {
		t.Fatalf("ReadLine() = %v", err)
	}
	if value != "~/Movies" {
		t.Fatalf("ReadLine() = %q, want the prefilled answer", value)
	}
}

func TestReadLineStillShowsTheQuestionWithoutATerminal(t *testing.T) {
	console, output := newTestConsole(t)

	reader, writer, _ := os.Pipe()
	defer reader.Close()
	go func() {
		_, _ = writer.WriteString("x\n")
		_ = writer.Close()
	}()

	_, _ = console.ReadLine(reader, "▸ Paste a media URL: ", "")

	if !strings.Contains(output.String(), "Paste a media URL") {
		t.Fatalf("the question was not asked:\n%q", output.String())
	}
}
