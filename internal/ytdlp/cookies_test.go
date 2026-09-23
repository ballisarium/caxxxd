package ytdlp

import "testing"

func TestCookiesAreOptInAndRejectProfileArguments(t *testing.T) {
	args, err := CookieArgs("")
	if err != nil || len(args) != 1 || args[0] != "--ignore-config" {
		t.Fatal("anonymous mode must ignore external authentication settings")
	}
	if _, err := CookieArgs("chrome:/private/profile"); err == nil {
		t.Fatal("unsupported browser specifications must be rejected")
	}
}
