// Package deps checks that the external tools caxxxd drives are installed and
// runnable. caxxxd never installs anything itself; it only reports.
package deps

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// Dependency describes a single external tool.
type Dependency struct {
	Name    string
	Path    string
	Version string
	Found   bool
	Err     error
}

// Status is the result of a full dependency check.
type Status struct {
	YTDLP  Dependency
	FFmpeg Dependency
}

// Ready reports whether caxxxd can run downloads.
func (s Status) Ready() bool {
	return s.YTDLP.Found && s.FFmpeg.Found
}

// Missing lists the tools the user still needs to install, in display order.
func (s Status) Missing() []string {
	missing := make([]string, 0, 2)
	for _, dependency := range []Dependency{s.YTDLP, s.FFmpeg} {
		if !dependency.Found {
			missing = append(missing, dependency.Name)
		}
	}
	return missing
}

// Checker resolves and probes the external tools. Both hooks are injectable so
// tests never touch the real PATH.
type Checker struct {
	LookPath func(string) (string, error)
	Version  func(context.Context, string) (string, error)
}

// NewChecker returns a Checker wired to the real filesystem.
func NewChecker() Checker {
	return Checker{
		LookPath: exec.LookPath,
		Version:  executableVersion,
	}
}

// Check probes yt-dlp and ffmpeg.
func (c Checker) Check(ctx context.Context) Status {
	return Status{
		YTDLP:  c.checkOne(ctx, "yt-dlp"),
		FFmpeg: c.checkOne(ctx, "ffmpeg"),
	}
}

func (c Checker) checkOne(ctx context.Context, name string) Dependency {
	path, err := c.LookPath(name)
	if err != nil {
		return Dependency{Name: name, Err: err}
	}

	version, err := c.Version(ctx, path)
	return Dependency{
		Name:    name,
		Path:    path,
		Version: version,
		Found:   err == nil,
		Err:     err,
	}
}

// versionFlags are tried in order. yt-dlp answers to the GNU-style flag, while
// ffmpeg accepts only the single-dash form and exits nonzero on the other.
var versionFlags = []string{"--version", "-version"}

// executableVersion asks a tool for its version and keeps the first line, which
// is the only part worth showing: ffmpeg follows it with its whole build config.
func executableVersion(ctx context.Context, path string) (string, error) {
	var lastErr error
	for _, flag := range versionFlags {
		version, err := runVersion(ctx, path, flag)
		if err == nil {
			return version, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func runVersion(ctx context.Context, path string, flag string) (string, error) {
	cmd := exec.CommandContext(ctx, path, flag)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}

	output := strings.TrimSpace(stdout.String())
	if line, _, found := strings.Cut(output, "\n"); found {
		return strings.TrimSpace(line), nil
	}
	return output, nil
}
