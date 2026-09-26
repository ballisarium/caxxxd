package ytdlp

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/ballisarium/caxxxd/internal/domain"
)

// Marker prefixes let caxxxd read yt-dlp's structured output without ever
// parsing its human-readable console text.
const (
	progressMarker    = "__CAXXXD_PROGRESS__"
	postProcessMarker = "__CAXXXD_POSTPROCESS__"
	fileMarker        = "__CAXXXD_FILE__"
)

// DownloadRequest is a fully resolved download: what to fetch, how, and where.
// Manual selections take precedence over the preset fields.
type DownloadRequest struct {
	CookieBrowser     string
	URL               string
	Mode              domain.MediaMode
	MaxHeight         int
	Container         domain.VideoContainer
	AudioFormat       domain.AudioFormat
	OutputDir         string
	VideoFormatID     string
	AudioFormatID     string
	MuxedFormatID     string
	SubtitleLanguage  string
	SubtitleAutomatic bool
	Section           *domain.TimeRange
}

// BuildCommand turns a request into the exact argument slice for yt-dlp. The
// URL is always the final argument and is never rewritten.
func BuildCommand(request DownloadRequest) ([]string, error) {
	if request.URL == "" {
		return nil, errors.New("URL is required")
	}
	if request.OutputDir == "" {
		return nil, errors.New("output directory is required")
	}
	if request.MaxHeight < 0 {
		return nil, fmt.Errorf("invalid maximum height %d", request.MaxHeight)
	}
	if request.Section != nil {
		if err := request.Section.Validate(); err != nil {
			return nil, fmt.Errorf("invalid section: %w", err)
		}
	}
	sectionDownload := request.Section != nil && request.Mode != domain.MediaModeSubtitles

	outputTemplate := "%(title)s [%(id)s].%(ext)s"
	if sectionDownload {
		// A clipped file needs a distinct name or a previous full download could
		// make yt-dlp treat the requested section as already complete.
		outputTemplate = "%(title)s [%(id)s] [%(section_start)s-%(section_end)s].%(ext)s"
	}

	args, err := CookieArgs(request.CookieBrowser)
	if err != nil {
		return nil, err
	}
	args = append(args,
		"--no-playlist",
		"--newline",
		"--progress",
		"--progress-delta", "0.2",
		"--output-na-placeholder", "NA",
		"--abort-on-unavailable-fragments",
		"--progress-template", "download:"+progressMarker+"%(progress.status)s\t%(progress.downloaded_bytes)s\t%(progress.total_bytes)s\t%(progress.total_bytes_estimate)s\t%(progress.speed)s\t%(progress.eta)s",
		"--progress-template", "postprocess:"+postProcessMarker+"%(progress.status)s",
		"--print", "after_move:"+fileMarker+"%(filepath)s",
		"-P", request.OutputDir,
		"-o", outputTemplate,
	)
	if sectionDownload {
		args = append(args, "--download-sections", request.Section.Selector())
	}

	switch request.Mode {
	case domain.MediaModeVideo:
		videoArgs, err := buildVideoArgs(request)
		if err != nil {
			return nil, err
		}
		args = append(args, videoArgs...)
	case domain.MediaModeAudio:
		audioArgs, err := buildAudioArgs(request)
		if err != nil {
			return nil, err
		}
		args = append(args, audioArgs...)
	case domain.MediaModeSubtitles:
		subtitleArgs, err := buildSubtitleArgs(request)
		if err != nil {
			return nil, err
		}
		args = append(args, subtitleArgs...)
	default:
		return nil, fmt.Errorf("unsupported media mode %q", request.Mode)
	}

	return append(args, request.URL), nil
}

func buildSubtitleArgs(request DownloadRequest) ([]string, error) {
	if request.SubtitleLanguage == "" {
		return nil, errors.New("subtitle language is required")
	}

	args := []string{"--skip-download", "--ignore-no-formats-error"}
	if request.SubtitleAutomatic {
		args = append(args, "--no-write-subs", "--write-auto-subs")
	} else {
		args = append(args, "--write-subs", "--no-write-auto-subs")
	}
	return append(args,
		"--sub-langs", "^"+regexp.QuoteMeta(request.SubtitleLanguage)+"$",
		"--sub-format", "srt/vtt/best",
		"--convert-subs", "srt",
	), nil
}

func buildVideoArgs(request DownloadRequest) ([]string, error) {
	manual := request.MuxedFormatID != "" || request.VideoFormatID != "" || request.AudioFormatID != ""
	if manual {
		selector, err := manualVideoSelector(request)
		if err != nil {
			return nil, err
		}

		args := []string{"-f", selector}
		if request.Container == "" {
			return args, nil
		}
		container, err := containerName(request.Container)
		if err != nil {
			return nil, err
		}
		return append(args, "--merge-output-format", container, "--remux-video", container), nil
	}

	container := request.Container
	if container == "" {
		container = domain.VideoContainerMKV
	}
	name, err := containerName(container)
	if err != nil {
		return nil, err
	}

	return []string{
		"-f", presetVideoSelector(container, request.MaxHeight),
		"--merge-output-format", name,
		// The merge option alone does nothing for an already combined stream.
		"--remux-video", name,
	}, nil
}

func manualVideoSelector(request DownloadRequest) (string, error) {
	if request.MuxedFormatID != "" {
		if request.VideoFormatID != "" || request.AudioFormatID != "" {
			return "", errors.New("choose either one combined stream or a video and audio pair")
		}
		return request.MuxedFormatID, nil
	}
	if request.VideoFormatID == "" || request.AudioFormatID == "" {
		return "", errors.New("a manual video selection needs both a video and an audio stream")
	}
	return request.VideoFormatID + "+" + request.AudioFormatID, nil
}

// presetVideoSelector builds a yt-dlp format selector that prefers separate
// best streams and falls back to a single combined stream.
func presetVideoSelector(container domain.VideoContainer, maxHeight int) string {
	height := ""
	if maxHeight > 0 {
		height = "[height<=" + strconv.Itoa(maxHeight) + "]"
	}

	switch container {
	case domain.VideoContainerMP4:
		return "bv*[vcodec^=avc1]" + height + "+ba[acodec^=mp4a]/b[ext=mp4]" + height
	case domain.VideoContainerWebM:
		return "bv*[ext=webm]" + height + "+ba[ext=webm]/b[ext=webm]" + height
	default:
		return "bv*" + height + "+ba/b" + height
	}
}

func containerName(container domain.VideoContainer) (string, error) {
	switch container {
	case domain.VideoContainerMKV, domain.VideoContainerMP4, domain.VideoContainerWebM:
		return string(container), nil
	default:
		return "", fmt.Errorf("unsupported container %q", container)
	}
}

func buildAudioArgs(request DownloadRequest) ([]string, error) {
	if request.VideoFormatID != "" || request.MuxedFormatID != "" {
		return nil, errors.New("audio downloads cannot use a video stream selection")
	}

	selector := "ba/b"
	if request.AudioFormatID != "" {
		selector = request.AudioFormatID
	}

	format := request.AudioFormat
	if format == "" {
		format = domain.AudioFormatSource
	}

	// "source" means "keep whatever the site served"; yt-dlp spells that "best".
	name := string(format)
	if format == domain.AudioFormatSource {
		name = "best"
	}

	args := []string{"-f", selector, "-x", "--audio-format", name}
	switch format {
	case domain.AudioFormatMP3:
		args = append(args, "--audio-quality", "0")
	case domain.AudioFormatWAV:
		// WAV carries no cover art, so thumbnail embedding is skipped.
		return append(args, "--embed-metadata"), nil
	case domain.AudioFormatSource, domain.AudioFormatOpus, domain.AudioFormatM4A, domain.AudioFormatFLAC:
	default:
		return nil, fmt.Errorf("unsupported audio format %q", format)
	}

	return append(args, "--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg"), nil
}
