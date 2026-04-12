package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// mockEmbedder returns sequential unit vectors for deterministic testing.
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, m.dim)
		vec[0] = float32(i) / float32(len(texts))
		vec[1] = 1.0
		result[i] = vec
	}
	return result, nil
}

func TestEmbeddingCompact_SelectsSubset(t *testing.T) {
	var messages []llm.Message
	for i := range 20 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, llm.NewTextMessage(role, strings.Repeat("test message content ", 50)))
	}
	for range 12 {
		messages = append(messages, llm.NewTextMessage("user", "recent user"))
		messages = append(messages, llm.NewTextMessage("assistant", "recent assistant"))
	}

	cfg := Config{ContextBudget: 5000}
	embedder := &mockEmbedder{dim: 8}

	result, ok := EmbeddingCompact(context.Background(), cfg, messages, embedder, nil)
	if !ok {
		t.Fatal("expected embedding compaction to fire")
	}
	if len(result) >= len(messages) {
		t.Errorf("expected fewer messages, got %d (original %d)", len(result), len(messages))
	}
	first := string(result[0].Content)
	if !strings.Contains(first, "Polaris embedding compaction") {
		t.Errorf("expected MMR marker message, got: %s", first)
	}
}

func TestEmbeddingCompact_TooFewMessages(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
	}
	cfg := Config{ContextBudget: 100}
	embedder := &mockEmbedder{dim: 8}

	_, ok := EmbeddingCompact(context.Background(), cfg, messages, embedder, nil)
	if ok {
		t.Error("expected no compaction for too few messages")
	}
}

func TestCosineSim(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	c := []float32{0, 1, 0}

	if sim := cosineSim(a, b); sim < 0.99 {
		t.Errorf("identical vectors should have sim ~1, got %f", sim)
	}
	if sim := cosineSim(a, c); sim > 0.01 {
		t.Errorf("orthogonal vectors should have sim ~0, got %f", sim)
	}
}

func TestCentroid(t *testing.T) {
	vecs := [][]float32{{2, 0}, {0, 4}}
	c := centroid(vecs)
	if c[0] != 1 || c[1] != 2 {
		t.Errorf("expected [1 2], got %v", c)
	}
}

func TestMMRSelect_DiversityProperty(t *testing.T) {
	// 3 clusters: A (idx 0,1), B (idx 2,3), C (idx 4,5).
	// MMR should pick one from each cluster, not two from the same.
	embeddings := [][]float32{
		{1, 0, 0, 0},    // A
		{1, 0.01, 0, 0}, // A (near-duplicate)
		{0, 1, 0, 0},    // B
		{0, 1, 0.01, 0}, // B (near-duplicate)
		{0, 0, 1, 0},    // C
		{0, 0, 1, 0.01}, // C (near-duplicate)
	}
	query := []float32{1, 1, 1, 0} // equidistant to all clusters

	pad := strings.Repeat("padding ", 10)
	messages := []llm.Message{
		llm.NewTextMessage("user", "cluster-A-0 "+pad),
		llm.NewTextMessage("user", "cluster-A-1 "+pad),
		llm.NewTextMessage("user", "cluster-B-0 "+pad),
		llm.NewTextMessage("user", "cluster-B-1 "+pad),
		llm.NewTextMessage("user", "cluster-C-0 "+pad),
		llm.NewTextMessage("user", "cluster-C-1 "+pad),
	}

	singleMsgTokens := EstimateTokens(string(messages[0].Content)) + 4
	budget := singleMsgTokens * 3

	selected := mmrSelect(embeddings, query, messages, budget)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selections with budget for 3, got %d", len(selected))
	}

	hasA, hasB, hasC := false, false, false
	for _, msg := range selected {
		content := string(msg.Content)
		if strings.Contains(content, "cluster-A") {
			hasA = true
		}
		if strings.Contains(content, "cluster-B") {
			hasB = true
		}
		if strings.Contains(content, "cluster-C") {
			hasC = true
		}
	}
	if !hasA || !hasB || !hasC {
		t.Errorf("expected one from each cluster, got A=%v B=%v C=%v", hasA, hasB, hasC)
	}
}

func TestMMRSelect_RelevanceProperty(t *testing.T) {
	// idx 2 is the only one matching query; rest are orthogonal.
	embeddings := [][]float32{
		{0, 0, 1, 0},
		{0, 1, 0, 0},
		{1, 0, 0, 0}, // matches query
		{0, 0, 0, 1},
	}
	query := []float32{1, 0, 0, 0}

	messages := []llm.Message{
		llm.NewTextMessage("user", "irrelevant-0"),
		llm.NewTextMessage("user", "irrelevant-1"),
		llm.NewTextMessage("user", "relevant"),
		llm.NewTextMessage("user", "irrelevant-3"),
	}

	// Tight budget: only 1 message.
	selected := mmrSelect(embeddings, query, messages, 10)
	if len(selected) != 1 {
		t.Fatalf("expected 1 selection, got %d", len(selected))
	}
	if content := string(selected[0].Content); !strings.Contains(content, "relevant") || strings.Contains(content, "irrelevant") {
		t.Errorf("expected the relevant message, got: %s", content)
	}
}

func TestMMRSelect_EmptySelected_NoPenalty(t *testing.T) {
	// First pick should be purely by relevance.
	embeddings := [][]float32{
		{0.5, 0.5, 0, 0},
		{1, 0, 0, 0}, // highest relevance to query
	}
	query := []float32{1, 0, 0, 0}

	messages := []llm.Message{
		llm.NewTextMessage("user", "half-relevant"),
		llm.NewTextMessage("user", "most-relevant"),
	}

	selected := mmrSelect(embeddings, query, messages, 5)
	if len(selected) != 1 {
		t.Fatalf("expected 1 selection, got %d", len(selected))
	}
	if content := string(selected[0].Content); !strings.Contains(content, "most-relevant") {
		t.Errorf("expected most-relevant message, got: %s", content)
	}
}

func TestRecencyCompact_DropsOldest(t *testing.T) {
	var messages []llm.Message
	for range 30 {
		messages = append(messages, llm.NewTextMessage("user", strings.Repeat("content ", 100)))
		messages = append(messages, llm.NewTextMessage("assistant", strings.Repeat("response ", 100)))
	}

	cfg := Config{ContextBudget: 5000}
	result, ok := RecencyCompact(cfg, messages, nil)
	if !ok {
		t.Fatal("expected recency compaction to fire")
	}
	if len(result) >= len(messages) {
		t.Errorf("expected fewer messages, got %d (original %d)", len(result), len(messages))
	}
}

func TestRecencyCompact_NoopUnderBudget(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
	}
	cfg := Config{ContextBudget: 100000}
	_, ok := RecencyCompact(cfg, messages, nil)
	if ok {
		t.Error("expected no compaction under budget")
	}
}
