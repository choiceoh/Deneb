package knowledge

import (
	"encoding/json"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TestMemoryGetPage_ReturnsStructuralMetadata: a client showing only the body
// cannot tell a healthy project page from one with an empty stage or a stale
// superseded_by flag — the two conditions this plan's guards exist to surface.
func TestMemoryGetPage_ReturnsStructuralMetadata(t *testing.T) {
	page := &wiki.Page{
		Meta: wiki.Frontmatter{
			Title: "기아 AL광주 2공장 태양광", Category: "프로젝트", Code: "pl2-kia-epc-002",
			Stage: "계약협의", Client: "기아", Sites: []string{"광주 광산구"},
			Kinds: []string{"태양광/루프탑"}, Program: "기아-광주",
			Sources: []string{"d2026-08-21#79da86dd"}, Archived: true,
			SupersededBy: "프로젝트/pl2-kia-epc-003/대표.md",
		},
		Body: "## 현재 상태",
	}
	store := peopleWikiStore(map[string]*wiki.Page{"프로젝트/pl2-kia-epc-002/대표.md": page})
	handler := memoryGetPage(MemoryDeps{Store: func() (MemorySearcher, error) { return store, nil }})

	params, _ := json.Marshal(map[string]string{"path": "프로젝트/pl2-kia-epc-002/대표.md"})
	resp := handler(authedCtx(), &protocol.RequestFrame{ID: "1", Params: params})
	if resp == nil || !resp.OK {
		t.Fatalf("get_page failed: %+v err=%+v", resp, resp.Error)
	}
	body, _ := json.Marshal(resp.Payload)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}

	for key, want := range map[string]any{
		"stage": "계약협의", "client": "기아", "program": "기아-광주",
		"supersededBy": "프로젝트/pl2-kia-epc-003/대표.md", "archived": true,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	for _, key := range []string{"sites", "kinds", "sources"} {
		if arr, ok := got[key].([]any); !ok || len(arr) != 1 {
			t.Errorf("%s = %v, want a single-entry list", key, got[key])
		}
	}
}
