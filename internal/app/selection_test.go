package app_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

const link = "https://www.youtube.com/watch?v=abc123"

func TestVideoPresetsProduceExpectedRequests(t *testing.T) {
	tests := []struct {
		name      string
		quality   string
		container string
		want      []string
	}{
		{
			name:      "best quality in MKV",
			quality:   "Best available",
			container: "MKV",
			want:      []string{"-f", "bv*+ba/b", "--merge-output-format", "mkv"},
		},
		{
			name:      "capped height in MP4",
			quality:   "Up to 1080p",
			container: "MP4",
			want: []string{
				"-f",
				"bv*[vcodec^=avc1][height<=1080]+ba[acodec^=mp4a]/b[ext=mp4][height<=1080]",
				"--merge-output-format", "mp4",
			},
		},
		{
			name:      "capped height in WebM",
			quality:   "Up to 720p",
			container: "WebM",
			want: []string{
				"-f",
				"bv*[ext=webm][height<=720]+ba[ext=webm]/b[ext=webm][height<=720]",
				"--merge-output-format", "webm",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newSession(t, script(
				text(link),
				pick("Video"),
				pick(test.quality),
				pick(test.container),
				pick("Download"),
				pick("Quit"),
			)).run()

			session.requireScripted()
			session.requireArgs(test.want...)
			session.requireArgs("-P", session.outputDir, link)
		})
	}
}

func TestMP4SaysWhatItCosts(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MP4"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireDrawn("About MP4", "may limit the maximum resolution")
}

func TestAudioPresetsProduceExpectedRequests(t *testing.T) {
	tests := []struct {
		name   string
		preset string
		want   []string
	}{
		{
			name:   "untouched source audio",
			preset: "Best source audio",
			want:   []string{"-f", "ba/b", "-x", "--audio-format", "best"},
		},
		{
			name:   "opus",
			preset: "Opus",
			want:   []string{"-x", "--audio-format", "opus"},
		},
		{
			name:   "mp3 at the best encoder setting",
			preset: "MP3",
			want:   []string{"-x", "--audio-format", "mp3", "--audio-quality", "0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newSession(t, script(
				text(link),
				pick("Audio"),
				pick(test.preset),
				pick("Download"),
				pick("Quit"),
			)).run()

			session.requireScripted()
			session.requireArgs(test.want...)
		})
	}
}

func TestLosslessFormatsWarnBeforeTheyAreUsed(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Audio"),
		pick("FLAC"),
		pick("Yes, use FLAC"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireDrawn("Converting to FLAC cannot recover quality", "already lossy")
	session.requireArgs("--audio-format", "flac")
}

func TestTheLosslessWarningCanBeDeclined(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Audio"),
		pick("WAV"),
		pick("No, choose another format"),
		pick("Best source audio"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	if format := session.argumentAfter("--audio-format"); format != "best" {
		t.Fatalf("--audio-format = %q, want the declined format gone", format)
	}
}

func TestManualMuxedSelectionSkipsTheAudioStep(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Manual format selection"),
		pick("18"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireArgs("-f", "18")

	for _, question := range session.prompter.questions {
		if strings.HasPrefix(question, "Choose an audio stream") {
			t.Fatal("a combined stream needs no audio stream to go with it")
		}
	}
}

func TestManualVideoSelectionAsksForAnAudioStream(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Manual format selection"),
		pick("137"),
		pick("140"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireArgs("-f", "137+140")
	session.requireAsked("Choose an audio stream to pair with 137")
}

func TestBackFromTheAudioPairingDropsTheHalfMadePair(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Manual format selection"),
		pick("137"),
		pick("‹ Back"),
		pick("18"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	if selector := session.argumentAfter("-f"); selector != "18" {
		t.Fatalf("-f = %q, want the combined stream and no trace of the abandoned one", selector)
	}
}

func TestManualAudioSelectionKeepsTheSourceFormat(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Audio"),
		pick("Manual audio stream selection"),
		pick("251"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireArgs("-f", "251", "--audio-format", "best")
}

func TestManualVideoTableOffersOnlyStreamsWithPicture(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Manual format selection"),
		pick("18"),
		pick("Quit"),
	)).run()

	rows := session.prompter.menuFor("Choose a video stream")
	if len(rows) != 4 {
		t.Fatalf("the video table offered %d rows, want 3 streams and a way back:\n%v", len(rows), rows)
	}
	for _, row := range rows[:3] {
		if strings.Contains(row, "audio only") {
			t.Fatalf("an audio-only stream reached the video table: %q", row)
		}
	}
}

func TestManualAudioTableOffersOnlyAudioStreams(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Audio"),
		pick("Manual audio stream selection"),
		pick("140"),
		pick("Quit"),
	)).run()

	rows := session.prompter.menuFor("Choose an audio stream")
	if len(rows) != 3 {
		t.Fatalf("the audio table offered %d rows, want 2 streams and a way back:\n%v", len(rows), rows)
	}
	for _, row := range rows[:2] {
		if !strings.Contains(row, "audio only") {
			t.Fatalf("a stream with picture reached the audio table: %q", row)
		}
	}
}

func TestManualTableColumnsStayAligned(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Manual format selection"),
		pick("18"),
		pick("Quit"),
	)).run()

	rows := session.prompter.menuFor("Choose a video stream")
	if len(rows) < 3 {
		t.Fatalf("expected the stream table, got %v", rows)
	}

	// Every row is built from the same column widths, so the extension — the
	// last column — has to start at the same place on each of them.
	want := -1
	for _, row := range rows[:3] {
		start := strings.LastIndex(row, " ") + 1
		if want == -1 {
			want = start
			continue
		}
		if start != want {
			t.Fatalf("column drift between rows:\n%s", strings.Join(rows[:3], "\n"))
		}
	}
}

func TestManualPickingIsRefusedWithoutVideoStreams(t *testing.T) {
	session := newSession(t, script(
		text("https://example.test/track"),
		pick("Video"),
		pick("Manual format selection"),
		pick("Best available"),
		pick("MKV"),
		pick("Download"),
		pick("Quit"),
	), func(options *app.Options) {
		options.Client = ytdlp.Client{
			Binary: "yt-dlp",
			Runner: stubRunner{payload: fixture(t, "audio-only.json")},
		}
	}).run()

	session.requireScripted()
	session.requireDrawn("No video streams")
	session.requireArgs("-f", "bv*+ba/b")
}

func TestAVideoOnlyStreamIsRefusedWhenThereIsNoSoundToPairIt(t *testing.T) {
	session := newSession(t, script(
		text("https://example.test/silent"),
		pick("Video"),
		pick("Manual format selection"),
		pick("v1"),
		pick("‹ Back"),
	), func(options *app.Options) {
		options.Client = ytdlp.Client{
			Binary: "yt-dlp",
			Runner: stubRunner{payload: fixture(t, "video-only.json")},
		}
	}).run()

	session.requireScripted()
	session.requireDrawn("Nothing to pair v1 with", "no separate audio stream")

	// The table is offered again rather than an empty one being opened.
	for _, question := range session.prompter.questions {
		if strings.HasPrefix(question, "Choose an audio stream") {
			t.Fatal("an audio table was opened with no audio streams in it")
		}
	}
}

func TestBackTransitions(t *testing.T) {
	tests := []struct {
		name      string
		script    []answer
		want      string
		skipRange bool
	}{
		{
			name:      "the media choice returns to the link",
			script:    []answer{text(link), pick("Whole video"), pick("‹ Back"), pick("‹ Back")},
			want:      "Paste a media URL",
			skipRange: true,
		},
		{
			name:   "video quality returns to the media choice",
			script: []answer{text(link), pick("Video"), pick("‹ Back")},
			want:   "What do you want out of it?",
		},
		{
			name:   "the container returns to the quality",
			script: []answer{text(link), pick("Video"), pick("Best available"), pick("‹ Back")},
			want:   "Video quality",
		},
		{
			name: "the review returns to the container",
			script: []answer{
				text(link), pick("Video"), pick("Best available"), pick("MKV"), pick("‹ Back"),
			},
			want: "Container",
		},
		{
			name:   "the audio format returns to the media choice",
			script: []answer{text(link), pick("Audio"), pick("‹ Back")},
			want:   "What do you want out of it?",
		},
		{
			name: "the audio review returns to the audio format",
			script: []answer{
				text(link), pick("Audio"), pick("Opus"), pick("‹ Back"),
			},
			want: "Audio format",
		},
		{
			name: "a manual review returns to the stream table",
			script: []answer{
				text(link), pick("Video"), pick("Manual format selection"), pick("18"), pick("‹ Back"),
			},
			want: "Choose a video stream",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newSession(t, script(test.script...))
			session.prompter.autoWholeVideo = !test.skipRange
			session.run()
			session.requireScripted()

			asked := session.prompter.questions
			last := asked[len(asked)-1]
			if !strings.HasPrefix(last, test.want) {
				t.Fatalf("after going back the flow asked %q, want %q\nquestions:\n%s",
					last, test.want, strings.Join(asked, "\n"))
			}
		})
	}
}

func TestTheDownloadFolderCanBeChanged(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")

	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change download folder"),
		text(elsewhere),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireArgs("-P", elsewhere)
}

func TestARelativeDownloadFolderIsRefused(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change download folder"),
		text("somewhere/relative"),
		pick("Download"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireDrawn("not a folder caxxxd can use")
	session.requireArgs("-P", session.outputDir)
}

func TestTheDownloadFolderUnderstandsHome(t *testing.T) {
	session := newSession(t, script(
		text(link),
		pick("Video"),
		pick("Best available"),
		pick("MKV"),
		pick("Change download folder"),
		text("~/Movies"),
		pick("Quit"),
	)).run()

	session.requireScripted()
	session.requireDrawn("~/Movies")
}
