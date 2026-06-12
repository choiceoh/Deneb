// apc_diag.go — prefix-stability diagnostics for the local-vLLM main path.
//
// Why this exists:
//   - The main model is served by vLLM with automatic prefix caching (APC).
//     Unlike Anthropic's checkpoint-based cache, APC is a pure longest-prefix
//     match: the first changed byte invalidates everything after it, and the
//     invalidated span must re-prefill (~1.7-2K tok/s on GB10 — tens of
//     seconds for a long history).
//   - The Aiden vLLM build does not fill per-request usage
//     prompt_tokens_details.cached_tokens, so the gateway is blind to per-run
//     cache behavior (engine /metrics totals are the only signal).
//
// This file closes both gaps without touching the wire: at run start it
// compares the assembled (system prompt, messages) against the previous run
// of the same session and classifies the divergence; around the run it
// scrapes the engine's prefix-cache counters and attributes the delta. One
// "apc diag" log line per run is the output — the measurement substrate for
// prefix-stability work (e.g. recall injection position, pruning gates) and
// its regression guard.
//
// The comparison runs on the post-compaction, pre-BeforeAPICall message list
// (the deterministic per-session shape; steer notes and trailing cache
// markers are per-request copies applied later).
package chat

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
)

// APC divergence classes, ordered from harmless to expensive.
const (
	apcClassFirstRun       = "first-run"       // no prior snapshot for this session
	apcClassAppendOnly     = "append-only"     // prior messages are a byte-identical prefix
	apcClassHistoryMutated = "history-mutated" // bytes changed mid-history (compaction, pruning)
	apcClassSystemChanged  = "system-changed"  // system prompt bytes changed → whole history invalidated
)

// apcSnapshot is the assembled-prompt shape of one session's previous run.
type apcSnapshot struct {
	systemHash uint64
	recallHash uint64
	msgHashes  []uint64
}

// apcSnapshotStore keeps one snapshot per session. Sessions are a small,
// bounded set in this single-operator deployment (client:main, cron:*, …), so
// entries are never evicted; a /reset reuses the same key and simply logs one
// large history-mutated divergence on the next run.
var apcSnapshotStore = struct {
	mu    sync.Mutex
	store map[string]apcSnapshot
}{store: make(map[string]apcSnapshot)}

// apcDiagRun carries one run's divergence result plus the engine counter
// baseline from run start. Used by a single goroutine — no locking needed.
type apcDiagRun struct {
	logger *slog.Logger

	class         string
	divergedAt    int // message index of first differing bytes (-1 when none)
	prevMsgs      int
	curMsgs       int
	invalidTokens int // est. tokens that must re-prefill beyond pure append
	appendTokens  int // est. tokens of the genuinely new tail
	recallChanged bool

	scrapeBases  []string
	model        string
	startQueries int64
	startHits    int64
	scraped      bool
	done         bool
}

// beginAPCDiag classifies how this run's assembled prompt diverges from the
// session's previous run and snapshots the engine prefix-cache counters.
// systemPrompt is the finalized system-block JSON (the wire form).
// Always returns a usable value; callers pair it with a deferred finish().
func beginAPCDiag(ctx context.Context, deps runDeps, sessionKey, providerID, model string, systemPrompt []byte, recallMemory string, messages []llm.Message, logger *slog.Logger) *apcDiagRun {
	d := &apcDiagRun{logger: logger, model: model, divergedAt: -1}

	cur := apcSnapshot{
		systemHash: apcHashBytes(systemPrompt),
		recallHash: apcHashBytes([]byte(recallMemory)),
		msgHashes:  apcHashMessages(messages),
	}

	apcSnapshotStore.mu.Lock()
	prev, hadPrev := apcSnapshotStore.store[sessionKey]
	apcSnapshotStore.store[sessionKey] = cur
	apcSnapshotStore.mu.Unlock()

	d.curMsgs = len(messages)
	switch {
	case !hadPrev:
		d.class = apcClassFirstRun
		d.appendTokens = compact.EstimateMessagesTokens(messages)
	default:
		d.prevMsgs = len(prev.msgHashes)
		d.recallChanged = prev.recallHash != cur.recallHash
		p := commonPrefixLen(prev.msgHashes, cur.msgHashes)
		switch {
		case prev.systemHash != cur.systemHash:
			// System prompt sits before all history on the wire: any byte
			// change re-prefills the entire message list.
			d.class = apcClassSystemChanged
			d.divergedAt = 0
			d.invalidTokens = compact.EstimateMessagesTokens(messages)
		case p == len(prev.msgHashes):
			d.class = apcClassAppendOnly
			d.appendTokens = compact.EstimateMessagesTokens(messages[p:])
		default:
			d.class = apcClassHistoryMutated
			d.divergedAt = p
			d.invalidTokens = compact.EstimateMessagesTokens(messages[p:])
		}
	}

	// Engine counter baseline — only meaningful when this run actually talks
	// to a vLLM engine (OpenAI mode). The scrape is local and bounded; a down
	// or non-vLLM endpoint contributes nothing and the diag line simply omits
	// the engine fields.
	if deps.registry != nil && resolveAPIMode(deps, providerID) == llm.APIModeOpenAI {
		if bases := deps.registry.VllmBaseURLs(); len(bases) > 0 {
			d.scrapeBases = bases
			if q, h, ok := scrapeAPCCounters(ctx, bases, model); ok {
				d.startQueries, d.startHits = q, h
				d.scraped = true
			}
		}
	}
	return d
}

// finish scrapes the engine counters again and emits the single "apc diag"
// line. Safe to call exactly once via defer; nil-safe for belt and braces.
func (d *apcDiagRun) finish() {
	if d == nil || d.done || d.logger == nil {
		return
	}
	d.done = true

	attrs := []any{
		"class", d.class,
		"divergedAt", d.divergedAt,
		"prevMsgs", d.prevMsgs,
		"msgs", d.curMsgs,
		"invalidatedTokensEst", d.invalidTokens,
		"appendedTokensEst", d.appendTokens,
		"recallChanged", d.recallChanged,
	}
	if d.scraped {
		// Decoupled from the request ctx on purpose: finish runs on the error
		// path too (deferred), where the request ctx may already be canceled.
		// Bounded by the scrape's own short timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if q, h, ok := scrapeAPCCounters(ctx, d.scrapeBases, d.model); ok {
			dq, dh := q-d.startQueries, h-d.startHits
			attrs = append(attrs, "engineQueriesDelta", dq, "engineHitsDelta", dh)
			if dq > 0 {
				attrs = append(attrs, "engineHitPct", float64(dh)/float64(dq)*100)
			}
		}
	}
	d.logger.Info("apc diag", attrs...)
}

// scrapeAPCCounters sums the engine prefix-cache counters across the given
// vLLM bases, preferring rows whose served-model name matches the run's model
// (so e.g. a sidecar OCR engine on another port cannot pollute the delta).
// Falls back to the sum of all rows when no row matches.
func scrapeAPCCounters(ctx context.Context, bases []string, model string) (queries, hits int64, ok bool) {
	rows := observe.FetchVllmPrefixCaches(ctx, bases)
	if len(rows) == 0 {
		return 0, 0, false
	}
	var mq, mh int64
	matched := false
	for _, r := range rows {
		queries += r.Queries
		hits += r.Hits
		if r.Model == model {
			mq += r.Queries
			mh += r.Hits
			matched = true
		}
	}
	if matched {
		return mq, mh, true
	}
	return queries, hits, true
}

func apcHashBytes(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// apcHashMessages hashes each message's role + raw content bytes. The raw
// JSON content is exactly what the provider serializer consumes, so byte
// equality here tracks wire-prefix equality.
func apcHashMessages(messages []llm.Message) []uint64 {
	out := make([]uint64, len(messages))
	for i, m := range messages {
		h := fnv.New64a()
		_, _ = h.Write([]byte(m.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(m.Content)
		out[i] = h.Sum64()
	}
	return out
}

func commonPrefixLen(a, b []uint64) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
