// Draft stream loop — throttled streaming message edit/delete lifecycle.
//
// DraftStreamLoop manages rate-limited message updates during LLM streaming,
// with lifecycle controls for finalization and clear/delete.
package telegram

import (
	"sync"
	"time"
)

// SendOrEditFunc sends or edits a streaming message. Returns true if the
// message was sent/edited successfully, false to retry with the same text.
type SendOrEditFunc func(text string) (bool, error)

// maxFloodStrikes bounds how many consecutive send/edit failures (e.g. 429
// flood control, transient HTTP errors) we tolerate before permanently
// disabling progressive streaming for this loop. Further updates no-op; the
// caller still gets a single final send via Flush when the turn completes.
// Inspired by NousResearch/hermes-agent stream_consumer._MAX_FLOOD_STRIKES.
const maxFloodStrikes = 3

// DraftStreamLoop provides throttled streaming message updates with lifecycle.
type DraftStreamLoop struct {
	mu          sync.Mutex
	throttleMs  int
	sendOrEdit  SendOrEditFunc
	lastSentAt  time.Time
	pendingText string
	inFlight    bool
	inFlightCh  chan struct{} // closed when in-flight completes
	timer       *time.Timer
	stopped     bool // marks loop as stopped (no new updates accepted)
	finalized   bool // marks loop as finalized (flush then stop)

	// Strike counter for consecutive send/edit failures. When reaching
	// maxFloodStrikes, editDisabled flips true and subsequent Update() calls
	// are dropped — this stops hammering the platform when we're clearly
	// rate-limited and prevents a cascade of error logs.
	strikeCount  int
	editDisabled bool
}

// NewDraftStreamLoop creates a new throttled draft stream loop.
func NewDraftStreamLoop(throttleMs int, sendOrEdit SendOrEditFunc) *DraftStreamLoop {
	return &DraftStreamLoop{
		throttleMs: throttleMs,
		sendOrEdit: sendOrEdit,
	}
}

// Update queues a text update. If enough time has passed since the last send,
// it flushes immediately; otherwise it schedules a throttled send.
// No-op if the loop is stopped, finalized, or has been rate-limited too
// many times in a row.
func (l *DraftStreamLoop) Update(text string) {
	l.mu.Lock()
	if l.stopped || l.finalized || l.editDisabled {
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
		l.mu.Lock()
		if l.stopped {
			l.mu.Unlock()
			return
		}

		// Wait for in-flight.
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
			l.strikeCount = 0 // success resets the strike counter
		} else {
			l.pendingText = text
			l.strikeCount++
			if l.strikeCount >= maxFloodStrikes && !l.editDisabled {
				// Too many consecutive failures — stop progressive editing.
				// The caller still gets a chance to send one final message
				// via Flush when the turn completes. This prevents a tight
				// retry loop under sustained rate limiting.
				l.editDisabled = true
				l.pendingText = "" // drop pending so Flush is a no-op too
			}
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

// Finalize flushes any remaining text then marks the loop as done.
// Further Update calls are ignored.
func (l *DraftStreamLoop) Finalize() {
	l.mu.Lock()
	l.finalized = true
	l.mu.Unlock()
	l.Flush()
}

// Stop clears pending text and cancels timers without flushing.
func (l *DraftStreamLoop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	l.pendingText = ""
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

// StopForClear marks the loop as stopped, clears pending text, and waits
// for any in-flight send to complete before returning.
func (l *DraftStreamLoop) StopForClear() {
	l.Stop()
	l.WaitForInFlight()
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
