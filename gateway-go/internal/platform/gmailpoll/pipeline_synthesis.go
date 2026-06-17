// pipeline_synthesis.go — stages of AnalyzeEmailPipeline (pipeline.go):
// stage-1 context extraction (thread, sender memory, wiki graph) and the
// stage-2 synthesis call, plus the importance-verdict and related-project
// suffixes parsed out of the synthesized text.
package gmailpoll

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/hanja"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func extractThreadContext(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) (ThreadContext, error) {
	var zero ThreadContext

	related := gatherRelatedMessages(ctx, deps, msg)
	if len(related) == 0 {
		return zero, nil // no source wired, or nothing related → skip
	}

	relatedEmails := make([]string, 0, len(related))
	for _, rm := range related {
		relatedEmails = append(relatedEmails, formatEmailBrief(rm))
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

// gatherRelatedMessages collects prior messages related to msg from whichever
// source is wired: the Gmail API (poll path) or the local archive (LMTP path).
func gatherRelatedMessages(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) []*gmail.MessageDetail {
	switch {
	case deps.GmailClient != nil:
		return gatherRelatedFromGmail(ctx, deps, msg)
	case deps.ThreadSource != nil:
		rel, err := deps.ThreadSource.RelatedMessages(ctx, msg)
		if err != nil {
			if deps.Logger != nil {
				deps.Logger.Warn("archive thread lookup failed", "error", err)
			}
			return nil // best-effort: proceed without thread context
		}
		return rel
	default:
		return nil // no thread source (e.g. LMTP without archive configured)
	}
}

// gatherRelatedFromGmail is the original Gmail-API gather: the real conversation
// thread (by threadId) plus the sender's recent messages, oldest-first-ish.
func gatherRelatedFromGmail(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) []*gmail.MessageDetail {
	var related []*gmail.MessageDetail

	// 1. The actual Gmail conversation thread. Every message carries a stable
	// threadId, so we fetch the real thread in one call rather than guessing it
	// by subject (which missed subject-changed replies and over-matched common
	// subjects).
	if msg.ThreadID != "" {
		if threadMsgs, err := deps.GmailClient.GetThread(ctx, msg.ThreadID); err == nil {
			for _, tm := range threadMsgs {
				if tm.ID == msg.ID {
					continue // skip the message being analyzed
				}
				related = append(related, tm)
				if len(related) >= maxThreadMessages {
					break
				}
			}
		}
	}

	// 2. Recent emails from the same sender.
	senderEmail := extractEmailAddr(msg.From)
	if senderEmail != "" {
		query := fmt.Sprintf("from:%s newer_than:30d", senderEmail)
		if senderMsgs, err := deps.GmailClient.Search(ctx, query, maxSenderMessages+1); err == nil {
			for _, sm := range senderMsgs {
				if sm.ID == msg.ID {
					continue
				}
				detail, err := deps.GmailClient.GetMessage(ctx, sm.ID)
				if err != nil {
					continue
				}
				related = append(related, detail)
				if len(related) >= maxThreadMessages+maxSenderMessages {
					break
				}
			}
		}
	}
	return related
}

// graphifyQueryTimeout caps how long the wiki-graph subprocess may run before
// the pipeline gives up and proceeds without graph context.
const graphifyQueryTimeout = 10 * time.Second

// maxSenderFactsChars bounds graphify output so the analyze prompt stays small.
const maxSenderFactsChars = 2000

// extractSenderContext resolves "who is this person to us" for the sender.
// Prefers the in-process wiki graph (deps.SenderFactsFn) — always current, no
// subprocess — and falls back to the external graphify CLI snapshot only when
// no in-process resolver is wired or it returns nothing. Best-effort: an empty
// MemoryContext on every failure path never blocks the pipeline.
func extractSenderContext(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) MemoryContext {
	name := extractDisplayName(msg.From)
	if name == "" {
		return MemoryContext{}
	}
	if deps.SenderFactsFn != nil {
		if facts := strings.TrimSpace(deps.SenderFactsFn(ctx, name)); facts != "" {
			if len(facts) > maxSenderFactsChars {
				facts = facts[:maxSenderFactsChars] + "\n...(생략)"
			}
			return MemoryContext{SenderFacts: facts}
		}
	}
	return extractWikiGraphContext(ctx, msg)
}

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

	// Read the attachments a local LLM judges relevant (견적서/계약서 등) so the
	// analysis — and the deal extractor downstream — see the actual document
	// content, not just the body. Best-effort: an empty selection leaves the
	// analysis body-only.
	attach := gateAndExtractAttachments(ctx, deps, msg)
	emailText += attach.Injected

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

	userPrompt := fmt.Sprintf(finalAnalysisPrompt, analysisPrompt(deps), emailText, threadSection, memorySection)
	userPrompt += projectSelectionSuffix(candidates)
	userPrompt += importanceSuffix

	// Token budget: the interactive Mini App path raises this so a deliberate
	// analysis can synthesize at depth; autonomous polling keeps the tighter
	// default to bound cost/latency.
	maxTok := stage2MaxTokens
	if deps.Stage2MaxTokens > 0 {
		maxTok = deps.Stage2MaxTokens
	}

	// Reasoning is disabled by default: GLM-5.1 and the local vLLM (OpenAI-mode)
	// stream chain-of-thought into the answer body as ordinary text, which
	// collectStreamText can't tell apart from the analysis. DeepThinking flips it
	// on ONLY when the synthesis provider emits reasoning as distinct Anthropic
	// thinking blocks (analysisThinking gates on APIMode); stripReasoningLeak
	// below still scrubs any stray marker as belt-and-suspenders.
	//
	// TemplateKwarg carries the model's chat_template_kwargs off-switch (e.g.
	// dsv4's "thinking"): without it, "disabled" falls back to reasoning_effort,
	// a no-op on dual-mode vLLM models — they keep reasoning, exhaust the token
	// budget, and return EMPTY content (the main chat path sets this via
	// applyModelTuning; this is the analysis-path equivalent). Empty for non-vLLM
	// models, where Anthropic-wire thinking handling applies instead.
	thinking := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: deps.ThinkingKwarg}
	if deps.DeepThinking {
		thinking = analysisThinking(deps.LLMClient, maxTok)
	}
	req := llm.ChatRequest{
		Model:     deps.MainModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:    llm.SystemString(finalAnalysisSystem),
		MaxTokens: maxTok,
		Stream:    true,
		Thinking:  thinking,
	}

	events, err := deps.LLMClient.StreamChat(ctx, req)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("final analysis LLM call failed: %w", err)
	}

	analysis, err := collectStreamText(ctx, events)
	if err != nil {
		return AnalysisResult{}, err
	}
	analysis = sanitizeAnalysisLeak(analysis)

	// Parse + strip the RELATED_PROJECTS tag before appending the facts
	// block, so the tag stays at the analysis tail where the parser expects
	// it (the facts block would otherwise bury it).
	clean, projects := parseRelatedProjects(analysis, candidates)
	clean, importance := parseImportance(clean)

	// Extract the operator's follow-up actions from the analysis prose — before
	// the facts block is appended below, so the extractor sees only the analysis
	// and not the "위키 갱신 제안" addendum. Best-effort; the server sink turns
	// high-priority items into to-dos.
	actions := extractActionItems(ctx, deps, clean)

	// Local-AI fact extraction for wiki write-back. The system prompt's
	// "분석 → 위키 갱신" section asks the agent to record new facts after
	// analyzing; this attaches a structured proposal block so the agent has
	// concrete `wiki(action="write")` inputs rather than having to derive
	// them from prose. Best-effort — empty when local AI is unavailable or
	// yields nothing to record.
	if factsBlock := extractFactsForWiki(ctx, deps, clean); factsBlock != "" {
		clean = clean + "\n\n" + factsBlock
	}

	// Deal-document extraction is gated on attachments: business documents
	// (견적서/계약서/세금계산서) arrive as files, so this avoids an extra local
	// call on every plain mail and keeps extraction precise. We feed the extractor
	// the verbatim attachment text (attach.Injected) alongside the analysis: the
	// synthesized prose reads well for a human but may have rounded or summarized
	// away the document's exact 금액·품목·납기, which the structured deal record
	// must keep faithfully. The analysis gives the who/why context; the raw
	// attachment gives the precise figures.
	var deal *DealInfo
	if len(msg.Attachments) > 0 {
		dealInput := clean + attach.Injected // attach.Injected is "" when nothing was selected
		deal = extractDealInfo(ctx, deps, dealInput)
	}

	// Surface a deep-review hint for attachments the gate judged dense enough to
	// warrant the chat agent's full read — a one-tap escalation, not a second
	// autonomous pass.
	if len(attach.DeepReview) > 0 {
		clean = clean + "\n\n📎 정밀 검토 권장: " + strings.Join(attach.DeepReview, ", ") +
			" — 자세히 보려면 채팅에서 이 첨부를 열어 분석을 요청하세요."
	}

	// Read Sino-Korean Hanja in the report as Hangul (報告書 → 보고서) — the
	// analysis model may be a Chinese-lineage one (analysis role can be cloud
	// GLM). Applied to the final prose only; the structured Deal/ActionItems were
	// already extracted above from the pre-conversion text.
	return AnalysisResult{Text: hanja.Transliterate(clean), RelatedProjects: projects, ActionItems: actions, Deal: deal, Importance: importance}, nil
}

// --- importance verdict ---

// importanceSuffix asks the model to end the analysis with one structured
// triage line. Same tag-line pattern as RELATED_PROJECTS: the prose stays
// free-form, only the last line is machine-readable.
const importanceSuffix = "\n\n## 중요도 분류\n" +
	"응답의 가장 마지막 줄에 정확히 다음 형식으로 이 메일의 중요도를 분류하라:\n" +
	"IMPORTANCE: 긴급|확인|참고 중 정확히 하나\n" +
	"긴급=마감·금전·계약·승인 등 즉시 행동이 필요한 메일, 확인=업무 관련이라 곧 봐야 하는 메일, 참고=알림·자동발신·FYI.\n"

// parseImportance extracts and strips the IMPORTANCE tag line, returning the
// cleaned text and the normalized tier ("urgent"/"attention"/"routine", ""
// when absent or unrecognized — callers fall back to the heuristic scorer).
func parseImportance(text string) (string, string) {
	lines := strings.Split(text, "\n")
	keep := make([]string, 0, len(lines))
	tier := ""
	for _, line := range lines {
		rest, ok := cutTagPrefix(strings.TrimSpace(line), "IMPORTANCE:")
		if !ok {
			keep = append(keep, line)
			continue
		}
		switch {
		case strings.Contains(rest, "긴급"), strings.Contains(strings.ToLower(rest), "urgent"):
			tier = "urgent"
		case strings.Contains(rest, "확인"), strings.Contains(strings.ToLower(rest), "attention"):
			tier = "attention"
		case strings.Contains(rest, "참고"), strings.Contains(strings.ToLower(rest), "routine"):
			tier = "routine"
		}
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n"), tier
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
