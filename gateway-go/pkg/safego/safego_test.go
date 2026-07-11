package safego

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type recordingLogger struct {
	calls chan string
}

func (l *recordingLogger) Error(msg string, args ...any) {
	l.calls <- fmt.Sprint(append([]any{msg}, args...)...)
}

func TestGoRecoversAndReportsPanic(t *testing.T) {
	logger := &recordingLogger{calls: make(chan string, 1)}
	Go(logger, "sync-loop", func() { panic("boom") })

	select {
	case got := <-logger.calls:
		if !strings.Contains(got, "sync-loop") || !strings.Contains(got, "boom") {
			t.Fatalf("panic log = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("panic was not recovered and logged")
	}
}

func TestGoRunsFunctionAndAllowsNilLogger(t *testing.T) {
	done := make(chan struct{})
	Go(nil, "worker", func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not run")
	}

	panicDone := make(chan struct{})
	Go(nil, "worker", func() {
		defer close(panicDone)
		panic("ignored")
	})
	select {
	case <-panicDone:
	case <-time.After(time.Second):
		t.Fatal("nil logger panic path blocked")
	}
}
