package domain_test

import (
	"strings"
	"testing"

	"github.com/ballisarium/caxxxd/internal/domain"
)

func TestParseTimeRangeAcceptsSupportedTimeFormats(t *testing.T) {
	tests := []struct {
		input string
		start int64
		end   int64
		label string
	}{
		{input: "90-120", start: 90, end: 120, label: "seconds"},
		{input: "05:30-06:20", start: 330, end: 380, label: "minutes and seconds"},
		{input: "1:02:03-1:03:00", start: 3723, end: 3780, label: "hours minutes and seconds"},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			got, err := domain.ParseTimeRange(test.input)
			if err != nil {
				t.Fatalf("ParseTimeRange(%q) returned error: %v", test.input, err)
			}
			if got.Start != test.start || got.End != test.end {
				t.Fatalf("ParseTimeRange(%q) = %#v, want start=%d end=%d", test.input, got, test.start, test.end)
			}
		})
	}
}

func TestParseTimeRangeRejectsMalformedOrEmptyRanges(t *testing.T) {
	for _, input := range []string{
		"",
		"05:30",
		"05:30-",
		"-06:20",
		"06:20-05:30",
		"01:30-01:30",
		"00:60-01:00",
		"1:2:3:4-2:00:00",
		"-1-10",
	} {
		t.Run(strings.ReplaceAll(input, ":", "_"), func(t *testing.T) {
			if _, err := domain.ParseTimeRange(input); err == nil {
				t.Fatalf("ParseTimeRange(%q) succeeded, want an error", input)
			}
		})
	}
}

func TestTimeRangeValidatesVideoDuration(t *testing.T) {
	rangeWithinVideo := domain.TimeRange{Start: 30, End: 125}
	if err := rangeWithinVideo.ValidateDuration(125.4); err != nil {
		t.Fatalf("range ending at 125s rejected for a 125.4s video: %v", err)
	}

	rangePastVideo := domain.TimeRange{Start: 30, End: 126}
	err := rangePastVideo.ValidateDuration(125.4)
	if err == nil || !strings.Contains(err.Error(), "exceeds video duration") {
		t.Fatalf("range past the video returned %v, want an explanatory error", err)
	}

	if err := rangeWithinVideo.ValidateDuration(0); err == nil || !strings.Contains(err.Error(), "duration is unavailable") {
		t.Fatalf("unknown duration returned %v, want an explanatory error", err)
	}
}
