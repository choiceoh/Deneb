package chat

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// apcDiagFor runs beginAPCDiag with no registry (scrape disabled) against the
// shared snapshot store, keyed uniquely per test.
func apcDiagFor(t *testing.T, system string, msgs []llm.Message, recall string) *apcDiagRun {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return beginAPCDiag(context.Background(), runDeps{}, "test:"+t.Name(), "vllm", "deepseek-v4-flash", []byte(system), recall, msgs, logger)
}

func TestAPCDiag_Classification(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "첫 질문"),
		llm.NewTextMessage("assistant", "첫 답변"),
	}

	// Run 1: no prior snapshot.
	d := apcDiagFor(t, "sys-v1", msgs, "")
	if d.class != apcClassFirstRun {
		t.Fatalf("run1 class = %q, want %q", d.class, apcClassFirstRun)
	}
	if d.appendTokens == 0 {
		t.Error("first-run should estimate the full prompt as appended")
	}

	// Run 2: same system, history grew by an appended tail.
	grown := append(append([]llm.Message{}, msgs...),
		llm.NewTextMessage("user", "다음 질문"))
	d = apcDiagFor(t, "sys-v1", grown, "")
	if d.class != apcClassAppendOnly {
		t.Fatalf("run2 class = %q, want %q", d.class, apcClassAppendOnly)
	}
	if d.invalidTokens != 0 || d.appendTokens == 0 {
		t.Errorf("append-only: invalid=%d append=%d, want 0/>0", d.invalidTokens, d.appendTokens)
	}

	// Run 3: same system, an OLD message's bytes changed (pruning/compaction).
	mutated := append([]llm.Message{}, grown...)
	mutated[0] = llm.NewTextMessage("user", "첫 질문 (stubbed)")
	d = apcDiagFor(t, "sys-v1", mutated, "")
	if d.class != apcClassHistoryMutated {
		t.Fatalf("run3 class = %q, want %q", d.class, apcClassHistoryMutated)
	}
	if d.divergedAt != 0 {
		t.Errorf("run3 divergedAt = %d, want 0", d.divergedAt)
	}
	if d.invalidTokens == 0 {
		t.Error("history-mutated must estimate re-prefill tokens")
	}

	// Run 4: system prompt bytes changed (e.g. recall injection) — whole
	// history invalidated regardless of identical messages.
	d = apcDiagFor(t, "sys-v2-recall-added", mutated, "recall evidence")
	if d.class != apcClassSystemChanged {
		t.Fatalf("run4 class = %q, want %q", d.class, apcClassSystemChanged)
	}
	if !d.recallChanged {
		t.Error("run4 should attribute the change to recall (recallChanged=true)")
	}
	if d.invalidTokens == 0 {
		t.Error("system-changed must estimate the full message list as invalidated")
	}

	// finish() must not panic without a scrape baseline and must be idempotent.
	d.finish()
	d.finish()
}

func TestAPCDiag_CommonPrefixLen(t *testing.T) {
	cases := []struct {
		a, b []uint64
		want int
	}{
		{nil, nil, 0},
		{[]uint64{1, 2}, []uint64{1, 2}, 2},
		{[]uint64{1, 2}, []uint64{1, 2, 3}, 2},
		{[]uint64{1, 9}, []uint64{1, 2, 3}, 1},
		{[]uint64{9}, []uint64{1}, 0},
	}
	for _, c := range cases {
		if got := commonPrefixLen(c.a, c.b); got != c.want {
			t.Errorf("commonPrefixLen(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
