package server

// auto_resume: resumes agent runs that were interrupted by a gateway crash
// or restart. Driven by persistent "run markers" under <denebDir>/run_markers/.
//
// Marker lifecycle (see internal/runtime/session/run_marker.go):
//   - PhaseStart  → Write(marker). initRunMarkerLifecycle wires this on the
//                   session EventBus.
//   - Terminal    → Delete(marker). Same listener.
//   - Gateway crash → marker survives. autoResumeInterruptedRuns picks it up
//                   next startup.
//
// Resume action: when a marker survives AND the session transcript does not
// end in a "logically done" state, we inject a new user-role message on the
// transcript that instructs the agent to pick up where it left off. No
// existing transcript content is modified — the prompt cache is preserved
// (see .claude/rules/prompt-cache.md, Rule A).
//
// Safety gates:
//   - Max age (resumeMaxAge, default 2h): older markers are cleared without
//     resuming, so a weeks-old crash does not suddenly revive a stale task.
//   - Max attempts (resumeMaxAttempts, default 1): if the resume itself
//     crashes the gateway, the counter stops us from looping forever.
//   - Config opt-out (deneb.json → session.autoResume false): disables the
//     whole subsystem.
//   - Per-marker one-shot: every marker scanned at boot is either consumed
//     (resume fires) or cleared. Nothing re-fires on subsequent boots.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Hard-coded defaults. Keep these inside the file so the policy is explicit
// and reviewable in one place. Config only toggles the on/off switch for
// now — more knobs can graduate if real-world use motivates them.
const (
	// resumeMaxAge is the oldest marker we will act on. Markers older than
	// this are deleted on restart with no action taken. 2 hours roughly
	// matches Anthropic's ephemeral cache TTL (5 min) * safety factor for
	// typical dev sessions — anything older is likely stale.
	resumeMaxAge = 2 * time.Hour

	// resumeMaxAttempts bounds how many times the same marker may trigger
	// a resume. Each attempt bumps marker.ResumeAttempts so a resume that
	// itself crashes does not loop.
	resumeMaxAttempts = 1

	// resumeDispatchDelay gives the channel plugins (Telegram in particular)
	// time to finish connecting before we inject the resume message.
	// Otherwise the first run would start with replyFunc == nil and fail
	// delivery immediately.
	resumeDispatchDelay = 3 * time.Second

	// resumeSystemNote is the Korean-first synthetic user message injected
	// into the session. "[SYSTEM:]" framing makes it clear to the agent
	// that this is an infrastructural resume, not a real user turn, and
	// matches the existing persistInterruptedContext style used elsewhere
	// in the chat pipeline.
	resumeSystemNote = "[SYSTEM: 이전 대화가 시스템 재시작으로 중단됐습니다. " +
		"필요하면 마지막 작업을 이어서 완료하거나, 상황을 간단히 정리해 주세요.]"
)

// runMarkerStore returns the server's marker store, lazily constructing
// it on first use. The store lives under <denebDir>/run_markers/.
func (s *Server) runMarkerStore() *session.RunMarkerStore {
	s.resumeMu.Lock()
	defer s.resumeMu.Unlock()
	if s.markerStore == nil {
		base := filepath.Join(s.denebDir, "run_markers")
		s.markerStore = session.NewRunMarkerStore(base)
	}
	return s.markerStore
}

// initRunMarkerLifecycle subscribes to session lifecycle events so the
// marker store mirrors the in-memory state machine. Returns an unsubscribe
// function that the caller stores for shutdown cleanup.
//
// Only KindDirect sessions get markers — transient kinds (cron, subagent)
// are considered ephemeral. Cron retries on its own schedule; subagents
// are orphan-reaped by tasks.StartMaintenanceLoop.
func (s *Server) initRunMarkerLifecycle() func() {
	if s.sessions == nil {
		return func() {}
	}
	store := s.runMarkerStore()
	logger := s.logger
	return s.sessions.EventBusRef().Subscribe(func(e session.Event) {
		switch e.Kind {
		case session.EventStatusChanged:
			switch {
			case e.NewStatus == session.StatusRunning:
				// Guard: only persistent direct sessions get markers.
				sess := s.sessions.Get(e.Key)
				if sess == nil || sess.Kind != session.KindDirect {
					return
				}
				now := time.Now().UnixMilli()
				m := session.RunMarker{
					SessionKey:     e.Key,
					StartedAt:      now,
					LastActivityAt: now,
					Channel:        sess.Channel,
				}
				if err := store.Write(m); err != nil {
					logger.Warn("failed to write run marker",
						"session", e.Key, "error", err)
				}
			case session.IsTerminal(e.NewStatus):
				if err := store.Delete(e.Key); err != nil {
					logger.Warn("failed to delete run marker",
						"session", e.Key, "error", err)
				}
			}
		case session.EventDeleted:
			if err := store.Delete(e.Key); err != nil {
				logger.Warn("failed to delete run marker on session delete",
					"session", e.Key, "error", err)
			}
		case session.EventCreated:
			// Session creation emits no marker — markers track running
			// state only, written on EventStatusChanged → running.
		}
	})
}

// transcriptTailShape classifies what the last messages in a transcript
// tell us about where the interrupted run stopped.
type transcriptTailShape int

const (
	tailEmpty               transcriptTailShape = iota // no user messages yet
	tailEndUserText                                    // last line = user text; LLM never replied
	tailEndAssistantText                               // last line = assistant text only; run finished cleanly
	tailEndAssistantToolUse                            // assistant emitted tool_use but no matching tool_result
	tailEndToolResult                                  // tool_result written but next assistant turn never ran
	tailUnknown                                        // unrecognized shape
)

// isLogicallyDone returns true for tail shapes that mean "run finished"
// or "cannot be safely resumed." Interrupted-mid-turn shapes return false —
// those are the ones we resume. tailUnknown is treated as done so
// ambiguous tails never trigger a speculative resume.
func (t transcriptTailShape) isLogicallyDone() bool {
	return t == tailEmpty || t == tailEndAssistantText || t == tailUnknown
}

// analyzeTranscriptTail inspects the last few non-header lines of a
// transcript JSONL file and returns its tail shape.
//
// Robustness: the file may have trailing junk from a kill-9, so we decode
// line-by-line and use the last successfully-decoded message as the tail.
// Corrupt/unparseable tails are classified as tailUnknown — we do NOT
// resume them, to avoid acting on ambiguous state.
func analyzeTranscriptTail(path string) (transcriptTailShape, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tailEmpty, nil
		}
		return tailUnknown, err
	}
	lines := bytes.Split(data, []byte{'\n'})
	var lastRole string
	var lastBlockTypes []string
	var lastIsStringContent bool
	sawAnyMessage := false

	// Minimal envelope — we only care about role and the shape of content.
	type msg struct {
		Type    string          `json:"type,omitempty"`
		Role    string          `json:"role,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}

	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m msg
		if err := json.Unmarshal(line, &m); err != nil {
			// Tolerate a single partial write at the tail; older lines
			// should still decode. Keep scanning but do not update the
			// "last message" state for this line.
			continue
		}
		// Session header line (first line only).
		if m.Type == "session" && m.Role == "" {
			continue
		}
		if m.Role == "" {
			continue
		}
		sawAnyMessage = true
		lastRole = m.Role
		lastBlockTypes = lastBlockTypes[:0]
		lastIsStringContent = false

		// Content is either a JSON string (legacy text-only) or a
		// ContentBlock array. Distinguish so we can tell a plain user
		// message apart from a user tool_result.
		if len(m.Content) > 0 {
			trimmed := bytes.TrimSpace(m.Content)
			if len(trimmed) > 0 && trimmed[0] == '"' {
				lastIsStringContent = true
			} else if len(trimmed) > 0 && trimmed[0] == '[' {
				var blocks []struct {
					Type string `json:"type"`
				}
				if jerr := json.Unmarshal(m.Content, &blocks); jerr == nil {
					for _, b := range blocks {
						if b.Type != "" {
							lastBlockTypes = append(lastBlockTypes, b.Type)
						}
					}
				}
			}
		}
	}

	if !sawAnyMessage {
		return tailEmpty, nil
	}

	switch lastRole {
	case "user":
		// String content = plain user text. Block content containing
		// tool_result = the tool executed, but the next assistant turn
		// never started.
		if lastIsStringContent {
			return tailEndUserText, nil
		}
		for _, t := range lastBlockTypes {
			if t == "tool_result" {
				return tailEndToolResult, nil
			}
		}
		return tailEndUserText, nil
	case "assistant":
		// Assistant with tool_use but no matching tool_result next
		// means we crashed between "LLM decided to call tool" and
		// "tool finished". Plain text (no tool_use) means the final
		// assistant turn was persisted cleanly.
		hasToolUse := false
		for _, t := range lastBlockTypes {
			if t == "tool_use" {
				hasToolUse = true
				break
			}
		}
		if hasToolUse {
			return tailEndAssistantToolUse, nil
		}
		return tailEndAssistantText, nil
	default:
		return tailUnknown, nil
	}
}

// autoResumeOptions mirrors configurable knobs. Future knobs can thread
// through here without ballooning the function signature.
type autoResumeOptions struct {
	Enabled     bool
	MaxAge      time.Duration
	MaxAttempts int
	Now         func() time.Time
	// DispatchFn is the async resume sender. Defaulted to chatHandler.Send
	// in production; swappable in tests.
	DispatchFn func(ctx context.Context, sessionKey, channel, chatID string) error
}

// autoResumeEnabled returns the opt-out flag from deneb.json. Default = true.
// Missing config file or missing field = enabled. Any explicit false = disabled.
func autoResumeEnabled() bool {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil || snap.Config.Session == nil {
		return true
	}
	if snap.Config.Session.AutoResume != nil {
		return *snap.Config.Session.AutoResume
	}
	return true
}

// autoResumeInterruptedRuns is the post-restore hook: scans run markers,
// deletes stale ones, and re-enqueues fresh ones as chat.send requests.
//
// Ordering note: restoreAndWakeSessions runs BEFORE this (from the same
// lifecycle hook) so the sessions referenced by markers are already in
// the in-memory manager by the time we scan.
func (s *Server) autoResumeInterruptedRuns(ctx context.Context) {
	opts := autoResumeOptions{
		Enabled:     autoResumeEnabled(),
		MaxAge:      resumeMaxAge,
		MaxAttempts: resumeMaxAttempts,
		Now:         time.Now,
		DispatchFn:  s.dispatchResumeMessage,
	}
	s.autoResumeInterruptedRunsWithOpts(ctx, opts)
}

// autoResumeInterruptedRunsWithOpts is the testable entry point.
func (s *Server) autoResumeInterruptedRunsWithOpts(ctx context.Context, opts autoResumeOptions) {
	logger := s.logger
	store := s.runMarkerStore()

	if !opts.Enabled {
		logger.Info("auto-resume disabled by config")
		// Still clear any stale markers so they do not accumulate forever.
		if markers, _ := store.List(); len(markers) > 0 {
			for _, m := range markers {
				_ = store.Delete(m.SessionKey)
			}
		}
		return
	}

	markers, err := store.List()
	if err != nil {
		// Non-fatal: corrupt markers were already skipped in List; log and continue.
		logger.Warn("auto-resume: errors listing markers", "error", err)
	}
	if len(markers) == 0 {
		return
	}

	transcriptDir := transcriptBaseDir()
	nowMs := opts.Now().UnixMilli()
	maxAgeMs := opts.MaxAge.Milliseconds()

	for _, m := range markers {
		// Per-marker decision: resume, discard-as-stale, or skip.
		age := nowMs - m.StartedAt
		if age > maxAgeMs {
			logger.Info("auto-resume: discarding stale marker",
				"session", m.SessionKey,
				"ageHours", time.Duration(age)*time.Millisecond/time.Hour)
			_ = store.Delete(m.SessionKey)
			continue
		}
		if m.ResumeAttempts >= opts.MaxAttempts {
			logger.Warn("auto-resume: attempt limit reached, discarding marker",
				"session", m.SessionKey,
				"attempts", m.ResumeAttempts)
			_ = store.Delete(m.SessionKey)
			continue
		}

		// Only Telegram direct sessions are resumable for now. Other
		// channels (cron, btw, subagent) have their own recovery paths.
		chatID, ok := parseTelegramChatID(m.SessionKey)
		if !ok {
			logger.Debug("auto-resume: non-telegram session, skipping",
				"session", m.SessionKey)
			_ = store.Delete(m.SessionKey)
			continue
		}

		// Classify the transcript tail. If the run had already finished
		// cleanly (last line = assistant text), the marker is stale —
		// probably from a race where the terminal event fired but the
		// delete did not flush to disk.
		transcriptPath := filepath.Join(transcriptDir, m.SessionKey+".jsonl")
		shape, tailErr := analyzeTranscriptTail(transcriptPath)
		if tailErr != nil {
			logger.Warn("auto-resume: transcript read failed",
				"session", m.SessionKey, "error", tailErr)
			_ = store.Delete(m.SessionKey)
			continue
		}
		if shape.isLogicallyDone() {
			logger.Info("auto-resume: transcript already done, dropping marker",
				"session", m.SessionKey, "tail", tailShapeString(shape))
			_ = store.Delete(m.SessionKey)
			continue
		}

		// Commit the decision: bump attempt counter BEFORE dispatch so a
		// crash during dispatch cannot loop.
		if _, incErr := store.IncrementResumeAttempts(m.SessionKey); incErr != nil {
			logger.Warn("auto-resume: failed to increment attempts, skipping",
				"session", m.SessionKey, "error", incErr)
			continue
		}

		logger.Info("auto-resume: scheduling resume",
			"session", m.SessionKey,
			"tail", tailShapeString(shape),
			"ageMinutes", time.Duration(age)*time.Millisecond/time.Minute)

		// Dispatch async so we do not block the startup path. Each dispatch
		// runs in its own safego to isolate panics.
		markerCopy := m
		chatIDCopy := chatID
		shapeCopy := shape
		safego.GoWithSlog(logger, "auto-resume-dispatch", func() {
			// Brief sleep to let telegram plugin finish starting.
			select {
			case <-ctx.Done():
				return
			case <-time.After(resumeDispatchDelay):
			}
			if err := opts.DispatchFn(ctx, markerCopy.SessionKey, markerCopy.Channel, chatIDCopy); err != nil {
				logger.Error("auto-resume: dispatch failed",
					"session", markerCopy.SessionKey,
					"tail", tailShapeString(shapeCopy),
					"error", err)
				return
			}
			logger.Info("auto-resume: dispatched",
				"session", markerCopy.SessionKey,
				"tail", tailShapeString(shapeCopy))
			// Marker is deleted by the normal terminal event on this new run
			// (via initRunMarkerLifecycle).
		})
	}
}

// dispatchResumeMessage builds a synthetic chat.send for the session and
// invokes the chat handler. Delivery context is reconstructed from the
// telegram session key; other channels would need their own reconstruction
// logic if resumable in the future.
func (s *Server) dispatchResumeMessage(ctx context.Context, sessionKey, channel, chatID string) error {
	if s.chatHandler == nil {
		return errors.New("chat handler not initialized")
	}
	params := map[string]any{
		"sessionKey":  sessionKey,
		"message":     resumeSystemNote,
		"clientRunId": shortid.New("resume"),
		"skipMerge":   true, // synthetic dispatch — do not collapse with real user input
	}
	if channel == "telegram" && chatID != "" {
		params["delivery"] = map[string]any{
			"channel": "telegram",
			"to":      chatID,
		}
	}
	req, err := protocol.NewRequestFrame(
		"auto-resume-"+sessionKey,
		"chat.send",
		params,
	)
	if err != nil {
		return err
	}

	sendCtx, cancel := context.WithTimeout(ctx, DefaultTurnDeadline)
	defer cancel()
	resp := s.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		if resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		return errors.New("chat.send returned !OK with no error detail")
	}
	return nil
}

// parseTelegramChatID extracts the numeric chat ID from a session key of
// the form "telegram:<id>". Sub-sessions ("telegram:<id>:<task>:<ts>")
// are rejected — they are ephemeral agent tasks, not main conversations.
func parseTelegramChatID(sessionKey string) (string, bool) {
	if !strings.HasPrefix(sessionKey, "telegram:") {
		return "", false
	}
	rest := strings.TrimPrefix(sessionKey, "telegram:")
	if strings.Contains(rest, ":") {
		return "", false
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

// transcriptBaseDir returns the directory where session transcripts live.
// Matches the path used by restoreAndWakeSessions so the two subsystems
// always agree on which files to inspect.
func transcriptBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "transcripts")
}

// tailShapeString renders the enum for structured logs.
func tailShapeString(s transcriptTailShape) string {
	switch s {
	case tailEmpty:
		return "empty"
	case tailEndUserText:
		return "end_user_text"
	case tailEndAssistantText:
		return "end_assistant_text"
	case tailEndAssistantToolUse:
		return "end_assistant_tool_use"
	case tailEndToolResult:
		return "end_tool_result"
	default:
		return "unknown"
	}
}
