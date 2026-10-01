package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestChromeEndpointUsesExplicitPortAndRejectsInvalidDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Error("unexpected discovery request")
		}
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/fixture"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	endpoint, err := (Launcher{DebugPort: port}).chromeEndpoint(context.Background())
	if err != nil || endpoint != "ws://127.0.0.1:9222/devtools/browser/fixture" {
		t.Fatal("explicit browser endpoint was not resolved")
	}
	for _, data := range []string{"0\n/devtools/browser/id", "9222\nhttp://remote.test/", "65536\n/devtools/browser/id", "9222\n/devtools/browser/id?secret=value"} {
		if _, err := activePortEndpoint(data); err == nil {
			t.Fatal("invalid discovery endpoint accepted")
		}
	}
	if endpoint, err := activePortEndpoint("12345\n/devtools/browser/fixture\n"); err != nil || endpoint != "ws://127.0.0.1:12345/devtools/browser/fixture" {
		t.Fatal("Chrome's active-port file was not recognized")
	}
}
