package compactuner

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// fakeSummaries implements SummarySource.
type fakeSummaries struct{ nodes []polaris.SummaryNode }

func (f fakeSummaries) RecentSummariesAcrossSessions(int) []polaris.SummaryNode { return f.nodes }

// fakeLLM returns a fixed JSON verdict as a one-delta stream.
type fakeLLM struct {
	reply string
	calls int
}

func (f *fakeLLM) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	f.calls++
	ch := make(chan llm.StreamEvent, 1)
	payload, _ := json.Marshal(map[string]any{"delta": map[string]string{"text": f.reply}})
	ch <- llm.StreamEvent{Type: "content_block_delta", Payload: payload}
	close(ch)
	return ch, nil
}

func leaf(content string) polaris.SummaryNode {
	return polaris.SummaryNode{Level: 1, Content: content}
}

func newTask(t *testing.T, src SummarySource, llmc llmClient) (*Task, *compaction.GuidelineStore) {
	t.Helper()
	gs := compaction.NewGuidelineStore(filepath.Join(t.TempDir(), compaction.GuidelineFileName))
	return NewTask(Deps{
		Summaries:  src,
		Guidelines: gs,
		Client:     llmc,
		Model:      "lw",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), gs
}

func TestRun_MergesProposedGuidelines(t *testing.T) {
	src := fakeSummaries{nodes: []polaris.SummaryNode{
		leaf("비용 논의를 했다"), leaf("담당자와 통화"), leaf("일정 방향 정함"), leaf("결제 관련 합의"),
	}}
	llmc := &fakeLLM{reply: `{"guidelines": ["금액은 정확한 숫자와 통화로 보존하라", "사람은 실제 이름으로 보존하라"]}`}
	task, gs := newTask(t, src, llmc)

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := gs.Load()
	if len(got) != 2 || got[0] != "금액은 정확한 숫자와 통화로 보존하라" {
		t.Fatalf("guidelines not persisted: %v", got)
	}
}

func TestRun_SkipsWhenTooFewSummaries(t *testing.T) {
	src := fakeSummaries{nodes: []polaris.SummaryNode{leaf("a"), leaf("b")}} // < minSummaries
	llmc := &fakeLLM{reply: `{"guidelines": ["x를 보존하라"]}`}
	task, gs := newTask(t, src, llmc)

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if llmc.calls != 0 {
		t.Fatalf("must not call the LLM with too few summaries (calls=%d)", llmc.calls)
	}
	if got := gs.Load(); got != nil {
		t.Fatalf("no guidelines should be written, got %v", got)
	}
}

func TestRun_EmptyVerdictWritesNothing(t *testing.T) {
	src := fakeSummaries{nodes: []polaris.SummaryNode{leaf("a"), leaf("b"), leaf("c"), leaf("d")}}
	llmc := &fakeLLM{reply: `{"guidelines": []}`}
	task, gs := newTask(t, src, llmc)

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := gs.Load(); got != nil {
		t.Fatalf("empty verdict must write nothing, got %v", got)
	}
}

func TestRun_IgnoresCondensedNodes(t *testing.T) {
	// Level-2 (condensed) nodes are filtered out; only 3 leaves remain (< min).
	src := fakeSummaries{nodes: []polaris.SummaryNode{
		leaf("a"), leaf("b"), leaf("c"),
		{Level: 2, Content: "condensed"}, {Level: 2, Content: "condensed2"},
	}}
	llmc := &fakeLLM{reply: `{"guidelines": ["x를 보존하라"]}`}
	task, _ := newTask(t, src, llmc)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if llmc.calls != 0 {
		t.Fatalf("condensed nodes must not count toward the audit floor (calls=%d)", llmc.calls)
	}
}

func TestParseGuidelines(t *testing.T) {
	got := parseGuidelines(`prose before {"guidelines": ["  금액 보존  ", "", "이름 보존"]} after`)
	if len(got) != 2 || got[0] != "금액 보존" || got[1] != "이름 보존" {
		t.Fatalf("parse/trim/drop-empty wrong: %v", got)
	}
	if parseGuidelines("no json here") != nil {
		t.Fatal("non-JSON must yield nil")
	}
}
