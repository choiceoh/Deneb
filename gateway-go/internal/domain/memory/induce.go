package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Induced is a classified candidate plus its resolved route.
type Induced struct {
	Candidate Candidate
	Route     Route
}

// InduceFromTurn classifies the user message and resolves the write route.
// Returns nil when there is nothing to induce (empty message).
func InduceFromTurn(userMessage string) *Induced {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return nil
	}
	c := ClassifyHeuristics(msg)
	c.SubjectID = NormalizeSubject(c.SubjectID)
	// Profile cues are deliberately recall-friendly and may occur inside quoted
	// mail, translation, or code payloads. Only a top-level self-directed
	// assertion/correction/forget command may reach the canonical memory sink.
	// Third-party profile observations retain their propose-only ledger route.
	if c.Target == TargetProfile && c.SubjectID == SubjectSelf && !HasDirectProfileMutationIntent(msg) {
		c.Target = TargetEpisodic
		c.Forget = false
	}
	return &Induced{
		Candidate: c,
		Route:     RouteFor(c.Target, c.SubjectID),
	}
}

// ApplyOptions configures disk sinks for induction writeback.
type ApplyOptions struct {
	// WorkspaceDir holds MEMORY.md (required for RouteMemory).
	WorkspaceDir string
	// LedgerPath is the JSONL propose-only ledger (required for RouteLedger).
	LedgerPath string
	// SessionKey is recorded on ledger rows for audit.
	SessionKey string
	// Now overrides time.Now in tests.
	Now func() time.Time
	// MainSessionOnly: when true, RouteMemory is dropped unless SessionKey is
	// exactly client:main (sub-conversations and cron never touch MEMORY.md).
	MainSessionOnly bool
}

// ApplyResult summarizes what was written.
type ApplyResult struct {
	Route   Route
	Target  WriteTarget
	Wrote   bool
	Dropped string // reason when not written
}

// Apply persists an induced item according to its route. Diary-only and drop
// routes are no-ops (diary is owned by the chat recorder).
func Apply(ind *Induced, opts ApplyOptions) (ApplyResult, error) {
	if ind == nil {
		return ApplyResult{Dropped: "nil"}, nil
	}
	res := ApplyResult{Route: ind.Route, Target: ind.Candidate.Target}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	switch ind.Route {
	case RouteDrop, RouteDiaryOnly, RouteUnspecified:
		res.Dropped = string(ind.Route)
		return res, nil
	case RouteMemory:
		if opts.MainSessionOnly && opts.SessionKey != "client:main" {
			res.Dropped = "not_main_session"
			return res, nil
		}
		if strings.TrimSpace(opts.WorkspaceDir) == "" {
			res.Dropped = "no_workspace"
			return res, nil
		}
		if err := appendMemorySection(opts.WorkspaceDir, ind.Candidate, nowFn()); err != nil {
			return res, err
		}
		res.Wrote = true
		return res, nil
	case RouteLedger:
		if strings.TrimSpace(opts.LedgerPath) == "" {
			res.Dropped = "no_ledger"
			return res, nil
		}
		if err := appendLedger(opts.LedgerPath, opts.SessionKey, ind, nowFn()); err != nil {
			return res, err
		}
		res.Wrote = true
		return res, nil
	default:
		res.Dropped = "unknown_route"
		return res, nil
	}
}

func appendMemorySection(workspaceDir string, c Candidate, at time.Time) error {
	path := filepath.Join(workspaceDir, "MEMORY.md")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	heading := fmt.Sprintf("## %s\n", at.Format("2006-01-02 15:04"))
	meta := fmt.Sprintf("<!-- induction write_target=%s subject_id=%s fact_key=%s -->\n",
		c.Target, NormalizeSubject(c.SubjectID), c.FactKey)
	body := strings.TrimSpace(c.Text) + "\n\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(heading + meta + body)
	return err
}

type ledgerRow struct {
	At        string `json:"at"`
	Session   string `json:"session,omitempty"`
	Target    string `json:"target"`
	Route     string `json:"route"`
	SubjectID string `json:"subjectId"`
	FactKey   string `json:"factKey"`
	Text      string `json:"text"`
}

func appendLedger(path, session string, ind *Induced, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	row := ledgerRow{
		At:        at.UTC().Format(time.RFC3339),
		Session:   session,
		Target:    string(ind.Candidate.Target),
		Route:     string(ind.Route),
		SubjectID: NormalizeSubject(ind.Candidate.SubjectID),
		FactKey:   ind.Candidate.FactKey,
		Text:      ind.Candidate.Text,
	}
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// DefaultLedgerPath returns ~/.deneb/data/memory_induction.jsonl under stateDir.
func DefaultLedgerPath(stateDir string) string {
	return filepath.Join(stateDir, "data", "memory_induction.jsonl")
}
