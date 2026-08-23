package safego

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// A nil logger must not turn a recovered panic into silence: the process
// default logger receives it. (The old contract documented nil as "panics are
// silently swallowed" — a goroutine that died with no trace is the one failure
// nobody can diagnose.)
func TestNilLoggerPanicsReachTheDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	done := make(chan struct{}, 2)
	Go(nil, "nil-logger-go", func() { defer func() { done <- struct{}{} }(); panic("boom-go") })
	GoWithSlog(nil, "nil-logger-slog", func() { defer func() { done <- struct{}{} }(); panic("boom-slog") })
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("goroutine did not finish")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		out := buf.String()
		mu.Unlock()
		if strings.Contains(out, "boom-go") && strings.Contains(out, "boom-slog") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("panics not logged via default logger; got %q", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
