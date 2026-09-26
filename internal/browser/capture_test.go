package browser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureKeepsPlayableResourcesAndHidesAddresses(t *testing.T) {
	s := &Session{}
	for _, item := range []struct{ url, mime string }{
		{"https://media.example.test/watch", "application/vnd.apple.mpegurl"},
		{"https://media.example.test/other", "video/\x1b[2J"},
		{"https://media.example.test/clip.mp4", "video/mp4"},
		{"https://media.example.test/part.m4s", "video/mp4"},
		{"https://media.example.test/part.ts", "video/mp2t"},
		{"https://media.example.test/watch", "application/vnd.apple.mpegurl"},
	} {
		s.observe(item.url, item.mime, "https://example.test/", "", "")
	}
	items := s.Candidates()
	if len(items) != 3 || items[0].Kind != "HLS" || items[1].Kind != "VIDEO" || items[2].Kind != "MP4" {
		t.Fatal("expected a playlist and two safe file labels, without fragments or duplicates")
	}
	if items[0].Host != "media.example.test" {
		t.Fatal("expected a host-only display label")
	}
}

func TestBrowserStartupFailureRemovesPrivateProfile(t *testing.T) {
	directory := t.TempDir()
	_, err := (Launcher{Binary: filepath.Join(directory, "missing-browser"), TempDir: directory}).Start(context.Background(), "https://example.test/")
	if err == nil {
		t.Fatal("missing browser must fail")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed startup left its temporary profile behind")
	}
}
