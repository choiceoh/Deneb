package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// A later-turn read of a spill produced by an external-origin tool must taint
// the turn, so the irreversible-tool gate closes.
//
// Spills now outlive the run that created them, so a blob fetched by `web` on
// turn N is readable on turn N+5 — carrying the same attacker-authored text,
// but outside the turn `web` tainted. Marking the turn context is not enough on
// its own: the gate has to consult it for a plain read_spillover result too.
func TestGateTaintsOnExternalOriginSpillRead(t *testing.T) {
	g := &untrustedToolGate{}
	tc := &toolport.TurnContext{}
	g.bindTurnContext(tc)

	// ToolRegistry.Execute marks this when the spill's origin tool is external.
	tc.MarkExternalOriginTouched()
	g.observeToolResult("read_spillover", "", "clean paraphrase, no signature", false)

	blocked, reason := g.beforeToolCall("exec", "", []byte(`{"command":"rm -rf /tmp/x"}`))
	if !blocked {
		t.Fatal("irreversible tool allowed after reading an external-origin spill — cross-turn sleeper gap is open")
	}
	if !strings.Contains(reason, "차단") && reason == "" {
		t.Errorf("block reason should explain itself, got %q", reason)
	}
}

// An operator-owned spill (exec, read, grep) must NOT taint: the turn context
// flag is only ever set for external origins, so a clean read stays clean.
func TestGateDoesNotTaintOnOperatorOwnedSpillRead(t *testing.T) {
	g := &untrustedToolGate{}
	g.bindTurnContext(&toolport.TurnContext{}) // never marked

	g.observeToolResult("read_spillover", "", "plain output", false)

	if blocked, _ := g.beforeToolCall("exec", "", []byte(`{"command":"ls"}`)); blocked {
		t.Error("operator-owned spill read tainted the turn — this over-blocks ordinary work")
	}
}

// Nested provenance is attributed per invocation, not by the turn-wide flag and
// not by a counter sampled around the call. A purely local code_action running
// in a turn where `web` already ran — or running CONCURRENTLY with one — must
// not have its spill labelled external: that label is permanent, so it would
// taint (and block exec on) every later turn that reads the blob.
func TestOriginSinkAttributesNestedReadsPerInvocation(t *testing.T) {
	tc := &toolport.TurnContext{}

	// An unrelated external read elsewhere in the turn.
	tc.MarkExternalOriginTouched()

	// This invocation's sink: nothing nested inside it read externally.
	own := &toolport.OriginSink{}
	if own.Touched() {
		t.Fatal("a fresh sink must start clean")
	}
	// A parallel sibling marking the TURN must not reach this invocation.
	tc.MarkExternalOriginTouched()
	if own.Touched() {
		t.Error("a concurrent external reader forged nested provenance — local output would be labelled external")
	}

	// A call nested inside this invocation marks the sink it was handed.
	ctx := toolport.WithOriginSink(context.Background(), own)
	toolport.OriginSinkFromContext(ctx).Mark()
	if !own.Touched() {
		t.Error("nested external read did not reach the invocation's sink — provenance lost")
	}

	// The sticky turn flag alone cannot tell those two apart.
	if !tc.ExternalOriginTouched() {
		t.Error("turn flag should still be set")
	}
}

// A top-level invocation has no parent sink; marking must not panic.
func TestOriginSinkNilSafeAtTopLevel(t *testing.T) {
	toolport.OriginSinkFromContext(context.Background()).Mark()
	if toolport.OriginSinkFromContext(context.Background()).Touched() {
		t.Error("absent sink must read as untouched")
	}
}
