package lmtpd

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// readReply reads one (possibly multi-line) SMTP/LMTP reply, returning the final
// line. Continuation lines have a '-' as the 4th char; the last has a space.
func readReply(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var last string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		last = strings.TrimRight(line, "\r\n")
		if len(last) < 4 || last[3] == ' ' {
			return last
		}
	}
}

func TestServer_Delivery(t *testing.T) {
	var (
		mu   sync.Mutex
		got  *Message
		seen = make(chan struct{}, 1)
	)
	handler := func(_ context.Context, msg *Message) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		select {
		case seen <- struct{}{}:
		default:
		}
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := New(ln.Addr().String(), handler, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.serveListener(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)

	if reply := readReply(t, r); !strings.HasPrefix(reply, "220") {
		t.Fatalf("greeting = %q, want 220", reply)
	}

	send := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	expect := func(want string) {
		t.Helper()
		if reply := readReply(t, r); !strings.HasPrefix(reply, want) {
			t.Fatalf("got %q, want prefix %q", reply, want)
		}
	}

	send("LHLO deneb.test")
	expect("250") // multiline LHLO reply
	send("MAIL FROM:<sender@topsolar.kr>")
	expect("250")
	send("RCPT TO:<a@deneb.local>")
	expect("250")
	send("RCPT TO:<b@deneb.local>")
	expect("250")
	send("DATA")
	expect("354")

	// Message with a dot-stuffed line to exercise un-stuffing.
	msg := strings.Join([]string{
		"From: 박영업 <park@vendor.co.kr>",
		"Subject: 발주서",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"본문 첫 줄",
		"..dot-stuffed line",
		".",
	}, "\r\n") + "\r\n"
	_, _ = io.WriteString(conn, msg)

	// LMTP sends one reply PER recipient (two here).
	expect("250")
	expect("250")

	send("QUIT")
	expect("221")

	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was never called")
	}

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("no message captured")
	}
	if got.Detail.Subject != "발주서" {
		t.Errorf("Subject = %q, want 발주서", got.Detail.Subject)
	}
	if !strings.Contains(got.Detail.Body, "본문 첫 줄") {
		t.Errorf("Body = %q", got.Detail.Body)
	}
	if !strings.Contains(got.Detail.Body, ".dot-stuffed line") {
		t.Errorf("dot-unstuffing failed: %q", got.Detail.Body)
	}
}

func TestSplitListenAddr(t *testing.T) {
	cases := map[string][2]string{
		"127.0.0.1:10024":     {"tcp", "127.0.0.1:10024"},
		"tcp:0.0.0.0:24":      {"tcp", "0.0.0.0:24"},
		"unix:/run/lmtp.sock": {"unix", "/run/lmtp.sock"},
	}
	for in, want := range cases {
		n, a := splitListenAddr(in)
		if n != want[0] || a != want[1] {
			t.Errorf("splitListenAddr(%q) = (%q,%q), want (%q,%q)", in, n, a, want[0], want[1])
		}
	}
}
