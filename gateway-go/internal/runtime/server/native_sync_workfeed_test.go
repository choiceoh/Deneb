package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func TestNativeWorkFeedSourceRefHelpers(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	native := &nativeWorkFeedStore{store: store}
	if _, err := store.Append(workfeed.Item{
		ID: "a", Source: workfeed.SourceGroupwareApproval, RefID: "99178", Body: "phone",
	}); err != nil {
		t.Fatal(err)
	}
	active, err := native.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "99178")
	if err != nil || !active {
		t.Fatalf("HasActiveSourceRef = %v, %v", active, err)
	}
	if err := native.AckBySourceRef(workfeed.SourceGroupwareApproval, "99178"); err != nil {
		t.Fatal(err)
	}
	active, err = native.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "99178")
	if err != nil || active {
		t.Fatalf("post-ack HasActiveSourceRef = %v, %v", active, err)
	}
	if err := native.AckBySourceRef(workfeed.SourceGroupwareApproval, "99178"); err != nil {
		t.Fatalf("idempotent AckBySourceRef: %v", err)
	}
}
