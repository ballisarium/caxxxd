package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/domain"
)

var (
	srtBlockBreak = regexp.MustCompile(`\n[\t ]*\n+`)
	srtTiming     = regexp.MustCompile(`^(\d+):(\d{2}):(\d{2})[,.](\d{3})[\t ]+-->[\t ]+(\d+):(\d{2}):(\d{2})[,.](\d{3})(?:[\t ].*)?$`)
	subtitleTag   = regexp.MustCompile(`</?[[:alpha:]][^>]*>`)
	assOverride   = regexp.MustCompile(`\{\\[^}]*\}`)
)

// TranscriptConverter turns one SRT subtitle file into atomic plain-text
// output. The source is produced by yt-dlp's subtitle conversion step.
type TranscriptConverter struct{}

type subtitleCue struct {
	start float64
	end   float64
	text  string
}

// Convert strips subtitle structure and writes a UTF-8 transcript. Automatic
// captions receive rolling-text de-duplication; uploaded captions keep one
// normalized line per cue.
func (TranscriptConverter) Convert(ctx context.Context, source, destination string, automatic bool, section *domain.TimeRange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read subtitles: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	cues, err := parseSRT(ctx, string(payload))
	if err != nil {
		return err
	}
	lines, err := transcriptLines(ctx, cues, automatic, section)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return errors.New("subtitles contain no text in the selected time range")
	}

	return writeAtomicText(ctx, destination, strings.Join(lines, "\n")+"\n")
}

func parseSRT(ctx context.Context, payload string) ([]subtitleCue, error) {
	normalized := strings.TrimPrefix(strings.ReplaceAll(payload, "\r\n", "\n"), "\ufeff")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	blocks := srtBlockBreak.Split(strings.TrimSpace(normalized), -1)
	cues := make([]subtitleCue, 0, len(blocks))

	for blockIndex, block := range blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines := strings.Split(block, "\n")
		timingIndex := -1
		var timing []string
		for index, line := range lines {
			if match := srtTiming.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				timingIndex = index
				timing = match
				break
			}
		}
		if timingIndex < 0 || timingIndex > 1 {
			return nil, fmt.Errorf("subtitle cue %d has no valid timing line", blockIndex+1)
		}
		if timingIndex == 1 {
			if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err != nil {
				return nil, fmt.Errorf("subtitle cue %d has an invalid sequence number", blockIndex+1)
			}
		}
		if timingIndex == len(lines)-1 {
			return nil, fmt.Errorf("subtitle cue %d has no text", blockIndex+1)
		}

		text := cleanSubtitleText(lines[timingIndex+1:])
		if text == "" {
			return nil, fmt.Errorf("subtitle cue %d has no readable text", blockIndex+1)
		}
		start, err := srtSeconds(timing[1:5])
		if err != nil {
			return nil, fmt.Errorf("parse subtitle start time: %w", err)
		}
		end, err := srtSeconds(timing[5:9])
		if err != nil {
			return nil, fmt.Errorf("parse subtitle end time: %w", err)
		}
		if end <= start {
			return nil, fmt.Errorf("subtitle cue %d ends before it starts", blockIndex+1)
		}
		cues = append(cues, subtitleCue{start: start, end: end, text: text})
	}

	if len(cues) == 0 {
		return nil, errors.New("subtitle file contains no readable SRT cues")
	}
	return cues, nil
}

func srtSeconds(parts []string) (float64, error) {
	if len(parts) != 4 {
		return 0, errors.New("invalid SRT timestamp")
	}
	values := make([]int, len(parts))
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return 0, err
		}
		values[index] = value
	}
	if values[1] > 59 || values[2] > 59 {
		return 0, errors.New("minutes and seconds must be between 00 and 59")
	}
	return float64(values[0]*3600+values[1]*60+values[2]) + float64(values[3])/1000, nil
}

func cleanSubtitleText(lines []string) string {
	text := strings.Join(lines, " ")
	text = html.UnescapeString(text)
	text = assOverride.ReplaceAllString(text, "")
	text = subtitleTag.ReplaceAllString(text, "")
	text = sanitize(text)
	return strings.Join(strings.Fields(text), " ")
}

func transcriptLines(ctx context.Context, cues []subtitleCue, automatic bool, section *domain.TimeRange) ([]string, error) {
	lines := make([]string, 0, len(cues))
	var previousEnd float64
	for _, cue := range cues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if section != nil && (cue.end <= float64(section.Start) || cue.start >= float64(section.End)) {
			continue
		}
		if automatic && len(lines) > 0 && cue.start < previousEnd {
			previous := strings.Fields(lines[len(lines)-1])
			current := strings.Fields(cue.text)
			overlap := wordOverlap(previous, current)
			if overlap > 0 {
				if overlap < len(current) {
					lines[len(lines)-1] += " " + strings.Join(current[overlap:], " ")
				}
				previousEnd = cue.end
				continue
			}
		}
		lines = append(lines, cue.text)
		previousEnd = cue.end
	}
	return lines, nil
}

func wordOverlap(previous, current []string) int {
	maximum := min(len(previous), len(current))
	for size := maximum; size > 0; size-- {
		matches := true
		for index := 0; index < size; index++ {
			if !strings.EqualFold(previous[len(previous)-size+index], current[index]) {
				matches = false
				break
			}
		}
		if matches {
			return size
		}
	}
	return 0
}

func writeAtomicText(ctx context.Context, destination, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".caxxxd-transcript-*.tmp")
	if err != nil {
		return fmt.Errorf("create transcript: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.WriteString(text); err != nil {
		temporary.Close()
		return fmt.Errorf("write transcript: %w", err)
	}
	if err := ctx.Err(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set transcript permissions: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync transcript: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close transcript: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("finish transcript: %w", err)
	}
	return nil
}
