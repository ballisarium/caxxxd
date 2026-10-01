package browser

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDialWebSocketCDPRoundTrip(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen on loopback:", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := serveWebSocketFixture(w, r)
		if err != nil {
			t.Error(err)
		}
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test-session"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := dialWebSocket(ctx, endpoint)
	if err != nil {
		t.Fatal("connect to loopback debugging endpoint:", err)
	}
	defer ws.Close()

	request := []byte(`{"id":7,"method":"Browser.getVersion"}` + "\x00")
	if n, err := ws.Write(request); err != nil || n != len(request) {
		t.Fatalf("write CDP command: wrote %d of %d bytes: %v", n, len(request), err)
	}

	response := []byte(`{"id":7,"result":{"protocolVersion":"1.3"}}` + "\x00")
	got := make([]byte, len(response))
	if _, err := io.ReadFull(ws, got); err != nil {
		t.Fatal("read fragmented CDP response:", err)
	}
	if string(got) != string(response) {
		t.Fatalf("unexpected CDP response: got %q, want %q", got, response)
	}
	largeResponse := []byte(`{"id":8,"result":{"padding":"` + strings.Repeat("x", 70<<10) + `"}}` + "\x00")
	largeGot := make([]byte, len(largeResponse))
	if _, err := io.ReadFull(ws, largeGot); err != nil {
		t.Fatal("read response beyond handshake limit:", err)
	}
	if string(largeGot) != string(largeResponse) {
		t.Fatal("large CDP response was not preserved")
	}
	if _, err := ws.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read after peer close: got %v, want EOF", err)
	}
}

func serveWebSocketFixture(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Path != "/devtools/browser/test-session" || r.Method != http.MethodGet {
		return fmt.Errorf("unexpected handshake request")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return fmt.Errorf("test server does not support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return fmt.Errorf("hijack test connection: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		return fmt.Errorf("set test deadline: %w", err)
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || r.Header.Get("Sec-WebSocket-Version") != "13" ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		!testHeaderHasToken(r.Header, "Connection", "upgrade") {
		return fmt.Errorf("client sent an invalid websocket upgrade")
	}
	accept := testWebSocketAccept(key)
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		return fmt.Errorf("write test handshake: %w", err)
	}
	if err := rw.Flush(); err != nil {
		return fmt.Errorf("flush test handshake: %w", err)
	}

	opcode, payload, err := readMaskedTestFrame(rw.Reader)
	if err != nil {
		return fmt.Errorf("read client command: %w", err)
	}
	if opcode != 1 || string(payload) != `{"id":7,"method":"Browser.getVersion"}` {
		return fmt.Errorf("client did not send one JSON text message")
	}

	first := []byte(`{"id":7,"result":{"protocolVersion":`)
	if err := writeTestFrame(rw, false, 1, first); err != nil {
		return fmt.Errorf("write first response fragment: %w", err)
	}
	if err := writeTestFrame(rw, true, 9, []byte("probe")); err != nil {
		return fmt.Errorf("write ping: %w", err)
	}
	if err := rw.Flush(); err != nil {
		return fmt.Errorf("flush response and ping: %w", err)
	}
	opcode, payload, err = readMaskedTestFrame(rw.Reader)
	if err != nil {
		return fmt.Errorf("read pong: %w", err)
	}
	if opcode != 10 || string(payload) != "probe" {
		return fmt.Errorf("client did not echo the ping in a masked pong")
	}
	if err := writeTestFrame(rw, true, 0, []byte(`"1.3"}}`)); err != nil {
		return fmt.Errorf("write final response fragment: %w", err)
	}
	largeResponse := []byte(`{"id":8,"result":{"padding":"` + strings.Repeat("x", 70<<10) + `"}}`)
	if err := writeTestFrame(rw, true, 1, largeResponse); err != nil {
		return fmt.Errorf("write large response: %w", err)
	}
	if err := writeTestFrame(rw, true, 8, []byte{0x03, 0xe8}); err != nil {
		return fmt.Errorf("write close frame: %w", err)
	}
	if err := rw.Flush(); err != nil {
		return fmt.Errorf("flush responses and close: %w", err)
	}
	opcode, payload, err = readMaskedTestFrame(rw.Reader)
	if err != nil {
		return fmt.Errorf("read close response: %w", err)
	}
	if opcode != 8 || string(payload) != string([]byte{0x03, 0xe8}) {
		return fmt.Errorf("client did not echo the close frame")
	}
	return nil
}

func testHeaderHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func readMaskedTestFrame(r io.Reader) (byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	if header[1]&0x80 == 0 || header[1]&0x7f >= 126 {
		return 0, nil, fmt.Errorf("expected a short masked client frame")
	}
	var mask [4]byte
	if _, err := io.ReadFull(r, mask[:]); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, int(header[1]&0x7f))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%len(mask)]
	}
	return header[0] & 0x0f, payload, nil
}

func writeTestFrame(w io.Writer, final bool, opcode byte, payload []byte) error {
	first := opcode
	if final {
		first |= 0x80
	}
	var header []byte
	switch {
	case len(payload) < 126:
		header = []byte{first, byte(len(payload))}
	case len(payload) < 1<<16:
		header = []byte{first, 126, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	default:
		header = make([]byte, 10)
		header[0], header[1] = first, 127
		binary.BigEndian.PutUint64(header[2:], uint64(len(payload)))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func testWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func TestDialWebSocketRejectsUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:9222/devtools/browser/private-token",
		"ws://localhost:9222/devtools/browser/private-token",
		"ws://127.0.0.1:9222/devtools/page/private-token",
		"ws://user@127.0.0.1:9222/devtools/browser/private-token",
		"ws://127.0.0.1:9222/devtools/browser/private-token?debug=1",
		"ws://127.0.0.1/devtools/browser/private-token",
		"ws://127.0.0.1:99999/devtools/browser/private-token",
	} {
		_, err := dialWebSocket(context.Background(), endpoint)
		if err == nil {
			t.Fatalf("accepted unsafe websocket endpoint")
		}
		if strings.Contains(err.Error(), "private-token") {
			t.Fatalf("endpoint token leaked in error: %v", err)
		}
	}
}

func TestDialWebSocketCancellationInterruptsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen on loopback:", err)
	}
	defer listener.Close()
	ready := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			ready <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		request, err := http.ReadRequest(reader)
		if err == nil {
			_ = request.Body.Close()
		}
		ready <- err
		if err != nil {
			return
		}
		_, err = reader.ReadByte()
		serverDone <- err
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint := "ws://" + listener.Addr().String() + "/devtools/browser/cancel-test"
	dialDone := make(chan error, 1)
	go func() {
		_, err := dialWebSocket(ctx, endpoint)
		dialDone <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal("read websocket upgrade request:", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not begin the websocket handshake")
	}
	cancel()
	select {
	case err := <-dialDone:
		if err != context.Canceled {
			t.Fatalf("handshake cancellation returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handshake did not stop after cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("canceled connection remained open")
	}
}
