package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// outcome is how a run of yt-dlp ended, from the user's point of view.
type outcome int

const (
	outcomeDone outcome = iota
	outcomeCancelled
	outcomeFailed
)

// runDownload turns the selection into a real file, or into an explanation.
func (a *App) runDownload(ctx context.Context) (stage, error) {
	request := a.request()
	var subtitleDirectory string
	cleanupSubtitles := func() error {
		if subtitleDirectory == "" {
			return nil
		}
		if err := os.RemoveAll(subtitleDirectory); err != nil {
			return fmt.Errorf("remove temporary subtitles: %w", err)
		}
		subtitleDirectory = ""
		return nil
	}
	if a.mode == domain.MediaModeSubtitles {
		if err := os.MkdirAll(a.outputDir, 0o755); err != nil {
			return a.reportFailure(
				classifyDownloadError(&outputDirError{err: err}, nil),
				recovery{"Back to the review", "choose another folder", stageReview},
			)
		}
		var err error
		subtitleDirectory, err = os.MkdirTemp(a.outputDir, ".caxxxd-subtitles-*")
		if err != nil {
			return a.reportFailure(
				classifyDownloadError(&outputDirError{err: err}, nil),
				recovery{"Back to the review", "choose another folder", stageReview},
			)
		}
		request.OutputDir = subtitleDirectory
	}

	args, err := ytdlp.BuildCommand(request)
	if err != nil {
		if cleanupErr := cleanupSubtitles(); cleanupErr != nil {
			return a.reportFailure(
				classifyTranscriptError(cleanupErr),
				recovery{"Back to the review", "pick something else", stageReview},
			)
		}
		return a.reportFailure(Failure{
			Category: FailureTool,
			NextStep: "That selection cannot be turned into a download: " + err.Error(),
		}, recovery{"Back to the review", "pick something else", stageReview})
	}

	a.logs = nil
	a.completedPath = ""
	a.failure = Failure{}

	// The destination is created here rather than when it was typed: a folder
	// is only worth making once something is actually going into it.
	if err := os.MkdirAll(a.outputDir, 0o755); err != nil {
		cleanupErr := cleanupSubtitles()
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		return a.reportFailure(
			classifyDownloadError(&outputDirError{err: err}, nil),
			recovery{"Back to the review", "choose another folder", stageReview},
		)
	}

	a.savePreferences()

	a.console.Accent(ui.Truncate(a.info.Title, a.console.Width()-2))
	a.console.Hint("Ctrl+C stops the download, and the ffmpeg it started with it.")
	result, runErr := a.stream(ctx, args)

	switch result {
	case outcomeCancelled:
		if a.mode == domain.MediaModeSubtitles {
			body := []string{"No transcript was written."}
			if cleanupErr := cleanupSubtitles(); cleanupErr != nil {
				body = append(body, cleanupErr.Error())
			}
			a.carry("Subtitle download cancelled", body...)
			return stageReview, nil
		}
		// yt-dlp writes to a .part file and is not told otherwise, so what it
		// had downloaded is still on disk. Saying it was discarded would be a
		// tidier sentence and a false one.
		a.carry("Download cancelled",
			"What had already downloaded is left in the destination as a .part file.",
			"Starting the same download again picks up from there.")
		return stageReview, nil

	case outcomeFailed:
		if cleanupErr := cleanupSubtitles(); cleanupErr != nil {
			a.appendLog(cleanupErr.Error())
		}
		if ctx.Err() != nil {
			// The whole program is going down, not just this download.
			return stageReview, ctx.Err()
		}
		a.failure = classifyDownloadError(runErr, a.logs)
		return a.reportFailure(a.failure,
			recovery{"Try again", "run the same download once more", stageDownload},
			recovery{"Change something", "go back to the review", stageReview},
		)

	default:
		if a.mode == domain.MediaModeSubtitles {
			runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			err := a.finishTranscript(runCtx, subtitleDirectory)
			conversionCancelled := runCtx.Err() != nil && ctx.Err() == nil
			stop()
			cleanupErr := cleanupSubtitles()
			if ctx.Err() != nil {
				return stageReview, ctx.Err()
			}
			if conversionCancelled {
				body := []string{"No transcript was written."}
				if cleanupErr != nil {
					body = append(body, cleanupErr.Error())
				}
				a.carry("Subtitle conversion cancelled", body...)
				return stageReview, nil
			}
			if err != nil {
				a.failure = classifyTranscriptError(err)
				return a.reportFailure(a.failure,
					recovery{"Try again", "download the same subtitle track again", stageDownload},
					recovery{"Change something", "go back to the review", stageReview},
				)
			}
			if cleanupErr != nil {
				a.failure = classifyTranscriptError(cleanupErr)
				return a.reportFailure(a.failure,
					recovery{"Try cleanup again", "download and replace the same transcript", stageDownload},
					recovery{"Change something", "go back to the review", stageReview},
				)
			}
		} else if a.options.Section != nil {
			sectionOutcome, sectionErr := a.trimSection(ctx)
			switch sectionOutcome {
			case outcomeCancelled:
				a.carry("Clipping cancelled",
					"The downloaded file was left in place and was not replaced.",
					"Finish the clip from the download step when you are ready.")
				return stageReview, nil
			case outcomeFailed:
				if ctx.Err() != nil {
					return stageReview, ctx.Err()
				}
				a.failure = classifyProcessingError(sectionErr)
				return a.reportFailure(a.failure,
					recovery{"Try again", "download and finalize the same section again", stageDownload},
					recovery{"Change something", "go back to the review", stageReview},
				)
			}
		}
		return a.reportSuccess()
	}
}

func (a *App) finishTranscript(ctx context.Context, directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read downloaded subtitles: %w", err)
	}

	var source string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".srt") {
			continue
		}
		if source != "" {
			return errors.New("yt-dlp produced more than one subtitle file for the selected track")
		}
		source = filepath.Join(directory, entry.Name())
	}
	if source == "" {
		return errors.New("yt-dlp did not produce an SRT subtitle file")
	}

	name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)) + ".txt"
	destination := filepath.Join(a.outputDir, name)
	if err := a.options.TranscriptConverter.Convert(
		ctx,
		source,
		destination,
		a.subtitle.Automatic,
		a.options.Section,
	); err != nil {
		return err
	}
	a.completedPath = destination
	return nil
}

// trimSection repairs the timestamps left by independent remote DASH streams.
// yt-dlp still performs the network-side range seek; this local pass only
// reads the short merged result and copies it into place atomically.
func (a *App) trimSection(ctx context.Context) (outcome, error) {
	if a.completedPath == "" {
		return outcomeFailed, errors.New("ffmpeg could not finalize clipped file: yt-dlp did not report an output path")
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		<-runCtx.Done()
		stop()
	}()

	spinner := a.console.Spinner("Normalizing the clipped file")
	err := a.options.Trimmer.Trim(runCtx, a.completedPath, *a.options.Section)
	if runCtx.Err() != nil && ctx.Err() == nil {
		spinner.Stop()
		return outcomeCancelled, runCtx.Err()
	}
	if err != nil {
		spinner.Fail("Could not finalize the clipped file")
		return outcomeFailed, err
	}
	spinner.Done("Clipped file is ready")
	return outcomeDone, nil
}

// stream runs yt-dlp and draws its progress until it stops.
//
// The run gets its own context so Ctrl+C reaches the process group — yt-dlp
// and the ffmpeg it spawned — instead of only this program. Once the first
// interrupt has been handled the signal goes back to the operating system, so
// a second Ctrl+C always gets someone out.
func (a *App) stream(ctx context.Context, args []string) (outcome, error) {
	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		<-runCtx.Done()
		stop()
	}()

	events, err := a.options.Downloader.Start(runCtx, args)
	if err != nil {
		return outcomeFailed, &startError{err: err}
	}

	progress := a.console.Progress()
	defer progress.Stop()

	var (
		sample    ytdlp.Progress
		phase     string
		partsDone int
	)

	for event := range events {
		switch {
		case event.Done:
			progress.Stop()
			return a.classifyOutcome(ctx, runCtx, event.Err), event.Err

		case event.Log != "":
			a.appendLog(event.Log)

		default:
			switch event.Parsed.Kind {
			case ytdlp.EventProgress:
				sample = event.Parsed.Progress
				phase = sample.Status
				// A merged download fetches video and audio one after the
				// other, so the bar legitimately restarts. Counting the
				// finished streams is what lets the line say so.
				if sample.Status == "finished" {
					partsDone++
				}
				progress.Show(progressSample(sample, phase, partsDone))

			case ytdlp.EventPostProcess:
				// ffmpeg reports nothing while it works, so this stays on
				// screen for as long as the merge takes. The view animates and
				// counts the wait rather than freezing on its last frame.
				phase = "postprocess"
				detail := "Merging streams and writing metadata"
				if a.mode == domain.MediaModeSubtitles {
					detail = "Preparing subtitles"
				}
				progress.Show(ui.Sample{Detail: detail})

			case ytdlp.EventCompletedFile:
				a.completedPath = event.Parsed.FilePath
			}
		}
	}

	// The runner always sends a terminal event before closing, so arriving
	// here means there is simply nothing left to report.
	progress.Stop()
	return a.classifyOutcome(ctx, runCtx, nil), nil
}

// classifyOutcome separates a failure from a download the user stopped.
func (a *App) classifyOutcome(ctx, runCtx context.Context, err error) outcome {
	switch {
	case ctx.Err() != nil:
		return outcomeFailed
	case runCtx.Err() != nil, errors.Is(err, context.Canceled):
		return outcomeCancelled
	case err != nil:
		return outcomeFailed
	default:
		return outcomeDone
	}
}

// progressSample turns one yt-dlp sample into the line under the bar.
//
// The separators are deliberately plain: pterm measures the text around the
// bar in bytes, so a line full of multi-byte glyphs would shorten the bar it
// is supposed to describe.
func progressSample(progress ytdlp.Progress, phase string, partsDone int) ui.Sample {
	total, known := progress.Total()

	// A stream that has just finished has nothing left to estimate. Reporting
	// a speed and an unknown time next to a full bar reads like a stall, which
	// is exactly what it is not.
	if phase == "finished" {
		detail := "stream complete"
		if progress.DownloadedBytes > 0 {
			detail = ui.FormatBytes(progress.DownloadedBytes) + " | " + detail
		}
		return ui.Sample{Known: known, Fraction: 1, Detail: detail}
	}

	// What has arrived is counted, not reported: at the start of a download
	// that is a genuine zero, and "?" would suggest the opposite.

	parts := []string{}
	if known {
		parts = append(parts, ui.FormatCounted(progress.DownloadedBytes)+" / "+ui.FormatBytes(total))
	} else {
		parts = append(parts, ui.FormatCounted(progress.DownloadedBytes)+" downloaded, size unknown")
	}
	// FormatSpeed says "—" when there is nothing to report, which is right in
	// a table cell and wrong on a line that has room to say what it means.
	speed := "speed unknown"
	if progress.SpeedBytes > 0 {
		speed = ui.FormatSpeed(progress.SpeedBytes)
	}
	parts = append(parts, speed, etaLabel(progress))

	if label := phaseLabel(phase, partsDone); label != "" {
		parts = append(parts, label)
	}

	return ui.Sample{
		Known:    known,
		Fraction: progress.Fraction(),
		Detail:   strings.Join(parts, " | "),
	}
}

func etaLabel(progress ytdlp.Progress) string {
	switch {
	case !progress.ETAKnown:
		return "time left unknown"
	case progress.ETASeconds <= 0:
		return "almost done"
	default:
		return ui.FormatETA(progress.ETASeconds) + " left"
	}
}

// phaseLabel names the stream being fetched, but only once there is more than
// one of them: "part 1" on a single-file download would be noise.
func phaseLabel(phase string, partsDone int) string {
	switch {
	case phase == "":
		return "starting"
	case phase == "downloading" && partsDone > 0:
		return "part " + strconv.Itoa(partsDone+1)
	case phase == "downloading":
		return ""
	default:
		return phase
	}
}

// savePreferences remembers the choices the user just confirmed.
func (a *App) savePreferences() {
	preferences := a.currentPreferences()
	if err := a.options.ConfigStore.Save(preferences); err != nil {
		a.carryHint("Preferences could not be saved for next time.")
		return
	}
	a.preferences = preferences
}

// reportSuccess shows where the file landed and what can be done next.
func (a *App) reportSuccess() (stage, error) {
	fields := []ui.Field{}
	if a.completedPath != "" {
		// Fields fits both of these to the panel on its own, shortening a
		// path from its front and a name from its end.
		fields = append(fields,
			ui.Field{Label: "File", Value: filepath.Base(a.completedPath)},
			ui.Field{Label: "Folder", Value: collapseHome(filepath.Dir(a.completedPath), a.options.Home)},
		)
	} else {
		// yt-dlp reported no final path, which happens when the file was
		// already on disk. Point at the folder rather than invent a name.
		fields = append(fields, ui.Field{Label: "Folder", Value: collapseHome(a.outputDir, a.options.Home)})
	}

	for {
		// Redrawn on every pass so that revealing the file, which has something
		// of its own to say, does not stack up under the menu.
		a.screen(stageDownload)
		a.console.Success("Download complete", a.console.Fields(fields)...)

		picked, err := a.prompt.Choose("What now?", []Choice{
			{Label: "Download another", Detail: "start again from a new link"},
			{Label: "Reveal in Finder", Detail: revealDetail(a.completedPath)},
			quitChoice,
		}, 0)
		if err != nil {
			return stageLink, err
		}

		switch picked {
		case 0:
			a.resetForNextDownload()
			return stageLink, nil
		case 1:
			a.reveal()
		default:
			return stageLink, errQuit
		}
	}
}

// revealDetail says what Finder will actually be pointed at.
func revealDetail(path string) string {
	if path == "" {
		return "open the folder it landed in"
	}
	return "show the file in Finder"
}

// reveal shows the finished file, or the folder when there is no file to name.
func (a *App) reveal() {
	target := a.completedPath
	if target == "" {
		target = a.outputDir
	}
	if target == "" {
		return
	}
	if err := a.options.RevealFile(target); err != nil {
		a.carryHint("Could not open Finder. The file is still where it says above.")
	}
}

// recovery is one way out of a failure, offered as a menu row.
type recovery struct {
	label  string
	detail string
	next   stage
}

// reportFailure explains what went wrong and offers the ways forward. The raw
// output is kept behind a choice: it is the second question someone asks, not
// the first.
func (a *App) reportFailure(failure Failure, ways ...recovery) (stage, error) {
	failure.Detail = ytdlp.SafeDiagnostic(failure.Detail)
	category := failure.Category
	if category == FailureNone {
		category = FailureTool
	}
	a.failure = failure

	fallback := stageLink
	if len(ways) > 0 {
		fallback = ways[0].next
	}

	details := false
	for {
		// The failure keeps its own screen: asking for the raw output redraws
		// it with the output in place, rather than below the menu that offered it.
		a.screen(a.current)
		a.console.Alert(string(category), failure.NextStep)
		if details {
			a.console.Bullets(a.detailLines())
		}

		choices := make([]Choice, 0, len(ways)+2)
		for _, way := range ways {
			choices = append(choices, Choice{Label: way.label, Detail: way.detail})
		}
		choices = append(choices,
			Choice{Label: detailsLabel(details), Detail: "the raw output caxxxd kept"},
			quitChoice,
		)

		picked, err := a.prompt.Choose("What now?", choices, 0)
		if err != nil {
			return fallback, err
		}

		switch {
		case picked < len(ways):
			return ways[picked].next, nil
		case picked == len(ways):
			details = !details
		default:
			return fallback, errQuit
		}
	}
}

func detailsLabel(shown bool) string {
	if shown {
		return "Hide details"
	}
	return "Show details"
}

// detailLines is the raw diagnostic tail kept for the details section.
func (a *App) detailLines() []string {
	if len(a.logs) > 0 {
		return a.logs
	}
	if a.failure.Detail == "" {
		return []string{"No diagnostic output was captured."}
	}
	return strings.Split(a.failure.Detail, "\n")
}
