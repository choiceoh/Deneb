package server

import (
	"strings"
	"testing"
)

func TestMailAnalysisAgentToolGateBlocksPersistentWrites(t *testing.T) {
	for _, tt := range []struct {
		name  string
		tool  string
		input string
	}{
		{name: "wiki write", tool: "wiki", input: `{"action":"write"}`},
		{name: "wiki log", tool: "wiki", input: `{"action":" log "}`},
		{name: "wiki close", tool: "wiki", input: `{"action":"close"}`},
		{name: "wiki reopen", tool: "wiki", input: `{"action":"reopen"}`},
		{name: "wiki ingest", tool: "wiki", input: `{"action":"ingest"}`},
		{name: "knowledge record", tool: "knowledge", input: `{"op":"record"}`},
		// Mail analysis reads attacker-authored content and does not enable the
		// untrusted-tool gate, so this gate is the only thing between mail
		// promptware and a durable write. Every alias and every write op.
		{name: "knowledge record via action", tool: "knowledge", input: `{"action":"record"}`},
		{name: "knowledge record via write alias", tool: "knowledge", input: `{"op":"write"}`},
		{name: "knowledge record via korean alias", tool: "knowledge", input: `{"action":"저장"}`},
		{name: "knowledge assert_fact", tool: "knowledge", input: `{"op":"assert_fact","fact_key":"x","value":"y"}`},
		{name: "knowledge forget_fact", tool: "knowledge", input: `{"op":"forget_fact","fact_key":"x"}`},
		{name: "knowledge assert_fact via action", tool: "knowledge", input: `{"action":"assert_fact"}`},
		{name: "knowledge unreadable payload", tool: "knowledge", input: `{"op":`},
		{name: "knowledge missing op", tool: "knowledge", input: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			block, reason := mailAnalysisAgentToolGate(tt.tool, "call-1", []byte(tt.input))
			if !block {
				t.Fatalf("gate allowed %s %s", tt.tool, tt.input)
			}
			if !strings.Contains(reason, "read-only") || !strings.Contains(reason, "Do not retry") {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}

func TestMailAnalysisAgentToolGateAllowsReadOnlyAndUnknownCalls(t *testing.T) {
	for _, tt := range []struct {
		name  string
		tool  string
		input string
	}{
		{name: "wiki search", tool: "wiki", input: `{"action":"search"}`},
		{name: "wiki read", tool: "wiki", input: `{"action":"read"}`},
		{name: "wiki index", tool: "wiki", input: `{"action":"index"}`},
		{name: "wiki daily", tool: "wiki", input: `{"action":"daily"}`},
		{name: "wiki status", tool: "wiki", input: `{"action":"status"}`},
		{name: "knowledge recall", tool: "knowledge", input: `{"op":"recall"}`},
		{name: "knowledge read", tool: "knowledge", input: `{"op":"read","ref":"w:x"}`},
		{name: "knowledge facts read", tool: "knowledge", input: `{"op":"facts","subject":"self"}`},
		{name: "knowledge search alias", tool: "knowledge", input: `{"action":"search"}`},
		{name: "mail archive", tool: "mail_archive", input: `{"action":"attachment"}`},
		{name: "invalid json", tool: "wiki", input: `{`},
		{name: "missing action", tool: "wiki", input: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			block, reason := mailAnalysisAgentToolGate(tt.tool, "call-1", []byte(tt.input))
			if block || reason != "" {
				t.Fatalf("gate blocked %s %s: %q", tt.tool, tt.input, reason)
			}
		})
	}
}
