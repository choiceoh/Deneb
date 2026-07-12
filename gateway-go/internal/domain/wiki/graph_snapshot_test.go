package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestBuildGraphSnapshotProjectsEachEdgeSource(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })

	writeGraphFixturePage(t, store, "기술/alpha.md", "alpha", "Alpha Page",
		[]string{"shared"}, []string{"기술/beta.md"},
		"See [[기술/gamma]] and Delta Report.")
	writeGraphFixturePage(t, store, "기술/beta.md", "beta", "Beta Page",
		[]string{"shared"}, nil, "No cross reference here.")
	writeGraphFixturePage(t, store, "기술/gamma.md", "gamma", "Gamma Page",
		nil, nil, "Standalone page.")
	writeGraphFixturePage(t, store, "기술/delta.md", "delta", "Delta Report",
		nil, nil, "Standalone page.")

	outDir := filepath.Join(dir, "snapshot")
	result, err := BuildGraphSnapshot(t.Context(), store, outDir, false)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot: %v", err)
	}
	if result.Nodes != 4 || result.Edges != 4 {
		t.Fatalf("snapshot size = %d nodes, %d edges; want 4, 4", result.Nodes, result.Edges)
	}
	if result.GraphPath != filepath.Join(outDir, "graphify-out", "graph.json") {
		t.Fatalf("GraphPath = %q", result.GraphPath)
	}
	if result.Clustered || result.ClusterError != "" {
		t.Fatalf("clustering ran despite runCluster=false: %+v", result)
	}

	data := testutil.Must(os.ReadFile(result.GraphPath))
	var graph graphifyGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("decode graph.json: %v", err)
	}
	if graph.Directed || graph.Multigraph || len(graph.Nodes) != 4 {
		t.Fatalf("unexpected graph envelope: %+v", graph)
	}

	wantEdges := map[string]bool{
		"alpha|beta|related":    false,
		"alpha|beta|tag:shared": false,
		"alpha|gamma|link":      false,
		"alpha|delta|mentions":  false,
	}
	for _, edge := range graph.Links {
		left, right := edge.Source, edge.Target
		if left > right {
			left, right = right, left
		}
		key := left + "|" + right + "|" + edge.Relation
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
		}
		if edge.Source != edge.Src || edge.Target != edge.Tgt {
			t.Errorf("graphify source aliases diverged for %+v", edge)
		}
	}
	for edge, found := range wantEdges {
		if !found {
			t.Errorf("missing projected edge %s; links=%+v", edge, graph.Links)
		}
	}
	if _, err := os.Stat(result.GraphPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary graph file remained after atomic rename: %v", err)
	}
}

func TestBuildGraphSnapshotRejectsInvalidDestination(t *testing.T) {
	if _, err := BuildGraphSnapshot(t.Context(), nil, t.TempDir(), false); err == nil {
		t.Fatal("nil store should fail")
	}
	store := testutil.Must(NewStore(filepath.Join(t.TempDir(), "wiki"), ""))
	t.Cleanup(func() { _ = store.Close() })
	if _, err := BuildGraphSnapshot(t.Context(), store, "", false); err == nil {
		t.Fatal("empty output directory should fail")
	}
}

func TestBuildGraphSnapshotKeepsDuplicateDeclaredIDsDistinct(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { _ = store.Close() })
	writeGraphFixturePage(t, store, "기술/first.md", "duplicate", "First", nil, nil, "")
	writeGraphFixturePage(t, store, "기술/second.md", "duplicate", "Second", nil, nil, "")

	result, err := BuildGraphSnapshot(t.Context(), store, filepath.Join(dir, "snapshot"), false)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot: %v", err)
	}
	data := testutil.Must(os.ReadFile(result.GraphPath))
	var graph graphifyGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("decode graph.json: %v", err)
	}
	ids := map[string]bool{}
	for _, node := range graph.Nodes {
		ids[node.ID] = true
	}
	if !ids["duplicate"] || !ids["duplicate-2"] || len(ids) != 2 {
		t.Fatalf("duplicate ids were not disambiguated: %v", ids)
	}
}

func writeGraphFixturePage(t *testing.T, store *Store, path, id, title string, tags, related []string, body string) {
	t.Helper()
	page := NewPage(title, "기술", tags)
	page.Meta.ID = id
	page.Meta.Related = related
	page.Body = body
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("WritePage(%s): %v", path, err)
	}
}
