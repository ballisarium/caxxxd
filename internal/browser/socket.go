package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	webSocketGUID             = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	webSocketHandshakeTimeout = 45 * time.Second
	webSocketHandshakeLimit   = 64 << 10
	webSocketMessageLimit     = 8 << 20
)

var (
	errInvalidWebSocketEndpoint = errors.New("invalid browser debugging endpoint")
	errWebSocketHandshake       = errors.New("browser debugging handshake failed")
	errWebSocketFrame           = errors.New("invalid browser debugging message")
	errWebSocketMessage         = errors.New("invalid browser debugging command")
)

// webSocket adapts CDP's one-JSON-message-per-frame protocol to the NUL-
// delimited stream expected by connection. Client frames are always masked.
type webSocket struct {
	conn   net.Conn
	reader *bufio.Reader

	readMu        sync.Mutex
	writeMu       sync.Mutex
	writeDeadline time.Time

	readBuffer []byte
	fragment   []byte
	fragmented bool
	closeOnce  sync.Once
	closeErr   error
}

// dialWebSocket connects only to a loopback Chrome browser endpoint and
// completes the permission-aware WebSocket upgrade. Chrome may wait for the
// user to approve the connection, so the handshake has a bounded 45-second
// timeout, also limited by ctx.
func dialWebSocket(ctx context.Context, address string) (*webSocket, error) {
	host, port, target, err := webSocketTarget(address)
	if err != nil {
		return nil, errInvalidWebSocketEndpoint
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("could not connect to browser debugging endpoint")
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = conn.Close()
		}
	}()

	deadline := time.Now().Add(webSocketHandshakeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, errWebSocketHandshake
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stopCancel()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("could not prepare browser debugging handshake")
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	request := "GET " + target + " HTTP/1.1\r\n" +
		"Host: " + net.JoinHostPort(host, strconv.Itoa(port)) + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if err := writeSocketBytes(conn, []byte(request)); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errWebSocketHandshake
	}

	limitedReader := &limitedSocketReader{reader: conn, remaining: webSocketHandshakeLimit, limited: true}
	reader := bufio.NewReaderSize(limitedReader, 16<<10)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errWebSocketHandshake
	}
	if response.StatusCode != http.StatusSwitchingProtocols || response.ProtoMajor != 1 || response.ProtoMinor != 1 ||
		!strings.EqualFold(strings.TrimSpace(response.Header.Get("Upgrade")), "websocket") ||
		!headerHasToken(response.Header, "Connection", "upgrade") ||
		response.Header.Get("Sec-WebSocket-Accept") != webSocketAccept(key) ||
		response.Header.Get("Sec-WebSocket-Extensions") != "" {
		return nil, errWebSocketHandshake
	}
	limitedReader.limited = false
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, errWebSocketHandshake
	}
	stopCancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	keep = true
	return &webSocket{conn: conn, reader: reader}, nil
}

func webSocketTarget(address string) (host string, port int, target string, err error) {
	u, parseErr := url.Parse(address)
	if parseErr != nil || u == nil || u.Scheme != "ws" || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(address, "#") {
		return "", 0, "", errInvalidWebSocketEndpoint
	}
	host = u.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return "", 0, "", errInvalidWebSocketEndpoint
	}
	portText := u.Port()
	if portText == "" {
		return "", 0, "", errInvalidWebSocketEndpoint
	}
	for _, digit := range portText {
		if digit < '0' || digit > '9' {
			return "", 0, "", errInvalidWebSocketEndpoint
		}
	}
	port, parseErr = strconv.Atoi(portText)
	if parseErr != nil || port < 1 || port > 65535 {
		return "", 0, "", errInvalidWebSocketEndpoint
	}
	target = u.EscapedPath()
	const prefix = "/devtools/browser/"
	if !strings.HasPrefix(target, prefix) || len(target) == len(prefix) || strings.ContainsAny(target, "\r\n\x00") {
		return "", 0, "", errInvalidWebSocketEndpoint
	}
	return host, port, target, nil
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + webSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

type limitedSocketReader struct {
	reader    io.Reader
	remaining int64
	limited   bool
}

func (r *limitedSocketReader) Read(p []byte) (int, error) {
	if !r.limited {
		return r.reader.Read(p)
	}
	if r.remaining <= 0 {
		return 0, errWebSocketHandshake
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (ws *webSocket) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	ws.readMu.Lock()
	defer ws.readMu.Unlock()
	for len(ws.readBuffer) == 0 {
		message, err := ws.readMessage()
		if err != nil {
			return 0, err
		}
		if bytesContainNUL(message) || !utf8.Valid(message) || len(message)+1 > webSocketMessageLimit {
			return 0, errWebSocketFrame
		}
		ws.readBuffer = append(message, 0)
	}
	n := copy(p, ws.readBuffer)
	ws.readBuffer = ws.readBuffer[n:]
	return n, nil
}

func (ws *webSocket) readMessage() ([]byte, error) {
	for {
		final, opcode, payload, err := ws.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x0: // Continuation.
			if !ws.fragmented || len(ws.fragment)+len(payload) > webSocketMessageLimit-1 {
				return nil, errWebSocketFrame
			}
			ws.fragment = append(ws.fragment, payload...)
			if final {
				message := ws.fragment
				ws.fragment = nil
				ws.fragmented = false
				return message, nil
			}
		case 0x1: // Text.
			if ws.fragmented {
				return nil, errWebSocketFrame
			}
			if len(payload) > webSocketMessageLimit-1 {
				return nil, errWebSocketFrame
			}
			if final {
				return payload, nil
			}
			ws.fragmented = true
			ws.fragment = append(ws.fragment[:0], payload...)
		case 0x8: // Close.
			if len(payload) == 1 || len(payload) > 2 && !utf8.Valid(payload[2:]) {
				return nil, errWebSocketFrame
			}
			_ = ws.writeClose(payload)
			return nil, io.EOF
		case 0x9: // Ping.
			if err := ws.writeControlFrame(0xa, payload); err != nil {
				return nil, err
			}
		case 0xa: // Pong.
			continue
		default:
			return nil, errWebSocketFrame
		}
	}
}

func (ws *webSocket) readFrame() (final bool, opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err = io.ReadFull(ws.reader, header[:]); err != nil {
		return false, 0, nil, err
	}
	final = header[0]&0x80 != 0
	opcode = header[0] & 0x0f
	if header[0]&0x70 != 0 || header[1]&0x80 != 0 {
		return false, 0, nil, errWebSocketFrame
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var extended [2]byte
		if _, err = io.ReadFull(ws.reader, extended[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
		if length < 126 {
			return false, 0, nil, errWebSocketFrame
		}
	case 127:
		var extended [8]byte
		if _, err = io.ReadFull(ws.reader, extended[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended[:])
		if length < 1<<16 || length>>63 != 0 {
			return false, 0, nil, errWebSocketFrame
		}
	}
	control := opcode&0x08 != 0
	if length > webSocketMessageLimit || control && (!final || length > 125) {
		return false, 0, nil, errWebSocketFrame
	}
	if opcode != 0x0 && opcode != 0x1 && opcode != 0x2 && opcode != 0x8 && opcode != 0x9 && opcode != 0xa {
		return false, 0, nil, errWebSocketFrame
	}
	payload = make([]byte, int(length))
	if _, err = io.ReadFull(ws.reader, payload); err != nil {
		return false, 0, nil, err
	}
	if opcode == 0x2 {
		return false, 0, nil, errWebSocketFrame
	}
	return final, opcode, payload, nil
}

func (ws *webSocket) Write(p []byte) (int, error) {
	if len(p) == 0 || p[len(p)-1] != 0 || bytesContainNUL(p[:len(p)-1]) || len(p) > webSocketMessageLimit {
		return 0, errWebSocketMessage
	}
	if err := ws.writeFrame(0x1, p[:len(p)-1]); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (ws *webSocket) writeFrame(opcode byte, payload []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return ws.writeFrameLocked(opcode, payload, ws.writeDeadline)
}

func (ws *webSocket) writeFrameLocked(opcode byte, payload []byte, deadline time.Time) error {
	if len(payload) > webSocketMessageLimit {
		return errWebSocketFrame
	}
	if opcode&0x08 != 0 && len(payload) > 125 {
		return errWebSocketFrame
	}
	if err := ws.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return errors.New("could not prepare browser debugging message")
	}
	headerLength := 2 + len(mask)
	if len(payload) >= 1<<16 {
		headerLength += 8
	} else if len(payload) >= 126 {
		headerLength += 2
	}
	frame := make([]byte, headerLength+len(payload))
	frame[0] = 0x80 | opcode
	index := 2
	switch {
	case len(payload) < 126:
		frame[1] = 0x80 | byte(len(payload))
	case len(payload) < 1<<16:
		frame[1] = 0x80 | 126
		binary.BigEndian.PutUint16(frame[index:index+2], uint16(len(payload)))
		index += 2
	default:
		frame[1] = 0x80 | 127
		binary.BigEndian.PutUint64(frame[index:index+8], uint64(len(payload)))
		index += 8
	}
	copy(frame[index:index+len(mask)], mask[:])
	index += len(mask)
	for i, value := range payload {
		frame[index+i] = value ^ mask[i%len(mask)]
	}
	return writeSocketBytes(ws.conn, frame)
}

func (ws *webSocket) SetWriteDeadline(deadline time.Time) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	if err := ws.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	ws.writeDeadline = deadline
	return nil
}

func (ws *webSocket) Close() error {
	ws.closeOnce.Do(func() {
		ws.closeErr = ws.conn.Close()
	})
	return ws.closeErr
}

func (ws *webSocket) writeClose(payload []byte) error {
	return ws.writeControlFrame(0x8, payload)
}

func (ws *webSocket) writeControlFrame(opcode byte, payload []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	err := ws.writeFrameLocked(opcode, payload, time.Now().Add(250*time.Millisecond))
	if resetErr := ws.conn.SetWriteDeadline(ws.writeDeadline); err == nil {
		err = resetErr
	}
	return err
}

func writeSocketBytes(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func bytesContainNUL(data []byte) bool {
	for _, value := range data {
		if value == 0 {
			return true
		}
	}
	return false
}
