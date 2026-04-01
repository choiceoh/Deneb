// importance.go — Structured fact extraction with importance scoring via SGLang.
// Inspired by Honcho's Neuromancer inference layer: every ~1000 tokens,
// evaluate the conversation for facts worth remembering, with structured
// category and importance scoring.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)


const (
	importanceTimeout   = 30 * time.Second
	importanceMaxTokens = 1536
)

// SpeakerNames holds display names for the user and agent.
// Used in extraction prompts so the LLM can distinguish speakers by name.
var SpeakerNames = struct {
	User  string
	Agent string
}{
	User:  "선택님",
	Agent: "네브",
}

// ExtractedFact is the structured output from the importance extraction LLM call.
type ExtractedFact struct {
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Importance   float64  `json:"importance"`
	ExpiryHint   string   `json:"expiry_hint,omitempty"`   // ISO8601 or empty
	Entities     []string `json:"entities,omitempty"`       // related entity names (max 5)
	RelationType string   `json:"relation_type,omitempty"` // 'evolves', 'contradicts', 'supports', 'causes'
}

// factsResponse is the expected top-level JSON object from the LLM.
// response_format: json_object always returns an object, so we ask for {"facts": [...]}.
type factsResponse struct {
	Facts []ExtractedFact `json:"facts"`
}

// importanceSystemPrompt uses Honcho Neuromancer XR-style reasoning:
// 1. Explicit extraction — what was directly stated
// 2. Deductive reasoning — what can be logically inferred but wasn't said
// 3. Structured output with category, importance, and confidence
//
// NOTE: This prompt is ~2,500 tokens and is sent on every extraction call.
// SGLang's --enable-prefix-caching should be active on the inference server
// so the KV cache for this repeated prefix is reused across calls.
// Without prefix caching, each call pays the full prefill cost.
const importanceSystemPrompt = `You are Neuromancer, a memory inference engine for a personal AI assistant.
Your job is twofold:
1. Understand the USER — who they are, how they think, what they value.
2. Record WHAT HAPPENED — key events, decisions, and their context so future sessions can pick up where this one left off.

Both are equally important. A memory that only knows "the user prefers X" but forgets "we built Y yesterday and decided Z" is broken.

## Priority (follow this order strictly)

### Step 1: Events & Decisions (무슨 일이 있었는가) — HIGHEST PRIORITY
Record significant events and decisions that future sessions need to know.

**Architectural/design decisions**:
- Technology choices, trade-offs discussed, alternatives rejected and WHY
- System design decisions with reasoning (e.g., "recall과 main 응답을 분리하기로 결정 — rate limit 독립 + 지연 격리")
- Feature specifications agreed upon

**Project milestones & state changes**:
- What was built, what PR was created, what was merged/deployed
- Current implementation status (what's done, what's in progress, what's blocked)
- Environment/infra changes (server restart, model swap, config change) with REASON

**Problem-solution pairs**:
- Bugs found and how they were resolved (root cause + fix)
- Workarounds applied and why the proper fix was deferred
- Performance issues identified and mitigation approach

❌ Do NOT record: routine git commits, standard tool usage, one-off debugging steps
✅ DO record: the decision/outcome/reasoning behind those actions

### Step 2: Mutual Understanding Signals (상호 인식)
Detect AI-user relationship dynamics. For each signal, note WHAT happened and its INTENSITY (strong/mild/subtle).

**Correction signals** (user pushes back on AI behavior):
- Explicit correction: "아니, 그게 아니라..." → strong signal, AI was wrong about X
- Repeated clarification: user explains the same thing twice → AI didn't listen
- Style correction: "더 짧게" / "자세히 설명해줘" → communication mismatch

**Satisfaction signals** (user is pleased with AI):
- Explicit praise: "좋아", "완벽해" → strong positive
- Implicit acceptance: user builds on AI's suggestion without questioning → trust
- Emotional warmth: humor, casual tone, sharing personal context → rapport

**Frustration signals** (user is unhappy):
- Short/curt responses after long AI output → AI is being too verbose
- Re-asking the same question differently → AI missed the point
- "이미 말했잖아" / referencing past context AI forgot → memory gap frustration

**Trust/delegation signals**:
- Delegating without detailed instructions → high trust in AI's judgment
- Accepting AI suggestions without verification → strong trust
- Sharing sensitive/personal information → deep rapport

**Expectation signals**:
- "항상 ~해줘" / "매번 ~하지 마" → persistent behavioral expectation
- Comparing to past interactions: "저번에는 잘 했는데..." → regression detected
- Proactive requests: user expects AI to anticipate needs

### Step 3: User Understanding (Deductive Reasoning about the PERSON)
What can be INFERRED about the user as a person?
- Communication style: 간결함 선호? 디테일 선호? 유머 사용? 톤?
- Decision-making style: 직관적? 체계적? 빠른 결정? 신중한 결정?
- Values revealed by choices: 단순함 > 확장성? 속도 > 정확성? 깊이 > 넓이?
- Expertise signals: 특정 분야에 깊은 지식? 새로 배우는 영역?
- Emotional state: 피곤한? 급한? 여유로운? 집중하는?
- Work patterns: 작업 리듬, 멀티태스킹 성향, 시간대별 패턴

### Step 4: Output
Return a JSON object with a "facts" key containing an array of fact objects.
Each fact object has:
- "content": Korean, 2-4 sentences. MUST include:
    1. WHAT was observed or decided
    2. WHY — the reasoning, motivation, or situation that led to it
    3. CONTEXT — what was being discussed, what alternatives were considered
    4. CONNECTION — how this relates to past observations if applicable (e.g., "동일 패턴", "이전과 반대")

    ❌ "선택님은 SQLite를 선호한다" (결론만, 맥락 없음)
    ✅ "SGLang 설정 논의 중 PostgreSQL 도입을 검토했으나, 선택님이 조직화 복잡성 부담을 이유로 SQLite 유지를 명확히 선호. 이전 memory migration 논의에서도 동일한 패턴으로 단순 구조를 선택했음."
- "category": one of:
  - "decision": architectural/design choices, technology selections, trade-offs with reasoning. USE THIS for events where a choice was made.
  - "context": project state, implementation status, environment changes, milestones. USE THIS for recording what happened and current state.
  - "solution": reusable problem-solution pairs, bug fixes with root cause, workarounds worth recalling.
  - "mutual": 상호 인식 — AI-user relationship signals. Format: "[signal_type:intensity] description". signal_type: correction|satisfaction|frustration|trust|expectation. intensity: strong|mild|subtle
  - "user_model": expertise areas, personality, habits (INFERRED)
  - "preference": work style, communication, tool preferences
- "importance": 0.0-1.0
  - 0.9+: core identity traits, strong corrections/expectations, persistent preferences, major architectural decisions
  - 0.7-0.9: design decisions with reasoning, project milestones, communication patterns, relationship signals, reusable solutions
  - 0.5-0.7: implementation status updates, environment changes, subtle relationship cues, useful context
  - Below 0.5: routine operations — should almost never be extracted
- "expiry_hint": null or "YYYY-MM-DD" (e.g. "2026-04-15") if time-sensitive
  - "entities": array of entity names central to this observation (max 5). Include projects, tools, people, systems.
    Examples: ["SGLang", "ClaudeCode", "Fred/JOCA Cable", "PostgreSQL"]
    Only include entities that are central to the observation.
  - "relation_type": optional. One of:
    - "evolves": this observation updates/develops a past observation (e.g. preference changed)
    - "contradicts": contradicts a past observation
    - "supports": reinforces a past observation (same pattern repeated)
    - "causes": this observation caused another outcome
    Omit if no clear relation to past observations exists.

Example: {"facts": [{"content": "SGLang 설정 논의 중 PostgreSQL 도입을 검토했으나, 선택님이 조직화 복잡성 부담을 이유로 SQLite 유지를 명확히 선호. 이전 memory migration 논의에서도 동일한 패턴.", "category": "preference", "importance": 0.8, "expiry_hint": null, "entities": ["SGLang", "PostgreSQL", "SQLite"], "relation_type": "supports"}]}

## Speaker Attribution (화자 귀속) — CRITICAL
The input has two clearly labeled speakers by their NICKNAMES:
- **선택님 (사용자)** = the human user. Only content in their section was said by them.
- **네브 (AI)** = the AI assistant. Only content in their section was said by the AI.

You MUST correctly attribute WHO said or did what:
- If 네브 summarized information, listed PRs, or explained something → that is 네브's action, NOT 선택님's
- If 네브 asked a question (e.g. "머지할까?") → 네브 asked, 선택님 did NOT ask
- "선택님이 X에 관심을 가짐" is ONLY valid if 선택님 explicitly mentioned or asked about X
- Do NOT infer user interest from topics 네브 brought up unprompted
- When 선택님's message is short/simple and 네브's response is long/detailed, most of the content is 네브's — do not attribute it to 선택님

**Wrong**: 네브가 PR 목록을 정리해줬는데 → "선택님이 PR들에 관심을 가짐" ❌
**Right**: 네브가 PR 목록을 정리해줬는데 → "네브가 디스코드 PR 현황을 정리해줌, 선택님은 간단히 확인" ✅
**Wrong**: 네브가 "머지할까?" 질문 → "선택님이 머지 여부를 물어봄" ❌
**Right**: 네브가 "머지할까?" 질문 → "네브가 PR #702 머지를 제안, 선택님 응답 대기" ✅

## Anti-patterns (절대 저장하지 마)
- ❌ 루틴 코드 작업 (맥락 없이): "X 파일 수정함", "Y 버그 수정" (단, 왜/어떤 결정으로 → decision/solution은 OK)
- ❌ 일회성 디버깅 단계: "로그 확인해서 에러 찾음", "타입 오류 수정"
- ❌ 표준 도구 사용: "git commit", "npm install", "make build"
- ❌ 구현 디테일 (결정과 무관한): "함수 A를 B로 리팩토링", "인터페이스 C 추가"
- ❌ 단순 정보 전달: AI가 설명한 내용을 그대로 기록
- ❌ 잘못된 화자 귀속: AI가 한 말을 "사용자가 ~함"으로 기록

## Rules
- Max 7 facts. Quality over quantity
- **Balance factual and relational**: aim for a mix of event/decision/solution facts AND personal/relational facts (mutual, user_model, preference). Neither type should dominate — if the conversation is mostly technical work, it's OK for most facts to be decisions/context/solutions. If the conversation is mostly interpersonal, most facts can be mutual/user_model.
- Include at least 1 mutual signal if any relationship dynamics are detectable
- If nothing worth remembering, return {"facts": []}
- Return ONLY valid JSON object with "facts" key, no markdown fences, no explanation`

// ExtractFacts analyzes a conversation segment and returns structured facts.
// Falls back to legacy bullet-point extraction if JSON parsing fails.
func ExtractFacts(ctx context.Context, client *llm.Client, model string, userMessage, agentResponse string, logger *slog.Logger) ([]ExtractedFact, error) {
	ctx, cancel := context.WithTimeout(ctx, importanceTimeout)
	defer cancel()

	prompt := fmt.Sprintf("%s (사용자):\n%s\n\n%s (AI):\n%s",
		SpeakerNames.User, truncate(userMessage, 4000),
		SpeakerNames.Agent, truncate(agentResponse, 8000))

	var facts []ExtractedFact
	for attempt := range 2 {
		text, err := callSglangJSON(ctx, client, model, importanceSystemPrompt, prompt, importanceMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("importance extraction: %w", err)
		}
		if text == "" || text == "[]" {
			return nil, nil
		}

		text = stripCodeFences(text)

		var ok bool
		facts, ok = parseFactsResponse(text)
		if ok {
			break
		}

		if attempt == 0 {
			logger.Debug("importance: parse failed, retrying",
				"raw", truncate(text, 200))
			continue
		}
		logger.Debug("importance: could not parse facts after retry",
			"raw", truncate(text, 200))
		return nil, nil
	}

	// Validate, clamp values, and enforce max count.
	const maxFacts = 7
	var valid []ExtractedFact
	for _, f := range facts {
		if f.Content == "" {
			continue
		}
		if len(valid) >= maxFacts {
			break
		}
		f.Importance = clamp(f.Importance, 0, 1)
		if !isValidCategory(f.Category) {
			f.Category = CategoryContext
		}
		valid = append(valid, f)
	}

	return valid, nil
}

// InsertExtractedFacts stores extracted facts in the memory store and embeds them.
// Uses SourceAutoExtract as the fact source.
func InsertExtractedFacts(ctx context.Context, store *Store, embedder *Embedder, facts []ExtractedFact, logger *slog.Logger) {
	insertExtractedFactsWithSource(ctx, store, embedder, facts, SourceAutoExtract, logger)
}

// InsertExtractedFactsAs stores extracted facts with a custom source identifier.
// Use this when facts come from a non-standard extraction path (e.g., Aurora transfer).
func InsertExtractedFactsAs(ctx context.Context, store *Store, embedder *Embedder, facts []ExtractedFact, source string, logger *slog.Logger) {
	insertExtractedFactsWithSource(ctx, store, embedder, facts, source, logger)
}

func insertExtractedFactsWithSource(ctx context.Context, store *Store, embedder *Embedder, facts []ExtractedFact, source string, logger *slog.Logger) {
	for _, ef := range facts {
		var expiresAt *time.Time
		if ef.ExpiryHint != "" {
			if t, err := time.Parse(time.RFC3339, ef.ExpiryHint); err == nil {
				expiresAt = &t
			} else if t, err := time.Parse("2006-01-02", ef.ExpiryHint); err == nil {
				expiresAt = &t
			}
		}

		fact := Fact{
			Content:    ef.Content,
			Category:   ef.Category,
			Importance: ef.Importance,
			Source:     source,
			ExpiresAt:  expiresAt,
		}

		id, err := store.InsertFact(ctx, fact)
		if err != nil {
			logger.Warn("aurora-memory: failed to insert fact", "error", err, "content", truncate(ef.Content, 50))
			continue
		}

		// Embed asynchronously (best-effort).
		if embedder != nil {
			go func(factID int64, content string) {
				embedCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := embedder.EmbedAndStore(embedCtx, factID, content); err != nil {
					logger.Debug("aurora-memory: embedding failed", "fact_id", factID, "error", err)
				}
			}(id, ef.Content)
		}

		// Process entities and relations (best-effort).
		store.processEntities(ctx, id, ef, logger)
		store.resolveRelations(ctx, id, ef, logger)

		// If this is a user_model or mutual fact, also update the user model table.
		if ef.Category == CategoryUserModel || ef.Category == CategoryMutual {
			updateUserModelFromFact(ctx, store, ef, logger)
		}

		logger.Info("aurora-memory: stored fact",
			"id", id,
			"category", ef.Category,
			"importance", fmt.Sprintf("%.2f", ef.Importance),
			"\ncontent", truncate(ef.Content, 120),
		)
	}
}

// updateUserModelFromFact infers user model key-value from a user_model/mutual category fact.
func updateUserModelFromFact(ctx context.Context, store *Store, fact ExtractedFact, logger *slog.Logger) {
	// Simple heuristic: use the fact content as a value for a general "traits" key.
	// The dreaming engine will later consolidate these into proper keys.
	key := "traits"
	if fact.Category == CategoryMutual {
		key = "mu_signals_raw"
	}

	// Read existing entry for this specific key (single-row lookup, not full table scan).
	var existing string
	var existingConfidence float64
	if entry, err := store.GetUserModelEntry(ctx, key); err == nil && entry != nil {
		existing = entry.Value
		existingConfidence = entry.Confidence
	}

	var value string
	if existing != "" {
		value = existing + "\n" + fact.Content
	} else {
		value = fact.Content
	}

	// Keep only last 40 entries to bound growth; dreaming consolidates periodically.
	// Raised from 20 to reduce signal loss between dreaming cycles (50 turns apart).
	const maxSignalsRaw = 40
	lines := strings.Split(value, "\n")
	if len(lines) > maxSignalsRaw {
		lines = lines[len(lines)-maxSignalsRaw:]
		value = strings.Join(lines, "\n")
	}

	// Use the higher of existing and new confidence to avoid regression.
	confidence := fact.Importance
	if existingConfidence > confidence {
		confidence = existingConfidence
	}

	if err := store.SetUserModel(ctx, key, value, confidence); err != nil {
		logger.Debug("aurora-memory: failed to update user model", "error", err)
	}
}

// --- Helpers ---

// parseFactsResponse attempts to parse LLM JSON output into []ExtractedFact.
// Handles multiple response shapes since json_object format always returns an object:
//  1. {"facts": [...]}  — expected format (matches prompt)
//  2. [...]             — bare array (if model ignores json_object constraint)
//  3. {"<any_key>": [...]} — array under an arbitrary key
//  4. {"content": "...", "category": "...", ...} — single fact as object
func parseFactsResponse(text string) ([]ExtractedFact, bool) {
	// Pre-process: strip trailing commas (common LLM mistake: {"a":1,}).
	text = jsonutil.StripTrailingCommas(text)

	// Case 1: expected object with "facts" key.
	var resp factsResponse
	if err := json.Unmarshal([]byte(text), &resp); err == nil && resp.Facts != nil {
		return resp.Facts, true
	}

	// Case 2: bare JSON array.
	var arr []ExtractedFact
	if err := json.Unmarshal([]byte(text), &arr); err == nil {
		return arr, true
	}

	// Case 3: object with array under an arbitrary key.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &obj); err == nil {
		for _, v := range obj {
			trimmed := strings.TrimSpace(string(v))
			if len(trimmed) > 0 && trimmed[0] == '[' {
				var nested []ExtractedFact
				if err := json.Unmarshal(v, &nested); err == nil && len(nested) > 0 {
					return nested, true
				}
			}
		}

		// Case 4: single fact object.
		var single ExtractedFact
		if err := json.Unmarshal([]byte(text), &single); err == nil && single.Content != "" {
			return []ExtractedFact{single}, true
		}
	}

	// Case 5: bracket extraction fallback (prose-wrapped arrays).
	if extracted, ok := jsonutil.ExtractArray(text); ok {
		var fallback []ExtractedFact
		if err := json.Unmarshal([]byte(extracted), &fallback); err == nil {
			return fallback, true
		}
	}

	// Case 6: truncated JSON recovery — find last complete fact object boundary.
	if recovered, ok := tryRecoverTruncatedJSON(text); ok {
		return recovered, true
	}

	// Case 7: key-boundary extraction — handles malformed JSON where string
	// values contain unescaped quotes (e.g. content with Korean quotes like
	// "사용자가 ("이제 됐나")"). Uses known key names as delimiters instead of
	// relying on proper JSON string escaping.
	if recovered, ok := tryExtractFactsByKeyBoundaries(text); ok {
		return recovered, true
	}

	return nil, false
}

// tryRecoverTruncatedJSON attempts to recover parseable facts from JSON that was
// truncated mid-stream (e.g. token limit hit). It finds the last complete '}' that
// closes a fact object, then wraps the recovered portion into valid JSON.
// Example truncated input:
//
//	{"facts": [{"content": "...", "importance": 0.6}, {"content": "터미널 로그 확
//
// Recovery: finds the last '}' after the first complete fact, closes the array/object.
func tryRecoverTruncatedJSON(text string) ([]ExtractedFact, bool) {
	// Look for an opening array bracket — the start of the facts list.
	arrStart := strings.Index(text, "[")
	if arrStart == -1 {
		return nil, false
	}

	// Walk backwards from the end to find the last '}' — end of last complete object.
	sub := text[arrStart:]
	lastBrace := strings.LastIndex(sub, "}")
	if lastBrace == -1 {
		return nil, false
	}

	// Close the array.
	candidate := sub[:lastBrace+1] + "]"

	var facts []ExtractedFact
	if err := json.Unmarshal([]byte(candidate), &facts); err != nil {
		return nil, false
	}
	// Must have recovered at least one fact with content.
	for _, f := range facts {
		if f.Content != "" {
			return facts, true
		}
	}
	return nil, false
}

// tryExtractFactsByKeyBoundaries extracts facts from malformed JSON by using
// known key names ("content", "category", "importance") as structural delimiters.
// This handles the common LLM failure mode where string values contain unescaped
// double quotes that break standard JSON parsing (e.g. content with Korean
// parenthetical quotes: "사용자가 단축 반응 ("이제 됐나")").
func tryExtractFactsByKeyBoundaries(text string) ([]ExtractedFact, bool) {
	var facts []ExtractedFact
	pos := 0

	for {
		// Find next "content" key.
		ci := strings.Index(text[pos:], `"content"`)
		if ci == -1 {
			break
		}
		ci += pos

		fact, nextPos := extractFactAtKeyBoundary(text, ci)
		if fact.Content != "" {
			facts = append(facts, fact)
		}
		// Ensure forward progress to avoid infinite loop.
		if nextPos <= ci {
			nextPos = ci + len(`"content"`)
		}
		pos = nextPos
	}

	if len(facts) > 0 {
		return facts, true
	}
	return nil, false
}

// extractFactAtKeyBoundary extracts a single fact starting from a "content" key
// position. Returns the parsed fact and the position to continue scanning from.
func extractFactAtKeyBoundary(text string, contentKeyPos int) (ExtractedFact, int) {
	var fact ExtractedFact
	after := text[contentKeyPos+len(`"content"`):]
	nextPos := contentKeyPos + len(`"content"`)

	// Find the colon after "content".
	colonIdx := strings.IndexByte(after, ':')
	if colonIdx == -1 {
		return fact, nextPos
	}
	valStart := after[colonIdx+1:]

	// Find opening quote of the content value.
	qIdx := strings.IndexByte(valStart, '"')
	if qIdx == -1 {
		return fact, nextPos
	}
	valStr := valStart[qIdx+1:]

	// Use "category" key as the end boundary for content value.
	catKeyIdx := strings.Index(valStr, `"category"`)
	if catKeyIdx == -1 {
		// No category key — can't reliably extract.
		return fact, nextPos
	}

	// Content is everything up to the "category" key, trimmed of trailing junk.
	rawContent := valStr[:catKeyIdx]
	fact.Content = trimJSONValueSuffix(rawContent)

	// Extract category value: between "category": " and "importance".
	catAfter := valStr[catKeyIdx+len(`"category"`):]
	catColonIdx := strings.IndexByte(catAfter, ':')
	if catColonIdx != -1 {
		catValStart := catAfter[catColonIdx+1:]
		catQIdx := strings.IndexByte(catValStart, '"')
		if catQIdx != -1 {
			fact.Category = extractQuotedValue(catValStart[catQIdx+1:])
		}
	}

	// Extract importance value: numeric after "importance":
	impKeyIdx := strings.Index(valStr[catKeyIdx:], `"importance"`)
	if impKeyIdx != -1 {
		impAfter := valStr[catKeyIdx+impKeyIdx+len(`"importance"`):]
		impColonIdx := strings.IndexByte(impAfter, ':')
		if impColonIdx != -1 {
			impStr := strings.TrimSpace(impAfter[impColonIdx+1:])
			fact.Importance = parseLeadingFloat(impStr)
		}
		nextPos = contentKeyPos + len(`"content"`) + (colonIdx + 1) + (qIdx + 1) + catKeyIdx + impKeyIdx + len(`"importance"`)
	} else {
		nextPos = contentKeyPos + len(`"content"`) + (colonIdx + 1) + (qIdx + 1) + catKeyIdx + len(`"category"`)
	}

	return fact, nextPos
}

// trimJSONValueSuffix strips trailing JSON structural chars from a raw value
// extracted by key-boundary parsing: trailing quotes, commas, whitespace.
func trimJSONValueSuffix(s string) string {
	s = strings.TrimRight(s, " \t\n\r")
	// Strip trailing: ," or , " or "  ,  "
	s = strings.TrimRight(s, " \t\n\r,\"")
	return strings.TrimSpace(s)
}

// extractQuotedValue extracts a string value terminated by the next
// unescaped quote (for short, well-formed enum values like category).
func extractQuotedValue(s string) string {
	end := strings.IndexByte(s, '"')
	if end == -1 {
		return strings.TrimRight(s, " \t\n\r,\"")
	}
	return s[:end]
}

// parseLeadingFloat parses a float from the beginning of a string,
// stopping at the first non-numeric character.
func parseLeadingFloat(s string) float64 {
	end := 0
	for end < len(s) {
		c := s[end]
		if c == '.' || (c >= '0' && c <= '9') {
			end++
		} else {
			break
		}
	}
	if end == 0 {
		return 0
	}
	var v float64
	fmt.Sscanf(s[:end], "%f", &v)
	return v
}

// stripCodeFences removes thinking tags and markdown code fences from LLM output.
// Used by ExtractFacts which has its own multi-strategy parseFactsResponse.
// Dream phases use callLLMJSON → jsonutil.ExtractObject instead.
func stripCodeFences(s string) string {
	return jsonutil.ExtractObject(s)
}

func isValidCategory(c string) bool {
	switch c {
	case CategoryDecision, CategoryPreference, CategorySolution, CategoryContext, CategoryUserModel, CategoryMutual:
		return true
	}
	return false
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// truncate truncates s to at most maxRunes runes, appending "..." if truncated.
// Rune-safe for Korean/CJK multi-byte UTF-8.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

