package domain_test

import (
	"testing"

	"github.com/ballisarium/caxxxd/internal/domain"
)

func TestVideoPresetsStartWithBestAvailable(t *testing.T) {
	presets := domain.VideoPresets()
	if len(presets) != 6 {
		t.Fatalf("len(VideoPresets) = %d, want 6", len(presets))
	}
	if presets[0].ID != "best" || presets[0].MaxHeight != 0 {
		t.Fatalf("first preset = %#v, want unlimited best", presets[0])
	}
	if presets[5].ID != "manual" {
		t.Fatalf("last preset ID = %q, want manual", presets[5].ID)
	}
}

func TestVideoPresetHeightsDescend(t *testing.T) {
	presets := domain.VideoPresets()
	previous := 0
	for _, preset := range presets[1:] {
		if preset.Manual {
			continue
		}
		if preset.MaxHeight == 0 {
			t.Fatalf("preset %q must limit height", preset.ID)
		}
		if previous != 0 && preset.MaxHeight >= previous {
			t.Fatalf("preset %q height %d must be below %d", preset.ID, preset.MaxHeight, previous)
		}
		previous = preset.MaxHeight
	}
}

func TestAudioPresetsMarkLosslessConversions(t *testing.T) {
	presets := domain.AudioPresets()
	warnings := map[string]bool{}
	for _, preset := range presets {
		warnings[preset.ID] = preset.ShowLossySourceWarning
	}
	if !warnings["flac"] || !warnings["wav"] {
		t.Fatalf("FLAC and WAV must show lossy-source warnings")
	}
	if warnings["source"] || warnings["opus"] || warnings["m4a"] || warnings["mp3"] {
		t.Fatalf("only FLAC and WAV should show the warning")
	}
}

func TestAudioPresetsExposeManualEntryLast(t *testing.T) {
	presets := domain.AudioPresets()
	if len(presets) != 7 {
		t.Fatalf("len(AudioPresets) = %d, want 7", len(presets))
	}
	last := presets[len(presets)-1]
	if last.ID != "manual" || !last.Manual {
		t.Fatalf("last preset = %#v, want manual entry", last)
	}
	if presets[0].Format != domain.AudioFormatSource {
		t.Fatalf("first preset format = %q, want source", presets[0].Format)
	}
}
