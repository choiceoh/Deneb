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

	keyNoSkills := buildStaticCacheKey(tools, deferred, "", "", false)

	// Same tools + deferred list; the SkillsPrompt is NOT an input to this
	// function. Calling again must return the identical key regardless of
	// what skills are active.
	keyAgain := buildStaticCacheKey(tools, deferred, "", "", false)
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

// TestStaticCacheKeyVariesByTopic asserts the per-topic knowledge cache key
// (a) splits distinct topics into distinct Static cache entries and
// (b) invalidates when a topic's content hash changes (its .md was edited).
// Without (a) two topics would overwrite each other's Static cache; without
// (b) an edited topic file would keep serving stale cached knowledge.
func TestStaticCacheKeyVariesByTopic(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "exec"}}
	deferred := []DeferredToolInfo{{Name: "gmail", Description: "Gmail"}}

	base := buildStaticCacheKey(tools, deferred, "", "", false)
	coding := buildStaticCacheKey(tools, deferred, "coding:hashA", "", false)
	codingEdited := buildStaticCacheKey(tools, deferred, "coding:hashB", "", false)
	work := buildStaticCacheKey(tools, deferred, "work:hashA", "", false)

	if coding == base {
		t.Errorf("topic key must differ from the topic-less key")
	}
	if coding == work {
		t.Errorf("distinct topics must produce distinct cache keys: both %q", coding)
	}
	if coding == codingEdited {
		t.Errorf("editing a topic's content (hash change) must change the cache key")
	}
}

// TestStaticCacheKeyTopicEmptyEqualsLegacy asserts an empty topicCacheKey adds
// no topic suffix, so sessions without per-topic knowledge keep sharing the
// existing Static cache entry (zero regression for the common case).
func TestStaticCacheKeyTopicEmptyEqualsLegacy(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "wiki"}}
	deferred := []DeferredToolInfo{{Name: "gmail", Description: "Gmail"}}

	withEmpty := buildStaticCacheKey(tools, deferred, "", "", false)
	if strings.Contains(withEmpty, "|topic=") {
		t.Errorf("empty topic must not append a topic suffix: %q", withEmpty)
	}
}

// TestTopicKnowledgeOnlyInStaticBlock asserts per-topic knowledge appears ONLY
// in the Static (cached) block — never in semi-static or dynamic. Static
// integration is the chosen design: a leak into the dynamic block would make
// every turn a cache miss, and a leak into semi-static would collide with the
// skills cache marker.
func TestTopicKnowledgeOnlyInStaticBlock(t *testing.T) {
	ResetContextFileCacheForTest()
	marker := "DENEB_TOPIC_CACHE_SENTINEL_ZZZ"
	pathMarker := "/tmp/topics/coding.md"
	params := SystemPromptParams{
		WorkspaceDir:       "/tmp",
		ToolDefs:           []ToolDef{{Name: "read"}},
		TopicKnowledge:     "코딩 토픽 배경지식: " + marker,
		TopicCacheKey:      "coding:hashA",
		TopicKnowledgePath: pathMarker,
	}

	staticText, semiStaticText, dynamicText := buildPromptSections(params)

	if !strings.Contains(staticText, marker) {
		t.Errorf("topic knowledge missing from STATIC block — not delivered to model")
	}
	if !strings.Contains(staticText, pathMarker) {
		t.Errorf("topic knowledge source path missing from STATIC block — the agent cannot locate the doc to edit it on request (chat edit + the Settings topicdocs editor both target this path)")
	}
	if strings.Contains(semiStaticText, marker) {
		t.Errorf("topic knowledge leaked into SEMI-STATIC block")
	}
	if strings.Contains(dynamicText, marker) {
		t.Errorf("topic knowledge leaked into DYNAMIC block — causes cache miss every turn")
	}
}

// TestPersonaDefaultByteIdentical asserts that with no persona override the 업무
// Static block still opens with DefaultPersona verbatim and the cache key adds
// no "|persona=" suffix — so the common (unedited) case is a zero-regression
// byte-identical match to the pre-feature behavior, preserving the existing
// vLLM APC / Anthropic Static cache entry.
func TestPersonaDefaultByteIdentical(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "persona_test_sentinel"}}
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     tools,
	}
	staticText, _, _ := buildPromptSections(params)
	if !strings.HasPrefix(staticText, DefaultPersona) {
		t.Errorf("default 업무 Static block must open with DefaultPersona verbatim (byte-identity); got prefix %q", staticText[:min(len(staticText), 120)])
	}
	key := buildStaticCacheKey(tools, nil, "", "", false)
	if strings.Contains(key, "|persona=") {
		t.Errorf("no override must not append a persona suffix: %q", key)
	}
}

// TestPersonaOverrideInStaticBlock asserts an override replaces the default
// identity/role text in the Static block (and only there), and that its content
// hash gives the Static cache key a distinct "|persona=" slot so an edited
// persona never reuses the default entry.
func TestPersonaOverrideInStaticBlock(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "persona_test_sentinel"}}
	marker := "DENEB_PERSONA_SENTINEL_QQQ"
	override := "너는 커스텀 페르소나다. " + marker
	personaKey := PersonaCacheKeyFor(override)
	params := SystemPromptParams{
		WorkspaceDir:    "/tmp",
		ToolDefs:        tools,
		PersonaText:     override,
		PersonaCacheKey: personaKey,
	}
	staticText, semiStaticText, dynamicText := buildPromptSections(params)
	if !strings.Contains(staticText, marker) {
		t.Errorf("persona override missing from STATIC block — not delivered to model")
	}
	if strings.Contains(staticText, "비서실장형 단일 에이전트") {
		t.Errorf("default role text must be replaced by the override, but it is still present")
	}
	if strings.Contains(semiStaticText, marker) || strings.Contains(dynamicText, marker) {
		t.Errorf("persona override leaked out of the STATIC block")
	}
	key := buildStaticCacheKey(tools, nil, "", personaKey, false)
	if !strings.Contains(key, "|persona="+personaKey) {
		t.Errorf("override must add its content-hash persona suffix to the cache key: %q", key)
	}
}

// TestPersonaIgnoredForChatbot asserts the 챗봇 (general-assistant) path never
// consumes the 업무 persona override — it keeps its neutral identity, since the
// prompt corner edits the 업무 persona only.
func TestPersonaIgnoredForChatbot(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "persona_cb_sentinel"}}
	marker := "DENEB_PERSONA_SENTINEL_CB"
	params := SystemPromptParams{
		WorkspaceDir:    "/tmp",
		ToolDefs:        tools,
		Chatbot:         true,
		PersonaText:     "업무 페르소나 " + marker,
		PersonaCacheKey: PersonaCacheKeyFor("업무 페르소나 " + marker),
	}
	staticText, _, _ := buildPromptSections(params)
	if strings.Contains(staticText, marker) {
		t.Errorf("챗봇 path must not render the 업무 persona override")
	}
	if !strings.Contains(staticText, "helpful, knowledgeable AI assistant") {
		t.Errorf("챗봇 path must keep its neutral general-assistant identity")
	}
}

// TestPersonaCacheKeyFor asserts the persona hash helper: empty for blank text,
// deterministic, distinct per content, and 12 hex chars (matching the topic
// hash scheme).
func TestPersonaCacheKeyFor(t *testing.T) {
	if PersonaCacheKeyFor("") != "" || PersonaCacheKeyFor("   \n\t ") != "" {
		t.Errorf("blank persona text must yield an empty cache key")
	}
	a := PersonaCacheKeyFor("페르소나 A")
	b := PersonaCacheKeyFor("페르소나 B")
	if a == "" || b == "" || a == b {
		t.Errorf("distinct persona texts must yield distinct non-empty keys: %q vs %q", a, b)
	}
	if a != PersonaCacheKeyFor("페르소나 A") {
		t.Errorf("persona cache key must be deterministic")
	}
	if len(a) != 12 {
		t.Errorf("persona cache key must be 12 hex chars (topic-hash scheme), got %d: %q", len(a), a)
	}
	// Leading/trailing whitespace must not change the key (TrimSpace parity with
	// the override render path).
	if a != PersonaCacheKeyFor("  페르소나 A  ") {
		t.Errorf("persona cache key must be whitespace-insensitive (TrimSpace)")
	}
}
