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

// ---------------------------------------------------------------------------
// context.go — With*/FromContext roundtrips
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// context.go — ContinuationSignal
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// context.go — SpawnFlag
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// context.go — DeferredActivation
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// run_cache.go
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// turn_context.go
// ---------------------------------------------------------------------------

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
