package mailstore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type mailTestReranker struct {
	calls  int
	err    error
	before func() error
}

func (r *mailTestReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	r.calls++
	if r.before != nil {
		if err := r.before(); err != nil {
			return nil, err
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	scores := make([]float64, len(documents))
	for i, document := range documents {
		if strings.Contains(document, "preferred evidence") {
			scores[i] = 10
		} else {
			scores[i] = -float64(i)
		}
	}
	return scores, nil
}

func TestSearchContextReranksCandidatesWithoutHoldingStoreLock(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(
		t,
		s,
		contractMessage("retrieval-first", "INBOX", "계약 일정", "계약 일정 확정 공지", "2026-07-01"),
		contractMessage("preferred", "INBOX", "계약 일정 관련 안내", "preferred evidence", "2026-07-02"),
	)
	baseline := s.SearchContext(context.Background(), nil, "계약 일정", time.Time{}, 10)
	if len(baseline) < 2 || baseline[0].ID != "retrieval-first" {
		t.Fatalf("test baseline = %v, want retrieval-first first", messageIDs(baseline))
	}

	reranker := &mailTestReranker{before: func() error {
		unlocked := make(chan int, 1)
		go func() {
			s.mu.Lock()
			count := len(s.byKey)
			s.mu.Unlock()
			unlocked <- count
		}()
		select {
		case count := <-unlocked:
			if count != 2 {
				return errors.New("mailstore contents changed during rerank")
			}
			return nil
		case <-time.After(250 * time.Millisecond):
			return errors.New("mailstore lock held during rerank")
		}
	}}
	s.SetReranker(reranker)

	hits := s.SearchContext(context.Background(), nil, "계약 일정", time.Time{}, 10)
	if len(hits) < 2 || hits[0].ID != "preferred" {
		t.Fatalf("reranked order = %v, want preferred first", messageIDs(hits))
	}
	if reranker.calls != 1 || !containsMailReason(hits[0].RankReasons, "rerank") {
		t.Fatalf("rerank evidence: calls=%d reasons=%v", reranker.calls, hits[0].RankReasons)
	}
	stored, ok := s.Read("preferred", "", nil)
	if !ok || containsMailReason(stored.RankReasons, "rerank") {
		t.Fatalf("rerank mutated stored message: ok=%v reasons=%v", ok, stored.RankReasons)
	}
}

func TestMailRerankerFailsOpen(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(
		t,
		s,
		contractMessage("a", "INBOX", "계약 일정", "계약 일정 확정", "2026-07-01"),
		contractMessage("b", "INBOX", "계약 일정 관련 안내", "preferred evidence", "2026-07-02"),
	)
	baseline := s.SearchContext(context.Background(), nil, "계약 일정", time.Time{}, 10)
	reranker := &mailTestReranker{err: errors.New("sidecar unavailable")}
	s.SetReranker(reranker)

	hits := s.SearchContext(context.Background(), nil, "계약 일정", time.Time{}, 10)
	if !reflect.DeepEqual(messageIDs(hits), messageIDs(baseline)) {
		t.Fatalf("failure changed order: got %v want %v", messageIDs(hits), messageIDs(baseline))
	}
	for _, hit := range hits {
		if containsMailReason(hit.RankReasons, "rerank") {
			t.Fatalf("failure added rerank reason: %+v", hit)
		}
	}
}

func TestProjectHistoryReranksBeforeLimit(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(
		t,
		s,
		contractMessage("retrieval-first", "INBOX", "계약 일정", "계약 일정 확정", "2026-07-01"),
		contractMessage("preferred", "INBOX", "계약 일정 관련 안내", "preferred evidence", "2026-07-02"),
	)
	reranker := &mailTestReranker{}
	s.SetReranker(reranker)

	history, ok := s.ProjectHistoryContext(context.Background(), "계약 일정", time.Time{}, 1, 20)
	if !ok || len(history.Messages) != 1 || history.Messages[0].ID != "preferred" {
		t.Fatalf("project history = ok=%v ids=%v, want preferred", ok, messageIDs(history.Messages))
	}
	if !containsMailReason(history.Messages[0].RankReasons, "rerank") {
		t.Fatalf("project history reasons = %v", history.Messages[0].RankReasons)
	}
}
