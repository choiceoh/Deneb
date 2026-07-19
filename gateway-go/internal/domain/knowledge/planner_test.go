package knowledge

import (
	"context"
	"testing"
	"time"
)

type describedMockAdapter struct {
	mockAdapter
	descriptor SourceDescriptor
}

func (m *describedMockAdapter) Descriptor() SourceDescriptor { return m.descriptor }

func TestPlanRecallDefaultsToCoverageAndPrioritizesFileIntent(t *testing.T) {
	router := New(
		&describedMockAdapter{mockAdapter: mockAdapter{layer: LayerWiki}, descriptor: SourceDescriptor{
			Layer: LayerWiki, Name: "wiki", Capabilities: []Capability{CapabilityLexical, CapabilitySemantic},
		}},
		&describedMockAdapter{mockAdapter: mockAdapter{layer: LayerFiles}, descriptor: SourceDescriptor{
			Layer: LayerFiles, Name: "files", Capabilities: []Capability{CapabilityLexical, CapabilitySemantic, CapabilityCode},
		}},
	)

	plan := router.PlanRecall("router.go 함수 구현", 5, RecallOptions{})
	if len(plan.Sources) != 2 {
		t.Fatalf("default plan sources = %d, want all 2", len(plan.Sources))
	}
	if plan.Sources[0].Source.Layer != LayerFiles || plan.Sources[0].Reason != "file-or-code intent" {
		t.Fatalf("file intent plan = %+v", plan.Sources)
	}

	narrow := router.PlanRecall("배포 결정", 5, RecallOptions{Layers: []Layer{LayerWiki}})
	if len(narrow.Sources) != 1 || narrow.Sources[0].Source.Layer != LayerWiki {
		t.Fatalf("explicit wiki plan = %+v", narrow.Sources)
	}

	code := router.PlanRecall("구현", 5, RecallOptions{Capabilities: []Capability{CapabilityCode}})
	if len(code.Sources) != 1 || code.Sources[0].Source.Layer != LayerFiles {
		t.Fatalf("code capability plan = %+v", code.Sources)
	}
}

func TestRecallPacketAppliesScopeAndNormalizesTypedProvenance(t *testing.T) {
	now := time.Now().UnixMilli()
	wikiAdapter := &describedMockAdapter{
		mockAdapter: mockAdapter{layer: LayerWiki, results: []Result{
			{Ref: Ref{Layer: LayerWiki, ID: "프로젝트/탑솔라/대표.md"}, Snippet: "keep", Score: 0.9, Time: now - int64(2*time.Hour/time.Millisecond)},
			{Ref: Ref{Layer: LayerWiki, ID: "프로젝트/다른곳/대표.md"}, Snippet: "drop", Score: 1.0},
		}},
		descriptor: SourceDescriptor{
			Layer: LayerWiki, Name: "wiki", Capabilities: []Capability{CapabilityLexical},
			Sync: SyncContract{
				StableID: "path", Cursor: "generation", ChangeDetection: "hash", DeletionDetection: "tombstone",
				FreshnessTargetMillis: int64(time.Hour / time.Millisecond), AuthorizationBoundary: "workspace",
			},
		},
	}
	packet := New(wikiAdapter).RecallPacket(context.Background(), "탑솔라", 5, RecallOptions{
		Scopes: []string{"wiki:프로젝트/탑솔라"},
	})
	if len(packet.Results) != 1 || packet.Results[0].Snippet != "keep" {
		t.Fatalf("scoped results = %+v", packet.Results)
	}
	provenance := packet.Results[0].Provenance
	if provenance.Source != "wiki" || provenance.StableID != "프로젝트/탑솔라/대표.md" || provenance.ContentHash == "" {
		t.Fatalf("provenance = %+v", provenance)
	}
	if provenance.Freshness.State != FreshnessStale {
		t.Fatalf("freshness = %+v, want stale", provenance.Freshness)
	}
	if packet.Plan.Scopes[0] != "wiki:프로젝트/탑솔라" {
		t.Fatalf("plan scopes = %v", packet.Plan.Scopes)
	}
}

func TestParseLayerNameAcceptsCatalogNamesAndPrefixes(t *testing.T) {
	for _, test := range []struct {
		in   string
		want Layer
	}{
		{"wiki", LayerWiki}, {"w", LayerWiki}, {"files", LayerFiles}, {"f", LayerFiles},
	} {
		got, ok := ParseLayerName(test.in)
		if !ok || got != test.want {
			t.Errorf("ParseLayerName(%q) = %q, %v; want %q", test.in, got, ok, test.want)
		}
	}
	if _, ok := ParseLayerName("unknown"); ok {
		t.Fatal("unknown source accepted")
	}
}
