package ytdlp_test

import (
	"testing"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestNormalizeFormatsClassifiesStreams(t *testing.T) {
	raw := []ytdlp.RawFormat{
		{ID: "18", VideoCodec: "avc1", AudioCodec: "mp4a", Height: 360},
		{ID: "137", VideoCodec: "avc1", AudioCodec: "none", Height: 1080},
		{ID: "140", VideoCodec: "none", AudioCodec: "mp4a", AudioBitrate: 129},
	}

	got := ytdlp.NormalizeFormats(raw)
	if got[0].Kind != ytdlp.FormatKindMuxed {
		t.Fatalf("format 18 kind = %q", got[0].Kind)
	}
	if got[1].Kind != ytdlp.FormatKindVideo {
		t.Fatalf("format 137 kind = %q", got[1].Kind)
	}
	if got[2].Kind != ytdlp.FormatKindAudio {
		t.Fatalf("format 140 kind = %q", got[2].Kind)
	}
}

func TestNormalizeFormatsDropsNonMediaEntries(t *testing.T) {
	raw := []ytdlp.RawFormat{
		{ID: "sb0", VideoCodec: "none", AudioCodec: "none", Extension: "mhtml"},
		{ID: "140", VideoCodec: "none", AudioCodec: "mp4a"},
	}

	got := ytdlp.NormalizeFormats(raw)
	if len(got) != 1 || got[0].ID != "140" {
		t.Fatalf("NormalizeFormats = %#v, want only the audio stream", got)
	}
}

func TestNormalizeFormatsTreatsUnknownCodecsAsMuxed(t *testing.T) {
	raw := []ytdlp.RawFormat{{ID: "direct", Extension: "mp4"}}

	got := ytdlp.NormalizeFormats(raw)
	if len(got) != 1 || got[0].Kind != ytdlp.FormatKindMuxed {
		t.Fatalf("NormalizeFormats = %#v, want one muxed stream", got)
	}
}

func TestNormalizeFormatsClassifiesByTheNumbersWhenACodecIsMissing(t *testing.T) {
	// Not every extractor fills in both codec fields. A missing one is not a
	// claim that the stream is there, so the rest of the entry has to answer.
	raw := []ytdlp.RawFormat{
		{ID: "video", Extension: "mp4", Height: 1080, VideoBitrate: 3000, AudioCodec: "none"},
		{ID: "picture-only", Extension: "mp4", Width: 1920, Height: 1080},
		{ID: "sound-only", Extension: "mp3", AudioBitrate: 128, AudioSampleRate: 44100},
	}

	got := ytdlp.NormalizeFormats(raw)
	if len(got) != 3 {
		t.Fatalf("NormalizeFormats kept %d of 3 streams: %#v", len(got), got)
	}

	wanted := []ytdlp.FormatKind{
		ytdlp.FormatKindVideo,
		ytdlp.FormatKindVideo,
		ytdlp.FormatKindAudio,
	}
	for index, want := range wanted {
		if got[index].Kind != want {
			t.Fatalf("format %q classified as %q, want %q", got[index].ID, got[index].Kind, want)
		}
	}
}

func TestNormalizeFormatsCopiesEveryField(t *testing.T) {
	raw := []ytdlp.RawFormat{{
		ID:              "137",
		Extension:       "mp4",
		Width:           1920,
		Height:          1080,
		FPS:             30,
		VideoCodec:      "avc1.640028",
		AudioCodec:      "none",
		TotalBitrate:    4600,
		VideoBitrate:    4500,
		AudioSampleRate: 0,
		FileSize:        75000000,
		ApproxFileSize:  74000000,
	}}

	got := ytdlp.NormalizeFormats(raw)[0]
	want := ytdlp.Format{
		ID:             "137",
		Kind:           ytdlp.FormatKindVideo,
		Extension:      "mp4",
		Width:          1920,
		Height:         1080,
		FPS:            30,
		VideoCodec:     "avc1.640028",
		AudioCodec:     "none",
		TotalBitrate:   4600,
		VideoBitrate:   4500,
		FileSize:       75000000,
		ApproxFileSize: 74000000,
	}
	if got != want {
		t.Fatalf("NormalizeFormats = %#v, want %#v", got, want)
	}
}

func TestEffectiveSizePrefersExactValue(t *testing.T) {
	exact := ytdlp.Format{FileSize: 100, ApproxFileSize: 90}
	size, approx := exact.EffectiveSize()
	if size != 100 || approx {
		t.Fatalf("EffectiveSize() = (%d, %t), want (100, false)", size, approx)
	}

	estimated := ytdlp.Format{ApproxFileSize: 90}
	size, approx = estimated.EffectiveSize()
	if size != 90 || !approx {
		t.Fatalf("EffectiveSize() = (%d, %t), want (90, true)", size, approx)
	}

	unknown := ytdlp.Format{}
	if size, approx = unknown.EffectiveSize(); size != 0 || approx {
		t.Fatalf("EffectiveSize() = (%d, %t), want (0, false)", size, approx)
	}
}

func TestFormatsByKindFiltersStreams(t *testing.T) {
	formats := ytdlp.NormalizeFormats([]ytdlp.RawFormat{
		{ID: "18", VideoCodec: "avc1", AudioCodec: "mp4a"},
		{ID: "137", VideoCodec: "avc1", AudioCodec: "none"},
		{ID: "140", VideoCodec: "none", AudioCodec: "mp4a"},
	})

	video := ytdlp.FormatsByKind(formats, ytdlp.FormatKindMuxed, ytdlp.FormatKindVideo)
	if len(video) != 2 || video[0].ID != "18" || video[1].ID != "137" {
		t.Fatalf("FormatsByKind(video) = %#v", video)
	}

	audio := ytdlp.FormatsByKind(formats, ytdlp.FormatKindAudio)
	if len(audio) != 1 || audio[0].ID != "140" {
		t.Fatalf("FormatsByKind(audio) = %#v", audio)
	}
}
