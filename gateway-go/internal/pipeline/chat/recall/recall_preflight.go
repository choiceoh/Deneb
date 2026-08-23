package recall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	mem "github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
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
// user intent) that searchQueries emits, or "" when the message had only
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

// isLedgerPage reports whether an evidence row points at a real wiki page the
// utility ledger should score.
//
// Kind is the SOURCE that produced the row, not the artifact it points at: an
// 인물 page pulled in through the org chart arrives as Kind "org" carrying the
// page's relPath in Source (recall_org.go: resolveOrgPersonPaths). Recording
// only Kind=="wiki" therefore made person pages structurally invisible to the
// ledger — measured 2026-07-27: the org source fired on 12% of preflights over
// 7 days and contributed ZERO ledger lines, so the utility report read 인물 as
// 2% used (255 pages) when that number was really coverage, not usage. Judge by
// what Source points at: a page path, not an "조직도: 이름" placeholder for a
// member with no page yet.
func isLedgerPage(ev recallEvidence) bool {
	if ev.Source == "" {
		return false
	}
	switch ev.Kind {
	case "wiki":
		return true
	case "org":
		// Org rows carry either a page relPath or a "조직도: <이름>" label for a
		// member the wiki has no page for; only the former is a page.
		return strings.HasSuffix(ev.Source, ".md")
	}
	return false
}

// recordRecallMiss tees an unanswered CUE turn into the demand ledger (wiki/
// recall_misses.go) — the supply side (recordRecallUtility) records which pages
// got used, this records which questions had no page at all. Best-effort: a nil
// store or a write error is swallowed after a single Warn.
func recordRecallMiss(store *wiki.Store, sessionKey, message string, logger *slog.Logger) {
	if store == nil || strings.TrimSpace(message) == "" {
		return
	}
	if err := store.RecordRecallMiss(wiki.RecallMiss{Query: message, Session: sessionKey}); err != nil && logger != nil {
		logger.Warn("recall preflight: demand ledger write failed", "error", err)
	}
}

// recordRecallUtility tees the injected wiki-page evidence into the store's
// recall-utility ledger (효용 접지) as inject events, each carrying the
// retrieval context (query label, injection rank, preflight score — so
// real-traffic (query → page) pairs can be mined as gold-set candidates) and
// the session, so downstream usage events (read/cite) can be attributed
// against the exposure. Which rows count as pages is isLedgerPage's call;
// diary/transcript/file rows are not dreamer-managed pages, so they are not
// scored. Rank is the row's 1-based position in the FULL ranked evidence list
// (all kinds) — i.e. its position in the recall block the model actually saw.
// Returns the recorded paths so the caller can arm the end-of-turn citation
// pass. Best-effort: a nil store or a write error is swallowed after a single
// Warn — losing this derived telemetry is not user-observable and self-heals
// next turn.
func recordRecallUtility(store *wiki.Store, evidence []recallEvidence, sessionKey string, cue bool, logger *slog.Logger) []string {
	if store == nil || len(evidence) == 0 {
		return nil
	}
	// Turn-level gate-shadow signals (arXiv 2607.14390's episodic confidence
	// gate): the paper separates useful from harmful injections by the ranked
	// block's top-1 score and top1−top2 gap. We record the signals on every
	// inject line — production behavior is UNCHANGED — so the silence gate can
	// be calibrated offline against this ledger's read/cite outcomes before
	// any threshold ever fires (recall bottleneck doctrine: no ungrounded
	// suppression).
	top1 := evidence[0].Score
	gap := 0.0
	if len(evidence) > 1 {
		gap = top1 - evidence[1].Score
	}
	events := make([]wiki.RecallEvent, 0, len(evidence))
	paths := make([]string, 0, len(evidence))
	for i, ev := range evidence {
		if isLedgerPage(ev) {
			events = append(events, wiki.RecallEvent{
				Path:    ev.Source,
				Event:   wiki.RecallEventInject,
				Query:   ev.Query,
				Rank:    i + 1,
				Score:   ev.Score,
				Session: sessionKey,
				Top1:    top1,
				Gap:     gap,
				Cue:     cue,
			})
			paths = append(paths, ev.Source)
		}
	}
	if len(events) == 0 {
		return nil
	}
	if err := store.RecordRecallEvents(events); err != nil && logger != nil {
		logger.Warn("recall preflight: recall-hit ledger write failed", "error", err)
	}
	return paths
}

type recallEvidence struct {
	Kind      string
	Source    string
	Query     string
	Note      string
	Score     float64
	At        int64
	SubjectID string // empty/self = operator; used for cross-subject filtering (M6)
}

type recallSource struct {
	name string
	run  func(context.Context) []recallEvidence
}

type recallSourceResult struct {
	evidence  []recallEvidence
	elapsed   time.Duration
	truncated bool
}

type recallCollection struct {
	evidence      []recallEvidence
	sourceSummary string
	truncated     bool
}

type indexedRecallSourceResult struct {
	index  int
	result recallSourceResult
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

// Build gathers and formats recall evidence for one turn. The second return
// reports whether the shared deadline cut at least one source short: the
// snapshot is usable now but must not be frozen (see ShouldFreeze).
func Build(ctx context.Context, params Params, deps Deps, logger *slog.Logger) (out string, truncated bool) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Warn("recall preflight recovered panic", "session", params.SessionKey, "panic", r)
			}
			if deps.Briefcase {
				deps.recordStrictError(errors.New("briefcase recall preflight panicked"))
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
	cue := hasCue(message)
	if ctx == nil {
		ctx = context.Background()
	}
	// Production caps broad ambient recall at 1.5s. Briefcase already carries a
	// signed per-turn/global deadline; an additional host-speed-sensitive cutoff
	// would make identical casepacks observe different partial source sets.
	if !deps.Briefcase {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, recallPreflightTimeout)
		defer cancel()
	}

	queries := searchQueries(message)

	sources := buildRecallSources(params, deps, queries, message)
	collection := runRecallSources(ctx, sources, deps, logger)
	truncated = collection.truncated
	evidence := collection.evidence
	// Recent-diary fallback ONLY for topicless cues ("아까 뭐였지?" — no signal
	// terms, so nothing was searchable). A topical question that found nothing
	// must get the honest no-evidence notice, not two unrelated recent diary
	// entries dressed up as recall (the bench caught exactly that). The cue
	// gate matters: a topicless NON-cue turn is smalltalk ("안녕, 오늘 뭐
	// 도와줄 수 있어?"), and silent auto-recall must stay silent there rather
	// than dress recent diary entries up as relevant context.
	evidence = withTopiclessDiaryFallback(ctx, evidence, cue, deps, queries)

	if len(evidence) == 0 {
		if logger != nil {
			logger.Info("recall preflight: no evidence",
				"session", params.SessionKey, "sources", collection.sourceSummary, "truncated", truncated)
		}
		// A turn that injected nothing must clear the citation candidates, or a
		// previous turn's paths would be mis-attributed to this turn's answer.
		if !deps.Briefcase {
			StoreInjectedPaths(params.SessionKey, nil)
		}
		// Explicit recall tells the user nothing was found; silent auto-recall on a
		// non-cue turn stays invisible so every-turn search adds no noise.
		if cue {
			// A cue turn that found nothing is a measured hole in the curated
			// memory — the demand signal the research lane consumes (wiki/
			// recall_misses.go). Cue-only: silent auto-recall legitimately finds
			// nothing on smalltalk, and recording that would bury real demand.
			recordRecallMiss(deps.Wiki, params.SessionKey, message, logger)
			// The no-evidence notice also carries the routing hint: "찾은 게
			// 없다"에서 끝내지 말고 맞는 도구로 가라는 다음 행선지.
			return appendRoutingHint(formatRecallNoEvidence(), message), truncated
		}
		return "", truncated
	}

	// wiki↔메일 담당자 충돌: 둘 다 올리고 불일치만 표시. 점수 중재 없음
	// (stale 위키가 이기면 안 된다). 검색은 cue 턴에서만 보강.
	evidence = attachWikiMailConflicts(ctx, deps.Wiki, evidence, cue)
	evidence = rankRecallEvidence(evidence, queries, message, cue, deps.now())
	if logger != nil {
		logger.Info("recall preflight: evidence injected",
			"session", params.SessionKey, "count", len(evidence), "sources", collection.sourceSummary, "truncated", truncated)
	}
	// 효용 접지: record which wiki pages this turn actually pulled into context so
	// the dream cycle can learn which of its writes earn their keep (recall_hits.go),
	// and arm the end-of-turn citation pass with the same paths (skipped in
	// briefcase mode — casepack replays have no citation pass to consume them).
	// Best-effort telemetry — a ledger write must never affect the turn.
	injected := recordRecallUtility(deps.Wiki, evidence, params.SessionKey, cue, logger)
	if !deps.Briefcase {
		StoreInjectedPaths(params.SessionKey, injected)
	}
	// Routing hint for query shapes page search cannot answer (aggregate/
	// temporal/graph — recall_route.go): the wiki evidence above usually EXISTS
	// for these but is the wrong answer surface, so the hint rides along to
	// nudge the right tool. Outside the fence — server guidance, not recall.
	return appendRoutingHint(formatRecallEvidenceAt(evidence, deps.now()), message), truncated
}

func buildRecallSources(params Params, deps Deps, queries []string, message string) []recallSource {
	var sources []recallSource
	if deps.Wiki != nil {
		store := deps.Wiki
		sources = append(
			sources,
			recallSource{name: "wiki", run: func(ctx context.Context) []recallEvidence {
				evidence, err := recallWikiEvidenceResult(ctx, store, queries, message)
				if err != nil && deps.Briefcase {
					deps.recordStrictError(fmt.Errorf("briefcase wiki recall: %w", err))
				}
				return evidence
			}},
			recallSource{name: "diary", run: func(ctx context.Context) []recallEvidence {
				evidence, err := recallDiaryEvidenceResult(ctx, store, queries, false)
				if err != nil && deps.Briefcase {
					deps.recordStrictError(fmt.Errorf("briefcase diary recall: %w", err))
				}
				return evidence
			}},
		)
	}

	if bridge, ok := deps.Transcript.(*polaris.Bridge); ok {
		sources = append(sources, recallSource{name: "polaris", run: func(ctx context.Context) []recallEvidence {
			return recallPolarisEvidence(ctx, bridge, params.SessionKey, queries)
		}})
	} else {
		sources = append(sources, recallSource{name: "transcript", run: func(ctx context.Context) []recallEvidence {
			return recallTranscriptEvidence(ctx, deps.Transcript, params.SessionKey, message, queries)
		}})
	}

	if deps.FileRecall != nil {
		search := deps.FileRecall
		sources = append(sources, recallSource{name: "file", run: func(ctx context.Context) []recallEvidence {
			return recallFilesEvidence(ctx, search, queries)
		}})
	}
	if deps.Org != nil {
		load := deps.Org
		store := deps.Wiki
		sources = append(sources, recallSource{name: "org", run: func(ctx context.Context) []recallEvidence {
			return recallOrgEvidence(ctx, load, store, message)
		}})
	}
	return sources
}

// runRecallSources keeps source execution concurrent while collecting results
// in declaration order. Completion order must not change tie-breaking input.
// A source that ignores cancellation must not defeat the shared preflight
// deadline: collect completed slots through a buffered channel and return the
// partial result when ctx expires. The buffer lets a late source finish without
// blocking after the caller has moved on.
func runRecallSources(ctx context.Context, sources []recallSource, deps Deps, logger *slog.Logger) recallCollection {
	if ctx == nil {
		ctx = context.Background()
	}
	results := make([]recallSourceResult, len(sources))
	completed := make([]bool, len(sources))
	resultCh := make(chan indexedRecallSourceResult, len(sources))
	for i, source := range sources {
		go func(index int, source recallSource) {
			resultCh <- indexedRecallSourceResult{
				index:  index,
				result: runRecallSource(ctx, source, deps, logger),
			}
		}(i, source)
	}

	remaining := len(sources)
	deadlineReached := false
	for remaining > 0 {
		select {
		case item := <-resultCh:
			results[item.index] = item.result
			completed[item.index] = true
			remaining--
		case <-ctx.Done():
			deadlineReached = true
			remaining = 0
		}
	}
	// Results already buffered at the deadline completed within the budget;
	// retain them without waiting for any still-running source.
	if deadlineReached {
	drainResults:
		for {
			select {
			case item := <-resultCh:
				results[item.index] = item.result
				completed[item.index] = true
			default:
				break drainResults
			}
		}
	}

	collection := recallCollection{}
	sourceStats := make([]string, 0, len(sources))
	for i, source := range sources {
		if !completed[i] {
			collection.truncated = true
			sourceStats = append(sourceStats, fmt.Sprintf("%s=0(deadline)", source.name))
			continue
		}
		result := results[i]
		collection.evidence = append(collection.evidence, result.evidence...)
		collection.truncated = collection.truncated || result.truncated
		sourceStats = append(sourceStats,
			fmt.Sprintf("%s=%d(%dms)", source.name, len(result.evidence), result.elapsed.Milliseconds()))
	}
	collection.sourceSummary = strings.Join(sourceStats, " ")
	return collection
}

func runRecallSource(
	ctx context.Context,
	source recallSource,
	deps Deps,
	logger *slog.Logger,
) (result recallSourceResult) {
	// The outer Build recovery cannot see goroutine panics. A broken source
	// therefore loses only its own ordered slot, never the whole turn.
	defer func() {
		if recovered := recover(); recovered != nil {
			if logger != nil {
				logger.Warn("recall preflight: source panicked", "source", source.name, "panic", recovered)
			}
			if deps.Briefcase {
				deps.recordStrictError(fmt.Errorf("briefcase recall source %s panicked", source.name))
			}
		}
	}()

	start := time.Now()
	result.evidence = source.run(ctx)
	result.elapsed = time.Since(start)
	// Sample at this source's return. A fast source remains complete even when
	// a sibling later exhausts the shared deadline.
	result.truncated = ctx.Err() != nil
	return result
}

func withTopiclessDiaryFallback(
	ctx context.Context,
	evidence []recallEvidence,
	cue bool,
	deps Deps,
	queries []string,
) []recallEvidence {
	if len(evidence) != 0 || !cue || deps.Wiki == nil || len(queries) != 0 {
		return evidence
	}
	return append(evidence, recallDiaryEvidence(ctx, deps.Wiki, queries, true)...)
}

func rankRecallEvidence(
	evidence []recallEvidence,
	queries []string,
	message string,
	cue bool,
	now time.Time,
) []recallEvidence {
	// Lower-precision broadening hits must be demoted before cross-source dedup,
	// which keeps the highest-scored copy of a fact.
	applyBroadeningPenalty(evidence, queries)
	evidence = dedupRecallEvidence(evidence)
	applyProvenancePenalty(evidence)
	evidence = filterCrossSubjectEvidence(evidence, message)

	if temporalRange := parseRecallTemporalRangeAt(message, now); temporalRange.ok {
		for i := range evidence {
			at := evidence[i].At
			if at > 0 && at >= temporalRange.From && at <= temporalRange.To {
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
	if budget := recallEvidenceBudget(cue); len(evidence) > budget {
		return evidence[:budget]
	}
	return evidence
}

// filterCrossSubjectEvidence drops wiki rows whose SubjectID is a non-self
// identity the query does not name (M6). Self/empty subjects always pass.
func filterCrossSubjectEvidence(evidence []recallEvidence, message string) []recallEvidence {
	if len(evidence) == 0 {
		return evidence
	}
	querySubjects := recallQuerySubjects(message)
	out := evidence[:0]
	for _, ev := range evidence {
		if ev.Kind == "wiki" && mem.CrossSubjectBlocked(ev.SubjectID, querySubjects) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// recallQuerySubjects extracts coarse subject tokens from the user message for
// cross-subject gating. Not a NER — path fragments and spaced tokens.
func recallQuerySubjects(message string) []string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Fields(msg) {
		p := strings.Trim(part, `.,!?"'“”`)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// hasCue reports whether a message explicitly asks to recall prior context.
func hasCue(message string) bool {
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

// searchQueries derives bounded evidence-search queries from a user message.
func searchQueries(message string) []string {
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
	return textutil.DedupeStrings(queries)
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
