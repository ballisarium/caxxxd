package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

type historyTestPrompter struct {
	picks     []int
	questions []string
	menus     [][]Choice
}

func (p *historyTestPrompter) Text(string, string, string) (string, error) {
	return "", ErrInterrupted
}

func (p *historyTestPrompter) Choose(question string, choices []Choice, _ int) (int, error) {
	p.questions = append(p.questions, question)
	p.menus = append(p.menus, append([]Choice(nil), choices...))
	if len(p.picks) == 0 {
		return 0, ErrInterrupted
	}
	pick := p.picks[0]
	p.picks = p.picks[1:]
	return pick, nil
}

func newHistoryTestApp(t *testing.T, picks ...int) (*App, config.HistoryStore, *historyTestPrompter) {
	t.Helper()
	root := t.TempDir()
	prompt := &historyTestPrompter{picks: picks}
	configStore := config.NewStore(filepath.Join(root, "config.json"))
	app := &App{
		options: Options{
			ConfigStore: configStore,
			OpenFile:    func(string) error { return nil },
			RevealFile:  func(string) error { return nil },
		},
		prompt:      prompt,
		preferences: config.Default(),
	}
	history := config.HistoryStore{Path: filepath.Join(root, "history.json")}
	return app, history, prompt
}

func writeHistoryMedia(t *testing.T, history config.HistoryStore, name string, entry config.HistoryEntry) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(history.Path), name)
	if err := os.WriteFile(path, []byte("verified media"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry.Path = path
	if err := history.Add(entry); err != nil {
		t.Fatalf("add history entry: %v", err)
	}
	return path
}

func TestAskHistoryListsUnavailableFileAndRepeatRestoresSettings(t *testing.T) {
	app, history, prompt := newHistoryTestApp(t, 0, 0)
	completedAt := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	path := writeHistoryMedia(t, history, "song.mp3", config.HistoryEntry{
		Name:        "ignored-name.mp3",
		Size:        999,
		CompletedAt: completedAt,
		Mode:        domain.MediaModeAudio,
		AudioFormat: domain.AudioFormatFLAC,
	})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	app.outputDir = "previous-output"
	app.preferences.OutputDir = "previous-output"
	app.preferences.AudioFormat = domain.AudioFormatMP3
	app.audioFormat = domain.AudioFormatMP3
	app.url = "UNSAVED_URL_SENTINEL"
	app.initialURL = "UNSAVED_PREFILL_SENTINEL"
	app.info = ytdlp.MediaInfo{Title: "UNSAVED_TITLE_SENTINEL"}
	app.mode = domain.MediaModeVideo
	openCalls, revealCalls := 0, 0
	app.options.OpenFile = func(string) error { openCalls++; return nil }
	app.options.RevealFile = func(string) error { revealCalls++; return nil }

	next, err := app.askHistory()
	if err != nil {
		t.Fatalf("askHistory: %v", err)
	}
	if next != stageLink {
		t.Fatalf("repeat returned stage %v, want stageLink", next)
	}
	if app.url != "" || app.initialURL != "" || app.info.Title != "" {
		t.Fatalf("repeat retained old link state: url=%q initial=%q info=%#v", app.url, app.initialURL, app.info)
	}
	if app.outputDir != filepath.Dir(path) || app.preferences.OutputDir != filepath.Dir(path) {
		t.Fatalf("repeat output directory = %q, preferences = %q, want %q", app.outputDir, app.preferences.OutputDir, filepath.Dir(path))
	}
	if app.mode != domain.MediaModeAudio || app.audioFormat != domain.AudioFormatFLAC || app.preferences.AudioFormat != domain.AudioFormatFLAC {
		t.Fatalf("repeat audio settings = mode %q, format %q, preference %q", app.mode, app.audioFormat, app.preferences.AudioFormat)
	}
	if openCalls != 0 || revealCalls != 0 {
		t.Fatalf("missing file actions were invoked: open=%d reveal=%d", openCalls, revealCalls)
	}
	if len(prompt.menus) != 2 {
		t.Fatalf("got %d menus, want history list and item actions", len(prompt.menus))
	}
	item := prompt.menus[0][0]
	if item.Label != filepath.Base(path) {
		t.Fatalf("history label = %q, want basename %q", item.Label, filepath.Base(path))
	}
	if !strings.Contains(item.Detail, "unavailable") || !strings.Contains(item.Detail, ui.FormatSize(int64(len("verified media")), false)) || !strings.Contains(item.Detail, completedAt.Local().Format("2006-01-02 15:04")) {
		t.Fatalf("history detail does not describe missing file, size, and time: %q", item.Detail)
	}
	for _, choice := range prompt.menus[1] {
		if choice.Label == "Open" || choice.Label == "Show in Finder" {
			t.Fatalf("missing file offered action %q", choice.Label)
		}
	}

	payload, err := os.ReadFile(history.Path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	for _, forbidden := range []string{"UNSAVED_URL_SENTINEL", "UNSAVED_PREFILL_SENTINEL", "UNSAVED_TITLE_SENTINEL"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("history persisted private session state %q", forbidden)
		}
	}
}

func TestAskHistoryRepeatRestoresVideoContainer(t *testing.T) {
	app, history, prompt := newHistoryTestApp(t, 0, 2)
	path := writeHistoryMedia(t, history, "video.mkv", config.HistoryEntry{
		CompletedAt:    time.Now(),
		Mode:           domain.MediaModeVideo,
		VideoContainer: domain.VideoContainerWebM,
	})

	next, err := app.askHistory()
	if err != nil {
		t.Fatalf("askHistory: %v", err)
	}
	if next != stageLink || app.mode != domain.MediaModeVideo {
		t.Fatalf("repeat returned stage %v with mode %q", next, app.mode)
	}
	if app.container != domain.VideoContainerWebM || app.preferences.VideoContainer != domain.VideoContainerWebM {
		t.Fatalf("repeat video container = %q, preference = %q", app.container, app.preferences.VideoContainer)
	}
	if app.outputDir != filepath.Dir(path) {
		t.Fatalf("repeat output directory = %q, want %q", app.outputDir, filepath.Dir(path))
	}
	if len(prompt.menus) != 2 || prompt.menus[1][2].Label != "Repeat with these settings" {
		t.Fatalf("history actions = %#v", prompt.menus)
	}
}

func TestAskHistoryOpenAndRevealUseInjectedActions(t *testing.T) {
	for _, test := range []struct {
		name       string
		pick       int
		wantOpen   bool
		wantReveal bool
	}{
		{name: "open", pick: 0, wantOpen: true},
		{name: "reveal", pick: 1, wantReveal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, history, prompt := newHistoryTestApp(t, 0, test.pick)
			path := writeHistoryMedia(t, history, "item.mp4", config.HistoryEntry{
				CompletedAt: time.Now(),
				Mode:        domain.MediaModeVideo,
			})
			var opened, revealed string
			app.options.OpenFile = func(value string) error { opened = value; return nil }
			app.options.RevealFile = func(value string) error { revealed = value; return nil }

			next, err := app.askHistory()
			if err != nil {
				t.Fatalf("askHistory: %v", err)
			}
			if next != stageHistory {
				t.Fatalf("action returned stage %v, want stageHistory", next)
			}
			if test.wantOpen && opened != path || !test.wantOpen && opened != "" {
				t.Fatalf("OpenFile received %q, wantOpen=%v", opened, test.wantOpen)
			}
			if test.wantReveal && revealed != path || !test.wantReveal && revealed != "" {
				t.Fatalf("RevealFile received %q, wantReveal=%v", revealed, test.wantReveal)
			}
			if len(prompt.menus) != 2 {
				t.Fatalf("got %d menus, want 2", len(prompt.menus))
			}
		})
	}
}

func TestRecordHistoryStoresCompletedFileWithoutSessionData(t *testing.T) {
	app, history, _ := newHistoryTestApp(t)
	path := filepath.Join(filepath.Dir(history.Path), "done.mkv")
	if err := os.WriteFile(path, []byte("verified media"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.completedPath = path
	app.mode = domain.MediaModeVideo
	app.container = domain.VideoContainerMP4
	app.url = "UNSAVED_URL_SENTINEL"
	app.preferences.CookieBrowser = "UNSAVED_COOKIE_SENTINEL"
	app.logs = []string{"UNSAVED_LOG_SENTINEL"}

	app.recordHistory()

	entries, err := history.Load()
	if err != nil {
		t.Fatalf("Load history: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != path || entries[0].Mode != domain.MediaModeVideo || entries[0].VideoContainer != domain.VideoContainerMP4 {
		t.Fatalf("recorded entry = %#v", entries)
	}
	payload, err := os.ReadFile(history.Path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	for _, forbidden := range []string{"UNSAVED_URL_SENTINEL", "UNSAVED_COOKIE_SENTINEL", "UNSAVED_LOG_SENTINEL"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("history persisted %q", forbidden)
		}
	}
}

func TestRecordHistoryFailureIsNonFatalAndCarriesHint(t *testing.T) {
	app, _, _ := newHistoryTestApp(t)
	app.completedPath = filepath.Join(t.TempDir(), "missing.mp4")
	app.mode = domain.MediaModeAudio
	app.audioFormat = domain.AudioFormatMP3

	app.recordHistory()

	if len(app.pending) != 1 || !app.pending[0].quiet {
		t.Fatalf("history failure did not queue one quiet hint: %#v", app.pending)
	}
	if app.pending[0].title == "" {
		t.Fatal("history failure queued an empty hint")
	}
}
