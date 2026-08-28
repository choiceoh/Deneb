package recall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
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
	// recallMaxEvidence is the evidence budget for an explicit recall cue;
	// silent every-turn auto-recall gets recallAutoMaxEvidence.
	//
	// The auto budget was 4 and is now 8. Four rows was the binding constraint on
	// the whole pipeline: retrieval reached the evidence 93.4% of the time but
	// only 70.9% of it fit (LongMemEval_s, 470 questions, 890 evidence sessions,
	// strict recall@k — the metric the paper reports). Widening the render alone,
	// with reach and rerank window left at their no-cue values, recovers most of
	// that gap for no latency at all: nothing is searched or reranked differently,
	// the ranked list is simply cut later.
	//
	//	auto budget                      recall@k   success@k   block tokens
	//	4                                70.9%      90.9%       1587
	//	8 (reach 2, window 20)           80.7%      93.2%       2263
	//	8 + cue reach 5 + cue window 50  82.8%      94.0%       2263
	//
	// The cost is context, not time: +676 tokens per recall turn, which against
	// the production median first turn (10,658 input tokens, 252 turns over 3
	// days) moves the block from 14.9% to 21.2% of the prompt — and on a small
	// turn (p25, 7,850) from 20.2% to 28.8%. That is the tradeoff being accepted
	// here; the remaining +2.1pp needs the wider cue reach, which stays reserved
	// for turns where the operator actually asked to remember.
	recallMaxEvidence     = 8
	recallAutoMaxEvidence = 8
	// recallBroadeningPenalty multiplies the score of a hit found only by an
	// individual broadening term (not the combined multi-term query), demoting
	// incidental single-common-term matches (e.g. "조직" → an unrelated "조직명"
	// page) below on-topic combined-query hits.
	recallBroadeningPenalty = 0.7
	recallMaxChars          = 6500
	currentFactMaxChars     = 2500
	recallContextOpenTag    = `<recall-context source="server-preflight" trust="untrusted">`
	recallContextCloseTag   = `</recall-context>`
)

// recallEvidenceBudget returns how many evidence rows a turn may carry.
type recallEvidence struct {
	Kind   string
	Source string
	Query  string
	Note   string
	// StaleValue is populated only on internal supersession markers. Those
	// rows are removed before ranking; the value becomes a deny phrase for
	// diary/transcript/file evidence gathered in the same recall.
	StaleValue string
	// Confidence, when set, is the row's own authority label and overrides the
	// score table in recallConfidence. Sources whose rows are a deterministic
	// lookup rather than a ranked guess declare it here — their score is a
	// fixed rank anchor, so no threshold over it can carry information.
	Confidence string
	// noteWide is the 4x lexical window around the same match cluster — input
	// for model-side pruning (rerankPolarisEvidence), never rendered as-is.
	noteWide  string
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
	"기억", "했던", "말했던", "말한", "해둔", "정리했던", "논의했던",
	"이어", "이어서", "계속", "뭐였", "뭐더라",
}

var recallStopWords = map[string]struct{}{
	"기억": {}, "회상": {}, "전에": {}, "저번": {}, "지난번": {}, "예전에": {}, "아까": {}, "방금": {}, "그때": {},
	"말했던": {}, "말한": {}, "했던": {}, "해둔": {}, "정리했던": {}, "논의했던": {}, "이어": {}, "이어서": {}, "계속": {},
	"문맥": {}, "컨텍스트": {}, "뭐였": {}, "뭐더라": {}, "그거": {}, "그": {}, "이": {}, "저": {}, "것": {}, "거": {},
	"좀": {}, "다시": {}, "관련": {}, "쪽": {}, "걸": {}, "를": {}, "을": {}, "은": {}, "는": {}, "이랑": {}, "하고": {},
	// English function words. The list used to hold nine of these, which left
	// "did", "where", "how" and "was" firing as standalone queries — on
	// LongMemEval roughly half of every question's derived queries were function
	// words, and completing the list moved evidence-hit from 64.7% to 76.4%
	// (longmemeval_bench_test.go). Korean-first does not make them harmless:
	// operator messages quote English prose (shared article titles, extracted
	// document text) often enough to matter.
	//
	// These apply unconditionally, which was a measured choice, not a default.
	// The obvious worry is a Korean message where an English keyword IS the
	// subject ("for 루프", "where 절") — dropping it would delete the question.
	// Across 2,081 real Korean operator messages that shape appears 4 times
	// (0.2%), and none of the four are genuine keyword use, while incidental
	// quoted English appears in 4.1%. Gating the list on "message has no
	// Hangul" was built and rejected: it costs the common case to protect a
	// case the corpus does not contain.
	"the": {}, "and": {}, "for": {}, "with": {}, "about": {}, "that": {}, "this": {},
	"what": {}, "when": {}, "where": {}, "which": {}, "who": {}, "whom": {}, "whose": {},
	"how": {}, "why": {}, "did": {}, "does": {}, "was": {}, "were": {}, "are": {},
	"has": {}, "have": {}, "had": {}, "been": {}, "will": {}, "would": {}, "should": {},
	"could": {}, "can": {}, "from": {}, "into": {}, "than": {}, "then": {}, "there": {},
	"their": {}, "some": {}, "any": {}, "all": {}, "get": {}, "got": {},
	"hello": {}, "thanks": {}, "thank": {}, "please": {}, "today": {}, "tomorrow": {}, "yesterday": {},
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

	if params.recallSuppressed() {
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

	// Multiturn context rewrite: an elliptical follow-up ("그 현장 계약 조건은
	// 어떻게 됐지?") carries no searchable subject of its own — prepend the
	// prior user turn so query assembly, anchors, and ranking see a
	// self-contained text (grounding: recall_context_rewrite.go).
	searchMessage := message
	if needsContextRewrite(message) {
		if prior := lastPriorUserTurn(deps, params.SessionKey, message); prior != "" {
			searchMessage = prior + " " + message
		}
	}

	queries := searchQueries(searchMessage)

	sources := buildRecallSources(params, deps, queries, searchMessage)
	collection := runRecallSources(ctx, sources, deps, logger)
	truncated = collection.truncated
	evidence, supersededPageValues := stripSupersededPageMarkers(collection.evidence)
	currentFacts := ""
	currentFactsResolveCue := false
	staleFactValues := supersededPageValues
	var factSnapshot *wiki.FactRecallSnapshot
	var unmatchedSubjectValues []string
	var unmatchedSubjectAliases []factSubjectAlias
	var matchedSubjects map[string]struct{}
	if deps.Wiki != nil {
		// Canonical current facts are a live, typed tail block rather than a
		// session-frozen system-prompt fragment. Filter every legacy source with
		// the same ledger snapshot before ranking, so an old diary/transcript
		// mention cannot outscore and resurrect a corrected value. Typed retired
		// values stay scoped to subject+key; source path/text/query supply identity
		// context when a row has no explicit SubjectID.
		snapshot := deps.Wiki.RecallFactSnapshot()
		factSnapshot = &snapshot
		evidence = filterStaleFactEvidence(evidence, staleFactValues)
		evidence = filterRecallFactLifecycleEvidence(deps.Wiki, searchMessage, evidence, snapshot)
		factRevision, activeFacts := snapshot.Revision, snapshot.Active
		matchedSubjects = explicitlyMatchedFactSubjects(searchMessage, activeFacts)
		unmatchedSubjectValues = unmatchedNonSelfFactValues(activeFacts, matchedSubjects)
		unmatchedSubjectAliases = unmatchedNonSelfFactAliases(activeFacts, matchedSubjects)
		evidence = filterUnmatchedSubjectFactEvidence(evidence, unmatchedSubjectValues)
		evidence = filterUnmatchedSubjectAliases(evidence, unmatchedSubjectAliases, matchedSubjects)
		currentFacts = subjectAwareCurrentFactContext(
			factRevision, activeFacts, matchedSubjects, searchMessage, currentFactMaxChars,
			deps.SelfFactsInSystemPrompt,
		)
		// The cue is computed from the FULL claim set, never from what the block
		// happened to render. Dropping duplicated self claims must not turn a
		// turn that resolves from current facts into a "no evidence" notice plus
		// a recorded recall miss — the accounting is a measured metric.
		currentFactsResolveCue = currentFactsResolveMessage(searchMessage, activeFacts, matchedSubjects)
	}
	// Recent-diary fallback ONLY for topicless cues ("아까 뭐였지?" — no signal
	// terms, so nothing was searchable). A topical question that found nothing
	// must get the honest no-evidence notice, not two unrelated recent diary
	// entries dressed up as recall (the bench caught exactly that). The cue
	// gate matters: a topicless NON-cue turn is smalltalk ("안녕, 오늘 뭐
	// 도와줄 수 있어?"), and silent auto-recall must stay silent there rather
	// than dress recent diary entries up as relevant context.
	evidence = withTopiclessDiaryFallback(ctx, evidence, cue, deps, queries)
	// The topicless fallback is gathered after the normal fan-out. Apply the
	// same deny set again so a vague cue cannot bypass lifecycle filtering via
	// the newest raw diary entries.
	evidence = filterStaleFactEvidence(evidence, staleFactValues)
	if factSnapshot != nil {
		evidence = filterRecallFactLifecycleEvidence(deps.Wiki, searchMessage, evidence, *factSnapshot)
	}
	evidence = filterUnmatchedSubjectFactEvidence(evidence, unmatchedSubjectValues)
	evidence = filterUnmatchedSubjectAliases(evidence, unmatchedSubjectAliases, matchedSubjects)

	// wiki↔메일 담당자 충돌: pull candidates without annotating either side,
	// apply the fact guards, then annotate only surviving pairs. Annotation is
	// filtered once more because the marker itself includes both email values.
	if cue {
		evidence = pullMissingMailAnalysisConflicts(ctx, deps.Wiki, evidence)
	}
	evidence = filterStaleFactEvidence(evidence, staleFactValues)
	if factSnapshot != nil {
		evidence = filterRecallFactLifecycleEvidence(deps.Wiki, searchMessage, evidence, *factSnapshot)
	}
	evidence = filterUnmatchedSubjectFactEvidence(evidence, unmatchedSubjectValues)
	evidence = filterUnmatchedSubjectAliases(evidence, unmatchedSubjectAliases, matchedSubjects)
	evidence = annotateExistingWikiMailConflicts(deps.Wiki, evidence)
	evidence = filterStaleFactEvidence(evidence, staleFactValues)
	if factSnapshot != nil {
		evidence = filterRecallFactLifecycleEvidence(deps.Wiki, searchMessage, evidence, *factSnapshot)
	}
	evidence = filterUnmatchedSubjectFactEvidence(evidence, unmatchedSubjectValues)
	evidence = filterUnmatchedSubjectAliases(evidence, unmatchedSubjectAliases, matchedSubjects)

	if len(evidence) == 0 {
		// The canonical fact plane is itself authoritative recall evidence. A cue
		// that resolves entirely from current facts must not be paired with a
		// contradictory source=none warning or recorded as a recall miss.
		if currentFactsResolveCue {
			if logger != nil {
				logger.Info("recall preflight: current facts only",
					"session", params.SessionKey, "sources", collection.sourceSummary, "truncated", truncated)
			}
			if !deps.Briefcase {
				StoreInjectedPaths(params.SessionKey, nil)
			}
			return currentFacts, truncated
		}
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
			return combineCurrentFactContext(currentFacts, appendRoutingHint(formatRecallNoEvidence(), message, params.SessionKey, logger)), truncated
		}
		return currentFacts, truncated
	}

	evidence = rankRecallEvidence(evidence, queries, searchMessage, cue, deps.now())
	block, budgetDropped := formatRecallEvidenceAt(evidence, deps.now(), params.FilesToolReachable, params.SessionsToolReachable)
	if budgetDropped > 0 {
		// The character budget cut rows the ranking chose. That is the same
		// class of degradation as a deadline cut, so it must reach `truncated`
		// too — otherwise ShouldFreeze pins a partial snapshot onto every later
		// turn about this topic (first-write-wins, no expiry).
		truncated = true
	}
	if logger != nil {
		logger.Info("recall preflight: evidence injected",
			"session", params.SessionKey, "count", len(evidence)-budgetDropped,
			"budgetDropped", budgetDropped,
			"sources", collection.sourceSummary, "truncated", truncated)
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
	return combineCurrentFactContext(currentFacts, appendRoutingHint(block, message, params.SessionKey, logger)), truncated
}

// high-risk winner or conflict cannot fall off the fixed live-context budget.

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
		reranker := deps.Reranker
		sources = append(sources, recallSource{name: "polaris", run: func(ctx context.Context) []recallEvidence {
			return rerankPolarisEvidence(
				ctx, reranker, message, hasCue(message),
				recallPolarisEvidence(ctx, bridge, params.SessionKey, queries, hasCue(message)),
			)
		}})
	} else {
		sources = append(sources, recallSource{name: "transcript", run: func(ctx context.Context) []recallEvidence {
			// The skip-current comparison inside must match the turn's raw
			// message, not the context-rewritten text — otherwise the current
			// message itself would surface as its own evidence.
			return recallTranscriptEvidence(ctx, deps.Transcript, params.SessionKey, params.Message, queries)
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
	return cutToBudgetWithDiversity(evidence, recallEvidenceBudget(cue))
}

// cutToBudgetWithDiversity fills the evidence budget by rank, skipping
// same-conversation near-duplicates (≥70% token overlap), with an optional
// soft per-conversation decay (DENEB_POLARIS_SESSION_DECAY): each row already
// picked from a conversation multiplies that conversation's next candidate by
// the decay factor. Unlike the removed hard cap (≤2 — it categorically evicted
// the third same-conversation row and cost knowledge-update 5.6pp, which needs
// the SAME conversation's latest update row), a decayed row still wins whenever
// its base score carries it: the mechanism only breaks ties toward unrendered
// conversations, which is what strict recall@k (all evidence sessions present)
// needs on multi-evidence questions. The first pick is provably unaffected.
// Overflow slots fall back to pure rank order, so a turn whose evidence
// genuinely lives in one conversation still fills its budget.
func cutToBudgetWithDiversity(evidence []recallEvidence, budget int) []recallEvidence {
	if len(evidence) <= budget {
		return evidence
	}
	perSession := make(map[string]int)
	sessionOf := func(ev recallEvidence) string {
		if ev.Kind != "session" {
			return ""
		}
		if i := strings.LastIndex(ev.Source, "#"); i > 0 {
			return ev.Source[:i]
		}
		return ev.Source
	}
	picked := make([]recallEvidence, 0, budget)
	pickedTokens := make([]map[string]struct{}, 0, budget)
	used := make([]bool, len(evidence))
	noteTokens := func(note string) map[string]struct{} {
		out := make(map[string]struct{})
		for _, t := range splitRecallTokens(note) {
			out[t] = struct{}{}
		}
		return out
	}
	// Near-duplicate skip applies ONLY within the same conversation. The first
	// version compared across sessions and collapsed retrieval outright
	// (rendered recall@4 80.7% → 34.9%): model pruning rewrites every relevant
	// row into the same question-shaped sentences, so cross-session evidence
	// rows — each carrying a DISTINCT fact ("visited the ENT", "visited the
	// dermatologist") — read as duplicates of each other, got skipped, and the
	// budget filled with low-rank chatter. hit@8 stayed at 94.5% the whole
	// time, because one surviving evidence row satisfies it — the damage was
	// invisible outside strict recall.
	pickedSessions := make([]string, 0, budget)
	overlaps := func(tokens map[string]struct{}, session string) bool {
		for pi, prev := range pickedTokens {
			if session == "" || pickedSessions[pi] != session {
				continue
			}
			if len(tokens) == 0 || len(prev) == 0 {
				continue
			}
			shared := 0
			for t := range tokens {
				if _, ok := prev[t]; ok {
					shared++
				}
			}
			smaller := len(tokens)
			if len(prev) < smaller {
				smaller = len(prev)
			}
			if smaller > 0 && float64(shared)/float64(smaller) >= 0.7 {
				return true
			}
		}
		return false
	}
	// Greedy argmax over decay-adjusted scores. At decay=1 (default) this IS
	// the plain rank-order walk: evidence arrives sorted by score descending
	// and strict > keeps the earliest maximum. The per-session hard cap (≤2)
	// that used to live here is REMOVED — see the function comment.
	decay := polarisSessionDecay()
	blocked := make([]bool, len(evidence))
	for len(picked) < budget {
		best, bestAdj := -1, 0.0
		for i := range evidence {
			if used[i] || blocked[i] {
				continue
			}
			adj := evidence[i].Score
			if decay < 1 {
				if key := sessionOf(evidence[i]); key != "" && perSession[key] > 0 {
					adj *= math.Pow(decay, float64(perSession[key]))
				}
			}
			if best == -1 || adj > bestAdj {
				best, bestAdj = i, adj
			}
		}
		if best == -1 {
			break
		}
		key := sessionOf(evidence[best])
		tokens := noteTokens(evidence[best].Note)
		if overlaps(tokens, key) {
			blocked[best] = true
			continue
		}
		used[best] = true
		picked = append(picked, evidence[best])
		pickedTokens = append(pickedTokens, tokens)
		pickedSessions = append(pickedSessions, key)
		if key != "" {
			perSession[key]++
		}
	}
	for i := range evidence { // fall back to rank order for any unfilled slots
		if len(picked) == budget {
			break
		}
		if !used[i] {
			picked = append(picked, evidence[i])
		}
	}
	return picked
}

// polarisSessionDecay reads DENEB_POLARIS_SESSION_DECAY (0.5..1.0); 1 = off.
// Default 0.7 — the knee of the measured ladder (LongMemEval_s 470, strict
// recall@8 = coverage of the 8 rendered rows; everything else invariant):
//
//	decay   strict@4   strict@8   KU hit@4/top1
//	1 (off) 80.2%      83.4%      97.2 / 88.9
//	0.85    80.2%      85.4%      97.2 / 88.9
//	0.7     80.6%      86.4%      97.2 / 88.9   ← default
//	0.5     80.8%      86.6%      97.2 / 88.9
//
// hit@4 93.2 / top1 62.1 at every rung, and knowledge-update — the category
// the removed HARD cap damaged 5.6pp — is untouched even at 0.5: demotion
// only flips near-ties, eviction flipped certainties. 0.5's extra +0.2 is not
// worth living at the clamp edge.
func polarisSessionDecay() float64 {
	if raw := strings.TrimSpace(os.Getenv("DENEB_POLARIS_SESSION_DECAY")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0.5 && v <= 1.0 {
			return v
		}
	}
	return 0.7
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
// recallMaxQueries bounds how many derived queries a turn issues — the single
// largest retrieval lever measured in this pipeline, and for a long time the one
// nobody was looking at. Every source shares this list, so a term cut here is a
// term that is never searched anywhere.
//
// LongMemEval_s (470 questions) against the Korean gold probe (198 cases,
// recall.Build end to end, which is the only harness that exercises this
// constant — cmd/recall-bench feeds gold questions straight to the wiki store
// and never derives queries at all):
//
//	queries  EN hit@4   EN hit@8   KO gold-in-block   KO p90   KO max
//	6        81.7%      88.7%      93.9%              644ms    840ms
//	7        87.9%      93.2%      94.4%              671ms    854ms
//	8        90.9%      94.0%      94.4%              665ms    840ms   <- current
//	10       91.9%      95.3%      94.4%              712ms    1554ms
//
// 8 takes 91% of what 10 offers at no measurable Korean cost — its tail matches
// the old default exactly. 10 is where the tail breaks: 1554ms exceeds
// recallPreflightTimeout, and a turn that trips the shared deadline loses whole
// sources and must not freeze its snapshot. Unlike the cross-session quota and
// the rerank window this is NOT cue-routed: the gain lands on the 4-row budget
// too, so routing it would forfeit exactly what it buys.
const recallMaxQueries = 8

func recallMaxQueriesValue() int {
	if raw := strings.TrimSpace(os.Getenv("DENEB_RECALL_MAX_QUERIES")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 30 {
			return v
		}
	}
	return recallMaxQueries
}

func searchQueries(message string) []string {
	terms := recallSignalTerms(message)
	var queries []string
	if len(terms) >= 2 {
		queries = append(queries, strings.Join(terms[:minInt(4, len(terms))], " "))
	}
	for _, term := range terms {
		queries = append(queries, term)
		if len(queries) >= recallMaxQueriesValue() {
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
