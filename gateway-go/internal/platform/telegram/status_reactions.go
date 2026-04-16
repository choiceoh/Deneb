// Status reaction controller for Telegram agent status display via emoji reactions.
// Features promise-chain serialization, debouncing, stall timers, and terminal
// state protection.
package telegram

import (
	"strings"
	"sync"
	"time"
)

// StatusReactionEmojis configures the emoji for each agent phase.
type StatusReactionEmojis struct {
	Queued     string
	Thinking   string
	Tool       string
	Coding     string
	Web        string
	Done       string
	Error      string
	StallSoft  string
	StallHard  string
	Compacting string
}

// DefaultStatusEmojis returns the default emoji set.
func DefaultStatusEmojis() StatusReactionEmojis {
	return StatusReactionEmojis{
		Queued:     "👀",
		Thinking:   "🤔",
		Tool:       "🔥",
		Coding:     "👨‍💻",
		Web:        "⚡",
		Done:       "👍",
		Error:      "😱",
		StallSoft:  "🥱",
		StallHard:  "😨",
		Compacting: "✍",
	}
}

// StatusReactionTiming configures debounce and stall intervals.
type StatusReactionTiming struct {
	DebounceMs  int // Intermediate state debounce (default 700).
	StallSoftMs int // Soft stall warning (default 10000).
	StallHardMs int // Hard stall warning (default 30000).
	DoneHoldMs  int // How long to show done emoji (default 1500, informational).
	ErrorHoldMs int // How long to show error emoji (default 2500, informational).
}

// DefaultStatusTiming returns the default timing configuration.
func DefaultStatusTiming() StatusReactionTiming {
	return StatusReactionTiming{
		DebounceMs:  700,
		StallSoftMs: 10_000,
		StallHardMs: 30_000,
		DoneHoldMs:  1500,
		ErrorHoldMs: 2500,
	}
}

// Tool name tokens for emoji resolution.
var codingToolTokens = []string{
	"exec", "process", "read", "write", "edit", "bash",
}

var webToolTokens = []string{
	"web", "web_search", "web-search", "web_fetch", "web-fetch", "browser",
}

// ResolveToolEmoji returns the appropriate emoji for a tool invocation.
func ResolveToolEmoji(toolName string, emojis StatusReactionEmojis) string {
	normalized := strings.TrimSpace(strings.ToLower(toolName))
	if normalized == "" {
		return emojis.Tool
	}
	for _, token := range webToolTokens {
		if strings.Contains(normalized, token) {
			return emojis.Web
		}
	}
	for _, token := range codingToolTokens {
		if strings.Contains(normalized, token) {
			return emojis.Coding
		}
	}
	return emojis.Tool
}

// StatusReactionControllerParams configures a new StatusReactionController.
type StatusReactionControllerParams struct {
	Enabled      bool
	SetReaction  func(emoji string) error // sets/replaces the current reaction emoji
	InitialEmoji string
	Emojis       *StatusReactionEmojis
	Timing       *StatusReactionTiming
	OnError      func(err error)
}

// StatusReactionController manages agent status display via message reactions.
type StatusReactionController struct {
	mu             sync.Mutex
	enabled        bool
	setReaction    func(emoji string) error
	emojis         StatusReactionEmojis
	timing         StatusReactionTiming
	onError        func(err error)
	currentEmoji   string
	pendingEmoji   string
	finished       bool
	debounceTimer  *time.Timer
	stallSoftTimer *time.Timer
	stallHardTimer *time.Timer
	knownEmojis    map[string]struct{}

	// Serialization: operations queue through this channel.
	opCh chan func()
	done chan struct{}
}

// NewStatusReactionController creates a new status reaction controller.
func NewStatusReactionController(params StatusReactionControllerParams) *StatusReactionController {
	emojis := DefaultStatusEmojis()
	if params.Emojis != nil {
		emojis = mergeEmojis(emojis, *params.Emojis)
	}
	// queued defaults to initialEmoji.
	if params.InitialEmoji != "" {
		emojis.Queued = params.InitialEmoji
	}

	timing := DefaultStatusTiming()
	if params.Timing != nil {
		timing = mergeTiming(timing, *params.Timing)
	}

	known := make(map[string]struct{})
	for _, e := range []string{
		params.InitialEmoji, emojis.Queued, emojis.Thinking, emojis.Tool,
		emojis.Coding, emojis.Web, emojis.Done, emojis.Error,
		emojis.StallSoft, emojis.StallHard, emojis.Compacting,
	} {
		if e != "" {
			known[e] = struct{}{}
		}
	}

	c := &StatusReactionController{
		enabled:     params.Enabled,
		setReaction: params.SetReaction,
		emojis:      emojis,
		timing:      timing,
		onError:     params.OnError,
		knownEmojis: known,
		opCh:        make(chan func(), 64),
		done:        make(chan struct{}),
	}

	// Start the serialization goroutine.
	go c.runLoop()

	return c
}

func (c *StatusReactionController) runLoop() {
	for {
		select {
		case <-c.done:
			return
		case op := <-c.opCh:
			op()
		}
	}
}

// enqueue serializes an async operation.
func (c *StatusReactionController) enqueue(fn func()) {
	select {
	case c.opCh <- fn:
	case <-c.done:
	}
}

// applyEmoji sets a new reaction, atomically replacing the previous one.
// Telegram's setMessageReaction replaces the entire reaction list in a single
// API call, so [old] → [new] happens with no visible gap. No separate remove
// call is needed.
func (c *StatusReactionController) applyEmoji(newEmoji string) {
	if !c.enabled {
		return
	}
	if err := c.setReaction(newEmoji); err != nil {
		if c.onError != nil {
			c.onError(err)
		}
		return
	}
	c.currentEmoji = newEmoji
}

// clearAllTimers stops all pending timers.
func (c *StatusReactionController) clearAllTimers() {
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
		c.debounceTimer = nil
	}
	if c.stallSoftTimer != nil {
		c.stallSoftTimer.Stop()
		c.stallSoftTimer = nil
	}
	if c.stallHardTimer != nil {
		c.stallHardTimer.Stop()
		c.stallHardTimer = nil
	}
}

// resetStallTimers resets stall detection timers.
func (c *StatusReactionController) resetStallTimers() {
	if c.stallSoftTimer != nil {
		c.stallSoftTimer.Stop()
	}
	if c.stallHardTimer != nil {
		c.stallHardTimer.Stop()
	}
	c.stallSoftTimer = time.AfterFunc(
		time.Duration(c.timing.StallSoftMs)*time.Millisecond,
		func() { c.scheduleEmoji(c.emojis.StallSoft, true, true) },
	)
	c.stallHardTimer = time.AfterFunc(
		time.Duration(c.timing.StallHardMs)*time.Millisecond,
		func() { c.scheduleEmoji(c.emojis.StallHard, true, true) },
	)
}

// scheduleEmoji schedules an emoji change (debounced or immediate).
func (c *StatusReactionController) scheduleEmoji(emoji string, immediate, skipStallReset bool) {
	c.mu.Lock()
	if !c.enabled || c.finished {
		c.mu.Unlock()
		return
	}

	// Deduplicate.
	if emoji == c.currentEmoji || emoji == c.pendingEmoji {
		if !skipStallReset {
			c.resetStallTimers()
		}
		c.mu.Unlock()
		return
	}

	c.pendingEmoji = emoji
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
		c.debounceTimer = nil
	}

	// Reset stall timers under the lock to prevent concurrent access
	// from parallel scheduleEmoji calls racing on timer fields.
	if !skipStallReset {
		c.resetStallTimers()
	}

	if immediate {
		c.mu.Unlock()
		e := emoji
		c.enqueue(func() {
			c.applyEmoji(e)
			c.mu.Lock()
			c.pendingEmoji = ""
			c.mu.Unlock()
		})
	} else {
		c.debounceTimer = time.AfterFunc(
			time.Duration(c.timing.DebounceMs)*time.Millisecond,
			func() {
				c.mu.Lock()
				e := c.pendingEmoji
				c.mu.Unlock()
				if e != "" {
					c.enqueue(func() {
						c.applyEmoji(e)
						c.mu.Lock()
						c.pendingEmoji = ""
						c.mu.Unlock()
					})
				}
			},
		)
		c.mu.Unlock()
	}
}

// SetQueued sets the queued reaction (immediate).
func (c *StatusReactionController) SetQueued() {
	c.scheduleEmoji(c.emojis.Queued, true, false)
}

// SetThinking sets the thinking reaction (debounced).
func (c *StatusReactionController) SetThinking() {
	c.scheduleEmoji(c.emojis.Thinking, false, false)
}

// SetTool sets the tool reaction based on the tool name (debounced).
func (c *StatusReactionController) SetTool(toolName string) {
	emoji := ResolveToolEmoji(toolName, c.emojis)
	c.scheduleEmoji(emoji, false, false)
}

// SetDone sets the done reaction (terminal state).
func (c *StatusReactionController) SetDone() {
	c.finishWithEmoji(c.emojis.Done)
}

// SetCompacting sets the compacting reaction (immediate, non-terminal).
// Shown during STW compaction while context is being summarized.
func (c *StatusReactionController) SetCompacting() {
	c.scheduleEmoji(c.emojis.Compacting, true, false)
}

// SetError sets the error reaction (terminal state).
func (c *StatusReactionController) SetError() {
	c.finishWithEmoji(c.emojis.Error)
}

// SetClear removes any reaction emoji from the message (terminal state).
// Used when a run is cancelled in a way that doesn't represent an error
// to the user — for example, a quick-fire merge where the user typed a
// follow-up message and the original run was superseded. Showing "😱
// error" in that case would mislead the user.
func (c *StatusReactionController) SetClear() {
	c.finishWithEmoji("")
}

func (c *StatusReactionController) finishWithEmoji(emoji string) {
	c.mu.Lock()
	if !c.enabled {
		c.mu.Unlock()
		return
	}
	c.finished = true
	c.clearAllTimers()
	c.mu.Unlock()

	c.enqueue(func() {
		c.applyEmoji(emoji)
		c.mu.Lock()
		c.pendingEmoji = ""
		c.mu.Unlock()
	})
}

// CloseAfterDrain waits for all enqueued operations (including the terminal
// emoji from SetDone/SetError) to complete before stopping the run loop.
// Times out after 15 seconds to avoid blocking indefinitely.
func (c *StatusReactionController) CloseAfterDrain() {
	drained := make(chan struct{})
	c.enqueue(func() {
		close(drained)
	})
	select {
	case <-drained:
	case <-time.After(15 * time.Second):
	}
	c.Close()
}

// Close stops the controller's serialization goroutine.
func (c *StatusReactionController) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}

// --- helpers ---

func mergeEmojis(base, override StatusReactionEmojis) StatusReactionEmojis {
	if override.Queued != "" {
		base.Queued = override.Queued
	}
	if override.Thinking != "" {
		base.Thinking = override.Thinking
	}
	if override.Tool != "" {
		base.Tool = override.Tool
	}
	if override.Coding != "" {
		base.Coding = override.Coding
	}
	if override.Web != "" {
		base.Web = override.Web
	}
	if override.Done != "" {
		base.Done = override.Done
	}
	if override.Error != "" {
		base.Error = override.Error
	}
	if override.StallSoft != "" {
		base.StallSoft = override.StallSoft
	}
	if override.StallHard != "" {
		base.StallHard = override.StallHard
	}
	if override.Compacting != "" {
		base.Compacting = override.Compacting
	}
	return base
}

func mergeTiming(base, override StatusReactionTiming) StatusReactionTiming {
	if override.DebounceMs > 0 {
		base.DebounceMs = override.DebounceMs
	}
	if override.StallSoftMs > 0 {
		base.StallSoftMs = override.StallSoftMs
	}
	if override.StallHardMs > 0 {
		base.StallHardMs = override.StallHardMs
	}
	if override.DoneHoldMs > 0 {
		base.DoneHoldMs = override.DoneHoldMs
	}
	if override.ErrorHoldMs > 0 {
		base.ErrorHoldMs = override.ErrorHoldMs
	}
	return base
}
