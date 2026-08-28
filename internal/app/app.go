// Package app is caxxxd's workflow: one linear conversation that walks the
// user from a URL to a finished file. It owns no process and no file handling
// of its own; those live in the deps, ytdlp, and config packages, and every
// pixel of it is drawn by the ui package.
package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/deps"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// ErrDependenciesMissing ends the run when yt-dlp or ffmpeg is not installed.
// The reason has already been shown by the time it is returned.
var ErrDependenciesMissing = errors.New("required tools are missing")

// errQuit unwinds the step machine when the user asks to leave. It never
// reaches the caller of Run.
var errQuit = errors.New("quit")

// maxDetailLines is how much raw diagnostic output an error keeps.
const maxDetailLines = 20

// Downloader is the part of ytdlp.Downloader the flow depends on. Keeping it
// an interface is what lets tests drive the whole download lifecycle from a
// fake event stream instead of a real process.
type Downloader interface {
	Start(ctx context.Context, args []string) (<-chan ytdlp.RunEvent, error)
}

// Trimmer is the local post-processing step used to normalize a downloaded
// time range without re-encoding its streams.
type Trimmer interface {
	Trim(ctx context.Context, path string, section domain.TimeRange) error
}

// TranscriptConverter turns yt-dlp's temporary subtitle file into the final
// plain-text output.
type TranscriptConverter interface {
	Convert(ctx context.Context, source, destination string, automatic bool, section *domain.TimeRange) error
}

// Options carries the flow's dependencies. Anything left zero-valued is filled
// in with the real implementation, so tests inject only what they care about.
type Options struct {
	InitialURL          string
	Section             *domain.TimeRange
	Version             string
	Checker             deps.Checker
	Client              ytdlp.Client
	Downloader          Downloader
	Trimmer             Trimmer
	TranscriptConverter TranscriptConverter
	ConfigStore         config.Store
	RevealFile          func(string) error
	Home                string
	Console             *ui.Console
	Prompter            Prompter
}

// manualSelection is an exact pick from the stream table. A muxed stream needs
// nothing else; a video-only stream is always paired with an audio one.
type manualSelection struct {
	videoID string
	audioID string
	muxedID string
}

func (m manualSelection) chosen() bool {
	return m.videoID != "" || m.muxedID != "" || m.audioID != ""
}

// App is one caxxxd session: the current selection and the services it needs.
type App struct {
	options Options
	console *ui.Console
	prompt  Prompter

	dependencies deps.Status
	preferences  config.Config
	outputDir    string

	url  string
	info ytdlp.MediaInfo

	sectionFixed bool
	rangeReturn  stage
	rangeRetry   bool

	mode        domain.MediaMode
	maxHeight   int
	container   domain.VideoContainer
	audioFormat domain.AudioFormat
	manual      manualSelection
	subtitle    ytdlp.SubtitleTrack

	logs          []string
	failure       Failure
	completedPath string
	initialURL    string

	current stage
	pending []message
}

// message is something worth saying that was discovered while a screen was
// already on its way out. Screens are cleared between steps, so anything found
// at the end of one has to be carried to the top of the next.
type message struct {
	title string
	body  []string
	quiet bool
}

// carry queues a panel for the next screen.
func (a *App) carry(title string, body ...string) {
	a.pending = append(a.pending, message{title: title, body: body})
}

// carryHint queues a single quiet line for the next screen.
func (a *App) carryHint(text string) {
	a.pending = append(a.pending, message{title: text, quiet: true})
}

// deliver prints what the last screen left to say, at the top of this one.
func (a *App) deliver() {
	for _, pending := range a.pending {
		if pending.quiet {
			a.console.Hint(pending.title)
			a.console.Blank()
			continue
		}
		a.console.Notice(pending.title, pending.body...)
	}
	a.pending = nil
}

// New builds a session.
func New(options Options) *App {
	options = withDefaults(options)
	preferences := config.Default()

	return &App{
		options:      options,
		console:      options.Console,
		prompt:       options.Prompter,
		preferences:  preferences,
		outputDir:    preferences.OutputDir,
		container:    preferences.VideoContainer,
		audioFormat:  preferences.AudioFormat,
		initialURL:   strings.TrimSpace(options.InitialURL),
		sectionFixed: options.Section != nil,
		rangeReturn:  stageMode,
	}
}

func withDefaults(options Options) Options {
	if options.Checker.LookPath == nil || options.Checker.Version == nil {
		options.Checker = deps.NewChecker()
	}
	if options.Client.Runner == nil {
		options.Client = ytdlp.NewClient(binaryOr(options.Client.Binary))
	}
	if options.Downloader == nil {
		options.Downloader = ytdlp.Downloader{Binary: "yt-dlp"}
	}
	if options.Trimmer == nil {
		options.Trimmer = ytdlp.Trimmer{Binary: "ffmpeg"}
	}
	if options.TranscriptConverter == nil {
		options.TranscriptConverter = ytdlp.TranscriptConverter{}
	}
	if options.ConfigStore.Path == "" {
		options.ConfigStore = config.NewStore(defaultConfigPath())
	}
	if options.RevealFile == nil {
		options.RevealFile = revealInFileBrowser
	}
	if options.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			options.Home = home
		}
	}
	if options.Console == nil {
		options.Console = ui.NewConsole(os.Stdout)
	}
	if options.Prompter == nil {
		options.Prompter = NewTerminalPrompter(options.Console)
	}
	return options
}

func binaryOr(binary string) string {
	if binary == "" {
		return "yt-dlp"
	}
	return binary
}

func defaultConfigPath() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		directory = os.TempDir()
	}
	return filepath.Join(directory, "caxxxd", "config.json")
}

// revealInFileBrowser shows the finished file in Finder. Like every other
// subprocess caxxxd starts, it runs under a context so it cannot hang.
func revealInFileBrowser(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "open", "-R", path).Run()
}

// Run draws the session and walks the steps until the user leaves.
func (a *App) Run(ctx context.Context) error {
	a.console.SetStatus(ui.Status{
		Version:     a.options.Version,
		Steps:       len(stepTitles),
		Destination: collapseHome(a.outputDir, a.options.Home),
	})

	a.loadPreferences()

	if err := a.checkDependencies(ctx); err != nil {
		if errors.Is(err, errQuit) {
			return a.leave()
		}
		return err
	}

	stage := stageLink
	for {
		next, err := a.step(ctx, stage)
		switch {
		case errors.Is(err, errQuit), errors.Is(err, ErrInterrupted):
			return a.leave()
		case err != nil:
			return err
		}
		stage = next
	}
}

// leave ends the session the same way however it was asked for.
func (a *App) leave() error {
	a.console.Blank()
	a.console.Hint("Bye.")
	return nil
}

// interrupted runs work that has no keyboard of its own — a metadata lookup, a
// dependency probe — under a context Ctrl+C cancels, and reports whether that
// is what ended it.
//
// Without this the default signal handling kills the process where it stands,
// which during a spinner means leaving the cursor hidden and skipping every
// bit of cleanup on the way out.
func interrupted(ctx context.Context, work func(context.Context)) bool {
	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	work(runCtx)
	return runCtx.Err() != nil && ctx.Err() == nil
}

// loadPreferences applies the stored settings. A broken config file is worth a
// note, never a stopped download.
func (a *App) loadPreferences() {
	loaded, err := a.options.ConfigStore.Load()
	if err != nil {
		a.carry("Preferences could not be read", "Falling back to the built-in defaults.")
		return
	}

	a.preferences = loaded
	a.outputDir = loaded.OutputDir
	a.container = loaded.VideoContainer
	a.audioFormat = loaded.AudioFormat
	a.console.SetDestination(collapseHome(a.outputDir, a.options.Home))
}

// checkDependencies probes the external tools and refuses to go on without
// them, offering to look again once they are installed.
func (a *App) checkDependencies(ctx context.Context) error {
	for {
		status, err := a.probeDependencies(ctx)
		if err != nil {
			return err
		}
		if status.Ready() {
			return nil
		}

		a.console.Alert("caxxxd cannot run without them",
			a.console.Fields([]ui.Field{
				{Label: "yt-dlp", Value: dependencyState(status.YTDLP)},
				{Label: "ffmpeg", Value: dependencyState(status.FFmpeg)},
				{Label: "Install with", Value: "brew install " + strings.Join(status.Missing(), " ")},
			})...)

		choice, err := a.prompt.Choose("What now?", []Choice{
			{Label: "Check again", Detail: "look for the tools once more"},
			{Label: "Quit", Detail: "leave caxxxd"},
		}, 0)
		if err != nil || choice == 1 {
			return ErrDependenciesMissing
		}
	}
}

// probeDependencies draws the opening screen and looks for the tools once.
// It is its own function so the spinner is cleaned up per attempt rather than
// piling up across every "check again".
func (a *App) probeDependencies(ctx context.Context) (deps.Status, error) {
	// The check is the opening screen, so it is where the logo lives.
	a.console.Screen()
	a.console.Logo(a.options.Version)

	spinner := a.console.Spinner("Looking for yt-dlp and ffmpeg")
	defer spinner.Stop()

	var status deps.Status
	if interrupted(ctx, func(runCtx context.Context) {
		status = a.options.Checker.Check(runCtx)
	}) {
		return status, errQuit
	}

	a.dependencies = status
	a.console.SetStatus(a.statusWithTools())

	if status.Ready() {
		spinner.Done("yt-dlp and ffmpeg are ready")
		return status, nil
	}

	spinner.Fail("Missing required tools")
	return status, nil
}

func dependencyState(dependency deps.Dependency) string {
	switch {
	case dependency.Found && dependency.Version != "":
		return dependency.Version
	case dependency.Found:
		return "found"
	case dependency.Path != "":
		// It is on the PATH but would not answer. Saying "not found" would
		// send someone off to install what they already have.
		return "found at " + dependency.Path + ", but it would not run"
	default:
		return "not found"
	}
}

// statusWithTools rebuilds the status bar's tool segment from the last check.
func (a *App) statusWithTools() ui.Status {
	status := ui.Status{
		Version:     a.options.Version,
		Steps:       len(stepTitles),
		Destination: collapseHome(a.outputDir, a.options.Home),
		Tools:       toolMark("yt-dlp", a.dependencies.YTDLP) + "  " + toolMark("ffmpeg", a.dependencies.FFmpeg),
	}
	return status
}

func toolMark(name string, dependency deps.Dependency) string {
	if dependency.Found {
		return name + " ✔"
	}
	return name + " ✖"
}

// request builds the download request described by the current selection.
func (a *App) request() ytdlp.DownloadRequest {
	request := ytdlp.DownloadRequest{
		URL:       a.url,
		Mode:      a.mode,
		OutputDir: a.outputDir,
		Section:   a.options.Section,
	}

	switch a.mode {
	case domain.MediaModeVideo:
		if a.manual.videoID != "" || a.manual.muxedID != "" {
			request.VideoFormatID = a.manual.videoID
			request.AudioFormatID = a.manual.audioID
			request.MuxedFormatID = a.manual.muxedID
			return request
		}
		request.MaxHeight = a.maxHeight
		request.Container = a.container
	case domain.MediaModeAudio:
		request.AudioFormat = a.audioFormat
		request.AudioFormatID = a.manual.audioID
	case domain.MediaModeSubtitles:
		request.SubtitleLanguage = a.subtitle.Language
		request.SubtitleAutomatic = a.subtitle.Automatic
	}

	return request
}

// currentPreferences is what the next run should start from.
func (a *App) currentPreferences() config.Config {
	preferences := a.preferences
	preferences.OutputDir = a.outputDir
	if a.mode == domain.MediaModeVideo {
		preferences.VideoContainer = a.container
	}
	if a.mode == domain.MediaModeAudio && a.audioFormat != "" {
		preferences.AudioFormat = a.audioFormat
	}
	return preferences
}

// clearManualSelection forgets an exact pick so a preset choice is never mixed
// with leftover stream ids.
func (a *App) clearManualSelection() {
	a.manual = manualSelection{}
}

// resetForNextDownload clears everything about the finished item but keeps the
// preferences the user just confirmed.
func (a *App) resetForNextDownload() {
	a.info = ytdlp.MediaInfo{}
	a.url = ""
	a.initialURL = ""
	if !a.sectionFixed {
		a.options.Section = nil
	}
	a.mode = ""
	a.maxHeight = 0
	a.subtitle = ytdlp.SubtitleTrack{}
	a.logs = nil
	a.failure = Failure{}
	a.completedPath = ""
	a.clearManualSelection()
}

// appendLog keeps only the diagnostic tail an error can show.
func (a *App) appendLog(line string) {
	a.logs = append(a.logs, line)
	if len(a.logs) > maxDetailLines {
		a.logs = a.logs[len(a.logs)-maxDetailLines:]
	}
}

// collapseHome shortens a path under the home directory back to ~ form.
func collapseHome(path string, home string) string {
	if home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
