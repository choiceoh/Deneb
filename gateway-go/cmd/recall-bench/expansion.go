// expansion.go — the bench-side LLM expander for A/B-ing the vocabulary-gap
// backfill (internal/domain/wiki/query_expansion.go). Talks to the wormhole
// script endpoint (the single LLM URL for offline tooling) so the bench never
// touches the gateway's role registry or the shared local engines.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// benchExpansionSystem mirrors the production expander prompt
// (runtime/server/wiki_query_expander.go) so the A/B measures the mechanism,
// not a prompt difference.
const benchExpansionSystem = "너는 업무 위키 검색어 브리지다. 사용자 질의의 어휘를 " +
	"태양광·전선·기자재 업무 위키가 실제로 쓸 법한 한국어 표현으로 바꾼 검색어를 " +
	"만들어라. 규칙: ①검색어 2개, 한 줄에 하나 ②원 질의에 이미 있는 단어 반복 금지 " +
	"③설명·번호·따옴표 없이 검색어만 출력."

// benchQueryExpander returns a QueryExpander backed by DENEB_EXPANSION_URL
// (default: the wormhole script endpoint). MaxTokens stays generous (2000):
// -api cloud models return EMPTY content on small budgets (#3338 pitfall).
func benchQueryExpander(model string) wiki.QueryExpander {
	baseURL := strings.TrimSpace(os.Getenv("DENEB_EXPANSION_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18800/v1"
	}
	client := llm.NewClient(baseURL, os.Getenv("DENEB_EXPANSION_API_KEY"))
	systemJSON, _ := json.Marshal(benchExpansionSystem)
	// Thinking off for dual-mode reasoning models (qwen burned the whole
	// budget on chain-of-thought — 7k reasoning chars, empty content); models
	// that ignore the kwarg still have the generous MaxTokens as headroom.
	kwargs, _ := json.Marshal(map[string]any{"enable_thinking": false})
	return func(ctx context.Context, intent string) []string {
		text, err := client.Complete(ctx, llm.ChatRequest{
			Model:     model,
			System:    llm.FlexibleFromRaw(systemJSON),
			Messages:  []llm.Message{llm.NewTextMessage("user", "질의: "+intent)},
			MaxTokens: 2000,
			ExtraBody: map[string]llm.FlexibleJSON{"chat_template_kwargs": llm.FlexibleFromRaw(kwargs)},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "expansion skipped (%v)\n", err)
			return nil
		}
		var terms []string
		for _, line := range strings.Split(text, "\n") {
			line = strings.Trim(strings.TrimSpace(line), "-•*\"'`")
			line = strings.TrimSpace(line)
			if line == "" || strings.Contains(intent, line) || len([]rune(line)) > 40 {
				continue
			}
			terms = append(terms, line)
		}
		return terms
	}
}
