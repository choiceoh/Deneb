package mailanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestIsArchivableContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		att  gmail.AttachmentInfo
		want bool
	}{
		{name: "pdf", att: gmail.AttachmentInfo{Filename: "quote.pdf", Size: 1024}, want: true},
		{name: "uppercase xlsx", att: gmail.AttachmentInfo{Filename: "QUOTE.XLSX", Size: 2048}, want: true},
		{name: "hwp", att: gmail.AttachmentInfo{Filename: "계약.hwp", Size: 4096}, want: true},
		{name: "hwpx", att: gmail.AttachmentInfo{Filename: "계약.hwpx", Size: 4096}, want: true},
		{name: "zip", att: gmail.AttachmentInfo{Filename: "bundle.zip", Size: 4096}, want: true},
		{name: "csv", att: gmail.AttachmentInfo{Filename: "lines.csv", Size: 1024}, want: true},
		{name: "tiny", att: gmail.AttachmentInfo{Filename: "quote.pdf", Size: 1023}},
		{name: "zero", att: gmail.AttachmentInfo{Filename: "quote.pdf"}},
		{name: "truncated", att: gmail.AttachmentInfo{Filename: "quote.pdf", Size: 4096, Truncated: true}},
		{name: "inline image", att: gmail.AttachmentInfo{Filename: "logo.png", Size: 4096}},
		{name: "calendar", att: gmail.AttachmentInfo{Filename: "invite.ics", Size: 4096}},
		{name: "suffix lookalike", att: gmail.AttachmentInfo{Filename: "quote.pdf.exe", Size: 4096}},
		{name: "blank", att: gmail.AttachmentInfo{Size: 4096}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := isArchivable(tt.att); got != tt.want {
				t.Fatalf("isArchivable(%+v) = %v, want %v", tt.att, got, tt.want)
			}
		})
	}
}

func TestSanitizePathComponentContract(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{in: " report.pdf ", want: "report.pdf"},
		{in: "<sender@example.com>", want: "sender@example.com"},
		{in: "a/b\\c.pdf", want: "a_b_c.pdf"},
		{in: "../secret.pdf", want: ".._secret.pdf"},
		{in: "\x00bad\nname.pdf", want: "badname.pdf"},
		{in: "<>\t", want: "unknown"},
		{in: "", want: "unknown"},
		{in: ".", want: "unknown"},
		{in: "견적서 1차.xlsx", want: "견적서 1차.xlsx"},
	} {
		if got := sanitizePathComponent(tt.in); got != tt.want {
			t.Errorf("sanitizePathComponent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAnalysisPromptFallbackToDefaultOtherwiseTrims(t *testing.T) {
	for _, tt := range []struct{ prompt, want string }{
		{want: DefaultPrompt},
		{prompt: " ", want: DefaultPrompt},
		{prompt: "\n custom prompt \t", want: "custom prompt"},
	} {
		if got := analysisPrompt(PipelineDeps{AnalysisPrompt: tt.prompt}); got != tt.want {
			t.Errorf("analysisPrompt(%q) = %q", tt.prompt, got)
		}
	}
}

func TestCanRunPipelineTrueWhenClientAndModelSet(t *testing.T) {
	client := llm.NewClient("http://example.test", "key")
	for _, tt := range []struct {
		name string
		deps PipelineDeps
		want bool
	}{
		{name: "none"},
		{name: "client only", deps: PipelineDeps{LocalClient: client}},
		{name: "model only", deps: PipelineDeps{LocalModel: "model"}},
		{name: "both", deps: PipelineDeps{LocalClient: client, LocalModel: "model"}, want: true},
		{name: "gmail irrelevant", deps: PipelineDeps{LocalClient: client, LocalModel: "model", GmailClient: &gmail.Client{}}, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.deps.canRunPipeline(); got != tt.want {
				t.Fatalf("canRunPipeline = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectCandidatesContract(t *testing.T) {
	if got := (&PipelineDeps{}).projectCandidates(); got != nil {
		t.Fatalf("nil provider = %#v", got)
	}
	if got := (&PipelineDeps{ProjectsFn: func() []ProjectCandidate { return nil }}).projectCandidates(); got != nil {
		t.Fatalf("nil candidates = %#v", got)
	}
	candidates := make([]ProjectCandidate, maxProjectCandidates+7)
	for i := range candidates {
		candidates[i] = ProjectCandidate{Path: fmt.Sprintf("projects/%02d.md", i), Title: fmt.Sprintf("Project %02d", i)}
	}
	got := (&PipelineDeps{ProjectsFn: func() []ProjectCandidate { return candidates }}).projectCandidates()
	if len(got) != maxProjectCandidates || got[0] != candidates[0] || got[len(got)-1] != candidates[maxProjectCandidates-1] {
		t.Fatalf("candidates len/edges = %d %+v %+v", len(got), got[0], got[len(got)-1])
	}
}

func TestAnalysisThinkingTypeAndBudgetBoundary(t *testing.T) {
	anthropic := llm.NewClient("http://example.test", "key", llm.WithAPIMode(llm.APIModeAnthropic))
	for _, tt := range []struct {
		max     int
		typeVal string
		budget  int
	}{
		{max: -1, typeVal: "disabled"},
		{max: 0, typeVal: "disabled"},
		{max: analysisThinkingMinTokens - 1, typeVal: "disabled"},
		{max: analysisThinkingMinTokens, typeVal: "enabled", budget: analysisThinkingMinTokens / 2},
		{max: 4096, typeVal: "enabled", budget: 2048},
		{max: analysisThinkingMaxBudget * 4, typeVal: "enabled", budget: analysisThinkingMaxBudget},
	} {
		got := analysisThinking(anthropic, tt.max)
		if got.Type != tt.typeVal || got.BudgetTokens != tt.budget {
			t.Errorf("analysisThinking(%d) = %+v", tt.max, got)
		}
	}
}

func TestFormatEmailBriefAndTruncateBody(t *testing.T) {
	msg := &gmail.MessageDetail{From: "from", To: "to", Subject: "subject", Date: "date", Body: "body"}
	if got := formatEmailBrief(msg); got != "From: from\nTo: to\nSubject: subject\nDate: date\n\nbody" {
		t.Fatalf("brief = %q", got)
	}
	for _, tt := range []struct {
		name string
		body string
		max  int
		want string
	}{
		{name: "under", body: "abc", max: 4, want: "abc"},
		{name: "exact", body: "abc", max: 3, want: "abc"},
		{name: "over", body: "abcd", max: 3, want: "abc\n... (생략)"},
		{name: "unicode", body: "가나다", max: 4, want: "가\n... (생략)"},
		{name: "zero", body: "abc", max: 0, want: "\n... (생략)"},
		{name: "empty", body: "", max: 0, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateBody(tt.body, tt.max)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("truncateBody = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractEmailAddrContractAdditional(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{in: "Name <person@example.com>", want: "person@example.com"},
		{in: "Name < person@example.com >", want: "person@example.com"},
		{in: "one@example.com", want: "one@example.com"},
		{in: "  one@example.com  ", want: "one@example.com"},
		{in: "Name <broken@example.com", want: "Name <broken@example.com"},
		{in: "Name only"},
		{in: ""},
	} {
		if got := extractEmailAddr(tt.in); got != tt.want {
			t.Errorf("extractEmailAddr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContextPresenceContracts(t *testing.T) {
	for _, tt := range []struct {
		name string
		tc   ThreadContext
		want bool
	}{
		{name: "empty"},
		{name: "summary", tc: ThreadContext{ThreadSummary: "x"}, want: true},
		{name: "prior", tc: ThreadContext{PriorExchanges: "x"}, want: true},
		{name: "topics", tc: ThreadContext{OngoingTopics: []string{"x"}}, want: true},
		{name: "relation", tc: ThreadContext{SenderRelation: "x"}, want: true},
	} {
		if got := hasThreadContext(tt.tc); got != tt.want {
			t.Errorf("%s hasThreadContext = %v", tt.name, got)
		}
	}
	for _, tt := range []struct {
		name string
		mc   MemoryContext
		want bool
	}{
		{name: "empty"},
		{name: "sender", mc: MemoryContext{SenderFacts: "x"}, want: true},
		{name: "topic", mc: MemoryContext{TopicFacts: "x"}, want: true},
		{name: "history", mc: MemoryContext{RelevantHistory: "x"}, want: true},
	} {
		if got := hasMemoryContext(tt.mc); got != tt.want {
			t.Errorf("%s hasMemoryContext = %v", tt.name, got)
		}
	}
}

func TestParseImportanceContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		text string
		tier string
	}{
		{name: "absent", in: "analysis", text: "analysis"},
		{name: "urgent korean", in: "analysis\nIMPORTANCE: 긴급", text: "analysis", tier: "urgent"},
		{name: "attention korean", in: "analysis\nimportance: 확인", text: "analysis", tier: "attention"},
		{name: "routine korean", in: "analysis\n**IMPORTANCE:** 참고", text: "analysis", tier: "routine"},
		{name: "urgent english", in: "analysis\nIMPORTANCE: URGENT", text: "analysis", tier: "urgent"},
		{name: "attention english", in: "analysis\nIMPORTANCE: Attention", text: "analysis", tier: "attention"},
		{name: "routine english", in: "analysis\nIMPORTANCE: routine", text: "analysis", tier: "routine"},
		{name: "unknown stripped", in: "analysis\nIMPORTANCE: maybe", text: "analysis"},
		{name: "middle tag", in: "first\nIMPORTANCE: urgent\nlast", text: "first\nlast", tier: "urgent"},
		{name: "trailing blanks", in: "analysis\n\nIMPORTANCE: 참고\n\n", text: "analysis", tier: "routine"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, tier := parseImportance(tt.in)
			if text != tt.text || tier != tt.tier {
				t.Fatalf("parseImportance = %q/%q, want %q/%q", text, tier, tt.text, tt.tier)
			}
		})
	}
}

func TestCutTagPrefixContract(t *testing.T) {
	for _, tt := range []struct {
		line string
		tag  string
		rest string
		ok   bool
	}{
		{line: "IMPORTANCE: urgent", tag: "IMPORTANCE:", rest: "urgent", ok: true},
		{line: "importance: urgent", tag: "IMPORTANCE:", rest: "urgent", ok: true},
		{line: " **IMPORTANCE:** urgent", tag: "IMPORTANCE:", rest: "urgent", ok: true},
		{line: "__IMPORTANCE:__ attention", tag: "IMPORTANCE:", rest: "attention", ok: true},
		{line: "XIMPORTANCE: urgent", tag: "IMPORTANCE:"},
		{line: "IMPORTANCE", tag: "IMPORTANCE:"},
		{line: "", tag: "IMPORTANCE:"},
	} {
		rest, ok := cutTagPrefix(tt.line, tt.tag)
		if rest != tt.rest || ok != tt.ok {
			t.Errorf("cutTagPrefix(%q) = %q/%v", tt.line, rest, ok)
		}
	}
}

func TestProjectSelectionSuffixContract(t *testing.T) {
	if got := projectSelectionSuffix(nil); got != "" {
		t.Fatalf("nil suffix = %q", got)
	}
	got := projectSelectionSuffix([]ProjectCandidate{
		{Path: "projects/a.md"},
		{Path: "projects/b.md", Title: "B"},
		{Path: "projects/c.md", Title: "C", Summary: "summary"},
	})
	for _, want := range []string{"RELATED_PROJECTS:", "- projects/a.md\n", "- projects/b.md: B\n", "- projects/c.md: C — summary\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("suffix missing %q:\n%s", want, got)
		}
	}
}

func TestParseRelatedProjectsContractAdditional(t *testing.T) {
	candidates := []ProjectCandidate{{Path: "projects/a.md"}, {Path: "projects/b.md"}, {Path: "프로젝트/c.md"}}
	for _, tt := range []struct {
		name string
		in   string
		text string
		want []string
	}{
		{name: "none", in: "analysis", text: "analysis"},
		{name: "valid", in: "analysis\nRELATED_PROJECTS: projects/a.md, projects/b.md", text: "analysis", want: []string{"projects/a.md", "projects/b.md"}},
		{name: "markdown", in: "analysis\n**RELATED_PROJECTS:** `projects/a.md`, \"프로젝트/c.md\"", text: "analysis", want: []string{"projects/a.md", "프로젝트/c.md"}},
		{name: "dedupe invalid", in: "head\nRELATED_PROJECTS: stale.md, projects/a.md, projects/a.md\ntail", text: "head\ntail", want: []string{"projects/a.md"}},
		{name: "empty tag", in: "analysis\nRELATED_PROJECTS: ", text: "analysis"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, got := parseRelatedProjects(tt.in, candidates)
			if text != tt.text || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseRelatedProjects = %q/%v, want %q/%v", text, got, tt.text, tt.want)
			}
		})
	}
	input := "analysis\nRELATED_PROJECTS: projects/a.md"
	if text, got := parseRelatedProjects(input, nil); text != input || got != nil {
		t.Fatalf("no candidates = %q/%v", text, got)
	}
}

func TestRenderFactsBlockContractAdditional(t *testing.T) {
	if got := renderFactsBlock(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := renderFactsBlock([]WikiFactProposal{{Entity: " ", Fact: "x"}, {Entity: "e", Fact: " "}}); got != "" {
		t.Fatalf("invalid = %q", got)
	}
	got := renderFactsBlock([]WikiFactProposal{
		{Entity: " 회사 A ", Type: " org ", Fact: " 6월 30일 계약 "},
		{Entity: "프로젝트 B", Fact: "예산 10억원"},
		{Entity: "", Type: "person", Fact: "ignored"},
	})
	want := "📝 위키 갱신 제안 (자동 추출):\n- **회사 A** (org): 6월 30일 계약\n- **프로젝트 B**: 예산 10억원"
	if got != want {
		t.Fatalf("facts = %q, want %q", got, want)
	}
}

func TestStripWikiFactsBlockContractAdditional(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{name: "absent", in: "analysis", want: "analysis"},
		{name: "tail", in: "analysis\n\n📝 위키 갱신 제안 (자동 추출):\n- **A**: fact", want: "analysis"},
		{name: "head", in: "📝 위키 갱신 제안 (자동 추출):\n- **A**: fact\n\nremaining", want: "remaining"},
		{name: "middle", in: "head\n\n📝 위키 갱신 제안 (자동 추출):\n- **A**: fact\n\nnote", want: "head\n\nnote"},
		{name: "only", in: "📝 위키 갱신 제안 (자동 추출):\n- **A**: fact", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripWikiFactsBlock(tt.in); got != tt.want {
				t.Fatalf("StripWikiFactsBlock = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeActionItemsContractAdditional(t *testing.T) {
	in := []ActionItem{
		{Title: " ", Priority: "high"},
		{Title: " 승인하기 ", DueHint: " 내일 ", Priority: "URGENT"},
		{Title: "검토하기", Priority: "낮음"},
		{Title: "회신하기", Priority: "unknown"},
		{Title: "fourth ignored", Priority: "high"},
	}
	want := []ActionItem{
		{Title: "승인하기", DueHint: "내일", Priority: "high"},
		{Title: "검토하기", Priority: "low"},
		{Title: "회신하기", Priority: "medium"},
	}
	if got := sanitizeActionItems(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %+v, want %+v", got, want)
	}
	if got := sanitizeActionItems(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil actions = %#v", got)
	}
}

func TestNormalizeActionPriorityMatrix(t *testing.T) {
	for input, want := range map[string]string{
		"high": "high", " HIGH ": "high", "urgent": "high", "높음": "high", "긴급": "high",
		"low": "low", " LOW ": "low", "낮음": "low",
		"medium": "medium", "MEDIUM": "medium", "": "medium", "unknown": "medium", "보통": "medium",
	} {
		if got := normalizeActionPriority(input); got != want {
			t.Errorf("normalizeActionPriority(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDealInfoFromExtractContractAdditional(t *testing.T) {
	if got := dealInfoFromExtract(dealExtract{}, "", nil); got != nil {
		t.Fatalf("non-deal = %+v", got)
	}
	if got := dealInfoFromExtract(dealExtract{IsDeal: true, Counterparty: " "}, "", nil); got != nil {
		t.Fatalf("empty counterparty = %+v", got)
	}
	if got := dealInfoFromExtract(dealExtract{IsDeal: true, Counterparty: "탑솔라"}, "", nil); got != nil {
		t.Fatalf("self counterparty = %+v", got)
	}
	ext := dealExtract{
		IsDeal: true, Counterparty: " Vendor Co ", DocType: " 견적서 ", Amount: " 1,000원 ",
		Date: " 2026-07-11 ", DueDate: " 2026-07-31 ", Items: []string{" module ", "", " inverter "}, Summary: " quote ",
	}
	got := dealInfoFromExtract(ext, "총액 1,000원", nil)
	if got == nil || got.Counterparty != "Vendor Co" || got.DocType != "견적서" || got.Amount != "1,000원" || !reflect.DeepEqual(got.Items, []string{"module", "inverter"}) {
		t.Fatalf("deal = %+v", got)
	}
	unverified := dealInfoFromExtract(ext, "source has no amount", nil)
	if unverified == nil || unverified.Amount != "" || !strings.Contains(unverified.Summary, "원문 대조 실패") {
		t.Fatalf("unverified deal = %+v", unverified)
	}
}

func TestCollectStreamTextContractAdditional(t *testing.T) {
	event := func(kind, text string) llm.StreamEvent {
		payload, _ := json.Marshal(map[string]any{"delta": map[string]string{"type": kind, "text": text}})
		return llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw(payload)}
	}
	ch := make(chan llm.StreamEvent, 8)
	ch <- llm.StreamEvent{Type: "message_start", Payload: llm.FlexibleFromRaw([]byte(`{}`))}
	ch <- event("thinking_delta", "secret")
	ch <- event("text_delta", " hello ")
	ch <- llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw([]byte(`not-json`))}
	ch <- event("text_delta", "world")
	close(ch)
	if got, err := collectStreamText(context.Background(), ch); err != nil || got != "hello world" {
		t.Fatalf("collect = %q/%v", got, err)
	}
	if _, err := collectStreamText(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nil event") {
		t.Fatalf("nil channel error = %v", err)
	}
	empty := make(chan llm.StreamEvent)
	close(empty)
	if _, err := collectStreamText(context.Background(), empty); err == nil || !strings.Contains(err.Error(), "empty LLM") {
		t.Fatalf("empty error = %v", err)
	}
}

func TestCollectStreamTextErrorPayloads(t *testing.T) {
	for _, tt := range []struct{ payload, want string }{
		{payload: `{"message":"overloaded"}`, want: "overloaded"},
		{payload: `{"message":""}`, want: `{"message":""}`},
		{payload: `not-json`, want: "not-json"},
	} {
		ch := make(chan llm.StreamEvent, 1)
		ch <- llm.StreamEvent{Type: "error", Payload: llm.FlexibleFromRaw([]byte(tt.payload))}
		close(ch)
		if _, err := collectStreamText(context.Background(), ch); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("payload %q error = %v", tt.payload, err)
		}
	}
}

func TestCollectStreamTextCancellationContract(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := make(chan llm.StreamEvent)
	if _, err := collectStreamText(ctx, blocked); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel empty = %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw([]byte(`{"delta":{"type":"text_delta","text":"partial"}}`))}
	go func() {
		time.Sleep(time.Millisecond)
		cancel()
	}()
	got, err := collectStreamText(ctx, ch)
	if err != nil || got != "partial" {
		t.Fatalf("cancel partial = %q/%v", got, err)
	}
}

type contractThreadSource struct {
	msgs  []*gmail.MessageDetail
	err   error
	calls int
}

func (s *contractThreadSource) RelatedMessages(_ context.Context, _ *gmail.MessageDetail) ([]*gmail.MessageDetail, error) {
	s.calls++
	return s.msgs, s.err
}

func TestGatherRelatedMessagesArchiveContract(t *testing.T) {
	msg := &gmail.MessageDetail{ID: "current"}
	source := &contractThreadSource{msgs: []*gmail.MessageDetail{{ID: "prior"}}}
	got := gatherRelatedMessages(context.Background(), PipelineDeps{ThreadSource: source}, msg)
	if len(got) != 1 || got[0].ID != "prior" || source.calls != 1 {
		t.Fatalf("related = %+v calls=%d", got, source.calls)
	}
	source.err = errors.New("archive unavailable")
	if got := gatherRelatedMessages(context.Background(), PipelineDeps{ThreadSource: source, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, msg); got != nil {
		t.Fatalf("error related = %+v", got)
	}
	if got := gatherRelatedMessages(context.Background(), PipelineDeps{}, msg); got != nil {
		t.Fatalf("no source = %+v", got)
	}
}

func TestExtractSenderContextTruncatesFactsToUnicodeCap(t *testing.T) {
	msg := &gmail.MessageDetail{From: "홍길동 <hong@example.test>"}
	calledWith := ""
	deps := PipelineDeps{SenderFactsFn: func(_ context.Context, from string) string {
		calledWith = from
		return " facts "
	}}
	got := extractSenderContext(context.Background(), deps, msg)
	if got.SenderFacts != "facts" || calledWith != msg.From {
		t.Fatalf("context = %+v called=%q", got, calledWith)
	}
	deps.SenderFactsFn = func(context.Context, string) string { return strings.Repeat("가", maxSenderFactsChars) }
	got = extractSenderContext(context.Background(), deps, msg)
	if !utf8.ValidString(got.SenderFacts) || len(got.SenderFacts) > maxSenderFactsChars+len("\n...(생략)") {
		t.Fatalf("invalid/cap facts len=%d valid=%v", len(got.SenderFacts), utf8.ValidString(got.SenderFacts))
	}
	if !strings.HasSuffix(got.SenderFacts, "\n...(생략)") {
		t.Fatalf("cap marker missing")
	}
	if got := extractSenderContext(context.Background(), deps, &gmail.MessageDetail{}); got != (MemoryContext{}) {
		t.Fatalf("empty sender = %+v", got)
	}
}

func TestExtractDisplayNameAdditionalMatrix(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{in: " 홍길동 <hong@example.test> ", want: "홍길동"},
		{in: `'홍길동' <hong@example.test>`, want: "'홍길동'"},
		{in: `"홍길동" <hong@example.test>`, want: "홍길동"},
		{in: "< hong@example.test >", want: "hong@example.test"},
		{in: "plain name", want: "plain name"},
		{in: ""},
	} {
		if got := extractDisplayName(tt.in); got != tt.want {
			t.Errorf("extractDisplayName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

type queuedOpenAIServer struct {
	t        *testing.T
	mu       sync.Mutex
	outputs  []string
	statuses []int
	requests []map[string]any
	server   *httptest.Server
}

func newQueuedOpenAIServer(t *testing.T, outputs ...string) *queuedOpenAIServer {
	t.Helper()
	q := &queuedOpenAIServer{t: t, outputs: append([]string(nil), outputs...)}
	q.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		q.mu.Lock()
		q.requests = append(q.requests, request)
		if len(q.outputs) == 0 {
			q.mu.Unlock()
			http.Error(w, "no queued output", http.StatusInternalServerError)
			return
		}
		out := q.outputs[0]
		q.outputs = q.outputs[1:]
		status := http.StatusOK
		if len(q.statuses) > 0 {
			status = q.statuses[0]
			q.statuses = q.statuses[1:]
		}
		q.mu.Unlock()
		if status != http.StatusOK {
			http.Error(w, out, status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{"id": "chatcmpl-test", "model": "test", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": out}}}}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	return q
}

func (q *queuedOpenAIServer) Close() { q.server.Close() }

// Client returns an LLM client pointed at the fake server with retries disabled.
// Production defaults (maxRetries=6, baseDelay=1s) turn a single 503 into ~60s of
// backoff — fine for live traffic, lethal for unit tests that assert failure paths.
func (q *queuedOpenAIServer) Client() *llm.Client {
	return llm.NewClient(q.server.URL, "key", llm.WithRetry(0, 0, 0))
}

func TestCallLocalLLMJSONContractAndSchemaFallback(t *testing.T) {
	t.Run("valid strict", func(t *testing.T) {
		q := newQueuedOpenAIServer(t, `{"actions":[{"title":"승인","dueHint":"내일","priority":"high"}]}`)
		defer q.Close()
		got, err := callLocalLLMJSON[actionItemsBundle](context.Background(), q.Client(), "model", "system", "user", 123, actionItemsSchema)
		if err != nil || len(got.Actions) != 1 || got.Actions[0].Title != "승인" {
			t.Fatalf("call = %+v/%v", got, err)
		}
		q.mu.Lock()
		defer q.mu.Unlock()
		if len(q.requests) != 1 || q.requests[0]["response_format"] == nil {
			t.Fatalf("requests = %+v", q.requests)
		}
	})
	t.Run("parse retry drops schema", func(t *testing.T) {
		q := newQueuedOpenAIServer(t, `not json`, `{"actions":[]}`)
		defer q.Close()
		got, err := callLocalLLMJSON[actionItemsBundle](context.Background(), q.Client(), "model", "system", "user", 123, actionItemsSchema)
		if err != nil || len(got.Actions) != 0 {
			t.Fatalf("call = %+v/%v", got, err)
		}
		q.mu.Lock()
		defer q.mu.Unlock()
		if len(q.requests) != 2 {
			t.Fatalf("request count = %d", len(q.requests))
		}
		first, _ := q.requests[0]["response_format"].(map[string]any)
		second, _ := q.requests[1]["response_format"].(map[string]any)
		if first["type"] != "json_schema" || second["type"] != "json_object" {
			t.Fatalf("formats = %+v %+v", first, second)
		}
	})
	t.Run("both invalid", func(t *testing.T) {
		q := newQueuedOpenAIServer(t, `{`, `still invalid`)
		defer q.Close()
		if _, err := callLocalLLMJSON[actionItemsBundle](context.Background(), q.Client(), "model", "system", "user", 123, nil); err == nil || !strings.Contains(err.Error(), "JSON parse failed") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestRunFinalSynthesisReturnsAgentResultWhenAvailable(t *testing.T) {
	called := 0
	deps := PipelineDeps{AgentSynthesisFn: func(_ context.Context, prompt string) (string, error) {
		called++
		if !strings.Contains(prompt, finalAnalysisSystem) ||
			!strings.Contains(prompt, agentSynthesisReadOnlyInstruction) ||
			!strings.Contains(prompt, "user prompt") {
			t.Errorf("agent prompt = %q", prompt)
		}
		return " agent result ", nil
	}}
	got, err := runFinalSynthesis(context.Background(), deps, "user prompt", 100)
	if err != nil || got != " agent result " || called != 1 {
		t.Fatalf("result = %q/%v calls=%d", got, err, called)
	}
}

func TestRunFinalSynthesisAgentFallback(t *testing.T) {
	for _, tt := range []struct {
		name string
		out  string
		err  error
	}{
		{name: "error", err: errors.New("agent unavailable")},
		{name: "empty", out: "  "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueuedOpenAIServer(t, "fallback result")
			defer q.Close()
			deps := PipelineDeps{LLMClient: q.Client(), MainModel: "main", ThinkingKwarg: "thinking", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AgentSynthesisFn: func(context.Context, string) (string, error) { return tt.out, tt.err }}
			got, err := runFinalSynthesis(context.Background(), deps, "prompt", 321)
			if err != nil || got != "fallback result" {
				t.Fatalf("fallback = %q/%v", got, err)
			}
			q.mu.Lock()
			defer q.mu.Unlock()
			if len(q.requests) != 1 || int(q.requests[0]["max_tokens"].(float64)) != 321 {
				t.Fatalf("request = %+v", q.requests)
			}
		})
	}
}

func TestAnalyzeEmailPipelineFallbackTags(t *testing.T) {
	q := newQueuedOpenAIServer(t, "분석 본문\nRELATED_PROJECTS: projects/a.md, fake.md\nIMPORTANCE: 긴급")
	defer q.Close()
	deps := PipelineDeps{LLMClient: q.Client(), MainModel: "main", ProjectsFn: func() []ProjectCandidate { return []ProjectCandidate{{Path: "projects/a.md"}} }}
	result, err := AnalyzeEmailPipeline(context.Background(), deps, &gmail.MessageDetail{From: "vendor@example.test", To: "me@example.test", Subject: "subject", Date: "Sat, 11 Jul 2026 10:00:00 +0900", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "분석 본문" || result.Importance != "urgent" || !reflect.DeepEqual(result.RelatedProjects, []string{"projects/a.md"}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestAnalyzeEmailPipelineFallbackErrorsAndThinkingFilter(t *testing.T) {
	q := newQueuedOpenAIServer(t, "")
	defer q.Close()
	_, err := AnalyzeEmailPipeline(context.Background(), PipelineDeps{LLMClient: q.Client(), MainModel: "main"}, &gmail.MessageDetail{Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "비어있습니다") {
		t.Fatalf("empty error = %v", err)
	}
}

func TestAnalyzeBatchFallbackIntegration(t *testing.T) {
	q := newQueuedOpenAIServer(
		t,
		"첫 분석\nIMPORTANCE: 참고",
		"둘 분석\nIMPORTANCE: 확인",
		"통합 報告書",
	)
	defer q.Close()
	deps := PipelineDeps{LLMClient: q.Client(), MainModel: "main", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	msgs := []*gmail.MessageDetail{
		{ID: "one", From: "a@example.test", To: "me@example.test", Subject: "one", Body: "body one"},
		{ID: "two", From: "b@example.test", To: "me@example.test", Subject: "two", Body: "body two"},
	}
	report, items, err := AnalyzeBatch(context.Background(), deps, msgs)
	if err != nil || len(items) != 2 || report != "통합 보고서" {
		t.Fatalf("AnalyzeBatch = %q %+v %v", report, items, err)
	}
	if items[0].Result.Importance != "routine" || items[1].Result.Importance != "attention" {
		t.Fatalf("items = %+v", items)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.requests) != 3 {
		t.Fatalf("requests = %d", len(q.requests))
	}
}

func TestAnalyzeBatchSingleAndEmptyContracts(t *testing.T) {
	if report, items, err := AnalyzeBatch(context.Background(), PipelineDeps{}, nil); err == nil || report != "" || items != nil {
		t.Fatalf("empty = %q/%+v/%v", report, items, err)
	}
	q := newQueuedOpenAIServer(t, "single analysis")
	defer q.Close()
	report, items, err := AnalyzeBatch(context.Background(), PipelineDeps{LLMClient: q.Client(), MainModel: "main"}, []*gmail.MessageDetail{{ID: "one", Body: "body"}})
	if err != nil || report != "single analysis" || len(items) != 1 {
		t.Fatalf("single = %q/%+v/%v", report, items, err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.requests) != 1 {
		t.Fatalf("single made %d requests", len(q.requests))
	}
}

func TestExtractForEvalRejectsInvalidInputsAndParsesKinds(t *testing.T) {
	if _, err := ExtractForEval(context.Background(), nil, "model", "deal", "input"); err == nil {
		t.Fatal("nil client accepted")
	}
	client := llm.NewClient("http://example.test", "key")
	if _, err := ExtractForEval(context.Background(), client, "", "deal", "input"); err == nil {
		t.Fatal("empty model accepted")
	}
	if _, err := ExtractForEval(context.Background(), client, "model", "unknown", "input"); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("unknown error = %v", err)
	}
	for _, tt := range []struct {
		kind  string
		json  string
		check func(any) bool
	}{
		{kind: "deal", json: `{"isDeal":false}`, check: func(v any) bool { return v == (*DealInfo)(nil) }},
		{kind: "dealfacts", json: `{"facts":[]}`, check: func(v any) bool { return v == (*DealFacts)(nil) }},
		{kind: "facts", json: `{"facts":[]}`, check: func(v any) bool { s, ok := v.(string); return ok && s == "" }},
		{kind: "actions", json: `{"actions":[]}`, check: func(v any) bool { a, ok := v.([]ActionItem); return ok && len(a) == 0 }},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			q := newQueuedOpenAIServer(t, tt.json)
			defer q.Close()
			got, err := ExtractForEval(context.Background(), q.Client(), "model", tt.kind, "input")
			if err != nil || !tt.check(got) {
				t.Fatalf("ExtractForEval = %#v/%v", got, err)
			}
		})
	}
}

func TestLoadPromptFallbacksAdditional(t *testing.T) {
	if got := loadPrompt(filepath.Join(t.TempDir(), "missing")); got != DefaultPrompt {
		t.Fatalf("missing prompt = %q", got)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadPrompt(empty); got != DefaultPrompt {
		t.Fatalf("blank prompt = %q", got)
	}
	custom := filepath.Join(t.TempDir(), "custom")
	if err := os.WriteFile(custom, []byte("\n custom \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadPrompt(custom); got != "custom" {
		t.Fatalf("custom prompt = %q", got)
	}
}
