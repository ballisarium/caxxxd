package ui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestFormatBytesUsesIECUnits(t *testing.T) {
	tests := map[int64]string{
		0:          "?",
		-1:         "?",
		512:        "512 B",
		1024:       "1.0 KiB",
		1536:       "1.5 KiB",
		1048576:    "1.0 MiB",
		1073741824: "1.0 GiB",
	}

	for size, want := range tests {
		if got := FormatBytes(size); got != want {
			t.Fatalf("FormatBytes(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestFormatSizeMarksEstimates(t *testing.T) {
	if got := FormatSize(1048576, true); got != "~1.0 MiB" {
		t.Fatalf("FormatSize(estimate) = %q", got)
	}
	if got := FormatSize(1048576, false); got != "1.0 MiB" {
		t.Fatalf("FormatSize(exact) = %q", got)
	}
	if got := FormatSize(0, true); got != "?" {
		t.Fatalf("an unknown size must not be marked as an estimate, got %q", got)
	}
}

func TestFormatSpeedAndETA(t *testing.T) {
	if got := FormatSpeed(0); got != "—" {
		t.Fatalf("FormatSpeed(0) = %q", got)
	}
	if got := FormatSpeed(1048576); got != "1.0 MiB/s" {
		t.Fatalf("FormatSpeed = %q", got)
	}

	etas := map[int64]string{
		0:    "—",
		45:   "45s",
		90:   "1m 30s",
		3600: "1h 00m",
		3725: "1h 02m",
	}
	for seconds, want := range etas {
		if got := FormatETA(seconds); got != want {
			t.Fatalf("FormatETA(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[float64]string{
		0:      "Unknown duration",
		-1:     "Unknown duration",
		65:     "1:05",
		125.4:  "2:05",
		3661:   "1:01:01",
		7199.6: "2:00:00",
	}

	for seconds, want := range tests {
		if got := FormatDuration(seconds); got != want {
			t.Fatalf("FormatDuration(%v) = %q, want %q", seconds, got, want)
		}
	}
}

func TestTruncateKeepsWidth(t *testing.T) {
	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Fatalf("short text should be left alone, got %q", got)
	}
	if got := Truncate("abc", 1); got != "…" {
		t.Fatalf("Truncate to one column = %q", got)
	}
	if got := Truncate("abc", 0); got != "" {
		t.Fatalf("Truncate to nothing = %q", got)
	}
}

func TestPadCountsColumnsNotBytes(t *testing.T) {
	// "—" is three bytes and one column: padding by bytes would come up short
	// and skew every row after it.
	if got := Pad("—", 4); got != "—   " {
		t.Fatalf("Pad(—) = %q", got)
	}
	if got := Pad("abcdef", 3); got != "abcdef" {
		t.Fatalf("Pad must never shorten, got %q", got)
	}
}

func TestCellsAlignRowsWithNonASCIIContent(t *testing.T) {
	widths := []int{6, 4, 0}
	plain := Cells([]string{"abc", "de", "end"}, widths)
	wide := Cells([]string{"—", "—", "end"}, widths)

	if len([]rune(plain)) != len([]rune(wide)) {
		t.Fatalf("rows drifted:\n%q\n%q", plain, wide)
	}
}

func TestTruncateCountsColumnsNotRunes(t *testing.T) {
	// A CJK character is one rune and two columns. Counting runes would let a
	// string come back twice as wide as the space it was given.
	tests := []struct {
		text  string
		width int
	}{
		{strings.Repeat("漢", 30), 20},
		{strings.Repeat("🎬", 12), 9},
		{"漢字mixed字漢", 7},
		{strings.Repeat("漢", 3), 1},
	}

	for _, test := range tests {
		got := Truncate(test.text, test.width)
		if runewidth.StringWidth(got) > test.width {
			t.Fatalf("Truncate(%q, %d) = %q, %d columns wide",
				test.text, test.width, got, runewidth.StringWidth(got))
		}
	}
}
