package gmailpoll

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
	stage2Timeout = 240 * time.Second

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
	LLMClient   *llm.Client  // main LLM for final analysis (stage 2)
	LocalClient *llm.Client  // local AI for extractors (stage 1)
	LocalModel  string       // local AI model name
	MainModel   string       // main LLM model name
	Logger      *slog.Logger // optional; nil = slog.Default()

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

// ThreadContext holds extracted context from email thread history.
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

const finalAnalysisSystem = `당신은 이메일 분석 어시스턴트입니다. 이메일 본문, 이전 메일 맥락, 관련 기억을 종합하여 깊이 있는 분석을 제공합니다. 모든 섹션 제목·라벨은 한국어로 쓰세요 ('Primary Analysis', 'Summary', 'Action Items' 같은 영문 라벨 금지).`

const finalAnalysisPrompt = `다음 이메일을 종합적으로 분석해주세요. 이건 채워야 할
양식이 아니라 분석 자세입니다.

먼저 이 메일이 왜 지금 왔는지 — 직전에 무슨 합의가 있었고 무엇이 바뀌었는지 —
아래 주어진 이전 맥락과 기억을 엮어 생각하세요. 그다음 사람을 봅니다: 누가
누구에게 어떤 입장에서 보냈는지. 결제 기한·마감·금액·미해결 이슈처럼 시간과 돈에
민감한 것은 묻히지 않게 하고, 마지막은 추측이 아닌 구체적인 다음 행동입니다.

무엇을 앞세우고 어떻게 묶을지는 그 메일이 정합니다. 고정된 섹션 틀에 끼워 맞추지
말고 사안에서 중요한 것이 먼저 오게 하세요. 짧으면 짧게, 복잡하면 깊게. 근거 있는
것만 말하되 리스크나 판단에는 그 근거가 된 메일 문구나 이전 맥락을 짧게 인용해
사실과 추측을 구분하고, 모르는 건 모른다고 두세요. 한국어로 간결하게.

## 이메일 원문
%s
%s%s`

// AnalyzeEmailPipeline runs a 2-stage multi-LLM analysis pipeline.
// Stage 1: extract thread context via local LLM and query the wiki knowledge
//
//	graph for the sender (parallel, both best-effort).
//
// Stage 2: final analysis combining email + thread context + memory via main LLM.
// Falls back to single-LLM analysis if pipeline deps are insufficient.
func AnalyzeEmailPipeline(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) (AnalysisResult, error) {
	candidates := deps.projectCandidates()

	if !deps.canRunPipeline() {
		// Single-call fallback (no local AI for stage-1 extractors). Project
		// selection is still offered by appending the candidate block to the
		// prompt, so the manual Mini App path — which never wires LocalClient
		// — still cites related projects.
		prompt := DefaultPrompt + projectSelectionSuffix(candidates) + importanceSuffix
		text, err := AnalyzeEmail(ctx, deps.LLMClient, deps.MainModel, prompt, msg)
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

// callLocalLLMJSON calls the local AI model with JSON mode and unmarshals the result.
func callLocalLLMJSON[T any](ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (T, error) {
	var zero T

	for attempt := range 2 {
		events, err := client.StreamChat(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", user)},
			System:         llm.SystemString(system),
			MaxTokens:      maxTokens,
			Stream:         true,
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
			// Reasoning OFF — chain-of-thought streamed into the body corrupts the
			// JSON this helper parses. See anthropic.go's disabled handling.
			Thinking: &llm.ThinkingConfig{Type: "disabled"},
		})
		if err != nil {
			return zero, err
		}

		raw, err := collectStreamText(ctx, events)
		if err != nil {
			return zero, err
		}

		result, err := jsonutil.UnmarshalLLM[T](raw)
		if err == nil {
			return result, nil
		}

		if attempt == 0 {
			continue
		}
		return zero, fmt.Errorf("JSON parse failed after retry: %s", jsonutil.Truncate(raw, 200))
	}

	return zero, fmt.Errorf("unreachable")
}

// collectStreamText gathers all text deltas from a streaming response.
func collectStreamText(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	if events == nil {
		return "", fmt.Errorf("nil event channel")
	}

	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				result := strings.TrimSpace(sb.String())
				if result == "" {
					return "", fmt.Errorf("empty LLM response")
				}
				return result, nil
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
				if json.Unmarshal(ev.Payload, &delta) == nil &&
					delta.Delta.Type != "thinking_delta" && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			case "error":
				var errBody struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(ev.Payload, &errBody) == nil && errBody.Message != "" {
					return "", fmt.Errorf("LLM stream error: %s", errBody.Message)
				}
				return "", fmt.Errorf("LLM stream error: %s", string(ev.Payload))
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
			return from[idx+1 : idx+end]
		}
	}
	// Might be a plain email address.
	if strings.Contains(from, "@") {
		return strings.TrimSpace(from)
	}
	return ""
}

// stripReplyPrefix removes Re:, Fwd:, etc. from an email subject.
func stripReplyPrefix(subject string) string {
	s := strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(s)
		switch {
		case strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "fw:"):
			s = strings.TrimSpace(s[3:])
		case strings.HasPrefix(lower, "fwd:"):
			s = strings.TrimSpace(s[4:])
		default:
			return s
		}
	}
}

func hasThreadContext(tc ThreadContext) bool {
	return tc.ThreadSummary != "" || tc.PriorExchanges != "" || len(tc.OngoingTopics) > 0 || tc.SenderRelation != ""
}

func hasMemoryContext(mc MemoryContext) bool {
	return mc.SenderFacts != "" || mc.TopicFacts != "" || mc.RelevantHistory != ""
}
