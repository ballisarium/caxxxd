package ui

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// ErrBack cancels the current prompt without ending the session.
var ErrBack = errors.New("back requested")

// promptReader distinguishes a standalone Esc from CSI/SS3 keys and paste.
// No background reader survives the prompt and consumes the next screen's input.
type promptReader struct {
	in       *os.File
	reader   *bufio.Reader
	paste    bool
	sequence string
}

func newPromptReader(in *os.File) *promptReader {
	return &promptReader{in: in, reader: bufio.NewReader(interruptReader{in})}
}

func (r *promptReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		key, err := r.reader.ReadByte()
		if err != nil {
			return n, err
		}
		if key == '\x1b' && !r.paste {
			ready, err := waitPromptInput(r.in, r.reader, 100)
			if err != nil {
				return 0, err
			}
			if !ready {
				return 0, ErrBack
			}
			next, err := r.reader.Peek(1)
			if err != nil {
				return 0, err
			}
			if next[0] == '\x1b' {
				return 0, ErrBack
			}
		}
		if key == '\x1b' {
			r.sequence = "\x1b"
		} else if r.sequence != "" {
			r.sequence += string(key)
			switch r.sequence {
			case "\x1b[200~":
				r.paste = true
				r.sequence = ""
			case "\x1b[201~":
				r.paste = false
				r.sequence = ""
			default:
				if !strings.HasPrefix("\x1b[200~", r.sequence) && !strings.HasPrefix("\x1b[201~", r.sequence) {
					r.sequence = ""
				}
			}
		}
		p[n] = key
		n++
		if r.reader.Buffered() == 0 {
			break
		}
	}
	return n, nil
}

func waitPromptInput(in *os.File, reader *bufio.Reader, timeout int) (bool, error) {
	if reader.Buffered() > 0 {
		return true, nil
	}
	fds := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}}
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		ready, err := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		if ready == 0 {
			return false, nil
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return false, os.ErrClosed
		}
		return fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}
