package chat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

const (
	recallPreflightTimeout = 1500 * time.Millisecond
	recallMaxQueries       = 6
	recallMaxEvidence      = 8
	recallMaxChars         = 6500
)

type recallEvidence struct {
	Kind   string
	Source string
	Query  string
	Note   string
	Score  float64
	At     int64
}

var recallCuePhrases = []string{
	"기억", "회상", "전에", "저번", "지난번", "예전에", "아까", "방금", "그때",
	"말했던", "말한", "했던", "해둔", "정리했던", "논의했던", "이어", "이어서", "계속",
	"문맥", "컨텍스트", "뭐였", "뭐더라", "뭘 했", "그거", "그 프로젝트", "그 방향",
}

var recallCueSubstrings = []string{
	"했던", "말했던", "말한", "해둔", "정리했던", "논의했던",
	"이어", "이어서", "계속", "뭐였", "뭐더라",
}

var recallStopWords = map[string]struct{}{
	"기억": {}, "회상": {}, "전에": {}, "저번": {}, "지난번": {}, "예전에": {}, "아까": {}, "방금": {}, "그때": {},
	"말했던": {}, "말한": {}, "했던": {}, "해둔": {}, "정리했던": {}, "논의했던": {}, "이어": {}, "이어서": {}, "계속": {},
	"문맥": {}, "컨텍스트": {}, "뭐였": {}, "뭐더라": {}, "그거": {}, "그": {}, "이": {}, "저": {}, "것": {}, "거": {},
	"좀": {}, "다시": {}, "관련": {}, "쪽": {}, "걸": {}, "를": {}, "을": {}, "은": {}, "는": {}, "이랑": {}, "하고": {},
	"the": {}, "and": {}, "for": {}, "with": {}, "about": {}, "that": {}, "this": {}, "what": {}, "when": {},
}

func buildRecallPreflight(ctx context.Context, params RunParams, deps runDeps, logger *slog.Logger) (out string) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Warn("recall preflight recovered panic", "session", params.SessionKey, "panic", r)
			}
			out = ""
		}
	}()

	message := strings.TrimSpace(params.Message)
	if !shouldRunRecallPreflight(message) {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, recallPreflightTimeout)
	defer cancel()

	queries := recallSearchQueries(message)
	terms := recallSignalTerms(message)
	var evidence []recallEvidence

	if deps.wikiStore != nil {
		evidence = append(evidence, recallWikiEvidence(ctx, deps.wikiStore, queries)...)
		evidence = append(evidence, recallDiaryEvidence(deps.wikiStore.DiaryDir(), terms, false)...)
	}
	_, hasPolarisBridge := deps.transcript.(*polaris.Bridge)
	if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
		evidence = append(evidence, recallPolarisEvidence(ctx, bridge, params.SessionKey, queries)...)
	}
	if !hasPolarisBridge {
		evidence = append(evidence, recallTranscriptEvidence(ctx, deps.transcript, params.SessionKey, message, queries)...)
	}
	if len(evidence) == 0 && deps.wikiStore != nil {
		evidence = append(evidence, recallDiaryEvidence(deps.wikiStore.DiaryDir(), terms, true)...)
	}

	if len(evidence) == 0 {
		if logger != nil {
			logger.Info("recall preflight: no evidence", "session", params.SessionKey)
		}
		return "## 회상 근거 (자동 검색)\n\n사용자 메시지가 과거 맥락을 암시해 위키/일지/세션 이력을 검색했지만 관련 근거를 찾지 못했다. 과거 내용을 확신하지 말고, 필요한 경우 사용자에게 확인하라."
	}

	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Score == evidence[j].Score {
			return evidence[i].At > evidence[j].At
		}
		return evidence[i].Score > evidence[j].Score
	})
	if len(evidence) > recallMaxEvidence {
		evidence = evidence[:recallMaxEvidence]
	}
	if logger != nil {
		logger.Info("recall preflight: evidence injected", "session", params.SessionKey, "count", len(evidence))
	}
	return formatRecallEvidence(evidence)
}

func shouldRunRecallPreflight(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	lower := strings.ToLower(message)
	for _, cue := range recallCuePhrases {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func recallSearchQueries(message string) []string {
	terms := recallSignalTerms(message)
	var queries []string
	if len(terms) >= 2 {
		queries = append(queries, strings.Join(terms[:minInt(4, len(terms))], " "))
	}
	for _, term := range terms {
		queries = append(queries, term)
		if len(queries) >= recallMaxQueries {
			break
		}
	}
	return dedupeStrings(queries)
}

func recallSignalTerms(message string) []string {
	tokens := splitRecallTokens(message)
	var terms []string
	seen := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		tok = normalizeRecallToken(tok)
		if isRecallCueToken(tok) {
			continue
		}
		if !isRecallSignalToken(tok) {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		terms = append(terms, tok)
		if len(terms) >= 8 {
			break
		}
	}
	return terms
}

func normalizeRecallToken(tok string) string {
	tok = strings.Trim(strings.ToLower(tok), "-_")
	if tok == "" {
		return ""
	}
	suffixes := []string{
		"해주세요", "해줘요", "해줘", "했어요", "했어", "했지", "했던", "하던",
		"하는", "하면", "해서", "해야", "해요", "하고", "해",
		"에서", "에게", "으로", "부터", "까지", "이랑",
		"은", "는", "이", "가", "을", "를", "에", "로", "와", "과", "도", "만", "랑",
	}
	for range 2 {
		changed := false
		for _, suffix := range suffixes {
			if !strings.HasSuffix(tok, suffix) {
				continue
			}
			candidate := strings.TrimSuffix(tok, suffix)
			if len([]rune(candidate)) < 2 {
				continue
			}
			tok = candidate
			changed = true
			break
		}
		if !changed {
			break
		}
	}
	return tok
}

func isRecallCueToken(tok string) bool {
	if _, ok := recallStopWords[tok]; ok {
		return true
	}
	for _, cue := range recallCueSubstrings {
		if len([]rune(cue)) >= 2 && strings.Contains(tok, cue) {
			return true
		}
	}
	return false
}

func splitRecallTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return false
		}
		return true
	})
}

func isRecallSignalToken(tok string) bool {
	runes := []rune(tok)
	if len(runes) == 0 {
		return false
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

func recallWikiEvidence(ctx context.Context, store *wiki.Store, queries []string) []recallEvidence {
	if store == nil || len(queries) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var evidence []recallEvidence
	for _, q := range queries {
		if ctx.Err() != nil {
			return evidence
		}
		results, err := store.Search(ctx, q, 3)
		if err != nil {
			continue
		}
		for _, r := range results {
			if _, ok := seen[r.Path]; ok {
				continue
			}
			seen[r.Path] = struct{}{}
			evidence = append(evidence, recallEvidence{
				Kind:   "wiki",
				Source: r.Path,
				Query:  q,
				Note:   formatRecallWikiNote(store, r),
				Score:  0.80 + r.Score,
			})
		}
	}
	return evidence
}

func formatRecallWikiNote(store *wiki.Store, result wiki.SearchResult) string {
	var parts []string
	if page, err := store.ReadPage(result.Path); err == nil && page != nil {
		if page.Meta.Title != "" {
			parts = append(parts, "title: "+page.Meta.Title)
		}
		if page.Meta.Summary != "" {
			parts = append(parts, "summary: "+page.Meta.Summary)
		}
		if len(page.Meta.Tags) > 0 {
			parts = append(parts, "tags: "+strings.Join(page.Meta.Tags, ", "))
		}
	}
	if strings.TrimSpace(result.Content) != "" {
		parts = append(parts, "match: "+strings.TrimSpace(result.Content))
	}
	if len(parts) == 0 {
		return result.Path
	}
	return truncateRecallText(strings.Join(parts, " | "), 420)
}

func recallDiaryEvidence(diaryDir string, terms []string, includeRecentFallback bool) []recallEvidence {
	entries := loadRecentDiaryEntries(diaryDir, 3, 12)
	if len(entries) == 0 {
		return nil
	}
	if len(terms) == 0 {
		if !includeRecentFallback {
			return nil
		}
		var recent []recallEvidence
		for i := 0; i < minInt(2, len(entries)); i++ {
			recent = append(recent, diaryEntryEvidence(entries[i], terms))
		}
		return recent
	}
	var evidence []recallEvidence
	for _, entry := range entries {
		if !containsAnyTerm(entry.Content, terms) {
			continue
		}
		evidence = append(evidence, diaryEntryEvidence(entry, terms))
		if len(evidence) >= 4 {
			return evidence
		}
	}
	if len(evidence) == 0 && includeRecentFallback {
		for i := 0; i < minInt(2, len(entries)); i++ {
			evidence = append(evidence, diaryEntryEvidence(entries[i], terms))
		}
	}
	return evidence
}

func recallTranscriptEvidence(ctx context.Context, transcript TranscriptStore, sessionKey, currentMessage string, queries []string) []recallEvidence {
	if transcript == nil || len(queries) == 0 {
		return nil
	}
	currentMessage = strings.TrimSpace(currentMessage)
	seen := make(map[string]struct{})
	var evidence []recallEvidence
	for _, q := range queries {
		if ctx.Err() != nil {
			return evidence
		}
		results, err := transcript.Search(q, 6)
		if err != nil {
			continue
		}
		for _, result := range results {
			if result.SessionKey != sessionKey {
				continue
			}
			for _, match := range result.Matches {
				text := strings.TrimSpace(match.Message.TextContent())
				if text == "" || text == currentMessage {
					continue
				}
				key := fmt.Sprintf("%s#%d", result.SessionKey, match.Index)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				evidence = append(evidence, recallEvidence{
					Kind:   "transcript",
					Source: fmt.Sprintf("%s#%d/%s", abbreviateSession(result.SessionKey), match.Index, match.Message.Role),
					Query:  q,
					Note:   formatRecallTranscriptNote(match),
					Score:  0.58,
					At:     match.Message.Timestamp,
				})
				if len(evidence) >= 4 {
					return evidence
				}
			}
		}
	}
	return evidence
}

func formatRecallTranscriptNote(match MatchedMsg) string {
	text := strings.TrimSpace(match.Message.TextContent())
	var contextParts []string
	for _, ctxMsg := range match.Context {
		ctxText := strings.TrimSpace(ctxMsg.TextContent())
		if ctxText == "" {
			continue
		}
		contextParts = append(contextParts, ctxMsg.Role+": "+truncateRecallText(ctxText, 120))
	}
	if len(contextParts) == 0 {
		return truncateRecallText(text, 300)
	}
	return truncateRecallText(text+" | context: "+strings.Join(contextParts, " / "), 420)
}

func recallPolarisEvidence(ctx context.Context, bridge *polaris.Bridge, sessionKey string, queries []string) []recallEvidence {
	if bridge == nil || sessionKey == "" || len(queries) == 0 {
		return nil
	}
	// Ensure legacy transcript messages are migrated before searching the Polaris FTS index.
	_, _, _ = bridge.Load(sessionKey, 0)
	store := bridge.Store()
	maxIdx, _ := store.MaxMsgIndex(sessionKey)

	seen := make(map[int]struct{})
	var evidence []recallEvidence
	for _, q := range queries {
		if ctx.Err() != nil {
			return evidence
		}
		hits, err := store.SearchMessages(sessionKey, q, 3)
		if err != nil {
			continue
		}
		for _, h := range hits {
			if h.MsgIndex == maxIdx {
				continue // current user message is already in context; do not echo it as recall.
			}
			if _, ok := seen[h.MsgIndex]; ok {
				continue
			}
			seen[h.MsgIndex] = struct{}{}
			evidence = append(evidence, recallEvidence{
				Kind:   "session",
				Source: fmt.Sprintf("msg#%d/%s", h.MsgIndex, h.Role),
				Query:  q,
				Note:   truncateRecallText(h.Snippet, 280),
				Score:  0.65 + h.Score,
				At:     h.Timestamp,
			})
		}
	}
	return evidence
}

type diaryEntry struct {
	File    string
	Header  string
	Content string
	At      int64
}

func loadRecentDiaryEntries(diaryDir string, maxFiles, maxEntries int) []diaryEntry {
	if diaryDir == "" {
		return nil
	}
	files, err := os.ReadDir(diaryDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasPrefix(name, "diary-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })
	if len(names) > maxFiles {
		names = names[:maxFiles]
	}

	var entries []diaryEntry
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(diaryDir, name))
		if err != nil {
			continue
		}
		parsed := parseDiaryEntries(name, string(data))
		for i, j := 0, len(parsed)-1; i < j; i, j = i+1, j-1 {
			parsed[i], parsed[j] = parsed[j], parsed[i]
		}
		entries = append(entries, parsed...)
		if len(entries) >= maxEntries {
			return entries[:maxEntries]
		}
	}
	return entries
}

func parseDiaryEntries(fileName, content string) []diaryEntry {
	chunks := strings.Split("\n"+content, "\n## ")
	var entries []diaryEntry
	for _, chunk := range chunks[1:] {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		header, body, _ := strings.Cut(chunk, "\n")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		entries = append(entries, diaryEntry{
			File:    fileName,
			Header:  strings.TrimSpace(header),
			Content: body,
			At:      diaryEntryUnix(fileName, header),
		})
	}
	return entries
}

func diaryEntryUnix(fileName, header string) int64 {
	date := strings.TrimSuffix(strings.TrimPrefix(fileName, "diary-"), ".md")
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+strings.TrimSpace(header), time.Local)
	if err != nil {
		return 0
	}
	return ts.UnixMilli()
}

func diaryEntryEvidence(entry diaryEntry, terms []string) recallEvidence {
	score := 0.70
	if len(terms) == 0 {
		score = 0.55
	}
	return recallEvidence{
		Kind:   "diary",
		Source: entry.File + "#" + entry.Header,
		Note:   truncateRecallText(entry.Content, 320),
		Score:  score,
		At:     entry.At,
	}
}

func containsAnyTerm(text string, terms []string) bool {
	lower := strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func formatRecallEvidence(evidence []recallEvidence) string {
	var sb strings.Builder
	sb.WriteString("## 회상 근거 (자동 검색)\n\n")
	sb.WriteString("사용자 메시지가 과거 맥락을 암시해 서버가 위키/일지/세션 이력을 미리 검색했다. 아래 근거만 확실한 과거 맥락으로 사용하고, 근거가 부족하면 부족하다고 말하라.\n\n")

	for _, ev := range evidence {
		entry := fmt.Sprintf("- [%s] `%s`", ev.Kind, ev.Source)
		if ev.Query != "" {
			entry += fmt.Sprintf(" (query: %q", ev.Query)
			entry += fmt.Sprintf(", score: %.2f)", ev.Score)
		} else {
			entry += fmt.Sprintf(" (score: %.2f)", ev.Score)
		}
		entry += "\n  " + strings.ReplaceAll(strings.TrimSpace(ev.Note), "\n", " ") + "\n"
		if sb.Len()+len(entry) > recallMaxChars {
			break
		}
		sb.WriteString(entry)
	}
	return sb.String()
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func truncateRecallText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
