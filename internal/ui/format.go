package ui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// FormatBytes renders a size with IEC units. An unknown size renders as "?".
func FormatBytes(size int64) string {
	if size <= 0 {
		return "?"
	}

	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	index := -1
	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

// FormatCounted renders bytes that have actually been counted. Zero is a real
// answer there — nothing has arrived yet — rather than the "?" that stands for
// a size nobody reported.
func FormatCounted(size int64) string {
	if size <= 0 {
		return "0 B"
	}
	return FormatBytes(size)
}

// FormatSize renders a size, marking estimates with a leading tilde.
func FormatSize(size int64, approximate bool) string {
	formatted := FormatBytes(size)
	if approximate && size > 0 {
		return "~" + formatted
	}
	return formatted
}

// FormatSpeed renders a transfer rate, or an em dash when it is unknown.
func FormatSpeed(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "—"
	}
	return FormatBytes(int64(bytesPerSecond)) + "/s"
}

// FormatETA renders a remaining time, or an em dash when it is unknown.
func FormatETA(seconds int64) string {
	if seconds <= 0 {
		return "—"
	}

	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dh %02dm", seconds/3600, (seconds%3600)/60)
	}
}

// FormatDuration renders a media duration as M:SS, or H:MM:SS past an hour.
func FormatDuration(seconds float64) string {
	if seconds <= 0 {
		return "Unknown duration"
	}

	total := int64(seconds + 0.5)
	hours := total / 3600
	minutes := (total % 3600) / 60
	rest := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, rest)
	}
	return fmt.Sprintf("%d:%02d", minutes, rest)
}

// Pad right-pads text to width terminal columns. Go's %-*s pads by bytes, so a
// cell holding "—" or "…" would come out short and skew a whole table.
func Pad(text string, width int) string {
	gap := width - runewidth.StringWidth(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}

// Cells joins padded cells into one aligned row.
func Cells(values []string, widths []int) string {
	parts := make([]string, 0, len(values))
	for index, value := range values {
		if index == len(values)-1 {
			parts = append(parts, value)
			continue
		}
		parts = append(parts, Pad(value, widths[index]))
	}
	return strings.Join(parts, " ")
}

// Truncate shortens plain text to at most width terminal columns, ending with
// an ellipsis. Columns, not runes: one CJK character is a single rune two
// columns wide, and counting it as one would return a string half again as
// wide as the space it was given. Styled text is the chrome's own business and
// never comes here.
func Truncate(text string, width int) string {
	switch {
	case width <= 0:
		return ""
	case runewidth.StringWidth(text) <= width:
		return text
	case width == 1:
		return "…"
	default:
		return runewidth.Truncate(text, width, "…")
	}
}

// padLeft right-aligns text within width columns, for the numbers that read as
// a column rather than as a label.
func padLeft(text string, width int) string {
	gap := width - runewidth.StringWidth(text)
	if gap <= 0 {
		return text
	}
	return strings.Repeat(" ", gap) + text
}
