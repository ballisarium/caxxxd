// Package config stores the handful of preferences caxxxd remembers between
// runs. Saves are atomic so an interrupted write can never leave a half-written
// file behind.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ballisarium/caxxxd/internal/domain"
)

// Config is the persisted preference set.
type Config struct {
	OutputDir      string                `json:"output_dir"`
	VideoContainer domain.VideoContainer `json:"video_container"`
	AudioFormat    domain.AudioFormat    `json:"audio_format"`
}

// Store reads and writes a Config at a fixed path.
type Store struct {
	Path string
}

// NewStore returns a Store for the given config.json path.
func NewStore(path string) Store {
	return Store{Path: path}
}

// Default returns the preferences a first-time user gets: maximum quality
// video in MKV and untouched source audio.
func Default() Config {
	return Config{
		OutputDir:      defaultOutputDir(),
		VideoContainer: domain.VideoContainerMKV,
		AudioFormat:    domain.AudioFormatSource,
	}
}

func defaultOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "caxxxd")
	}
	return filepath.Join(home, "Downloads", "caxxxd")
}

// Load reads the stored preferences. A missing file yields defaults; a
// malformed file is reported and left untouched so the user can inspect it.
func (s Store) Load() (Config, error) {
	payload, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	value := Default()
	if err := json.Unmarshal(payload, &value); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", s.Path, err)
	}
	return value, nil
}

// Save writes the preferences atomically: a private temporary file first, then
// a rename over the real one.
func (s Store) Save(value Config) error {
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	payload = append(payload, '\n')

	temporary := s.Path + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(temporary, s.Path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// ExpandHome expands a leading "~" or "~/" against home. Anything else,
// including "~user" forms caxxxd cannot resolve, is returned unchanged.
func ExpandHome(path string, home string) string {
	if home == "" || path == "" || path[0] != '~' {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
