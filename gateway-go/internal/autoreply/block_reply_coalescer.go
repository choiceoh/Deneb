// block_reply_coalescer.go — Full streaming coalescer for reply payloads.
// Mirrors src/auto-reply/reply/block-reply-coalescer.ts (151 LOC).
// Buffers text payloads and flushes based on character thresholds and idle timers.
package autoreply

import (
	"strings"
	"sync"
	"time"
)

// BlockStreamingCoalescing configures the coalescing behavior for block streaming.
type BlockStreamingCoalescing struct {
	MinChars      int    `json:"minChars"`
	MaxChars      int    `json:"maxChars"`
	IdleMs        int    `json:"idleMs"`
	Joiner        string `json:"joiner,omitempty"`
	FlushOnEnqueue bool  `json:"flushOnEnqueue,omitempty"`
}

// BlockReplyCoalescer buffers and coalesces reply payloads, flushing them
// based on character thresholds and idle timers.
type BlockReplyCoalescer struct {
	mu sync.Mutex

	minChars       int
	maxChars       int
	idleMs         int
	joiner         string
	flushOnEnqueue bool

	bufferText         string
	bufferReplyToID    string
	bufferAudioAsVoice *bool

	idleTimer *time.Timer
	stopCh    chan struct{}

	shouldAbort func() bool
	onFlush     func(payload ReplyPayload)
}

// NewBlockReplyCoalescer creates a new coalescer matching the TS createBlockReplyCoalescer.
func NewBlockReplyCoalescer(config BlockStreamingCoalescing, shouldAbort func() bool, onFlush func(payload ReplyPayload)) *BlockReplyCoalescer {
	minChars := config.MinChars
	if minChars < 1 {
		minChars = 1
	}
	maxChars := config.MaxChars
	if maxChars < minChars {
		maxChars = minChars
	}
	idleMs := config.IdleMs
	if idleMs < 0 {
		idleMs = 0
	}

	return &BlockReplyCoalescer{
		minChars:       minChars,
		maxChars:       maxChars,
		idleMs:         idleMs,
		joiner:         config.Joiner,
		flushOnEnqueue: config.FlushOnEnqueue,
		shouldAbort:    shouldAbort,
		onFlush:        onFlush,
		stopCh:         make(chan struct{}),
	}
}

func (c *BlockReplyCoalescer) clearIdleTimer() {
	if c.idleTimer != nil {
		c.idleTimer.Stop()
		c.idleTimer = nil
	}
}

func (c *BlockReplyCoalescer) resetBuffer() {
	c.bufferText = ""
	c.bufferReplyToID = ""
	c.bufferAudioAsVoice = nil
}

func (c *BlockReplyCoalescer) scheduleIdleFlush() {
	if c.idleMs <= 0 {
		return
	}
	c.clearIdleTimer()
	c.idleTimer = time.AfterFunc(time.Duration(c.idleMs)*time.Millisecond, func() {
		// Idle timer fires a non-forced flush. If the buffer is still below
		// minChars, it will reschedule (matching TS semantics).
		c.Flush(false)
	})
}

// Flush sends the buffered content. If force is false, only flushes when
// the buffer meets the minimum character threshold (or schedules an idle flush).
func (c *BlockReplyCoalescer) Flush(force bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked(force)
}

func (c *BlockReplyCoalescer) flushLocked(force bool) {
	c.clearIdleTimer()
	if c.shouldAbort() {
		c.resetBuffer()
		return
	}
	if c.bufferText == "" {
		return
	}
	if !force && !c.flushOnEnqueue && len(c.bufferText) < c.minChars {
		c.scheduleIdleFlush()
		return
	}
	payload := ReplyPayload{
		Text:      c.bufferText,
		ReplyToID: c.bufferReplyToID,
	}
	if c.bufferAudioAsVoice != nil {
		payload.AudioAsVoice = *c.bufferAudioAsVoice
	}
	c.resetBuffer()
	// Call onFlush outside the lock would be ideal, but for simplicity
	// and matching TS fire-and-forget semantics, call inline.
	c.onFlush(payload)
}

// Enqueue adds a payload to the coalescer buffer.
func (c *BlockReplyCoalescer) Enqueue(payload ReplyPayload) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.shouldAbort() {
		return
	}

	hasMedia := payload.MediaURL != "" || len(payload.MediaURLs) > 0
	text := strings.TrimSpace(payload.Text)
	hasText := text != ""

	if hasMedia {
		c.flushLocked(true)
		c.onFlush(payload)
		return
	}
	if !hasText {
		return
	}

	// FlushOnEnqueue: treat each payload as its own block.
	if c.flushOnEnqueue {
		if c.bufferText != "" {
			c.flushLocked(true)
		}
		c.bufferReplyToID = payload.ReplyToID
		audioAsVoice := payload.AudioAsVoice
		c.bufferAudioAsVoice = &audioAsVoice
		c.bufferText = text
		c.flushLocked(true)
		return
	}

	// Detect reply-to conflicts or audio mode changes.
	replyToConflict := c.bufferText != "" &&
		payload.ReplyToID != "" &&
		(c.bufferReplyToID == "" || c.bufferReplyToID != payload.ReplyToID)

	audioMismatch := c.bufferAudioAsVoice != nil && *c.bufferAudioAsVoice != payload.AudioAsVoice
	if c.bufferText != "" && (replyToConflict || audioMismatch) {
		c.flushLocked(true)
	}

	if c.bufferText == "" {
		c.bufferReplyToID = payload.ReplyToID
		audioAsVoice := payload.AudioAsVoice
		c.bufferAudioAsVoice = &audioAsVoice
	}

	var nextText string
	if c.bufferText != "" {
		nextText = c.bufferText + c.joiner + text
	} else {
		nextText = text
	}

	if len(nextText) > c.maxChars {
		if c.bufferText != "" {
			c.flushLocked(true)
			c.bufferReplyToID = payload.ReplyToID
			audioAsVoice := payload.AudioAsVoice
			c.bufferAudioAsVoice = &audioAsVoice
			if len(text) >= c.maxChars {
				c.onFlush(payload)
				return
			}
			c.bufferText = text
			c.scheduleIdleFlush()
			return
		}
		c.onFlush(payload)
		return
	}

	c.bufferText = nextText
	if len(c.bufferText) >= c.maxChars {
		c.flushLocked(true)
		return
	}
	c.scheduleIdleFlush()
}

// HasBuffered returns true if there is buffered content.
func (c *BlockReplyCoalescer) HasBuffered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bufferText != ""
}

// Stop cancels any pending idle timer.
func (c *BlockReplyCoalescer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearIdleTimer()
}
