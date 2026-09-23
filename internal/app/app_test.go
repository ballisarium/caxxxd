package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/deps"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestSessionOpensWithTheLogoAndAStatusBar(t *testing.T) {
	session := newSession(t, script()).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	// The destination is a temporary path here, so the bar is entitled to
	// shorten it; what it may never do is drop it altogether.
	session.requireDrawn(
		"caxxxd",
		"media downloads without the flag maze",
		"1/5  LINK",
		"yt-dlp ✔",
		"ffmpeg ✔",
		"/downloads",
	)
}

func TestSessionEndsCleanlyWhenTheUserLeaves(t *testing.T) {
	session := newSession(t, script()).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	session.requireDrawn("Bye.")
}

func TestDependencyCheckPassesWhenToolsAreInstalled(t *testing.T) {
	session := newSession(t, script()).run()

	session.requireDrawn("yt-dlp and ffmpeg are ready")
	if session.prompter.questions[0] != "Paste a media URL" {
		t.Fatalf("first question = %q, want the link prompt", session.prompter.questions[0])
	}
}

func TestDependencyCheckExplainsMissingTools(t *testing.T) {
	session := newSession(t, script(pick("Quit")), func(options *app.Options) {
		options.Checker = missingChecker()
	}).run()

	if !errors.Is(session.err, app.ErrDependenciesMissing) {
		t.Fatalf("Run() = %v, want ErrDependenciesMissing", session.err)
	}
	session.requireDrawn(
		"Missing required tools",
		"caxxxd cannot run without them",
		"not found",
		"brew install yt-dlp",
	)
}

func TestDependencyCheckLooksAgainOnDemand(t *testing.T) {
	attempts := 0
	checker := deps.Checker{
		LookPath: func(name string) (string, error) {
			if name == "yt-dlp" {
				attempts++
				if attempts == 1 {
					return "", errors.New("not found")
				}
			}
			return "/opt/homebrew/bin/" + name, nil
		},
		Version: func(context.Context, string) (string, error) { return "2026.01.01", nil },
	}

	session := newSession(t, script(pick("Check again")), func(options *app.Options) {
		options.Checker = checker
	}).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	session.requireDrawn("Missing required tools", "yt-dlp and ffmpeg are ready")
	session.requireScripted()
}

func TestInitialURLIsOfferedAsThePrefilledAnswer(t *testing.T) {
	const link = "https://www.youtube.com/watch?v=abc123"

	session := newSession(t, script(text("")), func(options *app.Options) {
		options.InitialURL = link
	}).run()

	if len(session.prompter.prefills) == 0 || session.prompter.prefills[0] != link {
		t.Fatalf("link prompt prefilled with %v, want %q", session.prompter.prefills, link)
	}
	session.requireDrawn("Example Video")
}

func TestGoingBackToTheLinkOffersTheOneAlreadyLoaded(t *testing.T) {
	const link = "https://www.youtube.com/watch?v=abc123"

	session := newSession(t, script(
		text(""),
		pick("Whole video"),
		pick("‹ Back"),
		pick("‹ Back"),
		text(""),
	), func(options *app.Options) {
		options.InitialURL = link
	})
	session.prompter.autoWholeVideo = false
	session.run()

	if len(session.prompter.prefills) != 2 {
		t.Fatalf("the link was asked %d times, want 2", len(session.prompter.prefills))
	}
	if session.prompter.prefills[1] != link {
		t.Fatalf("second link prompt prefilled with %q, want the loaded link", session.prompter.prefills[1])
	}
	session.requireScripted()
}

func TestEmptyLinkIsRefusedAndAsksAgain(t *testing.T) {
	session := newSession(t, script(text(""))).run()

	session.requireDrawn("Nothing to download")
	if len(session.prompter.prefills) != 2 {
		t.Fatalf("the link was asked %d times, want 2", len(session.prompter.prefills))
	}
}

func TestMediaCardShowsWhatWasFound(t *testing.T) {
	session := newSession(t, script(text("https://www.youtube.com/watch?v=abc123"))).run()

	session.requireDrawn(
		"Example Video",
		"Example Channel",
		"YouTube",
		"2:05",
		"with picture",
		"audio only",
	)
}

func TestMetadataFailureIsClassifiedWithADetailOnDemand(t *testing.T) {
	session := newSession(t,
		script(text("https://www.youtube.com/watch?v=abc123"), pick("Show details"), pick("Quit")),
		func(options *app.Options) {
			options.Client = ytdlp.Client{
				Binary: "yt-dlp",
				Runner: stubRunner{
					payload: fixture(t, "video.json"),
					err:     errors.New("ERROR: Video unavailable"),
				},
			}
		}).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	session.requireDrawn(
		"That link did not resolve",
		"Media is unavailable",
		"cannot be fetched without an account",
		"ERROR: Video unavailable",
	)
	session.requireScripted()
}

func TestMetadataFailureKeepsTheDetailOutOfTheWayUntilAsked(t *testing.T) {
	session := newSession(t,
		script(text("https://www.youtube.com/watch?v=abc123"), pick("Quit")),
		func(options *app.Options) {
			options.Client = ytdlp.Client{
				Binary: "yt-dlp",
				Runner: stubRunner{err: errors.New("ERROR: Video unavailable")},
			}
		}).run()

	session.requireDrawn("Media is unavailable")
	session.requireNotDrawn("ERROR: Video unavailable")
}

func TestAFailedLinkCanBeReplacedWithAnother(t *testing.T) {
	failing := &flakyRunner{payload: fixture(t, "video.json")}

	session := newSession(t,
		script(
			text("https://www.youtube.com/watch?v=abc123"),
			pick("Try another link"),
			text("https://www.youtube.com/watch?v=xyz789"),
		),
		func(options *app.Options) {
			options.Client = ytdlp.Client{Binary: "yt-dlp", Runner: failing}
		}).run()

	if len(session.prompter.prefills) != 2 {
		t.Fatalf("the link was asked %d times, want 2", len(session.prompter.prefills))
	}
	if session.prompter.prefills[1] != "" {
		t.Fatalf("retry link prefilled with %q, want empty", session.prompter.prefills[1])
	}
	session.requireDrawn("Network or site error", "Example Video")
	session.requireScripted()
}

func TestSectionIsShownAndPassedToDownloader(t *testing.T) {
	section := &domain.TimeRange{Start: 30, End: 80}
	session := newSession(t, script(videoPath(pick("Quit"))...), func(options *app.Options) {
		options.Section = section
	}).run()

	if session.err != nil {
		t.Fatalf("Run() = %v, want nil", session.err)
	}
	session.requireDrawn("Time range", "0:30-1:20")
	session.requireNotAsked("Time range")
	session.requireArgs("--download-sections", "*0:30-1:20")
	session.requireScripted()
}

func TestInteractiveWholeVideoKeepsTheFullDownload(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Whole video"),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Download"),
		pick("Quit"),
	))
	session.prompter.autoWholeVideo = false
	session.run()

	session.requireScripted()
	session.requireAsked("Time range")
	session.requireNotDrawn("Time range: ")
	session.requireArgs("-f", "bv*+ba/b")
}

func TestInteractiveRangeIsValidatedShownAndDownloaded(t *testing.T) {
	trimmer := &fakeTrimmer{}
	session := newSession(t, script(
		text(link),
		pick("Choose a range"),
		text("30"),
		text("01:20"),
		pick("Use this clip"),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Download"),
		pick("Quit"),
	), func(options *app.Options) {
		options.Trimmer = trimmer
	})
	session.prompter.autoWholeVideo = false
	session.run()

	session.requireScripted()
	session.requireDrawn("Time range", "0:30-1:20")
	session.requireAsked("Start at")
	session.requireAsked("End at")
	session.requireAsked("Confirm this clip?")
	session.requireDrawn("1 / 3", "2 / 3", "3 / 3", "0:50")
	session.requireArgs("--download-sections", "*0:30-1:20")
	if trimmer.section != (domain.TimeRange{Start: 30, End: 80}) {
		t.Fatalf("trimmer section = %#v, want 30-80", trimmer.section)
	}
}

func TestInteractiveRangeRetriesInvalidInput(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Choose a range"),
		text("not a time"),
		text("00:30"),
		text("20"),
		text("03:00"),
		text("01:20"),
		pick("Use this clip"),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Quit"),
	))
	session.prompter.autoWholeVideo = false
	session.run()

	session.requireScripted()
	session.requireDrawn("Check the time", "section start must be before section end", "section end 3:00 exceeds video duration")
	if session.downloader.runs != 0 {
		t.Fatalf("downloader ran %d times after invalid input, want zero", session.downloader.runs)
	}
}

func TestInteractiveRangeCanBeChangedFromReview(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Choose a range"),
		text("00:30"),
		text("01:20"),
		pick("Use this clip"),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change time range"),
		pick("Choose a range"),
		text("00:40"),
		text("01:10"),
		pick("Use this clip"),
		pick("Download"),
		pick("Quit"),
	))
	session.prompter.autoWholeVideo = false
	session.run()

	session.requireScripted()
	session.requireArgs("--download-sections", "*0:40-1:10")
	if strings.Contains(strings.Join(session.downloader.args, " "), "*0:30-1:20") {
		t.Fatalf("downloader kept the old range: %v", session.downloader.args)
	}
}

func TestInteractiveRangeBackReturnsToTheURL(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("‹ Back"),
		text(link),
		pick("‹ Back"),
	))
	session.prompter.autoWholeVideo = false
	session.run()

	session.requireScripted()
	if count := strings.Count(strings.Join(session.prompter.questions, "\n"), "Paste a media URL"); count != 3 {
		t.Fatalf("URL prompt appeared %d times, want 3 after two range-menu backs", count)
	}
}

func TestCancellingRangeEditsKeepsTheConfirmedClip(t *testing.T) {
	session := newSession(t, script(
		text(link), pick("Choose a range"), text("30"), text("80"), pick("Use this clip"),
		pick("Video"), pick("Best available"), pick("MKV"),
		pick("Change time range"), pick("Choose a range"), text("40"), text("70"),
		pick("Change end"), text("75"), pick("Cancel changes"),
		pick("Download"), pick("Quit"),
	))
	session.prompter.autoWholeVideo = false
	session.run()
	session.requireScripted()
	session.requireArgs("--download-sections", "*0:30-1:20")
}

func TestSectionBeyondVideoDurationStopsBeforeDownload(t *testing.T) {
	session := newSession(t, script(text("https://www.youtube.com/watch?v=abc123")), func(options *app.Options) {
		options.Section = &domain.TimeRange{Start: 120, End: 126}
	}).run()

	if session.err == nil || !strings.Contains(session.err.Error(), "exceeds video duration") {
		t.Fatalf("Run() = %v, want a duration validation error", session.err)
	}
	if session.downloader.runs != 0 {
		t.Fatalf("downloader ran %d times, want zero", session.downloader.runs)
	}
}

func TestUnsupportedURLIsItsOwnCategory(t *testing.T) {
	session := newSession(t,
		script(text("not-a-url"), pick("Quit")),
		func(options *app.Options) {
			options.Client = ytdlp.Client{
				Binary: "yt-dlp",
				Runner: stubRunner{err: errors.New("ERROR: Unsupported URL: not-a-url")},
			}
		}).run()

	session.requireDrawn("Invalid URL", "including https://")
}

func TestEveryFailureCategoryOffersANextStep(t *testing.T) {
	tests := []struct {
		message  string
		category string
		nextStep string
	}{
		{"ERROR: Unsupported URL: x", "Invalid URL", "including https://"},
		{"ERROR: Private video", "Media is unavailable", "without an account"},
		{
			message:  "ERROR: Unable to download webpage: connection refused",
			category: "Network or site error",
			nextStep: "Check your connection",
		},
		{"ERROR: something nobody predicted", "yt-dlp failed", "brew upgrade yt-dlp"},
	}

	for _, test := range tests {
		t.Run(test.category, func(t *testing.T) {
			session := newSession(t,
				script(text("https://example.test/video"), pick("Quit")),
				func(options *app.Options) {
					options.Client = ytdlp.Client{
						Binary: "yt-dlp",
						Runner: stubRunner{err: errors.New(test.message)},
					}
				}).run()

			// A category on its own only names the problem. What makes the
			// report worth showing is the line under it.
			session.requireDrawn(test.category, test.nextStep)
		})
	}
}

// flakyRunner fails the first lookup and answers every later one.
type flakyRunner struct {
	payload []byte
	calls   int
}

func (f *flakyRunner) Output(context.Context, string, ...string) ([]byte, error) {
	f.calls++
	if f.calls == 1 {
		return nil, errors.New("ERROR: unable to download webpage: connection refused")
	}
	return f.payload, nil
}
