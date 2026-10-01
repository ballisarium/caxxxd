package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Connect opens a capture tab in Chrome after the user enables remote debugging.
// It owns only its private download files and its debugging connection.
func (l Launcher) Connect(ctx context.Context, pageURL string) (capture Capture, err error) {
	if !ValidURL(pageURL) {
		return nil, errors.New("enter an http:// or https:// page URL without embedded credentials")
	}
	endpoint, err := l.chromeEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	// Chrome may ask the user to approve this connection in its own window.
	socket, err := dialWebSocket(ctx, endpoint)
	if err != nil {
		return nil, errors.New("could not connect to Chrome; enable remote debugging and allow the connection in Chrome")
	}
	directory, err := os.MkdirTemp(l.TempDir, "caxxxd-browser-*")
	if err != nil {
		_ = socket.Close()
		return nil, errors.New("could not create private download settings")
	}
	s := &Session{directory: directory, borrowed: true, ready: make(chan attached, 32), requests: make(map[string]request)}
	s.mu.Lock()
	s.conn = newConnection(socket, socket, s.event)
	s.mu.Unlock()
	if err := s.openPage(ctx, pageURL); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	return s, nil
}

func (l Launcher) chromeEndpoint(ctx context.Context) (string, error) {
	if l.DebugPort != 0 {
		if l.DebugPort < 1 || l.DebugPort > 65535 {
			return "", errors.New("Chrome debug port must be between 1 and 65535")
		}
		client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", l.DebugPort), nil)
		if err != nil {
			return "", errors.New("could not prepare Chrome connection")
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("Chrome debug port is unavailable; check the port and keep the browser open")
		}
		defer resp.Body.Close()
		var version struct {
			Endpoint string `json:"webSocketDebuggerUrl"`
		}
		if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&version) != nil || version.Endpoint == "" {
			return "", errors.New("Chrome did not provide a debugging endpoint")
		}
		return version.Endpoint, nil // The socket dialer independently validates loopback.
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("could not locate Chrome's remote debugging settings")
	}
	file, err := os.Open(filepath.Join(userHome, "Library", "Application Support", "Google", "Chrome", "DevToolsActivePort"))
	if err != nil {
		return "", errors.New("Chrome's debugging endpoint is unavailable. Enable chrome://inspect/#remote-debugging, or choose Chrome debugging port for a browser started with a debug port")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 2049))
	if err != nil || len(data) > 2048 {
		return "", errors.New("could not read Chrome's debugging endpoint")
	}
	return activePortEndpoint(string(data))
}

func activePortEndpoint(data string) (string, error) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) != 2 {
		return "", errors.New("Chrome's debugging endpoint is invalid; enable remote debugging again")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	path := strings.TrimSpace(lines[1])
	if err != nil || port < 1 || port > 65535 || !strings.HasPrefix(path, "/devtools/browser/") || strings.ContainsAny(path, "\r\n ?#") {
		return "", errors.New("Chrome's debugging endpoint is invalid; enable remote debugging again")
	}
	return fmt.Sprintf("ws://127.0.0.1:%d%s", port, path), nil
}
