package acp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestACPRegistryLifecycleAndSnapshotInvalidation(t *testing.T) {
	r := NewACPRegistry()
	if got := r.List(""); len(got) != 0 {
		t.Fatalf("new registry list = %#v, want empty", got)
	}
	if got := r.ActiveCount(""); got != 0 {
		t.Fatalf("new registry active count = %d, want 0", got)
	}

	r.Register(ACPAgent{
		ID:         "research-1",
		ParentID:   "root",
		Role:       "researcher",
		Status:     "idle",
		SessionKey: "acp:client:main:research-1",
		SpawnedAt:  100,
		Depth:      1,
	})
	r.Register(ACPAgent{
		ID:         "review-1",
		ParentID:   "root",
		Role:       "reviewer",
		Status:     "running",
		SessionKey: "acp:client:main:review-1",
		SpawnedAt:  200,
		Depth:      1,
	})
	r.Register(ACPAgent{
		ID:         "nested-1",
		ParentID:   "research-1",
		Role:       "fact-checker",
		Status:     "done",
		SessionKey: "acp:client:main:nested-1",
		SpawnedAt:  300,
		Depth:      2,
	})

	all := r.List("")
	if len(all) != 3 {
		t.Fatalf("all agents = %d, want 3", len(all))
	}
	rootChildren := r.List("root")
	if len(rootChildren) != 2 {
		t.Fatalf("root children = %#v, want 2", rootChildren)
	}
	if got := r.ActiveCount("root"); got != 2 {
		t.Fatalf("root active count = %d, want 2", got)
	}
	if got := r.ActiveCount("research-1"); got != 0 {
		t.Fatalf("nested active count = %d, want terminal child excluded", got)
	}

	// A mutation must invalidate the cached all-agent snapshot. Without this
	// guard, List can continue serving a stale pre-kill status indefinitely.
	if !r.Kill("review-1") {
		t.Fatal("Kill(review-1) = false")
	}
	killed := r.Get("review-1")
	if killed == nil || killed.Status != "killed" || killed.EndedAt == 0 {
		t.Fatalf("killed agent = %#v", killed)
	}
	if got := r.ActiveCount("root"); got != 1 {
		t.Fatalf("active count after kill = %d, want 1", got)
	}
	if r.Kill("missing") {
		t.Fatal("Kill(missing) = true")
	}

	if !r.UpdateStatusBySessionKey("acp:client:main:research-1", "done", 999) {
		t.Fatal("UpdateStatusBySessionKey(existing) = false")
	}
	updated := r.Get("research-1")
	if updated == nil || updated.Status != "done" || updated.EndedAt != 999 {
		t.Fatalf("updated agent = %#v", updated)
	}
	if r.UpdateStatusBySessionKey("acp:missing", "failed", 1) {
		t.Fatal("UpdateStatusBySessionKey(missing) = true")
	}

	r.Remove("nested-1")
	if got := r.Get("nested-1"); got != nil {
		t.Fatalf("removed agent still present: %#v", got)
	}
	if got := len(r.List("")); got != 2 {
		t.Fatalf("list after remove = %d, want 2", got)
	}
	// Removing a missing ID is intentionally idempotent.
	r.Remove("nested-1")
}

func TestACPRegistryGetReturnsIndependentCopy(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "a", Role: "original", Status: "running", SessionKey: "acp:p:a"})

	first := r.Get("a")
	if first == nil {
		t.Fatal("Get(a) = nil")
	}
	first.Role = "mutated by caller"
	first.Status = "failed"
	first.EndedAt = 123

	second := r.Get("a")
	if second == nil {
		t.Fatal("second Get(a) = nil")
	}
	if second.Role != "original" || second.Status != "running" || second.EndedAt != 0 {
		t.Fatalf("registry leaked caller mutation: %#v", second)
	}
	if got := r.Get("missing"); got != nil {
		t.Fatalf("Get(missing) = %#v", got)
	}
}

func TestACPRegistryRegisterReplacesSameIDAndInvalidatesCache(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "same", ParentID: "one", Role: "old", Status: "idle"})
	if got := r.List("one"); len(got) != 1 {
		t.Fatalf("initial filtered list = %#v", got)
	}

	r.Register(ACPAgent{ID: "same", ParentID: "two", Role: "new", Status: "running"})
	if got := r.List("one"); len(got) != 0 {
		t.Fatalf("old parent retained replacement: %#v", got)
	}
	got := r.List("two")
	if len(got) != 1 || got[0].Role != "new" {
		t.Fatalf("replacement = %#v", got)
	}
	if got := len(r.List("")); got != 1 {
		t.Fatalf("duplicate ID grew registry to %d", got)
	}
}

func TestRegisterIfUnderLimitIsAtomicUnderConcurrency(t *testing.T) {
	r := NewACPRegistry()
	const (
		attempts = 80
		limit    = 7
	)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan bool, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- r.RegisterIfUnderLimit(ACPAgent{
				ID:         fmt.Sprintf("agent-%02d", i),
				ParentID:   "parent",
				Status:     "running",
				SessionKey: fmt.Sprintf("acp:parent:agent-%02d", i),
			}, "parent", limit)
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != limit {
		t.Fatalf("accepted = %d, want exact atomic limit %d", accepted, limit)
	}
	if got := r.ActiveCount("parent"); got != limit {
		t.Fatalf("active count = %d, want %d", got, limit)
	}
	if got := len(r.List("parent")); got != limit {
		t.Fatalf("registered children = %d, want %d", got, limit)
	}
}

func TestRegisterIfUnderLimitIgnoresTerminalChildren(t *testing.T) {
	r := NewACPRegistry()
	for i, status := range []string{"done", "failed", "killed", "idle", "running"} {
		r.Register(ACPAgent{
			ID:       fmt.Sprintf("child-%d", i),
			ParentID: "p",
			Status:   status,
		})
	}
	if got := r.ActiveCount("p"); got != 2 {
		t.Fatalf("active count = %d, want idle+running only", got)
	}
	if !r.RegisterIfUnderLimit(ACPAgent{ID: "third", ParentID: "p", Status: "idle"}, "p", 3) {
		t.Fatal("third active child should fit limit")
	}
	if r.RegisterIfUnderLimit(ACPAgent{ID: "fourth", ParentID: "p", Status: "idle"}, "p", 3) {
		t.Fatal("fourth active child should be rejected")
	}
	// A zero limit is a closed gate, including for an empty parent.
	if r.RegisterIfUnderLimit(ACPAgent{ID: "blocked", ParentID: "empty", Status: "idle"}, "empty", 0) {
		t.Fatal("zero limit unexpectedly accepted an agent")
	}
}

func TestACPRegistryUpdatesStatusFromSessionManager(t *testing.T) {
	mgr := session.NewManager()
	r := NewACPRegistry()
	r.SetSessionManager(mgr)
	r.Register(ACPAgent{
		ID:         "worker",
		Role:       "worker",
		Status:     "idle",
		SessionKey: "acp:parent:worker",
	})

	start := int64(1_700_000_000_000)
	mgr.ApplyLifecycleEvent("acp:parent:worker", session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    start,
	})
	got := r.Get("worker")
	if got == nil || got.Status != "running" || got.EndedAt != 0 {
		t.Fatalf("running derived agent = %#v", got)
	}

	end := start + 275
	mgr.ApplyLifecycleEvent("acp:parent:worker", session.LifecycleEvent{
		Phase: session.PhaseEnd,
		Ts:    end,
	})
	got = r.Get("worker")
	if got == nil || got.Status != "done" || got.EndedAt != end {
		t.Fatalf("done derived agent = %#v, want endedAt %d", got, end)
	}
	if got := r.ActiveCount(""); got != 0 {
		t.Fatalf("active count after derived completion = %d", got)
	}

	// List must not reuse a stale cached status when a manager is attached.
	mgr.ApplyLifecycleEvent("acp:parent:worker", session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    end + 100,
	})
	listed := r.List("")
	if len(listed) != 1 || listed[0].Status != "running" || listed[0].EndedAt != 0 {
		t.Fatalf("freshly enriched list = %#v", listed)
	}
}

func TestACPRegistrySessionFallbacks(t *testing.T) {
	mgr := session.NewManager()
	r := NewACPRegistry()
	r.SetSessionManager(mgr)
	r.Register(ACPAgent{ID: "no-key", Status: "idle"})
	r.Register(ACPAgent{ID: "missing-session", Status: "idle", SessionKey: "acp:p:missing"})

	if got := r.Get("no-key"); got == nil || got.Status != "idle" {
		t.Fatalf("empty-session-key fallback = %#v", got)
	}
	if got := r.Get("missing-session"); got == nil || got.Status != "idle" {
		t.Fatalf("missing-session fallback = %#v", got)
	}

	// A session whose lifecycle status is empty must not erase the registry's
	// useful idle value.
	mgr.Create("acp:p:missing", session.KindSubagent)
	if got := r.Get("missing-session"); got == nil || got.Status != "idle" {
		t.Fatalf("empty manager status erased fallback: %#v", got)
	}
}

func TestSessionStatusMappingsCoverTerminalAndUnknownStates(t *testing.T) {
	cases := []struct {
		status session.RunStatus
		acp    string
		stop   string
	}{
		{session.StatusRunning, "running", ""},
		{session.StatusDone, "done", "stop"},
		{session.StatusFailed, "failed", "error"},
		{session.StatusKilled, "killed", "cancel"},
		{session.StatusTimeout, "failed", "error"},
		{session.RunStatus("stale"), "", ""},
		{session.RunStatus(""), "", ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := mapSessionStatusToACP(tc.status); got != tc.acp {
				t.Fatalf("mapSessionStatusToACP(%q) = %q, want %q", tc.status, got, tc.acp)
			}
			if got := TranslateStopReason(tc.status); got != tc.stop {
				t.Fatalf("TranslateStopReason(%q) = %q, want %q", tc.status, got, tc.stop)
			}
		})
	}

	reverse := []struct {
		stop   string
		status session.RunStatus
	}{
		{"stop", session.StatusDone},
		{"cancel", session.StatusKilled},
		{"error", session.StatusFailed},
		{"", session.StatusDone},
		{"future-value", session.StatusDone},
	}
	for _, tc := range reverse {
		if got := TranslateACPStopReasonToStatus(tc.stop); got != tc.status {
			t.Errorf("TranslateACPStopReasonToStatus(%q) = %q, want %q", tc.stop, got, tc.status)
		}
	}
}

func TestACPSessionDetectionRejectsInvalidPrefixVariants(t *testing.T) {
	valid := []string{"acp:", "acp:id", "acp:client:main:worker"}
	for _, key := range valid {
		if !IsACPSession(key) {
			t.Errorf("IsACPSession(%q) = false", key)
		}
	}
	invalid := []string{"", "ACP:id", " acp:id", "client:acp:id", "acp"}
	for _, key := range invalid {
		if IsACPSession(key) {
			t.Errorf("IsACPSession(%q) = true", key)
		}
	}
	translator := NewACPTranslator(NewACPRegistry(), NewSessionBindingService())
	if translator == nil || translator.registry == nil || translator.bindings == nil {
		t.Fatalf("translator dependencies not retained: %#v", translator)
	}
}

func TestACPProjectorFormatsKnownAndUnknownAgents(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "r1", Role: "researcher", Status: "done"})
	r.Register(ACPAgent{ID: "id-only", Status: "done"})
	p := NewACPProjector(r)

	got := p.ProjectResult("r1", &ACPTurnResult{
		OutputText: "found three sources",
		TokensUsed: ACPTokenUsage{TotalTokens: 128},
	})
	for _, part := range []string{"**[researcher]**", "found three sources", "_128 tokens_"} {
		if !strings.Contains(got, part) {
			t.Errorf("projected output %q missing %q", got, part)
		}
	}

	got = p.ProjectResult("id-only", &ACPTurnResult{OutputText: "done"})
	if got != "**[id-only]**\ndone" {
		t.Fatalf("ID fallback projection = %q", got)
	}

	got = p.ProjectResult("missing", &ACPTurnResult{OutputText: "raw fallback"})
	if got != "raw fallback" {
		t.Fatalf("unknown-agent projection = %q", got)
	}

	got = p.ProjectResult("r1", &ACPTurnResult{})
	if got != "**[researcher]**" {
		t.Fatalf("empty result projection = %q", got)
	}
	if got := formatACPTokenSummary(ACPTokenUsage{TotalTokens: 9}); got != "9 tokens" {
		t.Fatalf("token summary = %q", got)
	}
}

func TestFormatSubagentListRepresentsEveryLifecycleState(t *testing.T) {
	if got := FormatSubagentList(nil); got != "No active subagents." {
		t.Fatalf("empty list = %q", got)
	}
	agents := []ACPAgent{
		{ID: "a", Role: "runner", Status: "running"},
		{ID: "b", Role: "waiting", Status: "idle"},
		{ID: "c", Role: "finished", Status: "done"},
		{ID: "d", Role: "broken", Status: "failed"},
		{ID: "e", Role: "stopped", Status: "killed"},
		{ID: "fallback", Status: "future"},
	}
	got := FormatSubagentList(agents)
	for _, want := range []string{
		"**runner** — 🟢 running",
		"**waiting** — 🟡 idle",
		"**finished** — ✅ done",
		"**broken** — ❌ failed",
		"**stopped** — 💀 killed",
		"**fallback** — future",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted list missing %q:\n%s", want, got)
		}
	}
	if gotLines := strings.Count(got, "\n") + 1; gotLines != len(agents) {
		t.Fatalf("formatted lines = %d, want %d", gotLines, len(agents))
	}
}

func TestStartACPLifecycleSyncUpdatesOnlyStatusEvents(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "worker", Status: "idle", SessionKey: "acp:p:worker"})
	bus := session.NewEventBus()
	stop := StartACPLifecycleSync(r, bus)
	defer stop()

	bus.Emit(session.Event{Kind: session.EventCreated, Key: "acp:p:worker"})
	assertEventually(t, func() bool {
		return r.Get("worker").Status == "idle"
	})

	bus.Emit(session.Event{
		Kind:      session.EventStatusChanged,
		Key:       "acp:p:worker",
		NewStatus: session.StatusRunning,
	})
	assertEventually(t, func() bool {
		return r.Get("worker").Status == "running"
	})

	bus.Emit(session.Event{
		Kind:      session.EventStatusChanged,
		Key:       "acp:p:worker",
		NewStatus: session.StatusTimeout,
	})
	assertEventually(t, func() bool {
		got := r.Get("worker")
		return got.Status == "failed" && got.EndedAt > 0
	})

	ended := r.Get("worker").EndedAt
	bus.Emit(session.Event{
		Kind:      session.EventStatusChanged,
		Key:       "acp:p:worker",
		NewStatus: session.RunStatus("unknown"),
	})
	time.Sleep(10 * time.Millisecond)
	got := r.Get("worker")
	if got.Status != "failed" || got.EndedAt != ended {
		t.Fatalf("unknown status mutated agent: %#v", got)
	}

	stop()
	bus.Emit(session.Event{
		Kind:      session.EventStatusChanged,
		Key:       "acp:p:worker",
		NewStatus: session.StatusRunning,
	})
	time.Sleep(10 * time.Millisecond)
	if got := r.Get("worker"); got.Status != "failed" {
		t.Fatalf("unsubscribed handler still updated agent: %#v", got)
	}
}

func assertEventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

func TestStartSubagentResultInjectionDependencyFallbacks(t *testing.T) {
	// Each missing required dependency must return a safe no-op unsubscribe.
	for _, deps := range []ResultInjectionDeps{
		{},
		{Registry: NewACPRegistry()},
		{Registry: NewACPRegistry(), Sessions: session.NewManager()},
		{Sessions: session.NewManager(), Transcript: TranscriptAppendFunc(func(string, string) error { return nil })},
	} {
		stop := StartSubagentResultInjection(deps)
		if stop == nil {
			t.Fatal("missing-dependency fallback returned nil stop function")
		}
		stop()
	}
}

func TestStartSubagentResultInjectionCompletedOutput(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "worker", Role: "researcher", Status: "running", SessionKey: "acp:client:main:worker"})
	mgr := session.NewManager()

	type appended struct {
		key  string
		note string
	}
	gotCh := make(chan appended, 2)
	stop := StartSubagentResultInjection(ResultInjectionDeps{
		Registry:  r,
		Projector: NewACPProjector(r),
		Sessions:  mgr,
		Transcript: TranscriptAppendFunc(func(key, note string) error {
			gotCh <- appended{key: key, note: note}
			return nil
		}),
	})
	defer stop()

	start := time.Now().UnixMilli()
	mgr.ApplyLifecycleEvent("acp:client:main:worker", session.LifecycleEvent{Phase: session.PhaseStart, Ts: start})
	s := mgr.Get("acp:client:main:worker")
	if s == nil {
		t.Fatal("subagent session missing")
	}
	s.LastOutput = "evidence gathered"
	if err := mgr.Set(s); err != nil {
		t.Fatalf("Set session output: %v", err)
	}
	mgr.ApplyLifecycleEvent("acp:client:main:worker", session.LifecycleEvent{Phase: session.PhaseEnd, Ts: start + 5})

	select {
	case got := <-gotCh:
		if got.key != "client:main" {
			t.Fatalf("parent key = %q, want client:main", got.key)
		}
		if !strings.Contains(got.note, "[Subagent completed]") ||
			!strings.Contains(got.note, "**[researcher]**") ||
			!strings.Contains(got.note, "evidence gathered") {
			t.Fatalf("injected note = %q", got.note)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completed output was not injected")
	}
}

func TestStartSubagentResultInjectionSkipsIrrelevantAndEmptyEvents(t *testing.T) {
	r := NewACPRegistry()
	mgr := session.NewManager()
	gotCh := make(chan string, 1)
	stop := StartSubagentResultInjection(ResultInjectionDeps{
		Registry: r,
		Sessions: mgr,
		Transcript: TranscriptAppendFunc(func(_, note string) error {
			gotCh <- note
			return nil
		}),
	})
	defer stop()

	// A normal session completing is not an ACP child.
	now := time.Now().UnixMilli()
	mgr.ApplyLifecycleEvent("client:main", session.LifecycleEvent{Phase: session.PhaseStart, Ts: now})
	mgr.ApplyLifecycleEvent("client:main", session.LifecycleEvent{Phase: session.PhaseEnd, Ts: now + 1})
	// An ACP session with no LastOutput must also be ignored.
	mgr.ApplyLifecycleEvent("acp:client:main:empty", session.LifecycleEvent{Phase: session.PhaseStart, Ts: now})
	mgr.ApplyLifecycleEvent("acp:client:main:empty", session.LifecycleEvent{Phase: session.PhaseEnd, Ts: now + 1})

	select {
	case note := <-gotCh:
		t.Fatalf("irrelevant event injected note %q", note)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStartSubagentResultInjectionTruncatesOversizedOutput(t *testing.T) {
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "long", Role: "writer", Status: "running"})
	mgr := session.NewManager()
	gotCh := make(chan string, 1)
	stop := StartSubagentResultInjection(ResultInjectionDeps{
		Registry: r,
		Sessions: mgr,
		Transcript: TranscriptAppendFunc(func(_, note string) error {
			gotCh <- note
			return nil
		}),
	})
	defer stop()

	now := time.Now().UnixMilli()
	key := "acp:parent:long"
	mgr.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseStart, Ts: now})
	s := mgr.Get(key)
	s.LastOutput = strings.Repeat("x", 5000)
	if err := mgr.Set(s); err != nil {
		t.Fatalf("Set oversized output: %v", err)
	}
	mgr.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseEnd, Ts: now + 1})

	select {
	case note := <-gotCh:
		if !strings.HasSuffix(note, "\n... (truncated)") {
			t.Fatalf("truncated note suffix missing: len=%d", len(note))
		}
		if strings.Count(note, "x") != 4000 {
			t.Fatalf("truncated payload has %d x bytes, want 4000", strings.Count(note, "x"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("oversized output was not injected")
	}
}

func TestTranscriptAppendFuncPropagatesErrors(t *testing.T) {
	want := fmt.Errorf("partial transcript write")
	f := TranscriptAppendFunc(func(key, text string) error {
		if key != "client:main" || text != "note" {
			t.Fatalf("append args = %q, %q", key, text)
		}
		return want
	})
	if got := f.AppendSystemNote("client:main", "note"); !errors.Is(got, want) {
		t.Fatalf("AppendSystemNote error = %v, want identity %v", got, want)
	}
}

func TestParseACPSessionKeyMalformedBoundaries(t *testing.T) {
	cases := []struct {
		key    string
		parent string
		agent  string
	}{
		{"acp:client:main:worker", "client:main", "worker"},
		{"acp:parent:child:agent", "parent:child", "agent"},
		{"acp::agent", "", ""},
		{"acp:parent:", "parent", ""},
		{"acp:single", "", ""},
		{"acp:", "", ""},
		{"ACP:parent:agent", "", ""},
		{"prefix-acp:parent:agent", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			parent, agent := parseACPSessionKey(tc.key)
			if parent != tc.parent || agent != tc.agent {
				t.Fatalf("parseACPSessionKey(%q) = (%q,%q), want (%q,%q)", tc.key, parent, agent, tc.parent, tc.agent)
			}
		})
	}
}

func TestLifecycleSyncUnsubscribeIsConcurrentSafe(t *testing.T) {
	r := NewACPRegistry()
	bus := session.NewEventBus()
	stop := StartACPLifecycleSync(r, bus)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop()
		}()
	}
	wg.Wait()
	// Keep the context import live as part of the cancellation boundary: an
	// already-cancelled context must not affect the unsubscribe operation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-ctx.Done():
		stop()
	default:
		t.Fatal("cancelled context not cancelled")
	}
}
