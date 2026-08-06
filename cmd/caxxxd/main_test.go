package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgsWithoutArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer

	initialURL, section, done, err := parseArgs(nil, &stdout, &stderr)
	if err != nil || done {
		t.Fatalf("parseArgs = (%q, %#v, %t, %v), want a normal start", initialURL, section, done, err)
	}
	if initialURL != "" {
		t.Fatalf("initialURL = %q, want empty", initialURL)
	}
	if section != nil {
		t.Fatalf("section = %#v, want nil", section)
	}
}

func TestParseArgsAcceptsOneURL(t *testing.T) {
	var stdout, stderr bytes.Buffer

	initialURL, section, done, err := parseArgs([]string{"https://example.test/video"}, &stdout, &stderr)
	if err != nil || done {
		t.Fatalf("parseArgs = (%q, %#v, %t, %v)", initialURL, section, done, err)
	}
	if initialURL != "https://example.test/video" {
		t.Fatalf("initialURL = %q", initialURL)
	}
	if section != nil {
		t.Fatalf("section = %#v, want nil", section)
	}
}

func TestParseArgsAcceptsSectionAndURL(t *testing.T) {
	var stdout, stderr bytes.Buffer

	initialURL, section, done, err := parseArgs([]string{
		"--section", "05:30-06:20", "https://example.test/video",
	}, &stdout, &stderr)
	if err != nil || done {
		t.Fatalf("parseArgs = (%q, %#v, %t, %v)", initialURL, section, done, err)
	}
	if initialURL != "https://example.test/video" {
		t.Fatalf("initialURL = %q", initialURL)
	}
	if section == nil || section.Start != 330 || section.End != 380 {
		t.Fatalf("section = %#v, want 330-380 seconds", section)
	}
}

func TestParseArgsRejectsInvalidSectionBeforeStarting(t *testing.T) {
	var stdout, stderr bytes.Buffer

	_, _, done, err := parseArgs([]string{"--section", "06:20-05:30"}, &stdout, &stderr)
	if err == nil || !done {
		t.Fatalf("invalid section must stop startup: done=%t err=%v", done, err)
	}
	if !strings.Contains(stderr.String(), "invalid --section") || !strings.Contains(stderr.String(), "start") {
		t.Fatalf("stderr = %q, want a clear section error", stderr.String())
	}
}

func TestParseArgsRejectsMultipleURLs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	_, _, done, err := parseArgs([]string{"https://a.test/1", "https://b.test/2"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("two URLs must be rejected")
	}
	if !done {
		t.Fatal("a rejected invocation must not start the interface")
	}
	if !strings.Contains(stderr.String(), "one item at a time") {
		t.Fatalf("stderr = %q, want an explanation", stderr.String())
	}
}

func TestParseArgsPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	_, _, done, err := parseArgs([]string{"--version"}, &stdout, &stderr)
	if err != nil || !done {
		t.Fatalf("parseArgs = (%t, %v)", done, err)
	}
	if !strings.Contains(stdout.String(), "caxxxd ") {
		t.Fatalf("stdout = %q, want the version", stdout.String())
	}
}

func TestParseArgsPrintsHelpOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer

	_, _, done, err := parseArgs([]string{"--help"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("--help returned %v, want a clean exit", err)
	}
	if !done {
		t.Fatal("--help must not start the interface")
	}

	for _, want := range []string{
		"caxxxd — a friendly yt-dlp terminal interface",
		"caxxxd [--section START-END] [URL]",
		"yt-dlp",
		"ffmpeg",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestParseArgsReportsUnknownFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if _, _, done, err := parseArgs([]string{"--nope"}, &stdout, &stderr); err == nil || !done {
		t.Fatalf("unknown flags must fail: done=%t err=%v", done, err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestConfigPathIsUnderTheUserConfigDirectory(t *testing.T) {
	path := configPath()

	if !strings.HasSuffix(path, "caxxxd/config.json") {
		t.Fatalf("configPath() = %q", path)
	}
}
