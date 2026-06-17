// Package calprop stores calendar-event PROPOSALS — schedule-worthy items
// (meetings, important deadlines) that mail analysis surfaced from a message,
// pending the operator's accept/reject. Accepting one creates a real local
// calendar event; the proposal itself is never auto-added (능동적이되 침해적이지
// 않게: it only shows up as a bell badge on the calendar). Persisted to
// {stateDir}/calendar_proposals.json, mirroring the localtodo store.
package calprop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Status is the lifecycle of a proposal.
type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusRejected Status = "rejected"
)

// Proposal is one suggested calendar event awaiting the operator's decision.
type Proposal struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Start  string `json:"start"` // RFC3339 (timed) or "2006-01-02" (all-day)
	AllDay bool   `json:"allDay"`
	Kind   string `json:"kind"` // "meeting" | "deadline"

	// Source is a stable provenance/dedup key (e.g. "mail:<msgID>|<title>"),
	// so re-analysis of the same message never piles up duplicate proposals.
	Source        string   `json:"source,omitempty"`
	SourceSubject string   `json:"sourceSubject,omitempty"`
	SourceFrom    string   `json:"sourceFrom,omitempty"`
	Docs          []string `json:"docs,omitempty"` // originating mail's document attachments

	Status          Status `json:"status"`
	CalendarEventID string `json:"calendarEventId,omitempty"` // set when accepted
	CreatedAtMs     int64  `json:"createdAtMs"`
	DecidedAtMs     int64  `json:"decidedAtMs,omitempty"`
}

// CreateInput is the data for a new proposal. Source enables dedup.
type CreateInput struct {
	Title         string
	Start         string
	AllDay        bool
	Kind          string
	Source        string
	SourceSubject string
	SourceFrom    string
	Docs          []string
}

// Store is a JSON-file-backed proposal store. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
}

type fileModel struct {
	Proposals []Proposal `json:"proposals"`
}

// Default returns the process-wide store at {stateDir}/calendar_proposals.json.
func Default() (*Store, error) {
	return New(filepath.Join(config.ResolveStateDir(), "calendar_proposals.json"))
}

// New opens (or lazily creates) a store at path.
func New(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("calprop: empty path")
	}
	return &Store{path: path}, nil
}

func (s *Store) loadLocked() (*fileModel, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &fileModel{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("calprop: read %s: %w", s.path, err)
	}
	var m fileModel
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("calprop: parse %s: %w", s.path, err)
		}
	}
	return &m, nil
}

func (s *Store) saveLocked(m *fileModel) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("calprop: marshal: %w", err)
	}
	if err := atomicfile.WriteFile(s.path, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		return fmt.Errorf("calprop: write %s: %w", s.path, err)
	}
	return nil
}

// newID returns a unique proposal id.
func newID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return "cp_" + hex.EncodeToString(b[:])
}

// nowMs is overridable in tests; production uses the wall clock.
var nowMs = func() int64 { return time.Now().UnixMilli() }

// terminalRetention bounds how long decided (accepted/rejected) proposals are
// kept. They linger so a rejected item isn't re-proposed and an accepted one
// isn't re-suggested, but a months-old mail is never re-analyzed (LMTP dedups by
// Message-ID), so old terminal records are safe to drop — keeping the file (and
// the per-create dedup scan) bounded. Pending proposals are never pruned.
const terminalRetention = 90 * 24 * time.Hour

// pruneTerminalLocked drops decided proposals older than terminalRetention,
// in place. Returns whether anything was removed.
func pruneTerminalLocked(m *fileModel) bool {
	cutoff := nowMs() - terminalRetention.Milliseconds()
	kept := m.Proposals[:0]
	changed := false
	for _, p := range m.Proposals {
		if p.Status != StatusPending && p.DecidedAtMs > 0 && p.DecidedAtMs < cutoff {
			changed = true
			continue
		}
		kept = append(kept, p)
	}
	m.Proposals = kept
	return changed
}

// CreateIfAbsent inserts a pending proposal unless one with the same non-empty
// Source already exists (in any status — a rejected proposal stays rejected and
// is not re-proposed). Returns the proposal and whether it was newly created.
func (s *Store) CreateIfAbsent(in CreateInput) (Proposal, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return Proposal{}, false, err
	}
	pruned := pruneTerminalLocked(m)
	if in.Source != "" {
		for _, p := range m.Proposals {
			if p.Source == in.Source {
				if pruned {
					// Persist the prune even though we're not adding a proposal.
					_ = s.saveLocked(m)
				}
				return p, false, nil
			}
		}
	}
	p := Proposal{
		ID:            newID(),
		Title:         in.Title,
		Start:         in.Start,
		AllDay:        in.AllDay,
		Kind:          in.Kind,
		Source:        in.Source,
		SourceSubject: in.SourceSubject,
		SourceFrom:    in.SourceFrom,
		Docs:          in.Docs,
		Status:        StatusPending,
		CreatedAtMs:   nowMs(),
	}
	m.Proposals = append(m.Proposals, p)
	if err := s.saveLocked(m); err != nil {
		return Proposal{}, false, err
	}
	return p, true, nil
}

// ListPending returns pending proposals, soonest event first.
func (s *Store) ListPending() ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	var out []Proposal
	for _, p := range m.Proposals {
		if p.Status == StatusPending {
			out = append(out, p)
		}
	}
	sortByStart(out)
	return out, nil
}

// Get returns the proposal with id, or nil if absent.
func (s *Store) Get(id string) (*Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for i := range m.Proposals {
		if m.Proposals[i].ID == id {
			p := m.Proposals[i]
			return &p, nil
		}
	}
	return nil, nil
}

// ClaimForAccept atomically transitions a pending proposal to accepted so that
// only one of several concurrent accepts (e.g. a fast double-tap on 수락) can win
// the right to create the event. Returns (proposal, true) on a successful claim:
// the caller then creates the calendar event and calls Decide(StatusAccepted,
// eventID) to attach its id — or Decide(StatusPending, "") to release the claim
// if creation fails, leaving it retryable. Returns (proposal, false) when it was
// already decided (the caller must NOT create an event, or it would duplicate),
// or (nil, false) when id is unknown.
func (s *Store) ClaimForAccept(id string) (*Proposal, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return nil, false, err
	}
	for i := range m.Proposals {
		if m.Proposals[i].ID != id {
			continue
		}
		if m.Proposals[i].Status != StatusPending {
			p := m.Proposals[i]
			return &p, false, nil
		}
		m.Proposals[i].Status = StatusAccepted
		m.Proposals[i].DecidedAtMs = nowMs()
		if err := s.saveLocked(m); err != nil {
			return nil, false, err
		}
		p := m.Proposals[i]
		return &p, true, nil
	}
	return nil, false, nil
}

// Decide sets a proposal's status (and the created calendar event id on accept).
// Passing StatusPending with an empty event id reopens a proposal — used to
// release a ClaimForAccept when event creation fails — and clears DecidedAtMs so
// the "pending ⇒ no decision time" invariant holds. Returns the updated proposal,
// or nil if id is unknown.
func (s *Store) Decide(id string, status Status, calendarEventID string) (*Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for i := range m.Proposals {
		if m.Proposals[i].ID != id {
			continue
		}
		m.Proposals[i].Status = status
		m.Proposals[i].CalendarEventID = calendarEventID
		if status == StatusPending {
			m.Proposals[i].DecidedAtMs = 0 // reopened: no decision time
		} else {
			m.Proposals[i].DecidedAtMs = nowMs()
		}
		if err := s.saveLocked(m); err != nil {
			return nil, err
		}
		p := m.Proposals[i]
		return &p, nil
	}
	return nil, nil
}

func sortByStart(ps []Proposal) {
	// simple insertion sort by Start string (RFC3339 and date sort lexically)
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j].Start < ps[j-1].Start; j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
}
