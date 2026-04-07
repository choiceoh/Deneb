package autoresearch

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// pngMagic is the first 8 bytes of a valid PNG file.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func TestRenderChart_Empty(t *testing.T) {
	cfg := &Config{MetricName: "loss", MetricDirection: "minimize"}
	_, err := RenderChart(nil, cfg)
	if err == nil {
		t.Fatal("expected error for empty rows")
	}
}

func TestRenderChart_SingleRow(t *testing.T) {
	rows := []ResultRow{
		{Iteration: 0, Timestamp: time.Now(), Hypothesis: "baseline",
			MetricValue: 1.087, Kept: true, BestSoFar: 1.087},
	}
	cfg := &Config{MetricName: "val_bpb", MetricDirection: "minimize"}

	data, err := RenderChart(rows, cfg)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("output is not a valid PNG")
	}
	if len(data) < 100 {
		t.Fatalf("PNG too small: %d bytes", len(data))
	}
}

func TestRenderChart_MultipleRows(t *testing.T) {
	rows := []ResultRow{
		{Iteration: 0, Timestamp: time.Now(), Hypothesis: "baseline",
			MetricValue: 1.087, Kept: true, BestSoFar: 1.087},
		{Iteration: 1, Timestamp: time.Now(), Hypothesis: "increase lr",
			MetricValue: 1.042, Kept: true, BestSoFar: 1.042, DeltaFromBest: -0.045},
		{Iteration: 2, Timestamp: time.Now(), Hypothesis: "add layer norm",
			MetricValue: 1.095, Kept: false, BestSoFar: 1.042, DeltaFromBest: 0.053},
		{Iteration: 3, Timestamp: time.Now(), Hypothesis: "batch norm",
			MetricValue: 1.035, Kept: true, BestSoFar: 1.035, DeltaFromBest: -0.007},
		{Iteration: 4, Timestamp: time.Now(), Hypothesis: "dropout 0.3",
			MetricValue: 1.050, Kept: false, BestSoFar: 1.035, DeltaFromBest: 0.015},
	}
	cfg := &Config{MetricName: "val_bpb", MetricDirection: "minimize"}

	data, err := RenderChart(rows, cfg)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("output is not a valid PNG")
	}
}

func TestRenderChart_Maximize(t *testing.T) {
	rows := []ResultRow{
		{Iteration: 0, MetricValue: 50.0, Kept: true, BestSoFar: 50.0},
		{Iteration: 1, MetricValue: 55.0, Kept: true, BestSoFar: 55.0},
		{Iteration: 2, MetricValue: 48.0, Kept: false, BestSoFar: 55.0},
	}
	cfg := &Config{MetricName: "accuracy", MetricDirection: "maximize"}

	data, err := RenderChart(rows, cfg)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("output is not a valid PNG")
	}
}

func TestRenderChart_LargeIterationCount(t *testing.T) {
	var rows []ResultRow
	best := 2.0
	for i := range 50 {
		val := 2.0 - float64(i)*0.02 + float64(i%3)*0.01
		kept := val < best
		if kept {
			best = val
		}
		rows = append(rows, ResultRow{
			Iteration:   i,
			MetricValue: val,
			Kept:        kept,
			BestSoFar:   best,
		})
	}
	cfg := &Config{MetricName: "loss", MetricDirection: "minimize"}

	data, err := RenderChart(rows, cfg)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("output is not a valid PNG")
	}
}

func TestSaveChart(t *testing.T) {
	dir := t.TempDir()
	rows := []ResultRow{
		{Iteration: 0, MetricValue: 1.0, Kept: true, BestSoFar: 1.0},
		{Iteration: 1, MetricValue: 0.9, Kept: true, BestSoFar: 0.9},
	}
	cfg := &Config{MetricName: "test_metric", MetricDirection: "minimize"}

	path, err := SaveChart(dir, rows, cfg)
	testutil.NoError(t, err)

	expected := filepath.Join(dir, configDir, "chart.png")
	if path != expected {
		t.Fatalf("expected path %s, got %s", expected, path)
	}

	data, err := os.ReadFile(path)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("saved file is not a valid PNG")
	}
}

func TestRenderChart_FlatMetric(t *testing.T) {
	// All same metric value — should not panic on zero range.
	rows := []ResultRow{
		{Iteration: 0, MetricValue: 1.0, Kept: true, BestSoFar: 1.0},
		{Iteration: 1, MetricValue: 1.0, Kept: false, BestSoFar: 1.0},
		{Iteration: 2, MetricValue: 1.0, Kept: false, BestSoFar: 1.0},
	}
	cfg := &Config{MetricName: "flat", MetricDirection: "minimize"}

	data, err := RenderChart(rows, cfg)
	testutil.NoError(t, err)
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatal("output is not a valid PNG")
	}
}
