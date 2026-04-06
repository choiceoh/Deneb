// dream-once — One-shot dreaming cycle runner for testing.
// Usage: go run ./cmd/dream-once/
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	_ "modernc.org/sqlite"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".deneb", "memory.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store, err := memory.NewStoreFromDB(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create store: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	beforeCount, _ := store.ActiveFactCount(ctx)
	fmt.Fprintf(os.Stderr, "active facts before: %d\n", beforeCount)

	// LLM client: local vLLM.
	baseURL := os.Getenv("VLLM_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8000/v1"
	}
	apiKey := os.Getenv("VLLM_API_KEY")
	if apiKey == "" {
		apiKey = "placeholder"
	}
	model := os.Getenv("VLLM_MODEL")
	if model == "" {
		model = "gemma4"
	}

	client := llm.NewClient(baseURL, apiKey)

	fmt.Fprintf(os.Stderr, "starting dreaming cycle (model=%s, no embedder)...\n", model)
	start := time.Now()

	// Run without embedder — clustering uses existing DB embeddings,
	// new consolidated facts just won't get embeddings until next cycle.
	report, err := memory.RunDreamingCycle(ctx, store, nil, client, model, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dreaming failed: %v\n", err)
		os.Exit(1)
	}

	afterCount, _ := store.ActiveFactCount(ctx)
	elapsed := time.Since(start)

	fmt.Fprintf(os.Stderr, "\n=== Dreaming Complete ===\n")
	fmt.Fprintf(os.Stderr, "duration:      %s\n", elapsed.Round(time.Second))
	fmt.Fprintf(os.Stderr, "before:        %d facts\n", beforeCount)
	fmt.Fprintf(os.Stderr, "after:         %d facts\n", afterCount)
	fmt.Fprintf(os.Stderr, "reduced:       %d facts\n", beforeCount-afterCount)
	fmt.Fprintf(os.Stderr, "verified:      %d\n", report.FactsVerified)
	fmt.Fprintf(os.Stderr, "merged:        %d\n", report.FactsMerged)
	fmt.Fprintf(os.Stderr, "expired:       %d\n", report.FactsExpired)
	fmt.Fprintf(os.Stderr, "pruned:        %d\n", report.FactsPruned)
	fmt.Fprintf(os.Stderr, "patterns:      %d\n", report.PatternsExtracted)
	if len(report.PhaseErrors) > 0 {
		fmt.Fprintf(os.Stderr, "phase errors:  %v\n", report.PhaseErrors)
	}

	j, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(j))
}
