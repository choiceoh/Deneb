package approval

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateRequestBoundaryPreservesEveryField(t *testing.T) {
	t.Parallel()

	turn := &TurnSourceInfo{
		Channel:   "telegram",
		To:        "operator",
		AccountID: "account-7",
		ThreadID:  "thread-9",
	}
	plan := struct {
		Shell string
		Risk  int
	}{Shell: "bash", Risk: 4}
	planJSON := mustJSON(t, plan)
	s := NewStore()
	before := time.Now().UnixMilli()
	got := s.CreateRequest(CreateRequestParams{
		ID:            "fixed-id",
		Command:       "printf boundary",
		CommandArgv:   []string{"printf", "%s", "boundary"},
		Env:           map[string]string{"LANG": "ko_KR.UTF-8", "MODE": "test"},
		Cwd:           "/srv/work tree",
		SystemRunPlan: planJSON,
		Host:          "gateway",
		Security:      "allowlist",
		Ask:           "on-miss",
		AgentID:       "agent-main",
		ResolvedPath:  "/usr/bin/printf",
		SessionKey:    "client:main:boundary",
		TimeoutMs:     45_000,
		TwoPhase:      true,
		TurnSource:    turn,
	})
	after := time.Now().UnixMilli()

	if got.ID != "fixed-id" {
		t.Fatalf("ID = %q, want fixed-id", got.ID)
	}
	if got.Command != "printf boundary" {
		t.Fatalf("Command = %q", got.Command)
	}
	if !reflect.DeepEqual(got.CommandArgv, []string{"printf", "%s", "boundary"}) {
		t.Fatalf("CommandArgv = %#v", got.CommandArgv)
	}
	if !reflect.DeepEqual(got.Env, map[string]string{"LANG": "ko_KR.UTF-8", "MODE": "test"}) {
		t.Fatalf("Env = %#v", got.Env)
	}
	if got.Cwd != "/srv/work tree" {
		t.Fatalf("Cwd = %q", got.Cwd)
	}
	if !reflect.DeepEqual(got.SystemRunPlan, planJSON) {
		t.Fatalf("SystemRunPlan = %#v", got.SystemRunPlan)
	}
	if got.Host != "gateway" {
		t.Fatalf("Host = %q", got.Host)
	}
	if got.Security != "allowlist" {
		t.Fatalf("Security = %q", got.Security)
	}
	if got.Ask != "on-miss" {
		t.Fatalf("Ask = %q", got.Ask)
	}
	if got.AgentID != "agent-main" {
		t.Fatalf("AgentID = %q", got.AgentID)
	}
	if got.ResolvedPath != "/usr/bin/printf" {
		t.Fatalf("ResolvedPath = %q", got.ResolvedPath)
	}
	if got.SessionKey != "client:main:boundary" {
		t.Fatalf("SessionKey = %q", got.SessionKey)
	}
	if !got.TwoPhase {
		t.Fatal("TwoPhase = false, want true")
	}
	if got.Decision != nil || got.ResolvedAtMs != nil {
		t.Fatalf("new request is already resolved: decision=%v resolved=%v", got.Decision, got.ResolvedAtMs)
	}
	if got.CreatedAtMs < before || got.CreatedAtMs > after {
		t.Fatalf("CreatedAtMs %d outside [%d,%d]", got.CreatedAtMs, before, after)
	}
	if delta := got.ExpiresAtMs - got.CreatedAtMs; delta != 45_000 {
		t.Fatalf("expiry delta = %dms, want 45000ms", delta)
	}
	if got.TurnSourceInfo == nil || !reflect.DeepEqual(*got.TurnSourceInfo, *turn) {
		t.Fatalf("TurnSourceInfo = %#v, want %#v", got.TurnSourceInfo, turn)
	}
}

func TestCreateRequestCopiesMutableInputAndOutput(t *testing.T) {
	t.Parallel()

	argv := []string{"echo", "original"}
	env := map[string]string{"KEY": "original"}
	turn := &TurnSourceInfo{Channel: "native", ThreadID: "original"}
	s := NewStore()
	created := s.CreateRequest(CreateRequestParams{
		ID:          "isolation",
		Command:     "echo original",
		CommandArgv: argv,
		Env:         env,
		TurnSource:  turn,
	})

	// Mutate every caller-owned container after insertion.
	argv[0] = "rm"
	argv = append(argv, "extra") //nolint:ineffassign,staticcheck // mutation probe: proves the store copied, not aliased
	env["KEY"] = "changed"
	env["NEW"] = "value"
	turn.Channel = "changed"
	turn.ThreadID = "changed"

	// Mutate every mutable value returned from CreateRequest too.
	created.Command = "caller mutation"
	created.CommandArgv[1] = "caller mutation"
	created.Env["KEY"] = "caller mutation"
	created.TurnSourceInfo.Channel = "caller mutation"

	got := s.Get("isolation")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Command != "echo original" {
		t.Fatalf("stored Command = %q", got.Command)
	}
	if !reflect.DeepEqual(got.CommandArgv, []string{"echo", "original"}) {
		t.Fatalf("stored CommandArgv = %#v", got.CommandArgv)
	}
	if !reflect.DeepEqual(got.Env, map[string]string{"KEY": "original"}) {
		t.Fatalf("stored Env = %#v", got.Env)
	}
	if got.TurnSourceInfo == nil || got.TurnSourceInfo.Channel != "native" || got.TurnSourceInfo.ThreadID != "original" {
		t.Fatalf("stored TurnSourceInfo = %#v", got.TurnSourceInfo)
	}
}

func TestGetReturnsDeepCopiesOfMutableFields(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.CreateRequest(CreateRequestParams{
		ID:          "deep-copy",
		CommandArgv: []string{"one", "two"},
		Env:         map[string]string{"A": "1"},
		TurnSource:  &TurnSourceInfo{Channel: "native", To: "phone"},
	})
	one := s.Get("deep-copy")
	two := s.Get("deep-copy")
	one.CommandArgv[0] = "changed"
	one.Env["A"] = "changed"
	one.Env["B"] = "added"
	one.TurnSourceInfo.Channel = "changed"

	if two.CommandArgv[0] != "one" {
		t.Fatalf("second snapshot argv changed: %#v", two.CommandArgv)
	}
	if !reflect.DeepEqual(two.Env, map[string]string{"A": "1"}) {
		t.Fatalf("second snapshot env changed: %#v", two.Env)
	}
	if two.TurnSourceInfo.Channel != "native" {
		t.Fatalf("second snapshot turn source changed: %#v", two.TurnSourceInfo)
	}
	three := s.Get("deep-copy")
	if three.CommandArgv[0] != "one" || three.Env["A"] != "1" || three.TurnSourceInfo.Channel != "native" {
		t.Fatalf("store changed through snapshot: %#v", three)
	}
}

func TestCreateRequestExpiryTimeoutBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		defaultTT time.Duration
		timeoutMS int64
		wantDelta int64
	}{
		{
			name:      "positive override one millisecond",
			defaultTT: 5 * time.Minute,
			timeoutMS: 1,
			wantDelta: 1,
		},
		{
			name:      "positive override exact second",
			defaultTT: 5 * time.Minute,
			timeoutMS: 1_000,
			wantDelta: 1_000,
		},
		{
			name:      "positive override day",
			defaultTT: 5 * time.Minute,
			timeoutMS: int64((24 * time.Hour) / time.Millisecond),
			wantDelta: int64((24 * time.Hour) / time.Millisecond),
		},
		{
			name:      "zero uses default",
			defaultTT: 17 * time.Second,
			timeoutMS: 0,
			wantDelta: 17_000,
		},
		{
			name:      "negative uses default",
			defaultTT: 23 * time.Second,
			timeoutMS: -1,
			wantDelta: 23_000,
		},
		{
			name:      "large negative uses default",
			defaultTT: 31 * time.Second,
			timeoutMS: -9_999_999,
			wantDelta: 31_000,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewStore()
			s.defaultTTL = tc.defaultTT
			req := s.CreateRequest(CreateRequestParams{
				ID:        strings.ReplaceAll(tc.name, " ", "-"),
				TimeoutMs: tc.timeoutMS,
			})
			if got := req.ExpiresAtMs - req.CreatedAtMs; got != tc.wantDelta {
				t.Fatalf("expiry delta = %d, want %d", got, tc.wantDelta)
			}
		})
	}
}

func TestCreateRequestGeneratesUniqueHexIDs(t *testing.T) {
	t.Parallel()

	const count = 512
	s := NewStore()
	seen := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		req := s.CreateRequest(CreateRequestParams{Command: fmt.Sprintf("command-%d", i)})
		if len(req.ID) != 24 {
			t.Fatalf("generated ID %q has length %d, want 24", req.ID, len(req.ID))
		}
		for _, r := range req.ID {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Fatalf("generated ID %q contains non-lowercase-hex rune %q", req.ID, r)
			}
		}
		if seen[req.ID] {
			t.Fatalf("duplicate generated ID %q", req.ID)
		}
		seen[req.ID] = true
	}
}

func TestConcurrentGeneratedIDsAndReads(t *testing.T) {
	const (
		writers   = 24
		perWriter = 80
	)
	s := NewStore()
	ids := make(chan string, writers*perWriter)
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < perWriter; n++ {
				req := s.CreateRequest(CreateRequestParams{
					Command:     fmt.Sprintf("writer-%d-command-%d", writer, n),
					CommandArgv: []string{"writer", fmt.Sprint(writer), fmt.Sprint(n)},
					Env:         map[string]string{"WRITER": fmt.Sprint(writer)},
				})
				if got := s.Get(req.ID); got == nil || got.Command != req.Command {
					t.Errorf("Get(%q) = %#v after create", req.ID, got)
					return
				}
				ids <- req.ID
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool, writers*perWriter)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate concurrent ID %q", id)
		}
		seen[id] = true
	}
	if got, want := len(seen), writers*perWriter; got != want {
		t.Fatalf("created IDs = %d, want %d", got, want)
	}
}

func TestResolveUpdatesDecisionAndTimestampMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision Decision
	}{
		{name: "allow once", decision: DecisionAllowOnce},
		{name: "allow always", decision: DecisionAllowAlways},
		{name: "deny", decision: DecisionDeny},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewStore()
			req := s.CreateRequest(CreateRequestParams{ID: tc.name})
			before := time.Now().UnixMilli()
			if err := s.Resolve(req.ID, tc.decision); err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			after := time.Now().UnixMilli()
			got := s.Get(req.ID)
			if got.Decision == nil || *got.Decision != tc.decision {
				t.Fatalf("Decision = %v, want %q", got.Decision, tc.decision)
			}
			if got.ResolvedAtMs == nil || *got.ResolvedAtMs < before || *got.ResolvedAtMs > after {
				t.Fatalf("ResolvedAtMs = %v outside [%d,%d]", got.ResolvedAtMs, before, after)
			}
			if got.CreatedAtMs > *got.ResolvedAtMs {
				t.Fatalf("CreatedAtMs %d after ResolvedAtMs %d", got.CreatedAtMs, *got.ResolvedAtMs)
			}
		})
	}
}

func TestResolveErrorsDoNotMutateState(t *testing.T) {
	t.Parallel()

	s := NewStore()
	if err := s.Resolve("missing", DecisionDeny); err == nil || !strings.Contains(err.Error(), `"missing"`) {
		t.Fatalf("Resolve missing error = %v", err)
	}
	req := s.CreateRequest(CreateRequestParams{ID: "once"})
	if err := s.Resolve(req.ID, DecisionAllowOnce); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	first := s.Get(req.ID)
	if err := s.Resolve(req.ID, DecisionDeny); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("second Resolve error = %v", err)
	}
	second := s.Get(req.ID)
	if first.Decision == nil || second.Decision == nil || *first.Decision != *second.Decision {
		t.Fatalf("duplicate resolve changed decision: first=%v second=%v", first.Decision, second.Decision)
	}
	if first.ResolvedAtMs == nil || second.ResolvedAtMs == nil || *first.ResolvedAtMs != *second.ResolvedAtMs {
		t.Fatalf("duplicate resolve changed timestamp: first=%v second=%v", first.ResolvedAtMs, second.ResolvedAtMs)
	}
}

func TestConcurrentResolveHasExactlyOneWinner(t *testing.T) {
	const contenders = 128
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{ID: "contested"})
	decisions := []Decision{DecisionAllowOnce, DecisionAllowAlways, DecisionDeny}
	start := make(chan struct{})
	var successes atomic.Int64
	var already atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := s.Resolve(req.ID, decisions[i%len(decisions)])
			switch {
			case err == nil:
				successes.Add(1)
			case strings.Contains(err.Error(), "already resolved"):
				already.Add(1)
			default:
				t.Errorf("Resolve returned unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful resolutions = %d, want 1", got)
	}
	if got := already.Load(); got != contenders-1 {
		t.Fatalf("already-resolved errors = %d, want %d", got, contenders-1)
	}
	got := s.Get(req.ID)
	if got.Decision == nil || got.ResolvedAtMs == nil {
		t.Fatalf("resolved request incomplete: %#v", got)
	}
}

func TestWaitForDecisionFanoutClosesAllWaiterChannels(t *testing.T) {
	const waiterCount = 128
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{ID: "fanout"})
	waiters := make([]<-chan struct{}, waiterCount)
	for i := range waiters {
		waiters[i] = s.WaitForDecision(req.ID)
	}
	for i, ch := range waiters {
		assertNotClosed(t, ch, fmt.Sprintf("waiter %d before resolution", i))
	}
	if err := s.Resolve(req.ID, DecisionAllowAlways); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for i, ch := range waiters {
		assertClosed(t, ch, fmt.Sprintf("waiter %d after resolution", i))
	}
	if len(s.waiters) != 0 {
		t.Fatalf("waiters retained after resolution: %#v", s.waiters)
	}
}

func TestWaitForDecisionMissingAndResolvedAreImmediate(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		assertClosed(t, s.WaitForDecision("not-there"), "missing request")
		if len(s.waiters) != 0 {
			t.Fatalf("missing request registered waiter: %#v", s.waiters)
		}
	})
	t.Run("already resolved", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		s.CreateRequest(CreateRequestParams{ID: "resolved"})
		if err := s.Resolve("resolved", DecisionDeny); err != nil {
			t.Fatal(err)
		}
		assertClosed(t, s.WaitForDecision("resolved"), "resolved request")
		if len(s.waiters) != 0 {
			t.Fatalf("resolved request registered waiter: %#v", s.waiters)
		}
	})
}

func TestWaitersRegisteredConcurrentlyWithResolutionAllFinish(t *testing.T) {
	const attempts = 500
	for attempt := 0; attempt < attempts; attempt++ {
		s := NewStore()
		s.CreateRequest(CreateRequestParams{ID: "racy"})
		start := make(chan struct{})
		gotWaiter := make(chan (<-chan struct{}), 1)
		resolved := make(chan error, 1)
		go func() {
			<-start
			gotWaiter <- s.WaitForDecision("racy")
		}()
		go func() {
			<-start
			resolved <- s.Resolve("racy", DecisionAllowOnce)
		}()
		close(start)
		ch := <-gotWaiter
		if err := <-resolved; err != nil {
			t.Fatalf("attempt %d Resolve: %v", attempt, err)
		}
		assertClosed(t, ch, fmt.Sprintf("attempt %d waiter", attempt))
	}
}

func TestCleanupBoundaryMatrix(t *testing.T) {
	t.Parallel()

	s := NewStore()
	now := time.Now().UnixMilli()
	entries := []struct {
		id       string
		expires  int64
		resolved bool
		removed  bool
	}{
		{id: "expired-long-ago", expires: now - 10_000, removed: true},
		{id: "expired-one-ms", expires: now - 1, removed: true},
		{id: "future-one-ms", expires: now + 2_000, removed: false},
		{id: "future-long", expires: now + 100_000, removed: false},
		{id: "resolved-expired", expires: now - 10_000, resolved: true, removed: false},
		{id: "resolved-future", expires: now + 100_000, resolved: true, removed: false},
	}
	waiters := make(map[string]<-chan struct{})
	for _, entry := range entries {
		s.CreateRequest(CreateRequestParams{ID: entry.id})
		s.mu.Lock()
		s.requests[entry.id].ExpiresAtMs = entry.expires
		s.mu.Unlock()
		if entry.resolved {
			if err := s.Resolve(entry.id, DecisionDeny); err != nil {
				t.Fatalf("Resolve(%s): %v", entry.id, err)
			}
		} else {
			waiters[entry.id] = s.WaitForDecision(entry.id)
		}
	}
	if got := s.Cleanup(); got != 2 {
		t.Fatalf("Cleanup removed %d requests, want 2", got)
	}
	for _, entry := range entries {
		got := s.Get(entry.id)
		if entry.removed && got != nil {
			t.Errorf("%s survived cleanup: %#v", entry.id, got)
		}
		if !entry.removed && got == nil {
			t.Errorf("%s was removed by cleanup", entry.id)
		}
		if ch := waiters[entry.id]; ch != nil {
			if entry.removed {
				assertClosed(t, ch, entry.id+" cleanup waiter")
			} else {
				assertNotClosed(t, ch, entry.id+" surviving waiter")
			}
		}
	}
}

func TestCleanupIsIdempotentAndWakesAllExpiredWaiters(t *testing.T) {
	const requestCount = 64
	const waitersPerRequest = 8
	s := NewStore()
	all := make([]<-chan struct{}, 0, requestCount*waitersPerRequest)
	for i := 0; i < requestCount; i++ {
		id := fmt.Sprintf("expired-%02d", i)
		s.CreateRequest(CreateRequestParams{ID: id})
		s.mu.Lock()
		s.requests[id].ExpiresAtMs = time.Now().UnixMilli() - 1
		s.mu.Unlock()
		for j := 0; j < waitersPerRequest; j++ {
			all = append(all, s.WaitForDecision(id))
		}
	}
	if got := s.Cleanup(); got != requestCount {
		t.Fatalf("first Cleanup = %d, want %d", got, requestCount)
	}
	if got := s.Cleanup(); got != 0 {
		t.Fatalf("second Cleanup = %d, want 0", got)
	}
	for i, ch := range all {
		assertClosed(t, ch, fmt.Sprintf("cleanup waiter %d", i))
	}
	if len(s.requests) != 0 || len(s.waiters) != 0 {
		t.Fatalf("cleanup retained state: requests=%d waiters=%d", len(s.requests), len(s.waiters))
	}
}

func TestGlobalSnapshotPreservesDataDespiteInputAndOutputMutation(t *testing.T) {
	t.Parallel()

	file := ApprovalsFile{
		Version: 7,
		Rules: []ApprovalRule{
			{Pattern: "git status", Decision: "allow", AddedAt: "2026-01-02"},
			{Pattern: "rm *", Decision: "deny", AddedAt: "2026-01-03"},
		},
		GlobalDeny: []string{"shutdown", "reboot"},
		Metadata:   map[string]string{"owner": "operator", "revision": "7"},
	}
	s := NewStore()
	s.SetGlobalSnapshot(file, "sha256:seven")

	// Mutating input after SetGlobalSnapshot must not mutate the store.
	file.Version = 99
	file.Rules[0].Pattern = "changed-input"
	file.GlobalDeny[0] = "changed-input"
	file.Metadata["owner"] = "changed-input"
	file.Metadata["new"] = "changed-input"

	one := s.GlobalSnapshot()
	if one.File.Version != 7 || one.Hash != "sha256:seven" {
		t.Fatalf("snapshot scalar fields changed: %#v", one)
	}
	if one.File.Rules[0].Pattern != "git status" {
		t.Fatalf("snapshot rules share input: %#v", one.File.Rules)
	}
	if one.File.GlobalDeny[0] != "shutdown" {
		t.Fatalf("snapshot deny list shares input: %#v", one.File.GlobalDeny)
	}
	if !reflect.DeepEqual(one.File.Metadata, map[string]string{"owner": "operator", "revision": "7"}) {
		t.Fatalf("snapshot metadata shares input: %#v", one.File.Metadata)
	}

	// Mutating one returned snapshot must not mutate later snapshots.
	one.File.Version = 100
	one.File.Rules[0].Pattern = "changed-output"
	one.File.GlobalDeny[0] = "changed-output"
	one.File.Metadata["owner"] = "changed-output"
	two := s.GlobalSnapshot()
	if two.File.Version != 7 || two.File.Rules[0].Pattern != "git status" || two.File.GlobalDeny[0] != "shutdown" || two.File.Metadata["owner"] != "operator" {
		t.Fatalf("snapshot shares returned containers: %#v", two)
	}
}

func TestGlobalSnapshotConcurrentReadersAndWriters(t *testing.T) {
	const (
		writers    = 8
		readers    = 16
		iterations = 250
	)
	s := NewStore()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < iterations; n++ {
				version := writer*iterations + n + 1
				s.SetGlobalSnapshot(ApprovalsFile{
					Version: version,
					Rules: []ApprovalRule{{
						Pattern:  fmt.Sprintf("writer-%d-%d", writer, n),
						Decision: "allow",
					}},
					GlobalDeny: []string{fmt.Sprintf("deny-%d-%d", writer, n)},
					Metadata:   map[string]string{"version": fmt.Sprint(version)},
				}, fmt.Sprintf("hash-%d", version))
			}
		}()
	}
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < iterations*2; n++ {
				snap := s.GlobalSnapshot()
				if snap == nil {
					t.Error("GlobalSnapshot returned nil")
					return
				}
				if snap.Hash == "" {
					continue // the default snapshot can be observed before the first writer
				}
				if len(snap.File.Rules) != 1 || len(snap.File.GlobalDeny) != 1 || len(snap.File.Metadata) != 1 {
					t.Errorf("torn snapshot: %#v", snap)
					return
				}
				if snap.File.Metadata["version"] != fmt.Sprint(snap.File.Version) {
					t.Errorf("mixed snapshot versions: %#v", snap)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestConcurrentLifecycleOperationsRemainConsistent(t *testing.T) {
	const requests = 300
	s := NewStore()
	ids := make([]string, requests)
	for i := range ids {
		ids[i] = fmt.Sprintf("request-%03d", i)
		s.CreateRequest(CreateRequestParams{ID: ids[i], Command: ids[i]})
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 20; n++ {
				got := s.Get(id)
				if got == nil || got.ID != id || got.Command != id {
					t.Errorf("Get(%s) = %#v", id, got)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			ch := s.WaitForDecision(id)
			assertClosed(t, ch, id)
		}()
		go func() {
			defer wg.Done()
			<-start
			decision := []Decision{DecisionAllowOnce, DecisionAllowAlways, DecisionDeny}[i%3]
			if err := s.Resolve(id, decision); err != nil {
				t.Errorf("Resolve(%s): %v", id, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	resolved := make([]string, 0, requests)
	for _, id := range ids {
		got := s.Get(id)
		if got == nil || got.Decision == nil || got.ResolvedAtMs == nil {
			t.Errorf("request %s not fully resolved: %#v", id, got)
			continue
		}
		resolved = append(resolved, id)
	}
	sort.Strings(resolved)
	if !reflect.DeepEqual(resolved, ids) {
		t.Fatalf("resolved IDs mismatch: got=%v want=%v", resolved, ids)
	}
	if got := s.Cleanup(); got != 0 {
		t.Fatalf("Cleanup removed %d resolved requests", got)
	}
}

func TestNilMutableFieldsRemainNilAcrossCopies(t *testing.T) {
	t.Parallel()

	s := NewStore()
	created := s.CreateRequest(CreateRequestParams{ID: "nil-fields"})
	if created.CommandArgv != nil || created.Env != nil || created.TurnSourceInfo != nil {
		t.Fatalf("Create converted nil fields: %#v", created)
	}
	got := s.Get("nil-fields")
	if got.CommandArgv != nil || got.Env != nil || got.TurnSourceInfo != nil {
		t.Fatalf("Get converted nil fields: %#v", got)
	}
	s.SetGlobalSnapshot(ApprovalsFile{Version: 1}, "nil-containers")
	snap := s.GlobalSnapshot()
	if snap.File.Rules != nil || snap.File.GlobalDeny != nil || snap.File.Metadata != nil {
		t.Fatalf("snapshot converted nil containers: %#v", snap.File)
	}
}

func TestEmptyMutableFieldsRemainIndependent(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.CreateRequest(CreateRequestParams{
		ID:          "empty-fields",
		CommandArgv: []string{},
		Env:         map[string]string{},
	})
	one := s.Get("empty-fields")
	two := s.Get("empty-fields")
	if one.CommandArgv == nil || one.Env == nil || two.CommandArgv == nil || two.Env == nil {
		t.Fatalf("non-nil empty fields became nil: one=%#v two=%#v", one, two)
	}
	one.CommandArgv = append(one.CommandArgv, "local")
	one.Env["local"] = "only"
	if len(two.CommandArgv) != 0 || len(two.Env) != 0 {
		t.Fatalf("empty snapshots share containers: one=%#v two=%#v", one, two)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: channel did not close", label)
	}
}

func assertNotClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s: channel closed early", label)
	default:
	}
}
