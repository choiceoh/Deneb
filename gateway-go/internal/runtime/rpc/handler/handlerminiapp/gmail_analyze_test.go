package handlerminiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeAnalyzePipeline struct {
	analyzeFn func(ctx context.Context, msg *gmail.MessageDetail) (gmailpoll.AnalysisResult, error)
}

func (f *fakeAnalyzePipeline) Analyze(ctx context.Context, msg *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
	if f.analyzeFn == nil {
		return gmailpoll.AnalysisResult{}, errors.New("Analyze not stubbed")
	}
	return f.analyzeFn(ctx, msg)
}

func analyzeDeps(client GmailClient, pipeline AnalyzePipeline) GmailAnalyzeDeps {
	return GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return client, nil },
		Pipeline: func() (AnalyzePipeline, error) { return pipeline, nil },
	}
}

func TestGmailAnalyze_HappyPath(t *testing.T) {
	var seenID string
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			seenID = id
			return &gmail.MessageDetail{
				ID:      id,
				From:    "Alice <alice@example.com>",
				Subject: "Meeting tomorrow",
				Date:    "Mon, 26 May 2026 14:30:00 +0900",
				Body:    "Hi, can we sync at 2pm?",
			}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, msg *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			if msg.ID != "m1" {
				t.Errorf("pipeline got id=%q, want m1", msg.ID)
			}
			return gmailpoll.AnalysisResult{Text: "## 핵심 요약\n회의 일정 조율 요청.\n"}, nil
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	var got map[string]any
	decode(t, resp, &got)

	if seenID != "m1" {
		t.Errorf("client.GetMessage id = %q, want m1", seenID)
	}
	if got["id"] != "m1" || got["subject"] != "Meeting tomorrow" {
		t.Errorf("payload fields wrong: %+v", got)
	}
	if !strings.Contains(got["analysis"].(string), "핵심 요약") {
		t.Errorf("analysis missing expected content: %v", got["analysis"])
	}
	if _, ok := got["durationMs"].(float64); !ok {
		t.Errorf("durationMs missing/wrong type: %+v", got["durationMs"])
	}
}

func TestGmailAnalyze_MissingID(t *testing.T) {
	h := gmailAnalyze(analyzeDeps(&fakeGmailClient{}, &fakeAnalyzePipeline{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestGmailAnalyze_RequiresAuth(t *testing.T) {
	h := gmailAnalyze(analyzeDeps(&fakeGmailClient{}, &fakeAnalyzePipeline{}))
	resp := h(context.Background(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestGmailAnalyze_GmailGetNotFound(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return nil, errors.New("HTTP 404: Not Found")
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, &fakeAnalyzePipeline{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "missing"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want NOT_FOUND", resp.Error.Code)
	}
}

func TestGmailAnalyze_PipelineFailure(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1"}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			return gmailpoll.AnalysisResult{}, errors.New("LLM call timed out")
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestGmailAnalyze_EmptyAnalysisIsRejected(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1"}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			return gmailpoll.AnalysisResult{Text: "   \n  "}, nil // whitespace only
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error for empty analysis")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

// --- cache + wiki sink coverage --------------------------------------------
//
// CacheMiss + CacheHit + Force are covered further down by
// TestGmailAnalyze_CacheMiss_RunsLLMAndPersists,
// TestGmailAnalyze_CacheHit_SkipsLLMAndWiki, and
// TestGmailAnalyze_Force_BypassesCache. The wiki-failure-is-non-fatal
// contract has no sibling assertion, so it lives here.

// TestGmailAnalyze_WikiSinkFailure_NonFatal: if the wiki sink returns an
// error, the LLM result must still surface to the caller as success.
func TestGmailAnalyze_WikiSinkFailure_NonFatal(t *testing.T) {
	deps := analyzeDeps(
		&fakeGmailClient{getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: id}, nil
		}},
		&fakeAnalyzePipeline{analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			return gmailpoll.AnalysisResult{Text: "result"}, nil
		}},
	)
	deps.SaveToWiki = func(WikiAnalysisInput) error { return errors.New("disk full") }
	h := gmailAnalyze(deps)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("expected OK despite wiki failure, got %+v", resp.Error)
	}
}

func TestGmailAnalyze_PipelineFactoryError(t *testing.T) {
	deps := GmailAnalyzeDeps{
		Client: func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) {
			return nil, ErrAnalyzeNoLLM
		},
	}
	h := gmailAnalyze(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

// Cache hit short-circuits before client/pipeline are touched. Verified by
// returning errors from both factories — if the handler reaches them, the
// test fails. Also confirms the wiki sink is NOT invoked on a hit (we don't
// want to re-write the wiki page every time the operator reopens the mail).
func TestGmailAnalyze_CacheHit_SkipsLLMAndWiki(t *testing.T) {
	cacheDir := t.TempDir()
	cache := NewAnalysisStore(cacheDir)
	if err := cache.save(&analysisRecord{
		MsgID:         "m1",
		Subject:       "stored subject",
		From:          "cached@example.com",
		Date:          "2026-05-27T10:00:00Z",
		Analysis:      "## 저장된 분석\n캐시에서 나왔다.",
		DurationMs:    42,
		PromptVersion: AnalysisPromptVersion,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	wikiCalls := 0
	deps := GmailAnalyzeDeps{
		Client: func() (GmailClient, error) {
			t.Fatal("client factory must not be called on cache hit")
			return nil, nil
		},
		Pipeline: func() (AnalyzePipeline, error) {
			t.Fatal("pipeline factory must not be called on cache hit")
			return nil, nil
		},
		Cache: cache,
		SaveToWiki: func(WikiAnalysisInput) error {
			wikiCalls++
			return nil
		},
	}
	h := gmailAnalyze(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("expected OK on cache hit, got error %+v", resp.Error)
	}
	var got map[string]any
	decode(t, resp, &got)
	if cached, _ := got["cached"].(bool); !cached {
		t.Errorf("cached flag = %v, want true", got["cached"])
	}
	if got["subject"] != "stored subject" {
		t.Errorf("subject = %q, want from cache", got["subject"])
	}
	if wikiCalls != 0 {
		t.Errorf("wiki sink called %d times on cache hit, want 0", wikiCalls)
	}
}

// Cache miss → LLM runs → result persisted to both cache and wiki.
func TestGmailAnalyze_CacheMiss_RunsLLMAndPersists(t *testing.T) {
	cache := NewAnalysisStore(t.TempDir())

	pipelineCalls := 0
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			pipelineCalls++
			return gmailpoll.AnalysisResult{Text: "## 새로 생성\n신규 분석."}, nil
		},
	}
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{
				ID: id, From: "n@e.com", Subject: "Fresh",
				Date: "Mon, 26 May 2026 14:30:00 +0900",
			}, nil
		},
	}
	var wikiSeen *WikiAnalysisInput
	deps := analyzeDeps(gmailClient, pipeline)
	deps.Cache = cache
	deps.SaveToWiki = func(in WikiAnalysisInput) error {
		wikiSeen = &in
		return nil
	}
	h := gmailAnalyze(deps)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m2"}))
	var got map[string]any
	decode(t, resp, &got)
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	if pipelineCalls != 1 {
		t.Errorf("pipeline calls = %d, want 1", pipelineCalls)
	}
	if cached, _ := got["cached"].(bool); cached {
		t.Errorf("cached flag = true on fresh run, want false")
	}
	if wikiSeen == nil || wikiSeen.MsgID != "m2" {
		t.Errorf("wiki sink not invoked with correct payload: %+v", wikiSeen)
	}

	// Cache file should now exist and serve a follow-up call.
	stored, err := cache.load("m2")
	if err != nil || stored == nil {
		t.Fatalf("cache not populated: err=%v stored=%+v", err, stored)
	}
	if stored.Analysis != "## 새로 생성\n신규 분석." {
		t.Errorf("stored analysis = %q", stored.Analysis)
	}
}

// force=true must bypass the cache and re-run the LLM, replacing the
// stored copy with the fresh result.
func TestGmailAnalyze_Force_BypassesCache(t *testing.T) {
	cache := NewAnalysisStore(t.TempDir())
	if err := cache.save(&analysisRecord{
		MsgID:         "m3",
		Analysis:      "stale analysis",
		PromptVersion: AnalysisPromptVersion,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pipelineCalls := 0
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
			pipelineCalls++
			return gmailpoll.AnalysisResult{Text: "fresh analysis"}, nil
		},
	}
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: id, Subject: "S"}, nil
		},
	}
	deps := analyzeDeps(gmailClient, pipeline)
	deps.Cache = cache
	h := gmailAnalyze(deps)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{
		"id": "m3", "force": true,
	}))
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	if pipelineCalls != 1 {
		t.Errorf("pipeline calls = %d, want 1 (cache must be bypassed)", pipelineCalls)
	}
	stored, _ := cache.load("m3")
	if stored == nil || stored.Analysis != "fresh analysis" {
		t.Errorf("cache not refreshed: %+v", stored)
	}
}

func TestGmailAnalyzeMethods_MissingDepsReturnsNil(t *testing.T) {
	if got := GmailAnalyzeMethods(GmailAnalyzeDeps{}); got != nil {
		t.Errorf("expected nil when no deps wired, got %v", got)
	}
	if got := GmailAnalyzeMethods(GmailAnalyzeDeps{
		Client: func() (GmailClient, error) { return nil, nil },
	}); got != nil {
		t.Errorf("expected nil with only client, got %v", got)
	}
}

func TestPipelineFromGmailpoll_NoLLM(t *testing.T) {
	_, err := PipelineFromGmailpoll(nil, nil, "", nil)
	if !errors.Is(err, ErrAnalyzeNoLLM) {
		t.Errorf("err = %v, want ErrAnalyzeNoLLM", err)
	}
	_, err = PipelineFromGmailpoll(nil, nil, "claude-opus", nil)
	if !errors.Is(err, ErrAnalyzeNoLLM) {
		t.Errorf("nil LLMClient should still return ErrAnalyzeNoLLM, got %v", err)
	}
}

// analysis_cached returns a stored analysis (with its related projects)
// without touching the pipeline. WikiStore is nil here, so projects fall
// back to bare paths — which is exactly what the chip renders on enrichment
// failure.
func TestGmailAnalysisCached_HitReturnsProjects(t *testing.T) {
	cache := NewAnalysisStore(t.TempDir())
	if err := cache.save(&analysisRecord{
		MsgID:           "m1",
		Analysis:        "저장된 분석",
		RelatedProjects: []string{"프로젝트/deneb.md"},
		PromptVersion:   AnalysisPromptVersion,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	deps := GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) { return &fakeAnalyzePipeline{}, nil },
		Cache:    cache,
	}
	h := GmailAnalyzeMethods(deps)["miniapp.gmail.analysis_cached"]
	if h == nil {
		t.Fatal("analysis_cached handler not registered")
	}
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analysis_cached", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	var got map[string]any
	decode(t, resp, &got)
	if cached, _ := got["cached"].(bool); !cached {
		t.Errorf("cached = %v, want true", got["cached"])
	}
	if got["analysis"] != "저장된 분석" {
		t.Errorf("analysis = %q", got["analysis"])
	}
	projects, _ := got["relatedProjects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("relatedProjects len = %d, want 1 (%+v)", len(projects), got["relatedProjects"])
	}
	if p0, _ := projects[0].(map[string]any); p0["path"] != "프로젝트/deneb.md" {
		t.Errorf("project path = %v, want 프로젝트/deneb.md", p0["path"])
	}
}

func TestGmailAnalysisCached_MissReturnsNotCached(t *testing.T) {
	deps := GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) { return &fakeAnalyzePipeline{}, nil },
		Cache:    NewAnalysisStore(t.TempDir()),
	}
	h := GmailAnalyzeMethods(deps)["miniapp.gmail.analysis_cached"]
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analysis_cached", map[string]any{"id": "nope"}))
	if !resp.OK {
		t.Fatalf("expected OK on miss, got %+v", resp.Error)
	}
	var got map[string]any
	decode(t, resp, &got)
	if cached, _ := got["cached"].(bool); cached {
		t.Errorf("cached = true on miss, want false")
	}
}
