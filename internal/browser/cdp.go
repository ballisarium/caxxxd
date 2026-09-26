package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

var errClosed = errors.New("browser connection closed; reopen the page to scan again")

type packet struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// Chrome's debugging pipe uses NUL-delimited JSON on inherited descriptors.
// It exposes no listening port and requires no WebSocket dependency.
type connection struct {
	in      *os.File
	out     *os.File
	mu      sync.Mutex
	writeMu sync.Mutex
	next    int
	pending map[int]chan packet
	done    chan struct{}
	onEvent func(packet)
	once    sync.Once
}

func newConnection(in, out *os.File, onEvent func(packet)) *connection {
	c := &connection{in: in, out: out, pending: make(map[int]chan packet), done: make(chan struct{}), onEvent: onEvent}
	go c.read()
	return c
}

func (c *connection) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.in.Close()
		_ = c.out.Close()
	})
}

func (c *connection) read() {
	defer c.close()
	reader := bufio.NewReaderSize(c.in, 64*1024)
	var frame []byte
	for {
		part, err := reader.ReadSlice(0)
		if len(frame)+len(part) > 8<<20 {
			return
		}
		frame = append(frame, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return
		}
		var msg packet
		if json.Unmarshal(frame[:len(frame)-1], &msg) != nil {
			return
		}
		frame = frame[:0]
		if msg.ID != 0 {
			c.mu.Lock()
			reply := c.pending[msg.ID]
			c.mu.Unlock()
			if reply != nil {
				reply <- msg
			}
		} else {
			c.onEvent(msg)
		}
	}
}

func (c *connection) call(ctx context.Context, session, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c.mu.Lock()
	c.next++
	id := c.next
	reply := make(chan packet, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	data, err := json.Marshal(params)
	if err != nil {
		return errors.New("could not encode browser command")
	}
	data, err = json.Marshal(packet{ID: id, SessionID: session, Method: method, Params: data})
	if err != nil {
		return errors.New("could not encode browser command")
	}
	c.writeMu.Lock()
	deadline, _ := ctx.Deadline()
	_ = c.out.SetWriteDeadline(deadline)
	_, err = c.out.Write(append(data, 0))
	c.writeMu.Unlock()
	if err != nil {
		return errClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errClosed
	case msg := <-reply:
		// Chrome errors can echo signed addresses or page content.
		if len(msg.Error) > 0 {
			return errors.New("browser command failed: " + method)
		}
		if result != nil && json.Unmarshal(msg.Result, result) != nil {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
}
