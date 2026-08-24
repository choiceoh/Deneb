// retry_correction_hint.go — reactive consumption of the mined retry-correction
// ledger (AMD 2608.07169 review adoption, 2026-08-24).
//
// retry_correction_miner.go MINES failed→successful tool retries; until now the
// only consumers were the sweep's evidence clusters (skill pitfalls, L1
// proposals), all of which pay off days later through a skill edit. Agent
// Memory Distillation's finding is that the per-function layer of that memory
// pays off best when it is retrieved REACTIVELY — at the moment a tool call
// errors — rather than injected up front. That is the loop this file closes:
// the same ledger, read at error time, so the correction the agent already
// discovered once is handed back the next time the same error appears.
//
// Design constraints (why this is a separate read-only index, not a miner method):
//   - The hot path must never scan transcripts. This index only reads the
//     ledger JSON the sweep already wrote, reloading on mtime change.
//   - It costs nothing on the happy path: the tool executor calls it only when
//     a tool actually returned an error.
//   - Only RECURRING corrections qualify (support ≥ hintMinSupport). A one-off
//     argument fix is as likely to be situational as instructive, and a wrong
//     hint at error time is worse than none — it steers the retry.
package genesis

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// hintMinSupport is how many times a signature must have been corrected
	// before it is worth telling the agent about.
	hintMinSupport = 2
	// hintMinErrorOverlap is the shortest normalized error prefix that may
	// decide a match. Below this the "error head" is too generic (e.g.
	// "error: ") to distinguish one failure mode from another.
	hintMinErrorOverlap = 16
	// hintReloadInterval bounds how often the ledger file is stat-ed.
	hintReloadInterval = 30 * time.Second
)

// RetryHintIndex serves error-time corrections from the mined ledger.
// A nil pointer is inert (Advice returns ""), so callers need no nil checks.
type RetryHintIndex struct {
	mu         sync.Mutex // guards every field below; no callback runs under it
	ledgerPath string
	logger     *slog.Logger
	checkedAt  time.Time
	modTime    time.Time
	size       int64
	// hints is keyed by tool name; each list is sorted by descending support.
	hints map[string][]retryHint
}

type retryHint struct {
	normError   string
	changed     []string
	failedArgs  string
	successArgs string
	support     int
	lastAt      int64
}

// NewRetryHintIndex returns an index over stateDir's retry-correction ledger.
// An empty stateDir yields an inert index.
func NewRetryHintIndex(stateDir string, logger *slog.Logger) *RetryHintIndex {
	if strings.TrimSpace(stateDir) == "" {
		return &RetryHintIndex{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RetryHintIndex{
		ledgerPath: filepath.Join(stateDir, retryLedgerFileName),
		logger:     logger,
	}
}

// Advice returns a one-line Korean correction for a tool call that just failed,
// or "" when nothing recurring matches.
func (x *RetryHintIndex) Advice(tool, errText string) string {
	if x == nil || x.ledgerPath == "" || strings.TrimSpace(tool) == "" || strings.TrimSpace(errText) == "" {
		return ""
	}
	x.mu.Lock()
	x.refreshLocked(time.Now())
	candidates := x.hints[tool]
	x.mu.Unlock()
	if len(candidates) == 0 {
		return ""
	}
	best, ok := pickRetryHint(candidates, normalizeRetryError(stripToolErrorPrefix(errText)))
	if !ok {
		return ""
	}
	return renderRetryHint(best)
}

// refreshLocked reloads the ledger when the file changed, at most once per
// hintReloadInterval. A missing or unreadable ledger clears the index — an
// absent hint is the safe degradation, so this is Warn, not Error.
func (x *RetryHintIndex) refreshLocked(now time.Time) {
	if !x.checkedAt.IsZero() && now.Sub(x.checkedAt) < hintReloadInterval {
		return
	}
	x.checkedAt = now
	info, err := os.Stat(x.ledgerPath)
	if err != nil {
		x.hints = nil
		return
	}
	if x.hints != nil && info.ModTime().Equal(x.modTime) && info.Size() == x.size {
		return
	}
	x.modTime, x.size = info.ModTime(), info.Size()
	data, err := os.ReadFile(x.ledgerPath)
	if err != nil {
		x.hints = nil
		return
	}
	var led retryLedger
	if err := json.Unmarshal(data, &led); err != nil {
		if x.logger != nil {
			x.logger.Warn("retry-hint ledger unreadable", "path", x.ledgerPath, "error", err)
		}
		x.hints = nil
		return
	}
	x.hints = buildRetryHints(led.Records, now)
}

// buildRetryHints folds live records into per-tool hint lists. Grouping is by
// the SAME signature the miner clusters on, so support here means exactly what
// support means in the evidence clusters.
func buildRetryHints(records []RetryCorrectionRecord, now time.Time) map[string][]retryHint {
	cutoff := now.Add(-retryEvidenceTTL).UnixMilli()
	bySig := map[string]*retryHint{}
	sigTool := map[string]string{}
	for _, r := range records {
		if r.AtMs < cutoff || strings.TrimSpace(r.Tool) == "" {
			continue
		}
		h := bySig[r.Signature]
		if h == nil {
			h = &retryHint{normError: normalizeRetryError(stripToolErrorPrefix(r.ErrorHead))}
			bySig[r.Signature] = h
			sigTool[r.Signature] = r.Tool
		}
		h.support++
		if r.AtMs >= h.lastAt {
			h.lastAt = r.AtMs
			h.changed = r.ChangedFields
			h.failedArgs = r.FailedArgs
			h.successArgs = r.SuccessArgs
		}
	}
	out := map[string][]retryHint{}
	for sig, h := range bySig {
		if h.support < hintMinSupport || len([]rune(h.normError)) < hintMinErrorOverlap {
			continue
		}
		tool := sigTool[sig]
		out[tool] = append(out[tool], *h)
	}
	for tool, list := range out {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].support != list[j].support {
				return list[i].support > list[j].support
			}
			return list[i].lastAt > list[j].lastAt
		})
		out[tool] = list
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stripToolErrorPrefix removes the executor's "Error: " wrapper. The ledger
// stores the tool_result CONTENT (already wrapped), while the live lookup
// happens with the raw error string — without this the two sides never share a
// prefix and every hint silently misses. Found against the production ledger,
// not in unit tests, which is why the fixture below mirrors a real record.
func stripToolErrorPrefix(s string) string {
	t := strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(t, "Error:"); ok {
		return strings.TrimSpace(rest)
	}
	return t
}

// pickRetryHint selects the best-supported hint whose normalized error head is
// prefix-compatible with the live one. Prefix rather than equality: the stored
// head is truncated at 64 runes and the live error may be longer or shorter,
// but the shared leading run is what identifies the failure mode.
func pickRetryHint(candidates []retryHint, liveError string) (retryHint, bool) {
	for _, h := range candidates {
		if retryErrorPrefixMatch(h.normError, liveError) {
			return h, true
		}
	}
	return retryHint{}, false
}

func retryErrorPrefixMatch(a, b string) bool {
	if len([]rune(a)) < hintMinErrorOverlap || len([]rune(b)) < hintMinErrorOverlap {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// renderRetryHint writes the correction as one line the model can act on. It
// names the changed fields AND shows both argument sides, because the field
// name alone ("action") never says what to change it TO.
func renderRetryHint(h retryHint) string {
	fields := "인자"
	if len(h.changed) > 0 && !(len(h.changed) == 1 && h.changed[0] == "args") {
		fields = strings.Join(h.changed, ", ") + " 필드"
	}
	return fmt.Sprintf("이전에 같은 오류를 %d회 겪고 %s를 고쳐 성공했다: %s → %s",
		h.support, fields,
		truncateRetryText(h.failedArgs, 160), truncateRetryText(h.successArgs, 160))
}
