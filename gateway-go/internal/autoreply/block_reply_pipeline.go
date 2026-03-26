// block_reply_pipeline.go — Full streaming pipeline with dedup, timeout, abort.
// Mirrors src/auto-reply/reply/block-reply-pipeline.ts (261 LOC).
// Orchestrates reply delivery with deduplication, coalescing, buffering, and timeout.
package autoreply

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// BlockReplyPipelineConfig configures the block reply pipeline.
type BlockReplyPipelineConfig struct {
	// OnBlockReply delivers a single payload to the channel.
	OnBlockReply func(ctx context.Context, payload ReplyPayload) error
	// TimeoutMs per-payload delivery timeout.
	TimeoutMs int
	// Coalescing config (optional). When nil, payloads are sent directly.
	Coalescing *BlockStreamingCoalescing
	// Buffer strategy (optional).
	Buffer BlockReplyBufferStrategy
	// Logger for diagnostics.
	Logger *slog.Logger
}

// BlockReplyBufferStrategy allows certain payloads to be buffered until flush.
type BlockReplyBufferStrategy interface {
	ShouldBuffer(payload ReplyPayload) bool
	OnEnqueue(payload ReplyPayload)
	Finalize(payload ReplyPayload) ReplyPayload
}

// AudioAsVoiceBuffer buffers audio payloads and applies audioAsVoice flag at finalize.
type AudioAsVoiceBuffer struct {
	mu               sync.Mutex
	isAudioPayload   func(payload ReplyPayload) bool
	seenAudioAsVoice bool
}

// NewAudioAsVoiceBuffer creates a new audio buffer strategy.
func NewAudioAsVoiceBuffer(isAudioPayload func(ReplyPayload) bool) *AudioAsVoiceBuffer {
	return &AudioAsVoiceBuffer{isAudioPayload: isAudioPayload}
}

func (b *AudioAsVoiceBuffer) OnEnqueue(payload ReplyPayload) {
	if payload.AudioAsVoice {
		b.mu.Lock()
		b.seenAudioAsVoice = true
		b.mu.Unlock()
	}
}

func (b *AudioAsVoiceBuffer) ShouldBuffer(payload ReplyPayload) bool {
	return b.isAudioPayload(payload)
}

func (b *AudioAsVoiceBuffer) Finalize(payload ReplyPayload) ReplyPayload {
	b.mu.Lock()
	seen := b.seenAudioAsVoice
	b.mu.Unlock()
	if seen {
		payload.AudioAsVoice = true
	}
	return payload
}

// BlockReplyPipelineFull is the full streaming pipeline with deduplication and abort.
type BlockReplyPipelineFull struct {
	mu sync.Mutex

	cfg    BlockReplyPipelineConfig
	ctx    context.Context
	cancel context.CancelFunc

	sentKeys          map[string]bool
	sentContentKeys   map[string]bool
	pendingKeys       map[string]bool
	seenKeys          map[string]bool
	bufferedKeys      map[string]bool
	bufferedPayloadKs map[string]bool
	bufferedPayloads  []ReplyPayload

	coalescer *BlockReplyCoalescer

	aborted          bool
	didStreamFlag    bool
	didLogTimeout    bool
	droppedCount     int

	wg sync.WaitGroup
}

// NewBlockReplyPipelineFull creates a new full pipeline.
func NewBlockReplyPipelineFull(ctx context.Context, cfg BlockReplyPipelineConfig) *BlockReplyPipelineFull {
	pipeCtx, cancel := context.WithCancel(ctx)
	p := &BlockReplyPipelineFull{
		cfg:               cfg,
		ctx:               pipeCtx,
		cancel:            cancel,
		sentKeys:          make(map[string]bool),
		sentContentKeys:   make(map[string]bool),
		pendingKeys:       make(map[string]bool),
		seenKeys:          make(map[string]bool),
		bufferedKeys:      make(map[string]bool),
		bufferedPayloadKs: make(map[string]bool),
	}

	if cfg.Coalescing != nil {
		p.coalescer = NewBlockReplyCoalescer(*cfg.Coalescing, p.IsAborted, func(payload ReplyPayload) {
			p.mu.Lock()
			// Clear buffered keys on coalescer flush since they represent pre-coalesced state.
			p.bufferedKeys = make(map[string]bool)
			p.mu.Unlock()
			p.sendPayload(payload, true)
		})
	}

	return p
}

// payloadKey generates a full dedup key including text, media, and threading.
func payloadKey(p ReplyPayload) string {
	text := strings.TrimSpace(p.Text)
	replyToID := p.ReplyToID
	if replyToID == "" {
		replyToID = ""
	}
	key := struct {
		Text      string   `json:"text"`
		MediaList []string `json:"mediaList"`
		ReplyToID *string  `json:"replyToId"`
	}{
		Text:      text,
		MediaList: p.MediaURLs,
	}
	if replyToID != "" {
		key.ReplyToID = &replyToID
	}
	b, _ := json.Marshal(key)
	return string(b)
}

// contentKey generates a content-only dedup key (ignores threading).
func contentKey(p ReplyPayload) string {
	text := strings.TrimSpace(p.Text)
	key := struct {
		Text      string   `json:"text"`
		MediaList []string `json:"mediaList"`
	}{
		Text:      text,
		MediaList: p.MediaURLs,
	}
	b, _ := json.Marshal(key)
	return string(b)
}

func (p *BlockReplyPipelineFull) sendPayload(payload ReplyPayload, bypassSeenCheck bool) {
	p.mu.Lock()
	if p.aborted {
		p.droppedCount++
		if p.cfg.Logger != nil {
			text := payload.Text
			if len(text) > 80 {
				text = text[:80]
			}
			p.cfg.Logger.Debug("block reply pipeline: dropped payload after abort",
				"droppedTotal", p.droppedCount, "text", text)
		}
		p.mu.Unlock()
		return
	}

	pk := payloadKey(payload)
	ck := contentKey(payload)

	if !bypassSeenCheck {
		if p.seenKeys[pk] {
			p.mu.Unlock()
			return
		}
		p.seenKeys[pk] = true
	}
	if p.sentKeys[pk] || p.pendingKeys[pk] {
		p.mu.Unlock()
		return
	}
	p.pendingKeys[pk] = true
	p.mu.Unlock()

	timeoutMs := p.cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() {
			p.mu.Lock()
			delete(p.pendingKeys, pk)
			p.mu.Unlock()
		}()

		deliverCtx, cancel := context.WithTimeout(p.ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()

		err := p.cfg.OnBlockReply(deliverCtx, payload)
		if err != nil {
			if deliverCtx.Err() == context.DeadlineExceeded {
				p.mu.Lock()
				p.aborted = true
				if !p.didLogTimeout && p.cfg.Logger != nil {
					p.didLogTimeout = true
					p.cfg.Logger.Warn("block reply pipeline aborted: delivery timed out",
						"timeoutMs", timeoutMs)
				}
				p.mu.Unlock()
				return
			}
			if p.cfg.Logger != nil {
				p.cfg.Logger.Warn("block reply delivery failed", "error", err)
			}
			return
		}

		p.mu.Lock()
		p.sentKeys[pk] = true
		p.sentContentKeys[ck] = true
		p.didStreamFlag = true
		p.mu.Unlock()
	}()
}

func (p *BlockReplyPipelineFull) bufferPayload(payload ReplyPayload) bool {
	if p.cfg.Buffer == nil {
		return false
	}
	p.cfg.Buffer.OnEnqueue(payload)
	if !p.cfg.Buffer.ShouldBuffer(payload) {
		return false
	}
	pk := payloadKey(payload)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seenKeys[pk] || p.sentKeys[pk] || p.pendingKeys[pk] || p.bufferedPayloadKs[pk] {
		return true
	}
	p.seenKeys[pk] = true
	p.bufferedPayloadKs[pk] = true
	p.bufferedPayloads = append(p.bufferedPayloads, payload)
	return true
}

func (p *BlockReplyPipelineFull) flushBuffered() {
	p.mu.Lock()
	if len(p.bufferedPayloads) == 0 {
		p.mu.Unlock()
		return
	}
	payloads := make([]ReplyPayload, len(p.bufferedPayloads))
	copy(payloads, p.bufferedPayloads)
	p.bufferedPayloads = p.bufferedPayloads[:0]
	p.bufferedPayloadKs = make(map[string]bool)
	p.mu.Unlock()

	for _, payload := range payloads {
		finalPayload := payload
		if p.cfg.Buffer != nil {
			finalPayload = p.cfg.Buffer.Finalize(payload)
		}
		p.sendPayload(finalPayload, true)
	}
}

// Enqueue adds a payload to the pipeline for delivery.
func (p *BlockReplyPipelineFull) Enqueue(payload ReplyPayload) {
	p.mu.Lock()
	if p.aborted {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if p.bufferPayload(payload) {
		return
	}

	hasMedia := payload.MediaURL != "" || len(payload.MediaURLs) > 0
	if hasMedia {
		if p.coalescer != nil {
			p.coalescer.Flush(true)
		}
		p.sendPayload(payload, false)
		return
	}

	if p.coalescer != nil {
		pk := payloadKey(payload)
		p.mu.Lock()
		if p.seenKeys[pk] || p.pendingKeys[pk] || p.bufferedKeys[pk] {
			p.mu.Unlock()
			return
		}
		p.seenKeys[pk] = true
		p.bufferedKeys[pk] = true
		p.mu.Unlock()
		p.coalescer.Enqueue(payload)
		return
	}

	p.sendPayload(payload, false)
}

// FlushAndWait flushes any buffered content and waits for all pending sends.
func (p *BlockReplyPipelineFull) FlushAndWait(force bool) {
	if p.coalescer != nil {
		p.coalescer.Flush(force)
	}
	p.flushBuffered()
	p.wg.Wait()
}

// Stop cancels timers and prevents further sends.
func (p *BlockReplyPipelineFull) Stop() {
	if p.coalescer != nil {
		p.coalescer.Stop()
	}
}

// HasBuffered returns true if there is buffered content.
func (p *BlockReplyPipelineFull) HasBuffered() bool {
	p.mu.Lock()
	hasBufferedPayloads := len(p.bufferedPayloads) > 0
	p.mu.Unlock()
	if hasBufferedPayloads {
		return true
	}
	if p.coalescer != nil {
		return p.coalescer.HasBuffered()
	}
	return false
}

// DidStream returns true if any payload was successfully delivered.
func (p *BlockReplyPipelineFull) DidStream() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.didStreamFlag
}

// IsAborted returns true if the pipeline was aborted due to timeout.
func (p *BlockReplyPipelineFull) IsAborted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aborted
}

// DroppedAfterAbort returns the number of payloads dropped after abort.
func (p *BlockReplyPipelineFull) DroppedAfterAbort() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.droppedCount
}

// HasSentPayload checks if a payload with the same content was already sent.
func (p *BlockReplyPipelineFull) HasSentPayload(payload ReplyPayload) bool {
	ck := contentKey(payload)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sentContentKeys[ck]
}
