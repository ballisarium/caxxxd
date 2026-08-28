// Package domain holds the vocabulary caxxxd uses to describe a download:
// what kind of media the user wants and which container or codec it lands in.
package domain

// MediaMode is the top-level choice between media streams and plain subtitles.
type MediaMode string

const (
	MediaModeVideo     MediaMode = "video"
	MediaModeAudio     MediaMode = "audio"
	MediaModeSubtitles MediaMode = "subtitles"
)

// VideoContainer is the container caxxxd asks yt-dlp to merge streams into.
type VideoContainer string

const (
	VideoContainerMKV  VideoContainer = "mkv"
	VideoContainerMP4  VideoContainer = "mp4"
	VideoContainerWebM VideoContainer = "webm"
)

// AudioFormat is the audio output format. AudioFormatSource keeps whatever the
// site already serves so nothing is re-encoded.
type AudioFormat string

const (
	AudioFormatSource AudioFormat = "source"
	AudioFormatOpus   AudioFormat = "opus"
	AudioFormatM4A    AudioFormat = "m4a"
	AudioFormatMP3    AudioFormat = "mp3"
	AudioFormatFLAC   AudioFormat = "flac"
	AudioFormatWAV    AudioFormat = "wav"
)
