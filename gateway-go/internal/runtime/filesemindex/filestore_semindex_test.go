package filesemindex

import (
	"context"
	"testing"
	"time"
)

func TestNewRequiresStore(t *testing.T) {
	if got := New(nil, nil, nil); got != nil {
		t.Fatalf("New(nil) = %#v, want disabled service", got)
	}
}

func TestDisabledServiceDegradesWithoutWork(t *testing.T) {
	svc := &Service{}
	hits, err := svc.Search(context.Background(), "proposal", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("Search on disabled service = (%v, %v), want empty success", hits, err)
	}
	if task := svc.Task(); task != nil {
		t.Fatalf("Task on disabled service = %#v, want nil", task)
	}
	if adapter := svc.KnowledgeAdapter(); adapter != nil {
		t.Fatalf("KnowledgeAdapter on disabled service = %#v, want nil", adapter)
	}
	if recall := svc.Recall(context.Background(), "proposal", 5); len(recall) != 0 {
		t.Fatalf("Recall on disabled service = %v, want empty", recall)
	}
	// Mutation hooks are intentionally safe while semantic indexing is disabled.
	svc.Remove("/old.pdf")
	svc.Rename("/old.pdf", "/new.pdf")
}

func TestFileServerModifiedMillis(t *testing.T) {
	stamp := "2026-07-11T08:15:30Z"
	want, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileServerModifiedMillis(stamp); got != want.UnixMilli() {
		t.Fatalf("valid timestamp = %d, want %d", got, want.UnixMilli())
	}
	for _, input := range []string{"", "not-a-time"} {
		if got := fileServerModifiedMillis(input); got != 0 {
			t.Errorf("fileServerModifiedMillis(%q) = %d, want 0", input, got)
		}
	}
}
