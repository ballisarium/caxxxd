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

paste one media URL, choose the whole item or a clip, pick video, audio, or
subtitles, then review and download. the destination folder is remembered.

```bash
caxxxd
caxxxd "https://www.youtube.com/watch?v=..."
caxxxd --help
```

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
- `Ctrl+U` clears a text field; `Ctrl+C` cancels or exits.
- video preserves its codecs; MKV is the default. MP4 and WebM may limit quality
  to compatible source streams.
- audio defaults to the source format; MP3, M4A, Opus, FLAC, and WAV are available.
- subtitles produce one UTF-8 `.txt` from an uploaded or automatic track.
  no speech transcription is performed.
- cancelling a media download may leave a resumable `.part` file.

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
go build ./cmd/caxxxd
git diff --check
```

the normal test suite uses fixtures and fake services, without a network account.

## license

MIT. see [LICENSE](LICENSE).
- completion requires a nonempty output file, not just a successful tool exit.
- missing HLS/DASH fragments fail the download instead of silently leaving gaps.
