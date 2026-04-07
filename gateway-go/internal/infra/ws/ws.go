// Package ws implements a minimal RFC 6455 WebSocket client and server.
// It covers the subset used by the Deneb gateway: text frames, ping/pong,
// close handshake, read limits, and context-aware I/O. No compression,
// fragmentation, or binary frames — these features are unused.
package ws

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-5DF5-4A896FEA6B4C"

// MessageType identifies the WebSocket data frame type.
type MessageType = int

const (
	MessageText   MessageType = 1
	MessageBinary MessageType = 2
)

// Close status codes (RFC 6455 §7.4.1).
const (
	StatusNormalClosure   = 1000
	StatusGoingAway       = 1001
	StatusPolicyViolation = 1008
)

// Frame opcodes.
const (
	opText  = 1
	opClose = 8
	opPing  = 9
	opPong  = 10
)

// aLongTimeAgo unblocks pending reads when a context is cancelled.
var aLongTimeAgo = time.Unix(0, 1)

// AcceptOptions configures Accept. Origin checking is always skipped
// (single-user deployment); the struct is kept for call-site clarity.
type AcceptOptions struct {
	InsecureSkipVerify bool
}

// DialOptions configures Dial.
type DialOptions struct {
	HTTPHeader http.Header
}

// --- Errors ---

// CloseError is returned when a WebSocket close frame is received.
type CloseError struct {
	Code   int
	Reason string
}

func (e CloseError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("websocket: close %d: %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("websocket: close %d", e.Code)
}

// CloseStatus extracts the status code from a CloseError. Returns -1
// if err is not a CloseError.
func CloseStatus(err error) int {
	var ce CloseError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return -1
}

// --- Conn ---

// Conn is a WebSocket connection. It is safe for one concurrent reader
// and one concurrent writer; additional writers must use the exported
// methods which serialize via writeMu.
type Conn struct {
	rwc       net.Conn
	br        *bufio.Reader
	isClient  bool
	readLimit atomic.Int64
	writeMu   sync.Mutex
	closed    atomic.Bool
	pongCh    chan struct{}
}

const defaultReadLimit = 32 << 20 // 32 MB

func newConn(rwc net.Conn, br *bufio.Reader, isClient bool) *Conn {
	c := &Conn{
		rwc:      rwc,
		br:       br,
		isClient: isClient,
		pongCh:   make(chan struct{}, 1),
	}
	c.readLimit.Store(int64(defaultReadLimit))
	return c
}

// SetReadLimit sets the maximum message size in bytes.
func (c *Conn) SetReadLimit(n int64) { c.readLimit.Store(n) }

// Read returns the next data message. Control frames (ping, pong,
// close) are handled transparently: pings are auto-answered, pongs
// signal any waiting Ping call, and close frames trigger a close reply.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	// Set deadline from context (fast path for WithTimeout/WithDeadline).
	if dl, ok := ctx.Deadline(); ok {
		c.rwc.SetReadDeadline(dl)
		defer c.rwc.SetReadDeadline(time.Time{})
	}
	// Watch for context cancellation (handles cancel without deadline,
	// and early cancel before deadline).
	if ctx.Done() != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				c.rwc.SetReadDeadline(aLongTimeAgo)
			case <-done:
			}
		}()
	}

	for {
		op, payload, err := c.readFrame()
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			return 0, nil, err
		}
		switch op {
		case opText:
			return MessageText, payload, nil
		case opPing:
			c.writeControl(opPong, payload)
		case opPong:
			select {
			case c.pongCh <- struct{}{}:
			default:
			}
		case opClose:
			code, reason := parseClosePayload(payload)
			c.sendClose(code, reason)
			return 0, nil, CloseError{Code: code, Reason: reason}
		}
	}
}

// Write sends a message. msgType is typically MessageText.
func (c *Conn) Write(ctx context.Context, msgType MessageType, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if dl, ok := ctx.Deadline(); ok {
		c.rwc.SetWriteDeadline(dl)
		defer c.rwc.SetWriteDeadline(time.Time{})
	}
	return c.writeFrame(byte(msgType), data)
}

// Ping sends a ping and blocks until a pong is received or the context
// expires. Only one goroutine should call Ping at a time.
func (c *Conn) Ping(ctx context.Context) error {
	// Drain any stale pong from a previous timed-out ping.
	select {
	case <-c.pongCh:
	default:
	}
	c.writeMu.Lock()
	err := c.writeFrame(opPing, nil)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-c.pongCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close sends a close frame with the given status code and reason,
// then closes the underlying connection.
func (c *Conn) Close(code int, reason string) error {
	if c.closed.Swap(true) {
		return net.ErrClosed
	}
	c.writeMu.Lock()
	c.sendClose(code, reason)
	c.writeMu.Unlock()
	return c.rwc.Close()
}

// CloseNow closes the underlying connection immediately without
// sending a close frame.
func (c *Conn) CloseNow() error {
	c.closed.Store(true)
	return c.rwc.Close()
}

// --- Frame I/O ---

func (c *Conn) readFrame() (op byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return
	}
	op = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int64(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}

	if limit := c.readLimit.Load(); length > limit {
		err = fmt.Errorf("websocket: frame %d bytes exceeds limit %d", length, limit)
		return
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		maskBytes(payload, mask)
	}
	return
}

func (c *Conn) writeFrame(op byte, payload []byte) error {
	plen := len(payload)

	// Max header: 2 (base) + 8 (extended len) + 4 (mask key) = 14.
	var hdr [14]byte
	hdr[0] = 0x80 | op // FIN + opcode
	n := 2

	var maskBit byte
	if c.isClient {
		maskBit = 0x80
	}

	switch {
	case plen <= 125:
		hdr[1] = maskBit | byte(plen)
	case plen <= 65535:
		hdr[1] = maskBit | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(plen))
		n = 4
	default:
		hdr[1] = maskBit | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(plen))
		n = 10
	}

	if c.isClient {
		rand.Read(hdr[n : n+4])
		n += 4
	}

	// Combine header + payload into one write syscall.
	buf := make([]byte, n+plen)
	copy(buf, hdr[:n])
	if c.isClient {
		mask := [4]byte{hdr[n-4], hdr[n-3], hdr[n-2], hdr[n-1]}
		for i, b := range payload {
			buf[n+i] = b ^ mask[i&3]
		}
	} else {
		copy(buf[n:], payload)
	}
	_, err := c.rwc.Write(buf)
	return err
}

func (c *Conn) writeControl(op byte, payload []byte) {
	c.writeMu.Lock()
	c.writeFrame(op, payload)
	c.writeMu.Unlock()
}

func (c *Conn) sendClose(code int, reason string) {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], reason)
	c.writeFrame(opClose, payload)
}

func parseClosePayload(p []byte) (int, string) {
	if len(p) < 2 {
		return StatusNormalClosure, ""
	}
	return int(binary.BigEndian.Uint16(p[:2])), string(p[2:])
}

// --- Accept ---

// Accept upgrades an HTTP request to a WebSocket connection.
func Accept(w http.ResponseWriter, r *http.Request, _ *AcceptOptions) (*Conn, error) {
	if !headerHas(r.Header, "Connection", "upgrade") {
		http.Error(w, "missing Connection: upgrade", http.StatusBadRequest)
		return nil, errors.New("websocket: missing Connection: upgrade")
	}
	if !headerHas(r.Header, "Upgrade", "websocket") {
		http.Error(w, "missing Upgrade: websocket", http.StatusBadRequest)
		return nil, errors.New("websocket: missing Upgrade: websocket")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("websocket: missing Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("websocket: ResponseWriter does not support Hijack")
	}
	rwc, brw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("websocket: hijack: %w", err)
	}

	accept := acceptKey(key)
	const resp = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "
	if _, err := brw.WriteString(resp + accept + "\r\n\r\n"); err != nil {
		rwc.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		rwc.Close()
		return nil, err
	}
	return newConn(rwc, brw.Reader, false), nil
}

// --- Dial ---

// Dial connects to the WebSocket server at urlStr.
func Dial(ctx context.Context, urlStr string, opts *DialOptions) (*Conn, *http.Response, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" || u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var d net.Dialer
	rwc, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
	}

	var keyBuf [16]byte
	rand.Read(keyBuf[:])
	key := base64.StdEncoding.EncodeToString(keyBuf[:])

	var sb strings.Builder
	sb.WriteString("GET ")
	sb.WriteString(u.RequestURI())
	sb.WriteString(" HTTP/1.1\r\nHost: ")
	sb.WriteString(u.Host)
	sb.WriteString("\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: ")
	sb.WriteString(key)
	sb.WriteString("\r\nSec-WebSocket-Version: 13\r\n")
	if opts != nil {
		for k, vs := range opts.HTTPHeader {
			for _, v := range vs {
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(v)
				sb.WriteString("\r\n")
			}
		}
	}
	sb.WriteString("\r\n")

	if _, err := io.WriteString(rwc, sb.String()); err != nil {
		rwc.Close()
		return nil, nil, err
	}

	br := bufio.NewReaderSize(rwc, 4096)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		rwc.Close()
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		resp.Body.Close()
		rwc.Close()
		return nil, resp, fmt.Errorf("websocket: expected 101, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		rwc.Close()
		return nil, resp, errors.New("websocket: invalid Sec-WebSocket-Accept")
	}
	return newConn(rwc, br, true), resp, nil
}

// --- Helpers ---

func acceptKey(key string) string {
	h := sha1.New()
	io.WriteString(h, key)
	io.WriteString(h, wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func maskBytes(b []byte, mask [4]byte) {
	for i := range b {
		b[i] ^= mask[i&3]
	}
}

func headerHas(h http.Header, key, value string) bool {
	for _, v := range h[http.CanonicalHeaderKey(key)] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), value) {
				return true
			}
		}
	}
	return false
}
