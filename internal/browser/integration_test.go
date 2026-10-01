package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/process"
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
	var popup atomic.Int32
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			// localhost versus 127.0.0.1 exercises an out-of-process iframe.
			fmt.Fprintf(w, `<iframe src="/player"></iframe><iframe src="%s/remote"></iframe>`, strings.Replace(server.URL, "127.0.0.1", "localhost", 1))
		case "/popup":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<video autoplay muted src="/popup.mp4"></video>`)
		case "/popup.mp4":
			popup.Add(1)
			http.ServeFile(w, r, clip)
		case "/advert":
			fmt.Fprint(w, "Local advertising fixture")
		case "/remote":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<video autoplay muted controls src="/public.mp4"></video>`)
		case "/public.mp4":
			http.ServeFile(w, r, clip)
		case "/player":
			players.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "local-test", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<video autoplay muted controls src="/clip.mp4"></video><audio preload="auto" src="/sound.m4a"></audio><script>fetch('/master.m3u8');fetch('/variants.m3u8');fetch('/manifest.mpd')</script>`)
		case "/variants.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nmaster.m3u8\n")
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
	session := capture.(*Session)
	openedPopup := false
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	estimatesReady := func() bool {
		var hls, dash int
		for _, candidate := range capture.Candidates() {
			if candidate.Kind == "HLS" && candidate.Size > 0 && candidate.Approximate {
				hls++
			}
			if candidate.Kind == "DASH" && candidate.Size > 0 && candidate.Approximate {
				dash++
			}
		}
		return hls == 2 && dash == 1
	}
	playbackMetadataReady := func() bool {
		count := 0
		for _, candidate := range capture.Candidates() {
			if candidate.Kind == "MP4" && candidate.Height == 90 && candidate.Width == 160 && candidate.Duration >= 2 && candidate.Source != "" {
				count++
			}
		}
		return count >= 2
	}
	for len(capture.Candidates()) < 7 {
		if !openedPopup && len(capture.Candidates()) >= 6 && estimatesReady() && playbackMetadataReady() {
			if err := session.conn.call(ctx, session.rootSession, "Runtime.evaluate", map[string]any{"expression": "window.open('/popup'); location.href='/advert'", "userGesture": true}, nil); err != nil {
				t.Fatal(err)
			}
			openedPopup = true
		}
		select {
		case <-deadline.C:
			t.Fatalf("capture incomplete: player=%d media=%d popup=%d missing-cookie=%d missing-referer=%d estimates=%t playback-metadata=%t", players.Load(), media.Load(), popup.Load(), missingCookie.Load(), missingReferer.Load(), estimatesReady(), playbackMetadataReady())
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
			assertCapturedDownload(t, ctx, capture, candidate, filepath.Join(work, "download", fmt.Sprint(index)))
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

// A real independently running browser exercises attachment privacy and cleanup.
func TestLiveConnectedChromeFollowsPopupsAndStaysOpen(t *testing.T) {
	if os.Getenv("CAXXXD_BROWSER_TEST") != "1" {
		t.Skip("set CAXXXD_BROWSER_TEST=1 for local browser integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	work := t.TempDir()
	clip := filepath.Join(work, "clip.mp4")
	if err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", clip).Run(); err != nil {
		t.Fatal("generate local media fixture")
	}
	var popup, privateRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/unrelated":
			http.SetCookie(w, &http.Cookie{Name: "unrelated", Value: "fixture-only", Path: "/private.mp4"})
			fmt.Fprint(w, `<script>setInterval(()=>fetch('/private.mp4'),100)</script>`)
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "playback", Value: "fixture-only", Path: "/"})
			fmt.Fprint(w, `<video preload="auto" src="/original.mp4"></video>`)
		case "/popup":
			popup.Add(1)
			fmt.Fprint(w, `<video preload="auto" src="/popup.mp4"></video>`)
		case "/advert":
			fmt.Fprint(w, "Advertising fixture")
		default:
			if r.URL.Path == "/private.mp4" {
				privateRequests.Add(1)
			} else if _, err := r.Cookie("playback"); err != nil {
				http.Error(w, "missing playback cookie", http.StatusForbidden)
				return
			}
			http.ServeFile(w, r, clip)
		}
	}))
	defer server.Close()
	profile := filepath.Join(work, "existing-profile")
	cmd := exec.Command(findBrowser(), "--headless=new", "--no-first-run", "--no-default-browser-check", "--remote-debugging-port=0", "--user-data-dir="+profile, server.URL+"/unrelated")
	process.ConfigureGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal("could not start local fixture browser")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() {
		select {
		case <-done:
			return
		default:
		}
		_ = process.TerminateGroup(cmd)
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = process.KillGroup(cmd)
			<-done
		}
	}()
	var endpoint string
	for endpoint == "" {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			endpoint, _ = activePortEndpoint(string(data))
		}
		if endpoint == "" {
			select {
			case <-ctx.Done():
				t.Fatal("fixture browser did not publish an endpoint")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	u, _ := url.Parse(endpoint)
	port, _ := strconv.Atoi(u.Port())
	capture, err := (Launcher{DebugPort: port, TempDir: work}).Connect(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	s := capture.(*Session)
	for len(capture.Candidates()) < 1 {
		select {
		case <-ctx.Done():
			t.Fatal("existing browser did not capture media")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := s.conn.call(ctx, s.rootSession, "Runtime.evaluate", map[string]any{"expression": "window.open('/popup');location.href='/advert'", "userGesture": true}, nil); err != nil {
		t.Fatal(err)
	}
	for len(capture.Candidates()) < 2 {
		select {
		case <-ctx.Done():
			t.Fatalf("popup not captured, served=%d", popup.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}
	// Give the unrelated page several requests; none may enter this capture.
	for privateRequests.Load() < 2 {
		select {
		case <-ctx.Done():
			t.Fatal("unrelated fixture did not produce requests")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if len(capture.Candidates()) != 2 {
		t.Fatal("unrelated browser tab entered the capture")
	}
	candidate := capture.Candidates()[0]
	assertCapturedDownload(t, ctx, capture, candidate, filepath.Join(work, "download"))
	cookies, err := os.ReadFile(filepath.Join(s.directory, "download.cookies"))
	if err != nil || !strings.Contains(string(cookies), "\tplayback\t") || strings.Contains(string(cookies), "\tunrelated\t") {
		t.Fatal("connected capture did not scope cookies to the selected media")
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Launcher{DebugPort: port}).chromeEndpoint(ctx); err != nil {
		t.Fatal("closing capture closed the user's browser")
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatal("closing capture removed the user's profile")
	}
	if _, err := os.Stat(s.directory); !os.IsNotExist(err) {
		t.Fatal("private capture settings were not removed")
	}
}

func assertCapturedDownload(t *testing.T, ctx context.Context, capture Capture, candidate Candidate, destination string) {
	t.Helper()
	selected, err := capture.Prepare(ctx, candidate.ID)
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
	args, err := ytdlp.BuildCommand(ytdlp.DownloadRequest{URL: selected.URL, ConfigFile: selected.ConfigFile, Mode: mode, Container: domain.VideoContainerMKV, OutputDir: destination})
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
		t.Fatal("downloaded file does not contain expected playable streams")
	}
	if stat, err := os.Stat(selected.ConfigFile); err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatal("download configuration must be private")
	}
}
