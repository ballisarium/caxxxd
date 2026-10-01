package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureGroupsOnlyReferencedVariantsAndKeepsPublicMetadata(t *testing.T) {
	s := &Session{manifests: map[string]manifestEstimate{}}
	s.observe("https://media.example.test/master.m3u8", "application/vnd.apple.mpegurl", "", "", "")
	s.observe("https://media.example.test/video.m3u8", "application/vnd.apple.mpegurl", "", "", "")
	s.observe("https://media.example.test/advert.mp4", "video/mp4", "", "", "")
	s.manifests[s.items[0].url] = manifestEstimate{Variants: []manifestVariant{{URL: s.items[1].url, Bandwidth: 800000, Width: 1920, Height: 1080, Codecs: "avc1,mp4a"}}}
	s.manifests[s.items[1].url] = manifestEstimate{DurationSeconds: 120}
	s.applyManifestMetadata()
	items := s.Candidates()
	if items[0].Group == "" || items[0].Group != items[1].Group || items[2].Group != "" {
		t.Fatal("playlist references must group variants without folding in an unrelated advertisement")
	}
	if items[1].Duration != 120 || items[1].Height != 1080 || items[1].Codecs != "avc1,mp4a" {
		t.Fatal("variant metadata was not propagated to the captured resource")
	}
}

func TestCaptureRefreshRemovesObsoleteVariantMetadata(t *testing.T) {
	s := &Session{manifests: map[string]manifestEstimate{}}
	for _, name := range []string{"master", "high", "low"} {
		s.observe("https://media.example.test/"+name+".m3u8", "application/vnd.apple.mpegurl", "", "", "")
	}
	s.manifests[s.items[0].url] = manifestEstimate{Variants: []manifestVariant{{URL: s.items[1].url, Width: 1920, Height: 1080, Bandwidth: 800000}, {URL: s.items[2].url, Width: 1280, Height: 720, Bandwidth: 400000}}}
	s.manifests[s.items[1].url] = manifestEstimate{DurationSeconds: 120}
	s.manifests[s.items[2].url] = manifestEstimate{DurationSeconds: 120}
	s.applyManifestMetadata()
	s.manifests[s.items[0].url] = manifestEstimate{Variants: []manifestVariant{{URL: s.items[2].url, Width: 1280, Height: 720, Bandwidth: 400000}}}
	s.applyManifestMetadata()
	if s.items[0].Height != 720 || s.items[0].Size != 6000000 || s.items[1].Group != "" || s.items[1].Height != 0 || s.items[0].Group != s.items[2].Group {
		t.Fatal("refreshed manifest retained obsolete grouping, resolution, or size")
	}
}

func TestCaptureUsesFullRangeSizeAndKeepsSelectionStable(t *testing.T) {
	s := &Session{requests: make(map[string]request)}
	for _, response := range []string{
		`{"requestId":"small","response":{"url":"https://media.example.test/small.mp4","mimeType":"video/mp4","status":200,"headers":{"Content-Length":"100"}}}`,
		`{"requestId":"large","response":{"url":"https://media.example.test/large.mp4","mimeType":"video/mp4","status":206,"headers":{"content-length":"10","content-range":"bytes 0-9/9000"}}}`,
		`{"requestId":"playlist","response":{"url":"https://media.example.test/master.m3u8","mimeType":"application/vnd.apple.mpegurl","status":200,"headers":{"Content-Length":"40"}}}`,
		`{"requestId":"overflow","response":{"url":"https://media.example.test/overflow.mp4","mimeType":"video/mp4","status":200,"headers":{"Content-Length":"900000000000000000000000"}}}`,
	} {
		s.event(packet{Method: "Network.responseReceived", Params: json.RawMessage(response)})
	}
	items := s.Candidates()
	if len(items) != 4 || items[0].Host != "media.example.test" {
		t.Fatal("media candidates missing")
	}
	// The full resource total, not the fetched range or playlist text, orders files.
	if items[0].Kind != "MP4" || items[0].Size != 9000 || items[0].ID != 1 || items[1].Size != 100 || items[2].Size != 0 || items[3].Size != 0 {
		t.Fatal("expected full file sizes in descending order with stable selection IDs")
	}
}

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
