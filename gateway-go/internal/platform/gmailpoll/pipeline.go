package gmailpoll

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
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
}

// canRunPipeline returns true if we have enough deps for the multi-stage pipeline.
func (d *PipelineDeps) canRunPipeline() bool {
	return d.LocalClient != nil && d.LocalModel != "" && d.GmailClient != nil
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

const finalAnalysisSystem = `당신은 이메일 분석 어시스턴트입니다. 이메일 본문, 이전 메일 맥락, 관련 기억을 종합하여 깊이 있는 분석을 제공합니다.`

const finalAnalysisPrompt = `다음 이메일을 종합적으로 분석해주세요.

## 분석 항목
1. **요약**: 발신자와 주요 내용 (2-3문장)
2. **맥락**: 이전 소통/기억 기반 배경 설명
3. **중요도**: 높음/보통/낮음 (근거 포함)
4. **조치 사항**: 필요한 행동이 있다면 명시
5. **관계 맥락**: 이 사람과의 관계에서 주의할 점

간결하고 핵심만 전달해주세요. 맥락이 없으면 해당 항목은 생략하세요.

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
		prompt := DefaultPrompt + projectSelectionSuffix(candidates)
		text, err := AnalyzeEmail(ctx, deps.LLMClient, deps.MainModel, prompt, msg)
		if err != nil {
			return AnalysisResult{}, err
		}
		clean, projects := parseRelatedProjects(text, candidates)
		return AnalysisResult{Text: clean, RelatedProjects: projects}, nil
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
		memCtx = extractWikiGraphContext(stage1Ctx, msg) // best-effort
	}()
	wg.Wait()

	// Stage 2: final analysis combining all context.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	return synthesizeAnalysis(stage2Ctx, deps, msg, threadCtx, memCtx, candidates)
}

// --- batch analysis ---

const batchAnalysisSystem = `당신은 비서 역할의 이메일 분석 어시스턴트입니다.
여러 이메일을 한꺼번에 분석하여 프로젝트별로 그룹핑하고, 우선순위를 매겨 통합 리포트를 작성합니다.`

const batchAnalysisPrompt = `다음 %d건의 이메일을 분석하여 통합 리포트를 작성해주세요.

## 리포트 형식
1. 첫 줄: 📬 메일 N건 분석 (HH:MM 기준)
2. 프로젝트/안건별로 그룹핑
3. 우선순위별 이모지:
   - 🔴 긴급/즉시 조치 필요
   - 🟠 중요/이번 주 내 결정 필요
   - 🟡 일반 진행사항/확인 필요
   - 🔵 참고/단순 정보
4. 각 항목 형식:
   - 이모지 + 프로젝트명 — 한 줄 제목
   - 발신자 → 수신자
   - 핵심 내용 불릿 (•)
   - → 필요한 조치사항
5. 같은 프로젝트 관련 메일은 합쳐서 하나로
6. 단순 진행사항은 🔵 일반 진행사항으로 묶어서 간략히

## 구분선
우선순위 그룹 사이에 ━━━━━━━━━━━━━━━━━━━━ 사용

## 규칙
- 한국어로 작성
- 간결하고 핵심만
- 발신자 이름은 실명 사용
- 금액, 날짜, 수량 등 구체적 수치 포함
- 생략하지 말고 모든 메일을 포함

%s`

// BatchItem pairs an email with its individual analysis result. AnalyzeBatch
// returns these so the caller can cache/page each one, and the consolidated
// report is synthesized from the originals + these analyses.
type BatchItem struct {
	Msg    *gmail.MessageDetail
	Result AnalysisResult
}

// AnalyzeBatch analyzes a batch of emails: each email is analyzed
// individually (including related-project selection), and those per-email
// results are both returned (for the caller to cache/page) and fed — along
// with the original emails — into a single consolidated report grouped by
// project and priority. Per-email analysis is best-effort: a failed one is
// logged and skipped rather than failing the whole batch.
func AnalyzeBatch(ctx context.Context, deps PipelineDeps, msgs []*gmail.MessageDetail) (string, []BatchItem, error) {
	if len(msgs) == 0 {
		return "", nil, fmt.Errorf("no emails to analyze")
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Phase 1: individual analysis per email. Sequential to bound concurrent
	// LLM load on the local server (the batch caps at MaxPerCycle anyway).
	// These per-email results are the single source of truth — the caller
	// caches/pages each one, and they feed the consolidated report below.
	items := make([]BatchItem, 0, len(msgs))
	for _, msg := range msgs {
		res, err := AnalyzeEmailPipeline(ctx, deps, msg)
		if err != nil {
			logger.Warn("개별 메일 분석 실패 — 건너뜀", "id", msg.ID, "error", err)
			continue
		}
		items = append(items, BatchItem{Msg: msg, Result: res})
	}
	if len(items) == 0 {
		return "", nil, fmt.Errorf("모든 메일 분석 실패 (%d건)", len(msgs)) //nolint:staticcheck // ST1005 — Korean error message
	}

	// Single email: the individual analysis IS the report — no consolidation.
	if len(items) == 1 {
		return items[0].Result.Text, items, nil
	}

	// Phase 2: consolidated report from originals + individual analyses.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	report, err := synthesizeBatchReport(stage2Ctx, deps, items)
	if err != nil {
		// Per-email results are still valuable (cache/page) even if the
		// consolidated report failed — return them alongside the error.
		return "", items, err
	}
	return report, items, nil
}

// synthesizeBatchReport generates the consolidated report from the original
// emails plus their individual analyses. Feeding both — not just the
// analyses — gives the model the full source material for a richer
// big-picture report (grouping by project, prioritizing across emails).
func synthesizeBatchReport(ctx context.Context, deps PipelineDeps, items []BatchItem) (string, error) {
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "### 메일 %d\n", i+1)
		sb.WriteString(FormatEmailForAnalysis(it.Msg))
		if analysis := strings.TrimSpace(it.Result.Text); analysis != "" {
			sb.WriteString("\n\n[개별 분석]\n")
			sb.WriteString(analysis)
			sb.WriteString("\n")
		}
	}

	userPrompt := fmt.Sprintf(batchAnalysisPrompt, len(items), sb.String())

	req := llm.ChatRequest{
		Model:     deps.MainModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:    llm.SystemString(batchAnalysisSystem),
		MaxTokens: batchStage2Tokens,
		Stream:    true,
	}

	events, err := deps.LLMClient.StreamChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("batch analysis LLM call failed: %w", err)
	}

	return collectStreamText(ctx, events)
}

// extractThreadContext fetches related emails and extracts thread context via local LLM.
func extractThreadContext(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) (ThreadContext, error) {
	var zero ThreadContext

	// Collect related emails.
	var relatedEmails []string

	// 1. Fetch other messages with the same subject (thread-like behavior).
	// Gmail API doesn't expose a thread:ID search operator, so we search
	// by subject to find related messages in the conversation.
	if msg.Subject != "" {
		// Strip common reply/forward prefixes for broader matching.
		subj := stripReplyPrefix(msg.Subject)
		query := fmt.Sprintf("subject:%q", subj)
		threadMsgs, err := deps.GmailClient.Search(ctx, query, maxThreadMessages+1)
		if err == nil {
			for _, tm := range threadMsgs {
				if tm.ID == msg.ID {
					continue
				}
				detail, err := deps.GmailClient.GetMessage(ctx, tm.ID)
				if err != nil {
					continue
				}
				relatedEmails = append(relatedEmails, formatEmailBrief(detail))
				if len(relatedEmails) >= maxThreadMessages {
					break
				}
			}
		}
	}

	// 2. Fetch recent emails from the same sender.
	senderEmail := extractEmailAddr(msg.From)
	if senderEmail != "" {
		query := fmt.Sprintf("from:%s newer_than:30d", senderEmail)
		senderMsgs, err := deps.GmailClient.Search(ctx, query, maxSenderMessages+1)
		if err == nil {
			for _, sm := range senderMsgs {
				if sm.ID == msg.ID {
					continue
				}
				detail, err := deps.GmailClient.GetMessage(ctx, sm.ID)
				if err != nil {
					continue
				}
				relatedEmails = append(relatedEmails, formatEmailBrief(detail))
				if len(relatedEmails) >= maxThreadMessages+maxSenderMessages {
					break
				}
			}
		}
	}

	if len(relatedEmails) == 0 {
		// No context to extract — return empty.
		return zero, nil
	}

	currentEmail := formatEmailBrief(&gmail.MessageDetail{
		From:    msg.From,
		To:      msg.To,
		Subject: msg.Subject,
		Date:    msg.Date,
		Body:    truncateBody(msg.Body, 2000),
	})
	relatedText := strings.Join(relatedEmails, "\n---\n")

	userPrompt := fmt.Sprintf(threadExtractorPrompt, currentEmail, relatedText)

	result, err := callLocalLLMJSON[ThreadContext](ctx, deps.LocalClient, deps.LocalModel, threadExtractorSystem, userPrompt, stage1MaxTokens)
	if err != nil {
		return zero, fmt.Errorf("thread context extraction failed: %w", err)
	}
	return result, nil
}

// graphifyQueryTimeout caps how long the wiki-graph subprocess may run before
// the pipeline gives up and proceeds without graph context.
const graphifyQueryTimeout = 10 * time.Second

// maxSenderFactsChars bounds graphify output so the analyze prompt stays small.
const maxSenderFactsChars = 2000

// extractWikiGraphContext queries the wiki knowledge graph (built by the wiki
// dreamer at ~/.deneb/wiki-graph/graphify-out/graph.json) for the sender's
// identity and related context. The result populates MemoryContext.SenderFacts
// so final synthesis can answer "who is this person to us" without the agent
// having to call graphify mid-turn.
//
// Best-effort: returns an empty MemoryContext on any failure (binary not
// installed, graph not yet built, query timeout, empty result). Never blocks
// the pipeline.
func extractWikiGraphContext(ctx context.Context, msg *gmail.MessageDetail) MemoryContext {
	var zero MemoryContext

	name := extractDisplayName(msg.From)
	if name == "" {
		return zero
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return zero
	}
	graphPath := filepath.Join(home, ".deneb", "wiki-graph", "graphify-out", "graph.json")
	if _, err := os.Stat(graphPath); err != nil {
		return zero // wiki graph not built yet
	}
	if _, err := exec.LookPath("graphify"); err != nil {
		return zero // graphify CLI not installed
	}

	queryCtx, cancel := context.WithTimeout(ctx, graphifyQueryTimeout)
	defer cancel()

	question := fmt.Sprintf("%s에 대해 알려진 정보, 관련 프로젝트·거래·결정·인물 관계", name)
	cmd := exec.CommandContext(queryCtx, "graphify", "query", question, "--graph", graphPath)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return zero
	}

	facts := strings.TrimSpace(out.String())
	if facts == "" {
		return zero
	}
	if len(facts) > maxSenderFactsChars {
		facts = facts[:maxSenderFactsChars] + "\n...(생략)"
	}
	return MemoryContext{SenderFacts: facts}
}

// ExtractSenderFacts is the exported entry point for callers that need a
// wiki-graph snapshot for a single sender (Mini App sender_context handler).
// Takes a Gmail "From" header value verbatim (the unexported worker
// handles display-name extraction) and returns the graphify query result
// truncated to maxSenderFactsChars. Returns "" on any failure (graphify
// not installed, graph not yet built, query timeout, no result) so
// callers can treat empty as "no facts" without special-casing.
func ExtractSenderFacts(ctx context.Context, from string) string {
	return extractWikiGraphContext(ctx, &gmail.MessageDetail{From: from}).SenderFacts
}

// extractDisplayName returns the display name portion of "Name <email>",
// stripping surrounding quotes; falls back to the email address if no name is
// present. Used to seed wiki-graph queries with whatever the human typically
// writes (a person's name finds richer graph context than an email address).
func extractDisplayName(from string) string {
	s := strings.TrimSpace(from)
	if s == "" {
		return ""
	}
	if idx := strings.LastIndex(s, "<"); idx >= 0 {
		name := strings.TrimSpace(s[:idx])
		name = strings.Trim(name, `"`)
		if name != "" {
			return name
		}
		return extractEmailAddr(s)
	}
	return s
}

// synthesizeAnalysis combines the email with extracted contexts for final LLM analysis.
func synthesizeAnalysis(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail, tc ThreadContext, mc MemoryContext, candidates []ProjectCandidate) (AnalysisResult, error) {
	emailText := FormatEmailForAnalysis(msg)

	// Build optional context sections.
	var threadSection, memorySection string

	if hasThreadContext(tc) {
		var sb strings.Builder
		sb.WriteString("\n\n## 이전 메일 맥락\n")
		if tc.ThreadSummary != "" {
			fmt.Fprintf(&sb, "- **쓰레드 요약**: %s\n", tc.ThreadSummary)
		}
		if tc.PriorExchanges != "" {
			fmt.Fprintf(&sb, "- **이전 교환 내용**: %s\n", tc.PriorExchanges)
		}
		if len(tc.OngoingTopics) > 0 {
			fmt.Fprintf(&sb, "- **진행 중 주제**: %s\n", strings.Join(tc.OngoingTopics, ", "))
		}
		if tc.SenderRelation != "" {
			fmt.Fprintf(&sb, "- **발신자 관계**: %s\n", tc.SenderRelation)
		}
		threadSection = sb.String()
	}

	if hasMemoryContext(mc) {
		var sb strings.Builder
		sb.WriteString("\n\n## 관련 기억\n")
		if mc.SenderFacts != "" {
			fmt.Fprintf(&sb, "- **발신자 정보**: %s\n", mc.SenderFacts)
		}
		if mc.TopicFacts != "" {
			fmt.Fprintf(&sb, "- **주제 관련**: %s\n", mc.TopicFacts)
		}
		if mc.RelevantHistory != "" {
			fmt.Fprintf(&sb, "- **과거 맥락**: %s\n", mc.RelevantHistory)
		}
		memorySection = sb.String()
	}

	userPrompt := fmt.Sprintf(finalAnalysisPrompt, emailText, threadSection, memorySection)
	userPrompt += projectSelectionSuffix(candidates)

	req := llm.ChatRequest{
		Model:     deps.MainModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:    llm.SystemString(finalAnalysisSystem),
		MaxTokens: stage2MaxTokens,
		Stream:    true,
	}

	events, err := deps.LLMClient.StreamChat(ctx, req)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("final analysis LLM call failed: %w", err)
	}

	analysis, err := collectStreamText(ctx, events)
	if err != nil {
		return AnalysisResult{}, err
	}

	// Parse + strip the RELATED_PROJECTS tag before appending the facts
	// block, so the tag stays at the analysis tail where the parser expects
	// it (the facts block would otherwise bury it).
	clean, projects := parseRelatedProjects(analysis, candidates)

	// Local-AI fact extraction for wiki write-back. The system prompt's
	// "분석 → 위키 갱신" section asks the agent to record new facts after
	// analyzing; this attaches a structured proposal block so the agent has
	// concrete `wiki(action="write")` inputs rather than having to derive
	// them from prose. Best-effort — empty when local AI is unavailable or
	// yields nothing to record.
	if factsBlock := extractFactsForWiki(ctx, deps, clean); factsBlock != "" {
		clean = clean + "\n\n" + factsBlock
	}
	return AnalysisResult{Text: clean, RelatedProjects: projects}, nil
}

// --- related-project selection ---

// projectSelectionSuffix builds the prompt addendum that lists registered
// project pages and asks the model to tag related ones on the last line.
// Returns "" when there are no candidates, so prompts are unchanged when no
// project provider is wired. Appended in code (not baked into the prompt
// templates) so a custom analysis prompt still gets project tagging.
func projectSelectionSuffix(candidates []ProjectCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n## 등록된 프로젝트 목록\n")
	sb.WriteString("아래는 위키에 등록된 프로젝트다. 이 이메일과 직접 관련된 프로젝트가 있으면, ")
	sb.WriteString("응답의 가장 마지막 줄에 정확히 다음 형식으로 경로만 나열하라:\n")
	sb.WriteString("RELATED_PROJECTS: <경로1>, <경로2>\n")
	sb.WriteString("관련 프로젝트가 없으면 그 줄을 아예 생략하라. 목록에 없는 경로는 절대 만들지 마라.\n\n")
	for _, c := range candidates {
		sb.WriteString("- ")
		sb.WriteString(c.Path)
		if c.Title != "" {
			sb.WriteString(": ")
			sb.WriteString(c.Title)
		}
		if c.Summary != "" {
			sb.WriteString(" — ")
			sb.WriteString(c.Summary)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseRelatedProjects extracts the "RELATED_PROJECTS:" tag line from the
// analysis text, returning the text with that line removed plus the paths
// that actually exist in candidates (so a hallucinated or stale path is
// dropped). Order-preserving and de-duplicated.
func parseRelatedProjects(text string, candidates []ProjectCandidate) (string, []string) {
	if len(candidates) == 0 {
		return text, nil
	}
	valid := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		valid[c.Path] = struct{}{}
	}

	lines := strings.Split(text, "\n")
	keep := make([]string, 0, len(lines))
	var paths []string
	seen := make(map[string]struct{})
	for _, line := range lines {
		rest, ok := cutTagPrefix(strings.TrimSpace(line), "RELATED_PROJECTS:")
		if !ok {
			keep = append(keep, line)
			continue
		}
		for _, raw := range strings.Split(rest, ",") {
			p := strings.Trim(strings.TrimSpace(raw), "`\"'")
			if p == "" {
				continue
			}
			if _, isValid := valid[p]; !isValid {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
		// drop the tag line itself from the visible text
	}
	clean := strings.TrimRight(strings.Join(keep, "\n"), "\n ")
	return clean, paths
}

// cutTagPrefix returns the remainder after a case-insensitive prefix match,
// tolerating a leading markdown marker (e.g. "**RELATED_PROJECTS:**").
func cutTagPrefix(line, tag string) (string, bool) {
	stripped := strings.TrimLeft(line, "*_ \t")
	if len(stripped) < len(tag) || !strings.EqualFold(stripped[:len(tag)], tag) {
		return "", false
	}
	return strings.TrimLeft(stripped[len(tag):], "*_ \t"), true
}

// --- wiki fact extraction (local AI) ---

const factExtractorSystem = `당신은 이메일 분석에서 위키에 기록할 만한 사실을 추출하는 추출기입니다.
사람·조직·거래·프로젝트·결정·기한·금액 등 "다음에 이 분석을 다시 볼 때 알고 싶을 사실"만 뽑습니다.
잡담·인사·일반론은 제외합니다.
반드시 JSON으로만 응답하세요.`

const factExtractorPrompt = `다음 이메일 분석에서 위키에 기록할 만한 사실을 추출해주세요.

JSON 응답 형식:
{
  "facts": [
    {"entity": "엔티티 이름 (인물·회사·프로젝트·거래)", "type": "person|org|project|deal|decision|deadline", "fact": "기록할 사실 한 문장"}
  ]
}

추출 기준:
- 새로 알게 된 구체적 사실만 (자명한 일반 정보는 제외)
- 이름·숫자·날짜 포함
- 최대 6개
- 기록할 사실이 없으면 facts 배열을 비워서 응답

## 분석 결과
%s`

// WikiFactProposal is a single fact suggested for wiki write-back.
type WikiFactProposal struct {
	Entity string `json:"entity"`
	Type   string `json:"type"`
	Fact   string `json:"fact"`
}

// wikiFactsBundle is the JSON-mode response wrapper. Local LLM JSON mode
// requires an object root; this carries the fact array.
type wikiFactsBundle struct {
	Facts []WikiFactProposal `json:"facts"`
}

// extractFactsForWiki runs a local-AI extractor over the final analysis text
// and returns a pre-formatted Markdown block ready to append to the analyze
// output. The agent then writes each fact to wiki per the "분석 → 위키 갱신"
// system-prompt nudge.
//
// Best-effort: empty string when local AI is unavailable, extraction fails,
// or no qualifying facts are found.
func extractFactsForWiki(ctx context.Context, deps PipelineDeps, analysisText string) string {
	if deps.LocalClient == nil || deps.LocalModel == "" {
		return ""
	}
	if strings.TrimSpace(analysisText) == "" {
		return ""
	}

	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(factExtractorPrompt, analysisText)
	bundle, err := callLocalLLMJSON[wikiFactsBundle](extractCtx, deps.LocalClient, deps.LocalModel, factExtractorSystem, prompt, stage1MaxTokens)
	if err != nil || len(bundle.Facts) == 0 {
		return ""
	}
	return renderFactsBlock(bundle.Facts)
}

// renderFactsBlock formats a slice of WikiFactProposal as the Markdown block
// appended to the analyze output. Returns "" when no fact has both an entity
// and a fact (so the analyze output stays clean if extraction yields noise).
func renderFactsBlock(facts []WikiFactProposal) string {
	var sb strings.Builder
	sb.WriteString("📝 위키 갱신 제안 (자동 추출):\n")
	rendered := 0
	for _, f := range facts {
		entity := strings.TrimSpace(f.Entity)
		fact := strings.TrimSpace(f.Fact)
		if entity == "" || fact == "" {
			continue
		}
		typ := strings.TrimSpace(f.Type)
		if typ != "" {
			fmt.Fprintf(&sb, "- **%s** (%s): %s\n", entity, typ, fact)
		} else {
			fmt.Fprintf(&sb, "- **%s**: %s\n", entity, fact)
		}
		rendered++
	}
	if rendered == 0 {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

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
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
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
	return body[:maxChars] + "\n... (생략)"
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
