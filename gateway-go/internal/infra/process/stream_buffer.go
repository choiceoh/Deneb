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
	mu      sync.Mutex
	buf     []byte
	cap     int
	dropped int64 // bytes discarded from the head after the buffer filled
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
		// Keep only the most recent sb.cap bytes. Count what fell off the
		// head: a tail-only capture silently masquerades as complete output
		// otherwise (a 2MB result reads as if it started mid-line).
		sb.dropped += int64(len(sb.buf) - sb.cap)
		sb.buf = sb.buf[len(sb.buf)-sb.cap:]
	}
	return len(p), nil
}

// Dropped returns how many head bytes were discarded to stay under capacity.
func (sb *StreamBuffer) Dropped() int64 {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.dropped
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
