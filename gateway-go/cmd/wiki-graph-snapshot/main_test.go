package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestDefaultSnapshotPathsStayUnderDenebHome(t *testing.T) {
	home := filepath.Join("tmp", "operator")
	in, out, diary := defaultSnapshotPaths(home)
	if in != filepath.Join(home, ".deneb", "wiki") ||
		out != filepath.Join(home, ".deneb", "wiki-graph") ||
		diary != filepath.Join(home, ".deneb", "memory", "diary") {
		t.Fatalf("paths = %q, %q, %q", in, out, diary)
	}
}

func TestPrintSnapshotResultIncludesPartialClusterFailure(t *testing.T) {
	var out bytes.Buffer
	printSnapshotResult(&out, &wiki.SnapshotResult{
		GraphPath:    "/tmp/graph.json",
		Nodes:        12,
		Edges:        19,
		Clustered:    false,
		ClusterError: "graphify unavailable",
	})
	for _, want := range []string{"graphPath: /tmp/graph.json", "nodes:     12", "edges:     19", "clustered: false", "clusterErr:graphify unavailable"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
}
