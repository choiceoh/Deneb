// memory_transfer.go — Extracts important facts from Aurora compaction summaries
// and transfers them to the structured memory store as permanent long-term memory.
//
// When Aurora compacts conversation history into condensed summaries (depth >= 1),
// the distilled knowledge is worth preserving beyond the compaction lifecycle.
// This module uses LLM-based fact extraction to identify and graduate important
// knowledge into the memory store, where it becomes searchable and permanent.
package aurora

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// MemoryTransferConfig configures which summaries are eligible for transfer.
type MemoryTransferConfig struct {
	// MinDepth is the minimum summary depth required for transfer.
	// Default: 1 (condensed summaries only; leaf summaries are too granular).
	MinDepth uint32
	// MinTokens is the minimum token count for a summary to be worth extracting from.
	MinTokens uint64
}

// DefaultMemoryTransferConfig returns production defaults.
func DefaultMemoryTransferConfig() MemoryTransferConfig {
	return MemoryTransferConfig{
		MinDepth:  1,
		MinTokens: 100,
	}
}

// ShouldTransfer checks whether a summary meets the criteria for memory transfer.
func ShouldTransfer(summary SummaryRecord, cfg MemoryTransferConfig) bool {
	return summary.Depth >= cfg.MinDepth && summary.TokenCount >= cfg.MinTokens
}

const (
	transferTimeout   = 45 * time.Second
	transferMaxTokens = 1536
)

// transferSystemPrompt is optimized for extracting lasting knowledge from
// Aurora compaction summaries (already compressed conversation history).
const transferSystemPrompt = `당신은 개인 AI 비서의 장기 기억 큐레이터입니다.
입력은 COMPACTION SUMMARY(원본 대화를 이미 압축/요약한 텍스트)입니다.

## 작업 목표
추가 압축으로 사라질 수 있는 중요한 지식을 영구 장기 기억으로 보존하세요.

## 추출 우선순위
1. **결정사항**: 아키텍처/설계 선택, 도구·프레임워크 채택 등 향후 작업 제약
2. **선호도**: 사용자의 작업 방식, 소통 방식, 코딩 관례 선호
3. **해결방법**: 재사용 가능한 문제-해결 패턴, 디버깅 요령, 워크어라운드
4. **사용자 모델**: 사용자의 전문성·가치관·습관 등 추론 가능한 특성
5. **상호 인식**: 사용자-네브 사이의 신뢰/기대/교정 신호

## 화자 귀속(매우 중요)
요약에는 선택님(사용자)과 네브(AI)의 행동이 함께 있습니다.
- 네브가 설명/제안/요약한 내용은 네브의 행동으로 귀속
- 선택님이 요청/질문/결정한 내용은 선택님의 행동으로 귀속
- 선택님이 명시하지 않은 관심사를 추정해 기록하지 말 것
- 네브의 제안을 선택님의 의도로 오인하지 말 것

## 추출 금지
- ❌ 일시적 작업 상태: "X 파일 수정 중", "Y 브랜치 작업 중"
- ❌ 루틴 코드 작업: "버그 수정", "기능 추가", "리팩토링"
- ❌ 완료된 단발성 작업
- ❌ 구현 디테일: 함수명, 변수명, 파일 경로
- ❌ 잘못된 화자 귀속

## 출력 형식
"facts" 키를 가진 JSON 객체를 반환하세요.
facts 배열의 각 원소:
- "content": 반드시 한국어, 1~2문장, 핵심 근거 포함
- "category": "decision" | "preference" | "solution" | "user_model" | "mutual" | "context"
- "importance": 0.0~1.0
  - 0.9+: 핵심 결정/지속 선호
  - 0.7~0.9: 재사용 가치 높은 해결 패턴
  - 0.5~0.7: 보존 가치 있는 맥락
  - 0.5 미만: 추출 금지
- "expiry_hint": null 또는 "YYYY-MM-DD" (시한성 정보일 때)

예시:
{"facts":[{"content":"프로젝트는 Go gateway + Rust core FFI 아키텍처를 채택했으며, 성능 임계 경로는 Rust로 유지한다.","category":"decision","importance":0.95,"expiry_hint":null}]}

## 규칙
- 요약 1개당 최대 5개 사실(양보다 질 우선)
- 중요도 0.5 미만은 포함하지 말 것
- 추출할 내용이 없으면 {"facts": []} 반환
- 설명/마크다운 없이 **유효한 JSON만** 반환`

// TransferSummaryToMemory extracts important facts from an Aurora summary
// and stores them in the structured memory store.
func TransferSummaryToMemory(
	ctx context.Context,
	summary SummaryRecord,
	store *Store,
	memStore *memory.Store,
	memEmbedder *memory.Embedder,
	llmClient *llm.Client,
	model string,
	logger *slog.Logger,
) error {
	if store.IsTransferred(summary.SummaryID) {
		logger.Debug("aurora-transfer: already transferred", "summaryId", summary.SummaryID)
		return nil
	}

	if summary.Content == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, transferTimeout)
	defer cancel()

	facts, err := extractFactsFromSummary(ctx, llmClient, model, summary.Content, logger)
	if err != nil {
		return fmt.Errorf("aurora-transfer: extract facts: %w", err)
	}

	if len(facts) == 0 {
		logger.Debug("aurora-transfer: no facts extracted", "summaryId", summary.SummaryID)
		return store.MarkTransferred(summary.SummaryID)
	}

	// Enforce minimum importance for transferred facts.
	for i := range facts {
		if facts[i].Importance < 0.5 {
			facts[i].Importance = 0.5
		}
	}

	memory.InsertExtractedFactsAs(ctx, memStore, memEmbedder, facts, memory.SourceAuroraTransfer, logger)

	logger.Info("aurora-transfer: transferred summary to memory",
		"summaryId", summary.SummaryID,
		"depth", summary.Depth,
		"factsCount", len(facts),
	)

	return store.MarkTransferred(summary.SummaryID)
}

// networkRetryBackoffs defines the sleep durations between retries for
// transient network errors in LLM calls.
var networkRetryBackoffs = []time.Duration{2 * time.Second, 4 * time.Second}

// isNetworkError returns true if the error is likely a transient network issue
// (as opposed to a response format error). Context errors are not retryable.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "529")
}

// callWithNetworkRetry calls fn and retries with exponential backoff on
// transient network errors. Non-network errors and context cancellation
// fail immediately.
func callWithNetworkRetry(ctx context.Context, fn func() (string, error), logger *slog.Logger) (string, error) {
	text, err := fn()
	if err == nil {
		return text, nil
	}
	if !isNetworkError(err) {
		return "", err
	}
	for _, backoff := range networkRetryBackoffs {
		logger.Debug("aurora-transfer: network error, retrying", "backoff", backoff, "error", err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}
		text, err = fn()
		if err == nil {
			return text, nil
		}
		if !isNetworkError(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("aurora-transfer: network error after retries: %w", err)
}

// extractFactsFromSummary calls the LLM to extract structured facts from a summary.
// Network errors are retried via callWithNetworkRetry; parse errors retry once
// (the LLM may produce valid JSON on a second attempt).
func extractFactsFromSummary(
	ctx context.Context,
	client *llm.Client,
	model string,
	summaryContent string,
	logger *slog.Logger,
) ([]memory.ExtractedFact, error) {
	// Truncate very long summaries to avoid excessive token usage.
	content := summaryContent
	if len([]rune(content)) > 12000 {
		runes := []rune(content)
		content = string(runes[:12000]) + "..."
	}

	for attempt := range 2 {
		text, err := callWithNetworkRetry(ctx, func() (string, error) {
			return callTransferLLM(ctx, client, model, content)
		}, logger)
		if err != nil {
			return nil, err
		}
		if text == "" || text == "[]" {
			return nil, nil
		}

		facts, ok := parseTransferResponse(text)
		if ok {
			// Validate and cap count.
			var valid []memory.ExtractedFact
			for _, f := range facts {
				if f.Content == "" || len(valid) >= 5 {
					break
				}
				if !isValidCategory(f.Category) {
					f.Category = "context"
				}
				valid = append(valid, f)
			}
			return valid, nil
		}

		if attempt == 0 {
			logger.Debug("aurora-transfer: parse failed, retrying",
				"raw", truncateStr(text, 200))
		}
	}

	return nil, nil
}

// callTransferLLM sends the summary to the LLM with JSON mode for fact extraction.
func callTransferLLM(ctx context.Context, client *llm.Client, model, summaryContent string) (string, error) {
	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", summaryContent)},
		System:         llm.SystemString(transferSystemPrompt),
		MaxTokens:      transferMaxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", fmt.Errorf("transfer LLM call: %w", err)
	}
	if events == nil {
		return "", fmt.Errorf("transfer LLM: nil event channel")
	}

	var sb strings.Builder
	for ev := range events {
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// parseTransferResponse parses the LLM JSON output into extracted facts.
func parseTransferResponse(text string) ([]memory.ExtractedFact, bool) {
	extracted := jsonutil.ExtractObject(text)

	// Expected: {"facts": [...]}
	var resp struct {
		Facts []memory.ExtractedFact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err == nil && resp.Facts != nil {
		return resp.Facts, true
	}

	// Fallback: bare array (ExtractObject may return "" for non-object JSON).
	raw := strings.TrimSpace(text)
	var arr []memory.ExtractedFact
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr, true
	}

	// Fallback: array under arbitrary key.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &obj); err == nil {
		for _, v := range obj {
			trimmed := strings.TrimSpace(string(v))
			if len(trimmed) > 0 && trimmed[0] == '[' {
				var nested []memory.ExtractedFact
				if err := json.Unmarshal(v, &nested); err == nil && len(nested) > 0 {
					return nested, true
				}
			}
		}
	}

	return nil, false
}

func isValidCategory(c string) bool {
	switch c {
	case "decision", "preference", "solution", "context", "user_model", "mutual":
		return true
	}
	return false
}

func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
