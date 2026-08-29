package sessionops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Truncate shortens s to maxLen runes, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// --- unified sessions tool ---

// ToolSessions creates the unified sessions tool with action dispatch.
//
// It owns both planes of the session surface: peer sessions (list/history/
// search/send/stats) and this session's own children (scope="children" on list,
// plus result/kill/steer). They were two tools until the 2026-08-29 audit found
// the boundary had eroded — `subagents` was literally the session list filtered
// by SpawnedBy, over the same Manager and the same SessionDeps, so "which tool
// lists sessions" was a routing question with no principled answer. One tool,
// one scope switch. Spawning stays its own tool: it mutates, and the prompt
// directs delegation by name.
func ToolSessions(d *tooldeps.SessionDeps) toolport.ToolFunc {
	subagents := ToolSubagents(d)
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Scope  string `json:"scope"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		switch p.Action {
		case "list":
			// scope=children reuses the subagent lister, which filters the same
			// Manager.List() by SpawnedBy and renders child status/steer hints.
			if strings.EqualFold(strings.TrimSpace(p.Scope), "children") {
				return subagents(ctx, input)
			}
			return toolSessionsList(d.Manager)(ctx, input)
		case "history":
			return toolSessionsHistory(d.Transcript)(ctx, input)
		case "search":
			return toolSessionsSearch(d.Transcript)(ctx, input)
		case "send":
			return toolSessionsSend(d)(ctx, input)
		case "stats":
			return toolSessionsStats(d)(ctx, input)
		case "result", "kill", "steer":
			return subagents(ctx, input)
		default:
			return "action은 list, history, search, send, stats, result, kill, steer 중 하나를 지정하세요 (list는 scope=children으로 내 서브에이전트만).", nil
		}
	}
}

// --- sessions stats sub-action ---

// toolSessionsStats rolls up run/token/tool totals from the agent log — the
// Go replacement for the retired session-logs skill's brittle jq recipes over raw
// JSONL. Window totals via Aggregate, per-session table via
// AggregateBySession. Tokens only: agentlog persists no dollar cost, so none
// is invented here (per-tool histograms live in observe action=behavior).
func toolSessionsStats(d *tooldeps.SessionDeps) toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		if d == nil || d.AgentLog == nil {
			return "agent log가 배선되지 않아 통계를 낼 수 없습니다.", nil
		}
		var p struct {
			Days  int `json:"days"`
			Limit int `json:"limit"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.Days <= 0 {
			p.Days = 7
		}
		if p.Limit <= 0 {
			p.Limit = 15
		}
		since := time.Now().Add(-time.Duration(p.Days) * 24 * time.Hour).UnixMilli()

		agg := d.AgentLog.Aggregate(since)
		var b strings.Builder
		fmt.Fprintf(&b, "세션 통계 (최근 %d일): runs=%d proactive=%d in=%d out=%d cacheRead=%d 토큰\n",
			p.Days, agg.Runs, agg.ProactiveRuns, agg.TotalInputTokens, agg.TotalOutputTokens, agg.CacheReadTokens)

		bySession := d.AgentLog.AggregateBySession(since)
		if len(bySession) == 0 {
			b.WriteString("기간 내 기록된 세션이 없습니다.")
			return b.String(), nil
		}
		if len(bySession) > p.Limit {
			fmt.Fprintf(&b, "\n토큰 상위 %d개 세션 (전체 %d개):\n", p.Limit, len(bySession))
			bySession = bySession[:p.Limit]
		} else {
			fmt.Fprintf(&b, "\n세션별 (%d개):\n", len(bySession))
		}
		for _, st := range bySession {
			fmt.Fprintf(&b, "- %s: runs=%d", st.Session, st.Runs)
			if st.Errors > 0 {
				fmt.Fprintf(&b, " errors=%d", st.Errors)
			}
			fmt.Fprintf(&b, " in=%d out=%d tools=%d · 마지막 %s\n",
				st.InputTokens, st.OutputTokens, st.ToolCalls,
				time.UnixMilli(st.LastTs).Format("01-02 15:04"))
		}
		b.WriteString("\n(도구별 사용 히스토그램은 observe action=behavior, 비용($)은 게이트웨이 미보유 — 토큰 기준)")
		return b.String(), nil
	}
}

// --- sessions list sub-action ---

// toolSessionsList returns a tool function that lists active sessions.
func toolSessionsList(sessions *session.Manager) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		currentKey := toolport.SessionKeyFromContext(ctx)

		if sessions == nil {
			return fmt.Sprintf("Current session: %s\nSession manager not available.", currentKey), nil
		}

		var p struct {
			Limit int      `json:"limit"`
			Kinds []string `json:"kinds"`
		}
		if len(input) > 0 {
			_ = json.Unmarshal(input, &p) // best-effort: use defaults on parse failure
		}

		allSessions := sessions.List()
		if len(allSessions) == 0 {
			return fmt.Sprintf("Current session: %s\nNo other sessions found.", currentKey), nil
		}

		// Apply kind filter if specified.
		kindFilter := make(map[string]struct{}, len(p.Kinds))
		for _, k := range p.Kinds {
			kindFilter[k] = struct{}{}
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Sessions (%d total):\n\n", len(allSessions))
		shown := 0
		limit := p.Limit
		if limit <= 0 {
			limit = 50
		}
		for _, s := range allSessions {
			if _, ok := kindFilter[string(s.Kind)]; len(kindFilter) > 0 && !ok {
				continue
			}
			if shown >= limit {
				break
			}
			marker := ""
			if s.Key == currentKey {
				marker = " ← current"
			}
			fmt.Fprintf(&sb, "- **%s** (kind=%s, status=%s, model=%s)%s\n",
				s.Key, s.Kind, s.Status, s.Model, marker)
			shown++
		}
		return sb.String(), nil
	}
}

// --- sessions history sub-action ---

// resolveHistoryKey turns a recall-evidence session ref into a loadable
// session key. Recall rows render refs as short tails ("s38#2/user",
// "kimi/k3#5/assistant") — the metadata diet cut the long key prefix because
// nothing consumed it in one-shot reading; the follow-up read route consumes
// it here instead, by resolving the tail back against the store's key list.
// Returns the resolved key, the #N anchor parsed off the ref (-1 when absent),
// and — when the tail matches several sessions — the candidate keys to report.
func resolveHistoryKey(transcript toolport.TranscriptStore, raw string) (key string, anchor int, candidates []string) {
	key, anchor = raw, -1
	if i := strings.IndexByte(key, '#'); i >= 0 {
		tail := key[i+1:]
		key = key[:i]
		digits := tail
		if j := strings.IndexByte(digits, '/'); j >= 0 {
			digits = digits[:j]
		}
		if n, err := strconv.Atoi(digits); err == nil && n >= 0 {
			anchor = n
		}
	}
	// The recall renderer abbreviates "client:" to "cl:"; undo it before
	// matching so an un-dieted ref also resolves exactly.
	expanded := key
	if strings.HasPrefix(key, "cl:") {
		expanded = "client:" + key[len("cl:"):]
	}
	keys, err := transcript.ListKeys()
	if err != nil {
		return key, anchor, nil
	}
	for _, k := range keys {
		if k == key || k == expanded {
			return k, anchor, nil
		}
	}
	for _, k := range keys {
		if strings.HasSuffix(k, ":"+key) || strings.HasSuffix(k, ":"+expanded) {
			candidates = append(candidates, k)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], anchor, nil
	}
	return key, anchor, candidates
}

// dateNote renders a header date suffix for a unix-milli timestamp, or "" when
// the timestamp is unknown. One formatter for both the history window and the
// search results so the two headers cannot drift apart.
func dateNote(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return " (" + time.UnixMilli(ts).Format("2006-01-02") + ")"
}

// firstTimestamp returns the first nonzero message timestamp in the slice.
func firstTimestamp(msgs []toolport.ChatMessage) int64 {
	for _, m := range msgs {
		if m.Timestamp > 0 {
			return m.Timestamp
		}
	}
	return 0
}

// toolSessionsHistory returns a tool function that retrieves session transcript history.
func toolSessionsHistory(transcript toolport.TranscriptStore) toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string           `json:"sessionKey"`
			Limit      artifact.FlexInt `json:"limit"`
			Around     artifact.FlexInt `json:"around"`
		}
		if err := jsonutil.UnmarshalInto("sessions_history params", input, &p); err != nil {
			return "", err
		}
		if p.SessionKey == "" {
			return "", fmt.Errorf("sessionKey is required")
		}
		if transcript == nil {
			return "Transcript store not available. Cannot fetch session history.", nil
		}

		limit := p.Limit.Int()
		if limit <= 0 {
			limit = 20
		}

		sessionKey, refAnchor, cands := resolveHistoryKey(transcript, p.SessionKey)
		if len(cands) > 1 {
			if len(cands) > 8 {
				cands = cands[:8]
			}
			return fmt.Sprintf("Ref %q matches %d sessions — specify one sessionKey:\n%s",
				p.SessionKey, len(cands), strings.Join(cands, "\n")), nil
		}
		anchor := p.Around.Int()
		if anchor <= 0 && refAnchor >= 0 {
			anchor = refAnchor
		}

		if anchor > 0 {
			// Windowed read around one message — the follow-up route for a
			// recall snippet that lost its answer to the budget cut. The shape
			// is deep-read, not preview: FEWER messages (default 10, not 20)
			// with a DEEP per-message cap (4000B) under a 24KB total guard.
			// Measured on the tool-loop reader: the original 20x1200B window
			// re-cut exactly the long answers the open exists for — a 100-item
			// list's item 27 sat beyond 1200B and the model reported
			// "excerpts only show items 1-21"; at 4000B it answered.
			if p.Limit.Int() <= 0 {
				limit = 10
			}
			all, total, err := transcript.Load(sessionKey, 0)
			if err != nil {
				return fmt.Sprintf("Failed to load history for session %q: %s", sessionKey, err.Error()), nil
			}
			if len(all) == 0 {
				return fmt.Sprintf("Session %q has no history (or does not exist).", sessionKey), nil
			}
			center := anchor
			if center > len(all) {
				center = len(all)
			}
			start := center - 1 - limit/2
			if start < 0 {
				start = 0
			}
			end := start + limit
			if end > len(all) {
				end = len(all)
				if start = end - limit; start < 0 {
					start = 0
				}
			}
			var sb strings.Builder
			// The conversation's date belongs in the header: an opened window
			// otherwise LOSES the temporal context the recall row carried —
			// the reader confirms WHAT happened and erases WHEN (measured on
			// temporal-reasoning: opens fired on 26/64 questions yet the
			// category lagged its ceiling until the date was restored here).
			header := dateNote(firstTimestamp(all[start:end]))
			fmt.Fprintf(&sb, "Session %q%s — messages %d..%d of %d (window around #%d):\n\n",
				sessionKey, header, start+1, end, total, anchor)
			budget := 24000
			for i, msg := range all[start:end] {
				content := msg.SearchableText()
				if strings.TrimSpace(content) == "" {
					continue
				}
				if len(content) > 4000 {
					content = textutil.TruncateBytes(content, 4000) + "..."
				}
				line := fmt.Sprintf("%d. [%s] %s\n", start+i+1, msg.Role, content)
				if budget -= len(line); budget < 0 {
					sb.WriteString("… (window truncated at 24KB)\n")
					break
				}
				sb.WriteString(line)
			}
			return sb.String(), nil
		}

		msgs, total, err := transcript.Load(sessionKey, limit)
		if err != nil {
			return fmt.Sprintf("Failed to load history for session %q: %s", sessionKey, err.Error()), nil
		}
		if len(msgs) == 0 {
			return fmt.Sprintf("Session %q has no history (or does not exist).", sessionKey), nil
		}
		p.SessionKey = sessionKey

		var sb strings.Builder
		fmt.Fprintf(&sb, "Session %q history (%d of %d messages):\n\n", p.SessionKey, len(msgs), total)
		for i, msg := range msgs {
			// History replays what HAPPENED in a session, so a tool call is
			// content — rendering it through TextContent ("what was said") left
			// a tool-only turn as an empty "[assistant] " row.
			content := msg.SearchableText()
			if strings.TrimSpace(content) == "" {
				continue
			}
			if len(content) > 500 {
				// Rune-safe cut so a multi-byte char (Korean) never splits into a
				// U+FFFD replacement char in the history preview.
				content = textutil.TruncateBytes(content, 500) + "..."
			}
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, msg.Role, content)
		}
		return sb.String(), nil
	}
}

// --- sessions search sub-action ---

// toolSessionsSearch returns a tool function that searches across session transcripts.
func toolSessionsSearch(transcript toolport.TranscriptStore) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query      string           `json:"query"`
			MaxResults artifact.FlexInt `json:"maxResults"`
		}
		if err := jsonutil.UnmarshalInto("sessions_search params", input, &p); err != nil {
			return "", err
		}
		if p.Query == "" {
			return "", fmt.Errorf("query is required")
		}
		if transcript == nil {
			return "Transcript store not available.", nil
		}
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return "", fmt.Errorf("query is required")
		}

		maxResults := p.MaxResults.Int()
		if maxResults <= 0 {
			maxResults = 20
		}
		if maxResults > 100 {
			maxResults = 100
		}

		results, err := transcript.Search(query, maxResults)
		if err != nil {
			return fmt.Sprintf("Search failed: %s", err.Error()), nil
		}
		expandedQueries := sessionSearchExpandedQueries(query)
		expanded := false
		if len(results) == 0 && len(expandedQueries) > 0 {
			results, err = searchTranscriptExpanded(transcript, expandedQueries, maxResults)
			if err != nil {
				return fmt.Sprintf("Search failed: %s", err.Error()), nil
			}
			expanded = len(results) > 0
		}
		semantic := semanticSessionMatches(ctx, transcript, query, maxResults, results)
		if len(results) == 0 && len(semantic) == 0 {
			return fmt.Sprintf("No matches found for %q across session transcripts.", query), nil
		}

		var sb strings.Builder
		totalMatches := 0
		for _, r := range results {
			totalMatches += len(r.Matches)
		}
		if expanded {
			fmt.Fprintf(&sb, "Found %d match(es) across %d session(s) for %q via expanded terms (%s):\n\n",
				totalMatches, len(results), query, strings.Join(expandedQueries, ", "))
		} else {
			fmt.Fprintf(&sb, "Found %d match(es) across %d session(s) for %q:\n\n", totalMatches, len(results), query)
		}

		for _, r := range results {
			// The conversation's date on the header: enumeration-style questions
			// ("how many X since May") need to FILTER the listed conversations
			// by time, and match snippets rarely carry it.
			fmt.Fprintf(&sb, "### Session: %s%s\n", r.SessionKey, sessionMatchDate(r))
			for _, m := range r.Matches {
				// Context layout: [before, after] when both exist,
				// [after] when index==0, [before] when last message.
				hasBefore := m.Index > 0 && len(m.Context) > 0
				hasAfter := len(m.Context) > 1 || (len(m.Context) == 1 && !hasBefore)

				// The MATCH is found with SearchableText (transcript/store.go),
				// so it must be RENDERED with it too — otherwise a hit on a tool
				// name prints an empty line and the search claims to have found
				// something it cannot show.
				if hasBefore {
					c := m.Context[0]
					content := Truncate(c.SearchableText(), 200)
					fmt.Fprintf(&sb, "  [ctx] [%s] %s\n", c.Role, content)
				}

				fmt.Fprintf(&sb, "  **[%s]** %s\n", m.Message.Role, Truncate(m.Message.SearchableText(), 500))

				if hasAfter {
					c := m.Context[len(m.Context)-1]
					content := Truncate(c.SearchableText(), 200)
					fmt.Fprintf(&sb, "  [ctx] [%s] %s\n", c.Role, content)
				}
				sb.WriteString("\n")
			}
		}
		if len(semantic) > 0 {
			sb.WriteString("### 의미 일치 대화 (요약 기반 — 키워드가 달라도 같은 주제):\n")
			for _, h := range semantic {
				date := ""
				if h.At > 0 {
					date = time.UnixMilli(h.At).Format("2006-01-02") + " · "
				}
				fmt.Fprintf(&sb, "- %s (%s점수 %.2f): %s\n", h.SessionKey, date, h.Score, h.Snippet)
			}
		}
		return sb.String(), nil
	}
}

// sessionMatchDate renders the conversation date for a search-result header,
// taken from the first match's message timestamp ("" when unknown).
func sessionMatchDate(r toolport.SearchResult) string {
	for _, m := range r.Matches {
		if note := dateNote(m.Message.Timestamp); note != "" {
			return note
		}
	}
	return ""
}

// semanticSessionMatches asks the transcript store's OPTIONAL meaning-search
// capability for summary-level matches, excluding the current session and any
// session the keyword results already cover. Degrades to nil when the store
// does not offer the capability or the semantic index is disabled.
func semanticSessionMatches(ctx context.Context, transcript toolport.TranscriptStore, query string, maxResults int, keyword []toolport.SearchResult) []toolport.SemanticSessionHit {
	searcher, ok := transcript.(toolport.SemanticSessionSearcher)
	if !ok {
		return nil
	}
	// Scaled to the keyword arm's budget, not a fixed 5. The semantic arm exists
	// to surface the instances keyword search misses, so capping it an order of
	// magnitude tighter hides exactly what it was added to find — the same way a
	// low search-result cap hides instances from a counting question.
	limit := maxResults / 2
	if limit < 5 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	hits := searcher.SearchSessionsSemantic(ctx, toolport.SessionKeyFromContext(ctx), query, limit)
	if len(hits) == 0 {
		return nil
	}
	covered := make(map[string]struct{}, len(keyword))
	for _, r := range keyword {
		covered[r.SessionKey] = struct{}{}
	}
	// Fresh slice, not hits[:0]: an implementation may hand back a cached
	// slice and filtering in place would corrupt it for the next caller.
	out := make([]toolport.SemanticSessionHit, 0, len(hits))
	for _, h := range hits {
		if _, dup := covered[h.SessionKey]; dup {
			continue
		}
		out = append(out, h)
	}
	return out
}

var sessionSearchStopWords = map[string]struct{}{
	"그": {}, "이": {}, "저": {}, "것": {}, "거": {}, "좀": {}, "다시": {}, "관련": {}, "작업": {},
	"같은": {}, "처럼": {}, "지난번": {}, "전에": {}, "예전에": {}, "아까": {}, "방금": {}, "해줘": {},
	"해주세요": {}, "찾아줘": {}, "찾아": {}, "어떻게": {}, "뭐였": {}, "뭐더라": {},
	"the": {}, "and": {}, "for": {}, "with": {}, "about": {}, "that": {}, "this": {}, "what": {},
	"when": {}, "how": {}, "same": {}, "previous": {}, "last": {}, "again": {}, "task": {},
}

var sessionSearchShortSignalTokens = map[string]struct{}{
	"ai": {}, "ci": {}, "pr": {}, "ui": {}, "ux": {},
}

func sessionSearchExpandedQueries(query string) []string {
	tokens := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	})
	var out []string
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		token = normalizeSessionSearchToken(token)
		if token == "" || !isSessionSearchSignalToken(token) {
			continue
		}
		if _, stop := sessionSearchStopWords[token]; stop {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func normalizeSessionSearchToken(token string) string {
	token = strings.Trim(strings.ToLower(token), "-_")
	if token == "" {
		return ""
	}
	suffixes := []string{
		"해주세요", "해줘요", "해줘", "했어요", "했어", "했던", "하던",
		"하는", "하면", "해서", "해야", "해요", "하고", "해",
		"에서", "에게", "으로", "부터", "까지", "이랑",
		"은", "는", "이", "가", "을", "를", "에", "로", "와", "과", "도", "만", "랑",
	}
	for range 2 {
		changed := false
		for _, suffix := range suffixes {
			if !strings.HasSuffix(token, suffix) {
				continue
			}
			candidate := strings.TrimSuffix(token, suffix)
			if len([]rune(candidate)) < 2 {
				continue
			}
			token = candidate
			changed = true
			break
		}
		if !changed {
			break
		}
	}
	return token
}

func isSessionSearchSignalToken(token string) bool {
	runes := []rune(token)
	if len(runes) == 0 {
		return false
	}
	if _, ok := sessionSearchShortSignalTokens[token]; ok {
		return true
	}
	hasHangul := false
	hasLatin := false
	for _, r := range runes {
		if r >= 0xAC00 && r <= 0xD7A3 {
			hasHangul = true
		}
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			hasLatin = true
		}
	}
	if hasHangul {
		return len(runes) >= 2
	}
	if hasLatin {
		return len(runes) >= 3
	}
	return len(runes) >= 2
}

func searchTranscriptExpanded(transcript toolport.TranscriptStore, queries []string, maxResults int) ([]toolport.SearchResult, error) {
	if transcript == nil || len(queries) == 0 || maxResults <= 0 {
		return nil, nil
	}
	var results []toolport.SearchResult
	sessionIndex := make(map[string]int)
	seenMatches := make(map[string]struct{})
	remaining := maxResults

	for _, query := range queries {
		if remaining <= 0 {
			break
		}
		hits, err := transcript.Search(query, remaining)
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			if remaining <= 0 {
				break
			}
			idx, ok := sessionIndex[hit.SessionKey]
			if !ok {
				idx = len(results)
				sessionIndex[hit.SessionKey] = idx
				results = append(results, toolport.SearchResult{SessionKey: hit.SessionKey})
			}
			for _, match := range hit.Matches {
				key := fmt.Sprintf("%s#%d", hit.SessionKey, match.Index)
				if _, exists := seenMatches[key]; exists {
					continue
				}
				seenMatches[key] = struct{}{}
				results[idx].Matches = append(results[idx].Matches, match)
				remaining--
				if remaining <= 0 {
					break
				}
			}
		}
	}
	return results, nil
}

// --- sessions send sub-action ---

// toolSessionsSend returns a tool function that sends a message to another session.
func toolSessionsSend(d *tooldeps.SessionDeps) toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}
		if err := jsonutil.UnmarshalInto("sessions_send params", input, &p); err != nil {
			return "", err
		}
		if p.Message == "" {
			return "", fmt.Errorf("message is required")
		}

		targetKey := p.SessionKey
		if targetKey == "" {
			targetKey = "main"
		}

		if d == nil || d.SendFn == nil {
			return "Cross-session messaging is not available (session send function not wired).", nil
		}

		if err := d.SendFn(targetKey, p.Message); err != nil {
			return fmt.Sprintf("Failed to send message to session %q: %s", targetKey, err.Error()), nil
		}
		return fmt.Sprintf("Message sent to session %q.", targetKey), nil
	}
}

// --- sessions_spawn tool ---

// Spawn guardrails. A misbehaving model (or an injected prompt) must not be
// able to fork unbounded agent trees on a single-machine vLLM deployment.
const (
	// maxSubagentSpawnDepth caps nesting: a top-level session's children have
	// depth 1; depth-3 children cannot spawn further.
	maxSubagentSpawnDepth = 3
	// maxConcurrentSubagents caps non-terminal children per parent session.
	maxConcurrentSubagents = 5
	// spawnHandoffMaxRunes bounds the rendered L₂ handoff appended to a
	// child's task message.
	spawnHandoffMaxRunes = 4000
)

// ToolSessionsSpawn returns a tool function that spawns a sub-agent session.
func ToolSessionsSpawn(d *tooldeps.SessionDeps) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task       string `json:"task"`
			Label      string `json:"label"`
			Model      string `json:"model"`       // role name: "main","coding","lightweight","fallback"
			ToolPreset string `json:"tool_preset"` // tool preset: "researcher","implementer","verifier"
		}
		if err := jsonutil.UnmarshalInto("sessions_spawn params", input, &p); err != nil {
			return "", err
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}
		p.Model = strings.TrimSpace(p.Model)

		// Fail closed on presets outside the spawn contract. AllowedTools
		// returns nil (= no restriction) for unrecognized names, so a typo'd
		// preset would silently spawn a FULLY unrestricted child; and presets
		// that are valid elsewhere but carry side contracts this tool cannot
		// honor — "coding" grants the write/exec allow-list but its worktree
		// binding and prompt profile come only from code: sessions — must not
		// slip through a generic validity check either.
		p.ToolPreset = strings.TrimSpace(p.ToolPreset)
		if p.ToolPreset != "" {
			allowed := false
			valid := make([]string, 0, 3)
			for _, sp := range toolpreset.SpawnPresets() {
				valid = append(valid, string(sp))
				if toolpreset.Preset(p.ToolPreset) == sp {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Sprintf("Spawn rejected: unknown tool_preset %q. Valid presets: %s. Omit tool_preset for the full toolset.",
					p.ToolPreset, strings.Join(valid, ", ")), nil
			}
		}

		if d == nil || d.Manager == nil || d.SendFn == nil {
			return "Sub-agent spawning is not available (session dependencies not wired).", nil
		}
		codingDefaultModel := sessionCodingDefaultModel(d)
		if p.Model == "coding" && codingDefaultModel == "" {
			return "Spawn rejected: model \"coding\" is not configured. Choose another model or configure the coding role in Settings first.", nil
		}

		// Create a unique session key for the sub-agent.
		parentKey := toolport.SessionKeyFromContext(ctx)
		label := p.Label
		if label == "" {
			label = "subagent"
		}

		// Depth guard: child depth = parent depth + 1, capped so spawn chains
		// (A → B → C → …) cannot recurse unbounded.
		depth := 1
		if parent := d.Manager.Get(parentKey); parent != nil && parent.SpawnDepth != nil {
			depth = *parent.SpawnDepth + 1
		}
		if depth > maxSubagentSpawnDepth {
			return fmt.Sprintf("Spawn rejected: max sub-agent nesting depth (%d) reached. Do the task yourself instead of delegating further.", maxSubagentSpawnDepth), nil
		}

		// Concurrency guard: cap non-terminal children per parent so one turn
		// cannot flood the local inference queue.
		active := 0
		for _, s := range d.Manager.List() {
			if s.SpawnedBy == parentKey && !session.IsTerminal(s.Status) {
				active++
			}
		}
		if active >= maxConcurrentSubagents {
			return fmt.Sprintf("Spawn rejected: %d sub-agents are already active (max %d). Wait for some to finish (use the subagents tool to check), or kill ones you no longer need.", active, maxConcurrentSubagents), nil
		}

		childKey := session.SpawnedChildKey(label, time.Now().UnixMilli())

		// Create the child session.
		childSession := d.Manager.Create(childKey, session.KindDirect)
		if p.Model != "" {
			childSession.Model = p.Model
		} else if p.ToolPreset == string(toolpreset.PresetImplementer) && codingDefaultModel != "" {
			// Implementer children can write/edit/exec. When the operator has
			// configured a dedicated coding model, keep the session on the role
			// name rather than the resolved model ID so live role changes and
			// coding-specific fallback behavior still apply at run time.
			childSession.Model = "coding"
		} else if d.SubagentDefaultModel != "" {
			childSession.Model = d.SubagentDefaultModel
		}
		childSession.SpawnedBy = parentKey
		childSession.SpawnDepth = &depth
		childSession.Label = label
		childSession.ToolPreset = p.ToolPreset
		if err := d.Manager.Set(childSession); err != nil {
			return fmt.Sprintf("Sub-agent session %q created but failed to store its settings: %s", childKey, err.Error()), nil
		}

		// L₂ state handoff (bounded memory contract): a spawned child is a
		// fresh-prompt executor — without this it starts with none of the
		// parent's typed task state and re-derives it from prose. Bounded,
		// deterministic, and read-only for the child (its own board starts
		// empty); DENEB_SPAWN_L2=0 is the per-layer kill switch the contract
		// doc requires so the layer's benefit stays attributable.
		task := p.Task
		if os.Getenv("DENEB_SPAWN_L2") != "0" {
			if board := toolport.BlackboardFromContext(ctx); board != nil {
				if handoff := board.RenderHandoff(spawnHandoffMaxRunes); handoff != "" {
					task += "\n\n## 인계된 작업 상태 (부모 blackboard, 읽기 전용 참고)\n" + handoff
				}
			}
		}

		// Send the task message to the child session.
		if err := d.SendFn(childKey, task); err != nil {
			return fmt.Sprintf("Sub-agent session %q created but failed to send task: %s", childKey, err.Error()), nil
		}

		// Signal the executor that a sub-agent was spawned in this run.
		if flag := toolport.SpawnFlagFromContext(ctx); flag != nil {
			flag.Set()
		}

		result := fmt.Sprintf("Sub-agent spawned.\nSession: %s\nTask: %s", childKey, p.Task)
		if p.ToolPreset != "" {
			result += fmt.Sprintf("\nTool preset: %s", p.ToolPreset)
		}
		if childSession.Model != "" {
			result += fmt.Sprintf("\nModel: %s", childSession.Model)
		}
		return result, nil
	}
}

func sessionCodingDefaultModel(d *tooldeps.SessionDeps) string {
	if d == nil {
		return ""
	}
	if d.CodingDefaultModelFn != nil {
		return strings.TrimSpace(d.CodingDefaultModelFn())
	}
	return strings.TrimSpace(d.CodingDefaultModel)
}
