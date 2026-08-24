package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// overflowOutput builds grep output where the query-relevant match sits PAST
// the positional cap — the exact line the old first-N cut always dropped.
func overflowOutput(total int, needleAt int, needle string) string {
	lines := make([]string, total)
	for i := range lines {
		lines[i] = fmt.Sprintf("pkg/noise/file%d.go:%d: filler match line", i, i)
	}
	lines[needleAt] = fmt.Sprintf("pkg/core/target.go:%d: %s", needleAt, needle)
	return strings.Join(lines, "\n")
}

func TestGrepOverflowKeepsQueryRelevantTailMatch(t *testing.T) {
	needle := "retryLedger compaction window"
	out := overflowOutput(grepMaxMatches+150, grepMaxMatches+90, needle)
	ctx := toolport.WithTurnQuery(context.Background(), "retryLedger 압축 창이 어디서 결정되는지 찾아줘")

	got := GrepResultRelevanceSummarizer(ctx, "grep", out)
	if !strings.Contains(got, needle) {
		t.Fatal("query-relevant match past the positional cap was dropped")
	}
	if !strings.Contains(got, "most relevant") {
		t.Errorf("relevance-kept output must say so in the marker: %q", got[len(got)-160:])
	}
	// The cap itself must hold.
	kept := strings.Count(strings.SplitN(got, "\n\n[...", 2)[0], "\n") + 1
	if kept != grepMaxMatches {
		t.Errorf("kept %d lines, want %d", kept, grepMaxMatches)
	}
}

// Selection must preserve original ordering among kept lines — file grouping
// is how the model reads grep output.
func TestGrepOverflowKeepsOriginalOrder(t *testing.T) {
	total := grepMaxMatches + 50
	lines := make([]string, total)
	for i := range lines {
		lines[i] = fmt.Sprintf("a.go:%d: retryLedger hit %d", i, i)
	}
	ctx := toolport.WithTurnQuery(context.Background(), "retryLedger")
	got := GrepResultRelevanceSummarizer(ctx, "grep", strings.Join(lines, "\n"))
	body := strings.SplitN(got, "\n\n[...", 2)[0]
	prev := -1
	for _, line := range strings.Split(body, "\n") {
		var n int
		if _, err := fmt.Sscanf(line, "a.go:%d:", &n); err != nil {
			t.Fatalf("unparseable kept line %q", line)
		}
		if n <= prev {
			t.Fatalf("kept lines out of original order: %d after %d", n, prev)
		}
		prev = n
	}
}

// Without a query signal the behavior must be byte-compatible with the old
// positional summarizer — including the marker text.
func TestGrepOverflowFallsBackPositionally(t *testing.T) {
	out := overflowOutput(grepMaxMatches+10, grepMaxMatches+5, "unmatched needle text")
	for name, ctx := range map[string]context.Context{
		"no turn query":     context.Background(),
		"filler-only query": toolport.WithTurnQuery(context.Background(), "확인 해줘 그리고 정리"),
	} {
		got := GrepResultRelevanceSummarizer(ctx, "grep", out)
		want := GrepResultSummarizer(ctx, "grep", out)
		if got != want {
			t.Errorf("%s: fallback diverges from the legacy positional cut", name)
		}
	}
}

// Under the cap nothing may change at all.
func TestGrepUnderCapPassesThrough(t *testing.T) {
	out := overflowOutput(grepMaxMatches-1, 3, "retryLedger")
	ctx := toolport.WithTurnQuery(context.Background(), "retryLedger")
	if got := GrepResultRelevanceSummarizer(ctx, "grep", out); got != out {
		t.Error("under-cap output was modified")
	}
}
