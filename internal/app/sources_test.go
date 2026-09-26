package app_test

import (
	"context"
	"errors"
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

type fakeCapture struct {
	closed bool
	err    error
}

func (f *fakeCapture) Candidates() []browser.Candidate {
	return []browser.Candidate{{Kind: "HLS", Host: "media.example.test"}}
}
func (f *fakeCapture) Prepare(context.Context, int) (browser.Selection, error) {
	return browser.Selection{URL: "https://media.example.test/master.m3u8", ConfigFile: "/private/session/download.conf"}, nil
}
func (f *fakeCapture) Err() error   { return f.err }
func (f *fakeCapture) Close() error { f.closed = true; return nil }

func TestCapturedMediaUsesSameContextForMetadataAndDownload(t *testing.T) {
	capture := &fakeCapture{}
	runner := &cookieRunner{stubRunner: stubRunner{payload: fixture(t, "video.json")}}
	s := newSession(t, script(
		pick("Web page"), text("https://example.test/player"), pick("1 · HLS"),
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
	s := newSession(t, script(pick("Web page"), text("https://example.test/player"), pick("Quit")), func(o *app.Options) {
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
		pick("Web page"), text("https://example.test/player"), pick("1 · HLS"),
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
