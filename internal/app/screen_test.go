package app_test

import (
	"strings"
	"testing"
)

func TestEveryStepGetsItsOwnScreen(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Quit"))...)).onATerminal().run()

	session.requireScripted()

	// The dependency check, the link, the media choice, the quality, the
	// container, the review, and the download each own a screen.
	if screens := len(session.screens()); screens < 7 {
		t.Fatalf("the session drew %d screens for 7 decisions:\n%s", screens, session.screen())
	}

	// Nothing may pile up: no screen carries two status bars.
	for index, screen := range session.screens() {
		if bars := strings.Count(screen, "▌ "); bars > 1 {
			t.Fatalf("screen %d shows %d steps at once:\n%s", index, bars, screen)
		}
	}
}

func TestTheMediaStaysInViewWhileItIsBeingDecidedAbout(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Quit"))...)).onATerminal().run()

	// The card is redrawn on the media, quality, and container screens rather
	// than scrolling away above them.
	cards := 0
	for _, screen := range session.screens() {
		if strings.Contains(screen, "Example Channel") {
			cards++
		}
	}
	if cards < 3 {
		t.Fatalf("the media card survived on %d screens, want the ones deciding about it", cards)
	}
}

func TestTheReviewScreenDropsTheMediaCardForItsOwnSummary(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Quit"))...)).onATerminal().run()

	for _, screen := range session.screens() {
		if !strings.Contains(screen, "Ready to download") {
			continue
		}
		if strings.Contains(screen, "Example Channel") {
			t.Fatalf("the review repeats what its own panel already says:\n%s", screen)
		}
		return
	}
	t.Fatal("no review screen was drawn")
}

func TestANoticeFoundAtTheEndOfAStepIsShownOnTheNextOne(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MP4"),
		pick("Quit"),
	)).onATerminal().run()

	session.requireScripted()

	for _, screen := range session.screens() {
		if !strings.Contains(screen, "About MP4") {
			continue
		}
		// The note was discovered on the container screen, which is gone by
		// the time it can be read, so it belongs to the review.
		if !strings.Contains(screen, "Ready to download") {
			t.Fatalf("the note was left on a screen that is not the review:\n%s", screen)
		}
		return
	}
	t.Fatal("the MP4 note was lost when its screen was cleared")
}

func TestARefusedFolderIsExplainedOnTheScreenThatFollows(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change download folder"),
		text("somewhere/relative"),
		pick("Quit"),
	)).onATerminal().run()

	session.requireScripted()

	last := session.screens()[len(session.screens())-1]
	if !strings.Contains(last, "not a folder caxxxd can use") {
		t.Fatalf("the refusal never reached the screen that asks again:\n%s", last)
	}
	if !strings.Contains(last, "Ready to download") {
		t.Fatalf("the refusal was not shown with the review it returns to:\n%s", last)
	}
}

func TestTheLogoOpensASessionAndEveryNewItem(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Download another"))...)).onATerminal().run()

	// Once for the dependency check, then each source and link screen.
	logos := 0
	for _, screen := range session.screens() {
		if strings.Contains(screen, "media downloads without the flag maze") {
			logos++
		}
	}
	if logos != 5 {
		t.Fatalf("the logo was drawn on %d screens, want the opening one and both source/link pairs", logos)
	}
}

func TestTheDownloadResultReplacesTheProgressItFollows(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Quit"))...)).onATerminal().run()

	last := session.screens()[len(session.screens())-1]
	if !strings.Contains(last, "Download complete") {
		t.Fatalf("the result screen is not the last one:\n%s", last)
	}
	if strings.Contains(last, "MiB /") {
		t.Fatalf("the finished result still carries the progress line:\n%s", last)
	}
}

func TestFailureDetailsRedrawTheFailureRatherThanStackUnderIt(t *testing.T) {
	session := newSession(t, script(
		text("https://example.test/video"),
		pick("Show details"),
		pick("Hide details"),
		pick("Quit"),
	), failingClient(t)).onATerminal().run()

	session.requireScripted()

	// Both the shown and the hidden state are whole screens of their own, each
	// with exactly one copy of the failure on it.
	for index, screen := range session.screens() {
		if bars := strings.Count(screen, "Media is unavailable"); bars > 1 {
			t.Fatalf("screen %d repeats the failure %d times:\n%s", index, bars, screen)
		}
	}

	menus := session.prompter.menusFor("What now?")
	if len(menus) != 3 {
		t.Fatalf("the failure asked %d times, want three", len(menus))
	}
	if !strings.Contains(strings.Join(menus[1], " "), "Hide details") {
		t.Fatalf("the second menu should offer to hide what the first showed: %v", menus[1])
	}
}

func TestAPipeIsNeverCleared(t *testing.T) {
	// A session whose output is not a terminal keeps everything, because there
	// is no screen to wipe — only a file or a pipe that would end up with
	// escape codes in the middle of it.
	session := newSession(t, script(videoPath(pick("Quit"))...)).run()

	if strings.Contains(session.screen(), clearScreen) {
		t.Fatal("clear-screen codes were written into a plain stream")
	}
	if len(session.screens()) != 1 {
		t.Fatalf("a pipe was split into %d screens", len(session.screens()))
	}
}
