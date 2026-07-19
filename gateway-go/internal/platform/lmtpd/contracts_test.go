package lmtpd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func rawContractMail(subject, body string) []byte {
	return []byte(strings.Join([]string{
		"From: Sender <sender@example.test>",
		"To: Recipient <recipient@example.test>",
		"Message-ID: <contract@example.test>",
		"Date: Sat, 11 Jul 2026 10:00:00 +0900",
		"Subject: " + subject,
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
		"",
	}, "\r\n"))
}

func TestParseMessageExportedContract(t *testing.T) {
	raw := rawContractMail("계약 제목", "계약 본문")
	got, err := ParseMessage(raw, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got.Detail == nil || got.Detail.ID != "contract@example.test" || got.DedupKey != "contract@example.test" {
		t.Fatalf("message identity = %+v", got)
	}
	if got.Detail.Subject != "계약 제목" || got.Detail.Body != "계약 본문" {
		t.Fatalf("detail = %+v", got.Detail)
	}
	if got.Raw != nil {
		t.Fatalf("parser unexpectedly retained raw bytes")
	}
	got.Detail.Subject = "mutated"
	again, err := ParseMessage(raw, "fallback")
	if err != nil || again.Detail.Subject != "계약 제목" {
		t.Fatalf("parse result leaked mutation: %+v/%v", again, err)
	}
}

func TestParseMessageFallbackAndErrors(t *testing.T) {
	withoutID := []byte("From: a@example.test\r\nSubject: fallback\r\n\r\nbody\r\n")
	got, err := ParseMessage(withoutID, "generated-id")
	if err != nil {
		t.Fatal(err)
	}
	if got.Detail.ID != "generated-id" || got.DedupKey != "generated-id" {
		t.Fatalf("fallback identity = %+v", got)
	}
	for _, raw := range [][]byte{nil, {}, []byte("not an RFC message"), []byte("Bad Header\r\n\r\nbody")} {
		if _, err := ParseMessage(raw, "fallback"); err == nil || !strings.Contains(err.Error(), "parse message") {
			t.Errorf("ParseMessage(%q) error = %v", raw, err)
		}
	}
}

func TestProcessReturnsStatusForEachOutcome(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	good := rawContractMail("status", "body")
	for _, tt := range []struct {
		name    string
		body    []byte
		readErr error
		handler Handler
		want    string
	}{
		{name: "too big", readErr: errTooBig, want: "552 5.3.4 Message too big"},
		{name: "wrapped too big", readErr: fmt.Errorf("wrapped: %w", errTooBig), want: "552 5.3.4 Message too big"},
		{name: "read error", readErr: io.ErrUnexpectedEOF, want: "451 4.3.0 Read error, try again"},
		{name: "parse error", body: []byte("broken"), handler: func(context.Context, *Message) error { return nil }, want: "554 5.6.0 Parse error"},
		{name: "missing handler", body: good, want: "451 4.3.0 Processing unavailable, try again"},
		{name: "handler failure", body: good, handler: func(context.Context, *Message) error { return errors.New("offline") }, want: "451 4.3.0 Processing failed, try again"},
		{name: "success", body: good, handler: func(context.Context, *Message) error { return nil }, want: "250 2.0.0 OK"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := New("unused", tt.handler, logger)
			if got := s.process(context.Background(), tt.body, tt.readErr); got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessPreservesRawBytesAndContext(t *testing.T) {
	raw := rawContractMail("raw", "line one\r\nline two")
	type ctxKeyType struct{}
	ctxKey := ctxKeyType{}
	ctx := context.WithValue(context.Background(), ctxKey, "value")
	var got *Message
	s := New("unused", func(handlerCtx context.Context, msg *Message) error {
		if handlerCtx.Value(ctxKey) != "value" {
			t.Errorf("handler context not propagated")
		}
		got = msg
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if status := s.process(ctx, raw, nil); status != "250 2.0.0 OK" {
		t.Fatalf("status = %q", status)
	}
	if got == nil || !bytes.Equal(got.Raw, raw) {
		t.Fatalf("raw = %q, want %q", got.Raw, raw)
	}
}

func TestReadDataContract(t *testing.T) {
	for _, tt := range []struct {
		name  string
		wire  string
		max   int64
		want  string
		errIs error
	}{
		{name: "empty", wire: ".\r\n", max: 100, want: ""},
		{name: "crlf", wire: "one\r\ntwo\r\n.\r\n", max: 100, want: "one\r\ntwo\r\n"},
		{name: "lf", wire: "one\ntwo\n.\n", max: 100, want: "one\ntwo\n"},
		{name: "dot stuffed", wire: "..one\r\n...two\r\n.\r\n", max: 100, want: ".one\r\n..two\r\n"},
		{name: "limit exact", wire: "abcd\n.\n", max: 5, want: "abcd\n"},
		{name: "limit exceeded", wire: "abcdef\nignored\n.\n", max: 5, errIs: errTooBig},
		{name: "eof", wire: "", max: 100, errIs: io.EOF},
		{name: "partial eof", wire: "partial", max: 100, errIs: io.ErrUnexpectedEOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readData(bufio.NewReader(strings.NewReader(tt.wire)), tt.max)
			if !errors.Is(err, tt.errIs) {
				t.Fatalf("error = %v, want %v", err, tt.errIs)
			}
			if string(got) != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadDataOversizeDrainsToNextCommand(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("oversized\r\nsecond\r\n.\r\nNOOP\r\n"))
	if _, err := readData(br, 3); !errors.Is(err, errTooBig) {
		t.Fatalf("error = %v", err)
	}
	line, err := br.ReadString('\n')
	if err != nil || line != "NOOP\r\n" {
		t.Fatalf("next command = %q/%v", line, err)
	}
}

func TestDrainDataStopsAtTerminatorVariants(t *testing.T) {
	for _, tt := range []struct {
		name string
		wire string
		want string
	}{
		{name: "dot crlf", wire: "discard\r\n.\r\nnext\r\n", want: "next\r\n"},
		{name: "dot lf", wire: "discard\n.\nnext\n", want: "next\n"},
		{name: "eof", wire: "discard\n", want: ""},
		{name: "immediate eof", wire: "", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			br := bufio.NewReader(strings.NewReader(tt.wire))
			drainData(br)
			got, _ := io.ReadAll(br)
			if string(got) != tt.want {
				t.Fatalf("remainder = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitCommandContractAdditional(t *testing.T) {
	for _, tt := range []struct{ in, cmd, arg string }{
		{in: "", cmd: ""},
		{in: "NOOP", cmd: "NOOP"},
		{in: "  NOOP  ", cmd: "NOOP"},
		{in: "MAIL FROM:<a@b>", cmd: "MAIL", arg: "FROM:<a@b>"},
		{in: "RCPT\t TO:<a@b> ", cmd: "RCPT", arg: "TO:<a@b>"},
		{in: "X  a b c", cmd: "X", arg: "a b c"},
	} {
		cmd, arg := splitCommand(tt.in)
		if cmd != tt.cmd || arg != tt.arg {
			t.Errorf("splitCommand(%q) = %q/%q, want %q/%q", tt.in, cmd, arg, tt.cmd, tt.arg)
		}
	}
}

func TestNewContract(t *testing.T) {
	s := New("127.0.0.1:0", nil, nil)
	if s.addr != "127.0.0.1:0" || s.log == nil || s.hostname == "" {
		t.Fatalf("server = %+v", s)
	}
	custom := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := func(context.Context, *Message) error { return nil }
	s = New("unix:/tmp/test.sock", handler, custom)
	if s.handler == nil || s.addr != "unix:/tmp/test.sock" {
		t.Fatalf("custom server = %+v", s)
	}
}

func TestNewMessageIDCreatesUniqueFormattedIDs(t *testing.T) {
	seen := make(map[string]bool, 256)
	for range 256 {
		id := newMessageID()
		if !strings.HasPrefix(id, "lmtp-") {
			t.Fatalf("id = %q", id)
		}
		parts := strings.Split(id, "-")
		if len(parts) != 3 || len(parts[2]) != 12 {
			t.Fatalf("id shape = %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id = %q", id)
		}
		seen[id] = true
	}
}

type errorListener struct{ err error }

func (l errorListener) Accept() (net.Conn, error) { return nil, l.err }
func (errorListener) Close() error                { return nil }
func (errorListener) Addr() net.Addr              { return dummyAddr("error") }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }

func TestServeListenerAcceptError(t *testing.T) {
	s := New("unused", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := s.serveListener(context.Background(), errorListener{err: errors.New("accept exploded")})
	if err == nil || !strings.Contains(err.Error(), "accept exploded") {
		t.Fatalf("error = %v", err)
	}
}

func TestServeTCPAndCancellation(t *testing.T) {
	// Occupy then release an ephemeral address so Serve exercises its own bind.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s := New(addr, func(context.Context, *Message) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	var conn net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 30*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("dial Serve: %v", err)
	}
	r := bufio.NewReader(conn)
	if greeting := readReply(t, r); !strings.HasPrefix(greeting, "220 ") {
		t.Fatalf("greeting = %q", greeting)
	}
	_ = conn.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestServeListenFailure(t *testing.T) {
	ctx := context.Background()
	s := New("127.0.0.1:not-a-port", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Serve(ctx); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("Serve error = %v", err)
	}
}

func TestProtocolCommandStateContractMatrix(t *testing.T) {
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := New("unused", func(context.Context, *Message) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.connWg.Add(1)
	go s.serveConn(ctx, server)
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(client)
	if got := readReply(t, r); !strings.HasPrefix(got, "220") {
		t.Fatalf("greeting = %q", got)
	}
	send := func(command string, want string) {
		t.Helper()
		if _, err := io.WriteString(client, command+"\r\n"); err != nil {
			t.Fatal(err)
		}
		if got := readReply(t, r); !strings.HasPrefix(got, want) {
			t.Fatalf("%s => %q, want %s", command, got, want)
		}
	}
	send("RCPT TO:<a@example.test>", "503")
	send("DATA", "503")
	send("NOOP", "250")
	send("VRFY a@example.test", "252")
	send("UNKNOWN arg", "500")
	send("MAIL FROM:<a@example.test>", "250")
	send("RSET", "250")
	send("DATA", "503")
	send("QUIT", "221")
	_ = client.Close()
	done := make(chan struct{})
	go func() { s.connWg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection did not exit")
	}
}

func TestProtocolRejectsExcessRecipientsAndRepliesPerRecipient(t *testing.T) {
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	calls := 0
	s := New("unused", func(context.Context, *Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.connWg.Add(1)
	go s.serveConn(ctx, server)
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(client)
	_ = readReply(t, r)
	writeRead := func(command string) string {
		_, _ = io.WriteString(client, command+"\r\n")
		return readReply(t, r)
	}
	if got := writeRead("MAIL FROM:<a@example.test>"); !strings.HasPrefix(got, "250") {
		t.Fatal(got)
	}
	for i := 0; i < maxRecipients; i++ {
		if got := writeRead(fmt.Sprintf("RCPT TO:<r%d@example.test>", i)); !strings.HasPrefix(got, "250") {
			t.Fatalf("recipient %d = %q", i, got)
		}
	}
	if got := writeRead("RCPT TO:<overflow@example.test>"); !strings.HasPrefix(got, "452") {
		t.Fatalf("overflow = %q", got)
	}
	if got := writeRead("DATA"); !strings.HasPrefix(got, "354") {
		t.Fatalf("DATA = %q", got)
	}
	_, _ = client.Write(append(rawContractMail("many", "body"), []byte(".\r\n")...))
	for i := 0; i < maxRecipients; i++ {
		if got := readReply(t, r); !strings.HasPrefix(got, "250") {
			t.Fatalf("delivery reply %d = %q", i, got)
		}
	}
	if got := writeRead("QUIT"); !strings.HasPrefix(got, "221") {
		t.Fatal(got)
	}
	_ = client.Close()
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("handler calls = %d", calls)
	}
}

func TestQueueValidationAndDirectoryContract(t *testing.T) {
	if _, err := NewQueue(""); err == nil || !strings.Contains(err.Error(), "empty dir") {
		t.Fatalf("empty NewQueue = %v", err)
	}
	rootFile := filepath.Join(t.TempDir(), "root-file")
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQueue(rootFile); err == nil {
		t.Fatal("NewQueue accepted file root")
	}
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if q.Dir() == "" || filepath.Base(q.pendingDir) != "pending" || filepath.Base(q.processingDir) != "processing" || filepath.Base(q.failedDir) != "failed" {
		t.Fatalf("queue directories = %+v", q)
	}
	for _, tt := range []struct {
		name string
		msg  *Message
		want string
	}{
		{name: "nil", want: "nil message"},
		{name: "empty", msg: &Message{}, want: "empty key"},
		{name: "detail key but no raw", msg: &Message{Detail: queueTestMessage("id").Detail}, want: "empty raw"},
		{name: "dedup key but no raw", msg: &Message{DedupKey: "id"}, want: "empty raw"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := q.Enqueue(tt.msg)
			if ok || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Enqueue = %v/%v", ok, err)
			}
		})
	}
}

func TestQueueUsesDetailIDFallbackAndCopiesRaw(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("raw message")
	msg := &Message{Detail: queueTestMessage("detail-id").Detail, Raw: raw}
	ok, err := q.Enqueue(msg)
	if err != nil || !ok {
		t.Fatalf("Enqueue = %v/%v", ok, err)
	}
	raw[0] = 'X'
	item, err := q.Claim()
	if err != nil || item == nil {
		t.Fatalf("Claim = %+v/%v", item, err)
	}
	if item.Key != "detail-id" || string(item.Raw) != "raw message" {
		t.Fatalf("item = %+v", item)
	}
}

func TestQueueClaimReturnsOldestFirstWithStableTieBreak(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"zulu", "alpha", "middle"}
	for _, key := range keys {
		if ok, err := q.Enqueue(queueTestMessage(key)); err != nil || !ok {
			t.Fatalf("Enqueue %s = %v/%v", key, ok, err)
		}
	}
	base := time.Now().Add(-time.Hour)
	for i, key := range keys {
		path := filepath.Join(q.pendingDir, queueFileName(key))
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for range keys {
		item, err := q.Claim()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, item.Key)
		if err := q.Complete(item); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, keys) {
		t.Fatalf("claim order = %v, want %v", got, keys)
	}
	if item, err := q.Claim(); err != nil || item != nil {
		t.Fatalf("empty Claim = %+v/%v", item, err)
	}
}

func TestQueueCorruptClaimMovesItemToFailed(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "corrupt.json"
	if err := os.WriteFile(filepath.Join(q.pendingDir, name), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := q.Claim()
	if item != nil || err == nil {
		t.Fatalf("Claim = %+v/%v", item, err)
	}
	if _, err := os.Stat(filepath.Join(q.failedDir, name)); err != nil {
		t.Fatalf("corrupt item not failed: %v", err)
	}
	if stats := q.Stats(); stats.Pending != 0 || stats.Processing != 0 || stats.Failed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestQueueReadItemValidationMatrix(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		data string
		want string
	}{
		{name: "invalid json", data: "{", want: "unexpected end"},
		{name: "empty key", data: `{"raw":"cmF3"}`, want: "corrupt item"},
		{name: "empty raw", data: `{"key":"key"}`, want: "corrupt item"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "item.json")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := q.readItem(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readItem error = %v", err)
			}
		})
	}
	if _, err := q.readItem(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("readItem missing succeeded")
	}
}

func TestQueueEnqueueWriteFailureReturnsNotCreated(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(q.pendingDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q.pendingDir, []byte("blocks writes"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := q.Enqueue(queueTestMessage("cannot-write"))
	if ok || err == nil {
		t.Fatalf("Enqueue = %v/%v", ok, err)
	}
}

func TestQueueCompleteAndFailNilAreNoOps(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(nil); err != nil {
		t.Fatal(err)
	}
	if err := q.Fail(nil, errors.New("ignored"), 1); err != nil {
		t.Fatal(err)
	}
	if stats := q.Stats(); stats != (QueueStats{}) {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestQueueFailWithoutCauseAndUnlimitedRetries(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := q.Enqueue(queueTestMessage("retry")); err != nil || !ok {
		t.Fatalf("Enqueue = %v/%v", ok, err)
	}
	item, _ := q.Claim()
	if err := q.Fail(item, nil, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := q.Claim()
	if err != nil || retry.Attempts != 1 || retry.LastError != "" || retry.UpdatedAt.IsZero() {
		t.Fatalf("retry = %+v/%v", retry, err)
	}
	if err := q.Complete(retry); err != nil {
		t.Fatal(err)
	}
}

func TestQueuePathAndNameFallbacks(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := &QueueItem{Key: "key"}
	wantName := queueFileName("key")
	if got := q.itemName(item); got != wantName {
		t.Fatalf("itemName = %q", got)
	}
	if got := q.processingPath(item); got != filepath.Join(q.processingDir, wantName) {
		t.Fatalf("processingPath = %q", got)
	}
	item.name = "custom.json"
	item.path = "/custom/path"
	if q.itemName(item) != "custom.json" || q.processingPath(item) != "/custom/path" {
		t.Fatalf("custom fallbacks failed")
	}
}

func TestQueueFileNameFormatIsDeterministicSafeAndDistinct(t *testing.T) {
	a := queueFileName("same/key")
	b := queueFileName("same/key")
	c := queueFileName("same\\key")
	if a != b || a == c || len(a) != 69 || !strings.HasSuffix(a, ".json") {
		t.Fatalf("names = %q %q %q", a, b, c)
	}
	if strings.ContainsAny(a, `/\\ `) {
		t.Fatalf("unsafe filename = %q", a)
	}
}

func TestCountFilesAndRemoveIfExistsContracts(t *testing.T) {
	dir := t.TempDir()
	if got := countFiles(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing count = %d", got)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := countFiles(dir); got != 3 {
		t.Fatalf("count = %d", got)
	}
	path := filepath.Join(dir, "a")
	if err := removeIfExists(path); err != nil {
		t.Fatal(err)
	}
	if err := removeIfExists(path); err != nil {
		t.Fatal(err)
	}
	if err := removeIfExists(filepath.Join(dir, "nested")); err == nil {
		t.Fatal("removing non-empty/dir should fail")
	}
}
