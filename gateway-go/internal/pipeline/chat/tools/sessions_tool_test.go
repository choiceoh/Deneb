package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

type fakeSessionTranscript struct {
	searches []string
	results  map[string][]toolctx.SearchResult
}

func (f *fakeSessionTranscript) Load(string, int) ([]toolctx.ChatMessage, int, error) {
	return nil, 0, nil
}

func (f *fakeSessionTranscript) Append(string, toolctx.ChatMessage) error {
	return nil
}

func (f *fakeSessionTranscript) Delete(string) error {
	return nil
}

func (f *fakeSessionTranscript) ListKeys() ([]string, error) {
	return nil, nil
}

func (f *fakeSessionTranscript) Search(query string, _ int) ([]toolctx.SearchResult, error) {
	f.searches = append(f.searches, query)
	return f.results[query], nil
}

func (f *fakeSessionTranscript) CloneRecent(string, string, int) error {
	return nil
}

func sessionSearchJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestToolSessionsSearchExpandsNaturalLanguageQuery(t *testing.T) {
	match := toolctx.MatchedMsg{
		Index:   3,
		Message: toolctx.NewTextChatMessage("assistant", "PR 리뷰 후 체리픽 브랜치를 만들었다", 123),
	}
	transcript := &fakeSessionTranscript{
		results: map[string][]toolctx.SearchResult{
			"pr": {
				{SessionKey: "desktop:abc", Matches: []toolctx.MatchedMsg{match}},
			},
			"체리픽": {
				{SessionKey: "desktop:abc", Matches: []toolctx.MatchedMsg{match}},
			},
		},
	}

	out, err := toolSessionsSearch(transcript)(
		context.Background(),
		sessionSearchJSON(t, map[string]any{"query": "GitHub PR 리뷰하고 체리픽했던 작업 찾아줘"}),
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "via expanded terms") {
		t.Fatalf("expected expanded search output, got: %s", out)
	}
	if !strings.Contains(out, "Found 1 match(es)") {
		t.Fatalf("expected duplicate expanded hits to be merged, got: %s", out)
	}
	if !strings.Contains(out, "desktop:abc") || !strings.Contains(out, "체리픽 브랜치") {
		t.Fatalf("expected session and match text, got: %s", out)
	}
	if len(transcript.searches) < 2 || transcript.searches[0] != "GitHub PR 리뷰하고 체리픽했던 작업 찾아줘" {
		t.Fatalf("expected exact search before expansion, got: %v", transcript.searches)
	}
}

// --- sessions_spawn guardrails ---

func spawnDeps(sm *session.Manager) *toolctx.SessionDeps {
	return &toolctx.SessionDeps{
		Manager: sm,
		SendFn:  func(string, string) error { return nil },
	}
}

func spawnInput(t *testing.T, task, label string) json.RawMessage {
	t.Helper()
	return sessionSearchJSON(t, map[string]string{"task": task, "label": label})
}

func TestSessionsSpawn_RecordsDepthAndLabel(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, spawnInput(t, "research X", "researcher"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Sub-agent spawned") {
		t.Fatalf("unexpected spawn output: %s", out)
	}

	var child *session.Session
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main" {
			child = s
		}
	}
	if child == nil {
		t.Fatal("child session not created")
	}
	if child.SpawnDepth == nil || *child.SpawnDepth != 1 {
		t.Errorf("child SpawnDepth = %v, want 1", child.SpawnDepth)
	}
	if child.Label != "researcher" {
		t.Errorf("child Label = %q, want researcher", child.Label)
	}
}

func TestSessionsSpawn_StoresToolPreset(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	input := sessionSearchJSON(t, map[string]string{
		"task": "research X", "label": "r1", "tool_preset": "researcher",
	})
	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, input)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Tool preset: researcher") {
		t.Fatalf("expected preset echo in output, got: %s", out)
	}

	var child *session.Session
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main" {
			child = s
		}
	}
	if child == nil {
		t.Fatal("child session not created")
	}
	if child.ToolPreset != "researcher" {
		t.Errorf("child ToolPreset = %q, want researcher", child.ToolPreset)
	}
}

func TestSessionsSpawn_ImplementerUsesCodingRoleWhenConfigured(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")
	deps := spawnDeps(sm)
	deps.CodingDefaultModel = "kimi/kimi-for-coding"

	input := sessionSearchJSON(t, map[string]string{
		"task": "fix the build", "label": "impl", "tool_preset": "implementer",
	})
	out, err := ToolSessionsSpawn(deps)(ctx, input)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Model: coding") {
		t.Fatalf("expected coding role echo in output, got: %s", out)
	}

	var child *session.Session
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main" {
			child = s
		}
	}
	if child == nil {
		t.Fatal("child session not created")
	}
	if child.Model != "coding" {
		t.Errorf("child Model = %q, want coding", child.Model)
	}
	if child.ToolPreset != "implementer" {
		t.Errorf("child ToolPreset = %q, want implementer", child.ToolPreset)
	}
}

func TestSessionsSpawn_RejectsExplicitCodingWhenUnconfigured(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	input := sessionSearchJSON(t, map[string]string{
		"task": "fix the build", "label": "impl", "model": "coding",
	})
	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, input)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Spawn rejected: model \"coding\" is not configured") {
		t.Fatalf("expected coding rejection, got: %s", out)
	}
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main" {
			t.Fatalf("no child session should be created when coding is unconfigured, found %s", s.Key)
		}
	}
}

func TestSessionsSpawn_ImplementerUsesLiveCodingRole(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")
	deps := spawnDeps(sm)
	codingModel := ""
	deps.CodingDefaultModelFn = func() string { return codingModel }

	codingModel = "kimi/kimi-for-coding"
	input := sessionSearchJSON(t, map[string]string{
		"task": "fix the build", "label": "impl", "tool_preset": "implementer",
	})
	out, err := ToolSessionsSpawn(deps)(ctx, input)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Model: coding") {
		t.Fatalf("expected coding role echo in output, got: %s", out)
	}
}

// TestSessionsSpawn_RejectsUnknownToolPreset pins the fail-closed contract:
// toolpreset.AllowedTools returns nil (= unrestricted) for unknown names, so
// a typo'd preset must reject the spawn instead of silently granting the
// child the full toolset.
func TestSessionsSpawn_RejectsUnknownToolPreset(t *testing.T) {
	sm := session.NewManager()
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	input := sessionSearchJSON(t, map[string]string{
		"task": "research X", "tool_preset": "reseacher", // typo on purpose
	})
	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, input)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Spawn rejected: unknown tool_preset") {
		t.Fatalf("expected rejection message, got: %s", out)
	}
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main" {
			t.Fatalf("no child session should be created on invalid preset, found %s", s.Key)
		}
	}
}

func TestSessionsSpawn_RejectsBeyondMaxDepth(t *testing.T) {
	sm := session.NewManager()
	parent := sm.Create("client:main:deep", session.KindDirect)
	depth := maxSubagentSpawnDepth
	parent.SpawnDepth = &depth
	if err := sm.Set(parent); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	ctx := toolctx.WithSessionKey(context.Background(), "client:main:deep")

	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, spawnInput(t, "go deeper", ""))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Spawn rejected") || !strings.Contains(out, "depth") {
		t.Fatalf("expected depth rejection, got: %s", out)
	}
	for _, s := range sm.List() {
		if s.SpawnedBy == "client:main:deep" {
			t.Fatalf("child %q was created despite depth rejection", s.Key)
		}
	}
}

func TestSessionsSpawn_RejectsBeyondConcurrencyCap(t *testing.T) {
	sm := session.NewManager()
	for i := range maxConcurrentSubagents {
		key := spawnTestChildKey(i)
		c := sm.Create(key, session.KindDirect)
		c.SpawnedBy = "client:main"
		c.Status = session.StatusRunning
		if err := sm.Set(c); err != nil {
			t.Fatalf("set child %d: %v", i, err)
		}
	}
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, spawnInput(t, "one more", ""))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Spawn rejected") || !strings.Contains(out, "active") {
		t.Fatalf("expected concurrency rejection, got: %s", out)
	}
}

func TestSessionsSpawn_TerminalChildrenDoNotCountAgainstCap(t *testing.T) {
	sm := session.NewManager()
	for i := range maxConcurrentSubagents {
		key := spawnTestChildKey(i)
		c := sm.Create(key, session.KindDirect)
		c.SpawnedBy = "client:main"
		c.Status = session.StatusRunning
		if err := sm.Set(c); err != nil {
			t.Fatalf("set child %d running: %v", i, err)
		}
		c = sm.Get(key)
		c.Status = session.StatusDone
		if err := sm.Set(c); err != nil {
			t.Fatalf("set child %d done: %v", i, err)
		}
	}
	ctx := toolctx.WithSessionKey(context.Background(), "client:main")

	out, err := ToolSessionsSpawn(spawnDeps(sm))(ctx, spawnInput(t, "fresh task", ""))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "Sub-agent spawned") {
		t.Fatalf("expected spawn to succeed past terminal children, got: %s", out)
	}
}

func spawnTestChildKey(i int) string {
	return "client:main:worker:" + string(rune('a'+i))
}
