package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pterm/pterm"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/deps"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// TestMain turns pterm's styling off for the whole package. The chrome is
// asserted as plain text, and an animating spinner would otherwise write to
// the same buffer the test reads from, from its own goroutine.
func TestMain(m *testing.M) {
	pterm.DisableStyling()
	os.Exit(m.Run())
}

// stubRunner returns canned metadata instead of running yt-dlp.
type stubRunner struct {
	payload []byte
	err     error
}

func (s stubRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return s.payload, s.err
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return payload
}

// readyChecker reports both external tools as installed.
func readyChecker() deps.Checker {
	return deps.Checker{
		LookPath: func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil },
		Version:  func(context.Context, string) (string, error) { return "2026.01.01", nil },
	}
}

// missingChecker reports yt-dlp as absent.
func missingChecker() deps.Checker {
	return deps.Checker{
		LookPath: func(name string) (string, error) {
			if name == "yt-dlp" {
				return "", errors.New("not found")
			}
			return "/opt/homebrew/bin/" + name, nil
		},
		Version: func(context.Context, string) (string, error) { return "7.1", nil },
	}
}

// fakeDownloader replays a scripted event stream, one fresh channel per run
// just like the real downloader. Cancelling the context ends the run with
// context.Canceled, exactly as the runner does.
type fakeDownloader struct {
	script   []ytdlp.RunEvent
	perRun   [][]ytdlp.RunEvent
	startErr error
	args     []string
	runs     int
	onStart  func([]string)
}

func newFakeDownloader(events ...ytdlp.RunEvent) *fakeDownloader {
	return &fakeDownloader{script: events}
}

func (f *fakeDownloader) setScript(events ...ytdlp.RunEvent) {
	f.script = events
}

// setScripts gives each run its own stream, so a retry can succeed where the
// first attempt failed. The last one repeats once the list runs out.
func (f *fakeDownloader) setScripts(scripts ...[]ytdlp.RunEvent) {
	f.perRun = scripts
}

func (f *fakeDownloader) Start(ctx context.Context, args []string) (<-chan ytdlp.RunEvent, error) {
	f.args = args
	f.runs++
	if f.onStart != nil {
		f.onStart(args)
	}
	if f.startErr != nil {
		return nil, f.startErr
	}

	script := f.script
	if len(f.perRun) > 0 {
		script = f.perRun[min(f.runs-1, len(f.perRun)-1)]
	}

	events := make(chan ytdlp.RunEvent, len(script)+2)
	for _, event := range script {
		events <- event
	}

	go func() {
		<-ctx.Done()
		events <- ytdlp.RunEvent{Done: true, Err: ctx.Err()}
	}()

	return events, nil
}

type fakeTrimmer struct {
	calls   int
	path    string
	section domain.TimeRange
	err     error
}

func (f *fakeTrimmer) Trim(_ context.Context, path string, section domain.TimeRange) error {
	f.calls++
	f.path = path
	f.section = section
	return f.err
}

func progressEvent(downloaded, total int64, speed float64, eta int64) ytdlp.RunEvent {
	return ytdlp.RunEvent{Parsed: ytdlp.Event{
		Kind: ytdlp.EventProgress,
		Progress: ytdlp.Progress{
			Status:          "downloading",
			DownloadedBytes: downloaded,
			TotalBytes:      total,
			SpeedBytes:      speed,
			ETASeconds:      eta,
			ETAKnown:        true,
		},
	}}
}

func estimatedProgressEvent(downloaded, estimate int64) ytdlp.RunEvent {
	return ytdlp.RunEvent{Parsed: ytdlp.Event{
		Kind: ytdlp.EventProgress,
		Progress: ytdlp.Progress{
			Status:              "downloading",
			DownloadedBytes:     downloaded,
			EstimatedTotalBytes: estimate,
		},
	}}
}

func finishedStreamEvent() ytdlp.RunEvent {
	return ytdlp.RunEvent{Parsed: ytdlp.Event{
		Kind:     ytdlp.EventProgress,
		Progress: ytdlp.Progress{Status: "finished"},
	}}
}

func postProcessEvent() ytdlp.RunEvent {
	return ytdlp.RunEvent{Parsed: ytdlp.Event{Kind: ytdlp.EventPostProcess, Stage: "started"}}
}

func completedEvent(path string) ytdlp.RunEvent {
	return ytdlp.RunEvent{Parsed: ytdlp.Event{Kind: ytdlp.EventCompletedFile, FilePath: path}}
}

func logEvent(line string) ytdlp.RunEvent {
	return ytdlp.RunEvent{Log: line}
}

func doneEvent(err error) ytdlp.RunEvent {
	return ytdlp.RunEvent{Done: true, Err: err}
}

// answerKind separates the two things a prompt can ask for.
type answerKind int

const (
	answerText answerKind = iota
	answerChoice
)

// answer is one scripted reply.
type answer struct {
	kind  answerKind
	value string
}

// text answers a text prompt. An empty value accepts whatever the prompt
// prefilled, which is what pressing Enter does in the real thing.
func text(value string) answer { return answer{kind: answerText, value: value} }

// pick answers a menu by the start of an option's label.
func pick(label string) answer { return answer{kind: answerChoice, value: label} }

// scriptPrompter replies from a fixed script and records everything it was
// asked. Once the script runs out it interrupts, which ends the session the
// same way Ctrl+C would.
type scriptPrompter struct {
	t       *testing.T
	answers []answer
	index   int
	// Existing tests focus on later decisions. They keep the new default
	// behaviour by accepting the whole-video range unless a range test opts in.
	autoWholeVideo bool

	questions []string
	menus     []menu
	prefills  []string
}

// menu is one list the flow offered, kept with the question that introduced it.
type menu struct {
	question string
	labels   []string
}

func script(answers ...answer) *scriptPrompter {
	return &scriptPrompter{answers: answers, autoWholeVideo: true}
}

func (s *scriptPrompter) next(kind answerKind, question string) (answer, bool) {
	s.t.Helper()

	if s.index >= len(s.answers) {
		return answer{}, false
	}
	reply := s.answers[s.index]
	s.index++

	if reply.kind != kind {
		s.t.Fatalf("script step %d answers the wrong kind of prompt at %q", s.index, question)
	}
	return reply, true
}

func (s *scriptPrompter) Text(question, _, initial string) (string, error) {
	s.t.Helper()
	s.questions = append(s.questions, question)
	s.prefills = append(s.prefills, initial)

	reply, ok := s.next(answerText, question)
	if !ok {
		return "", app.ErrInterrupted
	}
	if reply.value == "" {
		return initial, nil
	}
	return reply.value, nil
}

func (s *scriptPrompter) Choose(question string, choices []app.Choice, _ int) (int, error) {
	s.t.Helper()
	s.questions = append(s.questions, question)

	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, choice.Label)
	}
	s.menus = append(s.menus, menu{question: question, labels: labels})
	if strings.HasPrefix(question, "Time range") && s.autoWholeVideo {
		for index, choice := range choices {
			if choice.Label == "Whole video" {
				return index, nil
			}
		}
	}

	reply, ok := s.next(answerChoice, question)
	if !ok {
		return 0, app.ErrInterrupted
	}

	for index, choice := range choices {
		if strings.HasPrefix(choice.Label, reply.value) {
			return index, nil
		}
	}
	s.t.Fatalf("script step %d wants %q, but %q offered %v", s.index, reply.value, question, labels)
	return 0, nil
}

// exhausted reports whether every scripted answer was used. A test that ends
// with answers left over asked fewer questions than it meant to.
func (s *scriptPrompter) exhausted() bool { return s.index == len(s.answers) }

// menuFor returns the options offered by the first menu whose question starts
// with prefix, which is how a test inspects a list it did not answer directly.
func (s *scriptPrompter) menuFor(prefix string) []string {
	for _, offered := range s.menus {
		if strings.HasPrefix(offered.question, prefix) {
			return offered.labels
		}
	}
	return nil
}

// menusFor returns every menu whose question starts with prefix, in order.
func (s *scriptPrompter) menusFor(prefix string) [][]string {
	matches := [][]string{}
	for _, offered := range s.menus {
		if strings.HasPrefix(offered.question, prefix) {
			matches = append(matches, offered.labels)
		}
	}
	return matches
}

// session is one scripted run of the app, with everything a test asserts on.
type session struct {
	t          *testing.T
	app        *app.App
	prompter   *scriptPrompter
	console    *ui.Console
	output     *bytes.Buffer
	store      config.Store
	downloader *fakeDownloader
	outputDir  string
	err        error
}

// clearScreen is the sequence a screen is wiped with before it is redrawn.
const clearScreen = "\x1b[H\x1b[2J"

// onATerminal makes the session behave as if its output were one, which is the
// only way the screen-per-step behaviour is visible to a buffer.
func (s *session) onATerminal() *session {
	s.console.SetClearScreens(true)
	return s
}

// screens splits what was drawn into the screens it was drawn on.
func (s *session) screens() []string {
	return strings.Split(s.screen(), clearScreen)
}

// newSession wires a session against fakes: canned metadata, a scripted event
// stream, a config store in a temporary directory, and a buffer for a screen.
func newSession(t *testing.T, prompter *scriptPrompter, mutate ...func(*app.Options)) *session {
	t.Helper()

	prompter.t = t
	output := &bytes.Buffer{}
	console := ui.NewConsole(output)
	console.SetWidth(100)
	// The buffer is not a terminal; the tests still want to read what the live
	// views would have drawn into one.
	console.SetAnimated(true)

	outputDir := filepath.Join(t.TempDir(), "downloads")
	store := writtenConfig(t, config.Config{
		OutputDir:      outputDir,
		VideoContainer: domain.VideoContainerMKV,
		AudioFormat:    domain.AudioFormatSource,
	})
	downloader := newFakeDownloader(completedEvent(filepath.Join(outputDir, "Example Video.mkv")), doneEvent(nil))

	options := app.Options{
		Version:     "test",
		Checker:     readyChecker(),
		Client:      ytdlp.Client{Binary: "yt-dlp", Runner: stubRunner{payload: fixture(t, "video.json")}},
		Downloader:  downloader,
		Trimmer:     &fakeTrimmer{},
		ConfigStore: store,
		RevealFile:  func(string) error { return nil },
		Home:        "/Users/test",
		Console:     console,
		Prompter:    prompter,
	}
	for _, apply := range mutate {
		apply(&options)
	}

	if actual, ok := options.Downloader.(*fakeDownloader); ok {
		downloader = actual
	}

	return &session{
		t:          t,
		app:        app.New(options),
		prompter:   prompter,
		console:    options.Console,
		output:     output,
		store:      options.ConfigStore,
		downloader: downloader,
		outputDir:  outputDir,
	}
}

// run walks the whole session, refusing to hang the suite on a flow that never
// asks its next question.
func (s *session) run() *session {
	s.t.Helper()

	done := make(chan error, 1)
	go func() { done <- s.app.Run(context.Background()) }()

	select {
	case err := <-done:
		s.err = err
	case <-time.After(10 * time.Second):
		s.t.Fatal("the session did not finish; the script is probably missing an answer")
	}
	return s
}

// screen is everything the session drew, as plain text.
func (s *session) screen() string { return s.output.String() }

// requireDrawn fails unless every fragment appears on screen.
func (s *session) requireDrawn(fragments ...string) {
	s.t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(s.screen(), fragment) {
			s.t.Fatalf("expected %q on screen, got:\n%s", fragment, s.screen())
		}
	}
}

// requireAsked fails unless the flow asked a question starting with prefix.
// Questions belong to the prompt, not to the chrome, so they are asserted
// against what the prompter was handed rather than against the screen.
func (s *session) requireAsked(prefix string) {
	s.t.Helper()
	for _, question := range s.prompter.questions {
		if strings.HasPrefix(question, prefix) {
			return
		}
	}
	s.t.Fatalf("expected the flow to ask %q, it asked:\n%s", prefix, strings.Join(s.prompter.questions, "\n"))
}

// requireNotAsked fails if the flow asks a question starting with prefix.
func (s *session) requireNotAsked(prefix string) {
	s.t.Helper()
	for _, question := range s.prompter.questions {
		if strings.HasPrefix(question, prefix) {
			s.t.Fatalf("did not expect the flow to ask %q, it asked:\n%s", prefix, strings.Join(s.prompter.questions, "\n"))
		}
	}
}

// requireNotDrawn fails if a fragment appears on screen.
func (s *session) requireNotDrawn(fragment string) {
	s.t.Helper()
	if strings.Contains(s.screen(), fragment) {
		s.t.Fatalf("did not expect %q on screen, got:\n%s", fragment, s.screen())
	}
}

// requireArgs fails unless the downloader ran with exactly these arguments
// somewhere in its command line, in order.
func (s *session) requireArgs(wanted ...string) {
	s.t.Helper()
	joined := strings.Join(s.downloader.args, " ")
	for _, argument := range wanted {
		if !strings.Contains(joined, argument) {
			s.t.Fatalf("expected %q in the yt-dlp arguments, got:\n%v", argument, s.downloader.args)
		}
	}
}

// argumentAfter is the value a flag carried. Scanning the whole command line
// for a stream id or a format name is not the same question: the destination
// path is in there too, and a temporary directory's random suffix can contain
// any digits it likes.
func (s *session) argumentAfter(flag string) string {
	s.t.Helper()

	for index, argument := range s.downloader.args {
		if argument == flag && index+1 < len(s.downloader.args) {
			return s.downloader.args[index+1]
		}
	}
	s.t.Fatalf("no %s in the yt-dlp arguments: %v", flag, s.downloader.args)
	return ""
}

// requireScripted fails if the flow stopped before using the whole script.
func (s *session) requireScripted() {
	s.t.Helper()
	if !s.prompter.exhausted() {
		s.t.Fatalf("the flow ended with %d scripted answers unused; questions asked:\n%s",
			len(s.prompter.answers)-s.prompter.index, strings.Join(s.prompter.questions, "\n"))
	}
}

// failingClient makes every metadata lookup fail the same way.
func failingClient(t *testing.T) func(*app.Options) {
	t.Helper()
	return func(options *app.Options) {
		options.Client = ytdlp.Client{
			Binary: "yt-dlp",
			Runner: stubRunner{err: errors.New("ERROR: Private video")},
		}
	}
}

// writtenConfig returns a store already holding value.
func writtenConfig(t *testing.T, value config.Config) config.Store {
	t.Helper()

	store := config.NewStore(filepath.Join(t.TempDir(), "caxxxd", "config.json"))
	if err := store.Save(value); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return store
}

// videoPath is the script of a full, unremarkable video download.
func videoPath(extra ...answer) []answer {
	base := []answer{
		text("https://www.youtube.com/watch?v=abc123"),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Download"),
	}
	return append(base, extra...)
}
