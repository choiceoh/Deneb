package genesis

import (
	"strings"
	"testing"
)

// The judge needs a referent for "존재하지 않는 도구". Without one it falls back to
// the incumbent SKILL.md and rejects every repair of a stale skill as
// fabrication — youtube-summary-cards named web(), YouTube had moved to watch,
// and the candidate saying so was refused as "검증 불가한 주장".
func TestRegisteredToolsSectionNamesTheRealToolset(t *testing.T) {
	e := &Evolver{}
	e.SetKnownTools(func() []string { return []string{"watch", "web", "read_spillover"} })

	got := e.registeredToolsSection()
	for _, want := range []string{"watch", "web", "read_spillover"} {
		if !strings.Contains(got, want) {
			t.Errorf("section omits %q: %s", want, got)
		}
	}
	// Sorted, so two runs of the same registry produce the same payload and a
	// verdict diff is never map-iteration noise.
	if strings.Index(got, "read_spillover") > strings.Index(got, "watch") {
		t.Error("tool list is not sorted")
	}
	// The instruction matters as much as the list: without it the drifted
	// "원본에 없으면 거부" rule in the live system prompt still wins.
	if !strings.Contains(got, "원본 SKILL.md에 없더라도") {
		t.Error("section does not tell the judge the list overrides the incumbent")
	}
}

// No registry wired — say nothing rather than assert an empty toolset. An empty
// list would read as "no tools exist" and reject everything.
func TestRegisteredToolsSectionSilentWithoutRegistry(t *testing.T) {
	if got := (&Evolver{}).registeredToolsSection(); got != "" {
		t.Fatalf("section = %q, want empty when no registry is wired", got)
	}
	e := &Evolver{}
	e.SetKnownTools(func() []string { return nil })
	if got := e.registeredToolsSection(); got != "" {
		t.Fatalf("section = %q, want empty for an empty registry", got)
	}
}
