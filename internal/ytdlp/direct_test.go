package ytdlp_test

import (
	"context"
	"testing"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestFetchAcceptsDirectMediaWithoutFormatsArray(t *testing.T) {
	client := ytdlp.Client{Binary: "yt-dlp", Runner: &fakeRunner{payload: []byte(`{
		"id":"clip", "title":"Clip", "url":"https://example.test/clip.mp4",
		"ext":"mp4", "format_id":"http", "vcodec":"h264", "acodec":"aac"
	}`)}}
	info, err := client.Fetch(context.Background(), "https://example.test/clip.mp4")
	if err != nil || len(info.Formats) != 1 {
		t.Fatalf("direct media must remain downloadable: formats=%d err=%v", len(info.Formats), err)
	}
}
