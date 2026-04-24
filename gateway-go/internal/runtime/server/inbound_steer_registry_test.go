package server

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
)

type fakeLookup struct {
	ids       map[string]map[string]struct{}
	callCount atomic.Int64
	mu        sync.RWMutex
}

func (f *fakeLookup) HasSubagent(sessionKey, token string) bool {
	f.callCount.Add(1)
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.ids == nil {
		return false
	}
	session := f.ids[sessionKey]
	if session == nil {
		return false
	}
	_, ok := session[token]
	return ok
}

func newFakeLookup(sessionKey string, activeIDs ...string) *fakeLookup {
	f := &fakeLookup{ids: make(map[string]map[string]struct{})}
	bucket := make(map[string]struct{}, len(activeIDs))
	for _, id := range activeIDs {
		bucket[id] = struct{}{}
	}
	f.ids[sessionKey] = bucket
	return f
}

func TestParseSteerCommand_RegistryHit_RoutesToSubagent(t *testing.T) {
	reg := newFakeLookup("session:main", "1", "worker", "abc123")

	cases := []struct {
		name, body, wantNote, wantSubID, sessionKey string
		wantKind                                    SteerKind
	}{
		{"numeric index hits", "/steer 1 please focus", "please focus", "1", "session:main", SteerSubagent},
		{"word-like label hits", "/steer worker run tests", "run tests", "worker", "session:main", SteerSubagent},
		{"hex-like id hits", "/steer abc123 do it", "do it", "abc123", "session:main", SteerSubagent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, note, id := parseSteerCommand(tc.body, tc.sessionKey, reg)
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", kind, tc.wantKind)
			}
			if note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
			if id != tc.wantSubID {
				t.Errorf("subagentID = %q, want %q", id, tc.wantSubID)
			}
		})
	}
}

func TestParseSteerCommand_RegistryMiss_RoutesToMainAgent(t *testing.T) {
	reg := newFakeLookup("session:other", "1", "worker")

	cases := []struct {
		name, body, sessionKey, wantNote string
	}{
		{"numeric-looking", "/steer 1 go faster", "session:main", "1 go faster"},
		{"hex-looking", "/steer abc12345 do it", "session:main", "abc12345 do it"},
		{"word-like", "/steer stranger please focus", "session:main", "stranger please focus"},
		{"korean", "/steer 테스트는 건너뛰어", "session:main", "테스트는 건너뛰어"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, note, id := parseSteerCommand(tc.body, tc.sessionKey, reg)
			if kind != SteerMainAgent {
				t.Fatalf("kind = %v, want SteerMainAgent", kind)
			}
			if note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
			if id != "" {
				t.Errorf("subagentID = %q, want empty", id)
			}
		})
	}
}

func TestParseSteerCommand_CrossSession_DoesNotLeak(t *testing.T) {
	reg := newFakeLookup("session:A", "1")
	kind, note, id := parseSteerCommand("/steer 1 help", "session:B", reg)
	if kind != SteerMainAgent {
		t.Fatalf("kind=%v, want SteerMainAgent", kind)
	}
	if note != "1 help" {
		t.Errorf("note = %q, want %q", note, "1 help")
	}
	if id != "" {
		t.Errorf("subagentID = %q, want empty", id)
	}
}

func TestParseSteerCommand_NilRegistry_PreservesHeuristic(t *testing.T) {
	cases := []struct {
		name, body, wantNote string
		wantKind             SteerKind
	}{
		{"plain word", "/steer skip the tests", "skip the tests", SteerMainAgent},
		{"korean", "/steer 테스트는 건너뛰어", "테스트는 건너뛰어", SteerMainAgent},
		{"mixed case prefix", "/Steer please run lint", "please run lint", SteerMainAgent},
		{"numeric id heuristic", "/steer 1 go faster", "", SteerNone},
		{"3-digit id heuristic", "/steer 42 retry", "", SteerNone},
		{"hex prefix heuristic", "/steer abc12345 do it", "", SteerNone},
		{"no prefix", "hello", "", SteerNone},
		{"prefix only", "/steer", "", SteerNone},
		{"prefix whitespace only", "/steer   ", "", SteerNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, note, _ := parseSteerCommand(tc.body, "session:main", nil)
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v (note=%q)", kind, tc.wantKind, note)
			}
			if note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
		})
	}
}

func TestParseSteerCommand_NotSteerCommand(t *testing.T) {
	reg := newFakeLookup("session:main", "1", "worker")
	cases := []string{"", "hello world", "/reset now", "/steerage", "/steer-at-target"}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			kind, note, id := parseSteerCommand(body, "session:main", reg)
			if kind != SteerNone {
				t.Errorf("kind = %v, want SteerNone (note=%q id=%q)", kind, note, id)
			}
		})
	}
}

func TestParseSteerCommand_ConcurrentRace(t *testing.T) {
	reg := newFakeLookup("session:main", "worker", "1")
	var wg sync.WaitGroup
	for w := range 16 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for i := range 200 {
				switch (idx + i) % 3 {
				case 0:
					kind, _, id := parseSteerCommand("/steer worker focus now", "session:main", reg)
					if kind != SteerSubagent || id != "worker" {
						t.Errorf("expected SteerSubagent worker, got kind=%v id=%q", kind, id)
						return
					}
				case 1:
					kind, note, _ := parseSteerCommand("/steer stranger help", "session:main", reg)
					if kind != SteerMainAgent || note != "stranger help" {
						t.Errorf("expected SteerMainAgent, got kind=%v note=%q", kind, note)
						return
					}
				case 2:
					kind, _, _ := parseSteerCommand("hello", "session:main", reg)
					if kind != SteerNone {
						t.Errorf("expected SteerNone, got %v", kind)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	if got := reg.callCount.Load(); got == 0 {
		t.Fatalf("fakeLookup was never consulted; got callCount=%d", got)
	}
}

func TestACPSubagentLookup_NilRegistry(t *testing.T) {
	if got := newACPSubagentLookup(nil); got != nil {
		t.Fatalf("newACPSubagentLookup(nil) = %v, want nil", got)
	}
}

func TestACPSubagentLookup_EmptyRegistry(t *testing.T) {
	registry := acp.NewACPRegistry()
	lookup := newACPSubagentLookup(registry)
	if lookup == nil {
		t.Fatal("newACPSubagentLookup returned nil for non-nil registry")
	}
	if lookup.HasSubagent("session:main", "1") {
		t.Error("HasSubagent returned true for empty registry")
	}
	if lookup.HasSubagent("", "") {
		t.Error("HasSubagent returned true for empty inputs")
	}
}

func TestACPSubagentLookup_MatchesActiveAgent(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{
		ID: "abc12345", ParentID: "session:main", SessionKey: "session:child-1",
		Role: "worker", Status: "running", SpawnedAt: 1,
	})
	lookup := newACPSubagentLookup(registry)

	if !lookup.HasSubagent("session:main", "1") {
		t.Error("index 1 did not match registered agent")
	}
	if !lookup.HasSubagent("session:main", "worker") {
		t.Error("label 'worker' did not match registered agent")
	}
	if !lookup.HasSubagent("session:main", "abc123") {
		t.Error("run-id prefix 'abc123' did not match registered agent")
	}
	if !lookup.HasSubagent("session:main", "session:child-1") {
		t.Error("exact session key did not match registered agent")
	}
	if lookup.HasSubagent("session:other", "worker") {
		t.Error("HasSubagent leaked across parent sessions")
	}
	if lookup.HasSubagent("session:main", "unknown-label-xyz") {
		t.Error("unknown label matched unexpectedly")
	}
}

func TestACPSubagentLookup_IgnoresTerminalAgents(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{
		ID: "done-agent", ParentID: "session:main", SessionKey: "session:child-done",
		Role: "completed", Status: "done", SpawnedAt: 1, EndedAt: 2,
	})
	lookup := newACPSubagentLookup(registry)

	if lookup.HasSubagent("session:main", "completed") {
		t.Error("terminal agent matched label; should be filtered")
	}
	if lookup.HasSubagent("session:main", "done-agent") {
		t.Error("terminal agent matched run-id; should be filtered")
	}
	if lookup.HasSubagent("session:main", "1") {
		t.Error("terminal agent matched index; should be filtered")
	}
}

func TestACPSubagentLookup_ConcurrentRace(t *testing.T) {
	registry := acp.NewACPRegistry()
	lookup := newACPSubagentLookup(registry)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 50 {
			registry.Register(acp.ACPAgent{
				ID:         "agent-" + string(rune('a'+i%26)),
				ParentID:   "session:main",
				SessionKey: "session:child",
				Role:       "worker",
				Status:     "running",
				SpawnedAt:  int64(i),
			})
		}
	}()
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = lookup.HasSubagent("session:main", "worker")
				_ = lookup.HasSubagent("session:main", "1")
				_ = lookup.HasSubagent("session:main", "unknown")
			}
		}()
	}
	wg.Wait()
}
