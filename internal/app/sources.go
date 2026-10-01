package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/browser"
)

func (a *App) askSource() (stage, error) {
	picked, err := a.prompt.Choose("Download from", []Choice{
		{Label: "Media URL", Detail: "a supported site or a direct media link"},
		{Label: "Web page · beta", Detail: "find media playing in a browser"},
		{Label: "Supported sources", Detail: "where caxxxd can download from"},
		{Label: fmt.Sprintf("Queue · %d", len(a.queue)), Detail: "download your reviewed selections in order"},
		{Label: "Download history", Detail: "open or reveal completed files"},
		quitChoice,
	}, 0)
	if errors.Is(err, ErrBack) {
		return stageSource, nil
	}
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
	case 3:
		return stageQueue, nil
	case 4:
		return stageHistory, nil
	default:
		return stageSource, errQuit
	}
}

func (a *App) showSources() (stage, error) {
	a.console.Panel("Supported sources",
		"Media URL: sites supported by your installed yt-dlp, including",
		"YouTube, Vimeo, Twitch, TikTok, SoundCloud, and many others.",
		"Direct links: video, audio, HLS (.m3u8), and DASH (.mpd).",
		"Web page · beta: play media in a capture tab, then select a stream.",
		"Availability depends on the site, your access, and the media format.",
		"Full extractor list: yt-dlp --list-extractors")
	_, err := a.prompt.Choose("Return to downloads", []Choice{backChoice}, 0)
	return stageSource, err
}

func (a *App) askBrowserPage(ctx context.Context) (stage, error) {
	page := ""
	for {
		var err error
		page, err = a.prompt.Text("Web page URL", "Play media in the browser. Blank goes back.", page)
		if errors.Is(err, ErrBack) {
			return stageSource, nil
		}
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
		open, err := a.askCaptureBrowser()
		if errors.Is(err, ErrBack) {
			a.screen(stageBrowserPage)
			continue
		}
		if err != nil {
			return stageBrowserPage, err
		}
		spinner := a.console.Spinner("Opening the capture browser")
		defer spinner.Stop()
		if interrupted(ctx, func(runCtx context.Context) {
			a.capture, err = open(runCtx, page)
		}) {
			return stageSource, errQuit
		}
		if err != nil {
			spinner.Fail("Browser capture could not start")
			a.carry("Browser capture unavailable", err.Error())
			return stageSource, nil
		}
		spinner.Done("Browser capture is ready")
		a.capture = newManagedCapture(a.capture)
		a.captureCursor = ""
		a.url = ""
		if !a.sectionFixed {
			a.options.Section = nil
		}
		return stageCapture, nil
	}
}

// askCaptureBrowser keeps a canceled port edit inside the browser selection.
func (a *App) askCaptureBrowser() (func(context.Context, string) (browser.Capture, error), error) {
	for {
		mode, err := a.prompt.Choose("Capture browser", []Choice{
			{Label: "Separate browser", Detail: "Chrome, Chromium, or Edge · sign in there if needed"},
			{Label: "My running Chrome", Detail: "existing profile · requires remote debugging"},
			{Label: "Chrome debugging port", Detail: "connect to a browser you started with a debug port"},
			backChoice,
		}, 0)
		if err != nil {
			return nil, err
		}
		if mode == 3 {
			return nil, ErrBack
		}
		open := a.options.OpenBrowser
		if mode == 1 || mode == 2 {
			port := 0
			if mode == 2 {
				value, askErr := a.prompt.Text("Chrome debug port", "Enter the loopback port of your running capture browser. Leave blank to go back.", "")
				if errors.Is(askErr, ErrBack) || (askErr == nil && strings.TrimSpace(value) == "") {
					a.screen(stageBrowserPage)
					continue
				}
				if askErr != nil {
					return nil, askErr
				}
				port, err = strconv.Atoi(strings.TrimSpace(value))
				if err != nil || port < 1 || port > 65535 {
					a.carry("Invalid browser port", "Use a port between 1 and 65535.")
					a.screen(stageBrowserPage)
					continue
				}
			}
			if mode == 1 {
				a.console.Hint("In Chrome, enable chrome://inspect/#remote-debugging and allow the connection.")
			}
			a.console.Hint("Capture opens a new tab. Your other tabs stay outside this scan.")
			open = func(ctx context.Context, page string) (browser.Capture, error) {
				return a.options.ConnectBrowser(ctx, page, port)
			}
		}
		return open, nil
	}
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
	a.console.Hint("Play media to discover streams.")
	a.console.Hint("Keep browser open. ~ = estimated size.")
	items := a.capture.Candidates()
	if len(items) == 200 {
		a.console.Hint("Showing the first 200 resources. Reopen capture to scan a different page.")
	}
	if len(items) == 0 {
		a.console.Hint("No media captured yet. Start playback or open the embedded player.")
	}
	picked, err := a.chooseCapture(ctx)
	if err != nil {
		if a.capture.Err() != nil {
			cleanupErr := a.closeCapture()
			a.carry("Browser capture ended", "Reopen capture to select media again.")
			if cleanupErr != nil {
				a.carry("Browser cleanup failed", cleanupErr.Error())
			}
			return stageSource, nil
		}
		return stageCapture, err
	}
	if picked == -1 {
		return stageCapture, nil
	}
	if picked == -2 {
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
	a.captureID = picked
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
