//go:build promptaudit

// Prompt-mass audit harness. Reproduces the production system-prompt assembly
// (run_prepare.go path) against a real workspace and dumps each block plus the
// EAGER tools wire JSON to /tmp/prompt-audit/ for tokenization — the input-side
// counterpart of the effort router's output-side savings. Excluded from normal
// builds/CI by the build tag; needs a live workspace, so it is operator-run:
//
//	AUDIT_WS=$HOME/.deneb/workspace go test -tags promptaudit \
//	    -run TestPromptAudit ./internal/pipeline/chat/ -v
//
// Tokenize the artifacts against the serving engine, e.g.:
//
//	curl -s http://127.0.0.1:8000/tokenize -H 'Content-Type: application/json' \
//	    -d "$(jq -n --rawfile p /tmp/prompt-audit/block-2-dynamic.txt \
//	          '{model:"deepseek-v4-flash",prompt:$p}')" | jq .count
//
// First run (2026-06-12, ~26K-token input): dynamic block 8,175 tok (context
// files), eager tools ~3.9K, static 3,577, skills index 1,830 — found AGENTS.md
// over its 8K budget (silently head/tail-truncated every turn) and led to the
// graphify deferral (-1.2K tok/turn).
package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolreg"
)

func TestPromptAudit(t *testing.T) {
	ws := os.Getenv("AUDIT_WS")
	if ws == "" {
		t.Skip("AUDIT_WS not set (operator-run harness)")
	}
	outDir := "/tmp/prompt-audit"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	reg := NewToolRegistry()
	toolreg.RegisterCoreTools(reg, &toolctx.CoreToolDeps{WorkspaceDir: ws})
	// NOTE: deps-gated registrations (wiki, polaris, kv, …) are absent here, so
	// the eager set is a subset of production's. The per-tool numbers and the
	// block structure are what the audit is for; compare tool COUNT against a
	// live "starting agent loop … tools=N" log line when reconciling totals.

	var eager, deferred []toolctx.ToolDef
	for _, d := range reg.FilteredDefinitions(nil) {
		if d.Hidden {
			continue
		}
		if d.Deferred {
			deferred = append(deferred, d)
		} else {
			eager = append(eager, d)
		}
	}
	t.Logf("tools: %d eager on the wire, %d deferred (80-char listing only)", len(eager), len(deferred))

	var deferredInfos []prompt.DeferredToolInfo
	for _, ds := range reg.DeferredSummaries() {
		deferredInfos = append(deferredInfos, prompt.DeferredToolInfo{Name: ds.Name, Description: ds.Description})
	}

	tz, _ := prompt.LoadCachedTimezone()
	spp := prompt.SystemPromptParams{
		WorkspaceDir:  ws,
		ToolDefs:      toPromptToolDefs(reg.FilteredDefinitions(nil)),
		DeferredTools: deferredInfos,
		UserTimezone:  tz,
		ContextFiles:  prompt.LoadContextFiles(ws),
		RuntimeInfo:   prompt.BuildDefaultRuntimeInfo("deepseek-v4-flash", "vllm/deepseek-v4-flash"),
		Channel:       "client",
		SkillsPrompt:  loadCachedSkillsPrompt(ws, availableToolNames(reg)),
	}
	blocks := prompt.BuildSystemPromptBlocks(spp)

	labels := []string{"static", "semistatic", "dynamic"}
	total := 0
	for i, b := range blocks {
		label := fmt.Sprintf("block-%d", i)
		if i < len(labels) {
			label = fmt.Sprintf("block-%d-%s", i, labels[i])
		}
		if err := os.WriteFile(filepath.Join(outDir, label+".txt"), []byte(b.Text), 0o644); err != nil {
			t.Fatal(err)
		}
		total += len(b.Text)
		t.Logf("%s: %d bytes (cache=%v)", label, len(b.Text), b.CacheControl != nil)
	}
	t.Logf("system blocks total: %d bytes", total)

	// Eager tools wire JSON — what the OpenAI request "tools" array carries
	// (openai.go: {type:function, function:{name,description,parameters}}).
	// Deferred tools are excluded by buildLLMToolsLocked and cost nothing here.
	type fn struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	type wireTool struct {
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	wire := make([]wireTool, 0, len(eager))
	for _, d := range eager {
		wire = append(wire, wireTool{Type: "function", Function: fn{Name: d.Name, Description: d.Description, Parameters: d.InputSchema}})
	}
	tj, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "tools-eager.json"), tj, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("eager tools wire JSON: %d bytes for %d tools", len(tj), len(wire))
	for _, d := range deferred {
		t.Logf("deferred (not on wire): %s", d.Name)
	}
}
