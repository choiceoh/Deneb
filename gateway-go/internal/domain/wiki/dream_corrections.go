// dream_corrections.go — the disconfirming-evidence queue (docs/research/
// improvement-ideas.md 5.7). The critique and verify passes used to see only
// proposals + index: a user who corrected a wrong memory had no durable way to
// make the NEXT dream cycle re-examine that fact — the correction lived in a
// chat turn and the agent's manual wiki edit, nothing more. Corrections
// recorded here (workfeed 정정 on dream/digest cards today) are injected into
// the critique prompt as explicit 반증 evidence and consumed by the cycle that
// actually considered them.
//
// Consumption model: append-only records + a small state file of consumed ids
// (same pattern as the watch snapshots). A correction stays pending until a
// critique-eligible cycle (≥ critiqueMinUpdates proposals with an LLM wired)
// has seen it — tiny cycles never burn the operator's correction.
package wiki

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dreamCorrectionsFile      = ".dream-corrections.jsonl"
	dreamCorrectionsStateFile = ".dream-corrections-state.json"
)

// DreamCorrection is one user correction waiting to be considered.
type DreamCorrection struct {
	AtMs   int64  `json:"atMs"`
	ID     string `json:"id"`
	Source string `json:"source"` // e.g. "workfeed-correct"
	Page   string `json:"page,omitempty"`
	Note   string `json:"note"`
}

// RecordDreamCorrection appends one correction. Package-level (not a
// WikiDreamer method) so non-wiki callers — the RPC layer holding just the
// wiki dir — can write without constructing a dreamer.
func RecordDreamCorrection(wikiDir, source, page, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil // nothing to corroborate
	}
	rec := DreamCorrection{
		AtMs:   time.Now().UnixMilli(),
		ID:     dreamCorrectionID(page, note),
		Source: strings.TrimSpace(source),
		Page:   strings.TrimSpace(page),
		Note:   note,
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	path := filepath.Join(wikiDir, dreamCorrectionsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

func dreamCorrectionID(page, note string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(page + "\x00" + note))
	return fmt.Sprintf("dc-%d-%08x", time.Now().UnixMilli(), h.Sum32())
}

type dreamCorrectionsState struct {
	Version  int             `json:"version"`
	Consumed map[string]bool `json:"consumed"`
}

// pendingDreamCorrections returns unconsumed corrections, oldest first.
func (wd *WikiDreamer) pendingDreamCorrections() []DreamCorrection {
	if wd == nil || wd.store == nil {
		return nil
	}
	dir := wd.store.Dir()
	raw, err := os.ReadFile(filepath.Join(dir, dreamCorrectionsFile))
	if err != nil {
		return nil
	}
	var state dreamCorrectionsState
	if sraw, serr := os.ReadFile(filepath.Join(dir, dreamCorrectionsStateFile)); serr == nil {
		_ = json.Unmarshal(sraw, &state) // corrupt state = treat all as pending
	}
	var out []DreamCorrection
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec DreamCorrection
		if json.Unmarshal([]byte(line), &rec) != nil || rec.ID == "" {
			continue
		}
		if state.Consumed[rec.ID] {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// consumeDreamCorrections marks the given corrections considered (tmp+rename).
// Best-effort: a failed state write only means they are reconsidered next
// cycle — the safer failure direction.
func (wd *WikiDreamer) consumeDreamCorrections(corrections []DreamCorrection) {
	if wd == nil || wd.store == nil || len(corrections) == 0 {
		return
	}
	dir := wd.store.Dir()
	var state dreamCorrectionsState
	path := filepath.Join(dir, dreamCorrectionsStateFile)
	if sraw, serr := os.ReadFile(path); serr == nil {
		_ = json.Unmarshal(sraw, &state)
	}
	if state.Consumed == nil {
		state.Consumed = map[string]bool{}
	}
	state.Version = 1
	changed := false
	for _, c := range corrections {
		if !state.Consumed[c.ID] {
			state.Consumed[c.ID] = true
			changed = true
		}
	}
	if !changed {
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// RenderDreamCorrections formats corrections for the critique prompt's
// 반증 block (bounded so an unbounded queue cannot bloat the call).
func RenderDreamCorrections(corrections []DreamCorrection, max int) string {
	if len(corrections) == 0 {
		return ""
	}
	if max <= 0 {
		max = 10
	}
	var sb strings.Builder
	for i, c := range corrections {
		if i >= max {
			fmt.Fprintf(&sb, "… 외 %d건\n", len(corrections)-max)
			break
		}
		if c.Page != "" {
			fmt.Fprintf(&sb, "- (페이지 %s) %s\n", c.Page, c.Note)
		} else {
			fmt.Fprintf(&sb, "- %s\n", c.Note)
		}
	}
	return sb.String()
}
