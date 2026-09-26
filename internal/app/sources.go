package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ballisarium/caxxxd/internal/browser"
)

func (a *App) askSource() (stage, error) {
	picked, err := a.prompt.Choose("Download from", []Choice{
		{Label: "Media URL", Detail: "a supported site or a direct media link"},
		{Label: "Web page · beta", Detail: "find media in a separate browser"},
		{Label: "Supported sources", Detail: "where caxxxd can download from"},
		quitChoice,
	}, 0)
	if err != nil {
		return stageSource, err
	}
	switch picked {
	case 0:
		return stageLink, nil
	case 1:
		return stageBrowserPage, nil
	case 2:
		return stageSources, nil
	default:
		return stageSource, errQuit
	}
}

func (a *App) showSources() (stage, error) {
	a.console.Panel("Supported sources",
		"Media URL: sites supported by your installed yt-dlp, including",
		"YouTube, Vimeo, Twitch, TikTok, SoundCloud, and many others.",
		"Direct links: video, audio, HLS (.m3u8), and DASH (.mpd).",
		"Web page · beta: play media in a separate browser, then select a stream.",
		"Availability depends on the site, your access, and the media format.",
		"Full extractor list: yt-dlp --list-extractors")
	_, err := a.prompt.Choose("Return to downloads", []Choice{backChoice}, 0)
	return stageSource, err
}

func (a *App) askBrowserPage(ctx context.Context) (stage, error) {
	a.console.Hint("A separate Chrome, Chromium, or Edge profile opens. Sign in there if needed.")
	page, err := a.prompt.Text("Web page URL", "Play the media in the browser. Leave blank to go back.", "")
	if err != nil {
		return stageBrowserPage, err
	}
	page = strings.TrimSpace(page)
	if page == "" {
		return stageSource, nil
	}
	if !browser.ValidURL(page) {
		a.carry("Invalid page URL", "Use an http:// or https:// URL without embedded credentials.")
		return stageBrowserPage, nil
	}
	spinner := a.console.Spinner("Opening the capture browser")
	defer spinner.Stop()
	if interrupted(ctx, func(runCtx context.Context) {
		a.capture, err = a.options.OpenBrowser(runCtx, page)
	}) {
		return stageSource, errQuit
	}
	if err != nil {
		spinner.Fail("Browser capture could not start")
		a.carry("Browser capture unavailable", err.Error())
		return stageSource, nil
	}
	spinner.Done("Browser capture is ready")
	a.url = ""
	if !a.sectionFixed {
		a.options.Section = nil
	}
	return stageCapture, nil
}

func (a *App) askCapture(ctx context.Context) (stage, error) {
	if err := a.capture.Err(); err != nil {
		cleanupErr := a.closeCapture()
		a.carry("Browser capture ended", err.Error())
		if cleanupErr != nil {
			a.carry("Browser cleanup failed", cleanupErr.Error())
		}
		return stageSource, nil
	}
	a.console.Hint("Play media in the browser, then refresh this list.")
	a.console.Hint("Keep the browser open until the download finishes.")
	items := a.capture.Candidates()
	if len(items) == 200 {
		a.console.Hint("Showing the first 200 resources. Reopen capture to scan a different page.")
	}
	choices := make([]Choice, 0, len(items)+2)
	for i, item := range items {
		choices = append(choices, Choice{Label: fmt.Sprintf("%d · %s", i+1, item.Kind), Detail: item.Host})
	}
	if len(items) == 0 {
		a.console.Hint("No media captured yet. Start playback or open the embedded player.")
	}
	choices = append(choices, Choice{Label: "Refresh streams", Detail: "include new browser requests"}, backChoice)
	picked, err := a.prompt.Choose("Captured media", choices, 0)
	if err != nil {
		return stageCapture, err
	}
	if picked == len(items) {
		return stageCapture, nil
	}
	if picked == len(items)+1 {
		if err := a.closeCapture(); err != nil {
			a.carry("Browser cleanup failed", err.Error())
		}
		return stageSource, nil
	}
	var selected browser.Selection
	if interrupted(ctx, func(runCtx context.Context) {
		selected, err = a.capture.Prepare(runCtx, picked)
	}) {
		return stageCapture, errQuit
	}
	if err != nil {
		a.carry("Could not prepare this stream", err.Error())
		return stageCapture, nil
	}
	a.url, a.captureConfig = selected.URL, selected.ConfigFile
	if !a.sectionFixed {
		a.options.Section = nil
	}
	return a.fetchMetadata(ctx)
}

func (a *App) closeCapture() error {
	if a.capture == nil {
		return nil
	}
	err := a.capture.Close()
	a.capture = nil
	a.captureConfig = ""
	a.url = ""
	return err
}
