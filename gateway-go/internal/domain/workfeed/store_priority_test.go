package workfeed

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func nowMs() int64 { return time.Now().UnixMilli() }

func mustAppend(t *testing.T, s *Store, item Item) {
	t.Helper()
	if _, err := s.Append(item); err != nil {
		t.Fatalf("append %q: %v", item.ID, err)
	}
}

func TestInferPriority(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		{"🔴 긴급: 서버 다운", PriorityUrgent},
		{"긴급 처리 요망", PriorityUrgent},
		{"asap please review", PriorityUrgent},
		{"🟠 중요: 계약 마감 임박", PriorityHigh},
		{"중요한 회의 정리 필요", PriorityHigh},
		{"deadline tomorrow", PriorityHigh},
		{"🔵 참고용 메모", PriorityLow},
		{"fyi: build passed", PriorityLow},
		{"오늘 점심 메뉴 추천", PriorityNormal},
		{"평범한 업무 항목", PriorityNormal},
		{"🔴 긴급 결제 + 🔵 참고 자료", PriorityUrgent}, // highest match wins
	}
	for _, c := range cases {
		if got := inferPriority(Item{Body: c.body}); got != c.want {
			t.Errorf("inferPriority(%q) = %d, want %d", c.body, got, c.want)
		}
	}
}

// The feed orders by priority first, so an urgent item stays above a newer
// normal one — a chief-of-staff briefing, not a reverse-chronological log.
func TestListSortsByPriorityThenRecency(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	now := nowMs()

	mustAppend(t, store, Item{ID: "old-urgent", Source: SourceProactive, Body: "🔴 긴급 서버 점검", CreatedAtMs: now - 100_000})
	mustAppend(t, store, Item{ID: "new-normal", Source: SourceProactive, Body: "일반 업무 메모", CreatedAtMs: now})
	mustAppend(t, store, Item{ID: "mid-high", Source: SourceProactive, Body: "🟠 중요 계약 검토", CreatedAtMs: now - 50_000})

	items, _, err := store.List(10, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"old-urgent", "mid-high", "new-normal"}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, id := range want {
		if items[i].ID != id {
			t.Errorf("position %d = %q (priority %d), want %q", i, items[i].ID, items[i].Priority, id)
		}
	}
}

// Retention bounds the stored items: oldest acked are dropped past the cap, but
// an unread item is never dropped even if it is the oldest.
func TestPruneRetentionKeepsActiveDropsOldAcked(t *testing.T) {
	items := make([]Item, 0, maxRetained+60)
	for i := range maxRetained + 50 {
		items = append(items, Item{ID: fmt.Sprintf("acked-%d", i), Status: StatusAcked, CreatedAtMs: int64(i + 1000)})
	}
	// The oldest item of all — but unread, so it must survive.
	items = append(items, Item{ID: "old-unread", Status: StatusUnread, CreatedAtMs: 1})

	pruned := pruneRetention(items)

	if len(pruned) != maxRetained+1 {
		t.Fatalf("pruned len = %d, want %d (cap + the kept unread)", len(pruned), maxRetained+1)
	}
	found := false
	for _, it := range pruned {
		if it.ID == "old-unread" {
			found = true
		}
	}
	if !found {
		t.Error("the oldest unread item was dropped by retention; active items must survive")
	}
}
