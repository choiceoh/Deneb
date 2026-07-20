package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func fakeReadFileMap(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if c, ok := files[p]; ok {
			return []byte(c), nil
		}
		return nil, os.ErrNotExist
	}
}

func TestEnrichWithFolderDocsResolvesNearestThenRoot(t *testing.T) {
	root := "/repo"
	files := map[string]string{
		filepath.Join(root, "gateway-go/internal/runtime/CLAUDE.md"): "runtime map: RPC server.",
		filepath.Join(root, "CLAUDE.md"):                             "ROOT project map.",
	}
	// externalmcp/ and runtime/server/ have no CLAUDE.md → both resolve to
	// runtime/CLAUDE.md (nearest ancestor, deduped). scripts/deploy has no
	// subtree map → falls back to the repo-root CLAUDE.md (not in the prompt).
	out := "`gateway-go/internal/runtime/externalmcp/mcp_external_tools.go:166`, " +
		"gateway-go/internal/runtime/server/server.go:10, scripts/deploy/deploy_helper.py:3"
	got := enrichWithFolderDocs(out, root, fakeReadFileMap(files))

	if !strings.Contains(got, "## 폴더 맥락") || !strings.Contains(got, "runtime map") {
		t.Fatalf("missing nearest-ancestor folder map: %q", got)
	}
	if n := strings.Count(got, "gateway-go/internal/runtime/CLAUDE.md"); n != 1 {
		t.Fatalf("nearest-ancestor + dedup should attach runtime map once, got %d", n)
	}
	if !strings.Contains(got, "ROOT project map") {
		t.Fatal("a map-less area (scripts/) should fall back to the root CLAUDE.md")
	}
}

func TestEnrichWithFolderDocsSkipsPathTraversal(t *testing.T) {
	root := "/repo"
	files := map[string]string{filepath.Join(root, "CLAUDE.md"): "ROOT"}
	// A `..` segment from (untrusted) tool output must never be joined+read.
	got := enrichWithFolderDocs("see `gateway-go/../../etc/evil.go:1`", root, fakeReadFileMap(files))
	if strings.Contains(got, "폴더 맥락") {
		t.Fatalf("a `..` path must be skipped (traversal), got %q", got)
	}
}

func TestCapHeadStaysValidUTF8(t *testing.T) {
	// A long Korean line (3 bytes/rune) with NO newline forces the rune-boundary
	// back-off; the truncated result must remain valid UTF-8.
	s := strings.Repeat("가나다", 500) // ~4500 bytes, no '\n'
	got := capHead(s, perFolderDocCap)
	if !utf8.ValidString(got) {
		t.Fatal("capHead produced invalid UTF-8 (cut mid-rune)")
	}
	if !strings.Contains(got, "생략") {
		t.Fatal("over-cap content should note the truncation")
	}
}

func TestTuneCodegraphHandlesUnprefixedToolNames(t *testing.T) {
	// A server advertising plain "explore"/"node" (no self-prefix) must still be
	// rerouted, dialing the sibling "node" (not the codegraph_-prefixed name).
	call, lastName, _ := fakeCall("**Sym** (struct)\n")
	base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE", nil }
	fn := tuneCodegraphTool("explore", base, call)
	out, err := fn(context.Background(), exploreInput("GatewayHub"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if *lastName != "node" {
		t.Fatalf("unprefixed explore should reroute to \"node\", got %q", *lastName)
	}
	if strings.Contains(out, "EXPLORE") {
		t.Fatal("reroute should have replaced explore output with node")
	}
}

func TestEnrichWithFolderDocsCapsFolderCount(t *testing.T) {
	root := "/repo"
	files := map[string]string{}
	var out strings.Builder
	for _, d := range []string{"a", "b", "c", "d", "e"} {
		files[filepath.Join(root, "pkg/"+d+"/CLAUDE.md")] = "map " + d
		fmt.Fprintf(&out, "pkg/%s/x.go:1 ", d)
	}
	got := enrichWithFolderDocs(out.String(), root, fakeReadFileMap(files))
	if n := strings.Count(got, "\n### "); n != maxFolderDocs {
		t.Fatalf("expected at most %d folder docs, got %d", maxFolderDocs, n)
	}
}

func TestEnrichWithFolderDocsTruncatesLongMap(t *testing.T) {
	root := "/repo"
	long := strings.Repeat("한 줄의 맵 내용\n", 300) // well over perFolderDocCap
	files := map[string]string{filepath.Join(root, "pkg/big/CLAUDE.md"): long}
	got := enrichWithFolderDocs("pkg/big/x.go:1", root, fakeReadFileMap(files))
	if !strings.Contains(got, "생략") {
		t.Fatalf("long map should be head-truncated with a note: %q", got[:min(len(got), 200)])
	}
}

func TestEnrichWithFolderDocsSelectsQuerySectionAndSafety(t *testing.T) {
	root := "/repo"
	doc := "# Runtime\n\n## Safety\nNever trust an escaped path.\n\n## Prompt cache\nInject recall at the user-message tail.\n\n## Release\nPublish an APK.\n"
	files := map[string]string{filepath.Join(root, "gateway-go/CLAUDE.md"): doc}
	got := enrichWithFolderDocsQuery(
		"gateway-go/internal/pipeline/chat/run_tail_inject.go:1",
		root,
		"prompt cache recall injection",
		fakeReadFileMap(files),
	)
	for _, want := range []string{"## Safety", "## Prompt cache", "user-message tail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("query-aware doc context missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Publish an APK") {
		t.Fatalf("unrelated section leaked into context: %s", got)
	}
}

func TestEnrichWithFolderDocsNoPathsUnchanged(t *testing.T) {
	in := "prose with no file paths, just words"
	got := enrichWithFolderDocs(in, "/repo", func(string) ([]byte, error) { return nil, os.ErrNotExist })
	if got != in {
		t.Fatalf("output with no source paths must be unchanged, got %q", got)
	}
}

// fakeCall records the last remote tool + args and returns canned output. When
// nodeOut contains codegraphNodeMiss the reroute treats it as a miss.
func fakeCall(nodeOut string) (fn func(context.Context, string, json.RawMessage) (string, error), last *string, lastArgs *json.RawMessage) {
	var name string
	var args json.RawMessage
	f := func(_ context.Context, n string, a json.RawMessage) (string, error) {
		name, args = n, a
		return nodeOut, nil
	}
	return f, &name, &args
}

func exploreInput(q string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"query": q})
	return b
}

func TestTuneCodegraphExploreReroutesSpecificSymbolToNode(t *testing.T) {
	call, lastName, lastArgs := fakeCall("**GatewayHub** (struct)\n\n**Members (25):**\n")
	base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
	fn := tuneCodegraphTool(codegraphExploreTool, base, call)

	out, err := fn(context.Background(), exploreInput("GatewayHub"))
	if err != nil {
		t.Fatalf("reroute err: %v", err)
	}
	if *lastName != codegraphNodeTool {
		t.Fatalf("expected reroute to %s, got %q", codegraphNodeTool, *lastName)
	}
	var na struct {
		Symbol      string `json:"symbol"`
		IncludeCode bool   `json:"includeCode"`
	}
	if err := json.Unmarshal(*lastArgs, &na); err != nil || na.Symbol != "GatewayHub" || !na.IncludeCode {
		t.Fatalf("node args = %s (want symbol=GatewayHub includeCode=true)", *lastArgs)
	}
	if strings.Contains(out, "EXPLORE_FALLBACK") {
		t.Fatal("reroute must not fall through to explore on a node hit")
	}
	if !strings.Contains(out, "GatewayHub") || !strings.Contains(out, "단일 심볼") {
		t.Fatalf("reroute output missing node body or note: %q", out)
	}
}

func TestTuneCodegraphExploreFallsBackOnNodeMiss(t *testing.T) {
	call, lastName, _ := fakeCall(`Symbol "GatewayHub" not found in the codebase`)
	base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
	fn := tuneCodegraphTool(codegraphExploreTool, base, call)

	out, err := fn(context.Background(), exploreInput("Zzznotasymbol"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// node was tried (it's a specific-looking symbol) but missed → explore wins.
	if *lastName != codegraphNodeTool {
		t.Fatalf("expected node attempt, got %q", *lastName)
	}
	if out != "EXPLORE_FALLBACK" {
		t.Fatalf("node miss must fall back to explore, got %q", out)
	}
}

func TestTuneCodegraphExplorePassesThroughNonSymbolQueries(t *testing.T) {
	cases := map[string]string{
		"multi-token flow": "where chat turn execution happens",
		"two symbols":      "GatewayHub Broadcaster",
		"lowercase area":   "auth", // names a symbol maybe, but area intent → keep explore
		"dotted method":    "miniapp.people.list",
		"too short":        "Go",
	}
	for label, q := range cases {
		t.Run(label, func(t *testing.T) {
			call, lastName, _ := fakeCall("NODE_SHOULD_NOT_RUN")
			base := func(context.Context, json.RawMessage) (string, error) { return "EXPLORE_FALLBACK", nil }
			fn := tuneCodegraphTool(codegraphExploreTool, base, call)
			out, err := fn(context.Background(), exploreInput(q))
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if *lastName != "" {
				t.Fatalf("%q must not reroute (node called %q)", q, *lastName)
			}
			if out != "EXPLORE_FALLBACK" {
				t.Fatalf("%q must pass through to explore, got %q", q, out)
			}
		})
	}
}

func TestTuneCodegraphLeavesOtherToolsUntouched(t *testing.T) {
	call, _, _ := fakeCall("x")
	base := func(context.Context, json.RawMessage) (string, error) { return "BASE", nil }
	for _, tool := range []string{"codegraph_node", "codegraph_callers", "codegraph_impact", "codegraph_search"} {
		fn := tuneCodegraphTool(tool, base, call)
		out, _ := fn(context.Background(), exploreInput("GatewayHub"))
		if out != "BASE" {
			t.Fatalf("%s must be passthrough, got %q", tool, out)
		}
	}
}
