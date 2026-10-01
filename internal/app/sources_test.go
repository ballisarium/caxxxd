package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/browser"
)

func TestOpeningMenuExplainsSourcesAndReturnsToDownload(t *testing.T) {
	s := newSession(t, script(pick("Supported sources"), pick("‹ Back"), pick("Media URL"), text(link)))
	s.prompter.autoSource = false
	s.run()
	s.requireScripted()
	s.requireDrawn("YouTube", "HLS", "DASH", "Example Video")
	if s.prompter.questions[0] != "Download from" {
		t.Fatal("the opening screen must offer a source menu")
	}
}

func TestRunningBrowserUsesConnectedSessionAndStableMediaID(t *testing.T) {
	capture := &fakeCapture{items: []browser.Candidate{{Kind: "MP4", Host: "media.example.test", ID: 7, Size: 9000}}}
	connected := false
	s := newSession(t, script(pick("Web page"), text("https://example.test/player"), pick("My running Chrome"), pick("1 · MP4"), pick("‹ Back"), pick("‹ Back"), pick("Quit")), func(o *app.Options) {
		o.ConnectBrowser = func(context.Context, string, int) (browser.Capture, error) { connected = true; return capture, nil }
	})
	s.prompter.autoSource, s.prompter.autoWholeVideo = false, false
	s.run()
	s.requireScripted()
	if !connected || capture.selected != 7 || !capture.closed {
		t.Fatal("running browser selection did not preserve media identity and cleanup")
	}
}

type fakeCapture struct {
	closed   bool
	err      error
	items    []browser.Candidate
	selected int
}

func (f *fakeCapture) Candidates() []browser.Candidate {
	if f.items != nil {
		return f.items
	}
	return []browser.Candidate{{Kind: "HLS", Host: "media.example.test"}}
}
func (f *fakeCapture) Prepare(_ context.Context, id int) (browser.Selection, error) {
	f.selected = id
	return browser.Selection{URL: "https://media.example.test/master.m3u8", ConfigFile: "/private/session/download.conf"}, nil
}
func (f *fakeCapture) Err() error   { return f.err }
func (f *fakeCapture) Close() error { f.closed = true; return nil }

func TestCapturedMediaUsesSameContextForMetadataAndDownload(t *testing.T) {
	capture := &fakeCapture{}
	runner := &cookieRunner{stubRunner: stubRunner{payload: fixture(t, "video.json")}}
	s := newSession(t, script(
		pick("Web page"), text("https://example.test/player"), pick("Separate browser"), pick("1 · HLS"),
		pick("Video"), pick("Best available"), pick("MKV"), pick("Download"), pick("Quit"),
	), func(o *app.Options) {
		o.Client.Runner = runner
		o.OpenBrowser = func(context.Context, string) (browser.Capture, error) { return capture, nil }
	})
	s.prompter.autoSource = false
	s.run()
	s.requireScripted()
	s.requireDrawn("Download complete")
	s.requireNotDrawn("master.m3u8")
	s.requireNotDrawn("download.conf")
	if !capture.closed || argumentAfter(runner.args, "--config-locations") != "/private/session/download.conf" {
		t.Fatal("browser session was not transferred and closed")
	}
	s.requireArgs("--config-locations", "/private/session/download.conf")
}

func TestClosedCaptureReturnsToSourceMenu(t *testing.T) {
	capture := &fakeCapture{err: errors.New("browser closed")}
	s := newSession(t, script(pick("Web page"), text("https://example.test/player"), pick("Separate browser"), pick("Quit")), func(o *app.Options) {
		o.OpenBrowser = func(context.Context, string) (browser.Capture, error) { return capture, nil }
	})
	s.prompter.autoSource = false
	s.run()
	s.requireScripted()
	s.requireDrawn("Browser capture ended")
	if !capture.closed {
		t.Fatal("closed browser was not cleaned up")
	}
}

func TestBackFromCapturedMediaReturnsToTheStreamList(t *testing.T) {
	capture := &fakeCapture{}
	s := newSession(t, script(
		pick("Web page"), text("https://example.test/player"), pick("Separate browser"), pick("1 · HLS"),
		pick("‹ Back"), pick("‹ Back"), pick("Quit"),
	), func(o *app.Options) {
		o.OpenBrowser = func(context.Context, string) (browser.Capture, error) { return capture, nil }
	})
	s.prompter.autoSource, s.prompter.autoWholeVideo = false, false
	s.run()
	s.requireScripted()
	if len(s.prompter.menusFor("Captured media")) != 2 || !capture.closed {
		t.Fatal("Back must return to captured resources, then close the browser")
	}
}

func TestCapturedVariantsExposeMetadataAndSelectTheirStableID(t *testing.T) {
	capture := &fakeCapture{items: []browser.Candidate{
		{ID: 9, Kind: "HLS", Group: "video:1", Height: 1080, Duration: 120, Codecs: "avc1,mp4a", Source: "player 1 · example.test"},
		{ID: 4, Kind: "HLS", Group: "video:1", Height: 720, Duration: 120},
	}}
	s := newSession(t, script(pick("Web page"), text("https://example.test/player"), pick("Separate browser"),
		pick("1 · HLS"), pick("2 · HLS"), pick("‹ Back"), pick("‹ Back"), pick("Quit")), func(o *app.Options) {
		o.OpenBrowser = func(context.Context, string) (browser.Capture, error) { return capture, nil }
	})
	s.prompter.autoSource, s.prompter.autoWholeVideo = false, false
	s.run()
	s.requireScripted()
	if capture.selected != 4 || len(s.prompter.menusFor("Streams for this video")) != 1 {
		t.Fatal("related variants must expose their metadata and preserve the selected resource ID")
	}
	menus := s.prompter.menusFor("Captured media")
	if len(menus) == 0 || !strings.Contains(menus[0][0], "2 streams") {
		t.Fatalf("related streams were not grouped: %#v", menus)
	}
}

func TestFailedCapturedDownloadOffersRecapture(t *testing.T) {
	capture := &fakeCapture{}
	downloader := newFakeDownloader(logEvent("ERROR: HTTP Error 403: Forbidden"), doneEvent(errors.New("download failed")))
	s := newSession(t, script(pick("Web page"), text("https://example.test/player"), pick("Separate browser"),
		pick("1 · HLS"), pick("Video"), pick("Best available"), pick("MKV"), pick("Download"),
		pick("Recapture stream"), pick("‹ Back"), pick("Quit")), func(o *app.Options) {
		o.OpenBrowser = func(context.Context, string) (browser.Capture, error) { return capture, nil }
		o.Downloader = downloader
	})
	s.prompter.autoSource = false
	s.run()
	s.requireScripted()
	if len(s.prompter.menusFor("Captured media")) != 2 || !capture.closed {
		t.Fatal("failed capture must return to its stream picker and release the browser on exit")
	}
}
