package ytdlp_test

import (
	"testing"

	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

func TestParseOutputLineReadsKnownTotal(t *testing.T) {
	event, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\t1048576\t10485760\tNA\t524288\t18")
	if !ok {
		t.Fatal("progress line was not recognized")
	}
	if event.Kind != ytdlp.EventProgress {
		t.Fatalf("Kind = %v, want EventProgress", event.Kind)
	}

	want := ytdlp.Progress{
		Status:          "downloading",
		DownloadedBytes: 1048576,
		TotalBytes:      10485760,
		SpeedBytes:      524288,
		ETASeconds:      18,
		ETAKnown:        true,
	}
	if event.Progress != want {
		t.Fatalf("Progress = %#v, want %#v", event.Progress, want)
	}
}

func TestParseOutputLineReadsEstimatedTotal(t *testing.T) {
	event, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\t1048576\tNA\t12582912\tNA\tNA")
	if !ok {
		t.Fatal("progress line was not recognized")
	}

	want := ytdlp.Progress{
		Status:              "downloading",
		DownloadedBytes:     1048576,
		EstimatedTotalBytes: 12582912,
	}
	if event.Progress != want {
		t.Fatalf("Progress = %#v, want %#v", event.Progress, want)
	}
}

func TestParseOutputLineSeparatesUnknownETAFromZero(t *testing.T) {
	almostDone, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\t99\t100\tNA\t524288\t0")
	if !ok {
		t.Fatal("progress line was not recognized")
	}
	if !almostDone.Progress.ETAKnown || almostDone.Progress.ETASeconds != 0 {
		t.Fatalf("a reported eta of 0 must stay known: %#v", almostDone.Progress)
	}

	noEstimate, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\t99\t100\tNA\t524288\tNA")
	if !ok {
		t.Fatal("progress line was not recognized")
	}
	if noEstimate.Progress.ETAKnown {
		t.Fatalf("NA must not be reported as a known eta: %#v", noEstimate.Progress)
	}
}

func TestParseOutputLineReadsFractionalSpeed(t *testing.T) {
	event, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\t10\t100\tNA\t1234.5\t7")
	if !ok {
		t.Fatal("progress line was not recognized")
	}
	if event.Progress.SpeedBytes != 1234.5 {
		t.Fatalf("SpeedBytes = %v, want 1234.5", event.Progress.SpeedBytes)
	}
}

func TestParseOutputLineReadsPostProcessAndFile(t *testing.T) {
	event, ok := ytdlp.ParseOutputLine("__CAXXXD_POSTPROCESS__started")
	if !ok || event.Kind != ytdlp.EventPostProcess || event.Stage != "started" {
		t.Fatalf("postprocess event = %#v ok=%t", event, ok)
	}

	event, ok = ytdlp.ParseOutputLine("__CAXXXD_FILE__/Users/test/Downloads/Example [abc123].mkv")
	if !ok || event.Kind != ytdlp.EventCompletedFile {
		t.Fatalf("file event = %#v ok=%t", event, ok)
	}
	if event.FilePath != "/Users/test/Downloads/Example [abc123].mkv" {
		t.Fatalf("FilePath = %q", event.FilePath)
	}
}

func TestParseOutputLineIgnoresUnrelatedOutput(t *testing.T) {
	lines := []string{
		"",
		"[youtube] abc123: Downloading webpage",
		"[download]  42.0% of 10.00MiB at 1.20MiB/s ETA 00:05",
		"ERROR: unable to download video data",
		"__CAXXXD_PROGRESS__downloading\t1\t2",
		"__CAXXXD_PROGRESS__downloading\t1\t2\t3\t4\t5\t6",
	}

	for _, line := range lines {
		if event, ok := ytdlp.ParseOutputLine(line); ok {
			t.Fatalf("ParseOutputLine(%q) = %#v, want not recognized", line, event)
		}
	}
}

func TestParseOutputLineToleratesNonNumericFields(t *testing.T) {
	event, ok := ytdlp.ParseOutputLine("__CAXXXD_PROGRESS__downloading\tabc\t\tNA\t-\tnope")
	if !ok {
		t.Fatal("progress line was not recognized")
	}
	if event.Progress.DownloadedBytes != 0 || event.Progress.SpeedBytes != 0 || event.Progress.ETASeconds != 0 {
		t.Fatalf("malformed numbers must resolve to zero, got %#v", event.Progress)
	}
}

func TestProgressTotalPrefersExactValue(t *testing.T) {
	exact := ytdlp.Progress{TotalBytes: 100, EstimatedTotalBytes: 90}
	if total, known := exact.Total(); total != 100 || !known {
		t.Fatalf("Total() = (%d, %t), want (100, true)", total, known)
	}

	estimated := ytdlp.Progress{EstimatedTotalBytes: 90}
	if total, known := estimated.Total(); total != 90 || !known {
		t.Fatalf("Total() = (%d, %t), want (90, true)", total, known)
	}

	unknown := ytdlp.Progress{DownloadedBytes: 5}
	if total, known := unknown.Total(); total != 0 || known {
		t.Fatalf("Total() = (%d, %t), want (0, false)", total, known)
	}
}

func TestProgressFractionClamps(t *testing.T) {
	tests := []struct {
		progress ytdlp.Progress
		want     float64
	}{
		{ytdlp.Progress{DownloadedBytes: 50, TotalBytes: 100}, 0.5},
		{ytdlp.Progress{DownloadedBytes: 150, TotalBytes: 100}, 1},
		{ytdlp.Progress{DownloadedBytes: -10, TotalBytes: 100}, 0},
		{ytdlp.Progress{DownloadedBytes: 10}, 0},
	}

	for _, test := range tests {
		if got := test.progress.Fraction(); got != test.want {
			t.Fatalf("Fraction(%#v) = %v, want %v", test.progress, got, test.want)
		}
	}
}
