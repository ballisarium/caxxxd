package app

import (
	"strings"
	"testing"
)

func TestMenuQuestionTextSeparatesTheHeadingFromChoices(t *testing.T) {
	got := menuQuestionText("What now?")
	want := "─────────\nWhat now?"
	if got != want {
		t.Fatalf("menuQuestionText() = %q, want %q", got, want)
	}
	if strings.Contains(got, "▸") {
		t.Fatalf("menu question must not contain the choice selector: %q", got)
	}
}
