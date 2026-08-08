// wiki_query_expander.go — tiny-role LLM expander for the wiki's vocabulary-gap
// backfill (domain/wiki/query_expansion.go). The expander is DORMANT unless
// DENEB_WIKI_QUERY_EXPANSION=backfill: the store only calls it when that gate
// is on AND a query under-filled its result limit, so wiring it unconditionally
// costs nothing at rest. Role, not model, is chosen here (model-roles rule) —
// tiny is the measured helper tier for small-budget rewrites.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// wikiExpanderTimeout bounds one expansion call. The recall preflight runs
// SearchPlan under its own ~1.5s budget (the ctx deadline still applies — this
// is a ceiling for the chat wiki TOOL path, which has no preflight cap).
const wikiExpanderTimeout = 4 * time.Second

const wikiExpanderSystem = "너는 업무 위키 검색어 브리지다. 사용자 질의의 어휘를 " +
	"태양광·전선·기자재 업무 위키가 실제로 쓸 법한 한국어 표현으로 바꾼 검색어를 " +
	"만들어라. 규칙: ①검색어 2개, 한 줄에 하나 ②원 질의에 이미 있는 단어 반복 금지 " +
	"③설명·번호·따옴표 없이 검색어만 출력."

// makeWikiQueryExpander returns the QueryExpander closure for the store. Errors
// and timeouts yield nil — expansion is best-effort by contract, the primary
// results always stand. extraBody carries the registry-aware thinking-off
// directive (dreamerLLMShape's pattern): a dual-mode reasoning model on a tiny
// budget otherwise burns the whole budget on chain-of-thought and returns
// empty content (measured on qwen: 7k reasoning chars, finish_reason=length).
func makeWikiQueryExpander(client *llm.Client, model string, extraBody map[string]any, logger *slog.Logger) wiki.QueryExpander {
	return func(ctx context.Context, intent string) []string {
		ctx, cancel := context.WithTimeout(ctx, wikiExpanderTimeout)
		defer cancel()
		systemJSON, _ := json.Marshal(wikiExpanderSystem)
		req := llm.ChatRequest{
			Model:     model,
			System:    llm.FlexibleFromRaw(systemJSON),
			Messages:  []llm.Message{llm.NewTextMessage("user", "질의: "+intent)},
			MaxTokens: 600,
		}
		if len(extraBody) > 0 {
			req.ExtraBody = make(map[string]llm.FlexibleJSON, len(extraBody))
			for k, v := range extraBody {
				req.ExtraBody[k] = llm.FlexibleFromValue(v)
			}
		}
		text, err := client.Complete(ctx, req)
		if err != nil {
			if logger != nil {
				logger.Debug("wiki query expansion skipped", "error", err)
			}
			return nil
		}
		var terms []string
		for _, line := range strings.Split(text, "\n") {
			line = strings.Trim(strings.TrimSpace(line), "-•*\"'`")
			line = strings.TrimSpace(line)
			// Drop empties, echoes of the intent, and prose-length lines (a
			// chatty model answering instead of listing).
			if line == "" || strings.Contains(intent, line) || len([]rune(line)) > 40 {
				continue
			}
			terms = append(terms, line)
		}
		return terms
	}
}
