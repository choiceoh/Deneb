// Draft stream controls — throttled streaming message edit/delete lifecycle.
//
// Provides a DraftStreamLoop for rate-limited message updates during streaming,
// and FinalizableDraftStreamControls for managing the full lifecycle of a
// draft message (update, finalize, or clear/delete).
//
// Mirrors src/channels/draft-stream-loop.ts and draft-stream-controls.ts.
package channel

import (
	"sync"
	"time"
)

// SendOrEditFunc sends or edits a streaming message. Returns true if the
// message was sent/edited successfully, false to retry with the same text.
type SendOrEditFunc func(text string) (bool, error)

// DraftStreamLoop provides throttled streaming message updates.
type DraftStreamLoop struct {
	mu          sync.Mutex
	throttleMs  int
	isStopped   func() bool
	sendOrEdit  SendOrEditFunc
	lastSentAt  time.Time
	pendingText string
	inFlight    bool
	inFlightCh  chan struct{} // closed when in-flight completes
	timer       *time.Timer
	stopCh      chan struct{}
}

// NewDraftStreamLoop creates a new throttled draft stream loop.
func NewDraftStreamLoop(throttleMs int, isStopped func() bool, sendOrEdit SendOrEditFunc) *DraftStreamLoop {
	return &DraftStreamLoop{
		throttleMs: throttleMs,
		isStopped:  isStopped,
		sendOrEdit: sendOrEdit,
		stopCh:     make(chan struct{}),
	}
}

// Update queues a text update. If enough time has passed since the last send,
// it flushes immediately; otherwise it schedules a throttled send.
func (l *DraftStreamLoop) Update(text string) {
	l.mu.Lock()
	if l.isStopped() {
		l.mu.Unlock()
		return
	}
	l.pendingText = text

	if l.inFlight {
		l.scheduleLocked()
		l.mu.Unlock()
		return
	}

	if l.timer == nil && time.Since(l.lastSentAt) >= time.Duration(l.throttleMs)*time.Millisecond {
		l.mu.Unlock()
		l.doFlush()
		return
	}
	l.scheduleLocked()
	l.mu.Unlock()
}

// Flush sends any pending text immediately, waiting for in-flight requests.
func (l *DraftStreamLoop) Flush() {
	l.mu.Lock()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.mu.Unlock()

	for {
		if l.isStopped() {
			return
		}

		// Wait for in-flight.
		l.mu.Lock()
		ch := l.inFlightCh
		l.mu.Unlock()
		if ch != nil {
			<-ch
			continue
		}

		l.mu.Lock()
		text := l.pendingText
		if text == "" || isBlank(text) {
			l.pendingText = ""
			l.mu.Unlock()
			return
		}
		l.pendingText = ""
		l.markInFlightLocked()
		l.mu.Unlock()

		ok, _ := l.sendOrEdit(text)
		l.mu.Lock()
		l.clearInFlightLocked()
		if ok {
			l.lastSentAt = time.Now()
		} else {
			l.pendingText = text
		}
		pending := l.pendingText
		l.mu.Unlock()

		if ok && pending == "" {
			return
		}
		if !ok {
			return
		}
	}
}

// Stop clears pending text and cancels timers.
func (l *DraftStreamLoop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pendingText = ""
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}


// WaitForInFlight waits for any in-flight send to complete.
func (l *DraftStreamLoop) WaitForInFlight() {
	l.mu.Lock()
	ch := l.inFlightCh
	l.mu.Unlock()
	if ch != nil {
		<-ch
	}
}

func (l *DraftStreamLoop) scheduleLocked() {
	if l.timer != nil {
		return
	}
	delay := time.Duration(l.throttleMs)*time.Millisecond - time.Since(l.lastSentAt)
	if delay < 0 {
		delay = 0
	}
	l.timer = time.AfterFunc(delay, func() {
		l.mu.Lock()
		l.timer = nil
		l.mu.Unlock()
		l.doFlush()
	})
}

func (l *DraftStreamLoop) doFlush() {
	l.Flush()
}

func (l *DraftStreamLoop) markInFlightLocked() {
	l.inFlight = true
	l.inFlightCh = make(chan struct{})
}

func (l *DraftStreamLoop) clearInFlightLocked() {
	l.inFlight = false
	if l.inFlightCh != nil {
		close(l.inFlightCh)
		l.inFlightCh = nil
	}
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// FinalizableDraftStreamControls manages the lifecycle of a finalizable
// draft message: update while streaming, stop to finalize, or clear to delete.
type FinalizableDraftStreamControls struct {
	mu      sync.Mutex
	stopped bool
	final   bool
	loop    *DraftStreamLoop
}

// FinalizableDraftParams configures a FinalizableDraftStreamControls.
type FinalizableDraftParams struct {
	ThrottleMs int
	SendOrEdit SendOrEditFunc
}

// NewFinalizableDraftStreamControls creates a new finalizable draft controller.
func NewFinalizableDraftStreamControls(p FinalizableDraftParams) *FinalizableDraftStreamControls {
	c := &FinalizableDraftStreamControls{}
	c.loop = NewDraftStreamLoop(
		p.ThrottleMs,
		func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.stopped
		},
		p.SendOrEdit,
	)
	return c
}

// Update queues a text update for the draft message.
func (c *FinalizableDraftStreamControls) Update(text string) {
	c.mu.Lock()
	if c.stopped || c.final {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.loop.Update(text)
}

// Stop finalizes the draft message by flushing any remaining text.
func (c *FinalizableDraftStreamControls) Stop() {
	c.mu.Lock()
	c.final = true
	c.mu.Unlock()
	c.loop.Flush()
}

// StopForClear marks the draft as stopped and waits for in-flight to complete.
func (c *FinalizableDraftStreamControls) StopForClear() {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	c.loop.Stop()
	c.loop.WaitForInFlight()
}

// Loop returns the underlying DraftStreamLoop.
func (c *FinalizableDraftStreamControls) Loop() *DraftStreamLoop {
	return c.loop
}
