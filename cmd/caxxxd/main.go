// Command caxxxd is a friendly terminal interface for yt-dlp.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ballisarium/caxxxd/internal/app"
	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/deps"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

var version = "dev"

const usage = `caxxxd — a friendly yt-dlp terminal interface

Usage:
  caxxxd [--cookies] [--section START-END] [URL]
  caxxxd --version

Options:
  --cookies            choose or disable remembered local browser cookies
  --section START-END  download only a time range (SS, MM:SS, or HH:MM:SS)

Required tools:
  yt-dlp
  ffmpeg
`

// errTooManyURLs is returned when more than one item is requested at once.
var errTooManyURLs = errors.New("caxxxd downloads one item at a time")

func main() {
	initialURL, section, cookies, done, err := parseArgs(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	if done {
		return
	}

	err = run(initialURL, section, cookies)
	switch {
	case errors.Is(err, app.ErrDependenciesMissing):
		// The missing tools have already been named on screen.
		os.Exit(1)
	case err != nil:
		fmt.Fprintf(os.Stderr, "caxxxd: %v\n", err)
		os.Exit(1)
	}
}

// parseArgs reads the supported flags. done reports that the work is finished
// without starting the interface, as with --help and --version.
func parseArgs(args []string, stdout, stderr io.Writer) (initialURL string, section *domain.TimeRange, cookies, done bool, err error) {
	flags := flag.NewFlagSet("caxxxd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// Usage is printed explicitly below so --help can go to stdout while a
	// mistyped flag reports on stderr.
	flags.Usage = func() {}

	showVersion := flags.Bool("version", false, "print the version and exit")
	sectionValue := flags.String("section", "", "download only a time range")
	flags.BoolVar(&cookies, "cookies", false, "configure local browser cookies")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return "", nil, false, true, nil
		}
		fmt.Fprint(stderr, "\n"+usage)
		return "", nil, false, true, err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "caxxxd %s\n", version)
		return "", nil, false, true, nil
	}

	if *sectionValue != "" {
		parsed, parseErr := domain.ParseTimeRange(*sectionValue)
		if parseErr != nil {
			err = fmt.Errorf("invalid --section %q: %w", *sectionValue, parseErr)
			fmt.Fprintf(stderr, "caxxxd: %v\n\n%s", err, usage)
			return "", nil, false, true, err
		}
		section = &parsed
	}

	switch flags.NArg() {
	case 0:
		return "", section, cookies, false, nil
	case 1:
		return flags.Arg(0), section, cookies, false, nil
	default:
		fmt.Fprintf(stderr, "caxxxd: %v\n\n%s", errTooManyURLs, usage)
		return "", nil, false, true, errTooManyURLs
	}
}

// run builds the real services and starts the interface.
func run(initialURL string, section *domain.TimeRange, cookies bool) error {
	ui.MatchOutput(os.Stdout)
	console := ui.NewConsole(os.Stdout)
	defer console.Restore()

	session := app.New(app.Options{
		ConfigureCookies: cookies,
		InitialURL:       initialURL,
		Section:          section,
		Version:          version,
		Checker:          deps.NewChecker(),
		Client:           ytdlp.NewClient("yt-dlp"),
		Downloader:       ytdlp.Downloader{Binary: "yt-dlp"},
		ConfigStore:      config.NewStore(configPath()),
		Console:          console,
	})

	return session.Run(context.Background())
}

// configPath is where preferences live: ~/Library/Application Support on macOS.
func configPath() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		directory = os.TempDir()
	}
	return filepath.Join(directory, "caxxxd", "config.json")
}
