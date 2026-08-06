package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const maxTimecodeSeconds = int64(1<<63 - 1)

// TimeRange is one download interval in whole seconds. yt-dlp and
// ffmpeg may resolve the actual cut to keyframes, but the requested bounds are
// kept exact for validation and command construction.
type TimeRange struct {
	Start int64
	End   int64
}

// ParseTimeRange parses START-END where each endpoint is SS, MM:SS, or
// HH:MM:SS. Whitespace around the range separator is accepted for copy/paste
// convenience.
func ParseTimeRange(input string) (TimeRange, error) {
	parts := strings.Split(strings.TrimSpace(input), "-")
	if len(parts) != 2 {
		return TimeRange{}, errors.New("expected START-END, for example 05:30-06:20")
	}

	start, err := parseTimecode(parts[0])
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid start time: %w", err)
	}
	end, err := parseTimecode(parts[1])
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid end time: %w", err)
	}

	rangeValue := TimeRange{Start: start, End: end}
	if err := rangeValue.Validate(); err != nil {
		return TimeRange{}, err
	}
	return rangeValue, nil
}

// Validate checks the relationship between the two endpoints without needing
// metadata. A zero-length section is not a useful download.
func (r TimeRange) Validate() error {
	if r.Start < 0 || r.End < 0 {
		return errors.New("time range cannot contain negative timestamps")
	}
	if r.Start >= r.End {
		return errors.New("section start must be before section end")
	}
	return nil
}

// ValidateDuration makes sure the requested endpoint fits inside the media
// item whose metadata has just been fetched.
func (r TimeRange) ValidateDuration(duration float64) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return errors.New("video duration is unavailable, so the section cannot be validated")
	}
	if float64(r.End) > duration {
		return fmt.Errorf("section end %s exceeds video duration %s", formatTimecode(r.End), formatDuration(duration))
	}
	return nil
}

// Selector is the value yt-dlp expects for a time-based --download-sections
// argument. The leading star distinguishes a time range from a chapter name.
func (r TimeRange) Selector() string {
	return "*" + r.String()
}

func (r TimeRange) String() string {
	return formatTimecode(r.Start) + "-" + formatTimecode(r.End)
}

func parseTimecode(input string) (int64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, errors.New("timestamp is empty")
	}

	parts := strings.Split(value, ":")
	switch len(parts) {
	case 1:
		return parseTimecodeComponent(parts[0], "seconds")
	case 2:
		minutes, err := parseTimecodeComponent(parts[0], "minutes")
		if err != nil {
			return 0, err
		}
		seconds, err := parseTimecodeComponent(parts[1], "seconds")
		if err != nil {
			return 0, err
		}
		if seconds > 59 {
			return 0, errors.New("seconds must be between 00 and 59")
		}
		return combineTimecode(0, minutes, seconds)
	case 3:
		hours, err := parseTimecodeComponent(parts[0], "hours")
		if err != nil {
			return 0, err
		}
		minutes, err := parseTimecodeComponent(parts[1], "minutes")
		if err != nil {
			return 0, err
		}
		seconds, err := parseTimecodeComponent(parts[2], "seconds")
		if err != nil {
			return 0, err
		}
		if minutes > 59 {
			return 0, errors.New("minutes must be between 00 and 59 in HH:MM:SS")
		}
		if seconds > 59 {
			return 0, errors.New("seconds must be between 00 and 59")
		}
		return combineTimecode(hours, minutes, seconds)
	default:
		return 0, errors.New("use SS, MM:SS, or HH:MM:SS")
	}
}

func parseTimecodeComponent(input, name string) (int64, error) {
	if strings.HasPrefix(strings.TrimSpace(input), "+") {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	value, err := strconv.ParseInt(strings.TrimSpace(input), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func combineTimecode(hours, minutes, seconds int64) (int64, error) {
	if hours > maxTimecodeSeconds/3600 {
		return 0, errors.New("timestamp is too large")
	}
	total := hours * 3600
	if minutes > (maxTimecodeSeconds-total)/60 {
		return 0, errors.New("timestamp is too large")
	}
	total += minutes * 60
	if seconds > maxTimecodeSeconds-total {
		return 0, errors.New("timestamp is too large")
	}
	return total + seconds, nil
}

func formatTimecode(seconds int64) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	rest := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, rest)
	}
	return fmt.Sprintf("%d:%02d", minutes, rest)
}

func formatDuration(seconds float64) string {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return "unknown"
	}
	return formatTimecode(int64(seconds + 0.5))
}
