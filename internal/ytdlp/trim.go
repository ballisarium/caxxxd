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
	"strings"
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
	Probe  string
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

	seek, err := t.inputSeek(ctx, path)
	if err != nil {
		return fmt.Errorf("ffprobe could not inspect clipped file: %w", err)
	}
	mapArgs, err := t.mapStreams(ctx, path)
	if err != nil {
		return fmt.Errorf("ffprobe could not inspect clipped streams: %w", err)
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
		"-ss", strconv.FormatFloat(seek, 'f', -6, 64),
		"-i", path,
		"-t", strconv.FormatInt(length, 10),
	}
	args = append(args, mapArgs...)
	args = append(args, "-c", "copy", "-avoid_negative_ts", "make_zero", temporaryPath)

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

// inputSeek is the timestamp of the first packet in the already-sectioned
// file. yt-dlp may seek a remote stream to a keyframe and leave that stream's
// first packet several seconds after the container's zero. Seeking by the
// user's original timestamp again would then cut past the file, producing a
// tiny or empty result.
func (t Trimmer) inputSeek(ctx context.Context, path string) (float64, error) {
	var lastErr error
	for _, stream := range []string{"v:0", "a:0"} {
		start, err := t.streamStart(ctx, stream, path)
		if err == nil {
			if start < 0 {
				return 0, nil
			}
			return start, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no media stream was found")
	}
	return 0, lastErr
}

func (t Trimmer) streamStart(ctx context.Context, stream, path string) (float64, error) {
	args := []string{
		"-v", "error",
		"-select_streams", stream,
		"-read_intervals", "%+#1",
		"-show_entries", "packet=pts_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	}
	output, err := runFFProbe(ctx, t.probeBinary(), args)
	if err != nil {
		return 0, err
	}
	value := strings.TrimSpace(string(output))
	if value == "" || value == "N/A" {
		return 0, errors.New("media stream has no start time")
	}
	start, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid media start time %q: %w", value, err)
	}
	return start, nil
}

// mapStreams drops an attached thumbnail from audio-only outputs. A picture
// is exposed as a video stream by ffprobe, but containers such as Ogg Opus do
// not accept that JPEG as a regular copied video stream.
func (t Trimmer) mapStreams(ctx context.Context, path string) ([]string, error) {
	args := []string{
		"-v", "error",
		"-show_entries", "stream=codec_type:stream_disposition=attached_pic",
		"-of", "csv=p=0",
		path,
	}
	output, err := runFFProbe(ctx, t.probeBinary(), args)
	if err != nil {
		return nil, err
	}

	seenStream := false
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) < 2 {
			continue
		}
		seenStream = true
		if fields[0] == "video" && fields[1] == "0" {
			return []string{"-map", "0"}, nil
		}
	}
	if seenStream {
		return []string{"-map", "0:a:0"}, nil
	}
	return []string{"-map", "0"}, nil
}

func (t Trimmer) probeBinary() string {
	if t.Probe != "" {
		return t.Probe
	}
	if filepath.Base(t.Binary) == "ffmpeg" && filepath.Dir(t.Binary) != "." {
		return filepath.Join(filepath.Dir(t.Binary), "ffprobe")
	}
	return "ffprobe"
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

func runFFProbe(ctx context.Context, binary string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	process.ConfigureGroup(cmd)
	return cmd.Output()
}
