// Package wizard manages interactive wizard/setup flows as state machines.
//
// This ports the TypeScript wizard system (src/gateway/server-methods/admin/wizard.ts)
// to Go, providing session-based wizard lifecycle management.
package wizard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// SessionStatus represents the lifecycle state of a wizard session.
type SessionStatus string

const (
	StatusRunning   SessionStatus = "running"
	StatusDone      SessionStatus = "done"
	StatusCancelled SessionStatus = "cancelled"
	StatusFailed    SessionStatus = "failed"
)

// Session represents an active wizard session.
type Session struct {
	SessionID string        `json:"sessionId"`
	Mode      string        `json:"mode"`
	Workspace string        `json:"workspace,omitempty"`
	Status    SessionStatus `json:"status"`
	Done      bool          `json:"done"`
	Value     any           `json:"value,omitempty"`
	Error     string        `json:"error,omitempty"`
	StepID    string        `json:"stepId,omitempty"`
	Prompt    any           `json:"prompt,omitempty"`
	Steps     []Step        `json:"steps,omitempty"`
	StepIndex int           `json:"stepIndex"`
}

// Step defines a single wizard step with an ID and optional prompt.
type Step struct {
	ID     string `json:"id"`
	Prompt any    `json:"prompt,omitempty"`
}

// Answer holds the user's response to a wizard step.
type Answer struct {
	StepID string `json:"stepId,omitempty"`
	Value  any    `json:"value,omitempty"`
}

// Engine manages wizard sessions.
type Engine struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewEngine creates a new wizard engine.
func NewEngine() *Engine {
	return &Engine{
		sessions: make(map[string]*Session),
	}
}

// Start begins a new wizard session in the given mode (single-step).
func (e *Engine) Start(mode, workspace string) *Session {
	return e.StartWithSteps(mode, workspace, nil)
}

// StartWithSteps begins a new wizard session with predefined steps.
// If steps is nil or empty, the session behaves as single-step (backward compatible).
func (e *Engine) StartWithSteps(mode, workspace string, steps []Step) *Session {
	id := genSessionID()
	sess := &Session{
		SessionID: id,
		Mode:      mode,
		Workspace: workspace,
		Status:    StatusRunning,
		Done:      false,
		Steps:     steps,
	}
	if len(steps) > 0 {
		sess.StepID = steps[0].ID
		sess.Prompt = steps[0].Prompt
	}

	e.mu.Lock()
	e.sessions[id] = sess
	e.mu.Unlock()

	cp := *sess
	return &cp
}

// Next advances a wizard session with the given answer.
func (e *Engine) Next(sessionID string, answer *Answer) (*Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sess, ok := e.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("wizard session %q not found", sessionID)
	}
	if sess.Status != StatusRunning {
		return nil, fmt.Errorf("wizard session %q is %s", sessionID, sess.Status)
	}

	if answer != nil {
		sess.Value = answer.Value
	}

	// Multi-step: advance to the next step if more remain.
	if len(sess.Steps) > 0 && sess.StepIndex < len(sess.Steps)-1 {
		sess.StepIndex++
		sess.StepID = sess.Steps[sess.StepIndex].ID
		sess.Prompt = sess.Steps[sess.StepIndex].Prompt
	} else {
		// Final step (or single-step session): mark done.
		sess.Done = true
		sess.Status = StatusDone
	}

	cp := *sess
	return &cp, nil
}

// Cancel cancels an active wizard session.
func (e *Engine) Cancel(sessionID string) (*Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sess, ok := e.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("wizard session %q not found", sessionID)
	}
	if sess.Status != StatusRunning {
		cp := *sess
		return &cp, nil
	}
	sess.Status = StatusCancelled
	sess.Done = true

	cp := *sess
	return &cp, nil
}

// GetStatus returns the current status of a wizard session.
func (e *Engine) GetStatus(sessionID string) (*Session, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sess, ok := e.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("wizard session %q not found", sessionID)
	}
	cp := *sess
	return &cp, nil
}

func genSessionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "wiz-" + hex.EncodeToString(b)
}
