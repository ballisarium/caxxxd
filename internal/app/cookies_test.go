package app_test

import (
	"context"
	"testing"

	"github.com/ballisarium/caxxxd/internal/app"
)

type cookieRunner struct {
	stubRunner
	args []string
}

func (r *cookieRunner) Output(ctx context.Context, binary string, args ...string) ([]byte, error) {
	r.args = args
	return r.stubRunner.Output(ctx, binary, args...)
}

func TestRememberedCookiesReachTheWholeWorkflow(t *testing.T) {
	configured := newSession(t, script(pick("firefox")), func(o *app.Options) {
		o.ConfigureCookies = true
	}).run()
	runner := &cookieRunner{stubRunner: stubRunner{payload: fixture(t, "video.json")}}
	session := newSession(t, script(videoPath(pick("Quit"))...), func(o *app.Options) {
		o.ConfigStore = configured.store
		o.Client.Runner = runner
	}).run()
	session.requireScripted()
	if session.err != nil || argumentAfter(runner.args, "--cookies-from-browser") != "firefox" {
		t.Fatal("saved browser did not reach metadata lookup")
	}
	session.requireArgs("--cookies-from-browser", "firefox")
}

func TestCookiePreferenceCanBeEnabledAndDisabled(t *testing.T) {
	first := newSession(t, script(pick("firefox")), func(o *app.Options) {
		o.ConfigureCookies = true
	}).run()
	if first.err != nil {
		t.Fatal(first.err)
	}
	stored, err := first.store.Load()
	if err != nil || stored.CookieBrowser != "firefox" {
		t.Fatal("browser choice was not saved")
	}
	second := newSession(t, script(pick("Off")), func(o *app.Options) {
		o.ConfigureCookies = true
		o.ConfigStore = first.store
	}).run()
	if second.err != nil {
		t.Fatal(second.err)
	}
	stored, err = first.store.Load()
	if err != nil || stored.CookieBrowser != "" {
		t.Fatal("browser cookies were not disabled")
	}
}
