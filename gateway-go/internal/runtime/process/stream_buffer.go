package process

import (
	"sync"
)

// StreamBuffer is a thread-safe buffer that captures subprocess output while
// allowing concurrent reads of the data captured so far. It keeps the most
// recent bytes up to a configured capacity, discarding older data when full.
//
// It is assigned directly as exec.Cmd Stdout/Stderr, so exec's internal copy
// goroutine writes it while RPC polls read Snapshot concurrently.
type StreamBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

// NewStreamBuffer creates a buffer that retains up to capacity bytes.
func NewStreamBuffer(capacity int) *StreamBuffer {
	return &StreamBuffer{
		buf: make([]byte, 0, capacity),
		cap: capacity,
	}
}

// Write appends data to the buffer, dropping oldest bytes if capacity exceeded.
// Always reports the full length as written so io.Copy never sees a short write.
func (sb *StreamBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.buf = append(sb.buf, p...)
	if len(sb.buf) > sb.cap {
		// Keep only the most recent sb.cap bytes.
		sb.buf = sb.buf[len(sb.buf)-sb.cap:]
	}
	return len(p), nil
}

// Snapshot returns a copy of the current buffer contents.
func (sb *StreamBuffer) Snapshot() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return string(sb.buf)
}

// Len returns the current buffer length.
func (sb *StreamBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return len(sb.buf)
}
