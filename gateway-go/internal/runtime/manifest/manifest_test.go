package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIsOrderIndependentAndRedactsRuntimeNames(t *testing.T) {
	dir := t.TempDir()
	skillA := filepath.Join(dir, "a.md")
	skillB := filepath.Join(dir, "b.md")
	mustWrite(t, skillA, "alpha instructions")
	mustWrite(t, skillB, "beta instructions")

	builder := newBuilder(StateLoaded, strings.Repeat("a", 64))
	input := Input{
		Version:      "test-version",
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Tools: []Tool{
			{Name: "browser_secret", Description: "private description", Schema: `{"type":"object"}`, SchemaValid: true},
			{Name: "files", Schema: "null", SchemaValid: true, Deferred: true},
		},
		Models: []Model{
			{Role: "main", Provider: "private-provider", Name: "private-model", BaseURL: "http://private-host/v1", CredentialSet: true},
			{Role: "tiny", Provider: "local", Name: "small"},
		},
		Skills: []Skill{
			{Name: "secret-skill-a", Version: "1", Path: skillA},
			{Name: "secret-skill-b", Version: "2", Path: skillB},
		},
	}

	first := builder.Build(input)
	reverse(input.Tools)
	reverse(input.Models)
	reverse(input.Skills)
	second := builder.Build(input)
	if first != second {
		t.Fatalf("manifest changed after input reordering:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if len(first.SHA256) != 64 || len(first.Binary.SHA256) != 64 {
		t.Fatalf("invalid manifest digests: %+v", first)
	}
	if first.Tools.Count != 2 || first.Models.Count != 2 || first.Skills.Count != 2 {
		t.Fatalf("unexpected component counts: %+v", first)
	}

	publicJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal public manifest: %v", err)
	}
	for _, sensitiveValue := range []string{
		"browser_secret", "private description", "private-provider", "private-model",
		"private-host", "secret-skill-a", skillA,
	} {
		if strings.Contains(string(publicJSON), sensitiveValue) {
			t.Fatalf("public manifest leaked %q: %s", sensitiveValue, publicJSON)
		}
	}
}

func TestBuildChangesWhenSkillContentsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	mustWrite(t, path, "first")
	builder := newBuilder(StateLoaded, strings.Repeat("b", 64))
	input := Input{
		Version:      "test",
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Skills:       []Skill{{Name: "skill", Path: path}},
	}
	first := builder.Build(input)

	mustWrite(t, path, "second content is longer")
	second := builder.Build(input)
	if first.Skills.SHA256 == second.Skills.SHA256 {
		t.Fatal("skill component digest did not change after instruction file changed")
	}
	if first.SHA256 == second.SHA256 {
		t.Fatal("overall manifest digest did not change after instruction file changed")
	}
}

func TestBuildChangesForRuntimeCompositionChanges(t *testing.T) {
	base := Input{
		Version:      "version-a",
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Tools:        []Tool{{Name: "files", Schema: `{"type":"object"}`, SchemaValid: true}},
		Models:       []Model{{Role: "main", Provider: "vllm", Name: "model-a"}},
	}
	builder := newBuilder(StateLoaded, strings.Repeat("c", 64))
	want := builder.Build(base)

	toolChanged := base
	toolChanged.Tools = append([]Tool(nil), base.Tools...)
	toolChanged.Tools[0].Schema = `{"type":"object","required":["path"]}`
	if got := builder.Build(toolChanged); got.Tools.SHA256 == want.Tools.SHA256 || got.SHA256 == want.SHA256 {
		t.Fatalf("tool schema change did not change manifest: before=%+v after=%+v", want, got)
	}

	modelChanged := base
	modelChanged.Models = append([]Model(nil), base.Models...)
	modelChanged.Models[0].Name = "model-b"
	if got := builder.Build(modelChanged); got.Models.SHA256 == want.Models.SHA256 || got.SHA256 == want.SHA256 {
		t.Fatalf("model mapping change did not change manifest: before=%+v after=%+v", want, got)
	}

	versionChanged := base
	versionChanged.Version = "version-b"
	if got := builder.Build(versionChanged); got.SHA256 == want.SHA256 {
		t.Fatalf("binary version change did not change manifest: before=%+v after=%+v", want, got)
	}
}

func TestBuildReportsPendingAndDegradedComponents(t *testing.T) {
	builder := newBuilder(StateUnavailable, "")
	pending := builder.Build(Input{Version: "dev"})
	if pending.Binary.State != StateUnavailable || pending.Tools.State != StatePending ||
		pending.Models.State != StatePending || pending.Skills.State != StatePending {
		t.Fatalf("unexpected pending states: %+v", pending)
	}

	degraded := builder.Build(Input{
		Version:      "dev",
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Skills:       []Skill{{Name: "missing", Path: filepath.Join(t.TempDir(), "missing.md")}},
	})
	if degraded.Skills.State != StateDegraded || degraded.Skills.Count != 1 || degraded.Skills.SHA256 == "" {
		t.Fatalf("unexpected degraded skills component: %+v", degraded.Skills)
	}

	invalidTool := builder.Build(Input{
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Tools:        []Tool{{Name: "broken-schema"}},
	})
	if invalidTool.Tools.State != StateDegraded {
		t.Fatalf("invalid tool schema must degrade tool component: %+v", invalidTool.Tools)
	}

	oversizedPath := filepath.Join(t.TempDir(), "oversized.md")
	mustWrite(t, oversizedPath, strings.Repeat("x", int(maxSkillFileBytes)+1))
	oversizedSkill := builder.Build(Input{
		ToolsLoaded:  true,
		ModelsLoaded: true,
		SkillsLoaded: true,
		Skills:       []Skill{{Name: "oversized", Path: oversizedPath}},
	})
	if oversizedSkill.Skills.State != StateDegraded {
		t.Fatalf("oversized skill must not be read through health: %+v", oversizedSkill.Skills)
	}
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
