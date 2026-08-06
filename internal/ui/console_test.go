package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pterm/pterm"
)

// TestMain turns styling off so the chrome can be asserted as plain text.
// The palette itself is checked directly, where the numbers are.
func TestMain(m *testing.M) {
	pterm.DisableStyling()
	os.Exit(m.Run())
}

func newTestConsole(t *testing.T) (*Console, *bytes.Buffer) {
	t.Helper()

	output := &bytes.Buffer{}
	console := NewConsole(output)
	console.SetWidth(80)
	// A buffer is not a terminal, so the live views would draw nothing at all.
	// Most tests here are about what they would have drawn.
	console.SetAnimated(true)
	return console, output
}

func TestThemeIsBuiltOnInternationalKleinBlue(t *testing.T) {
	theme := NewTheme()

	if theme.Klein.R != 0 || theme.Klein.G != 47 || theme.Klein.B != 167 {
		t.Fatalf("primary accent = rgb(%d, %d, %d), want International Klein Blue rgb(0, 47, 167)",
			theme.Klein.R, theme.Klein.G, theme.Klein.B)
	}
}

func TestTheAccentsAreAllTheSameBlue(t *testing.T) {
	theme := NewTheme()

	// Every accent is Klein lifted for legibility, so blue stays the dominant
	// channel in all of them.
	for name, colour := range map[string]pterm.RGB{
		"Klein": theme.Klein,
		"Lift":  theme.Lift,
		"Glow":  theme.Glow,
		"Mist":  theme.Mist,
	} {
		if colour.B <= colour.R || colour.B <= colour.G {
			t.Fatalf("%s is rgb(%d, %d, %d), which is not a blue", name, colour.R, colour.G, colour.B)
		}
	}
}

func TestConsoleWidthIsClamped(t *testing.T) {
	console, _ := newTestConsole(t)

	console.SetWidth(20)
	if console.Width() != MinWidth {
		t.Fatalf("width = %d, want it raised to %d", console.Width(), MinWidth)
	}

	console.SetWidth(400)
	if console.Width() != MaxWidth {
		t.Fatalf("width = %d, want it capped at %d", console.Width(), MaxWidth)
	}
}

func TestStatusBarShowsTheStepAndTheState(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetStatus(Status{
		Version:     "1.0.0",
		Steps:       5,
		Tools:       "yt-dlp ✔  ffmpeg ✔",
		Destination: "~/Downloads/caxxxd",
	})

	console.Step(2, "Media")

	bar := firstLine(output.String())
	for _, fragment := range []string{"2/5", "MEDIA", "yt-dlp ✔", "~/Downloads/caxxxd"} {
		if !strings.Contains(bar, fragment) {
			t.Fatalf("status bar = %q, want it to mention %q", bar, fragment)
		}
	}
	if visibleWidth(bar) != console.Width() {
		t.Fatalf("status bar is %d columns wide, want %d", visibleWidth(bar), console.Width())
	}
}

func TestStatusBarKeepsTheToolsWhenThePathIsLong(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetStatus(Status{
		Steps:       5,
		Tools:       "yt-dlp ✔  ffmpeg ✔",
		Destination: "/Volumes/An Extremely Long Drive Name/media/incoming/video/downloads",
	})

	console.Step(1, "Link")

	bar := firstLine(output.String())
	if !strings.Contains(bar, "yt-dlp ✔") {
		t.Fatalf("the tools were dropped to fit a path: %q", bar)
	}
	if !strings.Contains(bar, "downloads") {
		t.Fatalf("the end of the path was lost: %q", bar)
	}
	if visibleWidth(bar) != console.Width() {
		t.Fatalf("status bar is %d columns wide, want %d", visibleWidth(bar), console.Width())
	}
}

func TestStatusBarSurvivesANarrowTerminal(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetWidth(MinWidth)
	console.SetStatus(Status{
		Steps:       5,
		Tools:       "yt-dlp ✔  ffmpeg ✔",
		Destination: "/Volumes/Long/media/incoming/downloads",
	})

	console.Step(3, "Format")

	bar := firstLine(output.String())
	if !strings.Contains(bar, "3/5") {
		t.Fatalf("the step itself must always survive: %q", bar)
	}
	if visibleWidth(bar) > console.Width() {
		t.Fatalf("status bar is %d columns wide, want at most %d", visibleWidth(bar), console.Width())
	}
}

func TestStatusBarIsNotRedrawnForTheSameStep(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetStatus(Status{Steps: 5, Destination: "~/Downloads"})

	console.Step(3, "Format")
	console.Step(3, "Format")

	if count := strings.Count(output.String(), "3/5"); count != 1 {
		t.Fatalf("the bar was drawn %d times for one step, want 1", count)
	}
}

func TestStatusBarRedrawsWhenTheDestinationChanges(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetStatus(Status{Steps: 5, Destination: "~/Downloads"})

	console.Step(4, "Review")
	console.SetDestination("~/Movies")
	console.Step(4, "Review")

	if !strings.Contains(output.String(), "~/Movies") {
		t.Fatalf("a changed destination never reached the bar:\n%s", output.String())
	}
}

func TestPanelsKeepTheirBorderWhateverIsInThem(t *testing.T) {
	console, output := newTestConsole(t)

	console.Panel("Ready to download", console.Fields([]Field{
		{Label: "Title", Value: strings.Repeat("a very long title ", 12)},
		{Label: "Destination", Value: "/Volumes/" + strings.Repeat("deep/", 30) + "downloads"},
		{Label: "Mode", Value: "Video"},
	})...)

	for _, line := range strings.Split(strings.TrimRight(output.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		if visibleWidth(line) != console.Width() {
			t.Fatalf("panel line is %d columns wide, want %d:\n%q", visibleWidth(line), console.Width(), line)
		}
	}
}

func TestFieldsAlignTheirValues(t *testing.T) {
	console, _ := newTestConsole(t)

	lines := console.Fields([]Field{
		{Label: "Title", Value: "Example"},
		{Label: "Destination", Value: "~/Downloads"},
	})

	first := strings.Index(lines[0], "Example")
	second := strings.Index(lines[1], "~/Downloads")
	if first != second {
		t.Fatalf("values start at %d and %d, want one column:\n%s", first, second, strings.Join(lines, "\n"))
	}
}

func TestFieldsShortenPathsFromTheFront(t *testing.T) {
	console, _ := newTestConsole(t)

	lines := console.Fields([]Field{
		{Label: "Folder", Value: "/Volumes/" + strings.Repeat("deep/", 30) + "downloads"},
	})

	if !strings.HasSuffix(lines[0], "downloads") {
		t.Fatalf("the end of the path was lost: %q", lines[0])
	}
	if !strings.Contains(lines[0], "…") {
		t.Fatalf("a shortened path should say so: %q", lines[0])
	}
}

func TestResultPanelsAreLabelled(t *testing.T) {
	console, output := newTestConsole(t)

	console.Success("Download complete", "one line")
	console.Notice("About MP4", "another line")
	console.Alert("Invalid URL", "a third line")

	for _, fragment := range []string{
		"✔ Download complete", "• About MP4", "✖ Invalid URL",
		"one line", "another line", "a third line",
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("expected %q in:\n%s", fragment, output.String())
		}
	}
}

func TestBulletsRenderEveryItem(t *testing.T) {
	console, output := newTestConsole(t)

	console.Bullets([]string{"first line", "second line"})

	if !strings.Contains(output.String(), "first line") || !strings.Contains(output.String(), "second line") {
		t.Fatalf("bullets lost an item:\n%s", output.String())
	}
}

func TestLogoNamesTheProduct(t *testing.T) {
	console, output := newTestConsole(t)

	console.Logo("1.2.3")

	if !strings.Contains(output.String(), Tagline) {
		t.Fatalf("the tagline is missing:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "1.2.3") {
		t.Fatalf("the version is missing:\n%s", output.String())
	}
}

func TestWrapNeverExceedsTheLineItIsGiven(t *testing.T) {
	tests := []string{
		"short",
		strings.Repeat("word ", 40),
		strings.Repeat("x", 200),
		"label   " + strings.Repeat("y", 120),
	}

	for _, text := range tests {
		for _, line := range wrap(text, 30) {
			if visibleWidth(line) > 30 {
				t.Fatalf("wrap produced a %d column line from %q", visibleWidth(line), text)
			}
		}
	}
}

func TestTruncatePathKeepsTheTail(t *testing.T) {
	got := truncatePath("/Users/someone/Movies/incoming/downloads", 20)

	if visibleWidth(got) != 20 {
		t.Fatalf("truncatePath = %q, %d columns, want 20", got, visibleWidth(got))
	}
	if !strings.HasSuffix(got, "downloads") {
		t.Fatalf("truncatePath = %q, want it to end at the folder", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("truncatePath = %q, want it to say what it dropped", got)
	}
}

func TestTheDownloadLineKeepsMovingWithoutNewSamples(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	// ffmpeg reports nothing while it merges, so this is the whole of what the
	// view has to work with for as long as that takes.
	progress.Show(Sample{Detail: "Merging streams and writing metadata"})
	time.Sleep(spinnerDelay * 4)
	progress.Stop()

	frames := 0
	for _, frame := range spinnerFrames {
		if strings.Contains(output.String(), frame) {
			frames++
		}
	}
	if frames < 2 {
		t.Fatalf("the line drew %d frames while nothing changed, want it to keep moving", frames)
	}
}

func TestTheDownloadLineNeverLeavesAnOlderVersionOfItself(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	// Switching between the spinner and the bar is normal: the size becomes
	// known, then a merge starts. None of it may stack up.
	progress.Show(Sample{Detail: "1.0 MiB downloaded, size unknown"})
	progress.Show(Sample{Known: true, Fraction: 0.4, Detail: "4.0 MiB / 10.0 MiB"})
	progress.Show(Sample{Detail: "Merging streams and writing metadata"})
	progress.Stop()

	if strings.Contains(output.String(), "\n") {
		t.Fatalf("the download view broke onto a second line:\n%q", output.String())
	}
}

func TestALongWaitIsCountedSoItLooksSlowRatherThanStuck(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	progress.Show(Sample{Detail: "Merging streams and writing metadata"})
	time.Sleep(patienceDelay + spinnerDelay*3)
	progress.Stop()

	if !strings.Contains(output.String(), "  ·  ") {
		t.Fatalf("a message that sat still for %v never said how long:\n%q", patienceDelay, output.String())
	}
}

func TestTheBarReportsAPercentage(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	progress.Show(Sample{Known: true, Fraction: 0.635, Detail: "6.3 MiB / 10.0 MiB"})
	progress.Stop()

	if !strings.Contains(output.String(), "64%") {
		t.Fatalf("the bar did not report where it had got to:\n%q", output.String())
	}
}

func TestProgressShowsBothHalvesOfADownload(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	progress.Show(Sample{Detail: "1.0 MiB downloaded, size unknown"})
	progress.Show(Sample{Known: true, Fraction: 0.5, Detail: "2.0 MiB / 4.0 MiB"})
	progress.Stop()

	screen := output.String()
	if !strings.Contains(screen, "size unknown") {
		t.Fatalf("the waiting line never appeared:\n%s", screen)
	}
	if !strings.Contains(screen, "2.0 MiB / 4.0 MiB") {
		t.Fatalf("the bar never showed its numbers:\n%s", screen)
	}
}

func TestProgressRestartsForASecondStream(t *testing.T) {
	console, output := newTestConsole(t)
	progress := console.Progress()

	progress.Show(Sample{Known: true, Fraction: 0.9, Detail: "part 1"})
	progress.Show(Sample{Known: true, Fraction: 0.1, Detail: "part 2"})
	progress.Stop()

	if !strings.Contains(output.String(), "part 2") {
		t.Fatalf("the bar did not restart for the second stream:\n%s", output.String())
	}
}

func TestScreenClearsOnATerminal(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetClearScreens(true)

	console.Screen()

	if !strings.HasPrefix(output.String(), "\x1b[H\x1b[2J") {
		t.Fatalf("a new screen should start by wiping the last one, got %q", output.String())
	}
}

func TestScreenLeavesAPlainStreamAlone(t *testing.T) {
	console, output := newTestConsole(t)

	// A buffer is not a terminal, so there is nothing to clear and escape
	// codes would only end up in the middle of the text.
	console.Screen()

	if output.String() != "" {
		t.Fatalf("a plain stream was written to on a screen change: %q", output.String())
	}
}

func TestANewScreenRedrawsTheStatusBar(t *testing.T) {
	console, output := newTestConsole(t)
	console.SetStatus(Status{Steps: 5, Destination: "~/Downloads"})

	console.Step(3, "Format")
	console.Screen()
	console.Step(3, "Format")

	// Within one screen an unchanged bar is not repeated; across a wipe it has
	// to come back, because nothing survives the wipe.
	if count := strings.Count(output.String(), "3/5"); count != 2 {
		t.Fatalf("the bar was drawn %d times across two screens, want 2", count)
	}
}

func TestSpinnerResolvesIntoOneLine(t *testing.T) {
	console, output := newTestConsole(t)

	spinner := console.Spinner("Looking for yt-dlp")
	spinner.Done("yt-dlp is ready")

	if !strings.Contains(output.String(), "✔ yt-dlp is ready") {
		t.Fatalf("the spinner did not resolve:\n%q", output.String())
	}
}

func TestSpinnerFailureIsMarkedDifferently(t *testing.T) {
	console, output := newTestConsole(t)

	console.Spinner("Reading the page").Fail("that link did not resolve")

	if !strings.Contains(output.String(), "✖ that link did not resolve") {
		t.Fatalf("a failed spinner should be marked as one:\n%q", output.String())
	}
}

func TestStoppingASpinnerTwiceIsSafe(t *testing.T) {
	console, _ := newTestConsole(t)

	spinner := console.Spinner("Working")
	spinner.Stop()
	spinner.Stop()
}

// firstLine is the first non-empty line of what was drawn.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

func TestNothingDriftsRightWhileTheTerminalIsRaw(t *testing.T) {
	console, output := newTestConsole(t)

	// Raw mode turns off the translation that returns the carriage, so a line
	// drawn without one leaves the next thing starting where it ended.
	console.setRaw(true)
	console.Hint("Ctrl+U clears the line")
	console.setRaw(false)

	if !strings.HasSuffix(output.String(), "\r\n") {
		t.Fatalf("a line drawn in raw mode must carry its own carriage return: %q", output.String())
	}
}

func TestOrdinaryLinesDoNotCarryACarriageReturn(t *testing.T) {
	console, output := newTestConsole(t)

	console.Hint("Ctrl+U clears the line")

	if strings.Contains(output.String(), "\r") {
		t.Fatalf("a line outside raw mode needs no carriage return: %q", output.String())
	}
}

func TestTruncatePathSurvivesWideCharacters(t *testing.T) {
	// The tail is measured in columns. Taking it by rune count reads before
	// the start of the string as soon as a path holds anything wider than one
	// column, which is a panic rather than a short line.
	tests := []struct {
		path  string
		width int
	}{
		{"/" + strings.Repeat("漢", 30), 40},
		{"/Users/someone/" + strings.Repeat("動画", 10) + "/downloads", 24},
		{strings.Repeat("🎬", 20), 10},
		{strings.Repeat("漢", 5), 2},
	}

	for _, test := range tests {
		got := truncatePath(test.path, test.width)
		if visibleWidth(got) > test.width {
			t.Fatalf("truncatePath(%q, %d) = %q, %d columns wide",
				test.path, test.width, got, visibleWidth(got))
		}
	}
}

func TestTruncatePathKeepsAsMuchOfTheTailAsFits(t *testing.T) {
	got := truncatePath("/Users/someone/"+strings.Repeat("動画", 10)+"/downloads", 24)

	if !strings.HasSuffix(got, "downloads") {
		t.Fatalf("truncatePath = %q, want it to end at the folder", got)
	}
}

func TestLiveViewsDrawNothingIntoATranscript(t *testing.T) {
	// A file keeps every frame an animation ever drew: a spinner would land in
	// it as the same sentence over and over. The outcome still gets printed.
	console, output := newTestConsole(t)
	console.SetAnimated(false)

	progress := console.Progress()
	progress.Show(Sample{Known: true, Fraction: 0.5, Detail: "2.0 MiB / 4.0 MiB"})
	progress.Stop()

	spinner := console.Spinner("Looking for yt-dlp and ffmpeg")
	spinner.Done("yt-dlp and ffmpeg are ready")

	if strings.Contains(output.String(), "2.0 MiB / 4.0 MiB") {
		t.Fatalf("the progress bar drew into a transcript:\n%q", output.String())
	}
	if strings.Contains(output.String(), "Looking for") {
		t.Fatalf("the spinner drew into a transcript:\n%q", output.String())
	}
	if !strings.Contains(output.String(), "yt-dlp and ffmpeg are ready") {
		t.Fatalf("the outcome should still be reported:\n%q", output.String())
	}
}
