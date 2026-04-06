package gmailpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Pipeline timeouts.
const (
	stage1Timeout = 30 * time.Second
	stage2Timeout = 60 * time.Second

	// Stage 1a: max emails to fetch for context.
	maxThreadMessages = 5
	maxSenderMessages = 3

	// Stage 1 LLM token limits.
	stage1MaxTokens = 768
	// Stage 2 (final analysis) token limit.
	stage2MaxTokens   = 1536
	batchStage2Tokens = 4096 // batch analysis needs more tokens

	// Stage 3: memory extraction.
	stage3Timeout   = 30 * time.Second
	stage3MaxTokens = 768
)

// PipelineDeps holds dependencies for the multi-stage analysis pipeline.
type PipelineDeps struct {
	GmailClient *gmail.Client
	LLMClient   *llm.Client // main LLM for final analysis (stage 2)
	LocalClient *llm.Client // local AI for extractors (stage 1)
	LocalModel  string      // local AI model name
	MainModel   string      // main LLM model name
	Logger      *slog.Logger // optional; nil = slog.Default()
}

// canRunPipeline returns true if we have enough deps for the multi-stage pipeline.
func (d *PipelineDeps) canRunPipeline() bool {
	return d.LocalClient != nil && d.LocalModel != "" && d.GmailClient != nil
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

type emailFactsResponse struct {
	Facts []EmailFact `json:"facts"`
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

const memoryExtractorSystem = `당신은 기억 검색 분석기입니다. 메모리에서 검색된 관련 정보를 현재 이메일 맥락에 맞게 정리합니다.
반드시 JSON으로만 응답하세요.`

const memoryExtractorPrompt = `다음은 현재 이메일과 관련해 메모리에서 검색된 정보입니다.
현재 이메일 분석에 도움이 되는 맥락을 추출해주세요.

JSON으로 응답하세요:
{
  "sender_facts": "이 발신자에 대해 알고 있는 정보",
  "topic_facts": "이메일 주제와 관련된 기억/정보",
  "relevant_history": "관련된 과거 맥락이나 결정사항"
}

관련 정보가 없으면 해당 필드를 빈 문자열로 응답하세요.

## 현재 이메일 요약
From: %s
Subject: %s

## 메모리 검색 결과
%s`

// SourceEmailAnalysis is the fact source identifier for email-derived facts.
const SourceEmailAnalysis = "email_analysis"

const emailFactExtractorSystem = `당신은 이메일 정보 분류기입니다. 이메일 분석 결과에서 장기적으로 기억할 가치가 있는 사실을 추출합니다.
반드시 JSON으로만 응답하세요.`

const emailFactExtractorPrompt = `다음은 이메일과 그 분석 결과입니다.
장기적으로 기억할 가치가 있는 사실을 추출해주세요.

## 추출 기준
- 발신자에 대한 정보 (역할, 소속, 관계)
- 프로젝트/업무 관련 결정사항이나 약속
- 반복될 수 있는 요청 패턴
- 향후 이메일 분석에 도움이 될 맥락

## 추출하지 않을 것
- 일회성 알림이나 뉴스레터 내용
- 인증 코드, 배송 추적 등 일시적 정보
- 단순한 안부 인사

JSON으로 응답하세요:
{
  "facts": [
    {
      "content": "사실 내용 (한국어, 1-2문장)",
      "category": "context|decision|preference|solution",
      "importance": 0.5-0.9,
      "expiry_hint": null 또는 "YYYY-MM-DD",
      "project": "관련 프로젝트명 (없으면 빈 문자열)"
    }
  ]
}

기억할 것이 없으면 {"facts": []}으로 응답하세요. 최대 5개.

## 이메일
From: %s
Subject: %s
Date: %s

## 분석 결과
%s`

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

// AnalyzeEmailPipeline runs the 3-stage multi-LLM analysis pipeline.
// Falls back to single-LLM analysis if pipeline deps are insufficient.
func AnalyzeEmailPipeline(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) (string, error) {
	if !deps.canRunPipeline() {
		// Fallback: single-LLM analysis (existing behavior).
		return AnalyzeEmail(ctx, deps.LLMClient, deps.MainModel, DefaultPrompt, msg)
	}

	// Stage 1: parallel extraction.
	var (
		threadCtx ThreadContext
		memoryCtx MemoryContext
		threadErr error
		memoryErr error
		wg        sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		stage1aCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
		defer cancel()
		threadCtx, threadErr = extractThreadContext(stage1aCtx, deps, msg)
	}()

	wg.Wait()

	// Log errors but don't fail — graceful degradation.
	_ = threadErr
	_ = memoryErr

	// Stage 2: final analysis combining all context.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	analysis, err := synthesizeAnalysis(stage2Ctx, deps, msg, threadCtx, memoryCtx)
	if err != nil {
		return "", err
	}

	return analysis, nil
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

// emailWithContext pairs an email with its pre-extracted Stage 1 context.
type emailWithContext struct {
	Msg       *gmail.MessageDetail
	ThreadCtx ThreadContext
	MemoryCtx MemoryContext
}

// AnalyzeBatch runs the multi-stage pipeline on a batch of emails,
// producing a single consolidated report grouped by project and priority.
// For a single email, delegates to AnalyzeEmailPipeline.
func AnalyzeBatch(ctx context.Context, deps PipelineDeps, msgs []*gmail.MessageDetail) (string, error) {
	if len(msgs) == 0 {
		return "", fmt.Errorf("no emails to analyze")
	}

	// Single email — use the individual pipeline.
	if len(msgs) == 1 {
		return AnalyzeEmailPipeline(ctx, deps, msgs[0])
	}

	// Stage 1: extract context for each email in parallel.
	enriched := make([]emailWithContext, len(msgs))
	var wg sync.WaitGroup

	for i, msg := range msgs {
		enriched[i].Msg = msg

		if !deps.canRunPipeline() {
			continue
		}

		// Thread context.
		wg.Add(1)
		go func(idx int, m *gmail.MessageDetail) {
			defer wg.Done()
			s1Ctx, cancel := context.WithTimeout(ctx, stage1Timeout)
			defer cancel()
			tc, err := extractThreadContext(s1Ctx, deps, m)
			if err == nil {
				enriched[idx].ThreadCtx = tc
			}
		}(i, msg)

	}

	wg.Wait()

	// Stage 2: batch synthesis — all emails in one LLM call.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	report, err := synthesizeBatchReport(stage2Ctx, deps, enriched)
	if err != nil {
		return "", err
	}

	return report, nil
}

// synthesizeBatchReport generates the consolidated report from all enriched emails.
func synthesizeBatchReport(ctx context.Context, deps PipelineDeps, emails []emailWithContext) (string, error) {
	// Build the combined email block with context annotations.
	var sb strings.Builder
	for i, e := range emails {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "### 메일 %d\n", i+1)
		sb.WriteString(FormatEmailForAnalysis(e.Msg))

		if hasThreadContext(e.ThreadCtx) {
			sb.WriteString("\n[이전 맥락] ")
			if e.ThreadCtx.ThreadSummary != "" {
				sb.WriteString(e.ThreadCtx.ThreadSummary)
			}
			if e.ThreadCtx.PriorExchanges != "" {
				sb.WriteString(" / ")
				sb.WriteString(e.ThreadCtx.PriorExchanges)
			}
			sb.WriteString("\n")
		}

		if hasMemoryContext(e.MemoryCtx) {
			sb.WriteString("[기억] ")
			if e.MemoryCtx.SenderFacts != "" {
				sb.WriteString(e.MemoryCtx.SenderFacts)
			}
			if e.MemoryCtx.TopicFacts != "" {
				sb.WriteString(" / ")
				sb.WriteString(e.MemoryCtx.TopicFacts)
			}
			sb.WriteString("\n")
		}
	}

	userPrompt := fmt.Sprintf(batchAnalysisPrompt, len(emails), sb.String())

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
		query := fmt.Sprintf("subject:\"%s\"", subj)
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

// synthesizeAnalysis combines the email with extracted contexts for final LLM analysis.
func synthesizeAnalysis(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail, tc ThreadContext, mc MemoryContext) (string, error) {
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

	req := llm.ChatRequest{
		Model:     deps.MainModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:    llm.SystemString(finalAnalysisSystem),
		MaxTokens: stage2MaxTokens,
		Stream:    true,
	}

	events, err := deps.LLMClient.StreamChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("final analysis LLM call failed: %w", err)
	}

	return collectStreamText(ctx, events)
}

// normalizeCategory ensures the category is valid for the memory store.
func normalizeCategory(cat string) string {
	switch cat {
	case "context", "decision", "preference", "solution", "user_model", "mutual":
		return cat
	default:
		return "context"
	}
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

// extractDisplayName extracts the display name from a "Name <email>" string.
func extractDisplayName(from string) string {
	if idx := strings.LastIndex(from, "<"); idx > 0 {
		return strings.TrimSpace(from[:idx])
	}
	return ""
}

// stripReplyPrefix removes Re:, Fwd:, etc. from an email subject.
func stripReplyPrefix(subject string) string {
	s := strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(s)
		if strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "fw:") {
			s = strings.TrimSpace(s[3:])
		} else if strings.HasPrefix(lower, "fwd:") {
			s = strings.TrimSpace(s[4:])
		} else {
			break
		}
	}
	return s
}

func hasThreadContext(tc ThreadContext) bool {
	return tc.ThreadSummary != "" || tc.PriorExchanges != "" || len(tc.OngoingTopics) > 0 || tc.SenderRelation != ""
}

func hasMemoryContext(mc MemoryContext) bool {
	return mc.SenderFacts != "" || mc.TopicFacts != "" || mc.RelevantHistory != ""
}
