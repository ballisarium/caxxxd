// Package ytdlp is the only place in caxxxd that knows how to talk to yt-dlp.
// Everything it learns arrives as structured output: JSON metadata and
// prefixed progress templates. Human-readable console text is never parsed.
package ytdlp

// RawInfo mirrors the subset of `yt-dlp --dump-single-json` caxxxd needs.
type RawInfo struct {
	ID                string                   `json:"id"`
	Title             string                   `json:"title"`
	Uploader          string                   `json:"uploader"`
	Channel           string                   `json:"channel"`
	ExtractorKey      string                   `json:"extractor_key"`
	Duration          float64                  `json:"duration"`
	WebpageURL        string                   `json:"webpage_url"`
	Thumbnail         string                   `json:"thumbnail"`
	Formats           []RawFormat              `json:"formats"`
	Subtitles         map[string][]RawSubtitle `json:"subtitles"`
	AutomaticCaptions map[string][]RawSubtitle `json:"automatic_captions"`
}

// RawSubtitle is deliberately limited to public display metadata. Subtitle
// URLs can be signed credentials and must never leave yt-dlp's JSON payload.
type RawSubtitle struct {
	Extension string `json:"ext"`
	Name      string `json:"name"`
}

// RawFormat mirrors one entry of the JSON `formats` array.
type RawFormat struct {
	ID              string  `json:"format_id"`
	Extension       string  `json:"ext"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	FPS             float64 `json:"fps"`
	VideoCodec      string  `json:"vcodec"`
	AudioCodec      string  `json:"acodec"`
	TotalBitrate    float64 `json:"tbr"`
	VideoBitrate    float64 `json:"vbr"`
	AudioBitrate    float64 `json:"abr"`
	AudioSampleRate int     `json:"asr"`
	FileSize        int64   `json:"filesize"`
	ApproxFileSize  int64   `json:"filesize_approx"`
}

// MediaInfo is the normalized description of one media item.
type MediaInfo struct {
	ID           string
	Title        string
	Uploader     string
	Platform     string
	Duration     float64
	WebpageURL   string
	ThumbnailURL string
	Formats      []Format
	Subtitles    []SubtitleTrack
}

// SubtitleTrack is one language/source choice offered to the user.
type SubtitleTrack struct {
	Language  string
	Name      string
	Automatic bool
}
