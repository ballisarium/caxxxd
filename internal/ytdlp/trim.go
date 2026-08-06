package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/process"
)

const trimWaitGrace = 5 * time.Second

// Trimmer normalizes a section that yt-dlp has already downloaded. The first
// seek happens remotely, so this pass only reads the short local result; -c
// copy keeps the audio and video codecs untouched.
type Trimmer struct {
	Binary string
}

// Trim seeks the downloaded section again as one local input and atomically
// replaces it with a timestamp-normalized copy. Keeping the temporary file in
// the input directory makes the final rename atomic on the same filesystem.
func (t Trimmer) Trim(ctx context.Context, path string, section domain.TimeRange) error {
	if err := section.Validate(); err != nil {
		return fmt.Errorf("invalid section: %w", err)
	}
	if path == "" {
		return errors.New("ffmpeg could not finalize clipped file: output path is empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("ffmpeg could not finalize clipped file: %w", err)
	}
	if info.IsDir() {
		return errors.New("ffmpeg could not finalize clipped file: output path is a directory")
	}

	temporary, err := temporaryTrimFile(path)
	if err != nil {
		return fmt.Errorf("ffmpeg could not create a temporary clip: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("ffmpeg could not create a temporary clip: %w", err)
	}
	defer os.Remove(temporaryPath)

	length := section.End - section.Start
	args := []string{
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-ss", strconv.FormatInt(section.Start, 10),
		"-i", path,
		"-t", strconv.FormatInt(length, 10),
		"-map", "0",
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		temporaryPath,
	}

	output, err := runFFmpeg(ctx, t.Binary, args)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if detail := sanitize(string(output)); detail != "" {
			return fmt.Errorf("ffmpeg could not finalize clipped file: %s: %w", detail, err)
		}
		return fmt.Errorf("ffmpeg could not finalize clipped file: %w", err)
	}

	if err := os.Chmod(temporaryPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("ffmpeg could not preserve clip permissions: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("ffmpeg could not replace clipped file: %w", err)
	}
	return nil
}

func temporaryTrimFile(path string) (*os.File, error) {
	directory := filepath.Dir(path)
	extension := filepath.Ext(path)
	if extension == "" {
		extension = ".mkv"
	}
	pattern := "." + filepath.Base(path) + ".caxxxd-trim-*" + extension
	return os.CreateTemp(directory, pattern)
}

func runFFmpeg(ctx context.Context, binary string, args []string) ([]byte, error) {
	if binary == "" {
		binary = "ffmpeg"
	}

	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &output
	cmd.Stderr = &output
	process.ConfigureGroup(cmd)
	cmd.WaitDelay = trimWaitGrace
	finished := make(chan struct{})
	cmd.Cancel = func() error {
		err := process.TerminateGroup(cmd)
		go func() {
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-finished:
			case <-timer.C:
				_ = process.KillGroup(cmd)
			}
		}()
		return err
	}

	err := cmd.Run()
	close(finished)
	return output.Bytes(), err
}
