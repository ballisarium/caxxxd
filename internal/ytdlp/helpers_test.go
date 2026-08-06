package ytdlp_test

import "testing"

// assertContainsSequence fails unless args contains the given values adjacently
// and in order, which is how yt-dlp flags and their values must be passed.
func assertContainsSequence(t *testing.T, args []string, sequence ...string) {
	t.Helper()
	for start := 0; start+len(sequence) <= len(args); start++ {
		matched := true
		for offset := range sequence {
			if args[start+offset] != sequence[offset] {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("args %q do not contain sequence %q", args, sequence)
}

func assertLacks(t *testing.T, args []string, unwanted string) {
	t.Helper()
	for _, arg := range args {
		if arg == unwanted {
			t.Fatalf("args %q must not contain %q", args, unwanted)
		}
	}
}
