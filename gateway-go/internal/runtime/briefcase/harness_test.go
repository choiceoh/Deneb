package briefcase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

func TestWorldRejectsEarlyReleaseAndTamperedMaterialization(t *testing.T) {
	pack := writeHarnessCase(t)
	clock := NewManualClock(pack.Manifest.FrozenNow)
	world, err := NewWorld(pack, clock)
	if err != nil {
		t.Fatal(err)
	}
	if got := world.VisibleSourceIDs(); len(got) != 1 || got[0] != "wiki-old" {
		t.Fatalf("initial visible sources = %v", got)
	}
	if err := world.Release([]string{"mail-new"}); !errors.Is(err, ErrSourceNotDue) {
		t.Fatalf("early release error = %v, want ErrSourceNotDue", err)
	}
	if err := clock.AdvanceTo(pack.Manifest.Episodes[0].At); err != nil {
		t.Fatal(err)
	}
	if err := world.Release([]string{"mail-new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := world.Get("gold-contract"); !errors.Is(err, ErrSourceNotVisible) {
		t.Fatalf("sealed source Get error = %v", err)
	}
	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := world.Materialize(root); err != nil {
		t.Fatal(err)
	}
	paths, _ := root.Paths()
	mailPath := filepath.Join(paths.Workspace, "records", "mail", "mail-new.source")
	if data, err := os.ReadFile(mailPath); err != nil || !strings.Contains(string(data), "120") {
		t.Fatalf("materialized mail = %q, %v", data, err)
	}
	if err := os.WriteFile(mailPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := world.Materialize(root); err == nil || !strings.Contains(err.Error(), "was modified") {
		t.Fatalf("tampered materialization error = %v", err)
	}
}

func TestWorldOptionsEnforceTwoArmVisibilityBoundary(t *testing.T) {
	pack := writeHarnessMemoryCase(t)
	rawClock := NewManualClock(pack.Manifest.FrozenNow)
	assistedClock := NewManualClock(pack.Manifest.FrozenNow)
	raw, err := NewWorldWithOptions(pack, rawClock, WorldOptions{IncludeMemory: false})
	if err != nil {
		t.Fatal(err)
	}
	assisted, err := NewWorldWithOptions(pack, assistedClock, WorldOptions{IncludeMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := raw.VisibleSourceIDs(), []string{"wiki-old"}; !equalStrings(got, want) {
		t.Fatalf("raw initial sources = %v, want %v", got, want)
	}
	if got, want := assisted.VisibleSourceIDs(), []string{"wiki-old", "memory-snapshot"}; !equalStrings(got, want) {
		t.Fatalf("assisted initial sources = %v, want %v", got, want)
	}
	if _, err := raw.Get("memory-snapshot"); !errors.Is(err, ErrSourceNotVisible) {
		t.Fatalf("raw memory Get error = %v, want ErrSourceNotVisible", err)
	}

	releaseIDs := pack.Manifest.Episodes[0].ReleaseSourceIDs
	if _, err := raw.ReleaseWithOutcome(releaseIDs); !errors.Is(err, ErrSourceNotDue) {
		t.Fatalf("raw early memory release error = %v, want ErrSourceNotDue", err)
	}
	for _, clock := range []*ManualClock{rawClock, assistedClock} {
		if err := clock.AdvanceTo(pack.Manifest.Episodes[0].At); err != nil {
			t.Fatal(err)
		}
	}
	rawRelease, err := raw.ReleaseWithOutcome(releaseIDs)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(rawRelease.Released, []string{"mail-new"}) || !equalStrings(rawRelease.Withheld, []string{"memory-late"}) {
		t.Fatalf("raw release = %+v", rawRelease)
	}
	assistedRelease, err := assisted.ReleaseWithOutcome(releaseIDs)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(assistedRelease.Released, []string{"mail-new", "memory-late"}) || len(assistedRelease.Withheld) != 0 {
		t.Fatalf("assisted release = %+v", assistedRelease)
	}
	if got, want := raw.VisibleSourceIDs(), []string{"wiki-old", "mail-new"}; !equalStrings(got, want) {
		t.Fatalf("raw final sources = %v, want %v", got, want)
	}
	if got, want := assisted.VisibleSourceIDs(), []string{"wiki-old", "memory-snapshot", "mail-new", "memory-late"}; !equalStrings(got, want) {
		t.Fatalf("assisted final sources = %v, want %v", got, want)
	}
	if _, err := raw.ReleaseWithOutcome([]string{"memory-late"}); !errors.Is(err, ErrSourceNotVisible) {
		t.Fatalf("raw duplicate withheld release error = %v, want ErrSourceNotVisible", err)
	}

	rawRoot, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rawRoot.Close()
	if err := raw.Materialize(rawRoot); err != nil {
		t.Fatal(err)
	}
	rawPaths, _ := rawRoot.Paths()
	if _, err := os.Stat(filepath.Join(rawPaths.Workspace, "records", "wiki", "memory-snapshot.source")); !os.IsNotExist(err) {
		t.Fatalf("raw arm materialized snapshot memory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rawPaths.Workspace, "records", "workfeed", "memory-late.source")); !os.IsNotExist(err) {
		t.Fatalf("raw arm materialized timeline memory: %v", err)
	}
}

func TestToolGateFailClosedBudgets(t *testing.T) {
	gate, err := NewToolGate(casepack.ToolPolicy{
		Default: casepack.ToolDeny, MaxCalls: 5,
		Rules: []casepack.ToolRule{{Name: "mail_archive", Decision: casepack.ToolAllow, MaxCalls: 1}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := gate.RemainingAttempts(); got != 5 {
		t.Fatalf("initial remaining attempts = %d, want 5", got)
	}
	if block, _ := gate.BeforeToolCall("web", "web-1", []byte(`{}`)); !block {
		t.Fatal("denied tool was allowed")
	}
	if got := gate.RemainingAttempts(); got != 4 {
		t.Fatalf("denied attempt did not consume budget: remaining=%d", got)
	}
	if block, reason := gate.BeforeToolCall("web", "web-1", []byte(`{}`)); !block || !strings.Contains(reason, "duplicate") {
		t.Fatalf("denied call id was not reserved: block %v reason %q", block, reason)
	}
	if got := gate.RemainingAttempts(); got != 3 {
		t.Fatalf("duplicate attempt did not consume budget: remaining=%d", got)
	}
	if block, reason := gate.BeforeToolCall("mail_archive", "mail-1", []byte(`{"query":"budget"}`)); block {
		t.Fatalf("allowed tool was blocked: %s", reason)
	}
	if block, reason := gate.BeforeToolCall("mail_archive", "mail-1", []byte(`{"query":"budget"}`)); !block || !strings.Contains(reason, "duplicate") {
		t.Fatalf("duplicate call = block %v reason %q", block, reason)
	}
	if block, reason := gate.BeforeToolCall("mail_archive", "mail-2", []byte(`{"query":"budget"}`)); !block || !strings.Contains(reason, "budget") {
		t.Fatalf("per-tool budget = block %v reason %q", block, reason)
	}
	if got := gate.RemainingAttempts(); got != 0 {
		t.Fatalf("all five attempts were not charged: remaining=%d", got)
	}
	if block, reason := gate.BeforeToolCall("mail_archive", "mail-3", []byte(`{"query":"budget"}`)); !block || !strings.Contains(reason, "global") {
		t.Fatalf("global budget = block %v reason %q", block, reason)
	}
}

func TestFixtureRegistryUsesVisibleRecordsAndOutputOnlyWrites(t *testing.T) {
	pack := writeHarnessCase(t)
	clock := NewManualClock(pack.Manifest.FrozenNow)
	world, err := NewWorld(pack, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := clock.AdvanceTo(pack.Manifest.Episodes[0].At); err != nil {
		t.Fatal(err)
	}
	if err := world.Release([]string{"mail-new"}); err != nil {
		t.Fatal(err)
	}
	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := world.Materialize(root); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(root, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := root.Paths()
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace: paths.Workspace, World: world, Policy: policy,
		ToolPolicy: fixtureRegistryPolicy("mail_archive", "write", "grep"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithToolPreset(context.Background(), string(toolpreset.PresetBriefcase))
	mail, err := registry.Execute(ctx, "mail_archive", json.RawMessage(`{"action":"search","query":"120"}`))
	if err != nil || !strings.Contains(mail, "approved budget: 120") {
		t.Fatalf("mail fixture = %q, %v", mail, err)
	}
	if _, err := registry.Execute(ctx, "write", json.RawMessage(`{"file_path":"records/wiki/wiki-old.source","content":"changed"}`)); err == nil {
		t.Fatal("write to immutable records was allowed")
	}
	if _, err := registry.Execute(ctx, "write", json.RawMessage(`{"file_path":"output/report.md","content":"budget 120"}`)); err != nil {
		t.Fatalf("write output: %v", err)
	}
	grep, err := registry.Execute(ctx, "grep", json.RawMessage(`{"pattern":"budget 120","path":"output"}`))
	if err != nil || !strings.Contains(grep, "output/report.md:1:budget 120") {
		t.Fatalf("pure grep = %q, %v", grep, err)
	}
	canceledBase, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := toolport.WithToolPreset(canceledBase, string(toolpreset.PresetBriefcase))
	for _, tool := range []string{"mail_archive", "grep"} {
		if _, err := registry.Execute(canceled, tool, json.RawMessage(`{"pattern":"budget","query":"budget"}`)); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s ignored canceled context: %v", tool, err)
		}
	}
	if _, err := registry.Execute(ctx, "web", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unregistered network tool was executable")
	}
}

func TestFixtureWikiSchemaIgnoresAmbientActionFields(t *testing.T) {
	pack := writeHarnessCase(t)
	world, err := NewWorld(pack, NewManualClock(pack.Manifest.FrozenNow))
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	policy, err := NewPolicy(root, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace:  paths.Workspace,
		World:      world,
		Policy:     policy,
		ToolPolicy: fixtureRegistryPolicy("wiki"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wikiDef *chat.ToolDef
	for _, def := range registry.Definitions() {
		if def.Name == "wiki" {
			copy := def
			wikiDef = &copy
			break
		}
	}
	if wikiDef == nil {
		t.Fatal("Briefcase registry has no wiki definition")
	}
	properties, ok := wikiDef.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("wiki schema properties = %#v", wikiDef.InputSchema["properties"])
	}
	for _, required := range []string{"offsetBytes", "limitBytes", "recordOffset"} {
		if _, ok := properties[required]; !ok {
			t.Errorf("Briefcase wiki schema is missing bounded record field %q", required)
		}
	}
	schema, err := json.Marshal(wikiDef.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	exposedSurface := strings.ToLower(wikiDef.Description + "\n" + string(schema))
	for _, forbidden := range []string{"status", "index"} {
		if strings.Contains(exposedSurface, forbidden) {
			t.Errorf("Briefcase wiki schema exposes ambient action %q: description=%q schema=%s", forbidden, wikiDef.Description, schema)
		}
	}

	ctx := toolport.WithToolPreset(context.Background(), string(toolpreset.PresetBriefcase))
	baseline, err := registry.Execute(ctx, "wiki", json.RawMessage(`{"query":""}`))
	if err != nil {
		t.Fatalf("baseline wiki fixture query: %v", err)
	}
	for _, inertAction := range []string{"status", "index"} {
		input := json.RawMessage(fmt.Sprintf(`{"action":%q,"query":""}`, inertAction))
		got, err := registry.Execute(ctx, "wiki", input)
		if err != nil {
			t.Fatalf("wiki action %q should remain an inert record-query field: %v", inertAction, err)
		}
		if got != baseline {
			t.Errorf("wiki action %q routed outside the bounded record fixture\ngot:  %s\nwant: %s", inertAction, got, baseline)
		}
	}
}

func TestFixtureRegistryRoutesPhoneWriteOnlyToDeviceTwin(t *testing.T) {
	pack := writeHarnessCase(t)
	clock := NewManualClock(pack.Manifest.FrozenNow)
	world, err := NewWorld(pack, clock)
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	payload := json.RawMessage(`{"to":"notify","text":"hello"}`)
	actionID, err := DerivedDeviceActionID("notify", payload)
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewDeviceTwin(clock, []DevicePlan{{
		ActionID: actionID, Kind: "notify", Payload: payload,
		Status: DeviceConfirmed, Result: json.RawMessage(`{"receipt":"local-only"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(root, PolicyOptions{AllowedDeviceKinds: []string{"notify"}})
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := root.Paths()
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace: paths.Workspace, World: world, Policy: policy, Device: device,
		ToolPolicy: fixtureRegistryPolicy("phone_write"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithToolPreset(context.Background(), string(toolpreset.PresetBriefcase))
	first, err := registry.Execute(ctx, "phone_write", payload)
	if err != nil || !strings.Contains(first, `"status":"confirmed"`) || !strings.Contains(first, "local-only") {
		t.Fatalf("phone_write result = %q, %v", first, err)
	}
	second, err := registry.Execute(ctx, "phone_write", payload)
	if err != nil || !strings.Contains(second, `"duplicate":true`) {
		t.Fatalf("duplicate phone_write result = %q, %v", second, err)
	}
	if ledger := device.Ledger(); len(ledger) != 1 || ledger[0].DuplicateAttempts != 1 {
		t.Fatalf("device ledger = %+v", ledger)
	}
}

func TestChatHarnessRunsActualHandlerWithFrozenWorld(t *testing.T) {
	pack := writeHarnessCase(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests = append(requests, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if len(requests) == 1 {
			fmt.Fprint(w, toolCallSSE("tool-1", "mail_archive", `{"action":"search","query":"120"}`))
			return
		}
		fmt.Fprint(w, textSSE("The approved budget is 120."))
	}))
	defer server.Close()

	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient(server.URL, "test-key"), Model: "test-model",
		TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	result, err := harness.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Episodes) != 1 || result.Episodes[0].Text != "The approved budget is 120." {
		t.Fatalf("episode results = %+v", result.Episodes)
	}
	if result.Episodes[0].OutputTokens <= 0 {
		t.Fatalf("provider omitted usage but SyncResult did not propagate the local output estimate: %+v", result.Episodes[0])
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "mail_archive" || result.ToolCalls[0].Decision != "allowed" {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	if len(requests) != 2 || !strings.Contains(requests[1], "approved budget: 120") {
		t.Fatalf("second model request did not contain fixture evidence: %v", requests)
	}
	if !strings.Contains(requests[0], "Wednesday, July 4, 2029") {
		t.Fatalf("first model request did not use frozen semantic date: %s", requests[0])
	}
	if !strings.Contains(requests[0], "Workspace: /briefcase/workspace") || strings.Contains(requests[0], paths.Workspace) {
		t.Fatalf("first model request did not use stable logical workspace: %s", requests[0])
	}
	if !strings.Contains(requests[0], `"seed":7`) {
		t.Fatalf("first model request did not carry signed sampling seed: %s", requests[0])
	}
	var firstRequest map[string]any
	if err := json.Unmarshal([]byte(requests[0]), &firstRequest); err != nil {
		t.Fatalf("decode first model request: %v", err)
	}
	for field, want := range map[string]float64{
		"temperature":       0,
		"top_p":             1,
		"frequency_penalty": 0,
		"presence_penalty":  0,
	} {
		if got, ok := firstRequest[field].(float64); !ok || got != want {
			t.Fatalf("first model request %s = %#v, want %v", field, firstRequest[field], want)
		}
	}
	if got := result.VisibleSourceIDs; len(got) != 2 || got[0] != "wiki-old" || got[1] != "mail-new" {
		t.Fatalf("visible sources = %v", got)
	}
}

func TestChatHarnessIgnoresProcessGlobalMarketTokens(t *testing.T) {
	pack := writeHarnessCase(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, textSSE(`{{market:usd_krw}}`))
	}))
	defer server.Close()

	market.RecordLetterTokens(map[string]string{market.LetterTokenUSDKRW: "9,999"})
	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient(server.URL, "test-key"), Model: "test-model",
		TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	result, err := harness.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Episodes[0].Text; got != market.LetterTokenUSDKRW {
		t.Fatalf("score-visible text = %q, want raw model token %q", got, market.LetterTokenUSDKRW)
	}
}

func TestChatHarnessRejectsOversizedToolBatchBeforeGate(t *testing.T) {
	pack := writeHarnessCase(t) // signed MaxCalls is four
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		calls := make([]toolCallSSESpec, 5)
		for i := range calls {
			calls[i] = toolCallSSESpec{
				ID: fmt.Sprintf("tool-%d", i+1), Name: "mail_archive", Arguments: `{"action":"search","query":"120"}`,
			}
		}
		fmt.Fprint(w, toolCallsSSE(calls))
	}))
	defer server.Close()

	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient(server.URL, "test-key"), Model: "test-model",
		TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()

	result, err := harness.Run(context.Background())
	if result != nil {
		t.Fatalf("result = %+v, want no completed episode", result)
	}
	if !errors.Is(err, agent.ErrToolCallLimit) {
		t.Fatalf("error = %v, want agent.ErrToolCallLimit", err)
	}
	if got := len(harness.gate.Records()); got != 0 {
		t.Fatalf("over-limit turn created %d gate records, want zero", got)
	}
	if got := harness.gate.RemainingAttempts(); got != 4 {
		t.Fatalf("remaining signed attempts = %d, want 4", got)
	}
	if requests != 1 {
		t.Fatalf("model requests = %d, want one fail-fast response", requests)
	}
}

func TestChatHarnessReportsTwoArmsWithoutCrossRunCollisions(t *testing.T) {
	pack := writeHarnessMemoryCase(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests = append(requests, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, textSSE("done"))
	}))
	defer server.Close()

	type armRun struct {
		result  *RunResult
		harness *ChatHarness
		root    *RunRoot
	}
	runArm := func(t *testing.T, arm Arm) armRun {
		t.Helper()
		root, err := NewRunRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		harness, err := NewChatHarness(ChatHarnessConfig{
			Pack: pack, Root: root, Client: llm.NewClient(server.URL, "test-key"), Model: "test-model", Arm: arm,
			TokenEstimate: tokenest.EstimateUncalibrated,
		})
		if err != nil {
			_ = root.Close()
			t.Fatal(err)
		}
		result, err := harness.Run(t.Context())
		if err != nil {
			harness.Close()
			_ = root.Close()
			t.Fatal(err)
		}
		return armRun{result: result, harness: harness, root: root}
	}
	raw := runArm(t, ArmRawPrimary)
	defer raw.root.Close()
	defer raw.harness.Close()
	assisted := runArm(t, ArmMemoryAssisted)
	defer assisted.root.Close()
	defer assisted.harness.Close()

	if raw.result.Arm != ArmRawPrimary || assisted.result.Arm != ArmMemoryAssisted {
		t.Fatalf("arm accounting: raw=%q assisted=%q", raw.result.Arm, assisted.result.Arm)
	}
	if raw.result.RunID == assisted.result.RunID || raw.harness.sessionKey == assisted.harness.sessionKey {
		t.Fatalf("two arms collided: run IDs %q/%q session keys %q/%q", raw.result.RunID, assisted.result.RunID, raw.harness.sessionKey, assisted.harness.sessionKey)
	}
	if !raw.harness.skipRecall || assisted.harness.skipRecall {
		t.Fatalf("recall gates: raw=%v assisted=%v", raw.harness.skipRecall, assisted.harness.skipRecall)
	}
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want one per arm", len(requests))
	}
	if strings.Contains(requests[0], "durable memory: the budget owner") || strings.Contains(requests[0], `source=\"server-preflight\"`) {
		t.Fatalf("raw arm received Deneb memory recall: %s", requests[0])
	}
	if !strings.Contains(requests[1], `source=\"server-preflight\"`) || !strings.Contains(requests[1], "durable memory: the budget owner") {
		t.Fatalf("memory-assisted arm did not receive isolated Deneb recall: %s", requests[1])
	}
	if got, want := raw.result.VisibleSourceIDs, []string{"wiki-old", "mail-new"}; !equalStrings(got, want) {
		t.Fatalf("raw visible = %v, want %v", got, want)
	}
	if got, want := assisted.result.VisibleSourceIDs, []string{"wiki-old", "memory-snapshot", "mail-new", "memory-late"}; !equalStrings(got, want) {
		t.Fatalf("assisted visible = %v, want %v", got, want)
	}
	rawEpisode := raw.result.Episodes[0]
	if !equalStrings(rawEpisode.ReleasedSource, []string{"mail-new"}) || !equalStrings(rawEpisode.WithheldSource, []string{"memory-late"}) {
		t.Fatalf("raw episode release accounting = %+v", rawEpisode)
	}
	assistedEpisode := assisted.result.Episodes[0]
	if !equalStrings(assistedEpisode.ReleasedSource, []string{"mail-new", "memory-late"}) || len(assistedEpisode.WithheldSource) != 0 {
		t.Fatalf("assisted episode release accounting = %+v", assistedEpisode)
	}
}

func TestChatHarnessRejectsInvalidInputBeforeClaimingRunRoot(t *testing.T) {
	pack := writeHarnessCase(t)
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	client := llm.NewClient("http://127.0.0.1:1", "")

	if _, err := NewChatHarness(ChatHarnessConfig{Pack: pack, Root: root, Client: client, TokenEstimate: tokenest.EstimateUncalibrated}); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("missing model error = %v", err)
	}
	if _, err := NewChatHarness(ChatHarnessConfig{Pack: pack, Root: root, Client: client, Model: "test", Arm: Arm("unsupported"), TokenEstimate: tokenest.EstimateUncalibrated}); err == nil || !strings.Contains(err.Error(), "unsupported arm") {
		t.Fatalf("unsupported arm error = %v", err)
	}
	harness, err := NewChatHarness(ChatHarnessConfig{Pack: pack, Root: root, Client: client, Model: "test", TokenEstimate: tokenest.EstimateUncalibrated})
	if err != nil {
		t.Fatalf("valid construction after pre-claim validation failures: %v", err)
	}
	defer harness.Close()
}

func TestChatHarnessRejectsRunRootReuseAcrossArms(t *testing.T) {
	pack := writeHarnessMemoryCase(t)
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	client := llm.NewClient("http://127.0.0.1:1", "")
	first, err := NewChatHarness(ChatHarnessConfig{Pack: pack, Root: root, Client: client, Model: "test", Arm: ArmRawPrimary, TokenEstimate: tokenest.EstimateUncalibrated})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	paths, _ := root.Paths()
	if err := os.RemoveAll(filepath.Join(paths.Workspace, "records")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(paths.Workspace, "output")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewChatHarness(ChatHarnessConfig{Pack: pack, Root: root, Client: client, Model: "test", Arm: ArmMemoryAssisted, TokenEstimate: tokenest.EstimateUncalibrated}); !errors.Is(err, ErrRunRootClaimed) {
		t.Fatalf("reused RunRoot error = %v, want single-use claim rejection", err)
	}
}

func TestChatHarnessHashesCallerSessionKeyAndRejectsAbnormalStops(t *testing.T) {
	pack := writeHarnessCase(t)
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient("http://127.0.0.1:1", ""), Model: "test",
		SessionKey:    "../../../../outside/session",
		TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	if filepath.Base(harness.sessionKey) != harness.sessionKey || strings.ContainsAny(harness.sessionKey, `/\\`) || !strings.HasPrefix(harness.sessionKey, "briefcase-") {
		t.Fatalf("unsafe internal session key = %q", harness.sessionKey)
	}
	if err := validateTurnCompletion(&chat.SyncResult{StopReason: "timeout", AllText: "partial"}); !errors.Is(err, ErrTurnTimeout) {
		t.Fatalf("timeout completion error = %v", err)
	}
	if err := validateTurnCompletion(&chat.SyncResult{StopReason: "aborted"}); !errors.Is(err, ErrTurnIncomplete) {
		t.Fatalf("aborted completion error = %v", err)
	}
	if err := validateTurnCompletion(&chat.SyncResult{StopReason: "end_turn"}); err != nil {
		t.Fatalf("normal completion error = %v", err)
	}
}

func TestChatHarnessRejectsMissingDevicePlanSource(t *testing.T) {
	pack := writeHarnessDeviceCase(t)
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient("http://127.0.0.1:1", ""), Model: "test",
		TokenEstimate: tokenest.EstimateUncalibrated,
	}); err == nil || !strings.Contains(err.Error(), "must be loaded") {
		t.Fatalf("missing signed device plan error = %v", err)
	}

	var role casepack.Source
	for _, source := range pack.Manifest.Sources {
		if source.SourceRef == "briefcase:device-plan" {
			role = source
		}
	}
	data, err := pack.ReadFile(role.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient("http://127.0.0.1:1", ""), Model: "test",
		DevicePlanSource: data, DevicePlanSourceSHA256: role.SHA256,
		TokenEstimate: tokenest.EstimateUncalibrated,
	}); !errors.Is(err, ErrRunRootClaimed) {
		t.Fatalf("post-claim device-plan retry error = %v, want ErrRunRootClaimed", err)
	}
	configuredRoot, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer configuredRoot.Close()
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: configuredRoot, Client: llm.NewClient("http://127.0.0.1:1", ""), Model: "test",
		DevicePlanSource: data, DevicePlanSourceSHA256: role.SHA256,
		TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		t.Fatalf("configured signed device plan: %v", err)
	}
	defer harness.Close()
}

func TestChatHarnessConstructionFailureLeavesRunRootCleanable(t *testing.T) {
	pack := writeHarnessCase(t)
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.State, "transcripts"), []byte("block transcript setup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient("http://127.0.0.1:1", ""), Model: "test",
		TokenEstimate: tokenest.EstimateUncalibrated,
	}); err == nil || !strings.Contains(err.Error(), "transcript path must be a real directory") {
		t.Fatalf("transcript setup error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.State, "briefcase-memory")); err != nil {
		t.Fatalf("failure did not reach post-memory assembly: %v", err)
	}
	if err := root.Close(); err != nil {
		t.Fatalf("cleanup after post-memory construction failure: %v", err)
	}
	if _, err := os.Stat(paths.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run root remains after cleanup: %v", err)
	}
}

func TestValidateRunProvenanceRejectsDerivedFieldMutation(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	valid := &RunResult{
		Model:            "requested-model",
		ProviderModel:    "served-model",
		APIMode:          llm.APIModeOpenAI,
		CasepackSHA256:   digest,
		ToolSchemaSHA256: digest,
		EndpointSHA256:   digest,
		BuildSHA256:      digest,
		Episodes: []EpisodeResult{{
			EpisodeID: "episode-1", Model: "requested-model", ProviderModel: "served-model",
			StopReason: "end_turn", SystemPromptSHA256: digest, Text: "done",
		}},
	}
	if err := SetRunProvenance(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRunProvenance(valid); err != nil {
		t.Fatalf("valid provenance rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RunResult)
		want   string
	}{
		{
			name: "sampling",
			mutate: func(result *RunResult) {
				result.Sampling.Temperature = 1
			},
			want: "sampling profile",
		},
		{
			name: "system prompt sequence",
			mutate: func(result *RunResult) {
				result.SystemPromptSequenceSHA256 = strings.Repeat("f", 64)
			},
			want: "system prompt sequence digest mismatch",
		},
		{
			name: "execution profile",
			mutate: func(result *RunResult) {
				result.ExecutionProfileSHA256 = strings.Repeat("f", 64)
			},
			want: "execution profile digest mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := cloneRunResult(valid)
			tt.mutate(mutated)
			if err := ValidateRunProvenance(mutated); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("mutation error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestExecutionProfileBindsDeterministicStreamAndToolPolicies(t *testing.T) {
	profile := fixedExecutionProfile("model", llm.APIModeAnthropic, "tools", "endpoint", "build", SamplingProfile{})
	if profile.StreamIdleTimeoutMillis != chat.BriefcaseStreamIdleTimeout.Milliseconds() {
		t.Fatalf("stream idle timeout provenance = %dms, want %dms", profile.StreamIdleTimeoutMillis, chat.BriefcaseStreamIdleTimeout.Milliseconds())
	}
	if profile.ParallelToolsEnabled {
		t.Fatal("execution profile must bind the fixed sequential tool policy")
	}
	if profile.PromptCacheEnabled {
		t.Fatal("execution profile must bind endpoint cache metadata as disabled")
	}
}

func writeHarnessCase(t *testing.T) *casepack.Pack {
	return writeHarnessCaseOptions(t, false, false)
}

func writeHarnessMemoryCase(t *testing.T) *casepack.Pack {
	return writeHarnessCaseOptions(t, true, false)
}

func writeHarnessDeviceCase(t *testing.T) *casepack.Pack {
	return writeHarnessCaseOptions(t, false, true)
}

func writeHarnessCaseOptions(t *testing.T, includeMemory, includeDevicePlan bool) *casepack.Pack {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"snapshot/wiki-old.md":     "approved budget: 100\n",
		"timeline/mail-new.eml":    "Subject: revised budget\n\napproved budget: 120\n",
		"timeline/task-1.md":       "Find the latest approved budget and answer with evidence.\n",
		"sealed/gold-contract.txt": "signed budget: 120\n",
	}
	if includeMemory {
		files["snapshot/memory-snapshot.md"] = "durable memory: the budget owner prefers evidence tables\n"
		files["timeline/memory-late.md"] = "durable memory update: cite the signed revision\n"
	}
	if includeDevicePlan {
		files["sealed/device-plan.json"] = `{"plans":[{"actionId":"notify-1","kind":"notify","payload":{"text":"briefcase"},"status":"confirmed","result":{"receipt":"local"}}]}`
	}
	for relative, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Date(2029, time.July, 4, 9, 0, 0, 0, time.UTC)
	mailAt := cutoff.Add(time.Hour)
	sources := []casepack.Source{
		{
			ID: "wiki-old", Kind: casepack.SourceWiki, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessSnapshot,
			Path: "snapshot/wiki-old.md", SHA256: casepack.DigestBytes([]byte(files["snapshot/wiki-old.md"])), EventAt: cutoff.Add(-time.Hour), AvailableAt: cutoff.Add(-time.Hour), CapturedAt: cutoff,
		},
		{
			ID: "mail-new", Kind: casepack.SourceMail, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessTimeline,
			Path: "timeline/mail-new.eml", SHA256: casepack.DigestBytes([]byte(files["timeline/mail-new.eml"])), EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt, Supersedes: []string{"wiki-old"},
		},
		{
			ID: "gold-contract", Kind: casepack.SourceFile, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessSealed,
			Path: "sealed/gold-contract.txt", SHA256: casepack.DigestBytes([]byte(files["sealed/gold-contract.txt"])), EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt,
		},
	}
	releaseSourceIDs := []string{"mail-new"}
	if includeMemory {
		sources = append(
			sources,
			casepack.Source{
				ID: "memory-snapshot", Kind: casepack.SourceWiki, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessSnapshot,
				Path: "snapshot/memory-snapshot.md", SHA256: casepack.DigestBytes([]byte(files["snapshot/memory-snapshot.md"])), EventAt: cutoff.Add(-30 * time.Minute), AvailableAt: cutoff.Add(-30 * time.Minute), CapturedAt: cutoff, Memory: true,
			},
			casepack.Source{
				ID: "memory-late", Kind: casepack.SourceWorkfeed, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessTimeline,
				Path: "timeline/memory-late.md", SHA256: casepack.DigestBytes([]byte(files["timeline/memory-late.md"])), EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt, Memory: true,
			},
		)
		releaseSourceIDs = append(releaseSourceIDs, "memory-late")
	}
	if includeDevicePlan {
		sources = append(sources, casepack.Source{
			ID: "device-plan", Kind: casepack.SourceFile, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessSealed,
			Path: "sealed/device-plan.json", SHA256: casepack.DigestBytes([]byte(files["sealed/device-plan.json"])),
			EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt, SourceRef: "briefcase:device-plan",
		})
	}
	manifest := casepack.Manifest{
		SchemaVersion: casepack.SchemaVersionV1,
		CaseID:        "portable-budget", FamilyID: "budget", Split: casepack.SplitDev,
		PrivacyMode: casepack.PrivacyPortable, Seed: 7,
		CutoffAt: cutoff, FrozenNow: cutoff, Timezone: "Asia/Seoul", Locale: "ko-KR",
		Sources: sources,
		Episodes: []casepack.Episode{{
			ID: "episode-1", Kind: casepack.EpisodeUserTurn, At: mailAt,
			Input: &casepack.FileRef{Path: "timeline/task-1.md", SHA256: casepack.DigestBytes([]byte(files["timeline/task-1.md"]))}, ReleaseSourceIDs: releaseSourceIDs,
		}},
		RunPolicy: casepack.RunPolicy{MaxTurns: 8, TimeoutSeconds: 30, MaxTokens: 2048, MaxFollowUps: 1},
		ToolPolicy: casepack.ToolPolicy{Default: casepack.ToolDeny, MaxCalls: 4, Rules: []casepack.ToolRule{
			{Name: "mail_archive", Decision: casepack.ToolAllow, MaxCalls: 2},
			{Name: "read", Decision: casepack.ToolAllow, MaxCalls: 1},
			{Name: "write", Decision: casepack.ToolAllow, MaxCalls: 1},
		}},
		NetworkPolicy: casepack.NetworkPolicy{Mode: casepack.NetworkDeny},
	}
	digest, err := casepack.CanonicalDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestDigest = digest
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, casepack.ManifestFile), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	pack, err := casepack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func fixtureRegistryPolicy(names ...string) casepack.ToolPolicy {
	rules := make([]casepack.ToolRule, 0, len(names))
	for _, name := range names {
		rules = append(rules, casepack.ToolRule{Name: name, Decision: casepack.ToolAllow, MaxCalls: 100})
	}
	return casepack.ToolPolicy{Default: casepack.ToolDeny, MaxCalls: 1000, Rules: rules}
}

func toolCallSSE(id, name, arguments string) string {
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":null}]}\n\n"+
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"+
		"data: [DONE]\n\n", id, name, arguments)
}

type toolCallSSESpec struct {
	ID        string
	Name      string
	Arguments string
}

func toolCallsSSE(calls []toolCallSSESpec) string {
	toolCalls := make([]map[string]any, len(calls))
	for i, call := range calls {
		toolCalls[i] = map[string]any{
			"index": i,
			"id":    call.ID,
			"type":  "function",
			"function": map[string]any{
				"name": call.Name, "arguments": call.Arguments,
			},
		}
	}
	first, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-batch", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": toolCalls}, "finish_reason": nil,
		}},
	})
	finish, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-batch", "object": "chat.completion.chunk", "model": "test",
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls",
		}},
	})
	return fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, finish)
}

func textSSE(text string) string {
	encoded, _ := json.Marshal(text)
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":null}]}\n\n"+
		"data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
		"data: [DONE]\n\n", encoded)
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
