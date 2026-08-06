package app

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// FailureCategory is the user-facing classification of a failure. Every
// category maps to a concrete next step.
type FailureCategory string

const (
	FailureNone            FailureCategory = ""
	FailureInvalidURL      FailureCategory = "Invalid URL"
	FailureUnavailable     FailureCategory = "Media is unavailable"
	FailureNoFormats       FailureCategory = "No downloadable formats"
	FailureNetwork         FailureCategory = "Network or site error"
	FailureTool            FailureCategory = "yt-dlp failed"
	FailureOutputDirectory FailureCategory = "Output directory error"
	FailureCancelled       FailureCategory = "Download cancelled"
	FailureProcessing      FailureCategory = "Clipping failed"
)

// Failure is what an error report shows: a category, a next step, and the raw
// diagnostics behind them.
type Failure struct {
	Category FailureCategory
	NextStep string
	Detail   string
}

// outputDirError marks a failure that is about the destination folder rather
// than about the media itself.
type outputDirError struct {
	err error
}

func (e *outputDirError) Error() string { return "output directory: " + e.err.Error() }
func (e *outputDirError) Unwrap() error { return e.err }

// startError marks yt-dlp never having started, as opposed to a run that
// started and then went wrong. The distinction has to be carried rather than
// read out of the message: "no such file or directory" is what a missing
// binary and an unusable folder both say.
type startError struct {
	err error
}

func (e *startError) Error() string { return "start yt-dlp: " + e.err.Error() }
func (e *startError) Unwrap() error { return e.err }

// classifyMetadataError turns an opaque yt-dlp failure into a category the
// user can act on. The raw output is kept for the details section.
func classifyMetadataError(err error) Failure {
	if err == nil {
		return Failure{}
	}

	detail := err.Error()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		detail = strings.TrimSpace(string(exitErr.Stderr))
	}
	haystack := strings.ToLower(detail)

	switch {
	case errors.Is(err, ytdlp.ErrNoFormats):
		return Failure{
			Category: FailureNoFormats,
			NextStep: "Nothing downloadable was offered for this link. Try the original page URL.",
			Detail:   detail,
		}
	case strings.Contains(haystack, "invalid url"), strings.Contains(haystack, "unsupported url"):
		return Failure{
			Category: FailureInvalidURL,
			NextStep: "Check the link and paste the full address, including https://",
			Detail:   detail,
		}
	case containsAny(haystack, "video unavailable", "private video", "members-only", "has been removed", "sign in to confirm", "drm"):
		return Failure{
			Category: FailureUnavailable,
			NextStep: "This item cannot be fetched without an account, or it no longer exists.",
			Detail:   detail,
		}
	case containsAny(haystack, "unable to download webpage", "http error", "connection", "network", "timed out", "temporary failure", "name resolution", "ssl"):
		return Failure{
			Category: FailureNetwork,
			NextStep: "Check your connection and try the link again.",
			Detail:   detail,
		}
	default:
		return Failure{
			Category: FailureTool,
			NextStep: "yt-dlp could not read this link. Updating it often helps: brew upgrade yt-dlp",
			Detail:   detail,
		}
	}
}

// classifyDownloadError maps a failed run onto a category the user can act on.
func classifyDownloadError(err error, logs []string) Failure {
	if err == nil {
		return Failure{}
	}

	detail := strings.TrimSpace(strings.Join(logs, "\n"))
	if detail == "" {
		detail = err.Error()
	}

	var outputErr *outputDirError
	if errors.As(err, &outputErr) {
		return Failure{
			Category: FailureOutputDirectory,
			NextStep: "caxxxd could not use that folder. Pick another one from the review step.",
			Detail:   err.Error(),
		}
	}

	var launchErr *startError
	if errors.As(err, &launchErr) {
		return Failure{
			Category: FailureTool,
			NextStep: "caxxxd could not start yt-dlp. Check that it is still installed and on your PATH.",
			Detail:   err.Error(),
		}
	}
	if errors.Is(err, context.Canceled) {
		return Failure{Category: FailureCancelled, NextStep: "Nothing was kept.", Detail: detail}
	}

	haystack := strings.ToLower(detail)
	switch {
	case containsAny(haystack, "video unavailable", "private video", "members-only", "has been removed", "sign in to confirm", "drm"):
		return Failure{
			Category: FailureUnavailable,
			NextStep: "This item cannot be downloaded without an account, or it no longer exists.",
			Detail:   detail,
		}
	case containsAny(haystack, "requested format is not available", "no video formats"):
		return Failure{
			Category: FailureNoFormats,
			NextStep: "That exact combination is not offered. Try another quality or container.",
			Detail:   detail,
		}
	case containsAny(haystack, "unable to download", "http error", "connection", "network", "timed out", "temporary failure", "name resolution", "ssl"):
		return Failure{
			Category: FailureNetwork,
			NextStep: "Check your connection and try again.",
			Detail:   detail,
		}
	case containsAny(haystack, "permission denied", "read-only file system", "no such file or directory"):
		return Failure{
			Category: FailureOutputDirectory,
			NextStep: "caxxxd could not write to that folder. Pick another one from the review step.",
			Detail:   detail,
		}
	default:
		return Failure{
			Category: FailureTool,
			NextStep: "yt-dlp stopped with an error. The details say why; updating often helps: brew upgrade yt-dlp",
			Detail:   detail,
		}
	}
}

func classifyProcessingError(err error) Failure {
	detail := "ffmpeg could not finalize the clipped file"
	if err != nil {
		detail = strings.TrimSpace(err.Error())
	}
	return Failure{
		Category: FailureProcessing,
		NextStep: "The section was downloaded, but ffmpeg could not finalize it. Try the same section again.",
		Detail:   detail,
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
