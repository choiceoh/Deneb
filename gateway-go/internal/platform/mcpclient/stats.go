package mcpclient

import (
	"strings"
	"time"
)

// Stats is a point-in-time snapshot of one client's operational state, for
// health/observe surfaces. All fields are copies — safe to retain.
type Stats struct {
	Ready         bool
	Closed        bool
	ServerName    string
	ServerVersion string
	Spawns        int
	Calls         uint64
	CallErrors    uint64
	LastError     string
	LastErrorAt   time.Time
	RecentStderr  []string
}

// Stats returns the current snapshot.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	s := Stats{
		Ready:         c.ready,
		Closed:        c.closed,
		ServerName:    c.serverName,
		ServerVersion: c.serverVersion,
		Spawns:        c.spawns,
		LastError:     c.lastError,
		LastErrorAt:   c.lastErrorAt,
	}
	c.mu.Unlock()
	s.Calls = c.calls.Load()
	s.CallErrors = c.callErrors.Load()

	c.stderrMu.Lock()
	s.RecentStderr = append([]string(nil), c.stderrTail...)
	c.stderrMu.Unlock()
	return s
}

// noteCallError records a failed round trip for Stats and returns err
// unchanged so call sites stay one-line.
func (c *Client) noteCallError(err error) error {
	c.callErrors.Add(1)
	c.mu.Lock()
	c.noteErrorLocked(err)
	c.mu.Unlock()
	return err
}

// noteErrorLocked stamps the last-error fields. Caller holds c.mu.
func (c *Client) noteErrorLocked(err error) {
	c.lastError = err.Error()
	c.lastErrorAt = time.Now()
}

// recordStderr appends one child stderr line to the bounded diagnosis ring.
func (c *Client) recordStderr(line string) {
	if len(line) > stderrLineCap {
		line = truncateRuneSafe(line, stderrLineCap)
	}
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	c.stderrTail = append(c.stderrTail, line)
	if len(c.stderrTail) > stderrRingSize {
		c.stderrTail = c.stderrTail[len(c.stderrTail)-stderrRingSize:]
	}
}

// stderrSnippet joins the newest n ring lines for error enrichment ("" when
// the ring is empty). Safe without c.mu — only stderrMu is taken.
func (c *Client) stderrSnippet(n int) string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	if len(c.stderrTail) == 0 {
		return ""
	}
	start := len(c.stderrTail) - n
	if start < 0 {
		start = 0
	}
	return strings.Join(c.stderrTail[start:], " | ")
}
