package ytdlp_test

import (
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestBuildBestVideoMKV(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://example.test/video",
		Mode:      domain.MediaModeVideo,
		MaxHeight: 0,
		Container: domain.VideoContainerMKV,
		OutputDir: "/tmp/out",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "-f", "bv*+ba/b")
	assertContainsSequence(t, args, "--merge-output-format", "mkv")
	assertContainsSequence(t, args, "--remux-video", "mkv")
	assertContainsSequence(t, args, "-P", "/tmp/out")
}

func TestBuildMP3Audio(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:         "https://example.test/video",
		Mode:        domain.MediaModeAudio,
		AudioFormat: domain.AudioFormatMP3,
		OutputDir:   "/tmp/out",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
}

func TestBuildManualSubtitleDownloadSkipsTheMedia(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:              "https://example.test/video",
		Mode:             domain.MediaModeSubtitles,
		OutputDir:        "/tmp/out",
		SubtitleLanguage: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertContainsSequence(t, args, "--skip-download")
	assertContainsSequence(t, args, "--ignore-no-formats-error")
	assertContainsSequence(t, args, "--write-subs", "--no-write-auto-subs")
	assertContainsSequence(t, args, "--sub-langs", "^ru$")
	assertContainsSequence(t, args, "--sub-format", "srt/vtt/best", "--convert-subs", "srt")
	assertLacks(t, args, "-f")
}

func TestBuildAutomaticSubtitleDownloadUsesOnlyAutomaticCaptions(t *testing.T) {
	section := domain.TimeRange{Start: 30, End: 80}
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:               "https://example.test/video",
		Mode:              domain.MediaModeSubtitles,
		OutputDir:         "/tmp/out",
		SubtitleLanguage:  "en-US",
		SubtitleAutomatic: true,
		Section:           &section,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertContainsSequence(t, args, "--no-write-subs", "--write-auto-subs")
	assertContainsSequence(t, args, "--sub-langs", "^en-US$")
	assertLacks(t, args, "--download-sections")
}

func TestBuildSectionDownloadUsesTheTimeRangeAndDistinctOutputName(t *testing.T) {
	section := domain.TimeRange{Start: 330, End: 380}
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://example.test/video",
		Mode:      domain.MediaModeVideo,
		Container: domain.VideoContainerMKV,
		OutputDir: "/tmp/out",
		Section:   &section,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertContainsSequence(t, args, "--download-sections", "*5:30-6:20")
	assertContainsSequence(t, args, "-o", "%(title)s [%(id)s] [%(section_start)s-%(section_end)s].%(ext)s")
	assertLacks(t, args, "--force-keyframes-at-cuts")
}

func TestBuildAlwaysSetsStructuredOutputContract(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://example.test/video",
		Mode:      domain.MediaModeVideo,
		Container: domain.VideoContainerMKV,
		OutputDir: "/tmp/out",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertContainsSequence(t, args, "--no-playlist")
	assertContainsSequence(t, args, "--abort-on-unavailable-fragments")
	assertContainsSequence(t, args, "--newline")
	assertContainsSequence(t, args, "--progress-delta", "0.2")
	assertContainsSequence(t, args, "--output-na-placeholder", "NA")
	assertContainsSequence(t, args, "-o", "%(title)s [%(id)s].%(ext)s")
	assertContainsSequence(t, args, "--print", "after_move:__CAXXXD_FILE__%(filepath)s")

	var templates []string
	for index, arg := range args {
		if arg == "--progress-template" {
			templates = append(templates, args[index+1])
		}
	}
	if len(templates) != 2 {
		t.Fatalf("progress templates = %q, want two", templates)
	}
	if !strings.HasPrefix(templates[0], "download:__CAXXXD_PROGRESS__") {
		t.Fatalf("download template = %q", templates[0])
	}
	if !strings.HasPrefix(templates[1], "postprocess:__CAXXXD_POSTPROCESS__") {
		t.Fatalf("postprocess template = %q", templates[1])
	}
}

func TestBuildVideoSelectors(t *testing.T) {
	tests := []struct {
		name      string
		request   ytdlp.DownloadRequest
		selector  string
		container string
	}{
		{
			name:      "best mkv",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerMKV},
			selector:  "bv*+ba/b",
			container: "mkv",
		},
		{
			name:      "1080p mkv",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerMKV, MaxHeight: 1080},
			selector:  "bv*[height<=1080]+ba/b[height<=1080]",
			container: "mkv",
		},
		{
			name:      "best mp4",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerMP4},
			selector:  "bv*[vcodec^=avc1]+ba[acodec^=mp4a]/b[ext=mp4]",
			container: "mp4",
		},
		{
			name:      "720p mp4",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerMP4, MaxHeight: 720},
			selector:  "bv*[vcodec^=avc1][height<=720]+ba[acodec^=mp4a]/b[ext=mp4][height<=720]",
			container: "mp4",
		},
		{
			name:      "best webm",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerWebM},
			selector:  "bv*[ext=webm]+ba[ext=webm]/b[ext=webm]",
			container: "webm",
		},
		{
			name:      "1440p webm",
			request:   ytdlp.DownloadRequest{Container: domain.VideoContainerWebM, MaxHeight: 1440},
			selector:  "bv*[ext=webm][height<=1440]+ba[ext=webm]/b[ext=webm][height<=1440]",
			container: "webm",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := test.request
			request.URL = "https://example.test/video"
			request.Mode = domain.MediaModeVideo
			request.OutputDir = "/tmp/out"

			args, err := ytdlp.BuildCommand(request)
			if err != nil {
				t.Fatal(err)
			}
			assertContainsSequence(t, args, "-f", test.selector)
			assertContainsSequence(t, args, "--merge-output-format", test.container)
		})
	}
}

func TestBuildVideoDefaultsToMKV(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       "https://example.test/video",
		Mode:      domain.MediaModeVideo,
		OutputDir: "/tmp/out",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "--merge-output-format", "mkv")
}

func TestBuildManualMuxedFormat(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:           "https://example.test/video",
		Mode:          domain.MediaModeVideo,
		OutputDir:     "/tmp/out",
		MuxedFormatID: "18",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "-f", "18")
	assertLacks(t, args, "--merge-output-format")
}

func TestBuildManualSplitFormat(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:           "https://example.test/video",
		Mode:          domain.MediaModeVideo,
		OutputDir:     "/tmp/out",
		VideoFormatID: "137",
		AudioFormatID: "140",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "-f", "137+140")
}

func TestBuildManualSplitHonoursExplicitContainer(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:           "https://example.test/video",
		Mode:          domain.MediaModeVideo,
		OutputDir:     "/tmp/out",
		VideoFormatID: "137",
		AudioFormatID: "140",
		Container:     domain.VideoContainerMKV,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "--merge-output-format", "mkv")
	assertContainsSequence(t, args, "--remux-video", "mkv")
}

func TestBuildRejectsIncompleteManualSelection(t *testing.T) {
	base := ytdlp.DownloadRequest{
		URL:       "https://example.test/video",
		Mode:      domain.MediaModeVideo,
		OutputDir: "/tmp/out",
	}

	onlyVideo := base
	onlyVideo.VideoFormatID = "137"
	if _, err := ytdlp.BuildCommand(onlyVideo); err == nil {
		t.Fatal("a video-only manual selection needs an audio stream")
	}

	onlyAudio := base
	onlyAudio.AudioFormatID = "140"
	if _, err := ytdlp.BuildCommand(onlyAudio); err == nil {
		t.Fatal("a manual audio stream alone is not a video selection")
	}

	conflicting := base
	conflicting.MuxedFormatID = "18"
	conflicting.VideoFormatID = "137"
	conflicting.AudioFormatID = "140"
	if _, err := ytdlp.BuildCommand(conflicting); err == nil {
		t.Fatal("muxed and split selections must not be combined")
	}
}

func TestBuildAudioSelectors(t *testing.T) {
	tests := []struct {
		name     string
		format   domain.AudioFormat
		expected []string
		lacks    string
	}{
		{
			name:     "source",
			format:   domain.AudioFormatSource,
			expected: []string{"-x", "--audio-format", "best"},
		},
		{
			name:     "empty defaults to source",
			format:   "",
			expected: []string{"-x", "--audio-format", "best"},
		},
		{
			name:     "opus",
			format:   domain.AudioFormatOpus,
			expected: []string{"-x", "--audio-format", "opus"},
		},
		{
			name:     "m4a",
			format:   domain.AudioFormatM4A,
			expected: []string{"-x", "--audio-format", "m4a"},
		},
		{
			name:     "flac",
			format:   domain.AudioFormatFLAC,
			expected: []string{"-x", "--audio-format", "flac"},
		},
		{
			name:     "wav skips thumbnail embedding",
			format:   domain.AudioFormatWAV,
			expected: []string{"-x", "--audio-format", "wav", "--embed-metadata"},
			lacks:    "--embed-thumbnail",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
				URL:         "https://example.test/video",
				Mode:        domain.MediaModeAudio,
				AudioFormat: test.format,
				OutputDir:   "/tmp/out",
			})
			if err != nil {
				t.Fatal(err)
			}
			assertContainsSequence(t, args, "-f", "ba/b")
			assertContainsSequence(t, args, test.expected...)
			if test.lacks != "" {
				assertLacks(t, args, test.lacks)
			}
		})
	}
}

func TestBuildManualAudioStream(t *testing.T) {
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:           "https://example.test/video",
		Mode:          domain.MediaModeAudio,
		OutputDir:     "/tmp/out",
		AudioFormatID: "251",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsSequence(t, args, "-f", "251")
	assertContainsSequence(t, args, "-x", "--audio-format", "best")
	assertContainsSequence(t, args, "--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg")
}

func TestBuildRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		request ytdlp.DownloadRequest
	}{
		{"missing URL", ytdlp.DownloadRequest{Mode: domain.MediaModeVideo, OutputDir: "/tmp/out"}},
		{"missing output dir", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: domain.MediaModeVideo}},
		{"unknown mode", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: "photo", OutputDir: "/tmp/out"}},
		{"unknown container", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: domain.MediaModeVideo, Container: "avi", OutputDir: "/tmp/out"}},
		{"unknown audio format", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: domain.MediaModeAudio, AudioFormat: "aiff", OutputDir: "/tmp/out"}},
		{"negative height", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: domain.MediaModeVideo, MaxHeight: -1, OutputDir: "/tmp/out"}},
		{"invalid section", ytdlp.DownloadRequest{URL: "https://example.test/v", Mode: domain.MediaModeVideo, OutputDir: "/tmp/out", Section: &domain.TimeRange{Start: 10, End: 10}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ytdlp.BuildCommand(test.request); err == nil {
				t.Fatalf("BuildCommand(%#v) succeeded, want error", test.request)
			}
		})
	}
}

func TestBuildKeepsURLIntactAsSingleArgument(t *testing.T) {
	rawURL := `https://example.test/watch?v=a%20b&list=$(rm -rf /);echo 'x'&t=1|2`

	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{
		URL:       rawURL,
		Mode:      domain.MediaModeVideo,
		Container: domain.VideoContainerMKV,
		OutputDir: "/tmp/out",
	})
	if err != nil {
		t.Fatal(err)
	}

	if args[len(args)-1] != rawURL {
		t.Fatalf("final argument = %q, want the URL unchanged", args[len(args)-1])
	}
	occurrences := 0
	for _, arg := range args {
		if arg == rawURL {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Fatalf("URL appears %d times, want exactly once", occurrences)
	}
}
