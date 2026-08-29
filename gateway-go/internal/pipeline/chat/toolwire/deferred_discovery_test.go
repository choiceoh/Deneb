package toolwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/fetchops"
)

// discoveryRegistry is a mockRegistrar that can also answer fetch_tools, so a
// deferred tool can be searched for the way a live turn searches for it.
type discoveryRegistry struct {
	defs map[string]toolport.ToolDef
}

func (d *discoveryRegistry) RegisterTool(def toolport.ToolDef) { d.defs[def.Name] = def }

func (d *discoveryRegistry) HasTool(name string) bool { _, ok := d.defs[name]; return ok }

func (d *discoveryRegistry) DeferredToolDef(name string) (toolport.ToolDef, bool) {
	def, ok := d.defs[name]
	if !ok || !def.Deferred {
		return toolport.ToolDef{}, false
	}
	return def, true
}

func (d *discoveryRegistry) DeferredSummaries() []toolport.DeferredToolSummary {
	out := make([]toolport.DeferredToolSummary, 0, len(d.defs))
	for _, def := range d.defs {
		if def.Deferred && !def.Hidden {
			out = append(out, toolport.DeferredToolSummary{Name: def.Name, Description: def.Description})
		}
	}
	return out
}

// TestDeferredToolsStayReachableByKoreanQuery guards the 2026-08-29 audit's
// demotions. Moving a tool off the wire is only safe while fetch_tools can find
// it again from the words a Korean turn would actually use — a description that
// drifts away from its trigger phrasing turns a deferred tool into a silently
// dead one, with nothing failing to say so.
//
// Semantic ranking is not wired here on purpose: this asserts the lexical floor,
// which is what a gateway without the embedder falls back to.
func TestDeferredToolsStayReachableByKoreanQuery(t *testing.T) {
	registry := &discoveryRegistry{defs: map[string]toolport.ToolDef{}}
	RegisterCoreTools(registry, &tooldeps.CoreToolDeps{
		WorkspaceDir: t.TempDir(),
		Browser:      tooldeps.BrowserDeps{BaseURL: func() string { return "http://127.0.0.1:1" }},
	})

	RegisterPersonaTools(registry, t.TempDir())
	// notebook needs a live store to register; deal_ledger rides the wiki deps.
	// Both hold the same auto-pinned deal evidence, so their reachability is
	// part of this contract (2026-08-30).
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterNotebookTool(registry, &tooldeps.NotebookDeps{Store: store})

	fetch := fetchops.ToolFetchTools(registry)
	ctx := toolport.WithDeferredActivation(context.Background(), toolport.NewDeferredActivation())

	for _, tc := range []struct {
		query string
		want  string
	}{
		// The 2026-08-29 demotions.
		{"이 xlsx 좀 봐줘", "office"},
		{"워드 문서 편집", "office"},
		{"무엇을 하는 코드 찾기", "code_search"},
		{"장기 목표 설정", "goal"},
		{"화면에 띄워줘", "workstation"},
		// Tools that were deferred long before, and that the same audit found
		// unreachable in Korean: 8 of these queries returned the wrong tool (or
		// nothing) until the descriptions picked up the phrasing a user
		// actually types. Every one of them had zero recorded calls.
		{"조직도 그려줘", "diagram"},
		{"흐름도 그려줘", "diagram"},
		{"매출 그래프 그려줘", "chart"},
		{"이미지에서 텍스트 추출", "ocr"},
		{"파일 보내줘", "send_file"},
		{"이거 전송해줘", "send_file"},
		{"크롬으로 로그인해서 봐줘", "browser"},
		{"게이트웨이 상태 관찰", "observe"},
		{"앞으로 이렇게 해줘", "preference"},
		{"이 녹음 정리해줘", "transcribe"},
		// The deal-evidence pair: the notebooks are filled by mail analysis with
		// 견적/계약 extractions, but neither tool said "deal" in a way a Korean
		// turn would type, and both sat at zero calls (2026-08-30 audit).
		{"이 거래처 견적 이력", "notebook"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			input, err := json.Marshal(map[string]any{"query": tc.query})
			if err != nil {
				t.Fatal(err)
			}
			out, err := fetch(ctx, input)
			if err != nil {
				t.Fatalf("fetch_tools(%q): %v", tc.query, err)
			}
			if !strings.Contains(out, "## "+tc.want+"\n") {
				t.Fatalf("%q did not surface %s:\n%s", tc.query, tc.want, out)
			}
		})
	}
}

// TestFetchToolsDoesNotDenyEagerTools is the registration-side half of the
// fetch_tools fix: whatever RegisterCoreTools leaves eager must come back as
// "already in your tools array", never as "not found" — the old answer read as
// "no such tool" and cost a turn every time the model asked for something it
// already held.
func TestFetchToolsDoesNotDenyEagerTools(t *testing.T) {
	registry := &discoveryRegistry{defs: map[string]toolport.ToolDef{}}
	RegisterCoreTools(registry, &tooldeps.CoreToolDeps{
		WorkspaceDir: t.TempDir(),
		Browser:      tooldeps.BrowserDeps{BaseURL: func() string { return "http://127.0.0.1:1" }},
	})

	var eager []string
	for name, def := range registry.defs {
		if !def.Deferred {
			eager = append(eager, name)
		}
	}
	if len(eager) == 0 {
		t.Fatal("no eager tools registered; the fixture is wrong")
	}

	fetch := fetchops.ToolFetchTools(registry)
	ctx := toolport.WithDeferredActivation(context.Background(), toolport.NewDeferredActivation())
	input, err := json.Marshal(map[string]any{"names": eager})
	if err != nil {
		t.Fatal(err)
	}
	out, err := fetch(ctx, input)
	if err != nil {
		t.Fatalf("fetch_tools: %v", err)
	}
	if strings.Contains(out, "not found") {
		t.Fatalf("an eager tool was reported missing:\n%s", out)
	}
	for _, name := range eager {
		if !strings.Contains(out, "- "+name+": already in your tools array") {
			t.Errorf("%s missing the already-on-wire line", name)
		}
	}
}
