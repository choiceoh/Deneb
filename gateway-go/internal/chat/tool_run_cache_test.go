package chat

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestRunCache_GetSet(t *testing.T) {
	rc := NewRunCache()

	// Miss on empty cache.
	if _, ok := rc.Get("find:{}"); ok {
		t.Fatal("expected miss on empty cache")
	}

	// Store and retrieve.
	rc.Set("find:{}", "file1.go\nfile2.go")
	got, ok := rc.Get("find:{}")
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if got != "file1.go\nfile2.go" {
		t.Fatalf("got %q, want %q", got, "file1.go\nfile2.go")
	}
}

func TestRunCache_Invalidate(t *testing.T) {
	rc := NewRunCache()
	rc.Set("find:{}", "file1.go")
	rc.Set("tree:{}", "dir tree output")

	rc.Invalidate()

	if _, ok := rc.Get("find:{}"); ok {
		t.Fatal("expected miss after Invalidate")
	}
	if _, ok := rc.Get("tree:{}"); ok {
		t.Fatal("expected miss after Invalidate")
	}
	if rc.Len() != 0 {
		t.Fatalf("expected 0 entries after Invalidate, got %d", rc.Len())
	}
}

func TestRunCache_ConcurrentAccess(t *testing.T) {
	rc := NewRunCache()
	var wg sync.WaitGroup
	const n = 100

	// Concurrent writes.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "find:" + string(rune('a'+i%26))
			rc.Set(key, "result")
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "find:" + string(rune('a'+i%26))
			rc.Get(key)
		}(i)
	}

	// Concurrent invalidate.
	wg.Add(1)
	go func() {
		defer wg.Done()
		rc.Invalidate()
	}()

	wg.Wait()
}

func TestBuildCacheKey_Canonical(t *testing.T) {
	// Same fields, different JSON ordering → same key.
	input1 := json.RawMessage(`{"pattern":"*.go","path":"src"}`)
	input2 := json.RawMessage(`{"path":"src","pattern":"*.go"}`)

	key1 := BuildCacheKey("find", input1)
	key2 := BuildCacheKey("find", input2)

	if key1 != key2 {
		t.Fatalf("expected canonical keys to match:\n  key1=%q\n  key2=%q", key1, key2)
	}
}

func TestBuildCacheKey_StripsNonSemantic(t *testing.T) {
	// "compress" and "$ref" should be stripped.
	plain := json.RawMessage(`{"pattern":"*.go"}`)
	withCompress := json.RawMessage(`{"pattern":"*.go","compress":true}`)
	withRef := json.RawMessage(`{"pattern":"*.go","$ref":"tool_123"}`)

	keyPlain := BuildCacheKey("find", plain)
	keyCompress := BuildCacheKey("find", withCompress)
	keyRef := BuildCacheKey("find", withRef)

	if keyPlain != keyCompress {
		t.Fatalf("compress should be stripped:\n  plain=%q\n  compress=%q", keyPlain, keyCompress)
	}
	if keyPlain != keyRef {
		t.Fatalf("$ref should be stripped:\n  plain=%q\n  ref=%q", keyPlain, keyRef)
	}
}

func TestBuildCacheKey_DifferentTools(t *testing.T) {
	input := json.RawMessage(`{"pattern":"*.go"}`)
	findKey := BuildCacheKey("find", input)
	treeKey := BuildCacheKey("tree", input)

	if findKey == treeKey {
		t.Fatal("different tool names should produce different keys")
	}
}

func TestIsCacheableTool(t *testing.T) {
	if !IsCacheableTool("find") {
		t.Fatal("find should be cacheable")
	}
	if !IsCacheableTool("tree") {
		t.Fatal("tree should be cacheable")
	}
	if IsCacheableTool("grep") {
		t.Fatal("grep should not be cacheable")
	}
	if IsCacheableTool("read") {
		t.Fatal("read should not be cacheable")
	}
}

func TestIsMutationTool(t *testing.T) {
	for _, name := range []string{"write", "edit", "multi_edit", "apply_patch", "git"} {
		if !IsMutationTool(name) {
			t.Fatalf("%s should be a mutation tool", name)
		}
	}
	// exec is excluded from mutation tools: most exec calls are read-only
	// (cat, ls, curl) and blanket invalidation destroys cache hit rates.
	if IsMutationTool("exec") {
		t.Fatal("exec should not be a mutation tool")
	}
	if IsMutationTool("find") {
		t.Fatal("find should not be a mutation tool")
	}
	if IsMutationTool("read") {
		t.Fatal("read should not be a mutation tool")
	}
}

func TestRunCacheFromContext(t *testing.T) {
	// Nil when not set.
	ctx := context.Background()
	if rc := RunCacheFromContext(ctx); rc != nil {
		t.Fatal("expected nil RunCache from bare context")
	}

	// Non-nil when set.
	rc := NewRunCache()
	ctx = WithRunCache(ctx, rc)
	got := RunCacheFromContext(ctx)
	if got != rc {
		t.Fatal("expected to get back the same RunCache")
	}
}
