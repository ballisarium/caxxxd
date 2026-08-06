package ytdlp

import (
	"context"
	"strings"
	"testing"
)

func TestSanitizeRemovesWhatATerminalWouldActOn(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "a colour sequence",
			text: "\x1b[31mRed title\x1b[0m",
			want: "Red title",
		},
		{
			name: "a cursor move",
			text: "Title\x1b[2J\x1b[H overwritten",
			want: "Title overwritten",
		},
		{
			name: "an operating system command",
			text: "Title\x1b]8;;https://example.test\x07link\x1b]8;;\x07",
			want: "Titlelink",
		},
		{
			name: "a newline that would break a panel",
			text: "First line\nSecond line",
			want: "First line Second line",
		},
		{
			name: "a carriage return that would overwrite the line",
			text: "Real title\rFake title",
			want: "Real title Fake title",
		},
		{
			name: "a bell and other control bytes",
			text: "Title\x07\x00\x1f end",
			want: "Title end",
		},
		{
			name: "ordinary text is left alone",
			text: "Обычное название — 4K (HDR) 漢字",
			want: "Обычное название — 4K (HDR) 漢字",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitize(test.text); got != test.want {
				t.Fatalf("sanitize(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}

func TestMetadataIsSanitizedOnTheWayIn(t *testing.T) {
	// The control bytes arrive as JSON escapes, which is exactly how yt-dlp
	// encodes whatever the page gave it.
	payload := `{
	  "id": "abc\u001b[31m123",
	  "title": "Title\u001b[2J with an escape",
	  "uploader": "Channel\nwith a newline",
	  "extractor_key": "Youtube",
	  "formats": [
	    {"format_id":"18\u001b[0m","ext":"mp4","vcodec":"avc1","acodec":"mp4a","height":360}
	  ]
	}`

	client := Client{Binary: "yt-dlp", Runner: fixedRunner{payload: []byte(payload)}}
	info, err := client.Fetch(t.Context(), "https://example.test/video")
	if err != nil {
		t.Fatalf("Fetch() = %v", err)
	}

	for name, value := range map[string]string{
		"title":    info.Title,
		"uploader": info.Uploader,
		"id":       info.ID,
		"format":   info.Formats[0].ID,
	} {
		if strings.ContainsRune(value, '\x1b') || strings.ContainsAny(value, "\n\r") {
			t.Fatalf("%s reached the model unsanitised: %q", name, value)
		}
	}
}

func TestDiagnosticLinesAreSanitized(t *testing.T) {
	events := make(chan RunEvent, 4)
	writer := &lineWriter{events: events, parse: true}

	_, _ = writer.Write([]byte("ERROR: \x1b[31msomething broke\x1b[0m\n"))

	select {
	case event := <-events:
		if event.Log != "ERROR: something broke" {
			t.Fatalf("Log = %q", event.Log)
		}
	default:
		t.Fatal("the line never became an event")
	}
}

// fixedRunner answers every call with the same payload.
type fixedRunner struct{ payload []byte }

func (f fixedRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return f.payload, nil
}
