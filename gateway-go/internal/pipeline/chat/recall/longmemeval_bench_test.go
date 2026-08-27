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

	type bucket struct{ total, anyHit, top1Hit, renderedSum, emptySum int }
	buckets := map[string]*bucket{}
	overall := &bucket{}
	abstention := 0

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
		evidence := recallPolarisEvidence(context.Background(), bridge, sessionKey, queries)
		evidence = rankRecallEvidence(evidence, queries, q.Question, hasCue(q.Question), questionAt)
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
		t.Logf("%-28s n=%-3d evidence-hit=%-7s top1=%-7s avg-rows=%.1f empty=%s",
			k, b.total, pct(b.anyHit, b.total), pct(b.top1Hit, b.total),
			float64(b.renderedSum)/float64(maxInt(b.total, 1)), pct(b.emptySum, b.total))
	}
	t.Logf("%-28s n=%-3d evidence-hit=%-7s top1=%-7s avg-rows=%.1f empty=%s (abstention excluded: %d)",
		"OVERALL", overall.total, pct(overall.anyHit, overall.total), pct(overall.top1Hit, overall.total),
		float64(overall.renderedSum)/float64(maxInt(overall.total, 1)), pct(overall.emptySum, overall.total), abstention)
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
