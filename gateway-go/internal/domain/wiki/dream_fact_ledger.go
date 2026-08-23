// dream_fact_ledger.go — per-fact audit trail of dreamer mutations
// (docs/research/improvement-ideas.md 5.6). DreamReport counters were the only
// summary and the four legacy Facts* fields were never even set (always 0):
// what the dreamer learned/merged/expired overnight was reconstructable only
// from wiki git history. The ledger records every mutation at the moment it
// happens — phase, operation, page (and the folded page for merges), detail —
// so a wrong memory can be traced to its source, the weekly digest reports
// measured numbers, and fact-level feedback (5.7) has a key to point at.
//
// Best-effort by design: a ledger failure logs and moves on — the dream cycle
// itself must never fail because its audit trail hiccupped.
package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dreamFactLedgerFile = ".dream-fact-ledger.jsonl"

// DreamFactRecord is one fact-level dreamer mutation.
type DreamFactRecord struct {
	AtMs int64 `json:"atMs"`
	// Phase: "synthesize" (LLM proposals applied) | "verify" (deterministic
	// duplicate/misclassification/retention fixes).
	Phase string `json:"phase"`
	// Op: "learned" (page written from a proposal) | "merged" (duplicate
	// folded) | "expired" (archived — superseded or retention) | "moved"
	// (misclassification move).
	Op      string `json:"op"`
	Page    string `json:"page"`
	RefPage string `json:"refPage,omitempty"` // merge: the folded-away page
	Detail  string `json:"detail,omitempty"`  // detector detail / proposal summary
}

// dreamFactLedgerPath sits with the other dream state dotfiles in the wiki dir.
func (wd *WikiDreamer) dreamFactLedgerPath() string {
	if wd == nil || wd.store == nil {
		return ""
	}
	return filepath.Join(wd.store.Dir(), dreamFactLedgerFile)
}

// recordDreamFact appends one mutation to the fact ledger. Best-effort: empty
// pages skip silently (nothing to audit), write failures only warn.
func (wd *WikiDreamer) recordDreamFact(phase, op, page, refPage, detail string) {
	page = strings.TrimSpace(page)
	if page == "" {
		return
	}
	path := wd.dreamFactLedgerPath()
	if path == "" {
		return
	}
	rec := DreamFactRecord{
		AtMs:    time.Now().UnixMilli(),
		Phase:   phase,
		Op:      op,
		Page:    page,
		RefPage: strings.TrimSpace(refPage),
		Detail:  strings.TrimSpace(detail),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		wd.warnf("wiki-dream: fact ledger open failed", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		wd.warnf("wiki-dream: fact ledger append failed", err)
	}
}

// LoadDreamFactLedger returns the retained fact records (audit/debug surface).
func (wd *WikiDreamer) LoadDreamFactLedger() []DreamFactRecord {
	path := wd.dreamFactLedgerPath()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []DreamFactRecord
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec DreamFactRecord
		if json.Unmarshal([]byte(line), &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

// warnf logs through the dreamer's logger when one exists (tests construct
// WikiDreamer literals with none).
func (wd *WikiDreamer) warnf(msg string, err error) {
	if wd != nil && wd.logger != nil {
		wd.logger.Warn(msg, "error", err)
	}
}
