package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/domain"
)

func TestDefaultPrefersQualityPreservingChoices(t *testing.T) {
	value := config.Default()

	if value.VideoContainer != domain.VideoContainerMKV {
		t.Fatalf("VideoContainer = %q, want mkv", value.VideoContainer)
	}
	if value.AudioFormat != domain.AudioFormatSource {
		t.Fatalf("AudioFormat = %q, want source", value.AudioFormat)
	}
	if !strings.HasSuffix(filepath.ToSlash(value.OutputDir), "/Downloads/caxxxd") {
		t.Fatalf("OutputDir = %q, want ~/Downloads/caxxxd", value.OutputDir)
	}
	if !filepath.IsAbs(value.OutputDir) {
		t.Fatalf("OutputDir = %q, want an absolute path", value.OutputDir)
	}
}

func TestLoadReturnsDefaultsWhenFileIsMissing(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "caxxxd", "config.json"))

	value, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if value != config.Default() {
		t.Fatalf("Load = %#v, want defaults", value)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caxxxd", "config.json")
	store := config.NewStore(path)

	want := config.Config{
		OutputDir:      "/Users/test/Movies",
		VideoContainer: domain.VideoContainerMP4,
		AudioFormat:    domain.AudioFormatOpus,
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestSaveCreatesPrivateParentDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "caxxxd", "config.json")

	if err := config.NewStore(path).Save(config.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if mode := parent.Mode().Perm(); mode != 0o700 {
		t.Fatalf("parent mode = %o, want 700", mode)
	}

	file, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := file.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")

	if err := config.NewStore(path).Save(config.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("directory contains %v, want only config.json", entries)
	}
}

func TestSaveWritesReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.NewStore(path).Save(config.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(payload), "\n  \"output_dir\"") {
		t.Fatalf("config is not indented JSON:\n%s", payload)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"output_dir", "video_container", "audio_format"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing key %q in %v", key, decoded)
		}
	}
}

func TestLoadReportsMalformedJSONWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := config.NewStore(path)
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted malformed JSON")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "{oops" {
		t.Fatalf("Load rewrote the file: %q", payload)
	}
}

func TestLoadFillsMissingFieldsWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"output_dir":"/Users/test/Movies"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	value, err := config.NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if value.OutputDir != "/Users/test/Movies" {
		t.Fatalf("OutputDir = %q", value.OutputDir)
	}
	if value.VideoContainer != domain.VideoContainerMKV || value.AudioFormat != domain.AudioFormatSource {
		t.Fatalf("missing fields not defaulted: %#v", value)
	}
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"~", "/Users/test"},
		{"~/Downloads", "/Users/test/Downloads"},
		{"~/Downloads/caxxxd", "/Users/test/Downloads/caxxxd"},
		{"~other/file", "~other/file"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, test := range tests {
		if got := config.ExpandHome(test.path, "/Users/test"); got != test.want {
			t.Fatalf("ExpandHome(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestExpandHomeWithoutHomeLeavesPathAlone(t *testing.T) {
	if got := config.ExpandHome("~/Downloads", ""); got != "~/Downloads" {
		t.Fatalf("ExpandHome = %q, want the path unchanged", got)
	}
}

func TestSaveReportsUnwritableLocation(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "caxxxd")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	store := config.NewStore(filepath.Join(blocker, "config.json"))
	if err := store.Save(config.Default()); err == nil {
		t.Fatal("Save succeeded even though the parent path is a file")
	}
}
