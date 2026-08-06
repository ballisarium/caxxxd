package ui

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// spinnerFrames is the animation the spinner and the waiting line share.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerDelay is how long one frame stays on screen.
const spinnerDelay = 90 * time.Millisecond

// Spinner is a line that animates while something slow happens and resolves
// into a single ✔ or ✖, so a finished step reads like a finished panel.
//
// It is caxxxd's own rather than pterm's: pterm's spinner writes IsActive from
// Stop while its animation goroutine reads it, and the race detector is right
// to object. Nothing else may draw while a spinner is running — every caller
// resolves it first — and that is what keeps one goroutine on the writer.
type Spinner struct {
	console *Console

	mutex   sync.Mutex
	text    string
	stop    chan struct{}
	stopped chan struct{}
}

// Spinner starts a spinner with text next to it.
func (c *Console) Spinner(text string) *Spinner {
	spinner := &Spinner{
		console: c,
		text:    text,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	if !c.animates {
		// Nothing to animate into: a transcript would keep every frame. The
		// ✔ or ✖ this resolves into still says what happened.
		close(spinner.stopped)
		return spinner
	}

	c.hideCursor()
	go spinner.animate()
	return spinner
}

func (s *Spinner) animate() {
	defer close(s.stopped)

	ticker := time.NewTicker(spinnerDelay)
	defer ticker.Stop()

	for frame := 0; ; frame++ {
		s.draw(frame)
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
	}
}

func (s *Spinner) draw(frame int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	glyph := s.console.theme.Glow.Sprint(spinnerFrames[frame%len(spinnerFrames)])
	s.console.rewrite(" " + glyph + " " + s.console.theme.Ink.Sprint(s.text))
}

// Update replaces the message next to the spinner.
func (s *Spinner) Update(text string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.text = text
}

// Done stops the spinner and reports the step as finished.
func (s *Spinner) Done(text string) {
	s.Stop()
	s.console.line(" " + s.console.theme.Success.Sprint("✔ ") + s.console.theme.Ink.Sprint(text))
	s.console.line("")
}

// Fail stops the spinner and reports the step as failed.
func (s *Spinner) Fail(text string) {
	s.Stop()
	s.console.line(" " + s.console.theme.Danger.Sprint("✖ ") + s.console.theme.Ink.Sprint(text))
	s.console.line("")
}

// Stop erases the spinner without saying anything about the outcome. It waits
// for the animation to end, so on return the caller owns the writer again.
func (s *Spinner) Stop() {
	select {
	case <-s.stopped:
		return
	default:
	}

	close(s.stop)
	<-s.stopped
	s.console.clearLine()
	s.console.showCursor()
}

// patienceDelay is how long a message stays unchanged before the line starts
// saying how long it has been there. It is the point at which someone watching
// begins to wonder whether anything is still happening.
const patienceDelay = 2 * time.Second

// barCells is how many columns the bar itself occupies.
const barCells = 24

// Sample is one refresh of the download view.
type Sample struct {
	// Known says whether a total size has been reported yet.
	Known bool
	// Fraction is completion between 0 and 1, only meaningful when Known.
	Fraction float64
	// Detail is the line of numbers or words shown next to the bar.
	Detail string
}

// Progress is the download's single line: a Klein bar once a total size is
// known, a spinner until then, and the numbers next to either.
//
// It repaints on a timer as well as on new samples. yt-dlp goes quiet for as
// long as ffmpeg takes to merge a file, and a line that only moves when a
// sample arrives would spend that whole time looking like it had crashed.
// One line is redrawn in place throughout, so switching between the bar and
// the spinner never leaves an older version of itself on screen.
type Progress struct {
	console *Console

	mutex  sync.Mutex
	sample Sample
	shown  bool
	since  time.Time
	frame  int

	stop    chan struct{}
	stopped chan struct{}
}

// Progress opens the download view. Nothing is drawn until the first Show.
func (c *Console) Progress() *Progress {
	progress := &Progress{
		console: c,
		since:   time.Now(),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	if !c.animates {
		close(progress.stopped)
		return progress
	}

	c.hideCursor()
	go progress.animate()
	return progress
}

func (p *Progress) animate() {
	defer close(p.stopped)

	ticker := time.NewTicker(spinnerDelay)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.draw(true)
		}
	}
}

// Show reports the latest sample and redraws immediately, rather than leaving
// the news until the next tick.
func (p *Progress) Show(sample Sample) {
	p.mutex.Lock()
	if sample.Detail != p.sample.Detail {
		p.since = time.Now()
	}
	p.sample = sample
	p.shown = true
	p.mutex.Unlock()

	p.draw(false)
}

// Stop clears the line and waits for the animation to end, so on return the
// caller owns the writer again.
func (p *Progress) Stop() {
	select {
	case <-p.stopped:
		return
	default:
	}

	close(p.stop)
	<-p.stopped
	p.console.clearLine()
	p.console.showCursor()
}

func (p *Progress) draw(animating bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.shown || !p.console.animates {
		return
	}
	if animating {
		p.frame++
	}
	p.console.rewrite(p.render())
}

// render is the whole line: the bar or a spinner, then what it is doing.
func (p *Progress) render() string {
	detail := p.sample.Detail
	if waited := time.Since(p.since); waited >= patienceDelay {
		// Nothing has changed for a while. Saying how long makes a slow merge
		// look slow rather than stuck.
		detail += "  ·  " + FormatETA(int64(waited.Seconds()))
	}

	if !p.sample.Known {
		glyph := p.console.theme.Glow.Sprint(spinnerFrames[p.frame%len(spinnerFrames)])
		return " " + glyph + "  " + p.console.theme.Ink.Sprint(truncateVisible(detail, p.console.width-5))
	}

	percent := strconv.Itoa(clamp(int(p.sample.Fraction*100+0.5), 0, 100)) + "%"
	room := p.console.width - barCells - visibleWidth(percent) - 7

	return " " + p.bar() + "  " +
		p.console.theme.Glow.Sprint(padLeft(percent, 4)) + "  " +
		p.console.theme.Ink.Sprint(truncateVisible(detail, room))
}

// bar draws the filled part in Klein, with a brighter cell at its head so the
// edge of the progress stays visible against the dark fill.
func (p *Progress) bar() string {
	filled := clamp(int(p.sample.Fraction*barCells+0.5), 0, barCells)

	switch filled {
	case 0:
		return p.console.theme.Slate.Sprint(strings.Repeat("░", barCells))
	case barCells:
		return p.console.theme.Klein.Sprint(strings.Repeat("█", barCells-1)) +
			p.console.theme.Glow.Sprint("█")
	default:
		return p.console.theme.Klein.Sprint(strings.Repeat("█", filled-1)) +
			p.console.theme.Glow.Sprint("█") +
			p.console.theme.Slate.Sprint(strings.Repeat("░", barCells-filled))
	}
}
