package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// Opt-in real browser + downloader check, served entirely from loopback.
func TestLiveCaptureDownloadsFromEmbeddedPlayer(t *testing.T) {
	if os.Getenv("CAXXXD_BROWSER_TEST") != "1" {
		t.Skip("set CAXXXD_BROWSER_TEST=1 for local browser integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	work := t.TempDir()
	clip := filepath.Join(work, "clip.mp4")
	command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", clip)
	if err := command.Run(); err != nil {
		t.Fatalf("generate test media: %v", err)
	}
	for _, args := range [][]string{
		{"-v", "error", "-i", clip, "-vn", "-c:a", "copy", filepath.Join(work, "sound.m4a")},
		{"-v", "error", "-i", clip, "-c", "copy", "-f", "hls", "-hls_time", "1", filepath.Join(work, "master.m3u8")},
		{"-v", "error", "-i", clip, "-map", "0", "-c", "copy", "-f", "dash", filepath.Join(work, "manifest.mpd")},
	} {
		if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
			t.Fatalf("generate stream fixture: %v", err)
		}
	}
	var server *httptest.Server
	var players, media, missingCookie, missingReferer atomic.Int32
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			// localhost versus 127.0.0.1 exercises an out-of-process iframe.
			fmt.Fprintf(w, `<iframe src="/player"></iframe><iframe src="%s/remote"></iframe>`, strings.Replace(server.URL, "127.0.0.1", "localhost", 1))
		case "/remote":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<video autoplay muted controls src="/public.mp4"></video>`)
		case "/public.mp4":
			http.ServeFile(w, r, clip)
		case "/player":
			players.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "local-test", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<video autoplay muted controls src="/clip.mp4"></video><audio preload="auto" src="/sound.m4a"></audio><script>fetch('/master.m3u8');fetch('/manifest.mpd')</script>`)
		default:
			media.Add(1)
			cookie, err := r.Cookie("fixture")
			if err != nil {
				missingCookie.Add(1)
			}
			if !strings.Contains(r.Referer(), "/player") {
				missingReferer.Add(1)
			}
			if err != nil || cookie.Value != "local-test" || !strings.Contains(r.Referer(), "/player") {
				http.Error(w, "missing playback context", http.StatusForbidden)
				return
			}
			http.FileServer(http.Dir(work)).ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	capture, err := (Launcher{Headless: true, TempDir: work}).Start(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for len(capture.Candidates()) < 5 {
		select {
		case <-deadline.C:
			t.Fatalf("no media captured: player=%d media=%d missing-cookie=%d missing-referer=%d", players.Load(), media.Load(), missingCookie.Load(), missingReferer.Load())
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
			if err := capture.Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	for index, candidate := range capture.Candidates() {
		t.Run(fmt.Sprintf("%d-%s", index, candidate.Kind), func(t *testing.T) {
			selected, err := capture.Prepare(ctx, index)
			if err != nil {
				t.Fatal(err)
			}
			info, err := (ytdlp.Client{Binary: "yt-dlp", Runner: ytdlp.ExecOutputRunner{}, ConfigFile: selected.ConfigFile}).Fetch(ctx, selected.URL)
			if err != nil {
				t.Fatalf("captured metadata failed: %v", err)
			}
			if info.Title != "Browser media" || len(info.Formats) == 0 {
				t.Fatal("captured metadata was not normalized")
			}
			mode := domain.MediaModeVideo
			if candidate.Kind == "M4A" {
				mode = domain.MediaModeAudio
			}
			args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{URL: selected.URL, ConfigFile: selected.ConfigFile, Mode: mode, Container: domain.VideoContainerMKV, OutputDir: filepath.Join(work, "download", fmt.Sprint(index))})
			if err != nil {
				t.Fatal(err)
			}
			events, err := (ytdlp.Downloader{Binary: "yt-dlp"}).Start(ctx, args)
			if err != nil {
				t.Fatal(err)
			}
			completed := ""
			for event := range events {
				if event.Done && event.Err != nil {
					t.Fatalf("download failed: %v", event.Err)
				}
				if event.Parsed.Kind == ytdlp.EventCompletedFile {
					completed = event.Parsed.FilePath
				}
			}
			if completed == "" {
				t.Fatal("no completed file")
			}
			probe, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", completed).Output()
			if err != nil || !strings.Contains(string(probe), "audio") || (mode == domain.MediaModeVideo && !strings.Contains(string(probe), "video")) {
				t.Fatal("downloaded file does not contain the expected playable streams")
			}
			if stat, err := os.Stat(selected.ConfigFile); err != nil || stat.Mode().Perm() != 0o600 {
				t.Fatal("download configuration must be private")
			}
		})
	}
	directory := capture.(*Session).directory
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("browser profile was not removed")
	}
}
