package browser

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type request struct {
	referer, origin, agent string
	url                    string
}

type resource struct {
	Candidate
	url string
	request
	session string
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
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		if s.borrowed {
			s.mu.Lock()
			related := s.targets[p.TargetInfo.TargetID] || s.targets[p.TargetInfo.OpenerID] || s.sessions[msg.SessionID]
			if !related {
				s.mu.Unlock()
				go func() {
					_ = conn.call(context.Background(), p.SessionID, "Runtime.runIfWaitingForDebugger", map[string]any{}, nil)
					_ = conn.call(context.Background(), "", "Target.detachFromTarget", map[string]any{"sessionId": p.SessionID}, nil)
				}()
				return
			}
			s.targets[p.TargetInfo.TargetID] = true
			s.sessions[p.SessionID] = true
			s.mu.Unlock()
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
			if s.borrowed && err != nil {
				s.mu.Lock()
				s.failure = errors.New("could not capture a player tab; reopen browser capture")
				s.mu.Unlock()
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
				URL     string            `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		r := request{}
		r.url = p.Request.URL
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
				URL      string         `json:"url"`
				MimeType string         `json:"mimeType"`
				Status   int            `json:"status"`
				Headers  map[string]any `json:"headers"`
			} `json:"response"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Response.Status < 200 || p.Response.Status >= 300 {
			return
		}
		s.mu.Lock()
		r := s.requests[msg.SessionID+":"+p.RequestID]
		r.url = p.Response.URL
		if s.requests == nil {
			s.requests = make(map[string]request)
		}
		if len(s.requests) < 8192 {
			s.requests[msg.SessionID+":"+p.RequestID] = r
		}
		s.mu.Unlock()
		s.observe(p.Response.URL, p.Response.MimeType, r.referer, r.origin, r.agent)
		s.mu.Lock()
		for i := range s.items {
			item := &s.items[i]
			if item.url == p.Response.URL {
				item.session = msg.SessionID
			}
			if item.url == p.Response.URL && item.Kind != "HLS" && item.Kind != "DASH" {
				if size := responseSize(p.Response.Status, p.Response.Headers); size > 0 {
					item.Size = size
				}
			}
		}
		s.mu.Unlock()
	case "Network.loadingFinished", "Network.loadingFailed":
		var p struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(msg.Params, &p) == nil {
			s.mu.Lock()
			r := s.requests[msg.SessionID+":"+p.RequestID]
			kind := ""
			for _, item := range s.items {
				if item.url == r.url && (item.Kind == "HLS" || item.Kind == "DASH") {
					kind = item.Kind
					break
				}
			}
			delete(s.requests, msg.SessionID+":"+p.RequestID)
			s.mu.Unlock()
			if kind != "" && msg.Method == "Network.loadingFinished" && conn != nil {
				go s.readManifest(conn, msg.SessionID, p.RequestID, r.url, kind)
			}
		}
	}
}

func (s *Session) readManifest(conn *connection, session, requestID, rawURL, kind string) {
	var response struct {
		Body   string `json:"body"`
		Base64 bool   `json:"base64Encoded"`
	}
	if conn.call(context.Background(), session, "Network.getResponseBody", map[string]any{"requestId": requestID}, &response) != nil || len(response.Body) > 2<<20 {
		return
	}
	if response.Base64 {
		body, err := base64.StdEncoding.DecodeString(response.Body)
		if err != nil {
			return
		}
		response.Body = string(body)
	}
	parsed := manifestSize(rawURL, kind, response.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifests == nil {
		s.manifests = make(map[string]manifestEstimate)
	}
	s.manifests[rawURL] = parsed
	for i := range s.items {
		item := &s.items[i]
		manifest, ok := s.manifests[item.url]
		if !ok {
			continue
		}
		item.Size, item.Approximate = manifest.Bytes, !manifest.Exact
	}
	for i := range s.items {
		item := &s.items[i]
		manifest := s.manifests[item.url]
		for _, variant := range manifest.Variants {
			child := s.manifests[variant.URL]
			bytes := child.DurationSeconds * float64(variant.Bandwidth) / 8
			if bytes > 0 && bytes < float64(1<<63) {
				if int64(bytes) > item.Size {
					item.Size = int64(bytes)
					item.Approximate = true
				}
				for j := range s.items {
					variantItem := &s.items[j]
					if variantItem.url == variant.URL && !child.Exact && int64(bytes) > variantItem.Size {
						variantItem.Size = int64(bytes)
						variantItem.Approximate = true
					}
				}
			}
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
			s.items[i].request = request{referer: referer, origin: origin, agent: agent}
			return
		}
	}
	if len(s.items) < 200 {
		s.items = append(s.items, resource{Candidate: Candidate{Kind: kind, Host: u.Hostname(), ID: len(s.items)}, url: rawURL, request: request{referer: referer, origin: origin, agent: agent}})
	}
}

func (s *Session) Candidates() []Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Candidate, len(s.items))
	for i, item := range s.items {
		items[i] = item.Candidate
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Size > items[j].Size })
	return items
}

func responseSize(status int, headers map[string]any) int64 {
	value := func(name string) string {
		for key, v := range headers {
			if strings.EqualFold(key, name) {
				return fmt.Sprint(v)
			}
		}
		return ""
	}
	var raw string
	if status == 206 {
		rangeValue := value("content-range")
		if !strings.HasPrefix(rangeValue, "bytes ") {
			return 0
		}
		_, raw, _ = strings.Cut(rangeValue, "/")
	} else if status == 200 && (value("content-encoding") == "" || value("content-encoding") == "identity") {
		raw = value("content-length")
	}
	size, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || size < 0 {
		return 0
	}
	return size
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
	cookieSession, cookieMethod, cookieParams := "", "Storage.getCookies", map[string]any{}
	if s.borrowed {
		cookieSession, cookieMethod, cookieParams = item.session, "Network.getCookies", map[string]any{"urls": []string{item.url}}
	}
	if err := s.conn.call(ctx, cookieSession, cookieMethod, cookieParams, &jar); err != nil {
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
