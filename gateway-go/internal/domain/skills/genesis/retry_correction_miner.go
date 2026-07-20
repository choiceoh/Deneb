// retry_correction_miner.go — deterministic mining of failed→successful tool
// retry pairs from session transcripts (EMG review adoption, 2026-07-21).
//
// When the agent's tool call errors and a corrected call to the same tool
// succeeds moments later, the transcript already contains a contrastive
// failure/expert trajectory pair — the agent silently fixed itself and the
// lesson evaporated. This miner extracts the pair DETERMINISTICALLY (no LLM):
// it diffs the failed and successful arguments field-by-field and records the
// edit ("action: read→thread under error X") as a correction ledger entry.
// Grouped by signature, the entries surface as `tool_retry` failure-evidence
// clusters alongside the tracker's usage/rejection clusters, so the existing
// self-improve sweep lane consumes them (skill pitfalls, L1 proposals) without
// any new consumption machinery. Sibling of runtime_error_mining.go, which
// mines recurring runtime errors the same observation-side way.
//
// Measured supply (2026-07-21, 45d of prod transcripts): 53 retry-success
// pairs across 20 sessions, 27 argument-similar — a steady weekly trickle,
// concentrated in tool-argument mistakes (gmail 16/27).
//
// Doctrine fit: observation-side only — the LLM produces nothing here and no
// acceptance gate is touched; the sweep turn stays the judge of what to
// propose. EMG's Fused Gromov-Wasserstein alignment is intentionally NOT
// ported: Deneb trajectories are linear tool-call sequences, so windowed
// same-tool matching plus field diffs recover the same edit path at a
// fraction of the machinery.
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

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	// retryLedgerFileName lives under the state dir.
	retryLedgerFileName = "retry-corrections.json"
	// retryPairWindow bounds how many subsequent calls may separate the
	// failed call from its successful correction.
	retryPairWindow = 6
	// retryLedgerMaxRecords caps the ledger (newest kept).
	retryLedgerMaxRecords = 500
	// retryEvidenceTTL drops records from clustering and the ledger.
	retryEvidenceTTL = 45 * 24 * time.Hour
	// retryScanSlack re-reads files slightly older than the last scan so a
	// file whose mtime raced the previous scan is never skipped for good.
	retryScanSlack = 2 * time.Hour
	// retryMaxFileBytes skips pathologically large transcripts.
	retryMaxFileBytes = 64 << 20
	// retryArgsStoreCap bounds stored argument snippets.
	retryArgsStoreCap = 300
)

// retrySessionExcludes filters test/synthetic session transcripts.
var retrySessionExcludes = []string{"lt-", "puppet", "test", "verify", "wh-optout", "iterate", "wiki-rec"}

// RetryCorrectionRecord is one mined failed→successful correction.
type RetryCorrectionRecord struct {
	SessionKey      string   `json:"sessionKey"`
	FailedToolUseID string   `json:"failedToolUseId"` // dedupe key with SessionKey
	Tool            string   `json:"tool"`
	Signature       string   `json:"signature"`
	ChangedFields   []string `json:"changedFields"`
	ErrorHead       string   `json:"errorHead"`
	FailedArgs      string   `json:"failedArgs"`
	SuccessArgs     string   `json:"successArgs"`
	AtMs            int64    `json:"atMs"`
}

// retryLedger is the persisted envelope.
type retryLedger struct {
	Version    int                     `json:"version"`
	LastScanMs int64                   `json:"lastScanMs"`
	Records    []RetryCorrectionRecord `json:"records"`
}

// RetryCorrectionMiner incrementally scans transcripts and serves clusters.
type RetryCorrectionMiner struct {
	mu             sync.Mutex
	transcriptsDir string
	ledgerPath     string
	logger         *slog.Logger
	loaded         bool
	lastScanMs     int64
	records        []RetryCorrectionRecord
	seen           map[string]bool // SessionKey|FailedToolUseID
}

// NewRetryCorrectionMiner returns a miner rooted at stateDir. An empty
// stateDir yields an inert miner (every call no-ops).
func NewRetryCorrectionMiner(stateDir string, logger *slog.Logger) *RetryCorrectionMiner {
	if logger == nil {
		logger = slog.Default()
	}
	m := &RetryCorrectionMiner{logger: logger, seen: map[string]bool{}}
	if strings.TrimSpace(stateDir) != "" {
		m.transcriptsDir = filepath.Join(stateDir, "transcripts")
		m.ledgerPath = filepath.Join(stateDir, retryLedgerFileName)
	}
	return m
}

// MineAndClusters scans transcripts modified since the last scan, folds new
// correction records into the ledger, and returns the current clusters. It is
// called from the heartbeat sweep path (≤2/day), so a full call is cheap:
// only changed files are re-parsed.
func (m *RetryCorrectionMiner) MineAndClusters(limit int, now time.Time) []FailureClusterSummary {
	if m == nil || m.transcriptsDir == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	changed := m.scanLocked(now)
	changed = m.pruneLocked(now) || changed
	if changed {
		m.saveLocked()
	}
	return m.clustersLocked(limit, now)
}

// loadLocked reads the ledger once per process.
func (m *RetryCorrectionMiner) loadLocked() {
	if m.loaded {
		return
	}
	m.loaded = true
	data, err := os.ReadFile(m.ledgerPath)
	if err != nil {
		return
	}
	var led retryLedger
	if err := json.Unmarshal(data, &led); err != nil {
		m.logger.Warn("retry-correction ledger unreadable, starting empty", "path", m.ledgerPath, "error", err)
		return
	}
	m.lastScanMs = led.LastScanMs
	m.records = led.Records
	for _, r := range m.records {
		m.seen[r.SessionKey+"|"+r.FailedToolUseID] = true
	}
}

// scanLocked parses transcripts changed since the last scan. Returns whether
// new records were added.
func (m *RetryCorrectionMiner) scanLocked(now time.Time) bool {
	entries, err := os.ReadDir(m.transcriptsDir)
	if err != nil {
		return false
	}
	cutoff := m.lastScanMs - retryScanSlack.Milliseconds()
	added := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || isExcludedRetrySession(name) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > retryMaxFileBytes || info.ModTime().UnixMilli() <= cutoff {
			continue
		}
		sessionKey := strings.TrimSuffix(name, ".jsonl")
		for _, rec := range mineTranscriptFile(filepath.Join(m.transcriptsDir, name), sessionKey) {
			key := rec.SessionKey + "|" + rec.FailedToolUseID
			if m.seen[key] {
				continue
			}
			m.seen[key] = true
			m.records = append(m.records, rec)
			added = true
		}
	}
	m.lastScanMs = now.UnixMilli()
	return added
}

// pruneLocked applies TTL and the record cap. Returns whether it changed the set.
func (m *RetryCorrectionMiner) pruneLocked(now time.Time) bool {
	cutoff := now.Add(-retryEvidenceTTL).UnixMilli()
	kept := m.records[:0]
	for _, r := range m.records {
		if r.AtMs >= cutoff {
			kept = append(kept, r)
		}
	}
	changed := len(kept) != len(m.records)
	m.records = kept
	if len(m.records) > retryLedgerMaxRecords {
		sort.SliceStable(m.records, func(i, j int) bool { return m.records[i].AtMs > m.records[j].AtMs })
		m.records = m.records[:retryLedgerMaxRecords]
		changed = true
	}
	if changed {
		m.seen = map[string]bool{}
		for _, r := range m.records {
			m.seen[r.SessionKey+"|"+r.FailedToolUseID] = true
		}
	}
	return changed
}

// saveLocked persists the ledger (best-effort; a lost ledger only re-learns).
func (m *RetryCorrectionMiner) saveLocked() {
	data, err := json.Marshal(retryLedger{Version: 1, LastScanMs: m.lastScanMs, Records: m.records})
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(m.ledgerPath, data, nil); err != nil {
		m.logger.Warn("retry-correction ledger save failed", "path", m.ledgerPath, "error", err)
	}
}

// clustersLocked groups live records by signature into evidence clusters.
func (m *RetryCorrectionMiner) clustersLocked(limit int, now time.Time) []FailureClusterSummary {
	cutoff := now.Add(-retryEvidenceTTL).UnixMilli()
	byID := map[string]*FailureClusterSummary{}
	for _, r := range m.records {
		if r.AtMs < cutoff {
			continue
		}
		c := byID[r.Signature]
		if c == nil {
			c = &FailureClusterSummary{
				Kind:      "tool_retry",
				Skill:     r.Tool,
				Signature: r.Signature,
				Example:   retryExample(r),
			}
			byID[r.Signature] = c
		}
		c.Support++
		if r.AtMs > c.LastAt {
			c.LastAt = r.AtMs
			c.Example = retryExample(r)
		}
	}
	out := make([]FailureClusterSummary, 0, len(byID))
	for _, c := range byID {
		c.Route = routeFailureCluster(*c)
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Support != out[j].Support {
			return out[i].Support > out[j].Support
		}
		return out[i].LastAt > out[j].LastAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// retryExample renders the actionable diff for a record.
func retryExample(r RetryCorrectionRecord) string {
	return fmt.Sprintf("%s → %s (오류: %s)",
		truncateRetryText(r.FailedArgs, 120), truncateRetryText(r.SuccessArgs, 120), r.ErrorHead)
}

// MergeFailureClusters combines evidence clusters from multiple sources,
// ranking by support then recency, capped at limit.
func MergeFailureClusters(a, b []FailureClusterSummary, limit int) []FailureClusterSummary {
	out := make([]FailureClusterSummary, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Support != out[j].Support {
			return out[i].Support > out[j].Support
		}
		return out[i].LastAt > out[j].LastAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func isExcludedRetrySession(name string) bool {
	lower := strings.ToLower(name)
	for _, x := range retrySessionExcludes {
		if strings.Contains(lower, x) {
			return true
		}
	}
	return false
}

// --- transcript parsing ---

type retryTranscriptMsg struct {
	Content   json.RawMessage `json:"content"`
	Timestamp int64           `json:"timestamp"`
}

type retryTranscriptBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
}

// retryCall is one tool invocation reconstructed from use+result blocks.
type retryCall struct {
	name      string
	args      string // canonical (sorted-key) JSON, capped
	ok        bool
	toolUseID string
	errText   string
	atMs      int64
}

// mineTranscriptFile extracts correction records from one session transcript.
// Any parse trouble degrades to "fewer calls seen" — never an error.
func mineTranscriptFile(path, sessionKey string) []RetryCorrectionRecord {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	type useInfo struct{ name, args string }
	uses := map[string]useInfo{}
	var calls []retryCall
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg retryTranscriptMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		var blocks []retryTranscriptBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue // plain-string content
		}
		for _, blk := range blocks {
			switch blk.Type {
			case "tool_use":
				uses[blk.ID] = useInfo{name: blk.Name, args: canonicalRetryArgs(blk.Input)}
			case "tool_result":
				u, ok := uses[blk.ToolUseID]
				if !ok {
					continue
				}
				call := retryCall{
					name:      u.name,
					args:      u.args,
					ok:        !blk.IsError,
					toolUseID: blk.ToolUseID,
					atMs:      msg.Timestamp,
				}
				if blk.IsError {
					call.errText = retryResultText(blk)
				}
				calls = append(calls, call)
			}
		}
	}

	var out []RetryCorrectionRecord
	for i, c := range calls {
		if c.ok {
			continue
		}
		for j := i + 1; j < len(calls) && j <= i+retryPairWindow; j++ {
			n := calls[j]
			if n.name != c.name || !n.ok {
				continue
			}
			changed, same := sameRetryAttempt(c.args, n.args)
			if same {
				out = append(out, RetryCorrectionRecord{
					SessionKey:      sessionKey,
					FailedToolUseID: c.toolUseID,
					Tool:            c.name,
					Signature:       retrySignature(c.name, changed, c.errText),
					ChangedFields:   changed,
					ErrorHead:       truncateRetryText(collapseRetryWS(c.errText), 160),
					FailedArgs:      truncateRetryText(c.args, retryArgsStoreCap),
					SuccessArgs:     truncateRetryText(n.args, retryArgsStoreCap),
					AtMs:            n.atMs,
				})
			}
			break // nearest same-tool call decides; later calls are new business
		}
	}
	return out
}

// canonicalRetryArgs renders tool input with sorted keys so equal inputs
// compare equal regardless of original field order.
func canonicalRetryArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncateRetryText(string(raw), 600)
	}
	out, err := json.Marshal(v) // map keys marshal sorted
	if err != nil {
		return truncateRetryText(string(raw), 600)
	}
	return truncateRetryText(string(out), 600)
}

// retryResultText extracts the error text from a tool_result block whose
// content may be a plain string or a block array.
func retryResultText(blk retryTranscriptBlock) string {
	if len(blk.Content) > 0 {
		var s string
		if err := json.Unmarshal(blk.Content, &s); err == nil {
			return s
		}
		var inner []retryTranscriptBlock
		if err := json.Unmarshal(blk.Content, &inner); err == nil {
			var parts []string
			for _, b := range inner {
				if b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			return strings.Join(parts, " ")
		}
	}
	return blk.Text
}

// sameRetryAttempt decides whether a successful call is a CORRECTION of the
// failed one, and names the changed fields. Identical arguments are excluded:
// a retry that succeeded unchanged is a transient flake, not a lesson.
func sameRetryAttempt(failedArgs, successArgs string) ([]string, bool) {
	if failedArgs == successArgs {
		return nil, false
	}
	var f, s map[string]any
	if json.Unmarshal([]byte(failedArgs), &f) != nil || json.Unmarshal([]byte(successArgs), &s) != nil ||
		len(f) == 0 || len(s) == 0 {
		if retryLCSRatio(failedArgs, successArgs) >= 0.5 {
			return []string{"args"}, true
		}
		return nil, false
	}
	keys := map[string]bool{}
	for k := range f {
		keys[k] = true
	}
	for k := range s {
		keys[k] = true
	}
	equal := 0
	var changed []string
	for k := range keys {
		fv, fok := f[k]
		sv, sok := s[k]
		if fok && sok && jsonEqualRetry(fv, sv) {
			equal++
		} else {
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		return nil, false
	}
	if equal >= 1 {
		sort.Strings(changed)
		if len(changed) > 4 {
			changed = changed[:4]
		}
		return changed, true
	}
	// No shared field (e.g. single-field tools like web {url}): fall back to
	// textual similarity of the VALUES only — comparing raw JSON would let the
	// shared scaffolding ({"path":"…) pass unrelated calls as similar.
	if retryLCSRatio(retryValuesOnly(f), retryValuesOnly(s)) >= 0.5 {
		return []string{"args"}, true
	}
	return nil, false
}

// retryValuesOnly concatenates the map's values in key order, dropping the
// JSON key/punctuation scaffolding from similarity comparisons.
func retryValuesOnly(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%v", m[k]))
	}
	return strings.Join(parts, " ")
}

func jsonEqualRetry(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ab) == string(bb)
}

// retrySignature keys a cluster: tool + changed fields + normalized error head.
func retrySignature(tool string, changed []string, errText string) string {
	return tool + "|" + strings.Join(changed, ",") + "|" + normalizeRetryError(errText)
}

// normalizeRetryError lowercases, folds digit runs to '#', collapses spaces,
// and caps the head so volatile IDs/counts don't split clusters.
func normalizeRetryError(s string) string {
	s = strings.ToLower(collapseRetryWS(s))
	var b strings.Builder
	inDigits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !inDigits {
				b.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}
	return truncateRetryText(b.String(), 64)
}

func collapseRetryWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRetryText(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// retryLCSRatio is a bounded longest-common-subsequence similarity in [0,1].
func retryLCSRatio(a, b string) float64 {
	const bound = 300
	ra, rb := []rune(a), []rune(b)
	if len(ra) > bound {
		ra = ra[:bound]
	}
	if len(rb) > bound {
		rb = rb[:bound]
	}
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			switch {
			case ra[i-1] == rb[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	lcs := prev[len(rb)]
	return 2 * float64(lcs) / float64(len(ra)+len(rb))
}
