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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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
	storeDir := t.TempDir()

	type bucket struct{ total, anyHit, top1Hit, poolHit, pool10, hit8, renderedSum, emptySum int }
	buckets := map[string]*bucket{}
	overall := &bucket{}
	abstention := 0
	poolSize, rankedSize, dedupHits, filterHits := 0, 0, 0, 0

	for qi, q := range questions {
		// Abstention questions have no evidence to retrieve; a retrieval metric
		// has nothing to score. Counted separately.
		if strings.HasSuffix(q.QuestionID, "_abs") || len(q.AnswerSessionIDs) == 0 {
			abstention++
			continue
		}

		store, err := polaris.NewStore(filepath.Join(storeDir, fmt.Sprintf("lm-%d.db", qi)))
		if err != nil {
			t.Fatalf("q%d: polaris store: %v", qi, err)
		}
		bridge := polaris.NewBridge(transcript.NewMemoryTranscriptStore(), store, logger)
		sessionKey := "client:lm:" + q.QuestionID

		// Ingest the whole history as ONE Deneb session in chronological order —
		// the shape a long-lived chat takes in production. Track message index →
		// haystack session id for scoring.
		evidenceSessions := map[string]bool{}
		for _, id := range q.AnswerSessionIDs {
			evidenceSessions[id] = true
		}
		msgSession := []string{}
		for si, sess := range q.HaystackSessions {
			at := lmParseDate(q.HaystackDates[si]).UnixMilli()
			for _, turn := range sess {
				msg := chatport.ChatMessage{
					Role:      turn.Role,
					Content:   chatport.MarshalJSONString(turn.Content),
					Timestamp: at,
				}
				if err := bridge.Append(sessionKey, msg); err != nil {
					t.Fatalf("q%d: append: %v", qi, err)
				}
				msgSession = append(msgSession, q.HaystackSessionID[si])
			}
		}
		// The question itself is the newest user message, as in production —
		// the maxIdx guard then skips exactly it.
		questionAt := lmParseDate(q.QuestionDate)
		_ = bridge.Append(sessionKey, chatport.ChatMessage{
			Role: "user", Content: chatport.MarshalJSONString(q.Question),
			Timestamp: questionAt.UnixMilli(),
		})
		msgSession = append(msgSession, "__question__")

		// The production path, verbatim: query derivation → polaris source →
		// ranking → budget-cut rendering.
		queries := searchQueries(q.Question)
		candidates := recallPolarisEvidence(context.Background(), bridge, sessionKey, queries)
		// Third diagnostic: is the 3-hits-per-query quota the ceiling, or FTS
		// itself? Same store, same queries, limit 10 — a measurement-only call,
		// not a production path.
		pool10Hit := false
		for _, query := range queries {
			hits, err := store.SearchMessages(sessionKey, query, 10)
			if err != nil {
				continue
			}
			for _, h := range hits {
				if h.MsgIndex < len(msgSession) && evidenceSessions[msgSession[h.MsgIndex]] {
					pool10Hit = true
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
		poolHit := false
		for _, ev := range candidates {
			if idx, ok := polarisMsgIndex(ev.Source); ok && idx < len(msgSession) && evidenceSessions[msgSession[idx]] {
				poolHit = true
				break
			}
		}
		evidence := rankRecallEvidence(append([]recallEvidence(nil), candidates...), queries, q.Question, hasCue(q.Question), questionAt)
		// rankRecallEvidence cuts to recallEvidenceBudget(cue) internally, so the
		// budget-8 number needs its own ranking pass with cue=true — slicing the
		// returned rows to 8 would silently re-measure the same 4.
		cueRanked := rankRecallEvidence(append([]recallEvidence(nil), candidates...), queries, q.Question, true, questionAt)
		// Stage isolation: where does a pooled hit die — dedup or cross-subject filter?
		afterDedup := dedupRecallEvidence(append([]recallEvidence(nil), candidates...))
		afterFilter := filterCrossSubjectEvidence(append([]recallEvidence(nil), afterDedup...), q.Question)
		hitIn := func(rows []recallEvidence) bool {
			for _, ev := range rows {
				if idx, ok := polarisMsgIndex(ev.Source); ok && idx < len(msgSession) && evidenceSessions[msgSession[idx]] {
					return true
				}
			}
			return false
		}
		hitAt8 := hitIn(cueRanked)
		if hitIn(afterDedup) {
			dedupHits++
		}
		if hitIn(afterFilter) {
			filterHits++
		}
		poolSize += len(candidates)
		rankedSize += len(cueRanked)
		block, _ := formatRecallEvidenceAt(evidence, questionAt, true)

		rendered := renderedMsgIndices(block)
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
		for rank, idx := range rendered {
			if idx >= 0 && idx < len(msgSession) && evidenceSessions[msgSession[idx]] {
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

		if (qi+1)%50 == 0 {
			t.Logf("progress: %d/%d", qi+1, len(questions))
		}
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

// renderedMsgIndices parses the polaris row refs ("msg#<idx>/<role>") out of a
// rendered evidence block, in rendered (ranked) order.
func renderedMsgIndices(block string) []int {
	var out []int
	for _, line := range strings.Split(block, "\n") {
		i := strings.Index(line, `ref="msg#`)
		if i < 0 {
			continue
		}
		rest := line[i+len(`ref="msg#`):]
		j := strings.IndexAny(rest, "/\"")
		if j < 0 {
			continue
		}
		if n, err := strconv.Atoi(rest[:j]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
