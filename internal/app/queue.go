package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ballisarium/caxxxd/internal/browser"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

var errCaptureLeaseClosed = errors.New("browser capture lease is closed")

// captureReferences holds a browser session until its current selection and
// every queued selection have released their own lease.
type captureReferences struct {
	mu      sync.Mutex
	capture browser.Capture
	refs    int
	closed  bool
}

// managedCapture is one ownership lease on a shared browser capture. Each
// retained lease has its own Close guard, so transferring one queue entry
// cannot release another entry's ownership.
type managedCapture struct {
	references *captureReferences
	once       sync.Once
	mu         sync.Mutex
	released   bool
}

func newManagedCapture(capture browser.Capture) *managedCapture {
	if capture == nil {
		return nil
	}
	return &managedCapture{references: &captureReferences{capture: capture, refs: 1}}
}

func (capture *managedCapture) retain() *managedCapture {
	if capture == nil || capture.references == nil {
		return nil
	}
	capture.mu.Lock()
	if capture.released {
		capture.mu.Unlock()
		return nil
	}
	state := capture.references
	state.mu.Lock()
	if state.closed || state.capture == nil {
		state.mu.Unlock()
		capture.mu.Unlock()
		return nil
	}
	state.refs++
	state.mu.Unlock()
	capture.mu.Unlock()
	return &managedCapture{references: state}
}

func (capture *managedCapture) underlying() (browser.Capture, error) {
	if capture == nil || capture.references == nil {
		return nil, errCaptureLeaseClosed
	}
	capture.mu.Lock()
	if capture.released {
		capture.mu.Unlock()
		return nil, errCaptureLeaseClosed
	}
	state := capture.references
	state.mu.Lock()
	capture.mu.Unlock()
	defer state.mu.Unlock()
	if state.closed || state.capture == nil {
		return nil, errCaptureLeaseClosed
	}
	return state.capture, nil
}

func (capture *managedCapture) Candidates() []browser.Candidate {
	underlying, err := capture.underlying()
	if err != nil {
		return nil
	}
	return underlying.Candidates()
}

func (capture *managedCapture) Prepare(ctx context.Context, id int) (browser.Selection, error) {
	underlying, err := capture.underlying()
	if err != nil {
		return browser.Selection{}, err
	}
	return underlying.Prepare(ctx, id)
}

func (capture *managedCapture) Err() error {
	underlying, err := capture.underlying()
	if err != nil {
		return err
	}
	return underlying.Err()
}

func (capture *managedCapture) Close() error {
	if capture == nil || capture.references == nil {
		return nil
	}
	var closeErr error
	capture.once.Do(func() {
		capture.mu.Lock()
		if capture.released {
			capture.mu.Unlock()
			return
		}
		capture.released = true
		state := capture.references
		state.mu.Lock()
		capture.mu.Unlock()
		if state.refs > 0 {
			state.refs--
		}
		if state.refs == 0 && !state.closed {
			state.closed = true
			underlying := state.capture
			state.capture = nil
			state.mu.Unlock()
			if underlying != nil {
				closeErr = underlying.Close()
			}
			return
		}
		state.mu.Unlock()
	})
	return closeErr
}

type queuedDownload struct {
	url         string
	info        ytdlp.MediaInfo
	mode        domain.MediaMode
	maxHeight   int
	container   domain.VideoContainer
	audioFormat domain.AudioFormat
	manual      manualSelection
	subtitle    ytdlp.SubtitleTrack
	outputDir   string
	section     *domain.TimeRange
	captureID   int
	capture     *managedCapture
}

// enqueueCurrent saves the reviewed item, then returns to the source that can
// provide another one. The active capture remains open while its queue lease
// keeps the session available for later downloads.
func (a *App) enqueueCurrent() (stage, error) {
	if a.url == "" {
		return a.currentQueueSource(), errors.New("cannot queue an item without a media URL")
	}
	capture, err := a.retainCurrentCapture()
	if err != nil {
		return a.currentQueueSource(), err
	}
	captureID := a.captureID
	if capture == nil {
		captureID = 0
	}

	section := cloneTimeRange(a.options.Section)
	a.queue = append(a.queue, queuedDownload{
		url:         a.url,
		info:        cloneMediaInfo(a.info),
		mode:        a.mode,
		maxHeight:   a.maxHeight,
		container:   a.container,
		audioFormat: a.audioFormat,
		manual:      a.manual,
		subtitle:    a.subtitle,
		outputDir:   a.outputDir,
		section:     section,
		captureID:   captureID,
		capture:     capture,
	})

	a.url = ""
	a.info = ytdlp.MediaInfo{}
	a.captureConfig = ""
	a.captureID = 0
	a.mode = ""
	a.maxHeight = 0
	a.subtitle = ytdlp.SubtitleTrack{}
	a.clearManualSelection()
	if !a.sectionFixed {
		a.options.Section = nil
	}
	return a.currentQueueSource(), nil
}

func (a *App) retainCurrentCapture() (*managedCapture, error) {
	if a.capture == nil {
		return nil, nil
	}
	managed, ok := a.capture.(*managedCapture)
	if !ok {
		managed = newManagedCapture(a.capture)
		a.capture = managed
	}
	retained := managed.retain()
	if retained == nil {
		return nil, errCaptureLeaseClosed
	}
	return retained, nil
}

func (a *App) currentQueueSource() stage {
	if a.capture != nil {
		return stageCapture
	}
	return stageSource
}

// askQueue lets the user start reviewed items, inspect an item's settings, or
// remove that item. Queue entries are only downloaded after Start is chosen.
func (a *App) askQueue() (stage, error) {
	firstPass := true
	for {
		if !firstPass {
			a.screen(stageQueue)
		}
		firstPass = false

		choices := make([]Choice, 0, len(a.queue)+2)
		startIndex := -1
		if len(a.queue) > 0 {
			startIndex = len(choices)
			choices = append(choices, Choice{
				Label:  "Start queue",
				Detail: fmt.Sprintf("download %d reviewed item(s) in order", len(a.queue)),
			})
		} else {
			a.console.Hint("No queued downloads.")
		}
		itemStart := len(choices)
		for index, item := range a.queue {
			choices = append(choices, Choice{
				Label:  fmt.Sprintf("%d · %s", index+1, ui.Truncate(queueTitle(item.info), a.console.Width()-16)),
				Detail: queueDescription(item),
			})
		}
		backIndex := len(choices)
		choices = append(choices, backChoice)

		picked, err := a.prompt.Choose("Download queue", choices, 0)
		if err != nil {
			return stageQueue, err
		}
		if picked == startIndex && startIndex >= 0 {
			return a.startNextQueued()
		}
		if picked == backIndex {
			return a.currentQueueSource(), nil
		}
		itemIndex := picked - itemStart
		if itemIndex < 0 || itemIndex >= len(a.queue) {
			return stageQueue, errNoSuchChoice
		}

		item := a.queue[itemIndex]
		a.console.Panel("Queued item", a.console.Fields(queueFields(item, a.options.Home))...)
		action, err := a.prompt.Choose("Queued item", []Choice{
			{Label: "Remove from queue", Detail: "discard this reviewed selection"},
			backChoice,
		}, 0)
		if err != nil {
			return stageQueue, err
		}
		if action == 1 {
			continue
		}
		if action != 0 {
			return stageQueue, errNoSuchChoice
		}
		a.queue = append(a.queue[:itemIndex], a.queue[itemIndex+1:]...)
		if item.capture != nil {
			if err := item.capture.Close(); err != nil {
				a.carry("Browser cleanup failed", err.Error())
			}
		}
	}
}

// startNextQueued removes and activates the first item. The queue's capture
// lease transfers to App, while the previous active lease is released.
func (a *App) startNextQueued() (stage, error) {
	if len(a.queue) == 0 {
		a.queueRunning = false
		return a.currentQueueSource(), nil
	}

	item := a.queue[0]
	a.queue[0] = queuedDownload{}
	a.queue = a.queue[1:]
	if err := a.closeCapture(); err != nil {
		a.carry("Browser cleanup failed", err.Error())
	}

	a.url = item.url
	a.info = cloneMediaInfo(item.info)
	a.mode = item.mode
	a.maxHeight = item.maxHeight
	a.container = item.container
	a.audioFormat = item.audioFormat
	a.manual = item.manual
	a.subtitle = item.subtitle
	a.outputDir = item.outputDir
	a.options.Section = cloneTimeRange(item.section)
	a.captureID = item.captureID
	a.captureConfig = ""
	a.capture = nil
	if item.capture != nil {
		a.capture = item.capture
	}
	a.completedPath = ""
	a.failure = Failure{}
	a.logs = nil
	a.queueRunning = true
	return stageDownload, nil
}

func (a *App) closeQueuedCaptures() error {
	var closeErrors []error
	for index := range a.queue {
		if a.queue[index].capture != nil {
			if err := a.queue[index].capture.Close(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		a.queue[index] = queuedDownload{}
	}
	a.queue = nil
	return errors.Join(closeErrors...)
}

func cloneMediaInfo(info ytdlp.MediaInfo) ytdlp.MediaInfo {
	info.Formats = append([]ytdlp.Format(nil), info.Formats...)
	info.Subtitles = append([]ytdlp.SubtitleTrack(nil), info.Subtitles...)
	return info
}

func cloneTimeRange(section *domain.TimeRange) *domain.TimeRange {
	if section == nil {
		return nil
	}
	copy := *section
	return &copy
}

func queueTitle(info ytdlp.MediaInfo) string {
	title := strings.TrimSpace(info.Title)
	if title == "" {
		return "Untitled media"
	}
	return title
}

func queueDescription(item queuedDownload) string {
	parts := []string{queueModeLabel(item.mode)}
	switch item.mode {
	case domain.MediaModeVideo:
		if item.manual.chosen() {
			parts = append(parts, "manual streams")
		} else {
			parts = append(parts, qualityLabel(item.maxHeight), strings.ToUpper(string(item.container)))
		}
	case domain.MediaModeAudio:
		parts = append(parts, audioFormatLabel(item.audioFormat))
	case domain.MediaModeSubtitles:
		parts = append(parts, subtitleLabel(item.subtitle))
	}
	if item.section != nil {
		parts = append(parts, item.section.String())
	}
	return strings.Join(parts, " · ")
}

func queueModeLabel(mode domain.MediaMode) string {
	switch mode {
	case domain.MediaModeVideo:
		return "Video"
	case domain.MediaModeAudio:
		return "Audio"
	case domain.MediaModeSubtitles:
		return "Subtitles"
	default:
		return "Media"
	}
}

func subtitleLabel(track ytdlp.SubtitleTrack) string {
	name := track.Name
	if name == "" {
		name = track.Language
	}
	if name == "" {
		name = "selected track"
	}
	if track.Automatic {
		return name + " · automatic"
	}
	return name + " · uploaded"
}

func queueFields(item queuedDownload, home string) []ui.Field {
	fields := []ui.Field{{Label: "Title", Value: item.info.Title}, {Label: "Selection", Value: queueDescription(item)}}
	if item.section != nil {
		fields = append(fields, ui.Field{Label: "Time range", Value: item.section.String()})
	}
	if item.outputDir != "" {
		fields = append(fields, ui.Field{Label: "Folder", Value: collapseHome(item.outputDir, home)})
	}
	return fields
}
