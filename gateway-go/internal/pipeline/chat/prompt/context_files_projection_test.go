package prompt

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

const generatedHeader = "<!-- deneb:generated-fact-projection revision=126; source=.fact-mutations.jsonl; do-not-edit -->\n"

// The prompt package cannot import the marker constant without depending on
// domain/wiki at runtime, so the copy is pinned here instead.
func TestFactProjectionMarkerMatchesTheProducer(t *testing.T) {
	if !strings.HasPrefix(wiki.GeneratedFactProjectionMarker, factProjectionMarker) {
		t.Fatalf("marker drifted: prompt=%q wiki=%q", factProjectionMarker, wiki.GeneratedFactProjectionMarker)
	}
}

func TestGeneratedUserProjectionIsDroppedWhenMemoryProjectionIsPresent(t *testing.T) {
	files := []ContextFile{
		{Path: "AGENTS.md", Content: "rules"},
		{Path: "USER.md", Content: generatedHeader + "# USER\n\n## Identity\n- a\n"},
		{Path: "MEMORY.md", Content: generatedHeader + "# MEMORY\n\n- `a`: 1\n"},
	}

	kept := dropRedundantFactProjection(files)

	if len(kept) != 2 {
		t.Fatalf("kept %d files, want 2: %+v", len(kept), kept)
	}
	for _, f := range kept {
		if f.Path == "USER.md" {
			t.Fatal("generated USER.md should have been dropped")
		}
	}
}

func TestHandwrittenUserFileSurvives(t *testing.T) {
	files := []ContextFile{
		{Path: "USER.md", Content: "# USER\n\n선택님은 태양광 EPC 업무를 한다.\n"},
		{Path: "MEMORY.md", Content: generatedHeader + "# MEMORY\n\n- `a`: 1\n"},
	}

	if kept := dropRedundantFactProjection(files); len(kept) != 2 {
		t.Fatalf("hand-written USER.md must stay: %+v", kept)
	}
}

func TestUserProjectionSurvivesWithoutAMemoryProjection(t *testing.T) {
	files := []ContextFile{
		{Path: "USER.md", Content: generatedHeader + "# USER\n"},
		{Path: "MEMORY.md", Content: "# MEMORY\n\n손으로 쓴 기억\n"},
	}

	if kept := dropRedundantFactProjection(files); len(kept) != 2 {
		t.Fatalf("USER projection is the only fact view here: %+v", kept)
	}
}
