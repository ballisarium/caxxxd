package ytdlp

import (
	"strconv"
	"strings"
)

// EventKind identifies which structured marker a line carried.
type EventKind int

const (
	EventProgress EventKind = iota
	EventPostProcess
	EventCompletedFile
)

const progressFieldCount = 6

// Progress is one download progress sample. ETAKnown separates "no estimate
// yet" from "about to finish": yt-dlp reports the first as NA and the second
// as a plain 0, and they mean very different things to someone watching.
type Progress struct {
	Status              string
	DownloadedBytes     int64
	TotalBytes          int64
	EstimatedTotalBytes int64
	SpeedBytes          float64
	ETASeconds          int64
	ETAKnown            bool
}

// Total returns the best known total size and whether one is known at all.
func (p Progress) Total() (int64, bool) {
	if p.TotalBytes > 0 {
		return p.TotalBytes, true
	}
	if p.EstimatedTotalBytes > 0 {
		return p.EstimatedTotalBytes, true
	}
	return 0, false
}

// Fraction returns completion between 0 and 1, or 0 when the total is unknown.
func (p Progress) Fraction() float64 {
	total, known := p.Total()
	if !known || p.DownloadedBytes <= 0 {
		return 0
	}
	if p.DownloadedBytes >= total {
		return 1
	}
	return float64(p.DownloadedBytes) / float64(total)
}

// Event is a recognized line of yt-dlp output.
type Event struct {
	Kind     EventKind
	Progress Progress
	Stage    string
	FilePath string
}

// ParseOutputLine recognizes only caxxxd's own markers. Anything else, including
// yt-dlp's regular progress text, is reported as unrecognized.
func ParseOutputLine(line string) (Event, bool) {
	switch {
	case strings.HasPrefix(line, progressMarker):
		fields := strings.Split(strings.TrimPrefix(line, progressMarker), "\t")
		if len(fields) != progressFieldCount {
			return Event{}, false
		}
		eta, etaKnown := parseOptionalInt(fields[5])
		return Event{
			Kind: EventProgress,
			Progress: Progress{
				Status:              fields[0],
				DownloadedBytes:     parseInt(fields[1]),
				TotalBytes:          parseInt(fields[2]),
				EstimatedTotalBytes: parseInt(fields[3]),
				SpeedBytes:          parseFloat(fields[4]),
				ETASeconds:          eta,
				ETAKnown:            etaKnown,
			},
		}, true
	case strings.HasPrefix(line, postProcessMarker):
		return Event{Kind: EventPostProcess, Stage: strings.TrimPrefix(line, postProcessMarker)}, true
	case strings.HasPrefix(line, fileMarker):
		return Event{Kind: EventCompletedFile, FilePath: sanitize(strings.TrimPrefix(line, fileMarker))}, true
	default:
		return Event{}, false
	}
}

// parseInt treats missing and malformed values as zero: a bad progress sample
// must never stop a download that is otherwise fine.
func parseInt(value string) int64 {
	parsed, _ := parseOptionalInt(value)
	return parsed
}

// parseOptionalInt also reports whether the field carried a real number.
func parseOptionalInt(value string) (int64, bool) {
	if value == "" || value == "NA" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseFloat(value string) float64 {
	if value == "" || value == "NA" {
		return 0
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}
