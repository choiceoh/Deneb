// wiki-graph-snapshot — one-shot CLI that projects ~/.deneb/wiki/ into a
// graphify-compatible graph.json. Used as a manual rebuild + PoC tool. The
// dreamer runs the same projection automatically each cycle; this CLI just
// gives you a way to trigger it without waiting.
//
// Usage:
//   wiki-graph-snapshot                 # default in/out: ~/.deneb/wiki → ~/.deneb/wiki-graph
//   wiki-graph-snapshot --no-cluster    # skip the graphify cluster-only step
//   wiki-graph-snapshot --in <dir> --out <dir>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve home: %v\n", err)
		os.Exit(1)
	}
	defaultIn := filepath.Join(home, ".deneb", "wiki")
	defaultOut := filepath.Join(home, ".deneb", "wiki-graph")
	defaultDiary := filepath.Join(home, ".deneb", "memory", "diary")

	in := flag.String("in", defaultIn, "wiki directory")
	out := flag.String("out", defaultOut, "output directory for graph.json + GRAPH_REPORT.md")
	diary := flag.String("diary", defaultDiary, "diary directory (only used so the wiki Store opens cleanly)")
	noCluster := flag.Bool("no-cluster", false, "skip `graphify cluster-only` step")
	flag.Parse()

	store, err := wiki.NewStore(*in, *diary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open wiki store at %s: %v\n", *in, err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := wiki.BuildGraphSnapshot(ctx, store, *out, !*noCluster)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("graphPath: %s\n", res.GraphPath)
	fmt.Printf("nodes:     %d\n", res.Nodes)
	fmt.Printf("edges:     %d\n", res.Edges)
	fmt.Printf("clustered: %v\n", res.Clustered)
	if res.ClusterError != "" {
		fmt.Printf("clusterErr:%s\n", res.ClusterError)
	}

	if info, err := os.Stat(res.GraphPath); err == nil {
		fmt.Printf("size:      %d bytes (%.1f KB)\n", info.Size(), float64(info.Size())/1024)
	}
}
