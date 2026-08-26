package toolpreset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sessions_spawn schema tells the parent model what each preset can do, and
// that description is how it chooses. researcher's said "no file writes" while
// researcherTools deliberately keeps wiki/knowledge write sub-actions ("분석 →
// 위키 갱신" doctrine) — an understatement that pushes the parent to escalate to
// implementer, which additionally grants exec/write/edit, for work researcher
// could already do.
func TestSpawnPresetDescriptionMatchesTheResearcherWriteSurface(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "chat", "toolwire", "schema", "tool_schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	idx := strings.Index(body, "Tool preset restricting which tools the sub-agent can use")
	if idx < 0 {
		t.Fatal("sessions_spawn tool_preset description not found")
	}
	desc := body[idx:]
	if end := strings.Index(desc, "\""); end > 0 {
		desc = desc[:end]
	}

	if strings.Contains(desc, "no file writes") {
		t.Error("description still claims researcher cannot write; wiki/knowledge writes are deliberate")
	}
	if !strings.Contains(desc, "wiki/knowledge KEEP their write actions") {
		t.Errorf("description does not state the researcher write surface:\n%s", desc)
	}

	// Ground the claim in the actual allow-list rather than trusting the prose.
	researcher := AllowedTools(PresetResearcher)
	for _, name := range []string{"wiki", "knowledge"} {
		if _, ok := researcher[name]; !ok {
			t.Errorf("researcher preset no longer allows %q — update the description too", name)
		}
	}
	for _, name := range []string{"write", "edit", "exec", "process"} {
		if _, ok := researcher[name]; ok {
			t.Errorf("researcher preset unexpectedly allows %q; the 'no shell/filesystem writes' claim is now false", name)
		}
	}
}
