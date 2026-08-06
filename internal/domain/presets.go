package domain

// VideoPreset is a friendly quality choice. MaxHeight of 0 means unlimited.
type VideoPreset struct {
	ID        string
	Label     string
	MaxHeight int
	Manual    bool
}

// AudioPreset is a friendly audio choice. ShowLossySourceWarning marks the
// formats that cannot recover quality the source already threw away.
type AudioPreset struct {
	ID                     string
	Label                  string
	Format                 AudioFormat
	Manual                 bool
	ShowLossySourceWarning bool
}

// VideoPresets lists the video quality choices in display order.
func VideoPresets() []VideoPreset {
	return []VideoPreset{
		{ID: "best", Label: "Best available", MaxHeight: 0},
		{ID: "2160p", Label: "Up to 2160p", MaxHeight: 2160},
		{ID: "1440p", Label: "Up to 1440p", MaxHeight: 1440},
		{ID: "1080p", Label: "Up to 1080p", MaxHeight: 1080},
		{ID: "720p", Label: "Up to 720p", MaxHeight: 720},
		{ID: "manual", Label: "Manual format selection", Manual: true},
	}
}

// AudioPresets lists the audio choices in display order.
func AudioPresets() []AudioPreset {
	return []AudioPreset{
		{ID: "source", Label: "Best source audio", Format: AudioFormatSource},
		{ID: "opus", Label: "Opus", Format: AudioFormatOpus},
		{ID: "m4a", Label: "M4A", Format: AudioFormatM4A},
		{ID: "mp3", Label: "MP3, maximum encoder quality", Format: AudioFormatMP3},
		{ID: "flac", Label: "FLAC", Format: AudioFormatFLAC, ShowLossySourceWarning: true},
		{ID: "wav", Label: "WAV", Format: AudioFormatWAV, ShowLossySourceWarning: true},
		{ID: "manual", Label: "Manual audio stream selection", Manual: true},
	}
}
