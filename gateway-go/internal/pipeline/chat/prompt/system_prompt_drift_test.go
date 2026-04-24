package prompt

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestToolCategoriesMatchRegistry asserts every name in toolCategories exists
// in the tool registry. This prevents phantom (never-registered) names from
// accumulating — the render-time filter silently drops them, so drift is
// invisible without this check.
func TestToolCategoriesMatchRegistry(t *testing.T) {
	data, err := os.ReadFile("../toolreg/tool_schemas.json")
	if err != nil {
		t.Fatalf("read tool_schemas.json: %v", err)
	}
	var schemas []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &schemas); err != nil {
		t.Fatalf("parse tool_schemas.json: %v", err)
	}
	registered := make(map[string]struct{}, len(schemas))
	for _, s := range schemas {
		registered[s.Name] = struct{}{}
	}
	for _, cat := range toolCategories {
		for _, name := range cat.Names {
			if _, ok := registered[name]; !ok {
				t.Errorf("toolCategories[%q] references %q which is not in tool_schemas.json", cat.Label, name)
			}
		}
	}
}

// TestStaticCacheKeyIgnoresSkills asserts that adding/removing skills does NOT
// change the static prompt cache key. Skills belong in the SEMI-STATIC block,
// not the static block — if the static cache key depended on skills, every
// session with different active skills would get a different static prefix,
// defeating the Anthropic prompt cache across sessions.
//
// Prompt Cache Doctrine (see .claude/rules/prompt-cache.md):
//   - Static block: identity, tooling, safety. Cache key = sorted tool names.
//   - Semi-static block: skills prompt. Separate ephemeral cache breakpoint.
//   - Dynamic block: memory, context, runtime. Rebuilt per request.
func TestStaticCacheKeyIgnoresSkills(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "exec"}, {Name: "wiki"}}
	deferred := []DeferredToolInfo{{Name: "gmail", Description: "Gmail"}}

	keyNoSkills := buildStaticCacheKey(tools, deferred)

	// Same tools + deferred list; the SkillsPrompt is NOT an input to this
	// function. Calling again must return the identical key regardless of
	// what skills are active.
	keyAgain := buildStaticCacheKey(tools, deferred)
	if keyNoSkills != keyAgain {
		t.Fatalf("buildStaticCacheKey not deterministic: %q vs %q", keyNoSkills, keyAgain)
	}

	// Bytewise confirm no skill-related field ends up in the key.
	for _, forbidden := range []string{"skill", "Skill", "SKILL"} {
		if strings.Contains(keyNoSkills, forbidden) {
			t.Errorf("static cache key contains skill-related token %q: %q", forbidden, keyNoSkills)
		}
	}
}

// TestSemiStaticBlockStableAcrossCalls asserts that two calls with the same
// SkillsPrompt input produce byte-identical semi-static block output. The
// ephemeral cache_control marker on the semi-static block relies on this
// byte stability — if the block bytes drift (map iteration order, timestamps,
// etc.), the cache read silently misses on every turn.
func TestSemiStaticBlockStableAcrossCalls(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}, {Name: "exec"}},
		SkillsPrompt: `<available_skills>
  <skill>
    <name>deploy</name>
    <description>Deploy the gateway</description>
  </skill>
  <skill>
    <name>release</name>
    <description>Cut a release</description>
  </skill>
</available_skills>`,
	}

	_, ssA, _ := buildPromptSections(params)
	_, ssB, _ := buildPromptSections(params)

	if ssA != ssB {
		t.Fatalf("semi-static block not byte-stable across calls;\nA=%q\nB=%q", ssA, ssB)
	}
	if !strings.Contains(ssA, "deploy") || !strings.Contains(ssA, "release") {
		t.Errorf("semi-static block missing expected skill names; got %q", ssA)
	}
}

// TestSkillsInjectedOnlyInSemiStatic asserts that the SkillsPrompt payload
// appears ONLY in the semi-static block, never in the static or dynamic
// blocks. If a future refactor accidentally moves skill content into the
// static block, it would change the static cache key per-session (bad);
// if it moved into the dynamic block, every turn would be a cache miss.
func TestSkillsInjectedOnlyInSemiStatic(t *testing.T) {
	marker := "DENEB_SKILL_CACHE_SENTINEL_ZZZ"
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>` + marker + `</name></skill></available_skills>`,
	}

	staticText, semiStaticText, dynamicText := buildPromptSections(params)

	if strings.Contains(staticText, marker) {
		t.Errorf("skill payload leaked into STATIC block — breaks static cache key stability")
	}
	if strings.Contains(dynamicText, marker) {
		t.Errorf("skill payload leaked into DYNAMIC block — causes cache miss every turn")
	}
	if !strings.Contains(semiStaticText, marker) {
		t.Errorf("skill payload missing from SEMI-STATIC block — skills not delivered to model")
	}
}
