package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ballisarium/caxxxd/internal/domain"
)

const maxHistoryEntries = 100

// HistoryEntry records the local result of a completed download.
type HistoryEntry struct {
	Path           string                `json:"path"`
	Name           string                `json:"name"`
	Size           int64                 `json:"size"`
	CompletedAt    time.Time             `json:"completed_at"`
	Mode           domain.MediaMode      `json:"mode"`
	VideoContainer domain.VideoContainer `json:"video_container,omitempty"`
	AudioFormat    domain.AudioFormat    `json:"audio_format,omitempty"`
}

// HistoryStore reads and writes download history at a fixed path.
type HistoryStore struct {
	Path string
}

// Load reads saved history. A missing file yields an empty list. Entries whose
// media files have since disappeared remain available to the caller.
func (s HistoryStore) Load() ([]HistoryEntry, error) {
	payload, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return []HistoryEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}

	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("parse history: expected a JSON array")
	}

	var entries []HistoryEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, fmt.Errorf("parse history: %w", err)
	}
	if entries == nil {
		return nil, fmt.Errorf("parse history: expected a JSON array")
	}
	return entries, nil
}

// Add records a completed local file after verifying it exists as a nonempty
// regular file. The file's basename and size are read from the filesystem.
func (s HistoryStore) Add(entry HistoryEntry) error {
	entries, err := s.Load()
	if err != nil {
		return err
	}

	info, err := os.Lstat(entry.Path)
	if err != nil {
		return fmt.Errorf("inspect completed file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("inspect completed file: path is not a regular file")
	}
	if info.Size() <= 0 {
		return fmt.Errorf("inspect completed file: file is empty")
	}

	entry.Name = filepath.Base(entry.Path)
	entry.Size = info.Size()
	if entry.CompletedAt.IsZero() {
		entry.CompletedAt = time.Now().UTC()
	}

	updated := make([]HistoryEntry, 0, len(entries)+1)
	updated = append(updated, entry)
	for _, previous := range entries {
		if previous.Path != entry.Path {
			updated = append(updated, previous)
		}
	}
	sort.SliceStable(updated, func(i, j int) bool {
		return updated[i].CompletedAt.After(updated[j].CompletedAt)
	})
	if len(updated) > maxHistoryEntries {
		updated = updated[:maxHistoryEntries]
	}

	return s.save(updated)
}

func (s HistoryStore) save(entries []HistoryEntry) error {
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}

	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	payload = append(payload, '\n')

	temporary, err := os.CreateTemp(directory, ".caxxxd-history-*")
	if err != nil {
		return fmt.Errorf("create temporary history file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set history permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync history: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close history: %w", err)
	}
	if err := os.Rename(temporaryPath, s.Path); err != nil {
		return fmt.Errorf("replace history: %w", err)
	}
	return nil
}
