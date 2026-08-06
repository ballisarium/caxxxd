package ytdlp

// FormatKind says whether a stream carries picture, sound, or both.
type FormatKind string

const (
	FormatKindMuxed FormatKind = "muxed"
	FormatKindVideo FormatKind = "video"
	FormatKindAudio FormatKind = "audio"
)

// Format is a normalized downloadable stream.
type Format struct {
	ID              string
	Kind            FormatKind
	Extension       string
	Width           int
	Height          int
	FPS             float64
	VideoCodec      string
	AudioCodec      string
	TotalBitrate    float64
	VideoBitrate    float64
	AudioBitrate    float64
	AudioSampleRate int
	FileSize        int64
	ApproxFileSize  int64
}

// EffectiveSize returns the best known size in bytes and whether it is only an
// estimate. A zero size means yt-dlp did not report one.
func (f Format) EffectiveSize() (int64, bool) {
	if f.FileSize > 0 {
		return f.FileSize, false
	}
	if f.ApproxFileSize > 0 {
		return f.ApproxFileSize, true
	}
	return 0, false
}

// NormalizeFormats classifies raw formats and drops entries that carry no
// media at all, such as YouTube's storyboard images.
func NormalizeFormats(raw []RawFormat) []Format {
	result := make([]Format, 0, len(raw))
	for _, item := range raw {
		// yt-dlp writes "none" when a stream is absent and leaves the field
		// out when it does not know. Absent rules a stream out; unknown only
		// means the answer has to come from the numbers next to it.
		videoAbsent := item.VideoCodec == "none"
		audioAbsent := item.AudioCodec == "none"
		if videoAbsent && audioAbsent {
			continue
		}

		hasVideo := !videoAbsent &&
			(codecNamed(item.VideoCodec) || item.Height > 0 || item.Width > 0 || item.VideoBitrate > 0)
		hasAudio := !audioAbsent &&
			(codecNamed(item.AudioCodec) || item.AudioBitrate > 0 || item.AudioSampleRate > 0)

		// Nothing said either way — a direct file link, say — stays a combined
		// stream, because that is the only claim that cannot be wrong twice.
		kind := FormatKindMuxed
		switch {
		case hasVideo && !hasAudio:
			kind = FormatKindVideo
		case hasAudio && !hasVideo:
			kind = FormatKindAudio
		}

		result = append(result, Format{
			ID:              sanitize(item.ID),
			Kind:            kind,
			Extension:       sanitize(item.Extension),
			Width:           item.Width,
			Height:          item.Height,
			FPS:             item.FPS,
			VideoCodec:      sanitize(item.VideoCodec),
			AudioCodec:      sanitize(item.AudioCodec),
			TotalBitrate:    item.TotalBitrate,
			VideoBitrate:    item.VideoBitrate,
			AudioBitrate:    item.AudioBitrate,
			AudioSampleRate: item.AudioSampleRate,
			FileSize:        item.FileSize,
			ApproxFileSize:  item.ApproxFileSize,
		})
	}

	return result
}

// codecNamed reports whether a codec field actually names a codec.
func codecNamed(codec string) bool {
	return codec != "" && codec != "none"
}

// FormatsByKind keeps the formats matching any of the requested kinds, in the
// order yt-dlp reported them.
func FormatsByKind(formats []Format, kinds ...FormatKind) []Format {
	wanted := make(map[FormatKind]bool, len(kinds))
	for _, kind := range kinds {
		wanted[kind] = true
	}

	result := make([]Format, 0, len(formats))
	for _, format := range formats {
		if wanted[format.Kind] {
			result = append(result, format)
		}
	}
	return result
}
