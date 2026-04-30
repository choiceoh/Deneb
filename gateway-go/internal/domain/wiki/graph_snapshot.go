// graph_snapshot.go — serialize all wiki pages into a graphify-compatible
// graph.json (NetworkX node-link form). The wiki dreamer already curates a
// sparse semantic graph via Frontmatter.Related[]; this snapshotter projects
// that graph into a file the `graphify` CLI can query, cluster, and report on.
//
// No LLM calls — this is a pure projection of existing wiki state. The
// dreamer's synthesize() phase is the LLM-driven extractor; this is the
// serializer that runs after every dream cycle.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// graphifyNode mirrors the per-node shape produced by the `graphify` CLI.
// Field names match graphify's JSON output exactly (NetworkX node_link_data
// + graphify-specific fields), so cluster-only and the query/explain/path
// commands accept this file without modification.
type graphifyNode struct {
	Label          string `json:"label"`
	FileType       string `json:"file_type"`
	SourceFile     string `json:"source_file"`
	SourceLocation string `json:"source_location"`
	ID             string `json:"id"`
	Community      int    `json:"community"`
	NormLabel      string `json:"norm_label"`
}

// graphifyEdge mirrors the per-link shape produced by the `graphify` CLI.
type graphifyEdge struct {
	Relation        string  `json:"relation"`
	Confidence      string  `json:"confidence"`
	SourceFile      string  `json:"source_file"`
	SourceLocation  string  `json:"source_location"`
	Weight          float64 `json:"weight"`
	Src             string  `json:"_src"`
	Tgt             string  `json:"_tgt"`
	Source          string  `json:"source"`
	Target          string  `json:"target"`
	ConfidenceScore float64 `json:"confidence_score"`
}

// graphifyGraph is the top-level node_link_data envelope.
type graphifyGraph struct {
	Directed   bool                   `json:"directed"`
	Multigraph bool                   `json:"multigraph"`
	Graph      map[string]any         `json:"graph"`
	Nodes      []graphifyNode         `json:"nodes"`
	Links      []graphifyEdge         `json:"links"`
	Hyperedges []map[string]any       `json:"hyperedges"`
}

// SnapshotResult summarizes a graph snapshot run.
type SnapshotResult struct {
	OutDir       string
	GraphPath    string
	Nodes        int
	Edges        int
	Clustered    bool   // true iff graphify CLI was invoked successfully for cluster-only
	ClusterError string // non-empty if cluster-only was attempted but failed
}

// BuildGraphSnapshot projects every wiki page into a graphify-compatible
// graph.json. The graphify CLI expects the file at <parent>/graphify-out/
// graph.json (the `update` and `cluster-only` commands derive paths from
// that convention), so this function treats `outDir` as the *parent* and
// always writes to outDir/graphify-out/graph.json.
//
// If runCluster is true and the `graphify` CLI is on PATH, it then runs
// `graphify cluster-only graphify-out` (with cwd=outDir) to add community
// labels and a GRAPH_REPORT.md.
//
// Edges combine three sources, mirroring graphify's EXTRACTED/INFERRED
// confidence split: (1) explicit Frontmatter.Related[] (EXTRACTED), (2)
// shared tags between pages (INFERRED, weight 0.5), (3) a page's body
// mentioning another page's title or id (INFERRED, weight 0.7). The wiki
// itself is left untouched — this is a read-only snapshot.
func BuildGraphSnapshot(ctx context.Context, store *Store, outDir string, runCluster bool) (*SnapshotResult, error) {
	if store == nil {
		return nil, fmt.Errorf("graph snapshot: nil store")
	}
	if outDir == "" {
		return nil, fmt.Errorf("graph snapshot: empty outDir")
	}
	graphDir := filepath.Join(outDir, "graphify-out")
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		return nil, fmt.Errorf("graph snapshot: mkdir %s: %w", graphDir, err)
	}

	pages, err := store.ListPages("")
	if err != nil {
		return nil, fmt.Errorf("graph snapshot: list pages: %w", err)
	}

	wikiDir := store.Dir()
	graph := graphifyGraph{
		Directed:   false,
		Multigraph: false,
		Graph:      map[string]any{},
		Nodes:      make([]graphifyNode, 0, len(pages)),
		Links:      []graphifyEdge{},
		Hyperedges: []map[string]any{},
	}

	// Map relPath -> node id so edges can resolve targets even when the page
	// links by id, by path-with-suffix, or by path-without-suffix.
	pathToID := make(map[string]string, len(pages))
	titleToID := make(map[string]string, len(pages))
	usedIDs := make(map[string]int, len(pages))

	// Cache full page bodies for the mention pass.
	type pageInfo struct {
		relPath string
		page    *Page
		id      string
		title   string
	}
	infos := make([]pageInfo, 0, len(pages))

	// First pass: emit one node per page.
	for _, relPath := range pages {
		page, err := store.ReadPage(relPath)
		if err != nil {
			continue
		}
		id := pageNodeID(relPath, page)
		// Ensure node ids are unique even when two pages declare the same
		// frontmatter id or when path-derived ids collide. graphify's
		// build_from_json silently merges duplicate ids, which then trips
		// the cluster-only safety check and prevents community labels from
		// being written back.
		if n := usedIDs[id]; n > 0 {
			id = fmt.Sprintf("%s-%d", id, n+1)
		}
		usedIDs[pageNodeID(relPath, page)]++
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(relPath), ".md")
		}
		graph.Nodes = append(graph.Nodes, graphifyNode{
			Label:          title,
			FileType:       wikiFileType(page),
			SourceFile:     filepath.Join(wikiDir, relPath),
			SourceLocation: "L1",
			ID:             id,
			Community:      0, // cluster-only fills this in
			NormLabel:      strings.ToLower(title),
		})
		pathToID[relPath] = id
		pathToID[strings.TrimSuffix(relPath, ".md")] = id
		if title != "" {
			titleToID[strings.ToLower(strings.TrimSpace(title))] = id
		}
		infos = append(infos, pageInfo{relPath: relPath, page: page, id: id, title: title})
	}

	seenEdge := make(map[string]struct{}, len(pages))
	addEdge := func(srcID, tgtID, relation, confidence string, weight, score float64, sourceRel string) {
		if srcID == "" || tgtID == "" || srcID == tgtID {
			return
		}
		a, b := srcID, tgtID
		if a > b {
			a, b = b, a
		}
		key := a + "\x00" + b + "\x00" + relation
		if _, dup := seenEdge[key]; dup {
			return
		}
		seenEdge[key] = struct{}{}
		graph.Links = append(graph.Links, graphifyEdge{
			Relation:        relation,
			Confidence:      confidence,
			SourceFile:      filepath.Join(wikiDir, sourceRel),
			SourceLocation:  "L1",
			Weight:          weight,
			Src:             srcID,
			Tgt:             tgtID,
			Source:          srcID,
			Target:          tgtID,
			ConfidenceScore: score,
		})
	}

	// Pass 2a: explicit Related[] edges (EXTRACTED).
	for _, in := range infos {
		for _, rel := range in.page.Meta.Related {
			tgtID := resolveRelatedID(rel, pathToID)
			if tgtID == "" {
				// Related can also reference a title rather than a path.
				tgtID = titleToID[strings.ToLower(strings.TrimSpace(rel))]
			}
			addEdge(in.id, tgtID, "related", "EXTRACTED", 1.0, 1.0, in.relPath)
		}
	}

	// Pass 2b: shared-tag edges (INFERRED) — same tag implies a soft link.
	tagIndex := make(map[string][]string)
	for _, in := range infos {
		for _, t := range in.page.Meta.Tags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			tagIndex[t] = append(tagIndex[t], in.id)
		}
	}
	for tag, ids := range tagIndex {
		// Skip degenerate tags that span half the corpus — they add noise
		// rather than signal.
		if len(ids) < 2 || len(ids) > 12 {
			continue
		}
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				addEdge(ids[i], ids[j], "tag:"+tag, "INFERRED", 0.5, 0.5, "index.md")
			}
		}
	}

	// Pass 2c: body-mention edges (INFERRED) — a page's body mentioning
	// another page's title is a strong signal.
	for _, in := range infos {
		body := strings.ToLower(in.page.Body)
		if body == "" {
			continue
		}
		for tgtTitle, tgtID := range titleToID {
			if tgtID == in.id || len(tgtTitle) < 3 {
				continue
			}
			if strings.Contains(body, tgtTitle) {
				addEdge(in.id, tgtID, "mentions", "INFERRED", 0.7, 0.8, in.relPath)
			}
		}
	}

	graphPath := filepath.Join(graphDir, "graph.json")
	tmpPath := graphPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("graph snapshot: create %s: %w", tmpPath, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&graph); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("graph snapshot: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("graph snapshot: close: %w", err)
	}
	if err := os.Rename(tmpPath, graphPath); err != nil {
		return nil, fmt.Errorf("graph snapshot: rename: %w", err)
	}

	res := &SnapshotResult{
		OutDir:    outDir,
		GraphPath: graphPath,
		Nodes:     len(graph.Nodes),
		Edges:     len(graph.Links),
	}

	if runCluster {
		clusterCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		// graphify cluster-only resolves graph.json relative to cwd via the
		// `<path>/graphify-out/graph.json` convention. Pass "." with cwd=outDir
		// so it lands on the file we just wrote.
		cmd := exec.CommandContext(clusterCtx, "graphify", "cluster-only", ".")
		cmd.Dir = outDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.ClusterError = strings.TrimSpace(string(out))
			if res.ClusterError == "" {
				res.ClusterError = err.Error()
			}
		} else {
			res.Clustered = true
		}
	}

	return res, nil
}

// pageNodeID picks a stable id for a wiki page. Prefer the explicit
// frontmatter id (already kebab-case); fall back to the path-derived slug.
func pageNodeID(relPath string, page *Page) string {
	if page != nil && page.Meta.ID != "" {
		return page.Meta.ID
	}
	id := strings.TrimSuffix(relPath, ".md")
	id = strings.ReplaceAll(id, "/", "_")
	return id
}

// wikiFileType maps Frontmatter.Type to graphify's file_type field. Graphify
// only accepts six values: code, concept, document, image, paper, rationale.
// Wiki Type values (concept, entity, source, comparison, log) are projected
// onto that set so cluster-only doesn't reject the graph.
func wikiFileType(page *Page) string {
	if page == nil {
		return "concept"
	}
	switch page.Meta.Type {
	case "concept", "entity", "":
		return "concept"
	case "source", "paper":
		return "paper"
	case "comparison", "log", "document":
		return "document"
	default:
		return "concept"
	}
}

// graphSnapshotOutDir returns the absolute directory where the wiki graph
// snapshot lives (~/.deneb/wiki-graph). Returns ok=false when the home
// directory cannot be resolved (degenerate environments only).
func graphSnapshotOutDir() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".deneb", "wiki-graph"), true
}

// resolveRelatedID maps a Related[] entry — which can be a page id, a path
// with .md, a path without .md, or a free-form label — to a node id we
// emitted earlier. Returns "" when no match is found (the edge is dropped).
func resolveRelatedID(rel string, pathToID map[string]string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	// Strip Obsidian-style [[wikilinks]] if present.
	rel = strings.TrimPrefix(rel, "[[")
	rel = strings.TrimSuffix(rel, "]]")

	if id, ok := pathToID[rel]; ok {
		return id
	}
	if !strings.HasSuffix(rel, ".md") {
		if id, ok := pathToID[rel+".md"]; ok {
			return id
		}
	}
	if strings.HasSuffix(rel, ".md") {
		if id, ok := pathToID[strings.TrimSuffix(rel, ".md")]; ok {
			return id
		}
	}
	return ""
}
