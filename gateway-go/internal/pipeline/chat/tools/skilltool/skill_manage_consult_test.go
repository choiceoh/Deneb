package skilltool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// A skills(action=read) consult must be attributed to the skill's CANONICAL
// catalog name. The lookup matches sanitized-to-sanitized, so a name like
// "Playwright (Automation + MCP + Scraper)" resolves from any spelling — but
// recording the sanitized spelling keyed usage stats under a phantom name the
// evolver's exact catalog lookup could never find (2026-08: "skill \"sk\" not
// found in catalog" against a name that was never a skill).
func TestSkillManageReadRecordsCanonicalCatalogName(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "managed", "playwright")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: Playwright (Automation + MCP + Scraper)\n---\n# Playwright\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const canonical = "Playwright (Automation + MCP + Scraper)"
	snapshot := &skills.FullSkillSnapshot{
		ResolvedSkills: []skills.PromptSkill{{Name: canonical, FilePath: skillFile}},
	}
	fn := toolSkillManage(func() *skills.FullSkillSnapshot { return snapshot }, filepath.Join(dir, "workspace"), filepath.Join(dir, "bundled"), nil)

	for _, spelling := range []string{canonical, sanitizeSkillName(canonical), "PLAYWRIGHT (AUTOMATION + MCP + SCRAPER)"} {
		log := toolport.NewSkillConsultLog()
		ctx := toolport.WithSkillConsultLog(context.Background(), log)
		input, _ := json.Marshal(map[string]any{"action": "read", "name": spelling})
		if _, err := fn(ctx, input); err != nil {
			t.Fatalf("read %q: %v", spelling, err)
		}
		got := log.DrainNew()
		if len(got) != 1 || got[0] != canonical {
			t.Fatalf("read %q recorded %v, want [%q]", spelling, got, canonical)
		}
	}
}

// A name that does not resolve is not a consult — nothing may be recorded.
func TestSkillManageReadOfUnknownSkillRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	fn := toolSkillManage(func() *skills.FullSkillSnapshot { return &skills.FullSkillSnapshot{} }, filepath.Join(dir, "workspace"), filepath.Join(dir, "bundled"), nil)
	log := toolport.NewSkillConsultLog()
	ctx := toolport.WithSkillConsultLog(context.Background(), log)
	input, _ := json.Marshal(map[string]any{"action": "read", "name": "no-such-skill"})
	if _, err := fn(ctx, input); err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	if got := log.DrainNew(); len(got) != 0 {
		t.Fatalf("unknown skill must not accrue a consult: %v", got)
	}
}
