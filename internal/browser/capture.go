package browser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type request struct {
	referer, origin, agent string
}

type resource struct {
	Candidate
	url string
	request
}

func autoAttachParams() map[string]any {
	return map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}
}

func (s *Session) event(msg packet) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	switch msg.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				TargetID string `json:"targetId"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		go func() {
			ctx := context.Background()
			err := conn.call(ctx, p.SessionID, "Network.enable", map[string]any{}, nil)
			// Auto-attachment is recursive: cross-origin iframes and workers
			// must be enabled before allowing their scripts to run.
			if err == nil {
				err = conn.call(ctx, p.SessionID, "Target.setAutoAttach", autoAttachParams(), nil)
			}
			resumeErr := conn.call(ctx, p.SessionID, "Runtime.runIfWaitingForDebugger", map[string]any{}, nil)
			if err == nil {
				err = resumeErr
			}
			select {
			case s.ready <- attached{p.TargetInfo.TargetID, p.SessionID, err}:
			case <-conn.done:
			}
		}()
	case "Network.requestWillBeSent":
		var p struct {
			RequestID string `json:"requestId"`
			Request   struct {
				Headers map[string]string `json:"headers"`
			} `json:"request"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		r := request{}
		for key, value := range p.Request.Headers {
			switch strings.ToLower(key) {
			case "referer":
				r.referer = value
			case "origin":
				r.origin = value
			case "user-agent":
				r.agent = value
			}
		}
		s.mu.Lock()
		if len(s.requests) < 8192 {
			s.requests[msg.SessionID+":"+p.RequestID] = r
		}
		s.mu.Unlock()
	case "Network.responseReceived":
		var p struct {
			RequestID string `json:"requestId"`
			Response  struct {
				URL      string `json:"url"`
				MimeType string `json:"mimeType"`
				Status   int    `json:"status"`
			} `json:"response"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Response.Status < 200 || p.Response.Status >= 300 {
			return
		}
		s.mu.Lock()
		r := s.requests[msg.SessionID+":"+p.RequestID]
		s.mu.Unlock()
		s.observe(p.Response.URL, p.Response.MimeType, r.referer, r.origin, r.agent)
	case "Network.loadingFinished", "Network.loadingFailed":
		var p struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(msg.Params, &p) == nil {
			s.mu.Lock()
			delete(s.requests, msg.SessionID+":"+p.RequestID)
			s.mu.Unlock()
		}
	}
}

func (s *Session) observe(rawURL, mime, referer, origin, agent string) {
	if !ValidURL(rawURL) {
		return
	}
	u, _ := url.Parse(rawURL)
	ext := strings.ToLower(path.Ext(u.Path))
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	kind := ""
	switch {
	case ext == ".m3u8", mime == "application/vnd.apple.mpegurl", mime == "application/x-mpegurl", mime == "audio/mpegurl", mime == "audio/x-mpegurl":
		kind = "HLS"
	case ext == ".mpd", mime == "application/dash+xml":
		kind = "DASH"
	case ext == ".ts", ext == ".m4s", ext == ".cmfv", ext == ".cmfa", mime == "video/mp2t", strings.Contains(mime, "iso.segment"):
		return // Fragments alone are not complete, playable downloads.
	case ext == ".mp4", ext == ".webm", ext == ".mkv", ext == ".mov", ext == ".m4v", ext == ".mp3", ext == ".m4a", ext == ".ogg", ext == ".opus", ext == ".wav", ext == ".flac", ext == ".aac":
		kind = strings.ToUpper(strings.TrimPrefix(ext, "."))
	case strings.HasPrefix(mime, "video/"):
		kind = "VIDEO"
	case strings.HasPrefix(mime, "audio/"):
		kind = "AUDIO"
	}
	if kind == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].url == rawURL {
			s.items[i].request = request{referer, origin, agent}
			return
		}
	}
	if len(s.items) < 200 {
		s.items = append(s.items, resource{Candidate: Candidate{Kind: kind, Host: u.Hostname()}, url: rawURL, request: request{referer, origin, agent}})
	}
}

func (s *Session) Candidates() []Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Candidate, len(s.items))
	for i, item := range s.items {
		items[i] = item.Candidate
	}
	return items
}

// Prepare transfers domain-scoped cookies and the request's actual Referer,
// Origin, and User-Agent through private, temporary yt-dlp configuration.
// Authorization headers and cookies are never flattened into global headers.
func (s *Session) Prepare(ctx context.Context, index int) (Selection, error) {
	s.mu.Lock()
	if index < 0 || index >= len(s.items) {
		s.mu.Unlock()
		return Selection{}, errors.New("select a captured media resource")
	}
	item := s.items[index]
	s.mu.Unlock()
	var jar struct {
		Cookies []struct {
			Name               string          `json:"name"`
			Value              string          `json:"value"`
			Domain             string          `json:"domain"`
			Path               string          `json:"path"`
			Secure             bool            `json:"secure"`
			Session            bool            `json:"session"`
			Expires            float64         `json:"expires"`
			PartitionKey       json.RawMessage `json:"partitionKey"`
			PartitionKeyOpaque bool            `json:"partitionKeyOpaque"`
		} `json:"cookies"`
	}
	if err := s.conn.call(ctx, "", "Storage.getCookies", map[string]any{}, &jar); err != nil {
		return Selection{}, err
	}
	var cookies strings.Builder
	cookies.WriteString("# Netscape HTTP Cookie File\n")
	for _, c := range jar.Cookies {
		// Partitioned cookies cannot be represented safely in a Netscape jar.
		if len(c.PartitionKey) > 0 || c.PartitionKeyOpaque || strings.ContainsAny(c.Name+c.Value+c.Domain+c.Path, "\t\r\n\x00") {
			continue
		}
		expires := int64(c.Expires)
		if c.Session || expires < 0 {
			expires = 0
		}
		fmt.Fprintf(&cookies, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", c.Domain,
			strings.ToUpper(strconv.FormatBool(strings.HasPrefix(c.Domain, "."))), c.Path,
			strings.ToUpper(strconv.FormatBool(c.Secure)), expires, c.Name, c.Value)
	}
	cookieFile := filepath.Join(s.directory, "download.cookies")
	if err := os.WriteFile(cookieFile, []byte(cookies.String()), 0o600); err != nil {
		return Selection{}, errors.New("could not prepare private browser cookies")
	}
	if item.agent == "" {
		var version struct {
			UserAgent string `json:"userAgent"`
		}
		if err := s.conn.call(ctx, "", "Browser.getVersion", map[string]any{}, &version); err != nil {
			return Selection{}, err
		}
		item.agent = version.UserAgent
	}
	var config strings.Builder
	writeOption := func(flag, value string) {
		// shlex-compatible quoting without interpreting backslashes or newlines.
		value = strings.ReplaceAll(value, "'", "'\"'\"'")
		fmt.Fprintf(&config, "%s '%s'\n", flag, value)
	}
	writeOption("--cookies", cookieFile)
	for _, option := range []struct{ flag, value string }{
		{"--referer", item.referer}, {"--user-agent", item.agent},
	} {
		if strings.ContainsAny(option.value, "\r\n\x00") {
			return Selection{}, errors.New("invalid browser request headers")
		}
		if option.value != "" {
			writeOption(option.flag, option.value)
		}
	}
	if item.origin != "" {
		if strings.ContainsAny(item.origin, "\r\n\x00") {
			return Selection{}, errors.New("invalid browser request headers")
		}
		writeOption("--add-headers", "Origin:"+item.origin)
	}
	// Extractors often derive a direct file's title from its private URL path.
	// Use stable, local names before that metadata reaches the terminal or disk.
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(item.url)))[:12]
	writeOption("--parse-metadata", "Browser media:%(title)s")
	writeOption("--parse-metadata", id+":%(id)s")
	writeOption("--parse-metadata", ":(?P<meta_purl>)")
	writeOption("--parse-metadata", ":(?P<meta_comment>)")
	configFile := filepath.Join(s.directory, "download.conf")
	if err := os.WriteFile(configFile, []byte(config.String()), 0o600); err != nil {
		return Selection{}, errors.New("could not prepare private download settings")
	}
	return Selection{URL: item.url, ConfigFile: configFile}, nil
}
