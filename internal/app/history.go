package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
)

func (a *App) historyStore() config.HistoryStore {
	return config.HistoryStore{
		Path: filepath.Join(filepath.Dir(a.options.ConfigStore.Path), "history.json"),
	}
}

// recordHistory saves only the verified local output and the choices needed to
// repeat it. History is optional, so a failed save only leaves a quiet hint.
func (a *App) recordHistory() {
	if a.completedPath == "" {
		return
	}

	entry := config.HistoryEntry{
		Path:        a.completedPath,
		CompletedAt: time.Now().UTC(),
		Mode:        a.mode,
	}
	switch a.mode {
	case domain.MediaModeVideo:
		entry.VideoContainer = a.container
	case domain.MediaModeAudio:
		entry.AudioFormat = a.audioFormat
	}
	if err := a.historyStore().Add(entry); err != nil {
		a.carryHint("Download history could not be saved.")
	}
}

// askHistory lets the user open a previous output or start again with its
// local preferences. History paths stay local; source URLs are never recorded.
func (a *App) askHistory() (stage, error) {
	entries, err := a.historyStore().Load()
	if err != nil {
		a.carryHint("Download history could not be read.")
		return stageSource, nil
	}
	if len(entries) == 0 {
		a.carryHint("No completed downloads in history yet.")
		return stageSource, nil
	}

	available := make([]bool, len(entries))
	choices := make([]Choice, 0, len(entries)+1)
	for index, entry := range entries {
		name := filepath.Base(entry.Path)
		if entry.Path == "" {
			name = "Unknown file"
		}
		available[index] = historyFileAvailable(entry.Path)
		choices = append(choices, Choice{
			Label:  name,
			Detail: historyEntryDetail(entry, available[index]) + " · " + collapseHome(filepath.Dir(entry.Path), a.options.Home),
		})
	}
	choices = append(choices, backChoice)

	picked, err := a.prompt.Choose("Download history", choices, 0)
	if err != nil {
		return stageHistory, err
	}
	if picked == len(entries) {
		return stageSource, nil
	}
	if picked < 0 || picked >= len(entries) {
		return stageHistory, errNoSuchChoice
	}

	entry := entries[picked]
	actions := make([]Choice, 0, 4)
	actionKinds := make([]historyAction, 0, 4)
	if available[picked] {
		actions = append(actions,
			Choice{Label: "Open", Detail: "open in the default app"},
			Choice{Label: "Show in Finder", Detail: "reveal the file in Finder"},
		)
		actionKinds = append(actionKinds, historyActionOpen, historyActionReveal)
	}
	actions = append(actions,
		Choice{Label: "Repeat with these settings", Detail: "enter the URL again"},
		backChoice,
	)
	actionKinds = append(actionKinds, historyActionRepeat, historyActionBack)

	selected, err := a.prompt.Choose("History item", actions, 0)
	if err != nil {
		return stageHistory, err
	}
	if selected < 0 || selected >= len(actionKinds) {
		return stageHistory, errNoSuchChoice
	}

	switch actionKinds[selected] {
	case historyActionOpen:
		if err := a.options.OpenFile(entry.Path); err != nil {
			a.carryHint("Could not open the selected file.")
		}
		return stageHistory, nil
	case historyActionReveal:
		if err := a.options.RevealFile(entry.Path); err != nil {
			a.carryHint("Could not reveal the selected file in Finder.")
		}
		return stageHistory, nil
	case historyActionRepeat:
		a.repeatHistoryEntry(entry)
		return stageLink, nil
	default:
		return stageHistory, nil
	}
}

type historyAction uint8

const (
	historyActionOpen historyAction = iota
	historyActionReveal
	historyActionRepeat
	historyActionBack
)

func historyFileAvailable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func historyEntryDetail(entry config.HistoryEntry, available bool) string {
	parts := []string{ui.FormatSize(entry.Size, false)}
	if entry.CompletedAt.IsZero() {
		parts = append(parts, "time unknown")
	} else {
		parts = append(parts, entry.CompletedAt.Local().Format("2006-01-02 15:04"))
	}
	if !available {
		parts = append(parts, "unavailable")
	}
	return strings.Join(parts, " · ")
}

func (a *App) repeatHistoryEntry(entry config.HistoryEntry) {
	a.resetForNextDownload()

	a.outputDir = filepath.Dir(entry.Path)
	a.preferences.OutputDir = a.outputDir
	switch entry.Mode {
	case domain.MediaModeVideo:
		a.mode = domain.MediaModeVideo
		if entry.VideoContainer != "" {
			a.container = entry.VideoContainer
			a.preferences.VideoContainer = entry.VideoContainer
		}
	case domain.MediaModeAudio:
		a.mode = domain.MediaModeAudio
		if entry.AudioFormat != "" {
			a.audioFormat = entry.AudioFormat
			a.preferences.AudioFormat = entry.AudioFormat
		}
	case domain.MediaModeSubtitles:
		a.mode = domain.MediaModeSubtitles
	}
}
