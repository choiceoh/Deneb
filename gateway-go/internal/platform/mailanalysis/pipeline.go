package mailanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Pipeline timeouts.
const (
	stage1Timeout = 30 * time.Second
	// stage2Timeout bounds the synthesis stage INCLUDING the context stages
	// that share its budget. 240s proved too tight in prod (2026-07-06: a
	// system:mailpoll synthesis run was deadline-killed 190s in, stop=timeout,
	// after earlier stages consumed the rest — analysis-model p95 is ~170s per
	// turn). 360s absorbs a slow multi-turn synthesis without unbounding the
	// pipeline.
	stage2Timeout = 360 * time.Second

	// Stage 1a: max emails to fetch for context.
	maxThreadMessages = 5
	maxSenderMessages = 3

	// Stage 1 LLM token limits.
	stage1MaxTokens = 768
	// Stage 2 (final analysis) token limit.
	stage2MaxTokens   = 1536
	batchStage2Tokens = 4096 // batch analysis needs more tokens

	// Cap on project candidates injected into the analysis prompt. Keeps
	// the prompt bounded when the project wiki grows large.
	maxProjectCandidates = 40
)

// PipelineDeps holds dependencies for the multi-stage analysis pipeline.
type PipelineDeps struct {
	GmailClient *gmail.Client
	LLMClient   *llm.Client // main LLM for final analysis (stage 2)
	LocalClient *llm.Client // local AI for extractors (stage 1)
	LocalModel  string      // local AI model name
	MainModel   string      // main LLM model name
	// AnalysisPrompt is the operator-editable instruction block for the final
	// mail analysis. Empty falls back to DefaultPrompt.
	AnalysisPrompt string
	Logger         *slog.Logger // optional; nil = slog.Default()

	// ThreadSource supplies prior related messages (thread + sender history)
	// from the on-box archive when there is no Gmail client — i.e. the LMTP
	// ingest path reconstructs thread context locally instead of from Gmail.
	// nil → no archive thread context (analysis still runs without it).
	ThreadSource ThreadSource

	// ProjectsFn lists the registered project wiki pages so the analyzer
	// can cite related ones by real path. Optional; nil = no candidates
	// offered (analysis still runs, RelatedProjects stays empty).
	ProjectsFn func() []ProjectCandidate

	// Stage2MaxTokens overrides the final-synthesis token budget. 0 → the
	// autonomous default (stage2MaxTokens). The interactive Mini App path sets
	// this higher so a deliberate "analyze this" tap can synthesize at depth
	// (and leave headroom for extended thinking). Autonomous polling keeps the
	// tighter default to bound per-cycle cost and latency.
	Stage2MaxTokens int

	// DeepThinking opts the final synthesis into extended thinking when the
	// synthesis model's provider supports it cleanly (Anthropic Messages mode).
	// Off by default so autonomous polling and OpenAI-mode endpoints (the local
	// vLLM step3.7, which leaked chain-of-thought into the body — #1816) keep
	// the safe reasoning-disabled behavior. The interactive path sets it true.
	DeepThinking bool

	// SenderFactsFn resolves "who is this person to us" for the sender display
	// name, in-process (wiki graph traversal). When set it is preferred over the
	// external graphify CLI, so sender context is available even when the graph
	// was never snapshotted. nil → fall back to the graphify subprocess.
	SenderFactsFn func(ctx context.Context, displayName string) string

	// CounterpartyProjectsFn returns the linked project names for an active
	// counterparty mail domain (wiki-derived, cached server-side), nil/empty
	// otherwise. Enriches the party anchor's side labels — "외부(domain · 활성
	// 거래처: 프로젝트…)" — so stage2 gets the deal linkage deterministically.
	// nil → plain side labels.
	CounterpartyProjectsFn func(domain string) []string

	// AttachmentExtractFn extracts readable text from an attachment's raw bytes
	// (documents and images/OCR). When set together with a LocalClient, the
	// attachment selection gate (attachments.go) reads the business documents the
	// analysis would otherwise be blind to — but only the ones a local LLM judges
	// relevant, so logos/signatures/boilerplate never pollute the analysis. nil →
	// the gate is skipped and analysis stays body-only.
	AttachmentExtractFn func(ctx context.Context, data []byte, filename, mimeType string) string

	// AttachmentBytesFn fetches one attachment's raw bytes by (messageID,
	// attachmentID). It abstracts where the bytes come from so the same gate
	// (attachments.go) serves both ingest paths: the poll path wires
	// gmailClient.GetAttachment (lazy fetch from Gmail), while the LMTP path
	// wires a closure over the message's inline attachment bytes (no network —
	// they arrived in the message). nil → the attachment gate is skipped.
	AttachmentBytesFn func(ctx context.Context, messageID, attachmentID string) ([]byte, error)

	// ThinkingKwarg is the stage-2 main-role model's chat_template_kwargs off-switch
	// (modelcaps.ThinkingToggleKwarg, e.g. dsv4's "thinking"). Attached to the
	// "disabled" thinking config so a dual-mode vLLM model actually stops
	// reasoning instead of exhausting the token budget on a silent <think> block
	// (→ empty analysis). "" for non-vLLM models, which handle thinking on the wire.
	ThinkingKwarg string

	// AgentSynthesisFn runs the final synthesis as a chat agent turn with the full
	// toolset (wiki search, mail_archive, …) and returns the clean deliverable
	// text. When set, it replaces the tool-less single completion so the analysis
	// prompt's tool steps actually EXECUTE — the model calls the tools instead of
	// role-playing them as <tool_call> text that leaked into the feed. On any
	// failure the synthesis falls back to the single-completion path so an analysis
	// is never lost. nil = the legacy tool-less synthesis.
	AgentSynthesisFn func(ctx context.Context, prompt string) (string, error)
}

const (
	// analysisThinkingMinTokens is the floor below which extended thinking stays
	// disabled even on a capable provider: a small max-tokens budget would be
	// eaten by reasoning, leaving no room for the answer (the failure mode #1816
	// hit on vLLM). Thinking only turns on where we've allocated real headroom.
	analysisThinkingMinTokens = 3000
	// analysisThinkingMaxBudget caps reasoning tokens so the answer always has
	// room regardless of how large the caller's max-tokens budget is.
	analysisThinkingMaxBudget = 4096
)

// analysisThinking returns the thinking config for a final-synthesis call.
// Extended thinking deepens analysis, but it is only safe where the provider
// emits reasoning as distinct SSE thinking blocks — Anthropic Messages mode,
// where collectStreamText skips thinking_delta so chain-of-thought never reaches
// the answer body. On OpenAI-mode endpoints (the local vLLM included) it leaked
// into the body and starved the answer (#1816), so it stays disabled there.
func analysisThinking(client *llm.Client, maxTokens int) *llm.ThinkingConfig {
	if client == nil || client.APIMode() != llm.APIModeAnthropic || maxTokens < analysisThinkingMinTokens {
		return &llm.ThinkingConfig{Type: "disabled"}
	}
	budget := maxTokens / 2
	if budget > analysisThinkingMaxBudget {
		budget = analysisThinkingMaxBudget
	}
	return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
}

// canRunPipeline returns true if we have enough deps for the multi-stage pipeline.
func (d *PipelineDeps) canRunPipeline() bool {
	// GmailClient is optional — it only powers the best-effort thread-context
	// stage (Gmail subject search), which no-ops when nil (see
	// extractThreadContext). The LMTP ingest path has no Gmail client yet still
	// gets the full multi-stage analysis (sender facts + deal extraction).
	return d.LocalClient != nil && d.LocalModel != ""
}

// projectCandidates returns the registered project pages, or nil when no
// provider is wired. Capped so a large project wiki can't bloat the
// analysis prompt.
func (d *PipelineDeps) projectCandidates() []ProjectCandidate {
	if d.ProjectsFn == nil {
		return nil
	}
	cands := d.ProjectsFn()
	if len(cands) > maxProjectCandidates {
		cands = cands[:maxProjectCandidates]
	}
	return cands
}

// ThreadSource yields prior messages related to msg — same thread and recent
// same-sender history — for thread-context extraction. The mailarchive package
// implements it against the on-box archive IMAP; it is an interface here so
// mailanalysis does not depend on that package.
type ThreadSource interface {
	RelatedMessages(ctx context.Context, msg *gmail.MessageDetail) ([]*gmail.MessageDetail, error)
}

// ThreadContext holds extracted context from email thread history. Stays on
// plain json_object (no strict json_schema): no enum to enforce, and its long
// free-text fields are explosion-prone under strict guided decoding (see
// callLocalLLMJSON), so strict would add latency-tax risk for no shape benefit.
type ThreadContext struct {
	ThreadSummary  string   `json:"thread_summary"`
	PriorExchanges string   `json:"prior_exchanges"`
	OngoingTopics  []string `json:"ongoing_topics"`
	SenderRelation string   `json:"sender_relation"`
}

// EmailFact is a fact extracted from email analysis, with optional project tag.
type EmailFact struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Importance float64 `json:"importance"`
	ExpiryHint string  `json:"expiry_hint,omitempty"`
	Project    string  `json:"project,omitempty"`
}

// AnalysisResult is the outcome of analyzing one email: the human-readable
// analysis text plus the wiki paths of projects the analyzer judged related.
// RelatedProjects is always validated against the supplied candidate list,
// so it never contains a hallucinated path.
type AnalysisResult struct {
	Text            string
	RelatedProjects []string
	// Importance is the model's own triage verdict for this mail, parsed
	// from the IMPORTANCE tag line: "urgent" | "attention" | "routine",
	// or "" when the tag was missing/unparseable. The inbox list marker
	// prefers this over the cheap heuristic when present.
	Importance string
	// ActionItems are the operator's follow-up actions extracted from the
	// analysis (best-effort; nil when local AI is unavailable or nothing
	// qualifies). The server sink turns high-priority ones into to-dos.
	ActionItems []ActionItem
	// Deal is the structured business-document extraction (견적서/계약서/
	// 세금계산서 등), or nil when the mail carries no recognizable deal
	// document. The server sink files it onto a 거래 wiki page.
	Deal *DealInfo
	// StatusTag is a compact bracket tag ("[결정·승인]", "[리스크]", …) the server
	// appends to the project status bullet, from the mail's primary status signal
	// (type + decision state). "" when the mail is not project-linked, local AI is
	// unavailable, or the signal is the unremarkable 진행/없음 (no tag worth showing).
	StatusTag string
}

// ProjectCandidate is one registered project wiki page offered to the
// analyzer so it can cite related projects by their real path. The server
// layer supplies these via PipelineDeps.ProjectsFn, which keeps the wiki
// store out of this package's imports.
type ProjectCandidate struct {
	Path    string
	Title   string
	Summary string
}

// MemoryContext holds extracted context from memory recall.
type MemoryContext struct {
	SenderFacts     string `json:"sender_facts"`
	TopicFacts      string `json:"topic_facts"`
	RelevantHistory string `json:"relevant_history"`
}

// System prompts for each stage.
const threadExtractorSystem = `당신은 이메일 맥락 분석기입니다. 이전 메일 내용을 바탕으로 현재 이메일의 맥락을 파악합니다.
반드시 JSON으로만 응답하세요.`

const threadExtractorPrompt = `다음은 현재 분석 중인 이메일과 관련된 이전 메일들입니다.
이 정보를 바탕으로 현재 이메일의 맥락을 파악해주세요.

JSON으로 응답하세요:
{
  "thread_summary": "이 쓰레드의 전체 흐름 요약 (2-3문장)",
  "prior_exchanges": "이전에 주고받은 핵심 내용 요약",
  "ongoing_topics": ["진행 중인 주제1", "주제2"],
  "sender_relation": "이 발신자와의 관계/맥락 (어떤 용건으로 소통하는지)"
}

이전 메일이 없으면 모든 필드를 빈 값으로 응답하세요.

## 현재 이메일
%s

## 관련 이전 메일들
%s`

// SourceEmailAnalysis is the fact source identifier for email-derived facts.
const SourceEmailAnalysis = "email_analysis"

const finalAnalysisSystem = `당신은 이메일 분석 어시스턴트입니다. 이메일 본문, 이전 메일 맥락, 관련 기억을 종합하여 깊이 있는 분석을 제공합니다. 모든 섹션 제목·라벨은 한국어로 쓰세요 ('Primary Analysis', 'Summary', 'Action Items' 같은 영문 라벨 금지). ` +
	// Card-first reporting must ride the multi-stage pipeline's FINAL synthesis
	// too — analysisSystemPrompt only covers the single-call fallback, so an
	// operator-customized prompt file would otherwise ship plain prose.
	"보고가 구조적(중요도·수치·기한·다음 행동)이면 도입부를 deneb-ui 카드 한 블록으로 시작하세요 — 여는 펜스는 ```deneb-ui 한 줄 그대로(그 줄 뒤에 다른 글자 금지), 다음 줄부터 루트 <column> 하나. 카드 안에는 백틱을 쓰지 마세요. " +
	emojiRestraint

const agentSynthesisReadOnlyInstruction = `이 합성 턴은 읽기 전용입니다. 필요한 경우 wiki 검색/읽기 또는 mail_archive 조회로 근거와 첨부만 확인하세요. wiki 기록·로그·수정·닫기·다시열기·수집 또는 knowledge 기록은 하지 마세요. 메일 분석 파이프라인이 분석 결과와 관련 프로젝트 상태를 별도로 저장합니다. 도구 확인이 끝나면 추가 기록 작업 없이 최종 분석을 작성하세요.`

const finalAnalysisPrompt = `%s
## 이메일 원문
%s
%s%s`

func analysisPrompt(deps PipelineDeps) string {
	if prompt := strings.TrimSpace(deps.AnalysisPrompt); prompt != "" {
		return prompt
	}
	return DefaultPrompt
}

// AnalyzeEmailPipeline runs a 2-stage multi-LLM analysis pipeline.
// Stage 1: extract thread context via local LLM and query the wiki knowledge
//
//	graph for the sender (parallel, both best-effort).
//
// Stage 2: final analysis combining email + thread context + memory via main LLM.
// Falls back to single-LLM analysis if pipeline deps are insufficient.
func AnalyzeEmailPipeline(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) (AnalysisResult, error) {
	if msg == nil {
		return AnalysisResult{}, fmt.Errorf("email message is required")
	}
	if deps.LLMClient == nil && deps.AgentSynthesisFn == nil {
		return AnalysisResult{}, fmt.Errorf("analysis LLM client is required")
	}
	candidates := deps.projectCandidates()

	if !deps.canRunPipeline() {
		// Single-call fallback (no local AI for stage-1 extractors). Project
		// selection is still offered by appending the candidate block to the
		// prompt, so the manual Mini App path — which never wires LocalClient
		// — still cites related projects.
		prompt := analysisPrompt(deps) + projectSelectionSuffix(candidates) + importanceSuffix
		text, err := AnalyzeEmail(ctx, deps.LLMClient, deps.MainModel, prompt, deps.ThinkingKwarg, deps.CounterpartyProjectsFn, msg)
		if err != nil {
			return AnalysisResult{}, err
		}
		clean, projects := parseRelatedProjects(text, candidates)
		clean, importance := parseImportance(clean)
		return AnalysisResult{Text: clean, RelatedProjects: projects, Importance: importance}, nil
	}

	// Stage 1: extract thread context + wiki-graph context in parallel.
	stage1Ctx, stage1Cancel := context.WithTimeout(ctx, stage1Timeout)
	defer stage1Cancel()

	var (
		threadCtx ThreadContext
		memCtx    MemoryContext
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		tc, _ := extractThreadContext(stage1Ctx, deps, msg) // best-effort
		threadCtx = tc
	}()
	go func() {
		defer wg.Done()
		memCtx = extractSenderContext(stage1Ctx, deps, msg) // best-effort
	}()
	wg.Wait()

	// Stage 2: final analysis combining all context.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	return synthesizeAnalysis(stage2Ctx, deps, msg, threadCtx, memCtx, candidates)
}

// --- batch analysis ---
// --- helpers ---

// callLocalLLMJSON calls the local AI model with structured-output mode and
// unmarshals the result. A non-nil schema requests strict json_schema: vLLM
// constrains generation to the schema via guided decoding, so enum fields (action
// priority, fact type) and the object shape can't silently drift — the failure
// mode plain json_object allows, where a stray "urgent"/"높음" priority would slip
// past the downstream high-priority calendar gate. A nil schema requests plain
// json_object (used for the wide free-text extractors — deal, thread — where
// strict guided decoding triggers the explosion below for no enum benefit).
//
// Fallback: if a json_schema attempt errors OR the guided output is unparseable,
// it retries once in plain json_object. This covers two cases — an endpoint that
// doesn't support guided decoding, and vLLM xgrammar's whitespace-explosion bug
// (the model degenerates into an unbounded space run as a string value and
// truncates the JSON; rare on the narrow schemas we send strict, but real). The
// json_object retry is explosion-free in live probing, so extraction still lands.
func callLocalLLMJSON[T any](ctx context.Context, client *llm.Client, model, system, user string, maxTokens int, schema json.RawMessage) (T, error) {
	var zero T

	useSchema := len(schema) > 0
	for attempt := range 2 {
		format := &llm.ResponseFormat{Type: "json_object"}
		if useSchema {
			format = &llm.ResponseFormat{Type: "json_schema", JSONSchema: llm.FlexibleFromRaw(schema)}
		}

		events, err := client.StreamChat(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", user)},
			System:         llm.SystemString(system),
			MaxTokens:      maxTokens,
			Stream:         true,
			ResponseFormat: format,
			// Reasoning OFF — chain-of-thought streamed into the body corrupts the
			// JSON this helper parses. See anthropic.go's disabled handling.
			Thinking: &llm.ThinkingConfig{Type: "disabled"},
		})
		if err == nil {
			var raw string
			var usage llm.TokenUsage
			raw, usage, err = collectStreamTextCore(ctx, events)
			if err == nil {
				// The model produced a full response — record its tokens whether or
				// not the JSON parses. A parse-then-retry still spent tokens on both
				// attempts, so emitting per successful stream is the accurate count.
				emitLocalHelperUsage(model, usage)
				result, perr := jsonutil.UnmarshalLLM[T](raw)
				if perr == nil {
					return result, nil
				}
				err = fmt.Errorf("JSON parse failed: %s", jsonutil.Truncate(raw, 200))
			}
		}

		// err != nil here. Retry once on the first attempt, dropping json_schema if
		// that was the mode so an endpoint rejecting guided decoding still extracts.
		if attempt == 0 {
			useSchema = false
			continue
		}
		return zero, err
	}

	return zero, fmt.Errorf("unreachable")
}

// collectStreamText gathers all text deltas from a streaming response. It is a
// thin wrapper over collectStreamTextCore that discards the token usage — the
// signature most callers (and the contract tests) rely on.
func collectStreamText(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	text, _, err := collectStreamTextCore(ctx, events)
	return text, err
}

// collectStreamTextCore gathers all text deltas plus the token usage reported by
// the stream (message_start carries input, message_delta carries output). Usage
// fields stay zero for providers that do not report them. Behaves identically to
// the historical collectStreamText for text and error handling; the usage is
// purely additive so callers that ignore it are unaffected.
func collectStreamTextCore(ctx context.Context, events <-chan llm.StreamEvent) (string, llm.TokenUsage, error) {
	var usage llm.TokenUsage
	if events == nil {
		return "", usage, fmt.Errorf("nil event channel")
	}

	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), usage, nil
			}
			return "", usage, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				result := strings.TrimSpace(sb.String())
				if result == "" {
					return "", usage, fmt.Errorf("empty LLM response")
				}
				return result, usage, nil
			}
			switch ev.Type {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				// Skip thinking_delta: OpenAI-translated streams carry chain-of-
				// thought in .text, so the delta type is the reliable signal.
				// Reasoning is also disabled at the request level (analysis reqs
				// above); this is the belt-and-suspenders guard.
				if json.Unmarshal(ev.Payload.Bytes(), &delta) == nil &&
					delta.Delta.Type != "thinking_delta" && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			case "message_start":
				var ms llm.MessageStart
				if json.Unmarshal(ev.Payload.Bytes(), &ms) == nil {
					usage.InputTokens = ms.Message.Usage.InputTokens
					if v := ms.Message.Usage.CacheReadInputTokens; v > 0 {
						usage.CacheReadInputTokens = v
					}
					if v := ms.Message.Usage.CacheCreationInputTokens; v > 0 {
						usage.CacheCreationInputTokens = v
					}
				}
			case "message_delta":
				var md llm.MessageDelta
				if json.Unmarshal(ev.Payload.Bytes(), &md) == nil {
					if v := md.Usage.OutputTokens; v > 0 {
						usage.OutputTokens = v
					}
					if v := md.Usage.CacheReadInputTokens; v > 0 {
						usage.CacheReadInputTokens = v
					}
					if v := md.Usage.CacheCreationInputTokens; v > 0 {
						usage.CacheCreationInputTokens = v
					}
				}
			case "error":
				var errBody struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(ev.Payload.Bytes(), &errBody) == nil && errBody.Message != "" {
					return "", usage, fmt.Errorf("LLM stream error: %s", errBody.Message)
				}
				return "", usage, fmt.Errorf("LLM stream error: %s", ev.Payload.String())
			}
		}
	}
}

// formatEmailBrief creates a concise representation of an email for context.
func formatEmailBrief(msg *gmail.MessageDetail) string {
	body := truncateBody(msg.Body, 1500)
	return fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\nDate: %s\n\n%s", msg.From, msg.To, msg.Subject, msg.Date, body)
}

// truncateBody truncates the body to maxChars.
func truncateBody(body string, maxChars int) string {
	if len(body) <= maxChars {
		return body
	}
	return textutil.TruncateBytes(body, maxChars) + "\n... (생략)"
}

// extractEmailAddr extracts the email address from a "Name <email>" string.
func extractEmailAddr(from string) string {
	if idx := strings.LastIndex(from, "<"); idx >= 0 {
		end := strings.Index(from[idx:], ">")
		if end > 0 {
			return strings.TrimSpace(from[idx+1 : idx+end])
		}
	}
	// Might be a plain email address.
	if strings.Contains(from, "@") {
		return strings.TrimSpace(from)
	}
	return ""
}

func hasThreadContext(tc ThreadContext) bool {
	return tc.ThreadSummary != "" || tc.PriorExchanges != "" || len(tc.OngoingTopics) > 0 || tc.SenderRelation != ""
}

func hasMemoryContext(mc MemoryContext) bool {
	return mc.SenderFacts != "" || mc.TopicFacts != "" || mc.RelevantHistory != ""
}
