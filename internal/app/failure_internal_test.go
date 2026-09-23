package app

import (
	"context"
	"errors"
	"testing"
)

func TestMetadataCancellationIsNotAToolFailure(t *testing.T) {
	if failure := classifyMetadataError(context.Canceled); failure.Category != FailureCancelled {
		t.Fatalf("cancellation category = %s", failure.Category)
	}
}

func TestCookieSuccessDoesNotHideDownloadFailure(t *testing.T) {
	failure := classifyDownloadError(errors.New("exit status 1"), []string{
		"Extracted 42 cookies from chrome", "ERROR: HTTP Error 403: Forbidden",
	})
	if failure.Category != FailureNetwork {
		t.Fatalf("category = %s, want access error", failure.Category)
	}
}
