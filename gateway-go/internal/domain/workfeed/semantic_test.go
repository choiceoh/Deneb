package workfeed

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

type semanticWorkFeedEmbedder struct {
	mu          sync.Mutex
	kinds       []string
	failPass    bool
	fingerprint string
}

func (e *semanticWorkFeedEmbedder) IsHealthy() bool { return true }

func (e *semanticWorkFeedEmbedder) EmbeddingFingerprint() string { return e.fingerprint }

func (e *semanticWorkFeedEmbedder) EmbeddingDimensions() int { return 3 }

func (e *semanticWorkFeedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, "passage")
	e.mu.Unlock()
	if e.failPass {
		return nil, errors.New("embedding unavailable")
	}
	return semanticWorkFeedPassageVectors(texts), nil
}

func (e *semanticWorkFeedEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, kind)
	e.mu.Unlock()
	return semanticWorkFeedQueryVectors(texts), nil
}

func (e *semanticWorkFeedEmbedder) snapshotKinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.kinds...)
}

func semanticWorkFeedPassageVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch {
		case strings.Contains(text, "계약상 납기 지연"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(text, "약한 관련 카드"):
			out[i] = []float32{0.30, float32(math.Sqrt(1 - 0.30*0.30)), 0}
		default:
			out[i] = []float32{0, 1, 0}
		}
	}
	return out
}

func semanticWorkFeedQueryVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch {
		case strings.Contains(text, "배송 일정 위험"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(text, "완전히 무관한"):
			out[i] = []float32{0, 0, 1}
		default:
			out[i] = []float32{0, 1, 0}
		}
	}
	return out
}

func TestAppendIfNewSemanticallyGroupsRelatedCardsWithoutDeduping(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	embedder := &semanticWorkFeedEmbedder{}
	store.SetEmbedder(embedder, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()

	first, created, err := store.AppendIfNew(Item{
		ID: "risk-mail", Source: SourceMailReport, Title: "공급망 경보",
		Body: "계약상 납기 지연 가능성이 커졌습니다.", CreatedAtMs: now - 1_000,
	})
	if err != nil || !created {
		t.Fatalf("append first = created %v, err %v", created, err)
	}
	second, created, err := store.AppendIfNew(Item{
		ID: "risk-board", Source: SourceGroupwareBoard, Title: "배송 계획 재검토",
		Body: "배송 일정 위험 때문에 고객 공지를 준비해야 합니다.", CreatedAtMs: now,
	})
	if err != nil || !created {
		t.Fatalf("append second = created %v, err %v", created, err)
	}
	if first.ClusterID != "" || len(first.RelatedIDs) != 0 {
		t.Fatalf("first append should precede grouping: %+v", first)
	}
	if second.ClusterID == "" || !reflect.DeepEqual(second.RelatedIDs, []string{"risk-mail"}) {
		t.Fatalf("second grouping = cluster %q related %v", second.ClusterID, second.RelatedIDs)
	}

	items, total, err := store.List(10, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("semantic grouping removed a card: total %d items %+v", total, items)
	}
	byID := workFeedItemsByID(items)
	if byID["risk-mail"].ClusterID != second.ClusterID || byID["risk-board"].ClusterID != second.ClusterID {
		t.Fatalf("cluster IDs differ: mail=%q board=%q", byID["risk-mail"].ClusterID, byID["risk-board"].ClusterID)
	}
	if !reflect.DeepEqual(byID["risk-mail"].RelatedIDs, []string{"risk-board"}) {
		t.Fatalf("persisted first related IDs = %v", byID["risk-mail"].RelatedIDs)
	}
	if want := []string{"passage", "query"}; !reflect.DeepEqual(embedder.snapshotKinds(), want) {
		t.Fatalf("embedding roles = %v, want %v", embedder.snapshotKinds(), want)
	}
}

func TestAppendIfNewSemanticGroupingRejectsWeakOrUnrelatedHits(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	store.SetEmbedder(&semanticWorkFeedEmbedder{}, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()

	if _, _, err := store.AppendIfNew(Item{
		ID: "weak", Source: SourceProactive, Title: "약한 관련 카드",
		Body: "별도 검토 자료입니다.", CreatedAtMs: now - 1_000,
	}); err != nil {
		t.Fatalf("append weak candidate: %v", err)
	}
	out, created, err := store.AppendIfNew(Item{
		ID: "unrelated", Source: SourceProactive, Title: "완전히 무관한 업무",
		Body: "사무실 좌석 배치 변경 안내입니다.", CreatedAtMs: now,
	})
	if err != nil || !created {
		t.Fatalf("append unrelated = created %v, err %v", created, err)
	}
	if out.ClusterID != "" || len(out.RelatedIDs) != 0 {
		t.Fatalf("unrelated card grouped: %+v", out)
	}
}

func TestAppendIfNewSemanticGroupingRejectsUncalibratedModel(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	store.SetEmbedder(&semanticWorkFeedEmbedder{fingerprint: "future-embedder:3"}, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()
	for _, item := range []Item{
		{ID: "risk-mail", Source: SourceMailReport, Title: "공급망 경보", Body: "계약상 납기 지연 가능성이 커졌습니다.", CreatedAtMs: now - 1_000},
		{ID: "risk-board", Source: SourceGroupwareBoard, Title: "배송 계획 재검토", Body: "배송 일정 위험 때문에 고객 공지를 준비해야 합니다.", CreatedAtMs: now},
	} {
		if _, created, err := store.AppendIfNew(item); err != nil || !created {
			t.Fatalf("append %s = created %v err %v", item.ID, created, err)
		}
	}
	items, _, err := store.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ClusterID != "" || len(item.RelatedIDs) != 0 {
			t.Fatalf("uncalibrated grouping = %+v", item)
		}
	}
}

func TestAppendIfNewExactDuplicateBypassesSemanticGrouping(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	embedder := &semanticWorkFeedEmbedder{}
	store.SetEmbedder(embedder, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()
	item := Item{
		ID: "original", Source: SourceMailReport, Title: "공급망 경보",
		Body: "계약상 납기 지연 가능성이 커졌습니다.", CreatedAtMs: now,
	}
	if _, created, err := store.AppendIfNew(item); err != nil || !created {
		t.Fatalf("append original = created %v, err %v", created, err)
	}
	duplicate := item
	duplicate.ID = "duplicate"
	got, created, err := store.AppendIfNew(duplicate)
	if err != nil || created || got.ID != "original" {
		t.Fatalf("append duplicate = item %q created %v err %v", got.ID, created, err)
	}
	if kinds := embedder.snapshotKinds(); len(kinds) != 0 {
		t.Fatalf("duplicate path called embedder: %v", kinds)
	}
}

func TestAppendIfNewPersistsCardWhenSemanticGroupingFails(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	store.SetEmbedder(&semanticWorkFeedEmbedder{failPass: true}, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()

	for _, item := range []Item{
		{ID: "first", Source: SourceMailReport, Title: "공급망 경보", Body: "계약상 납기 지연 가능성", CreatedAtMs: now - 1_000},
		{ID: "second", Source: SourceGroupwareBoard, Title: "배송 계획", Body: "배송 일정 위험 재검토", CreatedAtMs: now},
	} {
		if _, created, err := store.AppendIfNew(item); err != nil || !created {
			t.Fatalf("append %s = created %v, err %v", item.ID, created, err)
		}
	}
	items, total, err := store.List(10, true)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("feed after semantic failure = total %d items %+v err %v", total, items, err)
	}
	for _, item := range items {
		if item.ClusterID != "" || len(item.RelatedIDs) != 0 {
			t.Fatalf("failed semantic call left grouping metadata: %+v", item)
		}
	}
}

func TestApplySemanticGroupDoesNotMergeTouchedClusters(t *testing.T) {
	items := []Item{
		{ID: "a", ClusterID: "cluster-a", RelatedIDs: []string{"b"}},
		{ID: "b", ClusterID: "cluster-a", RelatedIDs: []string{"a"}},
		{ID: "c", ClusterID: "cluster-c"},
	}
	item := applySemanticGroup(items, Item{ID: "d"}, []string{"b", "c"})
	if item.ClusterID != "cluster-a" {
		t.Fatalf("selected cluster = %q, want cluster-a", item.ClusterID)
	}
	if items[0].ClusterID != "cluster-a" || items[1].ClusterID != "cluster-a" || items[2].ClusterID != "cluster-c" {
		t.Fatalf("clusters were transitively merged: %+v", items)
	}
	if !reflect.DeepEqual(item.RelatedIDs, []string{"a", "b"}) {
		t.Fatalf("new related IDs = %v", item.RelatedIDs)
	}
}

func TestApplySemanticGroupCapsClusterGrowth(t *testing.T) {
	items := make([]Item, workFeedSemanticMaxCluster)
	for i := range items {
		items[i] = Item{ID: string(rune('a' + i)), ClusterID: "full-cluster"}
	}
	item := applySemanticGroup(items, Item{ID: "new"}, []string{items[0].ID})
	if item.ClusterID != "" || len(item.RelatedIDs) != 0 {
		t.Fatalf("full cluster accepted another member: %+v", item)
	}
}

func TestAckReconcilesSemanticGroupMembership(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	defer store.Close()
	store.SetEmbedder(&semanticWorkFeedEmbedder{}, embedindex.WithSyncRefresh())
	now := time.Now().UnixMilli()
	for _, item := range []Item{
		{ID: "risk-mail", Source: SourceMailReport, Title: "공급망 경보", Body: "계약상 납기 지연 가능성이 커졌습니다.", CreatedAtMs: now - 1_000},
		{ID: "risk-board", Source: SourceGroupwareBoard, Title: "배송 계획 재검토", Body: "배송 일정 위험 때문에 고객 공지를 준비해야 합니다.", CreatedAtMs: now},
	} {
		if _, created, err := store.AppendIfNew(item); err != nil || !created {
			t.Fatalf("append %s = created %v err %v", item.ID, created, err)
		}
	}
	acked, err := store.Ack("risk-mail")
	if err != nil || acked.ClusterID != "" || len(acked.RelatedIDs) != 0 {
		t.Fatalf("acked item kept cluster: %+v err=%v", acked, err)
	}
	items, _, err := store.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ClusterID != "" || len(item.RelatedIDs) != 0 {
			t.Fatalf("orphaned related metadata after ack: %+v", item)
		}
	}
}

func workFeedItemsByID(items []Item) map[string]Item {
	out := make(map[string]Item, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
