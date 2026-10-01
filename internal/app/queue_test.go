package app

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/ballisarium/caxxxd/internal/browser"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestEnqueueSnapshotsTheReviewedItemAndRetainsItsCapture(t *testing.T) {
	capture := &queueTestCapture{}
	managed := newManagedCapture(capture)
	section := &domain.TimeRange{Start: 12, End: 48}
	expectedManual := manualSelection{videoID: "137", audioID: "140"}
	a := queueTestApp()
	a.url = "https://media.example.test/video-1"
	a.info = ytdlp.MediaInfo{
		ID:        "item-1",
		Title:     "First item",
		Formats:   []ytdlp.Format{{ID: "137", Height: 1080}},
		Subtitles: []ytdlp.SubtitleTrack{{Language: "en", Name: "English", Automatic: true}},
	}
	originalInfo := cloneMediaInfo(a.info)
	a.mode = domain.MediaModeVideo
	a.maxHeight = 1080
	a.container = domain.VideoContainerMP4
	a.audioFormat = domain.AudioFormatM4A
	a.manual = expectedManual
	a.subtitle = ytdlp.SubtitleTrack{Language: "en", Name: "English", Automatic: true}
	a.outputDir = "/downloads/first"
	a.options.Section = section
	a.capture = managed
	a.captureID = 27

	next, err := a.enqueueCurrent()
	if err != nil {
		t.Fatalf("enqueueCurrent() error = %v", err)
	}
	if next != stageCapture {
		t.Fatalf("enqueueCurrent() stage = %v, want stageCapture", next)
	}
	if len(a.queue) != 1 {
		t.Fatalf("queue has %d items, want 1", len(a.queue))
	}

	queued := a.queue[0]
	if queued.url != "https://media.example.test/video-1" {
		t.Fatal("queue did not snapshot the reviewed URL")
	}
	if queued.info.Title != "First item" || queued.mode != domain.MediaModeVideo ||
		queued.maxHeight != 1080 || queued.container != domain.VideoContainerMP4 ||
		queued.audioFormat != domain.AudioFormatM4A || queued.manual != expectedManual {
		t.Fatal("queue did not preserve the reviewed format")
	}
	if queued.subtitle.Language != "en" || queued.outputDir != "/downloads/first" ||
		queued.section == nil || *queued.section != *section || queued.captureID != 27 {
		t.Fatal("queue did not preserve the reviewed item settings")
	}
	if queued.capture == nil || a.capture == nil || queued.capture == managed {
		t.Fatal("queue must own a retained capture lease while the active capture stays open")
	}

	section.End = 99
	queued.info.Formats[0].Height = 720
	if queued.section.End != 48 || originalInfo.Formats[0].Height != 1080 {
		t.Fatal("queued metadata and time range must be independent snapshots")
	}
	if a.info.Title != "" || a.url != "" || a.mode != "" || a.manual.chosen() || a.subtitle != (ytdlp.SubtitleTrack{}) {
		t.Fatal("enqueueCurrent() did not clear the current item's decision state")
	}
	if a.capture == nil || capture.closeCount != 0 {
		t.Fatal("enqueueCurrent() must keep the active browser capture alive")
	}
}

func TestStartNextQueuedTransfersCaptureLeaseWithoutClosingItEarly(t *testing.T) {
	capture := &queueTestCapture{}
	managed := newManagedCapture(capture)
	a := queueTestApp()
	a.capture = managed
	a.url = "https://media.example.test/video-2"
	a.info = ytdlp.MediaInfo{ID: "item-2", Title: "Second item"}
	a.mode = domain.MediaModeAudio
	a.audioFormat = domain.AudioFormatOpus
	a.outputDir = "/downloads/second"
	a.options.Section = &domain.TimeRange{Start: 5, End: 20}
	a.captureID = 42
	if _, err := a.enqueueCurrent(); err != nil {
		t.Fatalf("enqueueCurrent() error = %v", err)
	}

	next, err := a.startNextQueued()
	if err != nil {
		t.Fatalf("startNextQueued() error = %v", err)
	}
	if next != stageDownload || !a.queueRunning {
		t.Fatalf("startNextQueued() = (%v, %v), want stageDownload with queue running", next, a.queueRunning)
	}
	if len(a.queue) != 0 {
		t.Fatalf("queue still has %d items after activation", len(a.queue))
	}
	if capture.closeCount != 0 {
		t.Fatal("the underlying capture closed while the queued item still owned a lease")
	}
	if a.capture == nil || a.url != "https://media.example.test/video-2" ||
		a.info.Title != "Second item" || a.mode != domain.MediaModeAudio ||
		a.audioFormat != domain.AudioFormatOpus || a.outputDir != "/downloads/second" ||
		a.captureID != 42 || a.options.Section == nil || *a.options.Section != (domain.TimeRange{Start: 5, End: 20}) {
		t.Fatal("startNextQueued() did not activate the saved selection")
	}

	if err := a.closeCapture(); err != nil {
		t.Fatalf("closeCapture() error = %v", err)
	}
	if capture.closeCount != 1 {
		t.Fatalf("underlying capture closed %d times, want exactly once", capture.closeCount)
	}
}

func TestStartNextQueuedKeepsOrdinaryURLJobsWithoutACapture(t *testing.T) {
	a := queueTestApp()
	a.url = "https://media.example.test/video-4"
	a.info = ytdlp.MediaInfo{Title: "Ordinary URL"}
	a.mode = domain.MediaModeVideo
	a.captureID = 42
	if _, err := a.enqueueCurrent(); err != nil {
		t.Fatalf("enqueueCurrent() error = %v", err)
	}

	next, err := a.startNextQueued()
	if err != nil {
		t.Fatalf("startNextQueued() error = %v", err)
	}
	if next != stageDownload || a.capture != nil || a.captureID != 0 {
		t.Fatal("ordinary URL queue activation must leave capture nil and the capture ID empty")
	}
}

func TestAskQueueStartsItemsInOrderOnlyAfterTheUserChoosesStart(t *testing.T) {
	a := queueTestApp()
	a.url = "https://media.example.test/video-first"
	a.info = ytdlp.MediaInfo{Title: "First queued item"}
	a.mode = domain.MediaModeVideo
	if _, err := a.enqueueCurrent(); err != nil {
		t.Fatalf("enqueue first item: %v", err)
	}
	a.url = "https://media.example.test/video-second"
	a.info = ytdlp.MediaInfo{Title: "Second queued item"}
	if _, err := a.enqueueCurrent(); err != nil {
		t.Fatalf("enqueue second item: %v", err)
	}
	prompt := &queueTestPrompter{answers: []string{"Start queue"}}
	a.prompt = prompt

	next, err := a.askQueue()
	if err != nil {
		t.Fatalf("askQueue() error = %v", err)
	}
	if next != stageDownload || a.info.Title != "First queued item" || len(a.queue) != 1 {
		t.Fatal("choosing Start queue must activate only the first queued item")
	}

	if _, err := a.startNextQueued(); err != nil {
		t.Fatalf("startNextQueued() error = %v", err)
	}
	if a.info.Title != "Second queued item" || len(a.queue) != 0 {
		t.Fatal("queued items must activate in the order they were added")
	}
}

func TestCloseQueuedCapturesReleasesOnlyPendingLeases(t *testing.T) {
	capture := &queueTestCapture{}
	managed := newManagedCapture(capture)
	a := queueTestApp()
	a.capture = managed
	for index := 0; index < 2; index++ {
		a.url = "https://media.example.test/item"
		a.info = ytdlp.MediaInfo{Title: "Queued"}
		a.captureID = index + 1
		if _, err := a.enqueueCurrent(); err != nil {
			t.Fatalf("enqueueCurrent() error = %v", err)
		}
	}

	if err := a.closeQueuedCaptures(); err != nil {
		t.Fatalf("closeQueuedCaptures() error = %v", err)
	}
	if capture.closeCount != 0 {
		t.Fatal("closing pending queue leases closed the active capture prematurely")
	}
	if len(a.queue) != 0 {
		t.Fatalf("queue has %d entries after cleanup, want 0", len(a.queue))
	}
	if err := a.closeQueuedCaptures(); err != nil {
		t.Fatalf("second closeQueuedCaptures() error = %v", err)
	}
	if err := a.closeCapture(); err != nil {
		t.Fatalf("closeCapture() error = %v", err)
	}
	if capture.closeCount != 1 {
		t.Fatalf("underlying capture closed %d times, want exactly once", capture.closeCount)
	}
}

func TestAskQueueRemovesAnItemOnlyAfterTheUserChoosesRemove(t *testing.T) {
	capture := &queueTestCapture{}
	a := queueTestApp()
	a.capture = newManagedCapture(capture)
	a.url = "https://media.example.test/video-3"
	a.info = ytdlp.MediaInfo{Title: "Queue item"}
	a.mode = domain.MediaModeVideo
	a.container = domain.VideoContainerMKV
	a.captureID = 9
	if _, err := a.enqueueCurrent(); err != nil {
		t.Fatalf("enqueueCurrent() error = %v", err)
	}
	prompt := &queueTestPrompter{answers: []string{"1 · Queue item", "Remove from queue", "‹ Back"}}
	a.prompt = prompt

	next, err := a.askQueue()
	if err != nil {
		t.Fatalf("askQueue() error = %v", err)
	}
	if next != stageCapture {
		t.Fatalf("askQueue() stage = %v, want stageCapture", next)
	}
	if len(a.queue) != 0 {
		t.Fatalf("queue has %d entries after removal, want 0", len(a.queue))
	}
	if capture.closeCount != 0 {
		t.Fatal("removing a pending lease closed the active capture")
	}
	if len(prompt.questions) != 3 || prompt.questions[0] != "Download queue" || prompt.questions[1] != "Queued item" {
		t.Fatalf("queue did not offer inspect and remove screens: %v", prompt.questions)
	}
	if got := prompt.offered[1]; !reflect.DeepEqual(got, []string{"Remove from queue", "‹ Back"}) {
		t.Fatalf("inspect screen offered %v, want remove and back", got)
	}
}

func queueTestApp() *App {
	return &App{console: ui.NewConsole(io.Discard), outputDir: "/downloads"}
}

type queueTestCapture struct{ closeCount int }

func (*queueTestCapture) Candidates() []browser.Candidate { return nil }
func (*queueTestCapture) Prepare(context.Context, int) (browser.Selection, error) {
	return browser.Selection{}, nil
}
func (*queueTestCapture) Err() error { return nil }
func (c *queueTestCapture) Close() error {
	c.closeCount++
	return nil
}

type queueTestPrompter struct {
	answers   []string
	questions []string
	offered   [][]string
}

func (*queueTestPrompter) Text(string, string, string) (string, error) { return "", nil }
func (p *queueTestPrompter) Choose(question string, choices []Choice, _ int) (int, error) {
	p.questions = append(p.questions, question)
	labels := make([]string, len(choices))
	for index, choice := range choices {
		labels[index] = choice.Label
	}
	p.offered = append(p.offered, labels)
	if len(p.answers) == 0 {
		return 0, ErrInterrupted
	}
	wanted := p.answers[0]
	p.answers = p.answers[1:]
	for index, label := range labels {
		if label == wanted {
			return index, nil
		}
	}
	return 0, errNoSuchChoice
}
