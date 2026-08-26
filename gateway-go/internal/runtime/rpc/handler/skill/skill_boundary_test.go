package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	rtevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/events"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	infraprocess "github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func decodePayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	rpctest.MustOK(t, resp)
	var got T
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, resp.Payload)
	}
	return got
}

func callMalformed(method string, h func(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame) *protocol.ResponseFrame {
	return h(context.Background(), &protocol.RequestFrame{
		ID:     "malformed",
		Method: method,
		Params: json.RawMessage(`{"broken":`),
	})
}

func TestMethodsNilDependencyAndCompleteSurface(t *testing.T) {
	if got := Methods(Deps{}); got != nil {
		t.Fatalf("Methods without registry = %#v, want nil", got)
	}
	registry := skills.NewRegistry()
	methods := Methods(Deps{Skills: registry})
	want := []string{
		"skills.status",
		"skills.bins",
		"skills.install",
		"skills.update",
		"skills.snapshot",
		"skills.commands",
		"skills.discover",
		"skills.workspace_status",
		"skills.entries",
	}
	for _, name := range want {
		if methods[name] == nil {
			t.Errorf("method %q not registered", name)
		}
	}
	if len(methods) != len(want) {
		t.Fatalf("method count = %d, want %d: %#v", len(methods), len(want), methods)
	}
}

func TestSkillsStatusBinsAndMalformedStatusParams(t *testing.T) {
	registry := skills.NewRegistry()
	registry.Install("github", "install-1")
	methods := Methods(Deps{Skills: registry})

	status := decodePayload[skills.RegistryStatus](t, rpctest.Call(methods, "skills.status", map[string]any{"agentId": "a"}))
	if len(status.Skills) != 1 || status.Skills[0].Key != "github" || !status.Skills[0].Installed || !status.Skills[0].Enabled {
		t.Fatalf("skills.status = %#v", status)
	}
	bins := decodePayload[struct {
		Bins []string `json:"bins"`
	}](t, rpctest.Call(methods, "skills.bins", nil))
	if bins.Bins == nil || len(bins.Bins) != 0 {
		t.Fatalf("skills.bins = %#v, want non-nil empty", bins.Bins)
	}

	resp := callMalformed("skills.status", methods["skills.status"])
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("malformed status code = %q", resp.Error.Code)
	}
}

func TestSkillsInstallValidationBroadcastAndIdempotence(t *testing.T) {
	registry := skills.NewRegistry()
	type event struct {
		name    string
		payload map[string]any
	}
	var mu sync.Mutex
	var events []event
	methods := Methods(Deps{
		Skills: registry,
		Broadcaster: func(name string, payload rtevents.EventPayload) (int, []error) {
			mu.Lock()
			defer mu.Unlock()
			var m map[string]any
			_ = json.Unmarshal(payload.Bytes(), &m)
			events = append(events, event{name: name, payload: m})
			return 1, nil
		},
	})

	for _, params := range []map[string]any{
		{},
		{"name": "github"},
		{"installId": "id"},
		{"name": "", "installId": ""},
	} {
		resp := rpctest.Call(methods, "skills.install", params)
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("install params %#v code = %q", params, resp.Error.Code)
		}
	}
	malformed := callMalformed("skills.install", methods["skills.install"])
	rpctest.MustErr(t, malformed)

	first := decodePayload[skills.InstallAck](t, rpctest.Call(methods, "skills.install", map[string]any{
		"name": "github", "installId": "catalog/github", "timeoutMs": 1,
	}))
	if !first.OK || !strings.Contains(first.Message, "installed") {
		t.Fatalf("first install = %#v", first)
	}
	second := decodePayload[skills.InstallAck](t, rpctest.Call(methods, "skills.install", map[string]any{
		"name": "github", "installId": "catalog/github",
	}))
	if !second.OK || !strings.Contains(second.Message, "already installed") {
		t.Fatalf("idempotent install = %#v", second)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("broadcast events = %#v", events)
	}
	for _, got := range events {
		if got.name != "skills.changed" || got.payload["action"] != "installed" || got.payload["name"] != "github" {
			t.Errorf("install event = %#v", got)
		}
	}
}

func TestSkillsInstallWorksWithoutBroadcaster(t *testing.T) {
	methods := Methods(Deps{Skills: skills.NewRegistry()})
	got := decodePayload[skills.InstallAck](t, rpctest.Call(methods, "skills.install", map[string]any{
		"name": "local", "installId": "local-1",
	}))
	if !got.OK {
		t.Fatalf("install without broadcaster = %#v", got)
	}
	// Explicitly cover the nil-safe helper as a standalone contract.
	broadcast(nil, "ignored", map[string]any{"x": 1})
}

func TestSkillsUpdateValidationNotFoundAndMergedConfig(t *testing.T) {
	registry := skills.NewRegistry()
	registry.Install("github", "id")
	var events int
	methods := Methods(Deps{
		Skills: registry,
		Broadcaster: func(event string, payload rtevents.EventPayload) (int, []error) {
			if event != "skills.changed" {
				t.Errorf("event = %q", event)
			}
			var m map[string]any
			_ = json.Unmarshal(payload.Bytes(), &m)
			if m["action"] != "updated" || m["skillKey"] != "github" {
				t.Errorf("payload = %#v", payload)
			}
			events++
			return 1, nil
		},
	})

	resp := rpctest.Call(methods, "skills.update", map[string]any{})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("missing update code = %q", resp.Error.Code)
	}
	resp = rpctest.Call(methods, "skills.update", map[string]any{"skillKey": "missing"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("missing skill update code = %q", resp.Error.Code)
	}

	enabled := false
	got := decodePayload[struct {
		OK       bool              `json:"ok"`
		SkillKey string            `json:"skillKey"`
		Config   map[string]string `json:"config"`
	}](t, rpctest.Call(methods, "skills.update", map[string]any{
		"skillKey": "github",
		"enabled":  enabled,
		"apiKey":   "secret-value",
		"env": map[string]string{
			"GH_HOST":   "github.example",
			"LOG_LEVEL": "warn",
		},
	}))
	if !got.OK || got.SkillKey != "github" || got.Config["apiKey"] != "secret-value" ||
		got.Config["GH_HOST"] != "github.example" || got.Config["LOG_LEVEL"] != "warn" {
		t.Fatalf("updated payload = %#v", got)
	}
	status := registry.Status("")
	if len(status.Skills) != 1 || status.Skills[0].Enabled || status.Skills[0].Config["apiKey"] != "secret-value" {
		t.Fatalf("registry after update = %#v", status)
	}
	if events != 1 {
		t.Fatalf("successful update events = %d, want 1", events)
	}
}

func TestCatalogMethodsDefaultCustomAndMalformedParams(t *testing.T) {
	methods := CatalogMethods()
	if len(methods) != 1 || methods["tools.catalog"] == nil {
		t.Fatalf("CatalogMethods = %#v", methods)
	}
	type catalogPayload struct {
		AgentID  string             `json:"agentId"`
		Profiles []profileOption    `json:"profiles"`
		Groups   []ToolCatalogGroup `json:"groups"`
	}
	defaultCatalog := decodePayload[catalogPayload](t, rpctest.Call(methods, "tools.catalog", nil))
	if defaultCatalog.AgentID != "default" || len(defaultCatalog.Profiles) != 4 || len(defaultCatalog.Groups) == 0 {
		t.Fatalf("default catalog = %#v", defaultCatalog)
	}
	includePlugins := true
	custom := decodePayload[catalogPayload](t, rpctest.Call(methods, "tools.catalog", map[string]any{
		"agentId": "agent-7", "includePlugins": includePlugins,
	}))
	if custom.AgentID != "agent-7" || !reflect.DeepEqual(custom.Groups, defaultCatalog.Groups) {
		t.Fatalf("custom catalog = %#v", custom)
	}
	resp := callMalformed("tools.catalog", methods["tools.catalog"])
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("malformed catalog code = %q", resp.Error.Code)
	}
}

func TestBuildCoreToolCatalogReturnsFreshOuterAndInnerSlices(t *testing.T) {
	first := buildCoreToolCatalog()
	second := buildCoreToolCatalog()
	if len(first) == 0 || len(first[0].Tools) == 0 {
		t.Fatal("catalog fixture unexpectedly empty")
	}
	originalGroup := second[0].ID
	originalTool := second[0].Tools[0].ID
	first[0].ID = "mutated"
	first[0].Tools[0].ID = "mutated-tool"
	third := buildCoreToolCatalog()
	if third[0].ID != originalGroup || third[0].Tools[0].ID != originalTool {
		t.Fatalf("catalog builder retained caller mutation: %#v", third[0])
	}
}

func newProcessManager(t *testing.T) *infraprocess.Manager {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := infraprocess.NewManager(logger)
	t.Cleanup(mgr.Stop)
	return mgr
}

func TestToolMethodsReturnThreeHandlersAndRejectInvalidInvoke(t *testing.T) {
	methods := ToolMethods(ToolDeps{})
	for _, name := range []string{"tools.invoke", "tools.list", "tools.status"} {
		if methods[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if len(methods) != 3 {
		t.Fatalf("ToolMethods count = %d", len(methods))
	}

	resp := rpctest.Call(methods, "tools.invoke", map[string]any{})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("missing tool code = %q", resp.Error.Code)
	}
	resp = rpctest.Call(methods, "tools.invoke", map[string]any{"tool": "web", "args": map[string]any{}})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("standalone unavailable code = %q", resp.Error.Code)
	}
	resp = callMalformed("tools.invoke", methods["tools.invoke"])
	rpctest.MustErr(t, resp)
}

func TestToolsInvokeDryRunErrorsWithoutManagerAndSkipsExecution(t *testing.T) {
	methods := ToolMethods(ToolDeps{})
	// A nil manager means bash is unavailable even in dry-run mode because the
	// dispatcher cannot establish that local execution is configured.
	resp := rpctest.Call(methods, "tools.invoke", map[string]any{
		"tool": "bash", "dryRun": true, "args": map[string]any{"command": "touch forbidden"},
	})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("nil-manager dry-run code = %q", resp.Error.Code)
	}

	mgr := newProcessManager(t)
	methods = ToolMethods(ToolDeps{Processes: mgr})
	got := decodePayload[map[string]any](t, rpctest.Call(methods, "tools.invoke", map[string]any{
		"tool": "bash", "dryRun": true, "args": map[string]any{"command": "touch forbidden", "x": 1},
	}))
	if got["tool"] != "bash" || got["dryRun"] != true {
		t.Fatalf("dry-run payload = %#v", got)
	}
	if mgr.Get("") != nil {
		t.Fatal("dry run unexpectedly created tracked process")
	}
}

func TestToolsInvokeBashExecutesAndReturnsTrackedStatus(t *testing.T) {
	mgr := newProcessManager(t)
	methods := ToolMethods(ToolDeps{Processes: mgr})
	result := decodePayload[infraprocess.ExecResult](t, rpctest.Call(methods, "tools.invoke", map[string]any{
		"tool": "bash",
		"args": map[string]any{
			"command":    "printf 'hello'; printf 'warn' >&2",
			"timeoutMs":  2_000,
			"workingDir": t.TempDir(),
		},
	}))
	if result.Status != infraprocess.StatusDone || result.ExitCode != 0 || result.Stdout != "hello" || result.Stderr != "warn" || result.ID == "" {
		t.Fatalf("bash result = %#v", result)
	}

	tracked := decodePayload[infraprocess.ProcessSnapshot](t, rpctest.Call(methods, "tools.status", map[string]any{"id": result.ID}))
	if tracked.Request.ID != result.ID || tracked.Result == nil || tracked.Result.Stdout != "hello" {
		t.Fatalf("tracked process = %#v", tracked)
	}
	for _, params := range []map[string]any{{}, {"id": "missing"}} {
		resp := rpctest.Call(methods, "tools.status", params)
		rpctest.MustErr(t, resp)
		if params["id"] == nil && resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("empty status code = %q", resp.Error.Code)
		}
		if params["id"] == "missing" && resp.Error.Code != protocol.ErrNotFound {
			t.Errorf("missing status code = %q", resp.Error.Code)
		}
	}
	resp := rpctest.Call(ToolMethods(ToolDeps{}), "tools.status", map[string]any{"id": "x"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("nil manager status code = %q", resp.Error.Code)
	}
}

func TestToolsInvokeMissingCommandAndCancelledContext(t *testing.T) {
	mgr := newProcessManager(t)
	methods := ToolMethods(ToolDeps{Processes: mgr})
	for _, tool := range []string{"bash", "exec"} {
		resp := rpctest.Call(methods, "tools.invoke", map[string]any{"tool": tool, "args": map[string]any{}})
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("%s missing command code = %q", tool, resp.Error.Code)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(map[string]any{
		"tool": "bash", "args": map[string]any{"command": "sleep 10", "timeoutMs": 60_000},
	})
	start := time.Now()
	resp := methods["tools.invoke"](ctx, &protocol.RequestFrame{ID: "cancel", Method: "tools.invoke", Params: raw})
	result := decodePayload[infraprocess.ExecResult](t, resp)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancelled execution took %s", time.Since(start))
	}
	if result.Status == infraprocess.StatusDone || result.Error == "" {
		t.Fatalf("cancelled result = %#v", result)
	}
}

func TestToolsListMatchesCatalogWithoutAliasing(t *testing.T) {
	methods := ToolMethods(ToolDeps{})
	type listPayload struct {
		Tools []map[string]any `json:"tools"`
	}
	first := decodePayload[listPayload](t, rpctest.Call(methods, "tools.list", nil))
	second := decodePayload[listPayload](t, rpctest.Call(methods, "tools.list", map[string]any{"ignored": true}))
	if len(first.Tools) == 0 || !reflect.DeepEqual(first.Tools, second.Tools) {
		t.Fatalf("tools list mismatch: first=%#v second=%#v", first, second)
	}
	seen := make(map[string]bool)
	for _, tool := range first.Tools {
		id, _ := tool["id"].(string)
		if id == "" || seen[id] {
			t.Errorf("invalid or duplicate tool ID %q", id)
		}
		seen[id] = true
		for _, key := range []string{"label", "description", "source", "group"} {
			if tool[key] == "" || tool[key] == nil {
				t.Errorf("tool %q missing %s: %#v", id, key, tool)
			}
		}
	}
}

func writeWorkspaceSkill(t *testing.T, workspace, category, name, description string) {
	t.Helper()
	dir := filepath.Join(workspace, "skills")
	if category != "" {
		dir = filepath.Join(dir, category)
	}
	dir = filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll skill: %v", err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n\nOperational instructions.\n", name, description, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile skill: %v", err)
	}
}

func TestWorkspaceSkillHandlersDiscoverFilterAndEmitChangeEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	writeWorkspaceSkill(t, workspace, "coding", "github", "GitHub operations")
	writeWorkspaceSkill(t, workspace, "productivity", "minutes", "Meeting minutes")

	var eventName string
	var eventPayload map[string]any
	methods := Methods(Deps{
		Skills: skills.NewRegistry(),
		Broadcaster: func(name string, payload rtevents.EventPayload) (int, []error) {
			eventName = name
			_ = json.Unmarshal(payload.Bytes(), &eventPayload)
			return 1, nil
		},
	})
	base := map[string]any{
		"workspaceDir":     workspace,
		"managedSkillsDir": filepath.Join(home, "empty-managed"),
	}

	discover := decodePayload[map[string]any](t, rpctest.Call(methods, "skills.discover", map[string]any{
		"workspaceDir": workspace,
	}))
	if discover["ok"] != true || discover["count"] != float64(2) {
		t.Fatalf("discover payload = %#v", discover)
	}
	if eventName != "skills.changed" || eventPayload["action"] != "discovered" || eventPayload["count"] != float64(2) {
		t.Fatalf("discover event = %q %#v", eventName, eventPayload)
	}

	entries := decodePayload[struct {
		Entries []skills.SkillEntry `json:"entries"`
	}](t, rpctest.Call(methods, "skills.entries", base))
	if len(entries.Entries) != 2 {
		t.Fatalf("entries = %#v", entries.Entries)
	}
	filteredParams := map[string]any{
		"workspaceDir":     workspace,
		"managedSkillsDir": filepath.Join(home, "empty-managed"),
		"skillFilter":      []string{"github"},
	}
	filtered := decodePayload[struct {
		Entries []skills.SkillEntry `json:"entries"`
	}](t, rpctest.Call(methods, "skills.entries", filteredParams))
	if len(filtered.Entries) != 1 || filtered.Entries[0].Skill.Name != "github" {
		t.Fatalf("filtered entries = %#v", filtered.Entries)
	}

	commands := decodePayload[map[string]json.RawMessage](t, rpctest.Call(methods, "skills.commands", map[string]any{
		"workspaceDir":  workspace,
		"skillFilter":   []string{"github"},
		"reservedNames": []string{"minutes"},
	}))
	var commandList []map[string]any
	if err := json.Unmarshal(commands["commands"], &commandList); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if len(commandList) != 1 || commandList[0]["name"] != "github" {
		t.Fatalf("commands = %#v", commandList)
	}
}

func TestWorkspaceSkillHandlersRejectMissingWorkspaceDir(t *testing.T) {
	methods := Methods(Deps{Skills: skills.NewRegistry()})
	for _, method := range []string{
		"skills.snapshot", "skills.commands", "skills.discover", "skills.entries", "skills.workspace_status",
	} {
		resp := rpctest.Call(methods, method, map[string]any{})
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("%s missing workspace code = %q", method, resp.Error.Code)
		}
	}
}

func TestWorkspaceDiscoverConfigRejectsEmptyDirAndEligibilityContextAppliesOverrides(t *testing.T) {
	if _, err := workspaceDiscoverConfig("", "", "", nil, nil); err == nil {
		t.Fatal("empty workspace config succeeded")
	}
	want := skills.DiscoverConfig{
		WorkspaceDir:     "/workspace",
		BundledSkillsDir: "/bundled",
		ManagedSkillsDir: "/managed",
		ExtraDirs:        []string{"/extra-1", "/extra-2"},
		PluginSkillDirs:  []string{"/plugin"},
	}
	got, err := workspaceDiscoverConfig(want.WorkspaceDir, want.BundledSkillsDir, want.ManagedSkillsDir, want.ExtraDirs, want.PluginSkillDirs)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace config = %#v, %v; want %#v", got, err, want)
	}

	defaults := skills.DefaultEligibilityContext()
	ctx := eligibilityContext(nil, nil, nil, nil)
	if !reflect.DeepEqual(ctx, defaults) {
		t.Fatalf("nil eligibility overrides changed defaults:\ngot=%#v\nwant=%#v", ctx, defaults)
	}
	configs := map[string]skills.SkillConfig{"github": {Enabled: boolPointer(false)}}
	allow := []string{"github"}
	values := map[string]bool{"feature": true}
	env := map[string]string{"TOKEN": "x"}
	ctx = eligibilityContext(configs, allow, values, env)
	if !reflect.DeepEqual(ctx.SkillConfigs, configs) || !reflect.DeepEqual(ctx.AllowBundled, allow) ||
		!reflect.DeepEqual(ctx.ConfigValues, values) || !reflect.DeepEqual(ctx.EnvVars, env) {
		t.Fatalf("eligibility overrides = %#v", ctx)
	}
}

func boolPointer(v bool) *bool { return &v }

type transcriptStub struct {
	msgs  []toolport.ChatMessage
	err   error
	key   string
	limit int
}

func (s *transcriptStub) Load(key string, limit int) ([]toolport.ChatMessage, int, error) {
	s.key, s.limit = key, limit
	return s.msgs, len(s.msgs), s.err
}
func (*transcriptStub) Append(string, toolport.ChatMessage) error           { return nil }
func (*transcriptStub) Delete(string) error                                 { return nil }
func (*transcriptStub) ListKeys() ([]string, error)                         { return nil, nil }
func (*transcriptStub) Search(string, int) ([]toolport.SearchResult, error) { return nil, nil }
func (*transcriptStub) CloneRecent(string, string, int) error               { return nil }

func TestBuildSessionContextNilFailureAndRichTranscript(t *testing.T) {
	minimal, err := buildSessionContext(nil, "session:minimal")
	if err != nil || minimal.Key != "session:minimal" || minimal.Turns != 0 || minimal.AllText != "" || len(minimal.ToolActivities) != 0 {
		t.Fatalf("minimal session context = %#v, %v", minimal, err)
	}

	wantErr := errors.New("partial transcript read")
	failing := &transcriptStub{err: wantErr}
	failed, err := buildSessionContext(failing, "session:broken")
	if !errors.Is(err, wantErr) || failed.Key != "session:broken" {
		t.Fatalf("failed session context = %#v, %v", failed, err)
	}
	if failing.key != "session:broken" || failing.limit != 200 {
		t.Fatalf("Load args = %q, %d", failing.key, failing.limit)
	}

	store := &transcriptStub{msgs: []toolport.ChatMessage{
		toolport.NewTextChatMessage("user", "please inspect", 1),
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"working"},{"type":"tool_use","name":"read"},{"type":"tool_use","name":"grep"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","name":"read"},{"type":"tool_use","name":""}]`)},
		toolport.NewTextChatMessage("tool", "result", 4),
		{Role: "assistant"},
	}}
	got, err := buildSessionContext(store, "session:rich")
	if err != nil {
		t.Fatalf("buildSessionContext: %v", err)
	}
	if got.Key != "session:rich" || got.Turns != 3 {
		t.Fatalf("session identity/turns = %#v", got)
	}
	// A tool_use-only message contributes no TEXT: it used to paste its raw
	// serialized blocks into the transcript that downstream classifiers read,
	// which in production meant a thinking block's 4KB signature crowding out
	// the conversation. Its tools are still counted below via ToolActivities.
	if got.AllText != "user: please inspect\nassistant: working\ntool: result" {
		t.Fatalf("AllText = %q", got.AllText)
	}
	names := make([]string, 0, len(got.ToolActivities))
	for _, activity := range got.ToolActivities {
		names = append(names, activity.Name)
	}
	sort.Strings(names)
	if want := []string{"grep", "read"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tool activities = %#v, want %#v", names, want)
	}
}

func TestExtractToolNamesMalformedAndMixedBlocks(t *testing.T) {
	cases := []struct {
		name    string
		content json.RawMessage
		want    []string
	}{
		{name: "nil", content: nil, want: nil},
		{name: "string", content: json.RawMessage(`"read"`), want: nil},
		{name: "object", content: json.RawMessage(`{"type":"tool_use","name":"read"}`), want: nil},
		{name: "malformed", content: json.RawMessage(`[{`), want: nil},
		{name: "empty", content: json.RawMessage(`[]`), want: nil},
		{name: "mixed", content: json.RawMessage(`[
          {"type":"text","name":"ignored"},
          {"type":"tool_use","name":"read"},
          {"type":"tool_use","name":""},
          {"type":"tool_result","name":"grep"},
          {"type":"tool_use","name":"github:search"}
        ]`), want: []string{"read", "github:search"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractToolNames(tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractToolNames = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestGenesisMethodsReturnEmptyWithoutServices(t *testing.T) {
	if got := GenesisMethods(GenesisDeps{}); len(got) != 0 {
		t.Fatalf("empty genesis methods = %#v", got)
	}
	// Concrete service behavior is exhaustively tested in domain/skills/genesis;
	// this handler contract pins that unrelated nil dependencies do not register
	// handlers that would panic on first request.
}
