package app

import (
	"errors"
	"testing"
)

func TestCookieSuccessDoesNotHideDownloadFailure(t *testing.T) {
	failure := classifyDownloadError(errors.New("exit status 1"), []string{
		"Extracted 42 cookies from chrome", "ERROR: HTTP Error 403: Forbidden",
	})
	if failure.Category != FailureNetwork {
		t.Fatalf("category = %s, want access error", failure.Category)
	}
}
