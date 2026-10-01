package app_test

import (
	"errors"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
)

func TestCanceledPromptsReturnToTheirOwningScreen(t *testing.T) {
	backText := answer{kind: answerText, err: app.ErrBack}
	backMenu := answer{kind: answerChoice, err: app.ErrBack}
	for _, tc := range []struct {
		name      string
		replies   []answer
		rangeFlow bool
	}{
		{"URL", []answer{pick("Media URL"), backText, pick("Quit")}, false},
		{"web page", []answer{pick("Web page"), backText, pick("Quit")}, false},
		{"browser port", []answer{pick("Web page"), text("https://example.test/player"), pick("Chrome debugging port"), backText, pick("‹ Back"), backText, pick("Quit")}, false},
		{"clip start", []answer{pick("Media URL"), text("https://example.test/video"), pick("Choose a range"), backText, pick("‹ Back"), backText, pick("Quit")}, true},
		{"clip end", []answer{pick("Media URL"), text("https://example.test/video"), pick("Choose a range"), text("0"), backText, backText, pick("‹ Back"), backText, pick("Quit")}, true},
		{"clip confirmation", []answer{pick("Media URL"), text("https://example.test/video"), pick("Choose a range"), text("0"), text("20"), backMenu, pick("‹ Back"), backText, pick("Quit")}, true},
		{"folder", []answer{pick("Media URL"), text("https://example.test/video"), pick("Video"), pick("Best available"), pick("MKV"), pick("Change download folder"), backText, pick("Quit")}, false},
		{"lossy confirmation", []answer{pick("Media URL"), text("https://example.test/video"), pick("Audio"), pick("WAV"), backMenu, pick("MP3"), pick("Quit")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompter := script(tc.replies...)
			prompter.autoSource = false
			prompter.autoWholeVideo = !tc.rangeFlow
			session := newSession(t, prompter).run()
			session.requireScripted()
			if session.downloader.runs != 0 {
				t.Fatal("canceling navigation started a download")
			}
			if session.err != nil {
				t.Fatalf("canceling navigation failed: %v", session.err)
			}
		})
	}
}

func TestEscapeFromDownloadFailureDoesNotRetryTheOperation(t *testing.T) {
	replies := append([]answer{pick("Media URL")}, videoPath(answer{kind: answerChoice, err: app.ErrBack}, pick("Quit"))...)
	prompter := script(replies...)
	prompter.autoSource = false
	downloader := newFakeDownloader(doneEvent(errors.New("fixture download failure")))
	session := newSession(t, prompter, func(o *app.Options) { o.Downloader = downloader }).run()
	session.requireScripted()
	if session.err != nil || downloader.runs != 1 {
		t.Fatalf("canceling the failure ran %d downloads and returned %v", downloader.runs, session.err)
	}
}
