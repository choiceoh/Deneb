package runstate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func newBoundaryAbortEntry(sessionKey, clientRun string, expiresAt time.Time) (*AbortEntry, context.Context) {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &AbortEntry{
		SessionKey: sessionKey,
		ClientRun:  clientRun,
		CancelFn:   cancel,
		ExpiresAt:  expiresAt,
	}, ctx
}

func TestBoundaryAbortTrackerStartsEmpty(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	if tracker == nil {
		t.Fatal("NewAbortTracker returned nil")
	}
	if tracker.entries == nil {
		t.Fatal("NewAbortTracker did not initialize entries")
	}
	if tracker.done == nil {
		t.Fatal("NewAbortTracker did not initialize GC stop channel")
	}
	if tracker.gcClosed {
		t.Fatal("new tracker starts closed")
	}
	for _, session := range []string{"", "client:main", "client:main:topic", "cron:job:1"} {
		if tracker.HasActiveRun(session) {
			t.Fatalf("empty tracker reports active run for %q", session)
		}
		if got := tracker.CountForSession(session); got != 0 {
			t.Fatalf("empty CountForSession(%q) = %d", session, got)
		}
	}
}

func TestBoundaryAbortTrackerIgnoresEmptyClientRunID(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	entry, ctx := newBoundaryAbortEntry("client:main", "internal-run", time.Now().Add(time.Hour))
	tracker.Register("", entry)
	if tracker.HasActiveRun("client:main") || tracker.CountForSession("client:main") != 0 {
		t.Fatal("empty client run ID was registered")
	}
	if cause := context.Cause(ctx); cause != nil {
		t.Fatalf("ignored registration canceled context: %v", cause)
	}
	tracker.Cleanup("")
	if cause := context.Cause(ctx); cause != nil {
		t.Fatalf("empty cleanup canceled context: %v", cause)
	}
}

func TestBoundaryAbortTrackerSessionCountMatrix(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	now := time.Now().Add(time.Hour)
	registrations := []struct {
		id      string
		session string
	}{
		{id: "a-1", session: "client:a"},
		{id: "a-2", session: "client:a"},
		{id: "a-3", session: "client:a"},
		{id: "b-1", session: "client:b"},
		{id: "b-2", session: "client:b"},
		{id: "topic-1", session: "client:a:topic"},
		{id: "empty-session", session: ""},
	}
	for _, reg := range registrations {
		entry, _ := newBoundaryAbortEntry(reg.session, reg.id, now)
		tracker.Register(reg.id, entry)
	}
	tests := []struct {
		session string
		count   int
		active  bool
	}{
		{session: "client:a", count: 3, active: true},
		{session: "client:b", count: 2, active: true},
		{session: "client:a:topic", count: 1, active: true},
		{session: "", count: 1, active: true},
		{session: "client", count: 0, active: false},
		{session: "client:a:missing", count: 0, active: false},
		{session: "CLIENT:A", count: 0, active: false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("session_%q", tt.session), func(t *testing.T) {
			if got := tracker.CountForSession(tt.session); got != tt.count {
				t.Fatalf("CountForSession(%q) = %d, want %d", tt.session, got, tt.count)
			}
			if got := tracker.HasActiveRun(tt.session); got != tt.active {
				t.Fatalf("HasActiveRun(%q) = %v, want %v", tt.session, got, tt.active)
			}
		})
	}
}

func TestBoundaryAbortTrackerCleanupDoesNotCancel(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	entries := make(map[string]context.Context)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("run-%d", i)
		entry, ctx := newBoundaryAbortEntry("client:main", id, time.Now().Add(time.Hour))
		entries[id] = ctx
		tracker.Register(id, entry)
	}
	for i := 0; i < 8; i += 2 {
		tracker.Cleanup(fmt.Sprintf("run-%d", i))
	}
	if got := tracker.CountForSession("client:main"); got != 4 {
		t.Fatalf("remaining count = %d, want 4", got)
	}
	for id, ctx := range entries {
		if cause := context.Cause(ctx); cause != nil {
			t.Fatalf("Cleanup canceled %s: %v", id, cause)
		}
	}
	// Unknown and repeated cleanup remain safe no-ops.
	tracker.Cleanup("missing")
	tracker.Cleanup("run-0")
	if got := tracker.CountForSession("client:main"); got != 4 {
		t.Fatalf("idempotent cleanup changed count to %d", got)
	}
}

func TestBoundaryAbortTrackerCancelByRunIDMatrix(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	contexts := make(map[string]context.Context)
	for _, tc := range []struct {
		id      string
		session string
	}{
		{id: "run-a", session: "client:a"},
		{id: "run-b", session: "client:b"},
		{id: "run-topic", session: "client:a:topic"},
	} {
		entry, ctx := newBoundaryAbortEntry(tc.session, tc.id, time.Now().Add(time.Hour))
		contexts[tc.id] = ctx
		tracker.Register(tc.id, entry)
	}

	tests := []struct {
		name        string
		runID       string
		wantSession string
		wantRun     string
	}{
		{name: "unknown", runID: "missing", wantSession: "", wantRun: ""},
		{name: "empty", runID: "", wantSession: "", wantRun: ""},
		{name: "exact a", runID: "run-a", wantSession: "client:a", wantRun: "run-a"},
		{name: "case sensitive miss", runID: "RUN-B", wantSession: "", wantRun: ""},
		{name: "exact topic", runID: "run-topic", wantSession: "client:a:topic", wantRun: "run-topic"},
		{name: "repeated a", runID: "run-a", wantSession: "", wantRun: ""},
		{name: "exact b", runID: "run-b", wantSession: "client:b", wantRun: "run-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, run := tracker.CancelByRunID(tt.runID)
			if session != tt.wantSession || run != tt.wantRun {
				t.Fatalf("CancelByRunID(%q) = (%q, %q), want (%q, %q)", tt.runID, session, run, tt.wantSession, tt.wantRun)
			}
		})
	}
	for id, ctx := range contexts {
		if cause := context.Cause(ctx); !errors.Is(cause, context.Canceled) {
			t.Fatalf("%s cause = %v, want canceled", id, cause)
		}
	}
	if got := tracker.CountForSession("client:a") + tracker.CountForSession("client:b") + tracker.CountForSession("client:a:topic"); got != 0 {
		t.Fatalf("cancel matrix left %d entries", got)
	}
}

func TestBoundaryAbortTrackerCancelBySessionCauseIsolation(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	wantCause := errors.New("merged into newer run")
	var targetContexts []context.Context
	var otherContexts []context.Context
	for i := 0; i < 12; i++ {
		session := "client:other"
		if i%2 == 0 {
			session = "client:target"
		}
		id := fmt.Sprintf("run-%02d", i)
		entry, ctx := newBoundaryAbortEntry(session, id, time.Now().Add(time.Hour))
		tracker.Register(id, entry)
		if session == "client:target" {
			targetContexts = append(targetContexts, ctx)
		} else {
			otherContexts = append(otherContexts, ctx)
		}
	}
	abortedID := tracker.CancelBySessionKeyWithCause("client:target", wantCause)
	if abortedID == "" {
		t.Fatal("CancelBySessionKeyWithCause returned no representative run ID")
	}
	if tracker.HasActiveRun("client:target") || tracker.CountForSession("client:target") != 0 {
		t.Fatal("target session still active")
	}
	if got := tracker.CountForSession("client:other"); got != len(otherContexts) {
		t.Fatalf("other session count = %d, want %d", got, len(otherContexts))
	}
	for i, ctx := range targetContexts {
		if cause := context.Cause(ctx); !errors.Is(cause, wantCause) {
			t.Fatalf("target context %d cause = %v", i, cause)
		}
	}
	for i, ctx := range otherContexts {
		if cause := context.Cause(ctx); cause != nil {
			t.Fatalf("other context %d canceled: %v", i, cause)
		}
	}
}

func TestBoundaryAbortTrackerCancelBySessionDefaultCause(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	entry, ctx := newBoundaryAbortEntry("client:main", "run", time.Now().Add(time.Hour))
	tracker.Register("run", entry)
	if got := tracker.CancelBySessionKey("client:main"); got != "run" {
		t.Fatalf("CancelBySessionKey = %q, want run", got)
	}
	if cause := context.Cause(ctx); !errors.Is(cause, context.Canceled) {
		t.Fatalf("default cause = %v, want context.Canceled", cause)
	}
	if got := tracker.CancelBySessionKey("client:main"); got != "" {
		t.Fatalf("repeated cancellation = %q", got)
	}
	if got := tracker.CancelBySessionKey("missing"); got != "" {
		t.Fatalf("missing cancellation = %q", got)
	}
}

func TestBoundaryAbortTrackerInterruptCancelsOnlyExactSession(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	contexts := make(map[string]context.Context)
	for _, session := range []string{
		"client:main",
		"client:main",
		"client:main:topic",
		"client:main-extra",
		"CLIENT:MAIN",
		"client:other",
	} {
		id := fmt.Sprintf("run-%02d", len(contexts))
		entry, ctx := newBoundaryAbortEntry(session, id, time.Now().Add(time.Hour))
		tracker.Register(id, entry)
		contexts[id] = ctx
	}
	tracker.InterruptSession("client:main")
	if got := tracker.CountForSession("client:main"); got != 0 {
		t.Fatalf("exact session still has %d entries", got)
	}
	for session, want := range map[string]int{
		"client:main:topic": 1,
		"client:main-extra": 1,
		"CLIENT:MAIN":       1,
		"client:other":      1,
	} {
		if got := tracker.CountForSession(session); got != want {
			t.Fatalf("CountForSession(%q) = %d, want %d", session, got, want)
		}
	}
	// Empty and unknown interrupts are safe and preserve unrelated runs.
	tracker.InterruptSession("")
	tracker.InterruptSession("missing")
	if got := len(tracker.entries); got != 4 {
		t.Fatalf("unrelated entry count = %d, want 4", got)
	}
}

func TestBoundaryAbortTrackerRegisterReplacementUsesNewestEntry(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	oldEntry, oldCtx := newBoundaryAbortEntry("client:old", "shared", time.Now().Add(time.Hour))
	newEntry, newCtx := newBoundaryAbortEntry("client:new", "shared", time.Now().Add(time.Hour))
	tracker.Register("shared", oldEntry)
	tracker.Register("shared", newEntry)
	if tracker.HasActiveRun("client:old") {
		t.Fatal("replaced entry remains addressable under old session")
	}
	if !tracker.HasActiveRun("client:new") {
		t.Fatal("newest replacement is not active")
	}
	session, run := tracker.CancelByRunID("shared")
	if session != "client:new" || run != "shared" {
		t.Fatalf("canceled replacement = (%q, %q)", session, run)
	}
	if cause := context.Cause(newCtx); !errors.Is(cause, context.Canceled) {
		t.Fatalf("new context cause = %v", cause)
	}
	if cause := context.Cause(oldCtx); cause != nil {
		t.Fatalf("registration replacement unexpectedly canceled caller-owned old context: %v", cause)
	}
}

func TestBoundaryAbortTrackerCloseCancelsEverySessionAndIsConcurrentSafe(t *testing.T) {
	tracker := NewAbortTracker()
	const runs = 80
	contexts := make([]context.Context, 0, runs)
	for i := 0; i < runs; i++ {
		id := fmt.Sprintf("run-%03d", i)
		entry, ctx := newBoundaryAbortEntry(fmt.Sprintf("session-%d", i%7), id, time.Now().Add(time.Hour))
		contexts = append(contexts, ctx)
		tracker.Register(id, entry)
	}

	const closers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(closers)
	for i := 0; i < closers; i++ {
		go func() {
			defer wg.Done()
			<-start
			tracker.Close()
		}()
	}
	close(start)
	wg.Wait()
	if !tracker.gcClosed {
		t.Fatal("concurrent Close did not mark GC closed")
	}
	if got := len(tracker.entries); got != 0 {
		t.Fatalf("Close left %d entries", got)
	}
	select {
	case <-tracker.done:
	default:
		t.Fatal("Close did not close done channel")
	}
	for i, ctx := range contexts {
		if cause := context.Cause(ctx); !errors.Is(cause, context.Canceled) {
			t.Fatalf("context %d cause = %v", i, cause)
		}
	}
}

func TestBoundaryAbortTrackerConcurrentRegisterQueryCleanup(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)
	const workers = 128
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("run-%03d", i)
			session := fmt.Sprintf("session-%d", i%8)
			entry, _ := newBoundaryAbortEntry(session, id, time.Now().Add(time.Hour))
			tracker.Register(id, entry)
			_ = tracker.HasActiveRun(session)
			_ = tracker.CountForSession(session)
			if i%3 == 0 {
				tracker.Cleanup(id)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	want := workers - (workers+2)/3
	if got := len(tracker.entries); got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}
}

func TestBoundaryAbortGCLoopStopsOnClosedDone(t *testing.T) {
	done := make(chan struct{})
	close(done)
	tracker := &AbortTracker{
		entries: make(map[string]*AbortEntry),
		done:    done,
	}
	returned := make(chan struct{})
	go func() {
		tracker.gcLoop()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("gcLoop did not stop after done closed")
	}
}

func TestBoundaryPendingQueueStartsEmpty(t *testing.T) {
	queue := NewPendingQueue()
	if queue == nil || queue.items == nil {
		t.Fatalf("NewPendingQueue = %#v", queue)
	}
	for _, session := range []string{"", "client:main", "client:main:topic"} {
		if got := queue.Len(session); got != 0 {
			t.Fatalf("Len(%q) = %d", session, got)
		}
		if got := queue.Drain(session); got != nil {
			t.Fatalf("Drain(%q) = %#v", session, got)
		}
		queue.Clear(session)
	}
}

func TestBoundaryPendingQueueLatestValueFieldMatrix(t *testing.T) {
	queue := NewPendingQueue()
	temperature := 0.25
	topP := 0.9
	maxTokens := 1234
	frequency := -0.5
	presence := 0.75
	want := Params{
		SessionKey:           "client:main",
		Message:              "latest",
		Attachments:          []toolport.ChatAttachment{{Name: "report.pdf", MimeType: "application/pdf"}},
		Model:                "main",
		System:               "system override",
		ClientRunID:          "run-latest",
		WorkspaceDir:         "/workspace",
		Temperature:          &temperature,
		TopP:                 &topP,
		MaxTokens:            &maxTokens,
		FrequencyPenalty:     &frequency,
		PresencePenalty:      &presence,
		Stop:                 []string{"STOP", "END"},
		ResponseFormat:       &llm.ResponseFormat{Type: "json_object"},
		ToolChoice:           rawJSON(`"required"`),
		Thinking:             "high",
		EphemeralUser:        true,
		AppendCurrentMessage: true,
		SkipRecall:           true,
		FeedContext:          "feed",
		EphemeralAssistant:   true,
		AutoDeliveredOutput:  true,
		ToolDryRun:           true,
		GateUntrustedTools:   true,
	}
	queue.Enqueue(want.SessionKey, Params{SessionKey: want.SessionKey, Message: "old"})
	queue.Enqueue(want.SessionKey, want)
	got := queue.Drain(want.SessionKey)
	if got == nil {
		t.Fatal("Drain returned nil")
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("Drain = %#v, want %#v", *got, want)
	}
	if queue.Len(want.SessionKey) != 0 || queue.Drain(want.SessionKey) != nil {
		t.Fatal("drain did not empty latest-only queue")
	}
}

func TestBoundaryPendingQueueSessionKeyArgumentOwnsRoutingBucket(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("bucket-a", Params{SessionKey: "payload-b", Message: "mismatch"})
	if got := queue.Len("payload-b"); got != 0 {
		t.Fatalf("payload SessionKey unexpectedly selected bucket: %d", got)
	}
	got := queue.Drain("bucket-a")
	if got == nil || got.SessionKey != "payload-b" || got.Message != "mismatch" {
		t.Fatalf("argument-owned bucket returned %#v", got)
	}
	// Empty session is a valid exact map key for PendingQueue (unlike SteerQueue).
	queue.Enqueue("", Params{Message: "headless"})
	if got := queue.Drain(""); got == nil || got.Message != "headless" {
		t.Fatalf("empty bucket returned %#v", got)
	}
}

func TestBoundaryPendingQueueClearIsSessionExact(t *testing.T) {
	queue := NewPendingQueue()
	sessions := []string{
		"client:main",
		"client:main:topic",
		"client:main-extra",
		"CLIENT:MAIN",
		"client:other",
		"",
	}
	for _, session := range sessions {
		queue.Enqueue(session, Params{SessionKey: session, Message: "queued:" + session})
	}
	queue.Clear("client:main")
	if got := queue.Drain("client:main"); got != nil {
		t.Fatalf("cleared exact session returned %#v", got)
	}
	for _, session := range sessions[1:] {
		if got := queue.Drain(session); got == nil || got.Message != "queued:"+session {
			t.Fatalf("Clear affected %q: %#v", session, got)
		}
	}
}

func TestBoundaryPendingQueueResetDetachesOldPerSessionQueues(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("client:a", Params{Message: "old-a"})
	queue.Enqueue("client:b", Params{Message: "old-b"})
	oldA := queue.items["client:a"]
	oldB := queue.items["client:b"]
	queue.Reset()
	if len(queue.items) != 0 {
		t.Fatalf("Reset map length = %d", len(queue.items))
	}
	if queue.Drain("client:a") != nil || queue.Drain("client:b") != nil {
		t.Fatal("Reset left public entries visible")
	}
	if got := oldA.drain(); got == nil || got.Message != "old-a" {
		t.Fatalf("detached old A queue corrupted: %#v", got)
	}
	if got := oldB.drain(); got == nil || got.Message != "old-b" {
		t.Fatalf("detached old B queue corrupted: %#v", got)
	}
	queue.Enqueue("client:a", Params{Message: "new-a"})
	if queue.items["client:a"] == oldA {
		t.Fatal("Reset reused detached per-session queue")
	}
}

func TestBoundaryPendingRunQueueConcurrentLatestOnly(t *testing.T) {
	queue := &pendingRunQueue{}
	const writers = 160
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			queue.enqueue(Params{Message: fmt.Sprintf("message-%03d", i)})
		}(i)
	}
	close(start)
	wg.Wait()
	if got := queue.len(); got != 1 {
		t.Fatalf("latest-only internal length = %d", got)
	}
	got := queue.drain()
	if got == nil || !strings.HasPrefix(got.Message, "message-") {
		t.Fatalf("drained invalid message: %#v", got)
	}
	if queue.len() != 0 || queue.drain() != nil {
		t.Fatal("internal drain did not empty queue")
	}
}

func TestBoundaryPendingQueueConcurrentIndependentSessions(t *testing.T) {
	queue := NewPendingQueue()
	const sessions = 40
	const writesPerSession = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	for session := 0; session < sessions; session++ {
		for write := 0; write < writesPerSession; write++ {
			wg.Add(1)
			go func(session, write int) {
				defer wg.Done()
				<-start
				key := fmt.Sprintf("session-%02d", session)
				queue.Enqueue(key, Params{SessionKey: key, Message: fmt.Sprintf("message-%02d", write)})
			}(session, write)
		}
	}
	close(start)
	wg.Wait()
	for session := 0; session < sessions; session++ {
		key := fmt.Sprintf("session-%02d", session)
		if got := queue.Len(key); got != 1 {
			t.Fatalf("Len(%s) = %d", key, got)
		}
		got := queue.Drain(key)
		if got == nil || got.SessionKey != key || !strings.HasPrefix(got.Message, "message-") {
			t.Fatalf("Drain(%s) = %#v", key, got)
		}
	}
}

func TestBoundaryPendingQueueConcurrentDrainAtMostOnce(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("client:main", Params{Message: "one"})
	const readers = 96
	start := make(chan struct{})
	results := make(chan *Params, readers)
	for i := 0; i < readers; i++ {
		go func() {
			<-start
			results <- queue.Drain("client:main")
		}()
	}
	close(start)
	nonNil := 0
	for i := 0; i < readers; i++ {
		if got := <-results; got != nil {
			nonNil++
			if got.Message != "one" {
				t.Fatalf("unexpected drained value: %#v", got)
			}
		}
	}
	if nonNil != 1 {
		t.Fatalf("non-nil drains = %d, want 1", nonNil)
	}
}

func TestBoundarySteerQueueRejectsInvalidInputMatrix(t *testing.T) {
	queue := NewSteerQueue()
	tests := []struct {
		name    string
		session string
		note    string
	}{
		{name: "empty session empty note", session: "", note: ""},
		{name: "empty session valid note", session: "", note: "note"},
		{name: "valid session empty note", session: "client:main", note: ""},
		{name: "space note", session: "client:main", note: "   "},
		{name: "tab note", session: "client:main", note: "\t\t"},
		{name: "newline note", session: "client:main", note: "\r\n"},
		{name: "unicode whitespace note", session: "client:main", note: "\u2003\u3000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if queue.Enqueue(tt.session, tt.note) {
				t.Fatalf("Enqueue(%q, %q) accepted invalid input", tt.session, tt.note)
			}
			if queue.Len(tt.session) != 0 {
				t.Fatalf("invalid enqueue changed length for %q", tt.session)
			}
		})
	}
	if got := queue.Drain(""); got != nil {
		t.Fatalf("Drain(empty) = %#v", got)
	}
	queue.Restore("", []string{"note"})
	queue.Restore("client:main", nil)
	queue.Restore("client:main", []string{})
	queue.Clear("")
	if len(queue.items) != 0 {
		t.Fatalf("invalid operations populated queue: %#v", queue.items)
	}
}

func TestBoundarySteerQueueTrimsOnlyEdges(t *testing.T) {
	queue := NewSteerQueue()
	tests := []struct {
		name string
		note string
		want string
	}{
		{name: "plain", note: "keep", want: "keep"},
		{name: "leading spaces", note: "   keep", want: "keep"},
		{name: "trailing spaces", note: "keep   ", want: "keep"},
		{name: "tabs and newlines", note: "\t\n keep \r\n", want: "keep"},
		{name: "internal spaces", note: "keep   internal", want: "keep   internal"},
		{name: "internal newline", note: "first\n\nsecond", want: "first\n\nsecond"},
		{name: "unicode", note: "  일정 📅 변경  ", want: "일정 📅 변경"},
		{name: "unicode edge whitespace", note: "\u2003keep\u3000", want: "keep"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := fmt.Sprintf("session-%d", i)
			if !queue.Enqueue(session, tt.note) {
				t.Fatalf("valid note rejected: %q", tt.note)
			}
			got := queue.Drain(session)
			if !reflect.DeepEqual(got, []string{tt.want}) {
				t.Fatalf("Drain = %#v, want %#v", got, []string{tt.want})
			}
		})
	}
}

func TestBoundarySteerQueueOrderingAndSessionIsolation(t *testing.T) {
	queue := NewSteerQueue()
	inputs := []struct {
		session string
		note    string
	}{
		{session: "a", note: "a-1"},
		{session: "b", note: "b-1"},
		{session: "a", note: "a-2"},
		{session: "c", note: "c-1"},
		{session: "b", note: "b-2"},
		{session: "a", note: "a-3"},
		{session: "c", note: "c-2"},
	}
	for _, input := range inputs {
		if !queue.Enqueue(input.session, input.note) {
			t.Fatalf("rejected %#v", input)
		}
	}
	if got := queue.Drain("b"); !reflect.DeepEqual(got, []string{"b-1", "b-2"}) {
		t.Fatalf("Drain(b) = %#v", got)
	}
	if got := queue.Drain("a"); !reflect.DeepEqual(got, []string{"a-1", "a-2", "a-3"}) {
		t.Fatalf("Drain(a) = %#v", got)
	}
	if got := queue.Drain("c"); !reflect.DeepEqual(got, []string{"c-1", "c-2"}) {
		t.Fatalf("Drain(c) = %#v", got)
	}
	for _, session := range []string{"a", "b", "c"} {
		if queue.Len(session) != 0 || queue.Drain(session) != nil {
			t.Fatalf("session %q not empty after drain", session)
		}
	}
}

func TestBoundarySteerRestorePrependsBeforeNewArrivals(t *testing.T) {
	queue := NewSteerQueue()
	for _, note := range []string{"old-1", "old-2", "old-3"} {
		queue.Enqueue("client:main", note)
	}
	drained := queue.Drain("client:main")
	if !reflect.DeepEqual(drained, []string{"old-1", "old-2", "old-3"}) {
		t.Fatalf("first drain = %#v", drained)
	}
	queue.Enqueue("client:main", "new-1")
	queue.Enqueue("client:main", "new-2")
	queue.Restore("client:main", drained)
	want := []string{"old-1", "old-2", "old-3", "new-1", "new-2"}
	if got := queue.Drain("client:main"); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored order = %#v, want %#v", got, want)
	}
}

func TestBoundarySteerRestoreCopiesSliceHeaderAndBackingData(t *testing.T) {
	queue := NewSteerQueue()
	notes := []string{"one", "two"}
	queue.Restore("client:main", notes)
	notes[0] = "caller-mutated"
	notes = append(notes, "caller-appended") //nolint:ineffassign,staticcheck // mutation probe: proves Restore copied, not aliased
	got := queue.Drain("client:main")
	if !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("Restore retained caller alias: %#v", got)
	}
	got[0] = "consumer-mutated"
	if queue.Len("client:main") != 0 {
		t.Fatal("mutating drained slice changed empty queue")
	}
}

func TestBoundarySteerClearIsExactAndIdempotent(t *testing.T) {
	queue := NewSteerQueue()
	sessions := []string{"client:main", "client:main:topic", "client:main-extra", "CLIENT:MAIN", "other"}
	for _, session := range sessions {
		queue.Enqueue(session, "note:"+session)
	}
	queue.Clear("client:main")
	queue.Clear("client:main")
	queue.Clear("missing")
	if queue.Drain("client:main") != nil {
		t.Fatal("exact cleared session remained")
	}
	for _, session := range sessions[1:] {
		if got := queue.Drain(session); !reflect.DeepEqual(got, []string{"note:" + session}) {
			t.Fatalf("Clear affected %q: %#v", session, got)
		}
	}
}

func TestBoundarySteerResetDetachesAllSessions(t *testing.T) {
	queue := NewSteerQueue()
	for i := 0; i < 30; i++ {
		session := fmt.Sprintf("session-%02d", i)
		queue.Enqueue(session, "one")
		queue.Enqueue(session, "two")
	}
	old := queue.items
	queue.Reset()
	if len(queue.items) != 0 {
		t.Fatalf("Reset left %d sessions", len(queue.items))
	}
	if reflect.ValueOf(old).Pointer() == reflect.ValueOf(queue.items).Pointer() {
		t.Fatal("Reset reused old map")
	}
	for i := 0; i < 30; i++ {
		session := fmt.Sprintf("session-%02d", i)
		if queue.Len(session) != 0 || queue.Drain(session) != nil {
			t.Fatalf("Reset left %q visible", session)
		}
	}
	queue.Enqueue("new", "note")
	if got := queue.Drain("new"); !reflect.DeepEqual(got, []string{"note"}) {
		t.Fatalf("queue unusable after Reset: %#v", got)
	}
}

func TestBoundarySteerConcurrentProducersPreserveAllNotes(t *testing.T) {
	queue := NewSteerQueue()
	const writers = 200
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			if !queue.Enqueue("client:main", fmt.Sprintf("note-%03d", i)) {
				t.Errorf("writer %d rejected", i)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if got := queue.Len("client:main"); got != writers {
		t.Fatalf("Len = %d, want %d", got, writers)
	}
	notes := queue.Drain("client:main")
	if len(notes) != writers {
		t.Fatalf("Drain length = %d", len(notes))
	}
	sort.Strings(notes)
	for i, note := range notes {
		want := fmt.Sprintf("note-%03d", i)
		if note != want {
			t.Fatalf("sorted note %d = %q, want %q", i, note, want)
		}
	}
}

func TestBoundarySteerConcurrentDrainReturnsEachNoteOnce(t *testing.T) {
	queue := NewSteerQueue()
	const notes = 120
	for i := 0; i < notes; i++ {
		queue.Enqueue("client:main", fmt.Sprintf("note-%03d", i))
	}
	const readers = 64
	start := make(chan struct{})
	results := make(chan []string, readers)
	for i := 0; i < readers; i++ {
		go func() {
			<-start
			results <- queue.Drain("client:main")
		}()
	}
	close(start)
	nonEmpty := 0
	total := 0
	for i := 0; i < readers; i++ {
		got := <-results
		if len(got) > 0 {
			nonEmpty++
			total += len(got)
		}
	}
	if nonEmpty != 1 || total != notes {
		t.Fatalf("non-empty drains=%d total notes=%d", nonEmpty, total)
	}
}

func TestBoundarySteerConcurrentSessionsRemainIsolated(t *testing.T) {
	queue := NewSteerQueue()
	const sessions = 32
	const perSession = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for session := 0; session < sessions; session++ {
		for note := 0; note < perSession; note++ {
			wg.Add(1)
			go func(session, note int) {
				defer wg.Done()
				<-start
				queue.Enqueue(fmt.Sprintf("session-%02d", session), fmt.Sprintf("note-%02d", note))
			}(session, note)
		}
	}
	close(start)
	wg.Wait()
	for session := 0; session < sessions; session++ {
		key := fmt.Sprintf("session-%02d", session)
		if got := queue.Len(key); got != perSession {
			t.Fatalf("Len(%s) = %d", key, got)
		}
		notes := queue.Drain(key)
		if len(notes) != perSession {
			t.Fatalf("Drain(%s) length = %d", key, len(notes))
		}
		seen := make(map[string]bool, perSession)
		for _, note := range notes {
			if seen[note] {
				t.Fatalf("duplicate %q in %s", note, key)
			}
			seen[note] = true
		}
	}
}

func TestBoundarySteerMixedOperationsRemainRaceFreeAndBounded(t *testing.T) {
	queue := NewSteerQueue()
	const workers = 96
	const iterations = 100
	start := make(chan struct{})
	var accepted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				session := fmt.Sprintf("session-%d", (worker+iteration)%12)
				switch (worker + iteration) % 5 {
				case 0, 1:
					if queue.Enqueue(session, fmt.Sprintf("%d-%d", worker, iteration)) {
						accepted.Add(1)
					}
				case 2:
					notes := queue.Drain(session)
					if len(notes) > 0 && iteration%2 == 0 {
						queue.Restore(session, notes)
					}
				case 3:
					_ = queue.Len(session)
				case 4:
					if iteration%17 == 0 {
						queue.Clear(session)
					}
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	if accepted.Load() == 0 {
		t.Fatal("mixed workload accepted no notes")
	}
	total := 0
	for i := 0; i < 12; i++ {
		session := fmt.Sprintf("session-%d", i)
		if n := queue.Len(session); n < 0 {
			t.Fatalf("negative length for %s", session)
		}
		total += len(queue.Drain(session))
	}
	if int64(total) > accepted.Load() {
		t.Fatalf("drained %d notes after only %d accepted", total, accepted.Load())
	}
}
