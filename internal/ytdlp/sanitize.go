package ytdlp

import (
	"regexp"
	"strings"
)

// escapeSequence matches what a terminal acts on rather than shows: an OSC
// string, a two-byte escape, or a CSI sequence.
var escapeSequence = regexp.MustCompile(
	`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC ... BEL or ST
		`|\x1b[@-Z\\-_]` + // Fe escapes
		`|\x1b\[[0-?]*[ -/]*[@-~]`, // CSI
)

// sanitize makes a string from yt-dlp safe to draw.
//
// Titles, uploader names, stream ids and diagnostics all originate on a remote
// page and reach caxxxd through another program. A title carrying an escape
// sequence could move the cursor, clear the screen, or open a hyperlink; one
// carrying a newline could break the panel it is drawn inside, or forge a line
// in a saved transcript. None of that is anything a title needs to do, so it
// is removed at the edge and never reaches the code that draws.
func sanitize(text string) string {
	text = escapeSequence.ReplaceAllString(text, "")

	return strings.TrimSpace(strings.Map(func(character rune) rune {
		switch {
		case character == '\t', character == '\n', character == '\r':
			// These carry meaning in a single-line field: a space is the most
			// they are allowed to mean.
			return ' '
		case character < 0x20, character == 0x7f:
			return -1
		default:
			return character
		}
	}, text))
}
