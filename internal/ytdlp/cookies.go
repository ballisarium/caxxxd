package ytdlp

import "errors"

// CookieBrowsers is the set of browser integrations supported on macOS.
func CookieBrowsers() []string {
	return []string{"safari", "chrome", "firefox", "brave", "edge", "chromium", "opera", "vivaldi"}
}

// CookieArgs keeps authentication explicit and independent of global yt-dlp
// configuration. Never export a cookie jar or accept arbitrary profile paths.
func CookieArgs(browser string) ([]string, error) {
	args := []string{"--ignore-config"}
	if browser == "" {
		return args, nil
	}
	for _, supported := range CookieBrowsers() {
		if browser == supported {
			return append(args, "--cookies-from-browser", browser), nil
		}
	}
	return nil, errors.New("unsupported cookie browser; run caxxxd --cookies to choose a browser")
}
