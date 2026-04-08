package chat

import (
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
		t.Fatalf("got %d, want 0 entries after Invalidate", rc.Len())
	}
}

func TestRunCache_InvalidateByPath(t *testing.T) {
	rc := NewRunCache()

	// Scoped entries in different subtrees.
	rc.SetWithScope("grep:a", "match in src", "src")
	rc.SetWithScope("find:b", "files in core", "core-rs/core")
	rc.SetWithScope("tree:c", "tree of src/chat", "src/chat")

	// Mutate a file under src/chat — should invalidate src and src/chat scopes.
	rc.InvalidateByPath("src/chat/tools.go")

	if _, ok := rc.Get("grep:a"); ok {
		t.Fatal("grep:a (scope=src) should be invalidated — mutated file is under src/")
	}
	if _, ok := rc.Get("tree:c"); ok {
		t.Fatal("tree:c (scope=src/chat) should be invalidated — mutated file is in src/chat/")
	}
	// core-rs is a different subtree — should survive.
	if _, ok := rc.Get("find:b"); !ok {
		t.Fatal("find:b (scope=core-rs/core) should survive — different subtree")
	}
	if rc.Len() != 1 {
		t.Fatalf("got %d, want 1 surviving entry", rc.Len())
	}
}


func TestRunCache_InvalidateByPath_WorkspaceScope(t *testing.T) {
	rc := NewRunCache()

	// "." scope means workspace-wide — always affected.
	rc.SetWithScope("grep:ws", "workspace grep", ".")
	rc.SetWithScope("find:sub", "sub dir find", "sub/dir")

	rc.InvalidateByPath("anywhere/file.go")

	if _, ok := rc.Get("grep:ws"); ok {
		t.Fatal("workspace-scoped entry should always be invalidated")
	}
	if _, ok := rc.Get("find:sub"); !ok {
		t.Fatal("sub/dir scoped entry should survive — different subtree")
	}
}

func TestRunCache_ConcurrentAccess(t *testing.T) {
	rc := NewRunCache()
	var wg sync.WaitGroup
	const n = 100

	// Concurrent writes (mixed Set and SetWithScope).
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "find:" + string(rune('a'+i%26))
			if i%2 == 0 {
				rc.SetWithScope(key, "result", "src")
			} else {
				rc.Set(key, "result")
			}
		}(i)
	}

	// Concurrent reads.
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "find:" + string(rune('a'+i%26))
			rc.Get(key)
		}(i)
	}

	// Concurrent invalidations (full and path-scoped).
	wg.Add(1)
	go func() {
		defer wg.Done()
		rc.Invalidate()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		rc.InvalidateByPath("src/foo.go")
	}()

	wg.Wait()
}

func TestBuildCacheKey_Canonical(t *testing.T) {
	// Same fields, same JSON ordering → same key.
	input1 := json.RawMessage(`{"pattern":"*.go","path":"src"}`)
	input2 := json.RawMessage(`{"pattern":"*.go","path":"src"}`)

	key1 := BuildCacheKey("find", input1)
	key2 := BuildCacheKey("find", input2)

	if key1 != key2 {
		t.Fatalf("expected canonical keys to match:\n  key1=%q\n  key2=%q", key1, key2)
	}

	// Different JSON key ordering → different keys (BuildCacheKey uses raw JSON).
	input3 := json.RawMessage(`{"path":"src","pattern":"*.go"}`)
	key3 := BuildCacheKey("find", input3)
	if key1 == key3 {
		t.Fatal("different JSON key order should produce different cache keys")
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
	if !IsCacheableTool("grep") {
		t.Fatal("grep should be cacheable")
	}
	if IsCacheableTool("read") {
		t.Fatal("read should not be cacheable")
	}
}

func TestIsMutationTool(t *testing.T) {
	for _, name := range []string{"write", "edit", "multi_edit", "git"} {
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

