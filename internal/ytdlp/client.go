package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
)

// ErrNoFormats means yt-dlp understood the page but offered nothing to download.
var ErrNoFormats = errors.New("no downloadable formats")

// OutputRunner runs a command and returns its standard output.
type OutputRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecOutputRunner is the real implementation. Arguments are passed as a slice
// so no shell ever sees the URL.
type ExecOutputRunner struct{}

// Output implements OutputRunner.
func (ExecOutputRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Client fetches metadata for a single media item.
type Client struct {
	Binary string
	Runner OutputRunner
}

// NewClient returns a Client that shells out to the given yt-dlp binary.
func NewClient(binary string) Client {
	return Client{Binary: binary, Runner: ExecOutputRunner{}}
}

// Fetch loads and normalizes metadata for rawURL.
func (c Client) Fetch(ctx context.Context, rawURL string) (MediaInfo, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return MediaInfo{}, errors.New("invalid URL")
	}

	payload, err := c.Runner.Output(ctx, c.Binary,
		"--dump-single-json",
		"--no-warnings",
		"--no-playlist",
		rawURL,
	)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("fetch metadata: %w", err)
	}

	var raw RawInfo
	if err := json.Unmarshal(payload, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("decode metadata: %w", err)
	}
	formats := NormalizeFormats(raw.Formats)
	if len(formats) == 0 {
		return MediaInfo{}, ErrNoFormats
	}

	uploader := raw.Uploader
	if uploader == "" {
		uploader = raw.Channel
	}

	// Everything below is drawn on a terminal, and all of it was written by
	// whoever uploaded the media.
	return MediaInfo{
		ID:           sanitize(raw.ID),
		Title:        sanitize(raw.Title),
		Uploader:     sanitize(uploader),
		Platform:     sanitize(raw.ExtractorKey),
		Duration:     raw.Duration,
		WebpageURL:   raw.WebpageURL,
		ThumbnailURL: raw.Thumbnail,
		Formats:      formats,
	}, nil
}
