package chat

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

const (
	recallPreflightTimeout = 1500 * time.Millisecond
	recallMaxQueries       = 6
	// recallMaxEvidence is the evidence budget for an explicit recall cue;
	// silent every-turn auto-recall gets the tighter recallAutoMaxEvidence.
	recallMaxEvidence     = 8
	recallAutoMaxEvidence = 4
	// recallBroadeningPenalty multiplies the score of a hit found only by an
	// individual broadening term (not the combined multi-term query), demoting
	// incidental single-common-term matches (e.g. "조직" → an unrelated "조직명"
	// page) below on-topic combined-query hits.
	recallBroadeningPenalty = 0.7
	recallMaxChars          = 6500
	recallContextOpenTag    = `<recall-context source="server-preflight" trust="untrusted">`
	recallContextCloseTag   = `</recall-context>`
)

// recallEvidenceBudget returns how many evidence rows a turn may carry.
func recallEvidenceBudget(cue bool) int {
	if cue {
		return recallMaxEvidence
	}
	return recallAutoMaxEvidence
}

// recallPrimaryQuery returns the combined multi-term query (expressing the full
// user intent) that recallSearchQueries emits, or "" when the message had only
// one signal term. The combined query is the sole space-joined entry; tokenized
// single terms never contain spaces.
func recallPrimaryQuery(queries []string) string {
	for _, q := range queries {
		if strings.Contains(q, " ") {
			return q
		}
	}
	return ""
}

// applyBroadeningPenalty demotes evidence found only by an individual
// broadening term below combined-query hits (see the call site for rationale).
// Rows tagged with an anchor sentinel (project/counterparty) keep their score:
// anchors are pinned structurally, not found by a term.
func applyBroadeningPenalty(evidence []recallEvidence, queries []string) {
	primary := recallPrimaryQuery(queries)
	if primary == "" {
		return
	}
	for i := range evidence {
		q := evidence[i].Query
		if q != "" && q != primary && q != recallProjectAnchorQuery && q != recallCounterpartyAnchorQuery {
			evidence[i].Score *= recallBroadeningPenalty
		}
	}
}

// dedupRecallEvidence collapses rows describing the same content surfaced via
// different sources, keeping the best-scored row. Keyed on a normalized note
// prefix: refs differ across sources for the same fact, the words don't.
func dedupRecallEvidence(evidence []recallEvidence) []recallEvidence {
	if len(evidence) <= 1 {
		return evidence
	}
	bestIdx := make(map[string]int, len(evidence))
	out := evidence[:0]
	for _, ev := range evidence {
		key := recallContentKey(ev.Note)
		if key == "" {
			out = append(out, ev)
			continue
		}
		if i, ok := bestIdx[key]; ok {
			if ev.Score > out[i].Score {
				out[i] = ev
			}
			continue
		}
		bestIdx[key] = len(out)
		out = append(out, ev)
	}
	return out
}

// recallContentKey normalizes a note for duplicate detection: lowercase,
// letters/digits only, first 80 runes.
func recallContentKey(note string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.ToLower(note) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 80 {
			break
		}
	}
	return b.String()
}

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
	// Generic request/action verbs (stems after suffix-strip). The recall
	// subject is the nouns, never the imperative — left in, these fire as
	// standalone single-term queries that match unrelated entries by a common
	// word (puppet measurement: "정리" from "정리해줘" matched "디스크 정리"/
	// "키 정리" for a "탑솔라 조직" question). Domain nouns like 분석/보고/견적
	// are deliberately NOT here — they are real subjects.
	"정리": {}, "확인": {}, "검토": {}, "요청": {}, "처리": {}, "진행": {}, "작성": {}, "준비": {}, "전달": {}, "알려": {}, "보여": {}, "부탁": {},
	// Conversational filler: greetings/acks, deictic time words, question
	// words, and auxiliary verb stems. None of these are recall subjects, and
	// each fires as a standalone broadening query that matches unrelated
	// entries by a common word (puppet measurement: "안녕, 오늘 뭐 도와줄 수
	// 있어?" built the query "안녕 오늘 도와줄 있어" and pulled three rows of
	// an unrelated session via "오늘"). Time deictics are safe to drop here:
	// temporal scoping is handled separately by parseRecallTemporalRange on
	// the raw message, so the boost still applies — the words just stop
	// doubling as search terms.
	"안녕": {}, "안녕하세요": {}, "하이": {}, "헬로": {}, "반가워": {}, "반갑": {},
	"고마워": {}, "고맙": {}, "감사": {}, "땡큐": {}, "오케이": {}, "좋아": {}, "그래": {},
	"오늘": {}, "어제": {}, "내일": {}, "모레": {}, "지금": {}, "이제": {}, "현재": {}, "요즘": {}, "최근": {}, "이번": {}, "다음": {}, "이따": {}, "나중": {},
	"뭐": {}, "무엇": {}, "어떻게": {}, "어때": {}, "왜": {}, "언제": {}, "어디": {}, "누구": {}, "얼마": {}, "몇": {},
	"도와": {}, "도와줘": {}, "도와줄": {}, "있어": {}, "있어요": {}, "있나": {}, "있는": {}, "없어": {}, "없어요": {}, "없는": {},
	"할까": {}, "할래": {}, "될까": {}, "되나": {}, "알아": {}, "몰라": {}, "궁금": {},
	"hello": {}, "thanks": {}, "thank": {}, "please": {}, "today": {}, "tomorrow": {}, "yesterday": {},
}

var recallFenceTagPattern = regexp.MustCompile(`(?i)</?\s*recall-context\b[^>]*>`)

// The second return reports whether the shared preflight deadline cut at
// least one source short: the snapshot is still usable for the current turn
// but must not be frozen into the recall cache (see shouldFreezeRecallSnapshot).
func buildRecallPreflight(ctx context.Context, params RunParams, deps runDeps, logger *slog.Logger) (out string, truncated bool) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Warn("recall preflight recovered panic", "session", params.SessionKey, "panic", r)
			}
			out = ""
		}
	}()

	if params.EphemeralUser || params.SkipRecall {
		return "", false
	}

	message := strings.TrimSpace(params.Message)
	if message == "" {
		return "", false
	}
	// Hermes-style auto_recall: search EVERY turn, not just cue turns. The ~1.5s
	// preflight cost is accepted in exchange for automatic cross-session context
	// restoration — new sessions and ordinary turns pull relevant past work without
	// the user having to say "지난번"/"아까". cue now only affects visibility (below):
	// explicit recall surfaces a no-evidence notice, silent auto-recall stays invisible.
	cue := shouldRunRecallPreflight(message)
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, recallPreflightTimeout)
	defer cancel()

	queries := recallSearchQueries(message)

	// All sources run concurrently under the shared preflight deadline. They
	// used to run serially, so a slow wiki search ate the 1.5s budget and
	// starved diary/polaris — and every turn paid the SUM of source latencies
	// instead of the slowest one. Results land in ordered slots to keep the
	// historical evidence order (wiki, diary, session).
	type recallSource struct {
		name string
		run  func(context.Context) []recallEvidence
	}
	var sources []recallSource
	if deps.memory.Wiki != nil {
		store := deps.memory.Wiki
		sources = append(
			sources,
			recallSource{"wiki", func(c context.Context) []recallEvidence {
				return recallWikiEvidence(c, store, queries, message)
			}},
			recallSource{"diary", func(c context.Context) []recallEvidence {
				return recallDiaryEvidence(c, store, queries, false)
			}},
		)
	}
	if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
		sources = append(sources, recallSource{"polaris", func(c context.Context) []recallEvidence {
			return recallPolarisEvidence(c, bridge, params.SessionKey, queries)
		}})
	} else {
		sources = append(sources, recallSource{"transcript", func(c context.Context) []recallEvidence {
			return recallTranscriptEvidence(c, deps.transcript, params.SessionKey, message, queries)
		}})
	}
	// On-box file store (hybrid semantic search). Runs under the same shared
	// deadline; a down embedding server returns zero file evidence, so this never
	// blocks the turn. The per-source quota (recallFileQuota) is enforced inside
	// recallFilesEvidence so files cannot crowd out the other sources.
	if deps.memory.FileRecall != nil {
		search := deps.memory.FileRecall
		sources = append(sources, recallSource{"file", func(c context.Context) []recallEvidence {
			return recallFilesEvidence(c, search, queries)
		}})
	}
	// Org chart source: people/divisions named in the message → their 부서 + 인물
	// page. Runs under the same shared deadline; a missing org file yields zero
	// evidence (graceful-empty), so this never blocks the turn. Needs the wiki
	// store to resolve members to their person pages.
	if deps.memory.Org != nil {
		load := deps.memory.Org
		store := deps.memory.Wiki
		sources = append(sources, recallSource{"org", func(c context.Context) []recallEvidence {
			return recallOrgEvidence(c, load, store, message)
		}})
	}

	slots := make([][]recallEvidence, len(sources))
	elapsed := make([]time.Duration, len(sources))
	cut := make([]bool, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src recallSource) {
			defer wg.Done()
			// Per-goroutine recovery: the outer recover cannot see goroutine
			// panics, and one broken source must cost its slot, not the turn.
			defer func() {
				if r := recover(); r != nil && logger != nil {
					logger.Warn("recall preflight: source panicked", "source", src.name, "panic", r)
				}
			}()
			start := time.Now()
			slots[i] = src.run(ctx)
			elapsed[i] = time.Since(start)
			// Sampled at this source's own return: a source that finished
			// before the deadline stays false even if the budget expires
			// later, while one that early-returned on ctx.Err() (or came back
			// from a blocking call to find the deadline gone) is flagged —
			// its slot likely holds partial evidence.
			cut[i] = ctx.Err() != nil
		}(i, src)
	}
	wg.Wait()
	for _, c := range cut {
		if c {
			truncated = true
			break
		}
	}

	// Per-source contribution + latency. This accumulating record is the
	// evidence base for backend consolidation: a source that never
	// contributes rows but always burns the deadline argues for retirement.
	sourceStats := make([]string, 0, len(sources))
	for i, src := range sources {
		sourceStats = append(sourceStats,
			fmt.Sprintf("%s=%d(%dms)", src.name, len(slots[i]), elapsed[i].Milliseconds()))
	}
	sourceSummary := strings.Join(sourceStats, " ")

	var evidence []recallEvidence
	for _, slot := range slots {
		evidence = append(evidence, slot...)
	}
	// Recent-diary fallback ONLY for topicless cues ("아까 뭐였지?" — no signal
	// terms, so nothing was searchable). A topical question that found nothing
	// must get the honest no-evidence notice, not two unrelated recent diary
	// entries dressed up as recall (the bench caught exactly that). The cue
	// gate matters: a topicless NON-cue turn is smalltalk ("안녕, 오늘 뭐
	// 도와줄 수 있어?"), and silent auto-recall must stay silent there rather
	// than dress recent diary entries up as relevant context.
	if len(evidence) == 0 && cue && deps.memory.Wiki != nil && len(queries) == 0 {
		evidence = append(evidence, recallDiaryEvidence(ctx, deps.memory.Wiki, queries, true)...)
	}

	if len(evidence) == 0 {
		if logger != nil {
			logger.Info("recall preflight: no evidence",
				"session", params.SessionKey, "sources", sourceSummary, "truncated", truncated)
		}
		// Explicit recall tells the user nothing was found; silent auto-recall on a
		// non-cue turn stays invisible so every-turn search adds no noise.
		if cue {
			return formatRecallNoEvidence(), truncated
		}
		return "", truncated
	}

	// Broadening-query penalty: recallSearchQueries issues one combined
	// multi-term query (the full intent) plus one query per individual term to
	// broaden recall. A hit found ONLY by an individual term is lower precision
	// (e.g. the bare term "조직" matching an unrelated "조직명" page), so it must
	// rank below combined-query hits. Within-source dedup already recorded
	// combined-query hits under the combined query string (it runs first), so
	// this demotes only the term-only stragglers. No-op for single-term messages.
	// The project-anchor sentinel is exempt — it marks a guaranteed structural
	// anchor, not a term hit, and the penalty let combined-query wiki hits
	// outrank the named project's 대표페이지.
	applyBroadeningPenalty(evidence, queries)

	// The same fact often surfaces from several sources at once (wiki page +
	// polaris summary + diary echo); duplicate rows waste the evidence budget.
	evidence = dedupRecallEvidence(evidence)

	// Situational provenance weighting: a curated wiki figure that numerically
	// contradicts the raw diary for the same fact (dreamer synthesis drift) is
	// demoted below that raw observation, so the fixed wiki>diary prior cannot
	// rank a drifted value first. Type-aware + entity-scoped — see
	// recall_provenance.go. Must run before the sort (it adjusts scores).
	applyProvenancePenalty(evidence)

	// RaMem-style temporal scoping (arXiv 2606.22844): when the query names a
	// concrete past frame ("지난달", "6월"), softly boost evidence whose timestamp
	// falls inside it so the right-episode memory outranks a content-similar row
	// from another episode ("context collapse"). Cue-gated + soft (boost, not
	// filter): a frameless query is untouched and a strong out-of-frame row can
	// still surface. See recall_temporal.go.
	if tr := parseRecallTemporalRange(message); tr.ok {
		for i := range evidence {
			if at := evidence[i].At; at > 0 && at >= tr.From && at <= tr.To {
				evidence[i].Score *= recallTemporalBoost
			}
		}
	}

	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Score == evidence[j].Score {
			return evidence[i].At > evidence[j].At
		}
		return evidence[i].Score > evidence[j].Score
	})
	// Adaptive budget: an explicit cue earns the full evidence window; silent
	// every-turn auto-recall gets a tighter one — the user didn't ask, so
	// marginal rows are more likely noise than memory.
	if budget := recallEvidenceBudget(cue); len(evidence) > budget {
		evidence = evidence[:budget]
	}
	if logger != nil {
		logger.Info("recall preflight: evidence injected",
			"session", params.SessionKey, "count", len(evidence), "sources", sourceSummary, "truncated", truncated)
	}
	return formatRecallEvidence(evidence), truncated
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
		"하는", "하면", "해서", "해야", "해요", "하고", "줘", "한", "해",
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
