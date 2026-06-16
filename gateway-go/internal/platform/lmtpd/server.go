package lmtpd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// Handler processes one fully-received, parsed message (detail + attachment bytes
// + dedup key). Returning an error makes the server reply with a temporary failure
// (4xx) so the sending MTA retries later — the message is never silently dropped.
type Handler func(ctx context.Context, msg *Message) error

const (
	// maxMessageBytes bounds a single DATA payload. A docker-mailserver delivery
	// is one message; 50 MiB comfortably covers business mail with attachments.
	maxMessageBytes = 50 << 20
	commandTimeout  = 2 * time.Minute
	dataTimeout     = 5 * time.Minute
)

var errTooBig = errors.New("lmtp: message exceeds size limit")

// Server is a minimal LMTP (RFC 2033) receiver. It is meant for a TRUSTED local
// sender (the on-box Docker mail server) — bind it to loopback or a unix socket,
// never the public internet; there is no SMTP AUTH (LMTP assumes a trusted path).
type Server struct {
	addr     string
	handler  Handler
	log      *slog.Logger
	hostname string
}

// New builds a server. addr is "host:port" (TCP), "tcp:host:port", or
// "unix:/path/to.sock".
func New(addr string, handler Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "deneb"
	}
	return &Server{addr: addr, handler: handler, log: log.With("pkg", "lmtpd"), hostname: host}
}

// Serve listens and handles connections until ctx is cancelled (closing the
// listener). Blocking; run it under a supervised goroutine.
func (s *Server) Serve(ctx context.Context) error {
	network, address := splitListenAddr(s.addr)
	if network == "unix" {
		_ = os.Remove(address) // clear a stale socket from an unclean shutdown
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, network, address)
	if err != nil {
		return fmt.Errorf("lmtp: listen %s %s: %w", network, address, err)
	}
	s.log.Info("LMTP 서버 수신 대기", "network", network, "address", address)
	return s.serveListener(ctx, ln)
}

// serveListener accepts connections on ln until ctx is cancelled (which closes
// ln). Split from Serve so tests can supply an ephemeral-port listener.
func (s *Server) serveListener(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // listener closed by graceful shutdown
			default:
				return fmt.Errorf("lmtp: accept: %w", err)
			}
		}
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("panic in LMTP connection", "panic", r)
		}
		_ = conn.Close()
	}()

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	reply := func(line string) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := bw.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return bw.Flush() == nil
	}

	if !reply("220 " + s.hostname + " Deneb LMTP ready") {
		return
	}

	var rcptCount int
	for {
		_ = conn.SetReadDeadline(time.Now().Add(commandTimeout))
		raw, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line := strings.TrimRight(raw, "\r\n")
		cmd, _ := splitCommand(line) // address args are accepted but not parsed

		switch strings.ToUpper(cmd) {
		case "LHLO":
			// LMTP requires LHLO (not HELO/EHLO). Advertise the basics.
			reply("250-" + s.hostname)
			reply(fmt.Sprintf("250-SIZE %d", maxMessageBytes))
			reply("250-8BITMIME")
			reply("250-PIPELINING")
			reply("250 ENHANCEDSTATUSCODES")
		case "MAIL":
			reply("250 2.1.0 OK")
		case "RCPT":
			rcptCount++
			reply("250 2.1.5 OK")
		case "DATA":
			if rcptCount == 0 {
				reply("503 5.5.1 RCPT first")
				continue
			}
			if !reply("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(dataTimeout))
			body, derr := readData(br, maxMessageBytes)
			status := s.process(ctx, body, derr)
			// LMTP sends ONE reply per recipient (RFC 2033 §4.2): each local
			// delivery is acknowledged independently.
			for range rcptCount {
				if !reply(status) {
					return
				}
			}
			rcptCount = 0
		case "RSET":
			rcptCount = 0
			reply("250 2.0.0 OK")
		case "NOOP":
			reply("250 2.0.0 OK")
		case "VRFY":
			reply("252 2.5.2 Cannot VRFY")
		case "QUIT":
			reply("221 2.0.0 Bye")
			return
		default:
			reply("500 5.5.2 Unrecognized command")
		}
	}
}

// process parses + hands off a received message, returning the per-recipient LMTP
// status line. A parse failure is permanent (5xx, don't retry); a handler error
// is temporary (4xx) so the MTA retries rather than losing the mail.
func (s *Server) process(ctx context.Context, body []byte, readErr error) string {
	if readErr != nil {
		if errors.Is(readErr, errTooBig) {
			return "552 5.3.4 Message too big"
		}
		s.log.Error("LMTP DATA read 실패", "error", readErr)
		return "451 4.3.0 Read error, try again"
	}
	msg, err := parseMessage(body, newMessageID())
	if err != nil {
		s.log.Error("LMTP 메시지 파싱 실패", "error", err)
		return "554 5.6.0 Parse error"
	}
	if err := s.handler(ctx, msg); err != nil {
		s.log.Error("LMTP 메시지 처리 실패", "error", err, "subject", msg.Detail.Subject)
		return "451 4.3.0 Processing failed, try again"
	}
	s.log.Info("LMTP 메시지 수신·처리", "from", msg.Detail.From, "subject", msg.Detail.Subject)
	return "250 2.0.0 OK"
}

// readData reads a DATA payload until the terminating "." line, undoing
// dot-stuffing and preserving CRLF line endings for the MIME parser.
func readData(br *bufio.Reader, max int64) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF && line != "" {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if strings.TrimRight(line, "\r\n") == "." {
			return buf.Bytes(), nil
		}
		line = strings.TrimPrefix(line, ".") // dot-unstuffing: a leading "." is doubled on the wire
		buf.WriteString(line)
		if int64(buf.Len()) > max {
			// Drain to the terminator so the connection stays in sync, then fail.
			drainData(br)
			return nil, errTooBig
		}
	}
}

func drainData(br *bufio.Reader) {
	for {
		line, err := br.ReadString('\n')
		if err != nil || strings.TrimRight(line, "\r\n") == "." {
			return
		}
	}
}

// splitCommand splits a command line into the verb and the remainder.
func splitCommand(line string) (cmd, arg string) {
	line = strings.TrimSpace(line)
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return line[:i], strings.TrimSpace(line[i+1:])
	}
	return line, ""
}

// splitListenAddr maps a config address to (network, address) for net.Listen.
// "unix:/p" → unix socket; "tcp:h:p" or "h:p" → TCP.
func splitListenAddr(addr string) (network, address string) {
	switch {
	case strings.HasPrefix(addr, "unix:"):
		return "unix", strings.TrimPrefix(addr, "unix:")
	case strings.HasPrefix(addr, "tcp:"):
		return "tcp", strings.TrimPrefix(addr, "tcp:")
	default:
		return "tcp", addr
	}
}

// newMessageID gives each received message a stable, unique id for the
// MessageDetail (the analysis cache + per-message wiki page key).
func newMessageID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("lmtp-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}
