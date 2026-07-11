package compactuner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// recordingSummarySource makes the scheduler boundary observable without
// depending on the polaris store. The source intentionally returns a copy so
// the tests also catch accidental mutation of caller-owned summary slices.
type recordingSummarySource struct {
	mu     sync.Mutex
	nodes  []polaris.SummaryNode
	calls  int
	limits []int
}

func (s *recordingSummarySource) RecentSummariesAcrossSessions(limit int) []polaris.SummaryNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.limits = append(s.limits, limit)
	return append([]polaris.SummaryNode(nil), s.nodes...)
}

type boundaryLLM struct {
	mu       sync.Mutex
	reply    string
	err      error
	nilCh    bool
	requests []llm.ChatRequest
	calls    int
}

func (f *boundaryLLM) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	f.mu.Lock()
	f.calls++
	f.requests = append(f.requests, req)
	err := f.err
	nilCh := f.nilCh
	reply := f.reply
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if nilCh {
		return nil, nil
	}
	ch := make(chan llm.StreamEvent, 3)
	// Multiple deltas exercise the same boundary as a real streaming provider.
	runes := []rune(reply)
	cut := len(runes) / 2
	for _, part := range []string{string(runes[:cut]), string(runes[cut:])} {
		payload, _ := json.Marshal(map[string]any{
			"delta": map[string]string{"text": part},
		})
		ch <- llm.StreamEvent{Type: "content_block_delta", Payload: payload}
	}
	close(ch)
	return ch, nil
}

func newBoundaryTask(t *testing.T, src SummarySource, client llmClient) (*Task, *compaction.GuidelineStore) {
	t.Helper()
	store := compaction.NewGuidelineStore(filepath.Join(t.TempDir(), compaction.GuidelineFileName))
	return NewTask(Deps{
		Summaries:  src,
		Guidelines: store,
		Client:     client,
		Model:      "boundary-model",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), store
}

func fourBoundaryLeaves() []polaris.SummaryNode {
	return []polaris.SummaryNode{
		{Level: 1, Content: "첫 번째 요약"},
		{Level: 1, Content: "두 번째 요약"},
		{Level: 1, Content: "세 번째 요약"},
		{Level: 1, Content: "네 번째 요약"},
	}
}

func TestBoundaryRunWithReportUnavailableDependencyMatrix(t *testing.T) {
	validSource := &recordingSummarySource{nodes: fourBoundaryLeaves()}
	validClient := &boundaryLLM{reply: `{"guidelines":[]}`}
	validStore := compaction.NewGuidelineStore(filepath.Join(t.TempDir(), compaction.GuidelineFileName))

	tests := []struct {
		name string
		deps Deps
	}{
		{
			name: "all dependencies missing",
			deps: Deps{},
		},
		{
			name: "summary source missing",
			deps: Deps{Guidelines: validStore, Client: validClient},
		},
		{
			name: "guideline store missing",
			deps: Deps{Summaries: validSource, Client: validClient},
		},
		{
			name: "model client missing",
			deps: Deps{Summaries: validSource, Guidelines: validStore},
		},
		{
			name: "only source configured",
			deps: Deps{Summaries: validSource},
		},
		{
			name: "only store configured",
			deps: Deps{Guidelines: validStore},
		},
		{
			name: "only client configured",
			deps: Deps{Client: validClient},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := NewTask(tt.deps)
			report := task.RunWithReport(context.Background())
			if report.Reason != "unavailable" {
				t.Fatalf("reason = %q, want unavailable", report.Reason)
			}
			if report.Ran || report.Changed {
				t.Fatalf("unavailable task reported work: %#v", report)
			}
			if report.MinSummaries != minSummaries {
				t.Fatalf("min summaries = %d, want %d", report.MinSummaries, minSummaries)
			}
		})
	}
}

func TestBoundaryLeafSummaryTextsMatrix(t *testing.T) {
	tests := []struct {
		name  string
		nodes []polaris.SummaryNode
		want  []string
	}{
		{
			name:  "nil input",
			nodes: nil,
			want:  nil,
		},
		{
			name:  "empty input",
			nodes: []polaris.SummaryNode{},
			want:  nil,
		},
		{
			name: "zero level excluded",
			nodes: []polaris.SummaryNode{
				{Level: 0, Content: "root"},
			},
			want: nil,
		},
		{
			name: "condensed levels excluded",
			nodes: []polaris.SummaryNode{
				{Level: 2, Content: "level two"},
				{Level: 3, Content: "level three"},
				{Level: 99, Content: "level ninety nine"},
			},
			want: nil,
		},
		{
			name: "negative level excluded",
			nodes: []polaris.SummaryNode{
				{Level: -1, Content: "invalid"},
			},
			want: nil,
		},
		{
			name: "empty leaf excluded",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: ""},
			},
			want: nil,
		},
		{
			name: "ascii whitespace leaf excluded",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: " \t\r\n "},
			},
			want: nil,
		},
		{
			name: "unicode whitespace leaf excluded",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: "\u2003\u3000"},
			},
			want: nil,
		},
		{
			name: "leaf content trimmed",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: "  keep me \n"},
			},
			want: []string{"keep me"},
		},
		{
			name: "internal whitespace preserved",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: "first\n\nsecond"},
			},
			want: []string{"first\n\nsecond"},
		},
		{
			name: "unicode preserved",
			nodes: []polaris.SummaryNode{
				{Level: 1, Content: "  선택님과 📎 검토  "},
			},
			want: []string{"선택님과 📎 검토"},
		},
		{
			name: "relative order preserved",
			nodes: []polaris.SummaryNode{
				{Level: 2, Content: "skip before"},
				{Level: 1, Content: "one"},
				{Level: 0, Content: "skip middle"},
				{Level: 1, Content: "two"},
				{Level: 3, Content: "skip after"},
				{Level: 1, Content: "three"},
			},
			want: []string{"one", "two", "three"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := leafSummaryTexts(tt.nodes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("leafSummaryTexts() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBoundaryAddedGuidelinesSetDifferenceMatrix(t *testing.T) {
	tests := []struct {
		name   string
		before []string
		after  []string
		want   []string
	}{
		{
			name:   "both nil",
			before: nil,
			after:  nil,
			want:   []string{},
		},
		{
			name:   "new singleton",
			before: nil,
			after:  []string{"amount"},
			want:   []string{"amount"},
		},
		{
			name:   "unchanged singleton",
			before: []string{"amount"},
			after:  []string{"amount"},
			want:   []string{},
		},
		{
			name:   "removal is not addition",
			before: []string{"amount", "name"},
			after:  []string{"name"},
			want:   []string{},
		},
		{
			name:   "new values retain after order",
			before: []string{"old"},
			after:  []string{"second", "old", "first"},
			want:   []string{"second", "first"},
		},
		{
			name:   "matching is case sensitive",
			before: []string{"Amount"},
			after:  []string{"amount"},
			want:   []string{"amount"},
		},
		{
			name:   "matching is whitespace sensitive",
			before: []string{"amount"},
			after:  []string{" amount"},
			want:   []string{" amount"},
		},
		{
			name:   "duplicates in before collapse for membership",
			before: []string{"old", "old", "old"},
			after:  []string{"old", "new"},
			want:   []string{"new"},
		},
		{
			name:   "duplicates newly present remain observable",
			before: nil,
			after:  []string{"new", "new"},
			want:   []string{"new", "new"},
		},
		{
			name:   "empty string follows exact set semantics",
			before: nil,
			after:  []string{""},
			want:   []string{""},
		},
		{
			name:   "unicode exact match",
			before: []string{"금액을 보존하라"},
			after:  []string{"금액을 보존하라", "이름을 보존하라"},
			want:   []string{"이름을 보존하라"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addedGuidelines(tt.before, tt.after)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("addedGuidelines() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBoundaryParseGuidelinesMalformedAndTolerantMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "empty input",
			text: "",
			want: nil,
		},
		{
			name: "plain prose",
			text: "there is no verdict here",
			want: nil,
		},
		{
			name: "empty object",
			text: `{}`,
			want: []string{},
		},
		{
			name: "missing guidelines field",
			text: `{"reason":"specific enough"}`,
			want: []string{},
		},
		{
			name: "null guidelines",
			text: `{"guidelines":null}`,
			want: []string{},
		},
		{
			name: "empty guidelines",
			text: `{"guidelines":[]}`,
			want: []string{},
		},
		{
			name: "single guideline",
			text: `{"guidelines":["금액을 보존하라"]}`,
			want: []string{"금액을 보존하라"},
		},
		{
			name: "surrounding prose",
			text: `analysis before {"guidelines":["날짜를 보존하라"]} analysis after`,
			want: []string{"날짜를 보존하라"},
		},
		{
			name: "markdown json fence",
			text: "```json\n{\"guidelines\":[\"이름을 보존하라\"]}\n```",
			want: []string{"이름을 보존하라"},
		},
		{
			name: "leading and trailing whitespace",
			text: " \n\t {\"guidelines\":[\"결정을 보존하라\"]} \r\n ",
			want: []string{"결정을 보존하라"},
		},
		{
			name: "trim each element",
			text: `{"guidelines":["  amount  ","\tname\n"]}`,
			want: []string{"amount", "name"},
		},
		{
			name: "drop empty elements",
			text: `{"guidelines":[""," ","\t\n","keep"]}`,
			want: []string{"keep"},
		},
		{
			name: "preserve duplicates",
			text: `{"guidelines":["same","same"]}`,
			want: []string{"same", "same"},
		},
		{
			name: "preserve internal whitespace",
			text: `{"guidelines":["amount  and  currency"]}`,
			want: []string{"amount  and  currency"},
		},
		{
			name: "preserve unicode",
			text: `{"guidelines":["담당자 이름과 📅 날짜를 보존하라"]}`,
			want: []string{"담당자 이름과 📅 날짜를 보존하라"},
		},
		{
			name: "ignore unknown fields",
			text: `{"guidelines":["keep"],"confidence":0.9,"other":{"nested":true}}`,
			want: []string{"keep"},
		},
		{
			name: "wrong scalar field type",
			text: `{"guidelines":"keep"}`,
			want: nil,
		},
		{
			name: "wrong object field type",
			text: `{"guidelines":{"0":"keep"}}`,
			want: nil,
		},
		{
			name: "wrong element type number",
			text: `{"guidelines":[1]}`,
			want: nil,
		},
		{
			name: "wrong element type null",
			text: `{"guidelines":[null]}`,
			want: []string{},
		},
		{
			name: "truncated object",
			text: `{"guidelines":["keep"]`,
			want: nil,
		},
		{
			name: "malformed array",
			text: `{"guidelines":[,"keep"]}`,
			want: nil,
		},
		{
			name: "malformed quoted string",
			text: `{"guidelines":["keep]}`,
			want: nil,
		},
		{
			name: "escaped quote preserved",
			text: `{"guidelines":["\"actual\" quote"]}`,
			want: []string{`"actual" quote`},
		},
		{
			name: "escaped newline preserved internally",
			text: `{"guidelines":["first\nsecond"]}`,
			want: []string{"first\nsecond"},
		},
		{
			name: "braces inside string",
			text: `prefix {"guidelines":["preserve {amount}"]} suffix`,
			want: []string{"preserve {amount}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGuidelines(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseGuidelines(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}

func TestBoundaryClipRuneAndLimitMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{
			name: "empty at zero",
			text: "",
			max:  0,
			want: "",
		},
		{
			name: "nonempty at zero",
			text: "a",
			max:  0,
			want: "…",
		},
		{
			name: "below limit",
			text: "ab",
			max:  3,
			want: "ab",
		},
		{
			name: "exact limit",
			text: "abc",
			max:  3,
			want: "abc",
		},
		{
			name: "one past limit",
			text: "abcd",
			max:  3,
			want: "abc…",
		},
		{
			name: "unicode below limit",
			text: "가나",
			max:  3,
			want: "가나",
		},
		{
			name: "unicode exact limit",
			text: "가나다",
			max:  3,
			want: "가나다",
		},
		{
			name: "unicode one past limit",
			text: "가나다라",
			max:  3,
			want: "가나다…",
		},
		{
			name: "emoji counted as rune",
			text: "A📎B",
			max:  2,
			want: "A📎…",
		},
		{
			name: "combining mark counted independently",
			text: "e\u0301x",
			max:  2,
			want: "e\u0301…",
		},
		{
			name: "newline retained",
			text: "a\nb",
			max:  2,
			want: "a\n…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clip(tt.text, tt.max)
			if got != tt.want {
				t.Fatalf("clip(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("clip returned invalid UTF-8: %x", []byte(got))
			}
		})
	}
}

func TestBoundaryCritiqueRequestContractAndClipping(t *testing.T) {
	long := strings.Repeat("가", maxSummaryLn+27)
	source := &recordingSummarySource{nodes: []polaris.SummaryNode{
		{Level: 1, Content: long},
		{Level: 1, Content: "second"},
		{Level: 1, Content: "third"},
		{Level: 1, Content: "fourth"},
	}}
	client := &boundaryLLM{reply: `{"guidelines":["금액을 보존하라"]}`}
	task, _ := newBoundaryTask(t, source, client)

	report := task.RunWithReport(context.Background())
	if report.Reason != "updated" || !report.Ran || !report.Changed {
		t.Fatalf("unexpected report: %#v", report)
	}
	source.mu.Lock()
	if source.calls != 1 || !reflect.DeepEqual(source.limits, []int{auditLimit}) {
		t.Fatalf("summary source calls=%d limits=%v", source.calls, source.limits)
	}
	source.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	req := client.requests[0]
	if req.Model != "boundary-model" {
		t.Fatalf("model = %q", req.Model)
	}
	if !req.Stream || req.MaxTokens != llmMaxTokens {
		t.Fatalf("stream/max token contract changed: stream=%v max=%d", req.Stream, req.MaxTokens)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
		t.Fatalf("response format = %#v", req.ResponseFormat)
	}
	if got := llm.ExtractSystemText(req.System); got != auditSystemPrompt {
		t.Fatalf("system prompt mismatch: %q", got)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(req.Messages))
	}
	var prompt string
	if err := json.Unmarshal(req.Messages[0].Content, &prompt); err != nil {
		t.Fatalf("decode message content: %v", err)
	}
	if !strings.Contains(prompt, "### 요약 1") || !strings.Contains(prompt, "### 요약 4") {
		t.Fatalf("summary headings missing from prompt: %q", prompt)
	}
	if strings.Contains(prompt, strings.Repeat("가", maxSummaryLn+1)) {
		t.Fatal("long summary was not clipped before crossing the model boundary")
	}
	if !strings.Contains(prompt, strings.Repeat("가", maxSummaryLn)+"…") {
		t.Fatal("clipped summary and ellipsis missing")
	}
}

func TestBoundaryRunWithReportCritiqueFailureMatrix(t *testing.T) {
	source := &recordingSummarySource{nodes: fourBoundaryLeaves()}
	wantErr := errors.New("provider unavailable")
	tests := []struct {
		name      string
		client    *boundaryLLM
		wantError string
	}{
		{
			name:      "stream setup error",
			client:    &boundaryLLM{err: wantErr},
			wantError: wantErr.Error(),
		},
		{
			name:      "nil stream channel",
			client:    &boundaryLLM{nilCh: true},
			wantError: "nil event channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, store := newBoundaryTask(t, source, tt.client)
			report := task.RunWithReport(context.Background())
			if report.Reason != "critique_failed" || !report.Ran || report.Changed {
				t.Fatalf("unexpected report: %#v", report)
			}
			if !strings.Contains(report.Error, tt.wantError) {
				t.Fatalf("error = %q, want substring %q", report.Error, tt.wantError)
			}
			if got := store.Load(); len(got) != 0 {
				t.Fatalf("failure persisted guidelines: %v", got)
			}
		})
	}
}

func TestBoundaryRunDuplicateGuidelinesIsStableNoOp(t *testing.T) {
	source := &recordingSummarySource{nodes: fourBoundaryLeaves()}
	client := &boundaryLLM{reply: `{"guidelines":["금액을 보존하라"]}`}
	task, store := newBoundaryTask(t, source, client)
	if err := store.Save([]string{"금액을 보존하라"}); err != nil {
		t.Fatal(err)
	}

	report := task.RunWithReport(context.Background())
	if report.Reason != "duplicate_guidelines" {
		t.Fatalf("reason = %q, want duplicate_guidelines", report.Reason)
	}
	if !report.Ran || report.Changed {
		t.Fatalf("duplicate report state = %#v", report)
	}
	if report.BeforeCount != 1 || report.AfterCount != 1 {
		t.Fatalf("duplicate count state = %#v", report)
	}
	if len(report.Added) != 0 {
		t.Fatalf("duplicate reported additions: %v", report.Added)
	}
	if got := store.Load(); !reflect.DeepEqual(got, []string{"금액을 보존하라"}) {
		t.Fatalf("store changed: %v", got)
	}
}

type serializedLLM struct {
	active    atomic.Int32
	maxActive atomic.Int32
	calls     atomic.Int32
	release   <-chan struct{}
}

func (f *serializedLLM) StreamChat(ctx context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	f.calls.Add(1)
	active := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		old := f.maxActive.Load()
		if active <= old || f.maxActive.CompareAndSwap(old, active) {
			break
		}
	}
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}

func TestBoundaryConcurrentRunWithReportSerializesCycles(t *testing.T) {
	release := make(chan struct{})
	client := &serializedLLM{release: release}
	source := &recordingSummarySource{nodes: fourBoundaryLeaves()}
	task, _ := newBoundaryTask(t, source, client)

	const workers = 12
	start := make(chan struct{})
	done := make(chan Report, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			done <- task.RunWithReport(context.Background())
		}()
	}
	close(start)

	deadline := time.After(2 * time.Second)
	for client.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("first model call did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// The task mutex must keep all other calls outside StreamChat while the
	// first call is blocked at the provider boundary.
	time.Sleep(20 * time.Millisecond)
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("model calls while first cycle blocked = %d, want 1", got)
	}
	close(release)

	for i := 0; i < workers; i++ {
		select {
		case report := <-done:
			if report.Reason != "no_guidelines" {
				t.Fatalf("worker report = %#v", report)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("worker %d did not complete", i)
		}
	}
	if got := client.maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent provider calls = %d, want 1", got)
	}
	if got := client.calls.Load(); got != workers {
		t.Fatalf("provider calls = %d, want %d", got, workers)
	}
}

func TestBoundaryRunNotificationOnlyAfterChange(t *testing.T) {
	source := &recordingSummarySource{nodes: fourBoundaryLeaves()}
	tests := []struct {
		name       string
		reply      string
		wantNotify int
	}{
		{
			name:       "empty verdict does not notify",
			reply:      `{"guidelines":[]}`,
			wantNotify: 0,
		},
		{
			name:       "new guideline notifies once",
			reply:      `{"guidelines":["금액을 보존하라"]}`,
			wantNotify: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &boundaryLLM{reply: tt.reply}
			task, _ := newBoundaryTask(t, source, client)
			var calls atomic.Int32
			var message string
			task.deps.Notify = func(_ context.Context, msg string) error {
				calls.Add(1)
				message = msg
				return errors.New("delivery errors are recoverable")
			}
			if err := task.Run(context.Background()); err != nil {
				t.Fatalf("Run returned recoverable notification error: %v", err)
			}
			if got := int(calls.Load()); got != tt.wantNotify {
				t.Fatalf("notify calls = %d, want %d", got, tt.wantNotify)
			}
			if tt.wantNotify > 0 {
				if !strings.Contains(message, "압축 요약 보존 지침이 갱신") ||
					!strings.Contains(message, "금액을 보존하라") {
					t.Fatalf("notification missing change context: %q", message)
				}
			}
		})
	}
}

func TestBoundaryTaskIdentityAndLoggerDefault(t *testing.T) {
	task := NewTask(Deps{})
	if task == nil {
		t.Fatal("NewTask returned nil")
	}
	if task.deps.Logger == nil {
		t.Fatal("NewTask did not install a default logger")
	}
	if got := task.Name(); got != "compaction-tuner" {
		t.Fatalf("Name() = %q", got)
	}
	if got := task.Interval(); got != 24*time.Hour {
		t.Fatalf("Interval() = %s", got)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("unavailable periodic task should be a safe no-op: %v", err)
	}
}
