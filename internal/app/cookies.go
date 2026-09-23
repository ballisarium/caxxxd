package app

import "github.com/ballisarium/caxxxd/internal/ytdlp"

func (a *App) configureCookies() error {
	a.console.Panel("Browser cookies",
		"Use the account already signed in to your local browser.",
		"yt-dlp reads cookies for each lookup and download; caxxxd stores only the browser name.",
		"macOS may ask for Keychain or browser-data access. Cookies are off by default.")
	browsers := ytdlp.CookieBrowsers()
	choices := []Choice{{Label: "Off", Detail: "download without browser cookies"}}
	initial := 0
	for i, browser := range browsers {
		choices = append(choices, Choice{Label: browser, Detail: "use its local signed-in session"})
		if browser == a.preferences.CookieBrowser {
			initial = i + 1
		}
	}
	choices = append(choices, Choice{Label: "Cancel", Detail: "keep the current setting"})
	picked, err := a.prompt.Choose("Use cookies from", choices, initial)
	if err != nil {
		return err
	}
	if picked == len(choices)-1 {
		return nil
	}
	if picked < 0 || picked > len(browsers) {
		return errNoSuchChoice
	}
	preferences := a.preferences
	preferences.CookieBrowser = ""
	if picked > 0 {
		preferences.CookieBrowser = browsers[picked-1]
	}
	if err := a.options.ConfigStore.Save(preferences); err != nil {
		return err
	}
	a.preferences = preferences
	a.options.Client.CookieBrowser = preferences.CookieBrowser
	return nil
}
