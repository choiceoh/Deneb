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
const transferSystemPrompt = `You are a memory curator for a personal AI assistant.
Your input is a COMPACTION SUMMARY — compressed conversation history that has already been distilled from raw messages.

## Task
Extract lasting, reusable knowledge that should be preserved as permanent long-term memory.
The summary will eventually be deleted by further compaction, so anything important must be extracted NOW.

## What to extract (in priority order)

1. **결정사항 (Decisions)**: Architectural choices, design decisions, tool/framework selections that constrain future work
2. **선호도 (Preferences)**: User's work style, communication preferences, coding conventions
3. **해결방법 (Solutions)**: Reusable problem-solution pairs, debugging patterns, workarounds
4. **사용자 모델 (User Model)**: Inferred traits about the user — expertise, values, habits
5. **상호 인식 (Mutual)**: AI-user relationship dynamics — corrections, trust signals, expectations

## Speaker Attribution (화자 귀속) — CRITICAL
The summary contains actions by both 선택님 (사용자) and 네브 (AI).
You MUST correctly attribute WHO said or did what:
- If 네브 summarized, listed, or explained something → that is 네브's action
- If 선택님 requested, asked, or decided something → that is 선택님's action
- Do NOT write "선택님이 X에 관심을 가짐" unless 선택님 explicitly brought up X
- Do NOT confuse 네브's proposals/suggestions with 선택님's requests

## What NOT to extract
- ❌ 일시적 작업 상태: "X 파일 수정 중", "Y 브랜치 작업 중"
- ❌ 루틴 코드 작업: "버그 수정", "기능 추가", "리팩토링"
- ❌ 이미 완료된 단발성 작업
- ❌ 구현 디테일: 함수명, 변수명, 파일 경로
- ❌ 잘못된 화자 귀속: AI가 한 말을 "사용자가 ~함"으로 기록

## Output format
Return a JSON object with a "facts" key containing an array of fact objects:
- "content": Korean, concise (1-2 sentences). Include reasoning basis
- "category": one of "decision", "preference", "solution", "user_model", "mutual", "context"
- "importance": 0.0-1.0
  - 0.9+: core decisions, persistent preferences
  - 0.7-0.9: reusable solutions, strong patterns
  - 0.5-0.7: useful context worth preserving
  - Below 0.5: do not extract
- "expiry_hint": null or "YYYY-MM-DD" if time-sensitive

Example: {"facts": [{"content": "프로젝트에서 Go gateway + Rust core FFI 아키텍처를 채택 — 성능 critical path는 Rust, 나머지는 Go", "category": "decision", "importance": 0.95, "expiry_hint": null}]}

## Rules
- Max 5 facts per summary. Quality over quantity
- Minimum importance 0.5 — only extract what's worth remembering permanently
- If nothing worth extracting, return {"facts": []}
- Return ONLY valid JSON, no markdown fences, no explanation`

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

// extractFactsFromSummary calls the LLM to extract structured facts from a summary.
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
		text, err := callTransferLLM(ctx, client, model, content)
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
	events, err := client.StreamChatOpenAI(ctx, llm.ChatRequest{
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
	text = jsonutil.ExtractObject(text)

	// Expected: {"facts": [...]}
	var resp struct {
		Facts []memory.ExtractedFact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err == nil && resp.Facts != nil {
		return resp.Facts, true
	}

	// Fallback: bare array.
	var arr []memory.ExtractedFact
	if err := json.Unmarshal([]byte(text), &arr); err == nil {
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
