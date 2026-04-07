package process

import (
	"io"
	"sync"
)

// StreamBuffer is a thread-safe buffer that captures subprocess output while
// allowing concurrent reads of the data captured so far. It keeps the most
// recent bytes up to a configured capacity, discarding older data when full.
type StreamBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	done bool
}

// NewStreamBuffer creates a buffer that retains up to capacity bytes.
func NewStreamBuffer(capacity int) *StreamBuffer {
	return &StreamBuffer{
		buf: make([]byte, 0, capacity),
		cap: capacity,
	}
}

// Write appends data to the buffer, dropping oldest bytes if capacity exceeded.
// Implements io.Writer so it can be used as a pipe drain target.
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

// MarkDone signals that no more writes will occur.
func (sb *StreamBuffer) MarkDone() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.done = true
}

// IsDone returns true if MarkDone was called.
func (sb *StreamBuffer) IsDone() bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.done
}

// drainToBuffer copies all data from r into sb, then marks it done.
// Used as a goroutine to drain subprocess pipes.
func drainToBuffer(r io.Reader, sb *StreamBuffer) {
	buf := make([]byte, 32*1024) // 32KB read chunks
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n]) //nolint:errcheck // best-effort
		}
		if err != nil {
			break
		}
	}
	sb.MarkDone()
}
