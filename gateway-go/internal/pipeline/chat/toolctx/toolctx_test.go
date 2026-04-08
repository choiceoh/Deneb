package toolctx

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// types.go
// ---------------------------------------------------------------------------

func TestNewTextChatMessage(t *testing.T) {
	ts := time.Now().Unix()
	msg := NewTextChatMessage("user", "hello world", ts)

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if msg.Timestamp != ts {
		t.Errorf("Timestamp = %d, want %d", msg.Timestamp, ts)
	}
	// Content should be a JSON-quoted string.
	var s string
	if err := json.Unmarshal(msg.Content, &s); err != nil {
		t.Fatalf("Unmarshal Content: %v", err)
	}
	if s != "hello world" {
		t.Errorf("Content text = %q, want %q", s, "hello world")
	}
}

func TestTextContent(t *testing.T) {
	t.Run("json string", func(t *testing.T) {
		msg := ChatMessage{Content: MarshalJSONString("plain text")}
		if got := msg.TextContent(); got != "plain text" {
			t.Errorf("TextContent() = %q, want %q", got, "plain text")
		}
	})

	t.Run("content block array", func(t *testing.T) {
		blocks := []map[string]string{
			{"type": "text", "text": "first"},
			{"type": "tool_use"},
			{"type": "text", "text": "second"},
		}
		raw, _ := json.Marshal(blocks)
		msg := ChatMessage{Content: raw}
		want := "first\n\nsecond"
		if got := msg.TextContent(); got != want {
			t.Errorf("TextContent() = %q, want %q", got, want)
		}
	})

	t.Run("single text block", func(t *testing.T) {
		blocks := []map[string]string{
			{"type": "text", "text": "only"},
		}
		raw, _ := json.Marshal(blocks)
		msg := ChatMessage{Content: raw}
		if got := msg.TextContent(); got != "only" {
			t.Errorf("TextContent() = %q, want %q", got, "only")
		}
	})

	t.Run("nil content", func(t *testing.T) {
		msg := ChatMessage{Content: nil}
		if got := msg.TextContent(); got != "" {
			t.Errorf("TextContent() = %q, want empty", got)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		msg := ChatMessage{Content: json.RawMessage{}}
		if got := msg.TextContent(); got != "" {
			t.Errorf("TextContent() = %q, want empty", got)
		}
	})
}

func TestHasContent(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		msg := ChatMessage{Content: nil}
		if msg.HasContent() {
			t.Error("HasContent() = true for nil, want false")
		}
	})

	t.Run("empty json string", func(t *testing.T) {
		msg := ChatMessage{Content: json.RawMessage(`""`)}
		if msg.HasContent() {
			t.Error(`HasContent() = true for "", want false`)
		}
	})

	t.Run("real content", func(t *testing.T) {
		msg := ChatMessage{Content: MarshalJSONString("hello")}
		if !msg.HasContent() {
			t.Error("HasContent() = false for real content, want true")
		}
	})

	t.Run("empty raw message", func(t *testing.T) {
		msg := ChatMessage{Content: json.RawMessage{}}
		if msg.HasContent() {
			t.Error("HasContent() = true for empty RawMessage, want false")
		}
	})
}

func TestMarshalJSONString(t *testing.T) {
	got := MarshalJSONString("test")
	if string(got) != `"test"` {
		t.Errorf("MarshalJSONString(%q) = %s, want %q", "test", got, `"test"`)
	}

	// Verify special characters are escaped.
	got = MarshalJSONString(`say "hi"`)
	var decoded string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded != `say "hi"` {
		t.Errorf("roundtrip = %q, want %q", decoded, `say "hi"`)
	}
}

// ---------------------------------------------------------------------------
// context.go — With*/FromContext roundtrips
// ---------------------------------------------------------------------------

func TestDeliveryContextRoundtrip(t *testing.T) {
	dc := &DeliveryContext{Channel: "telegram", To: "user1"}
	ctx := WithDeliveryContext(context.Background(), dc)
	got := DeliveryFromContext(ctx)
	if !reflect.DeepEqual(dc, got) {
		t.Errorf("DeliveryFromContext mismatch:\n  want: %+v\n   got: %+v", dc, got)
	}
}

func TestDeliveryFromContext_empty(t *testing.T) {
	got := DeliveryFromContext(context.Background())
	if got != nil {
		t.Errorf("DeliveryFromContext on empty = %v, want nil", got)
	}
}

func TestSessionKeyRoundtrip(t *testing.T) {
	ctx := WithSessionKey(context.Background(), "sess-42")
	if got := SessionKeyFromContext(ctx); got != "sess-42" {
		t.Errorf("SessionKeyFromContext = %q, want %q", got, "sess-42")
	}
}

func TestSessionKeyFromContext_empty(t *testing.T) {
	if got := SessionKeyFromContext(context.Background()); got != "" {
		t.Errorf("SessionKeyFromContext on empty = %q, want empty", got)
	}
}

func TestMaxUploadBytesRoundtrip(t *testing.T) {
	ctx := WithMaxUploadBytes(context.Background(), 50*1024*1024)
	if got := MaxUploadBytesFromContext(ctx); got != 50*1024*1024 {
		t.Errorf("MaxUploadBytesFromContext = %d, want %d", got, 50*1024*1024)
	}
}

func TestMaxUploadBytesFromContext_empty(t *testing.T) {
	if got := MaxUploadBytesFromContext(context.Background()); got != 0 {
		t.Errorf("MaxUploadBytesFromContext on empty = %d, want 0", got)
	}
}

func TestToolPresetRoundtrip(t *testing.T) {
	ctx := WithToolPreset(context.Background(), "coding")
	if got := ToolPresetFromContext(ctx); got != "coding" {
		t.Errorf("ToolPresetFromContext = %q, want %q", got, "coding")
	}
}

func TestToolPreset_emptyStringNoop(t *testing.T) {
	base := context.Background()
	ctx := WithToolPreset(base, "")
	// Empty preset should return the original context unchanged.
	if got := ToolPresetFromContext(ctx); got != "" {
		t.Errorf("ToolPresetFromContext after empty preset = %q, want empty", got)
	}
}

func TestRunCacheContextRoundtrip(t *testing.T) {
	rc := NewRunCache()
	rc.Set("k", "v")
	ctx := WithRunCache(context.Background(), rc)
	got := RunCacheFromContext(ctx)
	if got == nil {
		t.Fatal("RunCacheFromContext = nil")
	}
	if v, ok := got.Get("k"); !ok || v != "v" {
		t.Errorf("Get(k) = (%q, %v), want (v, true)", v, ok)
	}
}

func TestTurnContextContextRoundtrip(t *testing.T) {
	tc := NewTurnContext()
	ctx := WithTurnContext(context.Background(), tc)
	got := TurnContextFromContext(ctx)
	if got != tc {
		t.Error("TurnContextFromContext did not return the same instance")
	}
}

// ---------------------------------------------------------------------------
// context.go — ContinuationSignal
// ---------------------------------------------------------------------------

func TestContinuationSignal_initialState(t *testing.T) {
	sig := NewContinuationSignal()
	if sig.Requested() {
		t.Error("new signal should not be requested")
	}
	if sig.Reason() != "" {
		t.Errorf("Reason = %q, want empty", sig.Reason())
	}
}

func TestContinuationSignal_request(t *testing.T) {
	sig := NewContinuationSignal()
	sig.Request("tool output too long")
	if !sig.Requested() {
		t.Error("Requested() = false after Request()")
	}
	if got := sig.Reason(); got != "tool output too long" {
		t.Errorf("Reason = %q, want %q", got, "tool output too long")
	}
}

func TestContinuationSignal_contextRoundtrip(t *testing.T) {
	sig := NewContinuationSignal()
	sig.Request("test")
	ctx := WithContinuationSignal(context.Background(), sig)
	got := ContinuationSignalFromContext(ctx)
	if got == nil || !got.Requested() {
		t.Error("ContinuationSignalFromContext did not return the requested signal")
	}
}

// ---------------------------------------------------------------------------
// context.go — SpawnFlag
// ---------------------------------------------------------------------------

func TestSpawnFlag_initialState(t *testing.T) {
	f := NewSpawnFlag()
	if f.IsSet() {
		t.Error("new SpawnFlag should not be set")
	}
}

func TestSpawnFlag_set(t *testing.T) {
	f := NewSpawnFlag()
	f.Set()
	if !f.IsSet() {
		t.Error("IsSet() = false after Set()")
	}
}

func TestSpawnFlag_contextRoundtrip(t *testing.T) {
	f := NewSpawnFlag()
	f.Set()
	ctx := WithSpawnFlag(context.Background(), f)
	got := SpawnFlagFromContext(ctx)
	if got == nil || !got.IsSet() {
		t.Error("SpawnFlagFromContext did not return the set flag")
	}
}

// ---------------------------------------------------------------------------
// context.go — DeferredActivation
// ---------------------------------------------------------------------------

func TestDeferredActivation_initiallyEmpty(t *testing.T) {
	da := NewDeferredActivation()
	if names := da.ActivatedNames(); len(names) != 0 {
		t.Errorf("ActivatedNames = %v, want empty", names)
	}
}

func TestDeferredActivation_activate(t *testing.T) {
	da := NewDeferredActivation()
	da.Activate([]string{"gmail", "calendar"})
	got := da.ActivatedNames()
	sort.Strings(got)
	want := []string{"calendar", "gmail"}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("ActivatedNames mismatch:\n  want: %v\n   got: %v", want, got)
	}
}

func TestDeferredActivation_multipleMerge(t *testing.T) {
	da := NewDeferredActivation()
	da.Activate([]string{"gmail"})
	da.Activate([]string{"calendar", "gmail"}) // duplicate
	da.Activate([]string{"exec"})
	got := da.ActivatedNames()
	sort.Strings(got)
	want := []string{"calendar", "exec", "gmail"}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("ActivatedNames mismatch:\n  want: %v\n   got: %v", want, got)
	}
}

func TestDeferredActivation_contextRoundtrip(t *testing.T) {
	da := NewDeferredActivation()
	da.Activate([]string{"fs"})
	ctx := WithDeferredActivation(context.Background(), da)
	got := DeferredActivationFromContext(ctx)
	if got == nil {
		t.Fatal("DeferredActivationFromContext = nil")
	}
	names := got.ActivatedNames()
	if len(names) != 1 || names[0] != "fs" {
		t.Errorf("ActivatedNames = %v, want [fs]", names)
	}
}

// ---------------------------------------------------------------------------
// run_cache.go
// ---------------------------------------------------------------------------

func TestRunCache_getEmpty(t *testing.T) {
	rc := NewRunCache()
	_, ok := rc.Get("missing")
	if ok {
		t.Error("Get on empty cache returned true")
	}
}

func TestRunCache_setGetRoundtrip(t *testing.T) {
	rc := NewRunCache()
	rc.Set("find:/src", "file1.go\nfile2.go")
	got, ok := rc.Get("find:/src")
	if !ok {
		t.Fatal("Get returned false after Set")
	}
	if got != "file1.go\nfile2.go" {
		t.Errorf("Get = %q, want %q", got, "file1.go\nfile2.go")
	}
}

func TestRunCache_setWithScopeGetRoundtrip(t *testing.T) {
	rc := NewRunCache()
	rc.SetWithScope("grep:pattern", "result", "/home/user/project")
	got, ok := rc.Get("grep:pattern")
	if !ok {
		t.Fatal("Get returned false after SetWithScope")
	}
	if got != "result" {
		t.Errorf("Get = %q, want %q", got, "result")
	}
}

func TestRunCache_invalidateAll(t *testing.T) {
	rc := NewRunCache()
	rc.Set("a", "1")
	rc.Set("b", "2")
	rc.SetWithScope("c", "3", "/tmp")
	rc.Invalidate()
	if rc.Len() != 0 {
		t.Errorf("Len after Invalidate = %d, want 0", rc.Len())
	}
}

func TestRunCache_invalidateByPath_removesOverlapping(t *testing.T) {
	rc := NewRunCache()
	// Scope = /home/user/project/src -> file in same dir invalidates.
	rc.SetWithScope("find:/src", "files", "/home/user/project/src")
	// Scope = /home/user/project/src/pkg -> narrower scope, file in parent /src
	// does NOT overlap (the file isn't inside src/pkg).
	rc.SetWithScope("grep:/src/pkg", "matches", "/home/user/project/src/pkg")
	// Scope = /home/user/project/docs -> unrelated directory.
	rc.SetWithScope("tree:/docs", "tree", "/home/user/project/docs")

	// Writing a file in /src (dir of main.go = /src) invalidates entries
	// whose scope contains that dir.
	rc.InvalidateByPath("/home/user/project/src/main.go")

	if _, ok := rc.Get("find:/src"); ok {
		t.Error("find:/src should be invalidated (same dir as file)")
	}
	// grep scoped to src/pkg should survive: file is in src, not in src/pkg.
	if _, ok := rc.Get("grep:/src/pkg"); !ok {
		t.Error("grep:/src/pkg should survive (narrower scope, file not inside)")
	}
	// docs-scoped entry should survive.
	if _, ok := rc.Get("tree:/docs"); !ok {
		t.Error("tree:/docs should survive invalidation")
	}

	// Now write inside src/pkg — that should invalidate the grep entry.
	rc.InvalidateByPath("/home/user/project/src/pkg/util.go")
	if _, ok := rc.Get("grep:/src/pkg"); ok {
		t.Error("grep:/src/pkg should be invalidated after file written in its scope")
	}
}

func TestRunCache_invalidateByPath_preservesNonOverlapping(t *testing.T) {
	rc := NewRunCache()
	rc.SetWithScope("a", "1", "/home/user/project/api")
	rc.SetWithScope("b", "2", "/home/user/project/web")

	rc.InvalidateByPath("/home/user/project/api/handler.go")

	if _, ok := rc.Get("a"); ok {
		t.Error("entry a should be invalidated")
	}
	if _, ok := rc.Get("b"); !ok {
		t.Error("entry b should survive (different scope)")
	}
}

func TestRunCache_invalidateByPath_unscopedEntriesRemoved(t *testing.T) {
	rc := NewRunCache()
	rc.Set("no-scope", "value") // no scope -> conservatively removed
	rc.SetWithScope("scoped", "value", "/other/path")

	rc.InvalidateByPath("/some/file.go")

	if _, ok := rc.Get("no-scope"); ok {
		t.Error("unscoped entry should be conservatively removed")
	}
	if _, ok := rc.Get("scoped"); !ok {
		t.Error("non-overlapping scoped entry should survive")
	}
}

func TestIsCacheableTool(t *testing.T) {
	cacheable := []string{"find", "tree", "grep", "analyze"}
	for _, name := range cacheable {
		if !IsCacheableTool(name) {
			t.Errorf("IsCacheableTool(%q) = false, want true", name)
		}
	}

	notCacheable := []string{"write", "exec", "edit", "git", "unknown"}
	for _, name := range notCacheable {
		if IsCacheableTool(name) {
			t.Errorf("IsCacheableTool(%q) = true, want false", name)
		}
	}
}

func TestIsMutationTool(t *testing.T) {
	mutations := []string{"write", "edit", "multi_edit", "git"}
	for _, name := range mutations {
		if !IsMutationTool(name) {
			t.Errorf("IsMutationTool(%q) = false, want true", name)
		}
	}

	nonMutations := []string{"find", "grep", "tree", "analyze", "read", "unknown"}
	for _, name := range nonMutations {
		if IsMutationTool(name) {
			t.Errorf("IsMutationTool(%q) = true, want false", name)
		}
	}
}

func TestBuildCacheKey_simple(t *testing.T) {
	input := json.RawMessage(`{"path":"/src","pattern":"*.go"}`)
	got := BuildCacheKey("find", input)
	want := `find:{"path":"/src","pattern":"*.go"}`
	if got != want {
		t.Errorf("BuildCacheKey = %q, want %q", got, want)
	}
}

func TestBuildCacheKey_stripsCompress(t *testing.T) {
	input := json.RawMessage(`{"path":"/src","compress":true}`)
	got := BuildCacheKey("find", input)
	// After stripping "compress", the key should not contain it.
	var m map[string]any
	// Parse the value portion (after "find:").
	if err := json.Unmarshal([]byte(got[len("find:"):]), &m); err != nil {
		t.Fatalf("Unmarshal cache key value: %v", err)
	}
	if _, exists := m["compress"]; exists {
		t.Error("cache key should not contain compress field")
	}
	if _, exists := m["path"]; !exists {
		t.Error("cache key should still contain path field")
	}
}

func TestBuildCacheKey_stripsRef(t *testing.T) {
	input := json.RawMessage(`{"path":"/src","$ref":"tool-abc"}`)
	got := BuildCacheKey("grep", input)
	var m map[string]any
	if err := json.Unmarshal([]byte(got[len("grep:"):]), &m); err != nil {
		t.Fatalf("Unmarshal cache key value: %v", err)
	}
	if _, exists := m["$ref"]; exists {
		t.Error("cache key should not contain $ref field")
	}
}

func TestBuildCacheKey_noStrippableFields(t *testing.T) {
	// Without compress/$ref, input is used as-is (no re-marshaling).
	input := json.RawMessage(`{"pattern":"error"}`)
	got := BuildCacheKey("grep", input)
	want := `grep:{"pattern":"error"}`
	if got != want {
		t.Errorf("BuildCacheKey = %q, want %q", got, want)
	}
}

func TestRunCache_len(t *testing.T) {
	rc := NewRunCache()
	if rc.Len() != 0 {
		t.Errorf("Len on empty = %d, want 0", rc.Len())
	}
	rc.Set("a", "1")
	rc.Set("b", "2")
	if rc.Len() != 2 {
		t.Errorf("Len = %d, want 2", rc.Len())
	}
}

// ---------------------------------------------------------------------------
// turn_context.go
// ---------------------------------------------------------------------------

func TestTurnContext_storeLoadRoundtrip(t *testing.T) {
	tc := NewTurnContext()
	result := &TurnResult{ToolName: "grep", Output: "match found", Duration: 50 * time.Millisecond}
	tc.Store("tool-1", result)

	got := tc.Load("tool-1")
	if got == nil {
		t.Fatal("Load returned nil after Store")
	}
	if !reflect.DeepEqual(result, got) {
		t.Errorf("Load mismatch:\n  want: %+v\n   got: %+v", result, got)
	}
}

func TestTurnContext_loadEmpty(t *testing.T) {
	tc := NewTurnContext()
	if got := tc.Load("nonexistent"); got != nil {
		t.Errorf("Load on empty = %v, want nil", got)
	}
}

func TestTurnContext_ids(t *testing.T) {
	tc := NewTurnContext()
	tc.Store("id-a", &TurnResult{ToolName: "find"})
	tc.Store("id-b", &TurnResult{ToolName: "grep"})

	ids := tc.IDs()
	sort.Strings(ids)
	want := []string{"id-a", "id-b"}
	if !reflect.DeepEqual(want, ids) {
		t.Errorf("IDs mismatch:\n  want: %v\n   got: %v", want, ids)
	}
}

func TestTurnContext_waitFastPath(t *testing.T) {
	tc := NewTurnContext()
	result := &TurnResult{ToolName: "read", Output: "content"}
	tc.Store("tool-fast", result)

	got, ok := tc.Wait(context.Background(), "tool-fast", time.Second)
	if !ok {
		t.Fatal("Wait returned false for already-stored result")
	}
	if got.Output != "content" {
		t.Errorf("Output = %q, want %q", got.Output, "content")
	}
}

func TestTurnContext_waitWithTimeout(t *testing.T) {
	tc := NewTurnContext()
	_, ok := tc.Wait(context.Background(), "never-stored", 10*time.Millisecond)
	if ok {
		t.Error("Wait returned true for missing result with short timeout")
	}
}

func TestTurnContext_waitContextCancellation(t *testing.T) {
	tc := NewTurnContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, ok := tc.Wait(ctx, "never-stored", 5*time.Second)
	if ok {
		t.Error("Wait returned true after context cancellation")
	}
}

func TestTurnContext_waitUnblockedByStore(t *testing.T) {
	tc := NewTurnContext()

	done := make(chan struct{})
	go func() {
		defer close(done)
		got, ok := tc.Wait(context.Background(), "delayed", 5*time.Second)
		if !ok {
			t.Error("Wait returned false")
			return
		}
		if got.Output != "delayed-result" {
			t.Errorf("Output = %q, want %q", got.Output, "delayed-result")
		}
	}()

	// Small delay to ensure the goroutine registers its waiter.
	time.Sleep(10 * time.Millisecond)
	tc.Store("delayed", &TurnResult{ToolName: "exec", Output: "delayed-result", Duration: time.Millisecond})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait was not unblocked by Store within 2s")
	}
}

func TestTurnContext_toolTiming(t *testing.T) {
	tc := NewTurnContext()
	tc.Store("t1", &TurnResult{ToolName: "grep", Duration: 100 * time.Millisecond})
	tc.Store("t2", &TurnResult{ToolName: "grep", Duration: 200 * time.Millisecond})
	tc.Store("t3", &TurnResult{ToolName: "find", Duration: 50 * time.Millisecond})

	stats, ok := tc.ToolTiming("grep")
	if !ok {
		t.Fatal("ToolTiming returned false for grep")
	}
	if stats.Count != 2 {
		t.Errorf("Count = %d, want 2", stats.Count)
	}
	if stats.Min != 100*time.Millisecond {
		t.Errorf("Min = %v, want 100ms", stats.Min)
	}
	if stats.Max != 200*time.Millisecond {
		t.Errorf("Max = %v, want 200ms", stats.Max)
	}
	wantMean := 150 * time.Millisecond
	if stats.Mean != wantMean {
		t.Errorf("Mean = %v, want %v", stats.Mean, wantMean)
	}

	// Unrecorded tool returns false.
	if _, ok := tc.ToolTiming("exec"); ok {
		t.Error("ToolTiming returned true for unrecorded tool")
	}
}

func TestTurnContext_toolTiming_zeroDurationIgnored(t *testing.T) {
	tc := NewTurnContext()
	tc.Store("t1", &TurnResult{ToolName: "grep", Duration: 0}) // zero duration -> not recorded
	if _, ok := tc.ToolTiming("grep"); ok {
		t.Error("ToolTiming should return false when only zero-duration results exist")
	}
}

// ---------------------------------------------------------------------------
// turn_context.go — DetectCycle
// ---------------------------------------------------------------------------

func TestDetectCycle_noCycle(t *testing.T) {
	refs := map[string]string{
		"a": "b",
		"b": "c",
	}
	if err := DetectCycle(refs); err != nil {
		t.Errorf("DetectCycle returned error for acyclic refs: %v", err)
	}
}

func TestDetectCycle_simpleCycle(t *testing.T) {
	refs := map[string]string{
		"a": "b",
		"b": "c",
		"c": "a",
	}
	if err := DetectCycle(refs); err == nil {
		t.Error("DetectCycle returned nil for cyclic refs")
	}
}

func TestDetectCycle_selfLoop(t *testing.T) {
	refs := map[string]string{
		"x": "x",
	}
	if err := DetectCycle(refs); err == nil {
		t.Error("DetectCycle returned nil for self-loop")
	}
}

func TestDetectCycle_emptyRefs(t *testing.T) {
	if err := DetectCycle(map[string]string{}); err != nil {
		t.Errorf("DetectCycle returned error for empty refs: %v", err)
	}
}

func TestDetectCycle_disconnectedNoCycle(t *testing.T) {
	refs := map[string]string{
		"a": "b",
		"c": "d",
	}
	if err := DetectCycle(refs); err != nil {
		t.Errorf("DetectCycle returned error for disconnected acyclic refs: %v", err)
	}
}
