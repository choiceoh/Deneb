// Standalone compaction benchmark metric for autoresearch.
//
// Runs the Aurora compaction benchmark with the current DefaultSweepConfig()
// and outputs metric_value=N (0-100) for autoresearch consumption.
//
// Usage:
//
//	go run ./cmd/compaction-metric
//	# Output: metric_value=67.3412
package main

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
)

func main() {
	cfg := aurora.DefaultSweepConfig()
	score := aurora.RunCompactionBenchmark(cfg)
	fmt.Printf("metric_value=%.4f\n", score)
}
