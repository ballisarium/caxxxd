// Package browser discovers media through an isolated Chromium session.
package browser

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ballisarium/caxxxd/internal/process"
)

// Candidate contains display metadata only, never the captured address.
type Candidate struct {
	Kind string
	Host string
}

// Selection stays private to the download pipeline for the session lifetime.
type Selection struct {
	URL        string
	ConfigFile string
}

type Capture interface {
	Candidates() []Candidate
	Prepare(context.Context, int) (Selection, error)
	Err() error
	Close() error
}

type Launcher struct {
	Binary   string
	Headless bool // For opt-in local integration tests; normal use is visible.
	TempDir  string
}

type attached struct {
	target, session string
	err             error
}

type Session struct {
	mu        sync.Mutex
	items     []resource
	requests  map[string]request
	cmd       *exec.Cmd
	conn      *connection
	directory string
	done      chan struct{}
	ready     chan attached
	once      sync.Once
	closeErr  error
}

func ValidURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

// Start opens one visible window with a disposable profile. The user's usual
// browser and profile are never attached, read, or terminated.
func (l Launcher) Start(ctx context.Context, pageURL string) (capture Capture, startErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ValidURL(pageURL) {
		return nil, errors.New("enter an http:// or https:// page URL without embedded credentials")
	}
	binary := l.Binary
	if binary == "" {
		binary = findBrowser()
	}
	if binary == "" {
		return nil, errors.New("install Google Chrome, Chromium, or Microsoft Edge to use browser capture")
	}
	directory, err := os.MkdirTemp(l.TempDir, "caxxxd-browser-*")
	if err != nil {
		return nil, errors.New("could not create a private browser profile")
	}
	s := &Session{directory: directory, done: make(chan struct{}), ready: make(chan attached, 32), requests: make(map[string]request)}
	ok := false
	defer func() {
		if !ok {
			startErr = errors.Join(startErr, s.Close())
		}
	}()
	readChild, writeParent, err := os.Pipe()
	if err != nil {
		return nil, errors.New("could not create browser pipe")
	}
	defer readChild.Close()
	readParent, writeChild, err := os.Pipe()
	if err != nil {
		_ = writeParent.Close()
		return nil, errors.New("could not create browser pipe")
	}
	defer writeChild.Close()
	args := []string{"--remote-debugging-pipe", "--user-data-dir=" + directory,
		"--no-first-run", "--no-default-browser-check", "--no-startup-window"}
	if l.Headless {
		args = append(args, "--headless=new")
	}
	s.cmd = exec.Command(binary, args...)
	s.cmd.ExtraFiles = []*os.File{readChild, writeChild}
	process.ConfigureGroup(s.cmd)
	if err := s.cmd.Start(); err != nil {
		_ = readParent.Close()
		_ = writeParent.Close()
		return nil, errors.New("could not start the capture browser")
	}
	// Establish the connection before its reader can invoke event handlers.
	s.mu.Lock()
	s.conn = newConnection(readParent, writeParent, s.event)
	s.mu.Unlock()
	go func() {
		_ = s.cmd.Wait()
		close(s.done)
		s.conn.close()
	}()
	if err := s.conn.call(ctx, "", "Target.setAutoAttach", autoAttachParams(), nil); err != nil {
		return nil, err
	}
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := s.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &target); err != nil {
		return nil, err
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		select {
		case <-startup.Done():
			return nil, startup.Err()
		case <-s.conn.done:
			return nil, errClosed
		case ready := <-s.ready:
			if ready.target != target.TargetID {
				continue
			}
			if ready.err != nil {
				return nil, ready.err
			}
			var navigation struct {
				ErrorText string `json:"errorText"`
			}
			if err := s.conn.call(ctx, ready.session, "Page.navigate", map[string]any{"url": pageURL}, &navigation); err != nil {
				return nil, err
			}
			if navigation.ErrorText != "" {
				return nil, errors.New("the browser could not open this page; check the address and connection")
			}
			ok = true
			return s, nil
		}
	}
}

func findBrowser() string {
	userHome, _ := os.UserHomeDir()
	for _, base := range []string{"/Applications", filepath.Join(userHome, "Applications")} {
		for _, name := range []string{"Google Chrome", "Chromium", "Microsoft Edge"} {
			path := filepath.Join(base, name+".app", "Contents", "MacOS", name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return path
			}
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "microsoft-edge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func (s *Session) Err() error {
	select {
	case <-s.conn.done:
		return errClosed
	default:
		return nil
	}
}

func (s *Session) Close() error {
	s.once.Do(func() {
		if s.conn != nil {
			s.conn.close()
		}
		if s.cmd != nil && s.cmd.Process != nil {
			select {
			case <-s.done:
				// Do not signal a process ID after the browser has been reaped.
			default:
				_ = process.TerminateGroup(s.cmd)
				select {
				case <-s.done:
				case <-time.After(time.Second):
					_ = process.KillGroup(s.cmd)
					<-s.done
				}
			}
		}
		if err := os.RemoveAll(s.directory); err != nil {
			s.closeErr = errors.New("could not remove the temporary browser profile")
		}
	})
	return s.closeErr
}
