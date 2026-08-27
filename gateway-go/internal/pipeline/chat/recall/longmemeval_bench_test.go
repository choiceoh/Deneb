package recall

// LongMemEval retrieval-only port — an EXTERNAL anchor for the recall
// pipeline's cross-session arm (polaris), scored deterministically against the
// dataset's evidence-session labels: no LLM reader, no LLM judge.
//
// Scope, stated honestly:
//   - This measures the polaris session arm only (wiki/diary/file/org receive
//     nothing from a chat-history ingest and stay nil).
//   - Questions are English, so the Korean cue phrases never fire and every
//     question runs on the tighter no-cue budget — exactly what production
//     would do with these messages.
//   - Retrieval hit ≠ answer accuracy. Vendor numbers on this dataset (e.g.
//     Memoria's 88.78%) are READER accuracy with an LLM judge and are not
//     comparable to these numbers in either direction.
//
// Run manually (never in CI — the env gate keeps it skipped):
//   LONGMEMEVAL_DATA=~/.deneb/bench/longmemeval/longmemeval_s.json \
//   LONGMEMEVAL_LIMIT=100 go test ./internal/pipeline/chat/recall/ \
//     -run TestLongMemEvalRetrieval -v -timeout 60m

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

type lmQuestion struct {
	QuestionID        string     `json:"question_id"`
	QuestionType      string     `json:"question_type"`
	Question          string     `json:"question"`
	QuestionDate      string     `json:"question_date"`
	HaystackDates     []string   `json:"haystack_dates"`
	HaystackSessionID []string   `json:"haystack_session_ids"`
	HaystackSessions  [][]lmTurn `json:"haystack_sessions"`
	AnswerSessionIDs  []string   `json:"answer_session_ids"`
}

type lmTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func lmParseDate(s string) time.Time {
	t, err := time.Parse("2006/01/02 (Mon) 15:04", strings.TrimSpace(s))
	if err != nil {
		return time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return t
}

func TestLongMemEvalRetrieval(t *testing.T) {
	dataPath := strings.TrimSpace(os.Getenv("LONGMEMEVAL_DATA"))
	if dataPath == "" {
		t.Skip("set LONGMEMEVAL_DATA to run the LongMemEval retrieval bench")
	}
	if strings.HasPrefix(dataPath, "~/") {
		home, _ := os.UserHomeDir()
		dataPath = filepath.Join(home, dataPath[2:])
	}
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read dataset: %v", err)
	}
	var questions []lmQuestion
	if err := json.Unmarshal(raw, &questions); err != nil {
		t.Fatalf("parse dataset: %v", err)
	}
	if limit, _ := strconv.Atoi(os.Getenv("LONGMEMEVAL_LIMIT")); limit > 0 && limit < len(questions) {
		questions = questions[:limit]
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// The semantic arm is wired in production (server_rpc_session.go); a bare
	// store has none, so leaving it out would measure the lexical half and call
	// it recall. LONGMEMEVAL_SEMANTIC=0 isolates that half deliberately.
	semanticOn := os.Getenv("LONGMEMEVAL_SEMANTIC") != "0"
	// The reranker is wired in production (server_rpc_session.go); NewFromEnv
	// returns nil without DENEB_RERANK_URL, which is the same no-op the code
	// takes when the sidecar is absent. LONGMEMEVAL_RERANK=0 forces it off.
	var reranker Reranker
	if os.Getenv("LONGMEMEVAL_RERANK") != "0" {
		if client := airerank.NewFromEnv(); client != nil {
			reranker = client
		}
	}
	var embedder embedindex.Embedder
	if semanticOn {
		// The client's health flag is set by a background probe, so embedding
		// immediately yields "server unhealthy" from a server that is fine.
		// Production waits the same way (waitForEmbeddingReady).
		client := embedding.New("", logger)
		embedder = client
		ready := false
		for range 40 {
			if client.IsHealthy() {
				ready = true
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if cachePath := strings.TrimSpace(os.Getenv("LONGMEMEVAL_EMBED_CACHE")); cachePath != "" && ready {
			cached := newCachedEmbedder(client, cachePath)
			defer cached.flush()
			embedder = cached
		}
		if !ready {
			t.Skip("embedding server not ready — a go test does not inherit the gateway unit's env, so set DENEB_EMBEDDING_URL (the code default :8001 is stale; Nemotron serves :8002), or LONGMEMEVAL_SEMANTIC=0 for the lexical-only half")
		}
	}
	storeDir := t.TempDir()

	type bucket struct{ total, anyHit, top1Hit, poolHit, pool10, hit8, renderedSum, emptySum int }
	buckets := map[string]*bucket{}
	overall := &bucket{}
	abstention := 0
	poolSize, rankedSize, dedupHits, filterHits := 0, 0, 0, 0

	// Questions are independent — each builds its own store — and the run is
	// dominated by round-trips to the embedding sidecar, not by GPU throughput
	// (measured near-idle during a sequential sweep). Running them concurrently
	// turns a latency-bound bench into a throughput-bound one. Workers stay
	// modest: each question holds a 54-session store in memory and this box runs
	// earlyoom.
	// Serial by default, and that is a correctness choice, not caution.
	// Concurrency puts the rerank sidecar under load, some calls exceed
	// polarisRerankTimeout, and those questions silently fall back to the fused
	// order — so a parallel run measures a DIFFERENT configuration than the one
	// under test, and a varying one: 60 questions repeated four times at 6
	// workers scored top1 71.7/73.3/73.3/78.3, while the same slice serial
	// scored 83.3 three times running. The embedding cache
	// (LONGMEMEVAL_EMBED_CACHE) is where the speed comes from — a cached sweep
	// runs in seconds — so there is nothing to buy with the accuracy.
	workers := 1
	if raw := strings.TrimSpace(os.Getenv("LONGMEMEVAL_WORKERS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 32 {
			workers = v
		}
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	// t.Fatalf is illegal off the test goroutine, so workers record the first
	// failure and the bench reports it after the pool drains.
	var firstErr error
	failq := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = fmt.Errorf(format, args...)
		}
	}
	var done int
	var wg sync.WaitGroup

	for qi, q := range questions {
		// Abstention questions have no evidence to retrieve; a retrieval metric
		// has nothing to score. Counted separately.
		if strings.HasSuffix(q.QuestionID, "_abs") || len(q.AnswerSessionIDs) == 0 {
			abstention++
			continue
		}
		wg.Add(1)
		go func(qi int, q lmQuestion) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			store, err := polaris.NewStore(filepath.Join(storeDir, fmt.Sprintf("lm-%d.db", qi)))
			if err != nil {
				failq("q%d: polaris store: %v", qi, err)
				return
			}
			bridge := polaris.NewBridge(transcript.NewMemoryTranscriptStore(), store, logger)
			if semanticOn {
				store.SetSummaryEmbedder(embedder)
			}

			evidenceSessions := map[string]bool{}
			for _, id := range q.AnswerSessionIDs {
				evidenceSessions[id] = true
			}
			questionAt := lmParseDate(q.QuestionDate)

			// Ingest shape decides which of polaris's three sub-arms can run, so it
			// is a measured variable, not an implementation detail:
			//
			//	multi (default) — each haystack session is its OWN Deneb session and
			//	  the question opens a fresh one. This is what LongMemEval actually
			//	  describes: 50+ separate conversations spread over months. Only this
			//	  shape lets appendPolarisCrossSessionHits and appendPolarisSummaryHits
			//	  run at all — both skip the current session by design (excludeKey).
			//	single — the whole history as one long chat. Structurally silences the
			//	  two cross-session arms, so it measures appendPolarisSessionHits alone.
			multi := !strings.EqualFold(os.Getenv("LONGMEMEVAL_INGEST"), "single")
			sessionKeyFor := func(si int) string {
				if multi {
					return fmt.Sprintf("client:lm:%s:s%d", q.QuestionID, si)
				}
				return "client:lm:" + q.QuestionID
			}
			// msgSession maps (sessionKey, msgIndex) back to the haystack session id
			// the message came from, which is what the labels are keyed on.
			msgSession := map[string][]string{}
			for si, sess := range q.HaystackSessions {
				key := sessionKeyFor(si)
				at := lmParseDate(q.HaystackDates[si]).UnixMilli()
				for _, turn := range sess {
					if err := bridge.Append(key, chatport.ChatMessage{
						Role:      turn.Role,
						Content:   chatport.MarshalJSONString(turn.Content),
						Timestamp: at,
					}); err != nil {
						failq("q%d: append: %v", qi, err)
						return
					}
					msgSession[key] = append(msgSession[key], q.HaystackSessionID[si])
				}
			}
			questionKey := "client:lm:" + q.QuestionID + ":q"
			if !multi {
				questionKey = sessionKeyFor(0)
			}
			// The question is the newest user message either way; the maxIdx guard
			// then skips exactly it.
			if err := bridge.Append(questionKey, chatport.ChatMessage{
				Role: "user", Content: chatport.MarshalJSONString(q.Question),
				Timestamp: questionAt.UnixMilli(),
			}); err != nil {
				failq("q%d: append question: %v", qi, err)
				return
			}
			msgSession[questionKey] = append(msgSession[questionKey], "__question__")

			// hitsEvidence resolves one rendered/candidate row back to its haystack
			// session. Rows carry three Source shapes: "msg#<i>/<role>" (current
			// session), "cl:<key>#<i>/<role>" (cross-session), and "cl:<key> 요약"
			// (semantic summary of another session).
			rowIsEvidence := func(source string) bool {
				key, idx, kind := parseRecallSource(source, questionKey)
				switch kind {
				case sourceKindSummary:
					ids := msgSession[key]
					return len(ids) > 0 && evidenceSessions[ids[0]]
				case sourceKindMessage:
					ids := msgSession[key]
					return idx >= 0 && idx < len(ids) && evidenceSessions[ids[idx]]
				case sourceKindOther:
					// A row from a non-polaris source (wiki/diary/file/org). None run
					// in this bench, so reaching here means an unparsed Source shape,
					// which must not be scored as a hit.
					return false
				}
				return false
			}
			hitIn := func(rows []recallEvidence) bool {
				for _, ev := range rows {
					if rowIsEvidence(ev.Source) {
						return true
					}
				}
				return false
			}

			if semanticOn {
				// RefreshAsync fills lazily, so an unwarmed first search would score
				// the semantic arm as absent rather than as bad.
				if err := store.WarmSemanticIndex(context.Background()); err != nil {
					failq("q%d: warm semantic: %v", qi, err)
					return
				}
			}
			// The production path, verbatim: query derivation → polaris source →
			// ranking → budget-cut rendering.
			queries := searchQueries(q.Question)
			// Reach now scales with the budget (polarisCrossHits), so the two
			// budgets need their own candidate pools — reusing one would score the
			// cue budget against the no-cue turn's narrower reach.
			candidates := rerankPolarisEvidence(
				context.Background(), reranker, q.Question,
				recallPolarisEvidence(context.Background(), bridge, questionKey, queries, false),
			)
			cueCandidates := rerankPolarisEvidence(
				context.Background(), reranker, q.Question,
				recallPolarisEvidence(context.Background(), bridge, questionKey, queries, true),
			)
			// Ceiling probe: what FTS could reach at limit 10, ignoring the per-query
			// quota. Measurement-only — not a production call.
			pool10Hit := false
			for _, query := range queries {
				for key, ids := range msgSession {
					if multi && key == questionKey {
						continue
					}
					hits, err := store.SearchMessages(key, query, 10)
					if err != nil {
						continue
					}
					for _, h := range hits {
						if h.MsgIndex < len(ids) && evidenceSessions[ids[h.MsgIndex]] {
							pool10Hit = true
							break
						}
					}
					if pool10Hit {
						break
					}
				}
				if pool10Hit {
					break
				}
			}
			// Diagnostic split: was the evidence IN THE CANDIDATE POOL at all
			// (finding problem), and where does the no-cue budget of 4 rows cut it
			// (ranking/budget problem)? The two need different fixes.
			poolHit := hitIn(candidates)
			evidence := rankRecallEvidence(append([]recallEvidence(nil), candidates...), queries, q.Question, hasCue(q.Question), questionAt)
			// rankRecallEvidence cuts to recallEvidenceBudget(cue) internally, so the
			// budget-8 number needs its own ranking pass with cue=true — slicing the
			// returned rows to 8 would silently re-measure the same 4.
			cueRanked := rankRecallEvidence(append([]recallEvidence(nil), cueCandidates...), queries, q.Question, true, questionAt)
			// Stage isolation: where does a pooled hit die — dedup or cross-subject filter?
			afterDedup := dedupRecallEvidence(append([]recallEvidence(nil), candidates...))
			afterFilter := filterCrossSubjectEvidence(append([]recallEvidence(nil), afterDedup...), q.Question)
			hitAt8 := hitIn(cueRanked)
			if hitIn(afterDedup) {
				dedupHits++
			}
			if hitIn(afterFilter) {
				filterHits++
			}
			block, _ := formatRecallEvidenceAt(evidence, questionAt, true)

			rendered := renderedSources(block)
			// Aggregation is the only shared state; everything above is per-question.
			mu.Lock()
			defer mu.Unlock()
			poolSize += len(candidates)
			rankedSize += len(cueRanked)
			b := buckets[q.QuestionType]
			if b == nil {
				b = &bucket{}
				buckets[q.QuestionType] = b
			}
			for _, tgt := range []*bucket{b, overall} {
				tgt.total++
				tgt.renderedSum += len(rendered)
				if len(rendered) == 0 {
					tgt.emptySum++
				}
				if poolHit {
					tgt.poolHit++
				}
				if pool10Hit {
					tgt.pool10++
				}
				if hitAt8 {
					tgt.hit8++
				}
			}
			for rank, source := range rendered {
				if rowIsEvidence(source) {
					b.anyHit++
					overall.anyHit++
					if rank == 0 {
						b.top1Hit++
						overall.top1Hit++
					}
					break
				}
			}
			store.Close()
			_ = os.Remove(filepath.Join(storeDir, fmt.Sprintf("lm-%d.db", qi)))
			done++
			if done%50 == 0 {
				t.Logf("progress: %d/%d", done, len(questions))
			}
		}(qi, q)
	}
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("bench worker failed: %v", firstErr)
	}

	pct := func(n, d int) string {
		if d == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(d))
	}
	types := make([]string, 0, len(buckets))
	for k := range buckets {
		types = append(types, k)
	}
	sort.Strings(types)
	t.Logf("== LongMemEval_s retrieval-only (polaris arm) ==")
	for _, k := range types {
		b := buckets[k]
		t.Logf("STAGE  pool=%s  after-dedup=%s  after-filter=%s  | avg pool=%.1f ranked(cue8)=%.1f",
			pct(overall.poolHit, overall.total), pct(dedupHits, overall.total), pct(filterHits, overall.total),
			float64(poolSize)/float64(maxInt(overall.total, 1)), float64(rankedSize)/float64(maxInt(overall.total, 1)))
		t.Logf("%-28s n=%-3d pool10=%-7s pool=%-7s hit@8=%-7s hit@4=%-7s top1=%-7s",
			k, b.total, pct(b.pool10, b.total), pct(b.poolHit, b.total), pct(b.hit8, b.total),
			pct(b.anyHit, b.total), pct(b.top1Hit, b.total))
	}
	t.Logf("%-28s n=%-3d pool10=%-7s pool=%-7s hit@8=%-7s hit@4=%-7s top1=%-7s (abstention excluded: %d)",
		"OVERALL", overall.total, pct(overall.pool10, overall.total), pct(overall.poolHit, overall.total), pct(overall.hit8, overall.total),
		pct(overall.anyHit, overall.total), pct(overall.top1Hit, overall.total), abstention)
}

// polarisMsgIndex parses the message index out of a polaris evidence Source
// ("msg#<idx>/<role>").
func polarisMsgIndex(source string) (int, bool) {
	if !strings.HasPrefix(source, "msg#") {
		return 0, false
	}
	rest := source[len("msg#"):]
	if j := strings.IndexByte(rest, '/'); j > 0 {
		rest = rest[:j]
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// cachedEmbedder memoizes embeddings by (role, content hash), on disk, across
// runs.
//
// A sweep re-embeds the identical corpus for every condition — the knobs under
// test (cross-session quota, BM25 length normalization, rerank window) change
// RETRIEVAL, never the text that gets embedded. Without this the sidecar, which
// saturates at ~6 concurrent callers (measured: 1 worker 2m55s, 6 workers 1m44s,
// 12 workers 1m55s for 60 questions), sets a hard floor per condition.
//
// EmbedKind is forwarded and keyed separately because Nemotron is asymmetric:
// it prefixes queries and passages differently, so caching them together — or
// dropping the method and letting embedindex fall back to plain Embed — would
// silently change what the bench measures.
//
// Bench-only on purpose: production embeds a live, growing corpus where a
// content-keyed disk cache would mostly miss, which is why polaris disables the
// embedindex cache (see semantic.go's header).
type cachedEmbedder struct {
	inner interface {
		embedindex.Embedder
		embedindex.RoleAwareEmbedder
	}
	path string

	mu    sync.Mutex
	cache map[string][]float32
	dirty bool
}

func newCachedEmbedder(inner *embedding.Client, path string) *cachedEmbedder {
	c := &cachedEmbedder{inner: inner, path: path, cache: map[string][]float32{}}
	if raw, err := os.ReadFile(path); err == nil {
		_ = gob.NewDecoder(bytes.NewReader(raw)).Decode(&c.cache)
	}
	return c
}

func (c *cachedEmbedder) IsHealthy() bool { return c.inner.IsHealthy() }

func (c *cachedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return c.embed(ctx, "", texts)
}

func (c *cachedEmbedder) EmbedKind(ctx context.Context, kind string, texts []string) ([][]float32, error) {
	return c.embed(ctx, kind, texts)
}

func (c *cachedEmbedder) embed(ctx context.Context, kind string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missing []string
	var missingAt []int
	c.mu.Lock()
	for i, text := range texts {
		if vec, ok := c.cache[kind+"\x00"+embedindex.ContentHash(text)]; ok {
			out[i] = vec
			continue
		}
		missing = append(missing, text)
		missingAt = append(missingAt, i)
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	var fresh [][]float32
	var err error
	if kind == "" {
		fresh, err = c.inner.Embed(ctx, missing)
	} else {
		fresh, err = c.inner.EmbedKind(ctx, kind, missing)
	}
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(missing) {
		return nil, fmt.Errorf("cachedEmbedder: got %d vectors for %d texts", len(fresh), len(missing))
	}
	c.mu.Lock()
	for i, vec := range fresh {
		out[missingAt[i]] = vec
		c.cache[kind+"\x00"+embedindex.ContentHash(missing[i])] = vec
		c.dirty = true
	}
	c.mu.Unlock()
	return out, nil
}

func (c *cachedEmbedder) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(c.cache); err != nil {
		return
	}
	_ = os.WriteFile(c.path, buf.Bytes(), 0o600)
	c.dirty = false
}

// renderedSources pulls the ref= values out of a rendered evidence block, in
// rendered (ranked, budget-cut) order.
func renderedSources(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		// Evidence rows only. The block also carries guidance prose containing a
		// literal ref="f:<경로>" (fileOpenHint), which would otherwise take rank 0
		// and zero out the top1 metric.
		if !strings.HasPrefix(line, "- source=") {
			continue
		}
		i := strings.Index(line, `ref="`)
		if i < 0 {
			continue
		}
		rest := line[i+len(`ref="`):]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			continue
		}
		out = append(out, rest[:j])
	}
	return out
}

type recallSourceKind int

const (
	sourceKindOther recallSourceKind = iota
	sourceKindMessage
	sourceKindSummary
)

// parseRecallSource maps an evidence row's Source back to (session key, message
// index, kind). The three shapes recallPolarisEvidence emits are
// "msg#<i>/<role>" (current session), "cl:<key>#<i>/<role>" (cross-session,
// abbreviateSession has replaced the "client:" prefix), and "cl:<key> 요약"
// (semantic summary of another session).
func parseRecallSource(source, currentKey string) (string, int, recallSourceKind) {
	const summarySuffix = " 요약"
	if key, ok := strings.CutSuffix(source, summarySuffix); ok {
		return unabbreviateSession(key), -1, sourceKindSummary
	}
	head, tail, ok := strings.Cut(source, "#")
	if !ok {
		return "", -1, sourceKindOther
	}
	idxPart, _, _ := strings.Cut(tail, "/")
	idx, err := strconv.Atoi(idxPart)
	if err != nil {
		return "", -1, sourceKindOther
	}
	if head == "msg" {
		return currentKey, idx, sourceKindMessage
	}
	return unabbreviateSession(head), idx, sourceKindMessage
}

// unabbreviateSession inverts abbreviateSession (types.go).
func unabbreviateSession(key string) string {
	if rest, ok := strings.CutPrefix(key, "cl:"); ok {
		return "client:" + rest
	}
	return key
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
