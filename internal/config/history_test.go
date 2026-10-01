package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/domain"
)

func TestHistoryStoreLoadMissingAndAddUsesFilesystemReceipt(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "clip.mkv")
	media := []byte("verified media bytes")
	if err := os.WriteFile(mediaPath, media, 0o600); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(root, "state", "history.json")
	store := HistoryStore{Path: historyPath}
	entries, err := store.Load()
	if err != nil {
		t.Fatalf("Load missing history: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Load missing history returned %d entries, want 0", len(entries))
	}
	if _, err := os.Stat(historyPath); !os.IsNotExist(err) {
		t.Fatalf("Load created a missing history file: stat error = %v", err)
	}

	before := time.Now()
	if err := store.Add(HistoryEntry{
		Path:           mediaPath,
		Name:           "untrusted supplied name",
		Size:           999,
		Mode:           domain.MediaModeVideo,
		VideoContainer: domain.VideoContainerMKV,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	after := time.Now()

	entries, err = store.Load()
	if err != nil {
		t.Fatalf("Load saved history: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Load returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Path != mediaPath || entry.Name != "clip.mkv" || entry.Size != int64(len(media)) {
		t.Fatalf("entry filesystem receipt = %#v", entry)
	}
	if entry.Mode != domain.MediaModeVideo || entry.VideoContainer != domain.VideoContainerMKV {
		t.Fatalf("entry media settings = %#v", entry)
	}
	if entry.CompletedAt.Before(before) || entry.CompletedAt.After(after) {
		t.Fatalf("zero completion time was not filled at Add: %s", entry.CompletedAt)
	}

	info, err := os.Stat(historyPath)
	if err != nil {
		t.Fatalf("stat saved history: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("history permissions = %04o, want 0600", got)
	}
	payload, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read saved history: %v", err)
	}
	if strings.Contains(string(payload), "untrusted supplied name") {
		t.Fatal("saved history included the caller-supplied name")
	}
}

func TestHistoryStoreAddRejectsUnverifiedFilesAndPreservesHistory(t *testing.T) {
	root := t.TempDir()
	validPath := filepath.Join(root, "valid.media")
	if err := os.WriteFile(validPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(root, "empty.media")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	store := HistoryStore{Path: filepath.Join(root, "history.json")}
	if err := store.Add(HistoryEntry{Path: validPath}); err != nil {
		t.Fatalf("Add initial receipt: %v", err)
	}
	if err := os.Remove(validPath); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Load()
	if err != nil {
		t.Fatalf("Load history after output was removed: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != validPath {
		t.Fatalf("Load dropped a record whose output was removed: %#v", entries)
	}

	invalidPaths := []struct {
		name string
		path string
	}{
		{name: "missing", path: validPath},
		{name: "directory", path: root},
		{name: "empty", path: emptyPath},
	}
	for _, test := range invalidPaths {
		t.Run(test.name, func(t *testing.T) {
			if err := store.Add(HistoryEntry{Path: test.path}); err == nil {
				t.Fatal("Add accepted a path without a nonempty regular file")
			}
			entries, err := store.Load()
			if err != nil {
				t.Fatalf("Load after rejected Add: %v", err)
			}
			if len(entries) != 1 || entries[0].Path != validPath {
				t.Fatalf("rejected Add changed saved history: %#v", entries)
			}
		})
	}
}

func TestHistoryStoreOrdersDeduplicatesAndCapsEntries(t *testing.T) {
	root := t.TempDir()
	store := HistoryStore{Path: filepath.Join(root, "history.json")}
	paths := make([]string, 102)
	for i := range paths {
		paths[i] = filepath.Join(root, fmt.Sprintf("%03d.media", i))
		if err := os.WriteFile(paths[i], []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.Add(HistoryEntry{
			Path:        paths[i],
			CompletedAt: time.Unix(int64(i), 0).UTC(),
		}); err != nil {
			t.Fatalf("Add entry %d: %v", i, err)
		}
	}

	if err := store.Add(HistoryEntry{
		Path:        paths[101],
		CompletedAt: time.Unix(102, 0).UTC(),
		Mode:        domain.MediaModeAudio,
		AudioFormat: domain.AudioFormatMP3,
	}); err != nil {
		t.Fatalf("Add duplicate path: %v", err)
	}

	entries, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 100 {
		t.Fatalf("Load returned %d entries, want maximum 100", len(entries))
	}
	if entries[0].Path != paths[101] || !entries[0].CompletedAt.Equal(time.Unix(102, 0).UTC()) {
		t.Fatalf("newest entry is %#v", entries[0])
	}
	if entries[0].Mode != domain.MediaModeAudio || entries[0].AudioFormat != domain.AudioFormatMP3 {
		t.Fatalf("duplicate path did not update its media settings: %#v", entries[0])
	}
	if entries[len(entries)-1].Path != paths[2] {
		t.Fatalf("oldest retained entry is %#v, want path %q", entries[len(entries)-1], paths[2])
	}
	for i, entry := range entries {
		if i > 0 && entries[i-1].CompletedAt.Before(entry.CompletedAt) {
			t.Fatalf("entries are not newest first at index %d", i)
		}
		for j := 0; j < i; j++ {
			if entries[j].Path == entry.Path {
				t.Fatalf("path %q appeared more than once", entry.Path)
			}
		}
	}
}

func TestHistoryStoreDoesNotOverwriteCorruptHistory(t *testing.T) {
	root := t.TempDir()
	historyPath := filepath.Join(root, "history.json")
	corrupt := []byte("{not valid json")
	if err := os.WriteFile(historyPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(root, "valid.media")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := HistoryStore{Path: historyPath}

	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted corrupt history")
	}
	if err := store.Add(HistoryEntry{Path: mediaPath}); err == nil {
		t.Fatal("Add overwrote corrupt history")
	}
	after, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read corrupt history after rejected operations: %v", err)
	}
	if string(after) != string(corrupt) {
		t.Fatalf("corrupt history changed from %q to %q", corrupt, after)
	}
}
