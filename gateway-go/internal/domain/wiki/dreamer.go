// dreamer.go — WikiDreamer: implements autonomous.Dreamer for wiki-based
// memory consolidation. Instead of SQL-based fact verification/merging,
// it scans diary entries and synthesizes them into wiki pages via LLM.
package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
)

// Dreaming configuration.
const (
	wikiDreamTurnThreshold = 50
	wikiDreamTimeIntervalH = 8
	// wikiDreamPrefMinInterval is the accelerated cadence while a
	// preference-tagged diary capsule (신호:선호 — chat's isPreferenceDirective)
	// awaits consolidation: a voiced standing preference should land on the
	// 사용자 pages within the hour, not after the regular 50-turn/8h batch
	// window. The floor keeps back-to-back preference turns from dreaming
	// every turn.
	wikiDreamPrefMinInterval = 30 * time.Minute
	// Cloud synthesis is intentionally allowed well beyond the old six-minute
	// retry wall. GLM's non-streaming response can spend several minutes on a
	// large wiki batch before returning headers; leave a second half-cycle for
	// critique, verification, snapshots, and notification.
	wikiDreamTimeout = 30 * time.Minute
	// wikiDreamSynthesisTimeout bounds the synthesis LLM call alone. Fifteen
	// minutes admits a healthy long cloud generation while still reserving half
	// of the cycle for downstream phases if the backend is actually wedged.
	wikiDreamSynthesisTimeout = 15 * time.Minute
	// wikiDreamMaxTokens sizes the synthesis answer. 4096 was tuned in the
	// qwen-lightweight era and truncated mid-JSON once dsv4 (thinking off)
	// started emitting full page bodies for a multi-day diary backlog — the
	// parser then failed the whole cycle. 8192 covers the observed batches;
	// the salvage parser (parseWikiUpdates) keeps a truncated tail from
	// zeroing the cycle regardless.
	wikiDreamMaxTokens    = 8192
	diaryProcessStateFile = ".diary-process-state.json"
	dreamProposalFile     = ".dream-last-proposal.json"
	// processedCapsuleLimit bounds the capsule slice the SYNTHESIS PROMPT sees
	// (formatProcessedDiaryCapsules) — recent context, kept small on purpose.
	processedCapsuleLimit = 12
	// scoredCapsuleLimit bounds capsule STORAGE. The utility axis judges pages
	// from prior capsules, and at 12 retained cycles (~4 days at the live
	// cadence) its denominator was 1–8 pages — the score swung 0.33↔0.88 on
	// single-page noise, useless to a slow-loop tuner. ~90 retains the full
	// utilityWindow (30d at ~3 cycles/day); the age cut in computeDreamQuality
	// is the real boundary, this is its storage backstop.
	scoredCapsuleLimit = 90
	// Transient-failure retry: a synthesis LLM call that dies on transport
	// (wormhole timeout, deploy hot-swap canceling the context) used to back
	// off the full 8h interval — 12 of the 14 synthesis failures in the week
	// of 2026-07-20 were this class, each costing a cycle. Such failures now
	// hold ShouldDream for a short delay and retry, escalating to the full
	// backoff after wikiDreamTransientRetryMax consecutive misses so a wedged
	// backend still cannot hot-loop.
	wikiDreamTransientRetryDelay = 30 * time.Minute
	wikiDreamTransientRetryMax   = 2
)

// errSynthesisLLMCall marks a synthesis failure where the LLM call itself
// died (transport/backend), as opposed to a parse failure on a delivered
// response. Call failures are transient by nature — nothing was consumed —
// so the cycle retries on the short delay instead of a full interval.
var errSynthesisLLMCall = errors.New("synthesis LLM call")

// Compile-time interface compliance.
var _ autonomous.Dreamer = (*WikiDreamer)(nil)

type diaryScanResult struct {
	Content    string
	State      diaryProcessState
	LatestDate string
	// PriorFiles is the offset ledger BEFORE this scan advanced it — restored
	// by RunDream when a partial (salvaged) synthesis leaves tail content
	// unconsumed, so the next cycle re-reads it.
	PriorFiles map[string]diaryFileState
	// MorePending is true when this scan stopped at the per-cycle byte cap with
	// unconsumed diary bytes still on disk. The autonomous service drains that
	// remainder with a near-term re-trigger instead of waiting the full 8h
	// interval, so a busy backlog lands in the wiki in minutes, not days.
	MorePending bool
}

type diaryProcessState struct {
	Version   int                       `json:"version"`
	Files     map[string]diaryFileState `json:"files"`
	Recent    []processedDiaryCapsule   `json:"recent,omitempty"`
	UpdatedAt string                    `json:"updatedAt,omitempty"`
	// PartialStreak counts consecutive cycles whose synthesis array was
	// damaged and only partially salvaged. Offsets are held (re-consumed) for
	// up to two such cycles; a third advances anyway so a deterministic
	// corruption cannot pin the pipeline to the same chunk forever.
	PartialStreak int `json:"partialStreak,omitempty"`
	// LastDreamMs is the unix-millis time of the last dream cycle, persisted so
	// the 8h time-trigger survives gateway restarts (which happen every few
	// minutes). Without it, in-memory lastDream reset to zero on every boot and
	// dreaming never fired.
	LastDreamMs int64 `json:"lastDreamMs,omitempty"`
	// MemoryConsumedThrough is the high-water stamp ("YYYY-MM-DD HH:MM") of
	// workspace MEMORY.md sections already distilled into the wiki. Sections
	// at or before this stamp may be dropped from MEMORY.md once they age out
	// of the keep window (see memory_curation.go).
	MemoryConsumedThrough string `json:"memoryConsumedThrough,omitempty"`
}

type diaryFileState struct {
	Offset  int64 `json:"offset"`
	Size    int64 `json:"size,omitempty"`
	ModUnix int64 `json:"modUnix,omitempty"`
}

type processedDiaryCapsule struct {
	At        string   `json:"at"`
	DiaryDate string   `json:"diaryDate,omitempty"`
	Proposed  int      `json:"proposed"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Paths     []string `json:"paths,omitempty"`
}

type dreamProposalReport struct {
	GeneratedAt     string               `json:"generatedAt"`
	LatestDiaryDate string               `json:"latestDiaryDate,omitempty"`
	DiaryBytes      int                  `json:"diaryBytes"`
	Proposed        []dreamUpdatePreview `json:"proposed"`
	Applied         dreamApplySummary    `json:"applied,omitempty"`
	PhaseErrors     []string             `json:"phaseErrors,omitempty"`
	DurationMs      int64                `json:"durationMs,omitempty"`
}

type dreamUpdatePreview struct {
	Action      string   `json:"action"`
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Category    string   `json:"category,omitempty"`
	Type        string   `json:"type,omitempty"`
	Confidence  string   `json:"confidence,omitempty"`
	Importance  float64  `json:"importance,omitempty"`
	Related     []string `json:"related,omitempty"`
	ContentHint string   `json:"contentHint,omitempty"`
}

type dreamApplySummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// WikiDreamer implements autonomous.Dreamer for wiki-based knowledge consolidation.
// Phases:
//  1. Scan unprocessed diary entries
//  2. LLM synthesis: identify which wiki pages to create/update
//  3. Apply page updates
//  4. Rebuild index
type WikiDreamer struct {
	store  *Store
	config Config
	client *llm.Client
	model  string
	logger *slog.Logger

	// cmu guards turnCount, lastDream and prefSignals: incremented from chat
	// turns, read from the autonomous dream timer loop, reset from async dream
	// runs — three goroutines on plain ints/time without it.
	cmu       sync.Mutex
	turnCount int
	lastDream time.Time
	// prefSignals counts preference-tagged diary capsules (신호:선호) recorded
	// since the last dream; >0 switches ShouldDream onto the accelerated
	// wikiDreamPrefMinInterval cadence so voiced preferences consolidate into
	// the 사용자 pages promptly. In-memory only: a restart loses the nudge, but
	// the capsule itself is already on disk and the regular thresholds still
	// pick it up.
	prefSignals int
	// synthRetryNotBefore holds ShouldDream after a transient synthesis
	// failure (see errSynthesisLLMCall); synthTransientFails counts the
	// consecutive misses that escalate to a full-interval backoff. In-memory
	// on purpose: when a deploy hot-swap kills the synthesis call, the fresh
	// process retries on its first tick (lastDream was not advanced).
	synthRetryNotBefore time.Time
	synthTransientFails int

	// polarisContextFn optionally returns formatted recent polaris compression
	// summaries to inject into the synthesis prompt as a higher-density fact
	// source alongside raw diary entries. Wired by the chat pipeline; the wiki
	// package does not import polaris directly.
	polarisContextFn func() string

	// rulesEvolve arms the RHI self-comparison + rules-revision lane
	// (dreamer_selfcompare.go). Off by default; the server enables it only for
	// the production state dir.
	rulesEvolve bool

	// workspaceDir is the agent workspace containing MEMORY.md. Empty disables
	// memory curation (see memory_curation.go).
	workspaceDir string

	// openLoopSink receives unfulfilled commitments extracted each cycle
	// (see open_loops.go). nil disables the extraction pass.
	openLoopSink func(ctx context.Context, loops []OpenLoop) (int, error)

	// personDirectory supplies the address-book snapshot for mention-driven
	// 인물 page seeding (see person_seed.go). nil disables seeding.
	personDirectory func() []PersonSeed

	// llmExtraBody is merged into every dreamer LLM request (synthesis,
	// verify, open-loops, project-digest). The chat pipeline wires it with
	// the selected model's thinking-off shaping: the dreamer calls the
	// raw client — not pilot/localai, not the chat effort router — so
	// without this a dual-mode reasoning model (deepseek-v4) spends the
	// whole output budget on chain-of-thought and returns empty content
	// (observed: 2026-07-02/03 synthesis failures, reasoning_chars≈13K vs
	// MaxTokens 4096). nil = no shaping (previous behavior).
	llmExtraBody jsonObject

	// synthesisMaxTokens overrides wikiDreamMaxTokens for the synthesis call
	// (0 = default). Set for reasoning models with no thinking off-switch,
	// where the budget must fit chain-of-thought + the JSON answer.
	synthesisMaxTokens int
}

// NewWikiDreamer creates a new wiki dreamer.
func NewWikiDreamer(store *Store, client *llm.Client, model string, cfg Config, logger *slog.Logger) *WikiDreamer {
	wd := &WikiDreamer{
		store:  store,
		config: cfg,
		client: client,
		model:  model,
		logger: logger,
	}
	// Restore lastDream from persisted state so the 8h time-trigger survives
	// gateway restarts. Without this, lastDream stayed zero on every boot,
	// ShouldDream's IsZero guard blocked the time path, and turnCount (also reset
	// on restart) rarely reached its threshold — dreaming was dead for ~26 days.
	// On the first run (no persisted value), seed lastDream=now and persist so
	// the interval starts counting from boot instead of staying zero forever.
	if store != nil {
		state := wd.loadDiaryProcessState()
		if state.LastDreamMs > 0 {
			wd.lastDream = time.UnixMilli(state.LastDreamMs)
		} else {
			wd.lastDream = time.Now()
			state.LastDreamMs = wd.lastDream.UnixMilli()
			if err := wd.saveDiaryProcessState(state); err != nil && logger != nil {
				logger.Warn("wiki-dream: seed lastDream failed", "error", err)
			}
		}
	}
	return wd
}

// IncrementTurn records a conversation turn for threshold tracking.
func (wd *WikiDreamer) IncrementTurn(_ context.Context) {
	wd.cmu.Lock()
	wd.turnCount++
	wd.cmu.Unlock()
}

// NotePreferenceSignal records that a preference-tagged diary capsule
// (신호:선호) was just appended. Wired from the chat diary recorder — the
// capsule is on disk before this is called, so an accelerated dream cycle
// always sees the preference it fires for.
func (wd *WikiDreamer) NotePreferenceSignal() {
	wd.cmu.Lock()
	wd.prefSignals++
	wd.cmu.Unlock()
}

// SetPolarisContextFn wires a closure that returns formatted recent polaris
// compression summaries. nil-safe; passing nil disables polaris injection.
func (wd *WikiDreamer) SetPolarisContextFn(fn func() string) {
	wd.polarisContextFn = fn
}

// SetWorkspaceDir wires the workspace directory so dream cycles can consume
// and curate the auto-recorded MEMORY.md (see memory_curation.go). Empty
// disables memory curation.
func (wd *WikiDreamer) SetWorkspaceDir(dir string) {
	wd.workspaceDir = dir
}

// SetLLMRequestShape installs the request shaping applied to every dreamer
// LLM call: extraBody (typically the model's thinking-off
// chat_template_kwargs — see the llmExtraBody field doc) and an optional
// synthesis MaxTokens override (0 keeps wikiDreamMaxTokens; used to budget
// chain-of-thought on reasoning models that cannot switch it off). Call
// before the first dream cycle; the wiring lives in server/chat_pipeline.go.
func (wd *WikiDreamer) SetLLMRequestShape(extraBody jsonObject, synthesisMaxTokens int) {
	wd.llmExtraBody = extraBody
	wd.synthesisMaxTokens = synthesisMaxTokens
}

// llmRequest builds the dreamer's standard one-shot chat request with the
// shared shaping applied — the single construction point for the synthesis,
// verify, open-loops, and project-digest calls so none can drift back to an
// unshaped request.
func (wd *WikiDreamer) llmRequest(system, prompt string, maxTokens int) llm.ChatRequest {
	// Headroom mode (untoggleable reasoning model: no off-switch kwargs, a
	// raised synthesis budget): every call pays chain-of-thought before the
	// answer, so the small fixed budgets of the non-synthesis calls (verify,
	// open-loops, digests) get the same 4x scaling the synthesis budget got —
	// otherwise those phases keep failing on exactly the models the headroom
	// exists for. The synthesis call itself passes synthesisBudget() and is
	// excluded by the < guard.
	if wd.llmExtraBody == nil && wd.synthesisMaxTokens > 0 && maxTokens < wd.synthesisMaxTokens {
		maxTokens *= 4
	}
	systemJSON, _ := json.Marshal(system)
	req := llm.ChatRequest{
		Model:     wd.model,
		System:    llm.FlexibleFromRaw(systemJSON),
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: maxTokens,
		ExtraBody: flexibleExtraBody(wd.llmExtraBody),
	}
	// A headroom-only shape means the model emits reasoning but has no
	// chat-template off-switch. These dreamer calls are bounded extraction and
	// synthesis jobs, so ask the transport to disable thinking explicitly while
	// retaining the larger budget as a fallback for models that cannot truly
	// switch it off. On GLM behind wormhole this becomes reasoning_effort=low,
	// which wormhole translates to thinking.type=disabled; without it GLM spent
	// the entire budget on reasoning and returned empty content.
	if wd.llmExtraBody == nil && wd.synthesisMaxTokens > 0 {
		req.Thinking = &llm.ThinkingConfig{Type: "disabled"}
	}
	return req
}

// synthesisBudget returns the synthesis call's MaxTokens (override or default).
func (wd *WikiDreamer) synthesisBudget() int {
	if wd.synthesisMaxTokens > 0 {
		return wd.synthesisMaxTokens
	}
	return wikiDreamMaxTokens
}

// ShouldDream checks if dreaming conditions are met.
func (wd *WikiDreamer) ShouldDream(_ context.Context) bool {
	wd.cmu.Lock()
	turns := wd.turnCount
	last := wd.lastDream
	prefs := wd.prefSignals
	hold := wd.synthRetryNotBefore
	wd.cmu.Unlock()

	// A transient synthesis failure holds every trigger (turns included —
	// active chatting must not hot-loop a dead LLM backend) until the retry
	// delay passes.
	if time.Now().Before(hold) {
		return false
	}

	if turns >= wikiDreamTurnThreshold {
		wd.logger.Info("wiki-dream: turn threshold reached", "turns", turns)
		return true
	}
	if !last.IsZero() && time.Since(last).Hours() >= float64(wikiDreamTimeIntervalH) {
		wd.logger.Info("wiki-dream: time threshold reached", "elapsed", time.Since(last).Round(time.Minute))
		return true
	}
	// Preference fast path: an unconsumed 신호:선호 capsule shortens the wait to
	// wikiDreamPrefMinInterval so the 사용자 model updates soon after the user
	// voices a standing preference (fired by the next turn or the dream timer).
	if prefs > 0 && !last.IsZero() && time.Since(last) >= wikiDreamPrefMinInterval {
		wd.logger.Info("wiki-dream: preference-signal threshold reached",
			"prefSignals", prefs, "elapsed", time.Since(last).Round(time.Minute))
		return true
	}
	return false
}

type dreamCycle struct {
	startedAt   time.Time
	report      *autonomous.DreamReport
	phaseErrors []string
	scan        *diaryScanResult
	memoryScan  *memoryScanResult
	synthInput  string
	// episodeRef is this cycle's provenance token (diary date + digest of the
	// full consumed batch), stamped on every page any write path touches so a
	// fact can be traced back to the source span that produced it.
	episodeRef string
	updates    []wikiUpdate
	partial    bool
	proposal   dreamProposalReport
	// prevProposal is the last cycle's persisted report, loaded before this
	// cycle's save overwrites it — the self-comparison anchor (rulesEvolve
	// lane only; nil otherwise).
	prevProposal *dreamProposalReport
	created      int
	updated      int
	userPages    int
	// appliedPaths are the pages this cycle actually wrote (created or
	// updated). The processed-capsule history records THESE, not the proposed
	// paths — a dropped proposal never existed on disk, and recording it would
	// poison the quality score's utility denominator with phantom pages that
	// can never be recalled (measured 2026-07-19: AR-glasses proposals dropped
	// by guards still dragged utility to 0).
	appliedPaths []string
}

func newDreamCycle() *dreamCycle {
	return &dreamCycle{
		startedAt: time.Now(),
		report:    &autonomous.DreamReport{},
	}
}

func (cycle *dreamCycle) addPhaseError(format string, args ...any) {
	cycle.phaseErrors = append(cycle.phaseErrors, fmt.Sprintf(format, args...))
}

// RunDream executes the wiki consolidation cycle as named, independently
// observable phases. Each phase owns one failure policy while this method owns
// only ordering and early exits.
func (wd *WikiDreamer) RunDream(ctx context.Context) (*autonomous.DreamReport, error) {
	ctx, cancel := context.WithTimeout(ctx, wikiDreamTimeout)
	defer cancel()
	cycle := newDreamCycle()

	wd.collectDreamSources(ctx, cycle)
	if cycle.synthInput == "" {
		return wd.finishDreamWithoutInput(cycle), nil
	}

	if !wd.synthesizeDreamCycle(ctx, cycle) {
		return cycle.report, nil
	}

	wd.applyDreamUpdates(ctx, cycle)
	wd.captureDreamOpenLoops(ctx, cycle)
	wd.captureDreamThemes(ctx, cycle)
	wd.captureDreamSelfComparison(ctx, cycle)
	wd.seedDreamPersonPages(ctx, cycle)
	wd.applyDreamProjectDigests(ctx, cycle)
	wd.applyDreamUserDirectives(cycle)

	wd.rebuildAndVerifyDreamWiki(ctx, cycle)
	wd.enrichDreamRelatedLinks(ctx)
	wd.writeDreamGraphSnapshot(ctx, cycle)

	heldOffsets := wd.applyDreamPartialBackpressure(cycle)
	wd.curateDreamMemory(cycle, heldOffsets)
	wd.scoreDreamCycle(cycle)
	// Signal remaining backlog for a near-term drain, but NOT while backpressure
	// holds cursors — re-running immediately would just re-consume the same
	// damaged chunk and hot-loop. A held cycle waits for the normal cadence.
	cycle.report.MoreBacklog = !heldOffsets && cycle.scan != nil && cycle.scan.MorePending
	wd.persistDreamProgress(cycle, heldOffsets)

	wd.completeDreamCycle(ctx, cycle)
	return cycle.report, nil
}

// scoreDreamCycle grades this cycle's output against the recall-utility ledger
// and records the score on the report. Runs BEFORE persistDreamProgress appends
// the current capsule, so the utility axis judges only PRIOR cycles' pages —
// which have had a chance to be recalled. Also compacts the ledger (maintenance)
// so it cannot grow unbounded. Advisory: a scoring failure never fails the cycle.
func (wd *WikiDreamer) scoreDreamCycle(cycle *dreamCycle) {
	now := time.Now()
	if dropped, err := wd.store.compactRecallHits(now); err != nil {
		cycle.addPhaseError("recall-hits-compact: %v", err)
	} else if dropped > 0 {
		wd.logger.Info("wiki-dream: recall-hit ledger compacted", "dropped", dropped)
	}
	// The demand ledger (unanswered cue turns) rides the same maintenance step.
	if dropped, err := wd.store.compactRecallMisses(now); err != nil {
		cycle.addPhaseError("recall-misses-compact: %v", err)
	} else if dropped > 0 {
		wd.logger.Info("wiki-dream: demand ledger compacted", "dropped", dropped)
	}
	// Surface standing demand so the operator sees what the wiki keeps failing
	// to answer — the research lane consumes the same terms for targeting.
	if terms := wd.store.RecallDemandTerms(now, 5); len(terms) > 0 {
		cycle.report.RecallDemandTerms = terms
	}

	var priorCapsules []processedDiaryCapsule
	if cycle.scan != nil {
		priorCapsules = cycle.scan.State.Recent
	}
	q := computeDreamQuality(dreamQualityInputs{
		proposed:   cycle.report.WikiUpdatesProposed,
		applied:    cycle.created + cycle.updated,
		updates:    cycle.updates,
		priorPaths: priorCapsules,
		recalls:    wd.store.RecallUsageScoreCounts(now),
		now:        now,
	})
	cycle.report.QualityScore = q.Score
	cycle.report.RecallHitPages = q.RecalledPages
	if q.Signals > 0 {
		wd.logger.Info("wiki-dream: quality",
			"score", int(q.Score+0.5),
			"precision", round2(q.Precision),
			"confidence", round2(q.Confidence),
			"utility", round2(q.Utility),
			"recalledPages", q.RecalledPages,
			"signals", q.Signals)
	}
}

// round2 rounds a 0–1 axis to two decimals for tidy log lines.
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func (wd *WikiDreamer) collectDreamSources(ctx context.Context, cycle *dreamCycle) {
	scan, err := wd.scanDiaries(ctx)
	if err != nil {
		cycle.addPhaseError("diary-scan: %v", err)
	}
	if scan == nil {
		// A state-bearing scan lets MEMORY.md-only cycles share the normal tail.
		scan = &diaryScanResult{State: wd.loadDiaryProcessState()}
	}
	cycle.scan = scan

	if dropped, err := wd.enforceMemoryDiskCap(); err != nil {
		cycle.addPhaseError("memory-disk-cap: %v", err)
	} else if dropped > 0 {
		wd.logger.Info("wiki-dream: MEMORY.md disk-capped", "droppedSections", dropped)
	}

	cycle.memoryScan = wd.scanWorkspaceMemory(scan.State.MemoryConsumedThrough)
	cycle.synthInput = scan.Content
	if cycle.memoryScan != nil {
		cycle.synthInput += cycle.memoryScan.Content
		wd.logger.Info("wiki-dream: memory sections queued for distillation",
			"sections", cycle.memoryScan.Sections,
			"through", cycle.memoryScan.ConsumedThrough)
	}
	// One episode ref for the whole cycle, minted once synthInput is assembled,
	// so every write path this cycle drives (synthesis, person seeds, project
	// digests) stamps the SAME provenance token. The digest covers the full
	// consumed batch (diary + MEMORY.md); the date is the latest diary date as
	// a coarse locator (see newEpisodeRef).
	cycle.episodeRef = newEpisodeRef(cycle.scan.LatestDate, cycle.synthInput)
}

func (wd *WikiDreamer) finishDreamWithoutInput(cycle *dreamCycle) *autonomous.DreamReport {
	wd.logger.Info("wiki-dream: no new diary or memory entries to process")
	wd.resetCounters()
	cycle.report.DurationMs = time.Since(cycle.startedAt).Milliseconds()
	return cycle.report
}

func (wd *WikiDreamer) synthesizeDreamCycle(ctx context.Context, cycle *dreamCycle) bool {
	if wd.client == nil {
		cycle.phaseErrors = append(cycle.phaseErrors, "synthesis: LLM client not available")
		wd.finishFailedDreamSynthesis(cycle)
		return false
	}

	updates, partial, err := wd.synthesize(ctx, cycle.synthInput, cycle.scan.State)
	if err != nil {
		if errors.Is(err, errSynthesisLLMCall) && wd.noteTransientSynthesisFailure() {
			wd.logger.Error("wiki-dream: synthesis failed; retrying after short delay",
				"error", err, "retryIn", wikiDreamTransientRetryDelay)
			cycle.addPhaseError("synthesis (transient): %v", err)
			wd.finishTransientDreamSynthesis(cycle)
			return false
		}
		wd.logger.Error("wiki-dream: synthesis failed; backing off one interval", "error", err)
		cycle.addPhaseError("synthesis: %v", err)
		wd.finishFailedDreamSynthesis(cycle)
		return false
	}
	wd.clearTransientSynthesisFailures()
	// WikiUpdatesProposed counts what synthesis proposed, BEFORE the critique —
	// so the quality precision axis (applied/proposed) reflects both the offline
	// critique and the apply guards.
	cycle.report.WikiUpdatesProposed = len(updates)

	// Offline self-critique (P3): a second, cheap model pass drops proposals that
	// duplicate the index or add no knowledge, before they reach the store.
	updates, dropped := wd.critiqueUpdates(ctx, updates)
	cycle.report.CritiqueDropped = dropped

	cycle.updates = updates
	cycle.partial = partial
	cycle.proposal = buildDreamProposalReport(cycle.scan, updates)
	// Stash the PREVIOUS cycle's report before the save below overwrites it —
	// the self-comparison pass judges this cycle against it (RHI trajectory-
	// local anchor, dreamer_selfcompare.go).
	if wd.rulesEvolve {
		cycle.prevProposal = wd.loadPrevDreamProposal()
	}
	cycle.report.WikiProposalPath = wd.dreamProposalPath()
	if err := wd.saveDreamProposalReport(cycle.proposal); err != nil {
		cycle.addPhaseError("proposal-save: %v", err)
	}
	return true
}

// loadPrevDreamProposal reads the last cycle's persisted proposal report; nil
// when absent or unreadable (first cycle, rotated state).
func (wd *WikiDreamer) loadPrevDreamProposal() *dreamProposalReport {
	data, err := os.ReadFile(wd.dreamProposalPath())
	if err != nil {
		return nil
	}
	var report dreamProposalReport
	if json.Unmarshal(data, &report) != nil {
		return nil
	}
	return &report
}

func (wd *WikiDreamer) finishFailedDreamSynthesis(cycle *dreamCycle) {
	// Back off a full interval so a missing or wedged LLM cannot hot-loop.
	wd.resetCounters()
	cycle.report.PhaseErrors = cycle.phaseErrors
	cycle.report.DurationMs = time.Since(cycle.startedAt).Milliseconds()
}

// finishTransientDreamSynthesis records the failure WITHOUT consuming the
// cycle's triggers: turn count, pref signals and lastDream stay so the retry
// (gated by synthRetryNotBefore) re-fires on the same accumulated demand.
func (wd *WikiDreamer) finishTransientDreamSynthesis(cycle *dreamCycle) {
	cycle.report.PhaseErrors = cycle.phaseErrors
	cycle.report.DurationMs = time.Since(cycle.startedAt).Milliseconds()
}

// noteTransientSynthesisFailure arms the short-delay retry. It returns false
// once the consecutive-failure budget is spent — the caller then falls back
// to the full-interval backoff — and resets the streak so the next interval
// starts with a fresh budget.
func (wd *WikiDreamer) noteTransientSynthesisFailure() bool {
	wd.cmu.Lock()
	defer wd.cmu.Unlock()
	wd.synthTransientFails++
	if wd.synthTransientFails > wikiDreamTransientRetryMax {
		wd.synthTransientFails = 0
		return false
	}
	wd.synthRetryNotBefore = time.Now().Add(wikiDreamTransientRetryDelay)
	return true
}

func (wd *WikiDreamer) clearTransientSynthesisFailures() {
	wd.cmu.Lock()
	wd.synthTransientFails = 0
	wd.synthRetryNotBefore = time.Time{}
	wd.cmu.Unlock()
}

func (wd *WikiDreamer) applyDreamUpdates(ctx context.Context, cycle *dreamCycle) {
	// The cycle's episode ref ties every page this cycle writes back to the
	// diary span synthesis consumed — deterministic, so no LLM cooperation is
	// needed and provenance can't be hallucinated.
	created, updated, userPages, oversized, appliedPaths := wd.applyUpdates(ctx, cycle.updates, cycle.episodeRef)
	cycle.created = created
	cycle.updated = updated
	cycle.appliedPaths = appliedPaths
	cycle.userPages = userPages
	cycle.report.WikiPagesCreated = created
	cycle.report.WikiPagesUpdated = updated
	cycle.report.UserModelUpdated = userPages
	if len(oversized) > 0 {
		cycle.addPhaseError("oversized pages: %s", strings.Join(oversized, ", "))
	}
}

// auxMinInputBytes gates the synthInput-consuming auxiliary LLM passes (open
// loops, project digests): below this the input is a diary header plus a word or
// two (e.g. a bare "안녕" that synthesis already discards), which cannot yield a
// commitment or a project-status digest — so the model call is pure waste. Set
// deliberately low so no substantive-but-short note is ever skipped; real cycles
// carry hundreds of bytes and always pass.
const auxMinInputBytes = 64

// auxInputTooSmall reports whether synthInput is too trivial to be worth an
// auxiliary LLM extraction pass.
func auxInputTooSmall(synthInput string) bool {
	return len(strings.TrimSpace(synthInput)) < auxMinInputBytes
}

func (wd *WikiDreamer) captureDreamOpenLoops(ctx context.Context, cycle *dreamCycle) {
	if wd.openLoopSink == nil || auxInputTooSmall(cycle.synthInput) {
		return
	}
	loops, err := wd.extractOpenLoops(ctx, cycle.synthInput)
	if err != nil {
		cycle.addPhaseError("open-loops: %v", err)
		return
	}
	if len(loops) == 0 {
		return
	}
	added, err := wd.openLoopSink(ctx, loops)
	if err != nil {
		cycle.addPhaseError("open-loops-sink: %v", err)
		return
	}
	if added > 0 {
		wd.logger.Info("wiki-dream: open loops captured", "extracted", len(loops), "new", added)
	}
}

func (wd *WikiDreamer) seedDreamPersonPages(ctx context.Context, cycle *dreamCycle) {
	created := wd.seedPersonPages(ctx, cycle.synthInput, cycle.episodeRef)
	if created == 0 {
		return
	}
	cycle.created += created
	cycle.report.WikiPagesCreated = cycle.created
}

func (wd *WikiDreamer) applyDreamProjectDigests(ctx context.Context, cycle *dreamCycle) {
	if auxInputTooSmall(cycle.synthInput) {
		return
	}
	digests, err := wd.extractProjectDigests(ctx, cycle.synthInput)
	if err != nil {
		cycle.addPhaseError("project-digests: %v", err)
		return
	}
	if len(digests) == 0 {
		return
	}
	written := wd.applyProjectDigests(digests, time.Now(), cycle.episodeRef)
	if written == 0 {
		return
	}
	cycle.report.WikiProjectDigests = written
	wd.logger.Info("wiki-dream: project status updated", "written", written)
}

func (wd *WikiDreamer) applyDreamUserDirectives(cycle *dreamCycle) {
	if !userDirectivesEnabled() {
		return
	}
	applied, err := wd.distillUserDirectives()
	if err != nil {
		cycle.addPhaseError("user-directives: %v", err)
		return
	}
	if applied > 0 {
		wd.logger.Info("wiki-dream: user directives applied", "directives", applied)
	}
}

func (wd *WikiDreamer) rebuildAndVerifyDreamWiki(ctx context.Context, cycle *dreamCycle) {
	if err := wd.rebuildIndex(); err != nil {
		cycle.addPhaseError("index-rebuild: %v", err)
	}

	findings := wd.verifyPages(ctx)
	// Reconcile even on zero findings: entries for issues that no longer
	// re-appear are how the ledger records "resolved" (a recurrence later
	// counts as new again).
	ledger, fresh, repeats := reconcileVerifyFindings(wd.loadVerifyLedger(), findings, time.Now())
	if err := wd.saveVerifyLedger(ledger); err != nil {
		cycle.addPhaseError("verify-ledger: %v", err)
	}
	if len(findings) == 0 {
		return
	}
	applied := wd.applyVerifyFixes(findings)
	// Only FIRST-TIME advisory findings are reported verbatim; ones the
	// operator has already been shown fold into a single count so the dream
	// notification announces news, not the standing backlog.
	for _, finding := range fresh {
		cycle.report.VerifyFindings = append(cycle.report.VerifyFindings, finding.Detail)
	}
	cycle.report.VerifyFindingsRepeat = repeats
	for _, finding := range findings {
		if finding.Type == "unrecalled" {
			cycle.report.UnrecalledFindings++
		}
	}
	wd.logger.Info("wiki-dream: verification",
		"findings", len(findings), "new", len(fresh), "repeat", repeats, "autoApplied", applied)
	if applied > 0 {
		if err := wd.rebuildIndex(); err != nil {
			cycle.addPhaseError("index-rebuild after auto-fix: %v", err)
		}
	}
}

func (wd *WikiDreamer) enrichDreamRelatedLinks(ctx context.Context) {
	if enriched := wd.enrichRelatedLinks(ctx); enriched > 0 {
		wd.logger.Info("wiki-dream: related-link enrichment", "linksAdded", enriched)
	}
}

func (wd *WikiDreamer) writeDreamGraphSnapshot(ctx context.Context, cycle *dreamCycle) {
	outDir, enabled := graphSnapshotOutDir()
	if !enabled {
		return
	}
	snapshot, err := BuildGraphSnapshot(ctx, wd.store, outDir, true)
	if err != nil {
		cycle.addPhaseError("graph-snapshot: %v", err)
		return
	}
	cycle.report.WikiGraphNodes = snapshot.Nodes
	cycle.report.WikiGraphEdges = snapshot.Edges
	cycle.report.WikiGraphClustered = snapshot.Clustered
	if snapshot.ClusterError != "" {
		wd.logger.Warn("wiki-dream: graph cluster step failed", "error", snapshot.ClusterError)
	}
	wd.logger.Info("wiki-dream: graph snapshot",
		"nodes", snapshot.Nodes, "edges", snapshot.Edges,
		"clustered", snapshot.Clustered, "out", snapshot.GraphPath)
}

// applyDreamPartialBackpressure holds every consumed cursor for the first two
// damaged synthesis responses, then advances to avoid permanent poisoning.
func (wd *WikiDreamer) applyDreamPartialBackpressure(cycle *dreamCycle) bool {
	if !cycle.partial {
		cycle.scan.State.PartialStreak = 0
		return false
	}
	if cycle.scan.State.PartialStreak >= 2 {
		wd.logger.Warn("wiki-dream: partial synthesis repeated — advancing past damaged input",
			"streak", cycle.scan.State.PartialStreak)
		cycle.scan.State.PartialStreak = 0
		return false
	}

	cycle.scan.State.PartialStreak++
	if cycle.scan.PriorFiles != nil {
		cycle.scan.State.Files = cycle.scan.PriorFiles
	}
	wd.logger.Warn("wiki-dream: partial synthesis — input cursors held for re-consumption",
		"streak", cycle.scan.State.PartialStreak)
	return true
}

func (wd *WikiDreamer) curateDreamMemory(cycle *dreamCycle, heldOffsets bool) {
	if cycle.memoryScan == nil || heldOffsets {
		return
	}
	if _, err := wd.curateWorkspaceMemory(cycle.memoryScan); err != nil {
		cycle.addPhaseError("memory-curation: %v", err)
	}
	cycle.scan.State.MemoryConsumedThrough = cycle.memoryScan.ConsumedThrough
}

func dreamProgressCursor(scan *diaryScanResult, heldOffsets bool, now time.Time) string {
	if heldOffsets {
		return ""
	}
	if scan.LatestDate != "" {
		return scan.LatestDate
	}
	return now.Format("2006-01-02")
}

func (wd *WikiDreamer) persistDreamProgress(cycle *dreamCycle, heldOffsets bool) {
	cursor := dreamProgressCursor(cycle.scan, heldOffsets, time.Now())
	if err := wd.store.setLastProcessedAndSave(cursor); err != nil {
		cycle.addPhaseError("index-save: %v", err)
	}
	if cycle.scan == nil {
		return
	}
	cycle.scan.State.Recent = appendProcessedDiaryCapsule(cycle.scan.State.Recent, processedDiaryCapsule{
		At:        time.Now().Format(time.RFC3339),
		DiaryDate: cycle.scan.LatestDate,
		Proposed:  len(cycle.updates),
		Created:   cycle.created,
		Updated:   cycle.updated,
		Paths:     cycle.appliedPaths,
	})
	if err := wd.saveDiaryProcessState(cycle.scan.State); err != nil {
		cycle.addPhaseError("diary-state-save: %v", err)
	}
}

func (wd *WikiDreamer) completeDreamCycle(ctx context.Context, cycle *dreamCycle) {
	wd.resetCounters()
	cycle.report.PhaseErrors = cycle.phaseErrors
	cycle.report.DurationMs = time.Since(cycle.startedAt).Milliseconds()
	cycle.proposal.Applied = dreamApplySummary{Created: cycle.created, Updated: cycle.updated}
	cycle.proposal.PhaseErrors = cycle.phaseErrors
	cycle.proposal.DurationMs = cycle.report.DurationMs
	if err := wd.saveDreamProposalReport(cycle.proposal); err != nil {
		cycle.report.PhaseErrors = append(cycle.report.PhaseErrors, fmt.Sprintf("proposal-save-final: %v", err))
	}

	message := fmt.Sprintf("dream: +%d페이지 생성, %d페이지 수정", cycle.created, cycle.updated)
	if hash := wd.store.SnapshotGit(ctx, message); hash != "" {
		cycle.report.WikiChangeSummary = formatWikiChangeSummary(
			hash,
			wd.store.gitSnapshotStat(ctx, hash),
			wd.store.Dir(),
			updatePaths(cycle.updates),
		)
	}

	wd.logger.Info("wiki-dream: cycle complete",
		"created", cycle.created,
		"updated", cycle.updated,
		"userModel", cycle.userPages,
		"duration", time.Since(cycle.startedAt).Round(time.Millisecond))
}

// scanDiaries reads diary bytes that have not yet been consolidated. The
// primary cursor is a per-file byte offset; index.LastProcessed is only a
// legacy migration hint for old diaries that predate the cursor file.

func flexibleExtraBody(m map[string]any) map[string]llm.FlexibleJSON {
	if m == nil {
		return nil
	}
	out := make(map[string]llm.FlexibleJSON, len(m))
	for k, v := range m {
		out[k] = llm.FlexibleFromValue(v)
	}
	return out
}
