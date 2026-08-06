package app

import (
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// streamTable is the shape of the stream table at a given width.
//
// The rows are menu options, and a menu option that does not fit wraps. That
// breaks the menu rather than merely looking untidy: pterm redraws it by
// counting newlines, not by asking where the terminal folded a line, so a
// wrapped row leaves its overflow on screen as the selection moves.
type streamTable struct {
	headings []string
	widths   []int
	compact  bool
}

// The widest layout, and what it needs: seven padded columns, one separator
// between each pair, the extension at the end, and the two columns the menu
// spends on its selector.
var fullTable = streamTable{
	headings: []string{"ID", "TYPE", "RESOLUTION", "FPS", "CODEC", "BITRATE", "SIZE", "EXT"},
	widths:   []int{8, 6, 11, 4, 12, 11, 10, 0},
}

// The narrow layout keeps what identifies a stream and what it costs, and
// drops what can be inferred or lived without.
var compactTable = streamTable{
	headings: []string{"ID", "TYPE", "RESOLUTION", "SIZE", "EXT"},
	widths:   []int{8, 6, 11, 10, 0},
	compact:  true,
}

// selectorRoom is the two columns the menu draws its selector in, plus the one
// the heading is indented by.
const selectorRoom = 3

// tableFor picks the widest layout that fits.
func tableFor(width int) streamTable {
	if width >= fullTable.minimumWidth() {
		return fullTable
	}
	return compactTable
}

// minimumWidth is the narrowest terminal this layout can be drawn in without
// a row wrapping.
func (t streamTable) minimumWidth() int {
	total := selectorRoom + len(t.widths) - 1
	for _, width := range t.widths {
		total += width
	}
	// The last column carries no width of its own; four is enough for "webm".
	return total + 4
}

// streamHeader labels the columns of the stream table.
//
// The menu prefixes every row with two columns, either its selector or the
// blank that stands in for it, and Accent adds one column of its own, so the
// heading carries a single space to land under the same letters.
func (t streamTable) header() string {
	return " " + ui.Cells(t.headings, t.widths)
}

// row describes one stream in the columns this layout has.
func (t streamTable) row(format ytdlp.Format) string {
	size, approximate := format.EffectiveSize()

	cells := []string{
		ui.Truncate(format.ID, 8),
		string(format.Kind),
		resolutionLabel(format),
	}
	if !t.compact {
		cells = append(cells,
			fpsLabel(format),
			ui.Truncate(codecLabel(format), 12),
			bitrateLabel(format),
		)
	}
	cells = append(cells, ui.FormatSize(size, approximate), format.Extension)

	return ui.Cells(cells, t.widths)
}

// streamChoices turns streams into menu rows whose columns line up. The rows
// are plain text: they are what type-to-search matches against.
func streamChoices(formats []ytdlp.Format, table streamTable) []Choice {
	choices := make([]Choice, 0, len(formats)+1)
	for _, format := range formats {
		choices = append(choices, Choice{Label: table.row(format)})
	}
	return choices
}

func resolutionLabel(format ytdlp.Format) string {
	if format.Height <= 0 {
		return "audio only"
	}
	if format.Width <= 0 {
		return strconv.Itoa(format.Height) + "p"
	}
	return strconv.Itoa(format.Width) + "x" + strconv.Itoa(format.Height)
}

func fpsLabel(format ytdlp.Format) string {
	if format.FPS <= 0 {
		return "—"
	}
	return strconv.FormatFloat(format.FPS, 'f', 0, 64)
}

func codecLabel(format ytdlp.Format) string {
	switch format.Kind {
	case ytdlp.FormatKindAudio:
		return shortCodec(format.AudioCodec)
	case ytdlp.FormatKindVideo:
		return shortCodec(format.VideoCodec)
	default:
		return shortCodec(format.VideoCodec) + "+" + shortCodec(format.AudioCodec)
	}
}

// shortCodec keeps codec names readable: avc1.640028 is just avc1 to a human.
func shortCodec(codec string) string {
	if codec == "" || codec == "none" {
		return "—"
	}
	if base, _, found := strings.Cut(codec, "."); found {
		return base
	}
	return codec
}

func bitrateLabel(format ytdlp.Format) string {
	bitrate := format.TotalBitrate
	if bitrate <= 0 {
		bitrate = format.VideoBitrate
	}
	if bitrate <= 0 {
		bitrate = format.AudioBitrate
	}
	if bitrate <= 0 {
		return "—"
	}
	return strconv.FormatFloat(bitrate, 'f', 0, 64) + " kbps"
}
