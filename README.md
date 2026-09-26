# caxxxd

> media downloads without the flag maze

a small terminal frontend for `yt-dlp` and `ffmpeg` on macOS. download video,
audio, or plain-text subtitles through a short conversation.

![caxxxd waiting for a media URL](docs/screen.png)

## install and update

requires macOS 13 or newer. Homebrew installs the external tools too.

```bash
brew install --cask ballisarium/tap/caxxxd
caxxxd
```

to update:

```bash
brew update
brew upgrade --cask caxxxd
caxxxd --version
```

or download an Apple Silicon / Intel archive from
[Releases](https://github.com/ballisarium/caxxxd/releases).
manual installations also need `yt-dlp`, `ffmpeg`, and `ffprobe` on `PATH`.

## use it

choose **Media URL** or **Web page · beta** in the opening menu. **Supported
sources** explains the available sources. after selecting media, choose the
whole item or a clip, pick video, audio, or subtitles, then review and download.
the destination folder is remembered. passing a URL on the command line goes
straight to the media URL prompt.

```bash
caxxxd
caxxxd "https://www.youtube.com/watch?v=..."
caxxxd --help
```

### capture media from a web page · beta

requires Google Chrome, Chromium, or Microsoft Edge in `/Applications`,
`~/Applications`, or on `PATH`. no browser is installed automatically.

1. choose **Web page · beta** and enter the page address.
2. play the media in the separate browser window; sign in there if needed.
3. return to the terminal and select **Refresh streams**.
4. choose a captured resource, review the format, and download. keep the
   browser open until the download finishes. **Back** closes the capture.

capture watches network responses, including embedded players and new tabs,
for direct video/audio files, HLS playlists, and DASH manifests. individual
stream fragments are excluded. the first 200 distinct resources are kept.
discovery follows the approach of [cat-catch](https://github.com/xifangczy/cat-catch),
implemented independently in Go using Chromium's DevTools pipe protocol;
cat-catch source code is not bundled.

the browser uses a disposable profile, separate from your regular browser.
captured addresses stay out of the resource menu. request context and
domain-scoped cookies are passed to yt-dlp through private temporary files,
removed with the profile on normal exit or Ctrl+C. captured downloads use
neutral filenames. a forced process kill or power loss can leave the temporary
profile in the system temporary directory.

this does not guarantee every page: DRM, browser-only blobs without a reusable
media URL, expiring links, partitioned cookies, and custom authorization headers
may prevent downloads. play or refresh the page and select a fresh resource
when a link expires. existing `--cookies` settings apply to **Media URL**;
browser capture uses only its own session.

### choose a clip

select **Choose a range**, then follow three screens:

1. **Start at** — enter seconds (`90`) or a timecode (`1:30`, `0:01:30`).
2. **End at** — enter the ending position, not the clip length.
3. **Confirm this clip?** — check the start, end, and length, then select
   **Use this clip**. you can change either endpoint or cancel your edits.

the end must follow the start and fit inside the video. **Change time range**
on the review screen lets you revise the clip before downloading.

for a range you already know:

```bash
caxxxd --section 90-120 "https://www.youtube.com/watch?v=..."
```

a range supplied with `--section` stays fixed for that run. clips use stream
copy, so cuts can land near keyframes rather than at an exact frame.

### browser cookies

```bash
caxxxd --cookies
```

choose the local browser where you are signed in. the choice is remembered;
select **Off** to disable it. cookies are off by default.

yt-dlp reads cookies for each lookup and download. caxxxd saves only the browser
name, never a cookie export. macOS may ask for browser-data or Keychain access.
global yt-dlp configuration is ignored to keep this flow predictable.

if a site returns HTTP 403, try updating `yt-dlp` with `brew upgrade yt-dlp`
and selecting a signed-in browser. cookies cannot guarantee access to every item.

### controls and output

- `↑` / `↓` select; `Enter` confirms; typing filters long menus.
- `Ctrl+U` clears a text field; `Ctrl+C` exits from any prompt or menu,
  including after Escape or a partial paste. during a download, it cancels
  the operation and returns to the review screen.
- video preserves its codecs; MKV is the default. MP4 and WebM may limit quality
  to compatible source streams.
- audio defaults to the source format; MP3, M4A, Opus, FLAC, and WAV are available.
- subtitles produce one UTF-8 `.txt` from an uploaded or automatic track.
  no speech transcription is performed.
- cancelling a media download may leave a resumable `.part` file.
- completion requires a nonempty output file, not just a successful tool exit.
- missing HLS/DASH fragments fail the download instead of silently leaving gaps.

one item at a time, with no playlists or DRM bypass. an interactive terminal is
required. download only media you are authorised to save.

## build and check

requires Go 1.26 and the external tools above.

```bash
go install github.com/ballisarium/caxxxd/cmd/caxxxd@latest
```

from a checkout:

```bash
gofmt -l .
go vet ./...
go test ./... -race -count=1
go build -o .scratch/caxxxd ./cmd/caxxxd
git diff --check
```

optional local integration check (Chrome, yt-dlp, ffmpeg, and ffprobe required):

```bash
mkdir -p .scratch
CAXXXD_BROWSER_TEST=1 TMPDIR="$PWD/.scratch" go test ./internal/browser -run TestLiveCapture -v -count=1
```

it generates short fixtures and verifies real downloads from local MP4, M4A, HLS,
DASH, and embedded players.

the normal test suite uses fixtures and fake services, without a network account.

## license

MIT. see [LICENSE](LICENSE).
