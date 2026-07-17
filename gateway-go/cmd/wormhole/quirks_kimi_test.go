package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func kimiBlocksOf(t *testing.T, body []byte, msgIndex int) []map[string]any {
	t.Helper()
	var req struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	return req.Messages[msgIndex].Content
}

// The live-bisected poison shape: a steering text merged in front of the
// turn's tool_results. The kimi profile must move the results first while
// preserving both relative orders and every other request field.
func TestApplyKimiQuirksReordersToolResultsFirst(t *testing.T) {
	body := []byte(`{"model":"k3[1m]","max_tokens":64,"messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call_a","name":"web","input":{}},{"type":"tool_use","id":"call_b","name":"web","input":{}}]},` +
		`{"role":"user","content":[{"type":"text","text":"steer: do it differently"},{"type":"tool_result","tool_use_id":"call_a","content":"A"},{"type":"tool_result","tool_use_id":"call_b","content":"B"}]}` +
		`]}`)
	out := applyKimiQuirks(body)
	blocks := kimiBlocksOf(t, out, 1)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	if blocks[0]["tool_use_id"] != "call_a" || blocks[1]["tool_use_id"] != "call_b" {
		t.Fatalf("tool_results not first / order lost: %+v", blocks)
	}
	if blocks[2]["type"] != "text" {
		t.Fatalf("text block lost: %+v", blocks[2])
	}
	if !strings.Contains(string(out), `"max_tokens":64`) {
		t.Fatalf("sibling field lost: %s", out)
	}
	// Assistant message untouched (text-before-tool_use is the normal shape).
	if asst := kimiBlocksOf(t, out, 0); asst[0]["type"] != "tool_use" || asst[0]["id"] != "call_a" {
		t.Fatalf("assistant message rewritten: %+v", asst)
	}
}

// Blank text blocks riding real blocks are dropped; blank thinking survives
// only with a signature; cache_control-bearing blanks are never dropped
// (they are cache breakpoints).
func TestApplyKimiQuirksDropsBlankInertBlocks(t *testing.T) {
	body := []byte(`{"model":"k3[1m]","messages":[` +
		`{"role":"assistant","content":[{"type":"text","text":""},{"type":"thinking","thinking":""},{"type":"thinking","thinking":"","signature":"sig1"},{"type":"tool_use","id":"c1","name":"fs","input":{}}]},` +
		`{"role":"user","content":[{"type":"text","text":"","cache_control":{"type":"ephemeral"}},{"type":"text","text":"real"}]}` +
		`]}`)
	out := applyKimiQuirks(body)
	asst := kimiBlocksOf(t, out, 0)
	if len(asst) != 2 {
		t.Fatalf("assistant: want signed thinking + tool_use, got %+v", asst)
	}
	if asst[0]["signature"] != "sig1" || asst[1]["type"] != "tool_use" {
		t.Fatalf("wrong survivors: %+v", asst)
	}
	user := kimiBlocksOf(t, out, 1)
	if len(user) != 2 {
		t.Fatalf("user: cache_control blank must survive, got %+v", user)
	}
}

// A message whose blocks are ALL blank is left alone — stripping to an empty
// array would trade one 400 for another; the upstream owns that rejection.
func TestApplyKimiQuirksKeepsAllBlankMessage(t *testing.T) {
	body := []byte(`{"model":"k3[1m]","messages":[{"role":"user","content":[{"type":"text","text":""}]}]}`)
	out := applyKimiQuirks(body)
	if len(kimiBlocksOf(t, out, 0)) != 1 {
		t.Fatalf("all-blank message must pass through: %s", out)
	}
}

// tool_use with null or missing input gets {} backfilled.
func TestApplyKimiQuirksBackfillsToolUseInput(t *testing.T) {
	body := []byte(`{"model":"k3[1m]","messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"fs","input":null},{"type":"tool_use","id":"c2","name":"fs"}]}` +
		`]}`)
	out := applyKimiQuirks(body)
	blocks := kimiBlocksOf(t, out, 0)
	for i, b := range blocks {
		in, ok := b["input"].(map[string]any)
		if !ok || len(in) != 0 {
			t.Fatalf("block %d input not backfilled to {}: %+v", i, b)
		}
	}
}

// A clean request passes through as the identical byte slice — no re-marshal,
// no key reordering (prompt-cache hygiene for the hot path).
func TestApplyKimiQuirksCleanRequestByteIdentical(t *testing.T) {
	body := []byte(`{"model":"k3[1m]","max_tokens":9,"messages":[` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"ok"},{"type":"text","text":"next"}]},` +
		`{"role":"assistant","content":"plain string reply"},` +
		`{"role":"user","content":"plain string ask"}` +
		`]}`)
	out := applyKimiQuirks(body)
	if string(out) != string(body) {
		t.Fatalf("clean request rewritten:\n in: %s\nout: %s", body, out)
	}
}

// Malformed / non-chat JSON passes through untouched.
func TestApplyKimiQuirksPassesThroughOddBodies(t *testing.T) {
	for _, body := range []string{
		`not json at all`,
		`{"model":"k3[1m]"}`,
		`{"model":"k3[1m]","messages":"weird"}`,
	} {
		if out := applyKimiQuirks([]byte(body)); string(out) != body {
			t.Fatalf("odd body rewritten: %q -> %q", body, out)
		}
	}
}

func TestKimiBadRequestHint(t *testing.T) {
	if h := kimiBadRequestHint([]byte(`{"error":{"message":"... tool_call_ids did not have response messages: web:24, web:25"}}`)); h == "" {
		t.Fatal("want a hint for the tool_call_ids signature")
	}
	if h := kimiBadRequestHint([]byte(`{"error":{"message":"Invalid request: text content is empty"}}`)); h == "" {
		t.Fatal("want a hint for the empty-text signature")
	}
	if h := kimiBadRequestHint([]byte(`{"error":{"message":"quota exceeded"}}`)); h != "" {
		t.Fatalf("unknown signature must not hint: %q", h)
	}
}
