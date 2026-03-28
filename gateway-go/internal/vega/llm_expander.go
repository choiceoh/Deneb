package vega

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	expandTimeout   = 5 * time.Second
	expandMaxTokens = 128
)

const expandSystemPrompt = `프로젝트 관리 DB 검색 쿼리 확장기.
주어진 검색 쿼리에서 3-5개 추가 검색어를 생성하라.
한국어 동의어, 관련 기술 용어, 약어, 영문 대응어를 포함하라.
반드시 JSON 문자열 배열만 출력하라. 사고 과정, 설명, 마크다운 없이 순수 JSON 배열만.
예: ["태양광", "solar", "PV", "발전소"]`

// LLMExpander generates expanded search terms via SGLang chat completion.
type LLMExpander struct {
	client *llm.Client
	model  string
	logger *slog.Logger
}

// NewLLMExpander creates a query expander that calls the SGLang server.
func NewLLMExpander(baseURL, model string, logger *slog.Logger) *LLMExpander {
	if logger == nil {
		logger = slog.Default()
	}
	return &LLMExpander{
		client: llm.NewClient(baseURL, "", llm.WithLogger(logger)),
		model:  model,
		logger: logger,
	}
}

// Expand generates additional search terms for the given query.
// Returns nil on timeout or error (caller should proceed with original query).
func (e *LLMExpander) Expand(ctx context.Context, query string) []string {
	if len(query) < 2 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, expandTimeout)
	defer cancel()

	req := llm.ChatRequest{
		Model:     e.model,
		Messages:  []llm.Message{llm.NewTextMessage("user", query)},
		System:    llm.SystemString(expandSystemPrompt),
		MaxTokens: expandMaxTokens,
		Stream:    true,
	}

	events, err := e.client.StreamChatOpenAI(ctx, req)
	if err != nil {
		e.logger.Debug("query expansion failed", "error", err)
		return nil
	}

	text := collectStreamText(ctx, events)
	if text == "" {
		return nil
	}

	// Parse JSON array from response.
	text = strings.TrimSpace(text)
	// Strip markdown code fences if present.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	// Extract JSON array even if the LLM produced preamble text.
	var terms []string
	if err := json.Unmarshal([]byte(text), &terms); err != nil {
		// Try to find a JSON array embedded in the response.
		if start := strings.Index(text, "["); start >= 0 {
			if end := strings.LastIndex(text, "]"); end > start {
				if err2 := json.Unmarshal([]byte(text[start:end+1]), &terms); err2 != nil {
					e.logger.Debug("query expansion parse failed", "raw", text, "error", err2)
					return nil
				}
			}
		}
		if terms == nil {
			e.logger.Debug("query expansion parse failed", "raw", text, "error", err)
			return nil
		}
	}

	if len(terms) > 10 {
		terms = terms[:10]
	}

	e.logger.Debug("query expanded", "query", query, "terms", terms)
	return terms
}

// BuildExpandedQuery combines the original query with expanded terms for FTS.
func BuildExpandedQuery(original string, expanded []string) string {
	if len(expanded) == 0 {
		return original
	}
	parts := make([]string, 0, len(expanded)+1)
	parts = append(parts, original)
	parts = append(parts, expanded...)
	return strings.Join(parts, " OR ")
}

// collectStreamText collects all text deltas from a stream.
func collectStreamText(ctx context.Context, events <-chan llm.StreamEvent) string {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			return sb.String()
		case ev, ok := <-events:
			if !ok {
				return sb.String()
			}
			if ev.Type == "content_block_delta" {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil {
					sb.WriteString(delta.Delta.Text)
				}
			}
		}
	}
}

// ExpandedSearchQuery holds the original and expanded queries.
type ExpandedSearchQuery struct {
	Original string   `json:"original"`
	Expanded []string `json:"expanded,omitempty"`
	Combined string   `json:"combined"`
}

// ExpandQuery runs expansion and builds the combined query.
func (e *LLMExpander) ExpandQuery(ctx context.Context, query string) ExpandedSearchQuery {
	expanded := e.Expand(ctx, query)
	return ExpandedSearchQuery{
		Original: query,
		Expanded: expanded,
		Combined: BuildExpandedQuery(query, expanded),
	}
}

// FormatForLog returns a concise string for logging.
func (eq ExpandedSearchQuery) FormatForLog() string {
	if len(eq.Expanded) == 0 {
		return eq.Original
	}
	return fmt.Sprintf("%s (+%d terms)", eq.Original, len(eq.Expanded))
}
