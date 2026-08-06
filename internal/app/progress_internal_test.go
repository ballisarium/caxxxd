package app

import (
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestProgressSampleReportsAKnownTotal(t *testing.T) {
	sample := progressSample(ytdlp.Progress{
		Status:          "downloading",
		DownloadedBytes: 2_097_152,
		TotalBytes:      8_388_608,
		SpeedBytes:      1_048_576,
		ETASeconds:      6,
		ETAKnown:        true,
	}, "downloading", 0)

	if !sample.Known {
		t.Fatal("a reported total should be treated as known")
	}
	if sample.Fraction != 0.25 {
		t.Fatalf("Fraction = %v, want 0.25", sample.Fraction)
	}
	for _, fragment := range []string{"2.0 MiB / 8.0 MiB", "1.0 MiB/s", "6s left"} {
		if !strings.Contains(sample.Detail, fragment) {
			t.Fatalf("Detail = %q, want it to mention %q", sample.Detail, fragment)
		}
	}
}

func TestAStartingDownloadCountsZeroRatherThanUnknown(t *testing.T) {
	// "?" is what a size nobody reported looks like. Nothing downloaded yet is
	// a number, and the first sample of every download carries it.
	started := progressSample(ytdlp.Progress{
		Status:     "downloading",
		TotalBytes: 8_388_608,
	}, "downloading", 0)

	if !strings.HasPrefix(started.Detail, "0 B / 8.0 MiB") {
		t.Fatalf("Detail = %q, want it to start from zero", started.Detail)
	}

	unsized := progressSample(ytdlp.Progress{Status: "downloading"}, "downloading", 0)
	if !strings.HasPrefix(unsized.Detail, "0 B downloaded") {
		t.Fatalf("Detail = %q, want it to start from zero", unsized.Detail)
	}
}

func TestProgressSampleSaysWhenTheTotalIsUnknown(t *testing.T) {
	sample := progressSample(ytdlp.Progress{
		Status:          "downloading",
		DownloadedBytes: 1_048_576,
	}, "downloading", 0)

	if sample.Known {
		t.Fatal("a download with no total must not claim one")
	}
	if sample.Fraction != 0 {
		t.Fatalf("Fraction = %v, want 0 with no total", sample.Fraction)
	}
	if !strings.Contains(sample.Detail, "size unknown") {
		t.Fatalf("Detail = %q, want it to say the size is unknown", sample.Detail)
	}
}

func TestProgressDetailAvoidsMultiByteSeparators(t *testing.T) {
	// pterm measures the text around the bar in bytes, so a detail line full
	// of wide glyphs would quietly shorten the bar it describes.
	sample := progressSample(ytdlp.Progress{
		Status:          "downloading",
		DownloadedBytes: 1,
		TotalBytes:      2,
		ETAKnown:        true,
		ETASeconds:      1,
	}, "downloading", 1)

	if len(sample.Detail) != len([]rune(sample.Detail)) {
		t.Fatalf("Detail = %q, want it to stay single-byte", sample.Detail)
	}
}

func TestAFinishedStreamDoesNotClaimAnUnknownTime(t *testing.T) {
	sample := progressSample(ytdlp.Progress{
		Status:          "finished",
		DownloadedBytes: 4_194_304,
		TotalBytes:      4_194_304,
	}, "finished", 0)

	if sample.Fraction != 1 {
		t.Fatalf("Fraction = %v, want a full bar", sample.Fraction)
	}
	if !strings.Contains(sample.Detail, "stream complete") {
		t.Fatalf("Detail = %q, want it to say the stream is done", sample.Detail)
	}
	for _, unwanted := range []string{"time left", "speed unknown", "/s"} {
		if strings.Contains(sample.Detail, unwanted) {
			t.Fatalf("Detail = %q, want no %q on a finished stream", sample.Detail, unwanted)
		}
	}
}

func TestAFinishedStreamOfUnknownSizeStillReadsSensibly(t *testing.T) {
	sample := progressSample(ytdlp.Progress{Status: "finished"}, "finished", 0)

	if sample.Detail != "stream complete" {
		t.Fatalf("Detail = %q, want it to skip a size it does not have", sample.Detail)
	}
}

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		phase     string
		partsDone int
		want      string
	}{
		{phase: "", want: "starting"},
		{phase: "downloading", want: ""},
		{phase: "downloading", partsDone: 1, want: "part 2"},
		{phase: "postprocess", want: "postprocess"},
	}

	for _, test := range tests {
		if got := phaseLabel(test.phase, test.partsDone); got != test.want {
			t.Fatalf("phaseLabel(%q, %d) = %q, want %q", test.phase, test.partsDone, got, test.want)
		}
	}
}

func TestETALabelSeparatesUnknownFromAlmostDone(t *testing.T) {
	unknown := etaLabel(ytdlp.Progress{})
	if unknown != "time left unknown" {
		t.Fatalf("etaLabel with no estimate = %q", unknown)
	}

	almost := etaLabel(ytdlp.Progress{ETAKnown: true})
	if almost != "almost done" {
		t.Fatalf("etaLabel at zero seconds = %q", almost)
	}

	counting := etaLabel(ytdlp.Progress{ETAKnown: true, ETASeconds: 90})
	if counting != "1m 30s left" {
		t.Fatalf("etaLabel at 90 seconds = %q", counting)
	}
}

func TestStreamRowDescribesEveryKindOfStream(t *testing.T) {
	tests := []struct {
		name   string
		format ytdlp.Format
		want   []string
	}{
		{
			name: "a video stream",
			format: ytdlp.Format{
				ID: "137", Kind: ytdlp.FormatKindVideo, Extension: "mp4",
				Width: 1920, Height: 1080, FPS: 30, VideoCodec: "avc1.640028",
				VideoBitrate: 4500, FileSize: 75_000_000,
			},
			want: []string{"137", "video", "1920x1080", "30", "avc1", "4500 kbps", "71.5 MiB", "mp4"},
		},
		{
			name: "an audio stream with an estimated size",
			format: ytdlp.Format{
				ID: "251", Kind: ytdlp.FormatKindAudio, Extension: "webm",
				AudioCodec: "opus", AudioBitrate: 135, ApproxFileSize: 2_200_000,
			},
			want: []string{"251", "audio", "audio only", "—", "opus", "~2.1 MiB"},
		},
		{
			name: "a combined stream",
			format: ytdlp.Format{
				ID: "18", Kind: ytdlp.FormatKindMuxed, Extension: "mp4",
				Width: 640, Height: 360, VideoCodec: "avc1.42001E", AudioCodec: "mp4a.40.2",
			},
			want: []string{"18", "muxed", "avc1+mp4a", "?"},
		},
		{
			name: "a stream with no width reported",
			format: ytdlp.Format{
				ID: "x", Kind: ytdlp.FormatKindVideo, Height: 720, VideoCodec: "vp9",
			},
			want: []string{"720p", "vp9"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := fullTable.row(test.format)
			for _, fragment := range test.want {
				if !strings.Contains(row, fragment) {
					t.Fatalf("row = %q, want it to mention %q", row, fragment)
				}
			}
		})
	}
}

func TestStreamRowsShareTheirColumns(t *testing.T) {
	// The heading is printed through Accent, which indents by one column, and
	// the menu indents every row by two: its selector, or the blank in its
	// place. Both are spelled out here because the alignment depends on them.
	header := " " + fullTable.header()
	row := "  " + fullTable.row(ytdlp.Format{
		ID: "137", Kind: ytdlp.FormatKindVideo, Extension: "mp4",
		Width: 1920, Height: 1080, FPS: 30, VideoCodec: "avc1.640028",
	})

	for heading, cell := range map[string]string{
		"ID":         "137",
		"RESOLUTION": "1920x1080",
		"CODEC":      "avc1",
		"EXT":        "mp4",
	} {
		if column(header, heading) != column(row, cell) {
			t.Fatalf("%s does not sit under its heading:\n%s\n%s", cell, header, row)
		}
	}
}

// column is the display column a fragment starts at. Byte offsets would not
// do: an em dash in an earlier cell is three bytes and one column, which is
// the whole reason the table pads the way it does.
func column(line, fragment string) int {
	index := strings.Index(line, fragment)
	if index < 0 {
		return -1
	}
	return len([]rune(line[:index]))
}

func TestShortCodecKeepsNamesReadable(t *testing.T) {
	tests := map[string]string{
		"avc1.640028": "avc1",
		"opus":        "opus",
		"none":        "—",
		"":            "—",
	}

	for codec, want := range tests {
		if got := shortCodec(codec); got != want {
			t.Fatalf("shortCodec(%q) = %q, want %q", codec, got, want)
		}
	}
}

// sampleStreams is one of each kind, with the widest cells the table can meet.
var sampleStreams = []ytdlp.Format{
	{
		ID: "616-drc", Kind: ytdlp.FormatKindVideo, Extension: "webm",
		Width: 3840, Height: 2160, FPS: 60, VideoCodec: "vp09.00.50.08",
		VideoBitrate: 24500, ApproxFileSize: 1_500_000_000,
	},
	{
		ID: "251", Kind: ytdlp.FormatKindAudio, Extension: "webm",
		AudioCodec: "opus", AudioBitrate: 135, FileSize: 2_200_000,
	},
	{
		ID: "18", Kind: ytdlp.FormatKindMuxed, Extension: "mp4",
		Width: 640, Height: 360, VideoCodec: "avc1.42001E", AudioCodec: "mp4a.40.2",
	},
}

func TestTheStreamTableFitsTheNarrowestTerminal(t *testing.T) {
	// A row that does not fit wraps, and a wrapped row breaks the menu it is
	// drawn in: pterm redraws by counting newlines, not folded lines.
	table := tableFor(ui.MinWidth)

	lines := []string{" " + table.header()}
	for _, format := range sampleStreams {
		lines = append(lines, "  "+table.row(format))
	}

	for _, line := range lines {
		// Every cell in the table is one column per rune.
		if width := len([]rune(line)); width > ui.MinWidth {
			t.Fatalf("a row is %d columns wide in a %d column terminal:\n%q",
				width, ui.MinWidth, line)
		}
	}
}

func TestTheWholeTableIsUsedWhenThereIsRoom(t *testing.T) {
	table := tableFor(ui.MaxWidth)
	if table.compact {
		t.Fatalf("a %d column terminal should get every column", ui.MaxWidth)
	}

	row := table.row(sampleStreams[0])
	for _, fragment := range []string{"60", "vp09", "24500 kbps"} {
		if !strings.Contains(row, fragment) {
			t.Fatalf("row = %q, want it to carry %q", row, fragment)
		}
	}
}

func TestTheNarrowTableKeepsWhatIdentifiesAStream(t *testing.T) {
	table := tableFor(ui.MinWidth)
	if !table.compact {
		t.Fatalf("a %d column terminal cannot hold every column", ui.MinWidth)
	}

	row := table.row(sampleStreams[0])
	for _, fragment := range []string{"616-drc", "video", "3840x2160", "webm"} {
		if !strings.Contains(row, fragment) {
			t.Fatalf("row = %q, want it to keep %q", row, fragment)
		}
	}
}

func TestBothLayoutsAlignTheirColumns(t *testing.T) {
	for name, table := range map[string]streamTable{"full": fullTable, "compact": compactTable} {
		header := " " + table.header()
		row := "  " + table.row(sampleStreams[0])

		for _, pair := range [][2]string{{"ID", "616-drc"}, {"RESOLUTION", "3840x2160"}, {"EXT", "webm"}} {
			if column(header, pair[0]) != column(row, pair[1]) {
				t.Fatalf("%s table: %s is not under %s:\n%s\n%s", name, pair[1], pair[0], header, row)
			}
		}
	}
}
