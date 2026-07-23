package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestBuildGraphSnapshotCreatesEdgeForEachSource(t *testing.T) {
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

func TestBuildGraphSnapshotPreservesDuplicateDeclaredIDsDistinct(t *testing.T) {
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

// TestBuildGraphSnapshotProjectsProvenance verifies the graph carries the
// provenance/temporal attributes the pages hold — the "citation needed" answer:
// a node cites its source episode (real source_location, not the old "L1"
// stub), its evidence confidence, its resource, its validity window, and a
// resolved successor id once superseded.
func TestBuildGraphSnapshotProjectsProvenance(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { _ = store.Close() })

	old := NewPage("Old Price", "업무", nil)
	old.Meta.ID = "old-price"
	old.Meta.Sources = []string{"d2026-07-01#aaaa1111"}
	old.Meta.Confidence = "medium"
	old.Meta.Resource = "gmail:thread-123"
	old.Meta.Updated = "2026-07-05"
	old.Meta.SupersededBy = "업무/new-price.md"
	old.Body = "Superseded facts."
	if err := store.WritePage("업무/old-price.md", old); err != nil {
		t.Fatalf("WritePage(old): %v", err)
	}

	cur := NewPage("New Price", "업무", nil)
	cur.Meta.ID = "new-price"
	cur.Meta.Sources = []string{"d2026-07-08#bbbb2222", "d2026-07-20#cccc3333"}
	cur.Meta.Confidence = "high"
	cur.Body = "Current facts."
	if err := store.WritePage("업무/new-price.md", cur); err != nil {
		t.Fatalf("WritePage(cur): %v", err)
	}

	result, err := BuildGraphSnapshot(t.Context(), store, filepath.Join(dir, "snapshot"), false)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot: %v", err)
	}
	data := testutil.Must(os.ReadFile(result.GraphPath))
	var graph graphifyGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("decode graph.json: %v", err)
	}
	nodes := map[string]graphifyNode{}
	for _, n := range graph.Nodes {
		nodes[n.ID] = n
	}

	curNode, ok := nodes["new-price"]
	if !ok {
		t.Fatalf("current node missing; nodes=%v", nodes)
	}
	if len(curNode.Provenance) != 2 || curNode.Provenance[1] != "d2026-07-20#cccc3333" {
		t.Errorf("provenance not projected: %v", curNode.Provenance)
	}
	// Episode refs live in Provenance; source_location stays graphify's clickable
	// line-anchor form so query/explain citations don't break.
	if curNode.SourceLocation != "L1" {
		t.Errorf("source_location = %q; want L1 line anchor", curNode.SourceLocation)
	}
	if curNode.Confidence != "high" {
		t.Errorf("confidence = %q; want high", curNode.Confidence)
	}
	if curNode.ValidAt == "" || curNode.UpdatedAt == "" {
		t.Errorf("validity window missing: valid_at=%q updated_at=%q", curNode.ValidAt, curNode.UpdatedAt)
	}
	if curNode.InvalidAt != "" {
		t.Errorf("current node should not carry invalid_at, got %q", curNode.InvalidAt)
	}

	oldNode := nodes["old-price"]
	if oldNode.Resource != "gmail:thread-123" {
		t.Errorf("resource not projected: %q", oldNode.Resource)
	}
	if oldNode.InvalidAt != "2026-07-05" {
		t.Errorf("superseded node invalid_at = %q; want its last-write date", oldNode.InvalidAt)
	}
	// superseded_by must resolve from the raw relPath to the successor node id.
	if oldNode.SupersededBy != "new-price" {
		t.Errorf("superseded_by = %q; want resolved node id new-price", oldNode.SupersededBy)
	}
	// A traversable edge must connect the stale page to its replacement — a node
	// attribute alone is not on any link graphify's query/path walks.
	edgeFound := false
	for _, e := range graph.Links {
		if e.Relation != "superseded_by" {
			continue
		}
		if (e.Source == "old-price" && e.Target == "new-price") ||
			(e.Source == "new-price" && e.Target == "old-price") {
			edgeFound = true
		}
	}
	if !edgeFound {
		t.Errorf("no superseded_by edge between old-price and new-price; links=%+v", graph.Links)
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
