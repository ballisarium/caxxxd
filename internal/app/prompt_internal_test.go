package app

import (
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/ui"
)

// wideEnough is the room a menu row has on the widest layout.
const wideEnough = ui.MaxWidth - selectorColumns

func TestAMenuAlignsTheDetailsAgainstEachOther(t *testing.T) {
	labels := menuLabels([]Choice{
		{Label: "MKV", Detail: "maximum source quality"},
		{Label: "WebM", Detail: "VP9/AV1 + Opus"},
	}, wideEnough)

	first := strings.Index(labels[0], "maximum")
	second := strings.Index(labels[1], "VP9")
	if first != second {
		t.Fatalf("details start at %d and %d, want one column:\n%s", first, second, strings.Join(labels, "\n"))
	}
}

func TestAMenuDoesNotAlignATableAgainstAWayBack(t *testing.T) {
	// A stream table is one long label per row with nothing after it. Padding
	// the way back to that width would strand its few words off to the right.
	labels := menuLabels([]Choice{
		{Label: "137      video  1920x1080   30   avc1         4500 kbps   71.5 MiB   mp4"},
		{Label: "248      video  1920x1080   30   vp9          3000 kbps   ~49.6 MiB  webm"},
		backChoice,
	}, wideEnough)

	if labels[len(labels)-1] != "‹ Back   return to the previous step" {
		t.Fatalf("the way back was padded to the table's width: %q", labels[len(labels)-1])
	}
}

func TestARowWithoutADetailIsLeftAsItIs(t *testing.T) {
	row := "18       muxed  640x360     30   avc1+mp4a"

	labels := menuLabels([]Choice{{Label: row}}, wideEnough)
	if labels[0] != row {
		t.Fatalf("menuLabels rewrote a table row: %q", labels[0])
	}
}

func TestIdenticalRowsStayDistinct(t *testing.T) {
	// pterm identifies a choice by its text, so two rows that read the same
	// would be the same option, and the second could never be picked.
	labels := menuLabels([]Choice{{Label: "same"}, {Label: "same"}, {Label: "same"}}, wideEnough)

	seen := map[string]bool{}
	for _, label := range labels {
		if seen[label] {
			t.Fatalf("two options share the text %q", label)
		}
		seen[label] = true
	}
}

func TestTypeToSearchIsOfferedOnlyForLongMenus(t *testing.T) {
	if filterable(4) {
		t.Fatal("a four-row menu does not need searching")
	}
	if !filterable(filterThreshold) {
		t.Fatalf("a %d-row menu should offer type-to-search", filterThreshold)
	}
}

func TestNoMenuRowIsWiderThanTheTerminal(t *testing.T) {
	// A row that does not fit wraps, and the menu redraws by counting
	// newlines: the overflow would stay on screen as the selection moved.
	room := ui.MinWidth - selectorColumns

	labels := menuLabels([]Choice{
		{Label: "Manual format selection", Detail: "pick exact streams from the table"},
		{Label: "Best available", Detail: "whatever the site offers"},
		{Label: "MP4", Detail: "H.264 + AAC/M4A where available, better Apple compatibility"},
		{Label: strings.Repeat("very long label ", 8)},
		backChoice,
	}, room)

	for _, label := range labels {
		if width := len([]rune(label)); width > room {
			t.Fatalf("a row is %d columns wide in %d columns of room: %q", width, room, label)
		}
	}
}
