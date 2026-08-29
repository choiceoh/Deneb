package mailanalysis

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// The topic half of MemoryContext shipped unwired: arrival analysis resolved
// the sender (identity graph) and the thread (previous mails), but the
// TopicFacts slot was always passed empty, so a message on a running deal
// arrived without the prior quotes, decisions and terms the wiki already held.
// The renderer for it existed the whole time ("- **주제 관련**: …").

func TestExtractTopicContextRecallsSubjectMaterial(t *testing.T) {
	var gotSubject, gotBody string
	deps := PipelineDeps{TopicFactsFn: func(_ context.Context, subject, body string) string {
		gotSubject, gotBody = subject, body
		return "- 프로젝트/pl2-dsv-epc-001 | 당진 솔라빌리지 | EPC 1,042.78억"
	}}
	msg := &gmail.MessageDetail{Subject: "당진 견적 회신", Body: "첨부 확인 부탁드립니다"}

	got := extractTopicContext(context.Background(), deps, msg)
	if !strings.Contains(got, "당진 솔라빌리지") {
		t.Fatalf("recalled topic material must survive: %q", got)
	}
	if gotSubject != "당진 견적 회신" || gotBody != "첨부 확인 부탁드립니다" {
		t.Errorf("resolver must see subject and body: %q / %q", gotSubject, gotBody)
	}
}

func TestExtractTopicContextIsBestEffort(t *testing.T) {
	msg := &gmail.MessageDetail{Subject: "제목"}
	// No resolver wired: the slot stays empty, which is how it shipped before.
	if got := extractTopicContext(context.Background(), PipelineDeps{}, msg); got != "" {
		t.Errorf("nil resolver must yield empty, got %q", got)
	}
	// A resolver that finds nothing must not manufacture a section.
	empty := PipelineDeps{TopicFactsFn: func(context.Context, string, string) string { return "   " }}
	if got := extractTopicContext(context.Background(), empty, msg); got != "" {
		t.Errorf("blank result must yield empty, got %q", got)
	}
	if got := extractTopicContext(context.Background(), empty, nil); got != "" {
		t.Errorf("nil message must yield empty, got %q", got)
	}
}

func TestExtractTopicContextBoundsThePrompt(t *testing.T) {
	// A subject can span several project pages; the analyze prompt still has
	// to stay small.
	huge := PipelineDeps{TopicFactsFn: func(context.Context, string, string) string {
		return strings.Repeat("가", 20000)
	}}
	got := extractTopicContext(context.Background(), huge,
		&gmail.MessageDetail{Subject: "제목"})
	if len(got) > maxTopicFactsChars+64 {
		t.Fatalf("topic facts must be bounded, got %d bytes", len(got))
	}
	if !strings.Contains(got, "생략") {
		t.Error("truncation must be visible to the reader")
	}
}

func TestMemoryContextRendersTopicFacts(t *testing.T) {
	// The renderer existed before the wiring; this pins that a filled slot
	// actually reaches the synthesis prompt rather than being dropped.
	if !hasMemoryContext(MemoryContext{TopicFacts: "x"}) {
		t.Fatal("topic-only memory must count as present, or the section is skipped")
	}
}
