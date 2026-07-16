package mailanalysis

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

type recordingNotifier struct {
	mu       sync.Mutex
	messages []string
	err      error
	block    bool
}

func (n *recordingNotifier) Notify(ctx context.Context, message string) error {
	if n.block {
		<-ctx.Done()
		return ctx.Err()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, message)
	return n.err
}

func (n *recordingNotifier) snapshot() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.messages...)
}

func quietMailLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewServiceDefaultsAndIdentityContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	svc := NewService(Config{}, nil)
	if svc == nil || svc.log == nil || svc.state == nil {
		t.Fatalf("NewService returned incomplete service: %+v", svc)
	}
	if svc.Name() != "gmailpoll" {
		t.Fatalf("Name = %q", svc.Name())
	}
	if svc.Interval() != defaultIntervalMin*time.Minute {
		t.Fatalf("Interval = %v", svc.Interval())
	}
	if svc.cfg.Query != defaultQuery || svc.cfg.MaxPerCycle != defaultMaxPerCycle {
		t.Fatalf("query/max defaults = %q/%d", svc.cfg.Query, svc.cfg.MaxPerCycle)
	}
	if svc.cfg.PromptFile != defaultPromptFile || svc.cfg.ArchiveFolder != "/Deneb-Archive/메일" {
		t.Fatalf("prompt/archive defaults = %q/%q", svc.cfg.PromptFile, svc.cfg.ArchiveFolder)
	}
	wantState := filepath.Join(home, ".deneb", defaultStateFile)
	if svc.state.path != wantState {
		t.Fatalf("default state path = %q, want %q", svc.state.path, wantState)
	}
}

func TestNewServicePreservesExplicitConfiguration(t *testing.T) {
	client := llm.NewClient("http://example.test", "key")
	stateDir := t.TempDir()
	cfg := Config{
		IntervalMin:   7,
		Query:         "label:ops is:unread",
		MaxPerCycle:   19,
		Model:         "analysis-model",
		PromptFile:    "/tmp/prompt.md",
		StateDir:      stateDir,
		LLMClient:     client,
		ArchiveFolder: "/Archive/Mail",
	}
	svc := NewService(cfg, quietMailLogger())
	if svc.Interval() != 7*time.Minute || svc.cfg.Query != cfg.Query || svc.cfg.MaxPerCycle != 19 {
		t.Fatalf("explicit schedule config changed: %+v", svc.cfg)
	}
	if svc.llmClient != client || svc.cfg.Model != "analysis-model" {
		t.Fatalf("explicit model/client changed: model=%q client=%p", svc.cfg.Model, svc.llmClient)
	}
	if svc.state.path != filepath.Join(stateDir, defaultStateFile) {
		t.Fatalf("state path = %q", svc.state.path)
	}
	if svc.cfg.ArchiveFolder != "/Archive/Mail" {
		t.Fatalf("archive folder = %q", svc.cfg.ArchiveFolder)
	}
}

func TestServicePipelineDepsForwardsConfigWithAndWithoutGmail(t *testing.T) {
	mainClient := llm.NewClient("http://main.example.test", "main-key")
	localClient := llm.NewClient("http://local.example.test", "local-key")
	projectsCalls := 0
	senderCalls := 0
	counterpartyCalls := 0
	extractCalls := 0
	agentCalls := 0
	sinkCalls := 0

	cfg := Config{
		LLMClient:   mainClient,
		LocalClient: localClient,
		LocalModel:  "local-model",
		Model:       "main-model",
		PromptOverride: func(id string) (string, bool) {
			if id != PromptIDAutoMailAnalysis {
				t.Errorf("prompt id = %q", id)
			}
			return " native prompt ", true
		},
		ProjectsFn: func() []ProjectCandidate {
			projectsCalls++
			return []ProjectCandidate{{Path: "projects/a.md"}}
		},
		SenderFactsFn: func(context.Context, string) string {
			senderCalls++
			return "facts"
		},
		CounterpartyProjectsFn: func(string) []string {
			counterpartyCalls++
			return []string{"Alpha"}
		},
		AttachmentExtractFn: func(context.Context, []byte, string, string) string {
			extractCalls++
			return "text"
		},
		ThinkingKwarg: "thinking",
		ThreadSource: threadSourceFunc(func(context.Context, *gmail.MessageDetail) ([]*gmail.MessageDetail, error) {
			return nil, nil
		}),
		AgentSynthesisFn: func(context.Context, string) (string, error) {
			agentCalls++
			return "analysis", nil
		},
		MailStoreSink: func(mailarchive.ContextMessage) (bool, error) {
			sinkCalls++
			return true, nil
		},
	}
	svc := NewService(cfg, quietMailLogger())

	withoutGmail := svc.pipelineDeps(nil)
	if withoutGmail.LLMClient != mainClient || withoutGmail.LocalClient != localClient {
		t.Fatal("LLM clients were not forwarded")
	}
	if withoutGmail.LocalModel != "local-model" || withoutGmail.MainModel != "main-model" {
		t.Fatalf("models = %q/%q", withoutGmail.LocalModel, withoutGmail.MainModel)
	}
	if withoutGmail.AnalysisPrompt != "native prompt" || withoutGmail.ThinkingKwarg != "thinking" {
		t.Fatalf("prompt/thinking = %q/%q", withoutGmail.AnalysisPrompt, withoutGmail.ThinkingKwarg)
	}
	if withoutGmail.AttachmentBytesFn != nil {
		t.Fatal("LMTP deps unexpectedly inherited a Gmail attachment fetcher")
	}
	if withoutGmail.ProjectsFn == nil || len(withoutGmail.ProjectsFn()) != 1 || projectsCalls != 1 {
		t.Fatalf("projects forwarding failed, calls=%d", projectsCalls)
	}
	if withoutGmail.SenderFactsFn(context.Background(), "sender") != "facts" || senderCalls != 1 {
		t.Fatalf("sender facts forwarding failed, calls=%d", senderCalls)
	}
	if got := withoutGmail.CounterpartyProjectsFn("vendor.test"); !reflect.DeepEqual(got, []string{"Alpha"}) || counterpartyCalls != 1 {
		t.Fatalf("counterparty projects = %#v, calls=%d", got, counterpartyCalls)
	}
	if got := withoutGmail.AttachmentExtractFn(context.Background(), []byte("x"), "x.pdf", "application/pdf"); got != "text" || extractCalls != 1 {
		t.Fatalf("attachment extraction = %q, calls=%d", got, extractCalls)
	}
	if got, err := withoutGmail.AgentSynthesisFn(context.Background(), "prompt"); err != nil || got != "analysis" || agentCalls != 1 {
		t.Fatalf("agent synthesis = %q/%v calls=%d", got, err, agentCalls)
	}
	if _, err := cfg.MailStoreSink(mailarchive.ContextMessage{}); err != nil || sinkCalls != 1 {
		t.Fatalf("mail sink calls=%d err=%v", sinkCalls, err)
	}

	withGmail := svc.pipelineDeps(&gmail.Client{})
	if withGmail.GmailClient == nil || withGmail.AttachmentBytesFn == nil {
		t.Fatal("poll deps did not install Gmail attachment fetcher")
	}
}

type threadSourceFunc func(context.Context, *gmail.MessageDetail) ([]*gmail.MessageDetail, error)

func (f threadSourceFunc) RelatedMessages(ctx context.Context, msg *gmail.MessageDetail) ([]*gmail.MessageDetail, error) {
	return f(ctx, msg)
}

func TestServiceNotificationOutcomesContract(t *testing.T) {
	svc := NewService(Config{}, quietMailLogger())
	if svc.sendNotification(context.Background(), "unroutable") {
		t.Fatal("notification without notifier reported success")
	}

	success := &recordingNotifier{}
	svc.SetNotifier(success)
	if !svc.sendNotification(context.Background(), " first ") {
		t.Fatal("successful notifier reported failure")
	}
	if got := success.snapshot(); !reflect.DeepEqual(got, []string{" first "}) {
		t.Fatalf("messages = %#v", got)
	}

	failure := &recordingNotifier{err: errors.New("delivery rejected")}
	svc.SetNotifier(failure)
	if svc.sendNotification(context.Background(), "second") {
		t.Fatal("failing notifier reported success")
	}
	if got := failure.snapshot(); !reflect.DeepEqual(got, []string{"second"}) {
		t.Fatalf("failed delivery was not attempted: %#v", got)
	}

	blocked := &recordingNotifier{block: true}
	svc.SetNotifier(blocked)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if svc.sendNotification(ctx, "cancelled") {
		t.Fatal("cancelled notification reported success")
	}
}

func TestServiceNotifierSwapIsRaceSafe(t *testing.T) {
	svc := NewService(Config{}, quietMailLogger())
	first := &recordingNotifier{}
	second := &recordingNotifier{}
	svc.SetNotifier(first)

	const sends = 80
	var wg sync.WaitGroup
	for i := 0; i < sends; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				svc.SetNotifier(first)
			} else {
				svc.SetNotifier(second)
			}
		}(i)
		go func() {
			defer wg.Done()
			_ = svc.sendNotification(context.Background(), "message")
		}()
	}
	wg.Wait()
	if got := len(first.snapshot()) + len(second.snapshot()); got != sends {
		t.Fatalf("delivered messages = %d, want %d", got, sends)
	}
}

func TestMarkSkippedAnalysesReportsOnlyMissingMessages(t *testing.T) {
	type failure struct {
		id  string
		err error
	}
	var failures []failure
	svc := NewService(Config{OnAnalysisFailed: func(msg *gmail.MessageDetail, err error) {
		failures = append(failures, failure{id: msg.ID, err: err})
	}}, quietMailLogger())
	details := []*gmail.MessageDetail{
		{ID: "analyzed"},
		nil,
		{ID: ""},
		{ID: "failed-a"},
		{ID: "failed-b"},
	}
	items := []BatchItem{{Msg: &gmail.MessageDetail{ID: "analyzed"}}, {Msg: nil}, {Msg: &gmail.MessageDetail{}}}
	wantErr := errors.New("batch unavailable")
	svc.markSkippedAnalyses(details, items, wantErr)
	if len(failures) != 2 || failures[0].id != "failed-a" || failures[1].id != "failed-b" {
		t.Fatalf("failures = %+v", failures)
	}
	if !errors.Is(failures[0].err, wantErr) || !errors.Is(failures[1].err, wantErr) {
		t.Fatalf("failure causes = %+v", failures)
	}
}

func TestMarkSkippedAnalysesSynthesizesPerItemFailure(t *testing.T) {
	var gotErr error
	svc := NewService(Config{OnAnalysisFailed: func(_ *gmail.MessageDetail, err error) {
		gotErr = err
	}}, quietMailLogger())
	svc.markSkippedAnalyses([]*gmail.MessageDetail{{ID: "failed"}}, nil, nil)
	if gotErr == nil || gotErr.Error() != "individual email analysis failed" {
		t.Fatalf("synthetic failure = %v", gotErr)
	}

	gotErr = nil
	svc.markSkippedAnalyses(nil, nil, nil)
	if gotErr != nil {
		t.Fatalf("empty details emitted callback: %v", gotErr)
	}
}

func TestSearchMessagesRetriesTransientFailuresWithoutChangingRequest(t *testing.T) {
	svc := NewService(Config{Query: "label:ops is:unread", MaxPerCycle: 3}, quietMailLogger())
	attempts := 0
	var retryAttempts []int
	messages, err := svc.searchMessages(
		context.Background(),
		func(_ context.Context, query string, maxResults int) ([]gmail.MessageSummary, error) {
			attempts++
			if query != "label:ops is:unread" || maxResults != 13 {
				t.Fatalf("search request changed on attempt %d: query=%q max=%d", attempts, query, maxResults)
			}
			if attempts < 3 {
				return nil, errors.New("temporary search failure")
			}
			return []gmail.MessageSummary{{ID: "mail-1"}}, nil
		},
		func(attempt int) time.Duration {
			retryAttempts = append(retryAttempts, attempt)
			return 0
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || !reflect.DeepEqual(retryAttempts, []int{0, 1}) {
		t.Fatalf("attempts=%d retryAttempts=%v", attempts, retryAttempts)
	}
	if len(messages) != 1 || messages[0].ID != "mail-1" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestSelectNewMessagesFiltersSeenIDsWithCycleCap(t *testing.T) {
	svc := NewService(Config{MaxPerCycle: 2}, quietMailLogger())
	state := &PollState{SeenIDs: []string{"seen"}}
	messages := []gmail.MessageSummary{
		{ID: "seen"},
		{ID: "new-a"},
		{ID: "new-b"},
		{ID: "new-c"},
	}

	got := svc.selectNewMessages(messages, state)
	want := []gmail.MessageSummary{{ID: "new-a"}, {ID: "new-b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected messages = %+v, want %+v", got, want)
	}
}

func TestFetchMessageDetailsTreatsFetchFailureAsTerminal(t *testing.T) {
	stateDir := t.TempDir()
	var failed *gmail.MessageDetail
	svc := NewService(Config{
		StateDir: stateDir,
		OnAnalysisFailed: func(msg *gmail.MessageDetail, _ error) {
			failed = msg
		},
	}, quietMailLogger())
	state := &PollState{}
	messages := []gmail.MessageSummary{
		{ID: "available", Subject: "available subject"},
		{ID: "unreadable", ThreadID: "thread-2", From: "sender", Subject: "broken", Date: "date"},
	}

	details := svc.fetchMessageDetails(
		context.Background(),
		func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			if id == "unreadable" {
				return nil, errors.New("body unavailable")
			}
			return &gmail.MessageDetail{ID: id, Subject: "available subject"}, nil
		},
		messages,
		state,
	)
	if len(details) != 1 || details[0].ID != "available" {
		t.Fatalf("details = %+v", details)
	}
	if !state.hasSeen("unreadable") || state.hasSeen("available") {
		t.Fatalf("seen ids after fetch = %v", state.SeenIDs)
	}
	if failed == nil || failed.ID != "unreadable" || failed.ThreadID != "thread-2" || failed.Subject != "broken" {
		t.Fatalf("failure callback message = %+v", failed)
	}
}

func TestResolvePollAnalysisTotalFailureLeavesFetchedMessagesRetryable(t *testing.T) {
	wantErr := errors.New("analysis backend unavailable")
	var failures []string
	svc := NewService(Config{OnAnalysisFailed: func(msg *gmail.MessageDetail, err error) {
		if !errors.Is(err, wantErr) {
			t.Errorf("failure cause = %v, want %v", err, wantErr)
		}
		failures = append(failures, msg.ID)
	}}, quietMailLogger())
	details := []*gmail.MessageDetail{{ID: "mail-a"}, {ID: "mail-b"}}

	report, proceed := svc.resolvePollAnalysis(details, "discarded", nil, wantErr)
	if proceed || report != "" {
		t.Fatalf("total failure resolution = report %q proceed %v", report, proceed)
	}
	if !reflect.DeepEqual(failures, []string{"mail-a", "mail-b"}) {
		t.Fatalf("failure callbacks = %v", failures)
	}
}

func TestResolvePollAnalysisPartialFailureMarksOnlySuccessfulItemsSeen(t *testing.T) {
	var failedIDs []string
	svc := NewService(Config{OnAnalysisFailed: func(msg *gmail.MessageDetail, err error) {
		if err == nil || err.Error() != "individual email analysis failed" {
			t.Errorf("partial failure cause = %v", err)
		}
		failedIDs = append(failedIDs, msg.ID)
	}}, quietMailLogger())
	details := []*gmail.MessageDetail{{ID: "analyzed"}, {ID: "retry"}}
	items := []BatchItem{{Msg: details[0], Result: AnalysisResult{Text: "analysis"}}}

	report, proceed := svc.resolvePollAnalysis(details, "", items, errors.New("consolidation failed"))
	if !proceed || report != "(분석 실패)" {
		t.Fatalf("partial failure resolution = report %q proceed %v", report, proceed)
	}
	if !reflect.DeepEqual(failedIDs, []string{"retry"}) {
		t.Fatalf("partial failure callbacks = %v", failedIDs)
	}

	state := &PollState{}
	markAnalyzedMessagesSeen(state, []gmail.MessageSummary{{ID: "analyzed"}, {ID: "retry"}}, items)
	if !state.hasSeen("analyzed") || state.hasSeen("retry") {
		t.Fatalf("partial failure seen ids = %v", state.SeenIDs)
	}
}

func TestServiceDiaryLoggingContract(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(Config{DiaryDir: dir}, quietMailLogger())
	svc.logToDiary(3, "중요한 통합 보고서")

	path := filepath.Join(dir, "diary-"+time.Now().Format("2006-01-02")+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "📬 메일 분석 (3건)") || !strings.Contains(text, "중요한 통합 보고서") {
		t.Fatalf("diary content = %q", text)
	}

	before := append([]byte(nil), data...)
	NewService(Config{}, quietMailLogger()).logToDiary(1, "must not be written")
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("disabled diary changed file: err=%v before=%q after=%q", err, before, after)
	}
}

func TestIngestMessageDeliveryPipelineContract(t *testing.T) {
	q := newQueuedOpenAIServer(t, "분석 결과\nIMPORTANCE: 긴급")
	defer q.Close()

	var analyzedID string
	var analyzed AnalysisResult
	var delivered [][]string
	svc := NewService(Config{
		LLMClient: q.Client(),
		Model:     "main",
		OnAnalyzed: func(msg *gmail.MessageDetail, res AnalysisResult) error {
			analyzedID = msg.ID
			analyzed = res
			return nil
		},
		OnDelivered: func(ids []string) {
			delivered = append(delivered, append([]string(nil), ids...))
		},
	}, quietMailLogger())
	notifier := &recordingNotifier{}
	svc.SetNotifier(notifier)

	msg := &gmail.MessageDetail{
		ID:      "mail-1",
		From:    "Vendor <vendor@example.test>",
		To:      "me@example.test",
		Subject: "승인 요청",
		Date:    "Sat, 11 Jul 2026 10:00:00 +0900",
		Body:    "승인 부탁드립니다.",
	}
	res, err := svc.IngestMessage(context.Background(), msg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "분석 결과" || res.Importance != "urgent" {
		t.Fatalf("result = %+v", res)
	}
	if analyzedID != msg.ID || analyzed.Text != res.Text {
		t.Fatalf("OnAnalyzed = id=%q result=%+v", analyzedID, analyzed)
	}
	if got := notifier.snapshot(); !reflect.DeepEqual(got, []string{"분석 결과"}) {
		t.Fatalf("notifications = %#v", got)
	}
	if !reflect.DeepEqual(delivered, [][]string{{"mail-1"}}) {
		t.Fatalf("delivered = %#v", delivered)
	}
}

func TestIngestMessageSenderReviewStopsBeforeLLMButKeepsBodyReadable(t *testing.T) {
	var decided, reviewed *gmail.MessageDetail
	var archived mailarchive.ContextMessage
	svc := NewService(Config{
		SenderTrustFn: func(msg *gmail.MessageDetail) SenderTrustDecision {
			decided = msg
			return SenderTrustDecision{Disposition: SenderReview, Reason: "  unknown\n sender  "}
		},
		OnSenderReview: func(msg *gmail.MessageDetail, decision SenderTrustDecision) error {
			reviewed = msg
			if decision.Reason != "unknown sender" {
				t.Fatalf("review reason = %q", decision.Reason)
			}
			return nil
		},
		MailStoreSink: func(msg mailarchive.ContextMessage) (bool, error) {
			archived = msg
			return true, nil
		},
	}, quietMailLogger())
	notifier := &recordingNotifier{}
	svc.SetNotifier(notifier)

	msg := &gmail.MessageDetail{
		ID:          "review-1",
		From:        "Unknown <unknown@example.test>",
		Subject:     "do not ingest",
		Body:        "private body",
		Attachments: []gmail.AttachmentInfo{{Filename: "secret.pdf"}},
		LargeAttachments: []gmail.LargeAttachmentRef{{
			URL: "https://files.example.test/secret",
		}},
	}
	res, err := svc.IngestMessage(context.Background(), msg, nil)
	if err != nil || !reflect.DeepEqual(res, AnalysisResult{}) {
		t.Fatalf("review ingest = %+v/%v", res, err)
	}
	for name, got := range map[string]*gmail.MessageDetail{"decision": decided, "review": reviewed} {
		if got == nil || got.ID != msg.ID || got.From != msg.From {
			t.Fatalf("%s metadata = %+v", name, got)
		}
		if got.Body != "" || len(got.Attachments) != 0 || len(got.LargeAttachments) != 0 {
			t.Fatalf("%s crossed metadata-only boundary: %+v", name, got)
		}
	}
	// Operator review needs the body; trust only gates autonomous LLM intake.
	if archived.ID == "" || archived.Body != msg.Body || len(notifier.snapshot()) != 0 {
		t.Fatalf("review archive must keep body and skip notify: archive=%+v notify=%#v", archived, notifier.snapshot())
	}
}

func TestIngestMessageReviewRequiresDurableSink(t *testing.T) {
	svc := NewService(Config{SenderTrustFn: func(*gmail.MessageDetail) SenderTrustDecision {
		return SenderTrustDecision{Disposition: SenderReview}
	}}, quietMailLogger())
	_, err := svc.IngestMessage(context.Background(), &gmail.MessageDetail{ID: "review"}, nil)
	if err == nil || !strings.Contains(err.Error(), "review sink") {
		t.Fatalf("missing review sink error = %v", err)
	}
}

func TestPartitionPollMessagesPersistsReviewsWithoutFetchingBodies(t *testing.T) {
	var reviewed []string
	svc := NewService(Config{
		SenderTrustFn: func(msg *gmail.MessageDetail) SenderTrustDecision {
			if strings.Contains(msg.From, "unknown") {
				return SenderTrustDecision{Disposition: SenderReview, Reason: "unknown"}
			}
			return SenderTrustDecision{Disposition: SenderTrusted}
		},
		OnSenderReview: func(msg *gmail.MessageDetail, _ SenderTrustDecision) error {
			reviewed = append(reviewed, msg.ID)
			return nil
		},
	}, quietMailLogger())
	state := &PollState{}
	trusted, count := svc.partitionPollMessages([]gmail.MessageSummary{
		{ID: "known", From: "known@example.test", Subject: "known"},
		{ID: "unknown", From: "unknown@example.test", Subject: "unknown", Snippet: "must stay out"},
	}, state)
	if count != 1 || len(trusted) != 1 || trusted[0].ID != "known" {
		t.Fatalf("partition = trusted=%+v count=%d", trusted, count)
	}
	if !reflect.DeepEqual(reviewed, []string{"unknown"}) || !state.hasSeen("unknown") || state.hasSeen("known") {
		t.Fatalf("reviewed=%v seen=%v", reviewed, state.SeenIDs)
	}
}

func TestIngestMessageAnalysisFailureIsObservable(t *testing.T) {
	q := newQueuedOpenAIServer(t, "upstream unavailable")
	q.statuses = []int{503}
	defer q.Close()

	var failedMsg *gmail.MessageDetail
	var failedErr error
	svc := NewService(Config{
		LLMClient: q.Client(),
		Model:     "main",
		OnAnalysisFailed: func(msg *gmail.MessageDetail, err error) {
			failedMsg, failedErr = msg, err
		},
	}, quietMailLogger())
	notifier := &recordingNotifier{}
	svc.SetNotifier(notifier)
	msg := &gmail.MessageDetail{ID: "mail-fail", Body: "body"}

	if _, err := svc.IngestMessage(context.Background(), msg, nil); err == nil {
		t.Fatal("failed analysis returned nil error")
	}
	if failedMsg != msg || failedErr == nil {
		t.Fatalf("failure callback = msg=%p err=%v", failedMsg, failedErr)
	}
	if got := notifier.snapshot(); len(got) != 0 {
		t.Fatalf("failed analysis notified: %#v", got)
	}
}

func TestIngestMessageSinkFailureStopsDelivery(t *testing.T) {
	q := newQueuedOpenAIServer(t, "valid analysis")
	defer q.Close()
	wantErr := errors.New("persistence unavailable")
	delivered := false
	svc := NewService(Config{
		LLMClient: q.Client(),
		Model:     "main",
		OnAnalyzed: func(*gmail.MessageDetail, AnalysisResult) error {
			return wantErr
		},
		OnDelivered: func([]string) { delivered = true },
	}, quietMailLogger())
	notifier := &recordingNotifier{}
	svc.SetNotifier(notifier)

	_, err := svc.IngestMessage(context.Background(), &gmail.MessageDetail{ID: "mail", Body: "body"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("sink error = %v", err)
	}
	if delivered || len(notifier.snapshot()) != 0 {
		t.Fatalf("sink failure leaked delivery: delivered=%v messages=%#v", delivered, notifier.snapshot())
	}
}

func TestMailAnalysisRejectsNilRuntimeDependencies(t *testing.T) {
	msg := &gmail.MessageDetail{ID: "mail", Body: "body"}
	if _, err := AnalyzeEmail(context.Background(), nil, "model", "prompt", "", nil, msg); err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("AnalyzeEmail nil client error = %v", err)
	}
	client := llm.NewClient("http://example.test", "key")
	if _, err := AnalyzeEmail(context.Background(), client, "model", "prompt", "", nil, nil); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("AnalyzeEmail nil message error = %v", err)
	}
	if _, err := AnalyzeEmailPipeline(context.Background(), PipelineDeps{}, msg); err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("pipeline nil client error = %v", err)
	}
	if _, err := AnalyzeEmailPipeline(context.Background(), PipelineDeps{LLMClient: client}, nil); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("pipeline nil message error = %v", err)
	}
	if _, err := NewService(Config{LLMClient: client}, quietMailLogger()).IngestMessage(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("ingest nil message error = %v", err)
	}
	if got := FormatEmailForAnalysis(nil); got != "" {
		t.Fatalf("nil formatted email = %q", got)
	}
}

func TestAnalyzeBatchSkipsNilEntriesWithoutPanicking(t *testing.T) {
	q := newQueuedOpenAIServer(t, "single result")
	defer q.Close()
	deps := PipelineDeps{LLMClient: q.Client(), MainModel: "main", Logger: quietMailLogger()}
	msg := &gmail.MessageDetail{ID: "valid", Body: "body"}
	report, items, err := AnalyzeBatch(context.Background(), deps, []*gmail.MessageDetail{nil, msg, nil})
	if err != nil || report != "single result" || len(items) != 1 || items[0].Msg != msg {
		t.Fatalf("batch = report=%q items=%+v err=%v", report, items, err)
	}

	if report, items, err := AnalyzeBatch(context.Background(), deps, []*gmail.MessageDetail{nil}); err == nil || report != "" || items != nil {
		t.Fatalf("all-nil batch = report=%q items=%+v err=%v", report, items, err)
	}
}

func TestSynthesizeBatchReportRejectsInvalidInputs(t *testing.T) {
	if _, err := synthesizeBatchReport(context.Background(), PipelineDeps{}, nil); err == nil || !strings.Contains(err.Error(), "no analyzed") {
		t.Fatalf("empty items error = %v", err)
	}
	item := BatchItem{Msg: &gmail.MessageDetail{Body: "body"}, Result: AnalysisResult{Text: "analysis"}}
	if _, err := synthesizeBatchReport(context.Background(), PipelineDeps{}, []BatchItem{item}); err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("nil client error = %v", err)
	}
	client := llm.NewClient("http://example.test", "key")
	if _, err := synthesizeBatchReport(context.Background(), PipelineDeps{LLMClient: client}, []BatchItem{{}}); err == nil || !strings.Contains(err.Error(), "no message") {
		t.Fatalf("nil item message error = %v", err)
	}
}

func TestRunFinalSynthesisAgentOnlyAndMissingFallback(t *testing.T) {
	got, err := runFinalSynthesis(context.Background(), PipelineDeps{AgentSynthesisFn: func(context.Context, string) (string, error) {
		return "agent-only result", nil
	}}, "prompt", 123)
	if err != nil || got != "agent-only result" {
		t.Fatalf("agent-only synthesis = %q/%v", got, err)
	}
	_, err = runFinalSynthesis(context.Background(), PipelineDeps{AgentSynthesisFn: func(context.Context, string) (string, error) {
		return "", errors.New("agent failed")
	}}, "prompt", 123)
	if err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("missing fallback client error = %v", err)
	}
}

func TestAttachmentGateFetchExtractAndJudgeContract(t *testing.T) {
	q := newQueuedOpenAIServer(t, `{"selections":[{"index":2,"relevant":true},{"index":99,"relevant":true},{"index":1,"relevant":false}]}`)
	defer q.Close()

	var fetched []string
	var extracted []string
	deps := PipelineDeps{
		LocalClient: q.Client(),
		LocalModel:  "local",
		Logger:      quietMailLogger(),
		AttachmentBytesFn: func(_ context.Context, messageID, attachmentID string) ([]byte, error) {
			if messageID != "mail" {
				t.Errorf("message id = %q", messageID)
			}
			fetched = append(fetched, attachmentID)
			return []byte("bytes-" + attachmentID), nil
		},
		AttachmentExtractFn: func(_ context.Context, data []byte, filename, mimeType string) string {
			extracted = append(extracted, filename+"|"+mimeType+"|"+string(data))
			return "TEXT " + filename
		},
	}
	msg := &gmail.MessageDetail{
		ID:      "mail",
		From:    "vendor@example.test",
		Subject: "documents",
		Body:    "please review",
		Attachments: []gmail.AttachmentInfo{
			{Filename: "견적서.pdf", MimeType: "application/pdf", AttachmentID: "quote", Size: 3000},
			{Filename: "notes.pdf", MimeType: "application/pdf", AttachmentID: "notes", Size: 3000},
			{Filename: "scan.png", MimeType: "image/png", AttachmentID: "scan", Size: 3000},
		},
	}
	sel := gateAndExtractAttachments(context.Background(), deps, msg)
	if !reflect.DeepEqual(fetched, []string{"quote", "notes", "scan"}) || len(extracted) != 3 {
		t.Fatalf("fetch/extract = %#v/%#v", fetched, extracted)
	}
	if !strings.Contains(sel.Injected, "견적서.pdf") || !strings.Contains(sel.Injected, "scan.png") {
		t.Fatalf("selected content = %q", sel.Injected)
	}
	if strings.Contains(sel.Injected, "notes.pdf") {
		t.Fatalf("judge-rejected notes injected: %q", sel.Injected)
	}
	if len(sel.Truncated) != 0 {
		t.Fatalf("unexpected truncation = %#v", sel.Truncated)
	}
}

func TestAttachmentGateFailureOpensForAllExtractedDocuments(t *testing.T) {
	q := newQueuedOpenAIServer(t, "not-json", "still-not-json")
	defer q.Close()
	deps := PipelineDeps{
		LocalClient: q.Client(),
		LocalModel:  "local",
		Logger:      quietMailLogger(),
		AttachmentBytesFn: func(context.Context, string, string) ([]byte, error) {
			return []byte("bytes"), nil
		},
		AttachmentExtractFn: func(_ context.Context, _ []byte, filename, _ string) string {
			return "content of " + filename
		},
	}
	msg := &gmail.MessageDetail{ID: "mail", Attachments: []gmail.AttachmentInfo{
		{Filename: "ordinary.pdf", AttachmentID: "a", Size: 3000},
		{Filename: "photo.jpg", AttachmentID: "b", Size: 3000},
	}}
	sel := gateAndExtractAttachments(context.Background(), deps, msg)
	if !strings.Contains(sel.Injected, "ordinary.pdf") || !strings.Contains(sel.Injected, "photo.jpg") {
		t.Fatalf("failure-open selection = %q", sel.Injected)
	}
}

func TestAttachmentGateSkipsIndividualFetchAndExtractionFailures(t *testing.T) {
	q := newQueuedOpenAIServer(t, `{"selections":[{"index":0,"relevant":true}]}`)
	defer q.Close()
	deps := PipelineDeps{
		LocalClient: q.Client(),
		LocalModel:  "local",
		Logger:      quietMailLogger(),
		AttachmentBytesFn: func(_ context.Context, _ string, id string) ([]byte, error) {
			switch id {
			case "fetch-error":
				return nil, errors.New("unavailable")
			case "empty":
				return nil, nil
			default:
				return []byte("ok"), nil
			}
		},
		AttachmentExtractFn: func(_ context.Context, _ []byte, filename, _ string) string {
			if filename == "blank.pdf" {
				return "  "
			}
			return "usable text"
		},
	}
	msg := &gmail.MessageDetail{ID: "mail", Attachments: []gmail.AttachmentInfo{
		{Filename: "broken.pdf", AttachmentID: "fetch-error", Size: 3000},
		{Filename: "empty.pdf", AttachmentID: "empty", Size: 3000},
		{Filename: "blank.pdf", AttachmentID: "blank", Size: 3000},
		{Filename: "useful.pdf", AttachmentID: "good", Size: 3000},
	}}
	sel := gateAndExtractAttachments(context.Background(), deps, msg)
	if !strings.Contains(sel.Injected, "useful.pdf") || strings.Contains(sel.Injected, "broken.pdf") || strings.Contains(sel.Injected, "blank.pdf") {
		t.Fatalf("selection = %q", sel.Injected)
	}
}

func TestClipCharsNonPositiveLimitContract(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		if got := clipChars("가나다", n); got != "" {
			t.Errorf("clipChars limit %d = %q", n, got)
		}
	}
}

func TestStateStoreDefaultPathAndNilStateGuards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := newStateStore("   ")
	if want := filepath.Join(home, ".deneb", defaultStateFile); store.path != want {
		t.Fatalf("default path = %q, want %q", store.path, want)
	}
	if err := store.Save(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("Save(nil) error = %v", err)
	}
	var nilState *PollState
	if nilState.hasSeen("x") {
		t.Fatal("nil state reported seen id")
	}
	nilState.markSeen("x")
}

func TestPollStateMarkSeenDeduplicatesAndRebuildsIndex(t *testing.T) {
	state := &PollState{SeenIDs: []string{"existing"}}
	if !state.hasSeen("existing") {
		t.Fatal("hasSeen did not rebuild missing index")
	}
	state.markSeen("existing")
	state.markSeen("")
	state.markSeen("new")
	state.markSeen("new")
	if !reflect.DeepEqual(state.SeenIDs, []string{"existing", "new"}) {
		t.Fatalf("seen ids = %#v", state.SeenIDs)
	}
	if !state.hasSeen("new") || state.hasSeen("") {
		t.Fatalf("seen lookup mismatch: new=%v empty=%v", state.hasSeen("new"), state.hasSeen(""))
	}
}
