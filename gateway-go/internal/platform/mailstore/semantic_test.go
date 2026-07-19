package mailstore

import (
	"context"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

type semanticMailEmbedder struct {
	mu    sync.Mutex
	kinds []string
}

func (e *semanticMailEmbedder) IsHealthy() bool { return true }

func (e *semanticMailEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, "passage")
	e.mu.Unlock()
	return semanticMailPassageVectors(texts), nil
}

func (e *semanticMailEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, kind)
	e.mu.Unlock()
	return semanticMailQueryVectors(texts), nil
}

func (e *semanticMailEmbedder) snapshotKinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.kinds...)
}

func semanticMailPassageVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch {
		case strings.Contains(text, "계약상 납기 지연"):
			out[i] = []float32{1, 0}
		case strings.Contains(text, "약한 관련 문서"):
			out[i] = []float32{0.30, float32(math.Sqrt(1 - 0.30*0.30))}
		default:
			out[i] = []float32{0, 1}
		}
	}
	return out
}

func semanticMailQueryVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "배송 일정 위험") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out
}

func TestSearchContextFindsSemanticOnlyMailAndUsesQueryRole(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(
		t,
		s,
		contractMessage("risk", "INBOX", "공급망 경보", "계약상 납기 지연 가능성이 커졌습니다", "2026-07-01"),
		contractMessage("other", "INBOX", "점심 메뉴", "구내식당 안내", "2026-07-02"),
	)
	embedder := &semanticMailEmbedder{}
	s.SetEmbedder(embedder, embedindex.WithSyncRefresh())

	hits := s.SearchContext(context.Background(), nil, "배송 일정 위험", time.Time{}, 10)
	if len(hits) == 0 || hits[0].ID != "risk" {
		t.Fatalf("semantic hits = %v, want risk first", messageIDs(hits))
	}
	if !containsMailReason(hits[0].RankReasons, "semantic") || containsMailReason(hits[0].RankReasons, "local_fts") {
		t.Fatalf("rank reasons = %v, want semantic-only", hits[0].RankReasons)
	}
	if want := []string{"passage", "query"}; !reflect.DeepEqual(embedder.snapshotKinds(), want) {
		t.Fatalf("embedding roles = %v, want %v", embedder.snapshotKinds(), want)
	}
}

func TestSearchContextRejectsSemanticOnlyHitBelowFloor(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(t, s, contractMessage("noise", "INBOX", "약한 관련 문서", "별도 본문", "2026-07-01"))
	s.SetEmbedder(&semanticMailEmbedder{}, embedindex.WithSyncRefresh())

	if hits := s.SearchContext(context.Background(), nil, "배송 일정 위험", time.Time{}, 10); len(hits) != 0 {
		t.Fatalf("below-floor semantic hits = %v", messageIDs(hits))
	}
}

func TestSearchContextRRFPromotesLexicalSemanticAgreement(t *testing.T) {
	s := newContractStore(t)
	defer s.Close()
	putContractMessages(
		t,
		s,
		contractMessage("both", "INBOX", "배송 일정 위험", "계약상 납기 지연", "2026-07-01"),
		contractMessage("lexical", "INBOX", "배송 일정 위험", "일반 공지", "2026-07-02"),
	)
	s.SetEmbedder(&semanticMailEmbedder{}, embedindex.WithSyncRefresh())

	hits := s.SearchContext(context.Background(), nil, "배송 일정 위험", time.Time{}, 10)
	if len(hits) < 2 || hits[0].ID != "both" {
		t.Fatalf("hybrid order = %v, want agreement first", messageIDs(hits))
	}
	if !containsMailReason(hits[0].RankReasons, "local_fts") || !containsMailReason(hits[0].RankReasons, "semantic") {
		t.Fatalf("agreement reasons = %v", hits[0].RankReasons)
	}
}

func containsMailReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
