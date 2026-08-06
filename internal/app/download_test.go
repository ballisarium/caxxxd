package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// downloadSession scripts an ordinary video download over a given event stream.
func downloadSession(t *testing.T, downloader *fakeDownloader, extra ...answer) *session {
	t.Helper()

	return newSession(t, script(videoPath(extra...)...), func(options *app.Options) {
		options.Downloader = downloader
	})
}

func TestDownloadReportsProgressAndSucceeds(t *testing.T) {
	completed := "Example Video [abc123].mkv"
	downloader := newFakeDownloader(
		progressEvent(1_000_000, 4_000_000, 500_000, 6),
		progressEvent(2_000_000, 4_000_000, 500_000, 4),
		completedEvent(filepath.Join(t.TempDir(), completed)),
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireScripted()
	session.requireDrawn(
		"Download complete",
		completed,
		"1.9 MiB / 3.8 MiB",
		"488.3 KiB/s",
		"4s left",
	)
}

func TestDownloadCreatesTheOutputDirectory(t *testing.T) {
	session := downloadSession(t, newFakeDownloader(doneEvent(nil)), pick("Quit")).run()

	if _, err := os.Stat(session.outputDir); err != nil {
		t.Fatalf("the destination was not created: %v", err)
	}
	session.requireArgs("-P", session.outputDir)
}

func TestAnEstimatedTotalIsStillATotal(t *testing.T) {
	downloader := newFakeDownloader(
		estimatedProgressEvent(1_000_000, 8_000_000),
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireDrawn("976.6 KiB / 7.6 MiB")
	session.requireNotDrawn("size unknown")
}

func TestAnUnknownTotalIsSaidPlainly(t *testing.T) {
	downloader := newFakeDownloader(
		ytdlp.RunEvent{Parsed: ytdlp.Event{
			Kind:     ytdlp.EventProgress,
			Progress: ytdlp.Progress{Status: "downloading", DownloadedBytes: 3_000_000},
		}},
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireDrawn("2.9 MiB downloaded, size unknown", "time left unknown")
}

func TestPostProcessingIsNamed(t *testing.T) {
	downloader := newFakeDownloader(
		progressEvent(4_000_000, 4_000_000, 500_000, 0),
		postProcessEvent(),
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireDrawn("Merging streams and writing metadata")
}

func TestASectionIsNormalizedAfterTheDownloadCompletes(t *testing.T) {
	section := &domain.TimeRange{Start: 30, End: 80}
	trimmer := &fakeTrimmer{}
	session := newSession(t, script(videoPath(pick("Quit"))...), func(options *app.Options) {
		options.Section = section
		options.Trimmer = trimmer
	}).run()

	if trimmer.calls != 1 {
		t.Fatalf("trimmer calls = %d, want one", trimmer.calls)
	}
	if trimmer.section != *section {
		t.Fatalf("trimmed section = %#v, want %#v", trimmer.section, *section)
	}
	if filepath.Base(trimmer.path) != "Example Video.mkv" {
		t.Fatalf("trimmed path = %q, want the downloaded file", trimmer.path)
	}
	session.requireDrawn("Clipped file is ready", "Download complete")
}

func TestSectionTrimFailureDoesNotReportASuccess(t *testing.T) {
	trimmer := &fakeTrimmer{err: errors.New("ffmpeg exited with status 1")}
	session := newSession(t, script(videoPath(pick("Show details"), pick("Quit"))...), func(options *app.Options) {
		options.Section = &domain.TimeRange{Start: 30, End: 80}
		options.Trimmer = trimmer
	}).run()

	session.requireDrawn("Clipping failed", "ffmpeg exited with status 1")
	session.requireNotDrawn("Download complete")
}

func TestAMergedDownloadNamesItsSecondStream(t *testing.T) {
	downloader := newFakeDownloader(
		progressEvent(4_000_000, 4_000_000, 500_000, 0),
		finishedStreamEvent(),
		progressEvent(500_000, 2_000_000, 400_000, 3),
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireDrawn("part 2")
}

func TestASingleStreamIsNeverCalledPartOne(t *testing.T) {
	downloader := newFakeDownloader(
		progressEvent(1_000_000, 4_000_000, 500_000, 6),
		doneEvent(nil),
	)

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireNotDrawn("part 1")
}

func TestDownloadFailureIsClassifiedWithDetailsOnDemand(t *testing.T) {
	downloader := newFakeDownloader(
		logEvent("ERROR: unable to download video data: HTTP Error 403: Forbidden"),
		doneEvent(errors.New("exit status 1")),
	)

	session := downloadSession(t, downloader, pick("Show details"), pick("Quit")).run()

	session.requireScripted()
	session.requireDrawn(
		"Network or site error",
		"HTTP Error 403: Forbidden",
	)
}

func TestDownloadKeepsOnlyTheDiagnosticTail(t *testing.T) {
	events := []ytdlp.RunEvent{}
	for line := 1; line <= 30; line++ {
		events = append(events, logEvent("line "+strconv.Itoa(line)))
	}
	events = append(events, doneEvent(errors.New("exit status 1")))

	session := downloadSession(t, newFakeDownloader(events...), pick("Show details"), pick("Quit")).run()

	session.requireDrawn("line 30", "line 11")
	session.requireNotDrawn("line 10 ")
}

func TestOutputDirectoryFailureIsItsOwnCategory(t *testing.T) {
	// A file where the folder should go is the simplest way to make the
	// destination unusable without depending on permissions.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change download folder"),
		text(filepath.Join(blocked, "downloads")),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireDrawn("Output directory error", "could not use that folder")
}

func TestAFailureToStartIsReported(t *testing.T) {
	downloader := newFakeDownloader()
	downloader.startErr = errors.New("fork/exec yt-dlp: no such file or directory")

	session := downloadSession(t, downloader, pick("Quit")).run()

	session.requireDrawn("yt-dlp failed")
}

func TestARetryRunsTheDownloadAgain(t *testing.T) {
	downloader := newFakeDownloader()
	downloader.setScripts(
		[]ytdlp.RunEvent{doneEvent(errors.New("exit status 1"))},
		[]ytdlp.RunEvent{completedEvent("/tmp/Example Video.mkv"), doneEvent(nil)},
	)

	session := downloadSession(t, downloader, pick("Try again"), pick("Quit")).run()

	session.requireScripted()
	if downloader.runs != 2 {
		t.Fatalf("yt-dlp ran %d times, want 2", downloader.runs)
	}
	session.requireDrawn("Download complete")
}

func TestAFailureCanBeAnsweredByChangingSomething(t *testing.T) {
	downloader := newFakeDownloader(doneEvent(errors.New("exit status 1")))

	session := downloadSession(t, downloader, pick("Change something")).run()

	session.requireScripted()
	session.requireAsked("Start?")
}

func TestCancellationIsNotAFailure(t *testing.T) {
	downloader := newFakeDownloader(doneEvent(context.Canceled))

	session := downloadSession(t, downloader).run()

	// yt-dlp is not told --no-part, so a cancelled download really does leave
	// its .part file behind. The message has to match the disk.
	session.requireDrawn("Download cancelled", ".part file")
	session.requireNotDrawn("discarded")
	session.requireNotDrawn("yt-dlp failed")
	session.requireAsked("Start?")
}

func TestDownloadRemembersThePreferences(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("WebM"),
		pick("Download"),
		pick("Quit"),
	)).run()

	saved, err := session.store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if saved.VideoContainer != domain.VideoContainerWebM {
		t.Fatalf("saved container = %q, want webm", saved.VideoContainer)
	}
	if saved.OutputDir != session.outputDir {
		t.Fatalf("saved output dir = %q, want %q", saved.OutputDir, session.outputDir)
	}
}

func TestAudioPreferencesAreRememberedToo(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Audio"),
		pick("Opus"),
		pick("Download"),
		pick("Quit"),
	)).run()

	saved, err := session.store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if saved.AudioFormat != domain.AudioFormatOpus {
		t.Fatalf("saved audio format = %q, want opus", saved.AudioFormat)
	}
	if saved.VideoContainer != domain.VideoContainerMKV {
		t.Fatalf("an audio download changed the video container to %q", saved.VideoContainer)
	}
}

func TestSuccessWithoutAReportedPathPointsAtTheFolder(t *testing.T) {
	session := downloadSession(t, newFakeDownloader(doneEvent(nil)), pick("Quit")).run()

	session.requireDrawn("Download complete", "Folder")
	session.requireNotDrawn("File ")
}

func TestRevealFailureIsNotFatal(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Reveal in Finder"), pick("Quit"))...),
		func(options *app.Options) {
			options.RevealFile = func(string) error { return errors.New("no Finder here") }
		}).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	session.requireScripted()
	session.requireDrawn("Could not open Finder")
}

func TestDownloadingAnotherStartsFromAnEmptyLink(t *testing.T) {
	session := newSession(t, script(videoPath(pick("Download another"))...)).run()

	if len(session.prompter.prefills) != 2 {
		t.Fatalf("the link was asked %d times, want 2", len(session.prompter.prefills))
	}
	if session.prompter.prefills[1] != "" {
		t.Fatalf("the second link prompt offered %q, want an empty field", session.prompter.prefills[1])
	}
}

func TestTheDownloadStepNamesWhatIsRunning(t *testing.T) {
	session := downloadSession(t, newFakeDownloader(doneEvent(nil)), pick("Quit")).run()

	session.requireDrawn("5/5  DOWNLOAD", "Example Video")
}

func TestAFailedRunKeepsTheLogsOutOfTheNextOne(t *testing.T) {
	downloader := newFakeDownloader()
	downloader.setScripts(
		[]ytdlp.RunEvent{logEvent("ERROR: first attempt exploded"), doneEvent(errors.New("exit status 1"))},
		[]ytdlp.RunEvent{doneEvent(errors.New("exit status 1"))},
	)

	session := downloadSession(t, downloader, pick("Try again"), pick("Show details"), pick("Quit")).run()

	session.requireScripted()

	// Only the second failure's details were ever asked for, so the first
	// run's output must not appear anywhere on screen.
	session.requireNotDrawn("first attempt exploded")
	session.requireDrawn("exit status 1")
}
