package app_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestQueueRetriesInOrderAndRecordsOnlyVerifiedOutputs(t *testing.T) {
	firstURL, secondURL := "https://media.example.test/first", "https://media.example.test/second"
	root := t.TempDir()
	first, second := filepath.Join(root, "first.mkv"), filepath.Join(root, "second.mp4")
	downloader := newFakeDownloader()
	downloader.setScripts(
		[]ytdlp.RunEvent{logEvent("ERROR: HTTP Error 503: Service Unavailable"), doneEvent(errors.New("download failed"))},
		[]ytdlp.RunEvent{completedEvent(first), doneEvent(nil)},
		[]ytdlp.RunEvent{completedEvent(second), doneEvent(nil)},
	)
	var runs [][]string
	downloader.onStart = func(args []string) { runs = append(runs, append([]string(nil), args...)) }
	s := newSession(t, script(
		pick("Media URL"), text(firstURL), pick("Video"), pick("Best available"), pick("MKV"), pick("Add to queue"),
		pick("Media URL"), text(secondURL), pick("Video"), pick("Best available"), pick("MP4"), pick("Add to queue"),
		pick("Queue"), pick("Start queue"), pick("Try again"), pick("Quit"),
	), func(o *app.Options) { o.Downloader = downloader })
	s.prompter.autoSource = false
	s.run()
	s.requireScripted()
	if len(runs) != 3 || runs[0][len(runs[0])-1] != firstURL || runs[1][len(runs[1])-1] != firstURL || runs[2][len(runs[2])-1] != secondURL {
		t.Fatal("queue did not retry its current item before advancing")
	}
	if argumentAfter(runs[1], "--remux-video") != "mkv" || argumentAfter(runs[2], "--remux-video") != "mp4" {
		t.Fatal("queue lost reviewed format choices")
	}
	entries, err := (config.HistoryStore{Path: filepath.Join(filepath.Dir(s.store.Path), "history.json")}).Load()
	if err != nil || len(entries) != 2 || entries[0].Path != second || entries[1].Path != first {
		t.Fatal("verified queue outputs were not recorded once in completion order")
	}
}
