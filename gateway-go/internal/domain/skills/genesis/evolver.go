package genesis

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/guardrails"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*EvolutionTask)(nil)

// DefaultEvolveEventThreshold is how many new skills must accumulate before the
// event-driven evolve trigger fires (every 3 new skills), instead of waiting
// for the 6h periodic cycle.
const DefaultEvolveEventThreshold = 3

// DefaultRollbackThreshold is how many consecutive post-evolve failures revert
// an evolution. Single-user traffic is sparse, so a small fixed count (not a
// success-rate delta, which needs weeks of samples) is the practical signal: an
// evolution that breaks the next few uses in a row is reverted to its backup.
const DefaultRollbackThreshold = 3

// EvolveResult describes the outcome of an evolution attempt.
type EvolveResult struct {
	SkillName            string            `json:"skillName"`
	Evolved              bool              `json:"evolved"`
	NewVersion           string            `json:"newVersion,omitempty"`
	Description          string            `json:"description,omitempty"`
	Reason               string            `json:"reason,omitempty"` // when skipped
	Audit                *HarnessEditAudit `json:"selfHarnessAudit,omitempty"`
	JudgeVersion         string            `json:"judgeVersion,omitempty"`
	JudgeMargin          *float64          `json:"judgeMargin,omitempty"`
	NeedsOperatorVerdict bool              `json:"needsOperatorVerdict,omitempty"`
}

// HarnessEditAudit is the Self-Harness transition metadata for a candidate
// skill-body edit. It keeps the "why this changed" fields queryable instead of
// burying them in a free-form description.
type HarnessEditAudit = guardrails.Audit

// Evolver auto-improves skills based on usage data.
type Evolver struct {
	llmClient        *llm.Client
	catalog          *skills.Catalog
	tracker          *Tracker
	validationEngine *SkillValidationEngine
	// knownTools returns the registered gateway tool names. The judge needs a
	// referent for "존재하지 않는 도구"; without one it falls back to the incumbent
	// SKILL.md and rejects every repair of a stale skill as fabrication.
	// Injected because the tool registry lives in the chat pipeline.
	knownTools func() []string
	model      string
	logger     *slog.Logger
	configMu   sync.RWMutex

	// selfTest gates the verification loop: when true, a rewritten skill is
	// judged before being committed (a bad "improvement" is worse than none).
	selfTest bool
	// teacherClient/teacherModel are an optional stronger (main) model used to
	// re-attempt a rewrite that fails self-test (#4 teacher-escalation). nil → no
	// escalation. It no longer doubles as the judge — see judgeClient.
	teacherClient *llm.Client
	teacherModel  string

	// judgeClient/judgeModel are an optional independent model (typically
	// modelrole main) that grades a candidate rewrite. Kept separate from the
	// producer (primary) and the teacher so the verdict is never self-judged:
	// same-family self-preference bias skews a self-judge toward accepting
	// (arXiv:2508.02994). When a dedicated coding model owns rewrites the teacher
	// is nil, but this stays wired so pickCandidateJudge still resolves to a
	// non-producer judge. nil → fall back to teacher, then self-judge (logged).
	judgeClient *llm.Client
	judgeModel  string

	// thinkingKwargs maps a bare model name to its chat_template_kwargs toggle
	// that truly disables the thinking phase (e.g. "thinking" for DeepSeek V4).
	// Wired from the model registry (SetThinkingKwargs). An absent/empty entry
	// means the model has no per-request off-switch and the provider layer falls
	// back to a minimal reasoning-effort floor. Without this the dsv4 judge and
	// teacher spent their whole output budget reasoning and returned truncated
	// JSON ("judge error" / "parse response: unexpected end of JSON input").
	thinkingKwargs map[string]string

	// bundledSkillsDir/managedSkillsDir drive copy-on-evolve adoption of
	// bundled (repo) skills — see evolver_adopt.go. Guarded by configMu.
	bundledSkillsDir string
	managedSkillsDir string

	// runMu serializes evolve cycles so the periodic task and the event
	// trigger can't overlap (TryLock: a second concurrent caller skips).
	runMu sync.Mutex

	// skillLocks serializes evolves of the SAME skill across ALL entry points
	// (periodic, review fork, manual RPC). runMu only guards the periodic
	// cycle, so a review/RPC evolve could previously read-gate-write the same
	// file concurrently with the periodic one — last-writer-wins on the body,
	// and the backup could capture an intermediate version, corrupting
	// rollback (RSI code eval M4). Different skills never contend. Guarded by
	// skillLocksMu; the per-skill mutex is a leaf (never held across another
	// lock acquisition).
	skillLocksMu sync.Mutex
	skillLocks   map[string]*sync.Mutex

	// meta resolves prompt artifacts (RSI P1); nil → compiled-in prompts.
	meta *generation.MetaArtifacts

	// lowConfidenceObserver surfaces accepted-but-borderline judge verdicts to
	// an operator without blocking the evolve. Guarded by configMu.
	lowConfidenceObserver func(EvolveResult)
}

// NewEvolver creates a skill evolver. Self-test defaults on; disable with
// DENEB_SKILL_EVOLVE_SELFTEST=0.
func NewEvolver(llmClient *llm.Client, catalog *skills.Catalog, tracker *Tracker, model string, logger *slog.Logger) *Evolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Evolver{
		llmClient:        llmClient,
		catalog:          catalog,
		tracker:          tracker,
		validationEngine: NewSkillValidationEngine(tracker, logger),
		model:            model,
		logger:           logger,
		selfTest:         envBool("DENEB_SKILL_EVOLVE_SELFTEST", true),
		skillLocks:       make(map[string]*sync.Mutex),
	}
}

// lockSkill serializes evolves of one skill across every entry point. Returns
// the unlock func. Different skills never contend; the per-skill mutex is a
// leaf lock.
func (e *Evolver) lockSkill(skillName string) func() {
	e.skillLocksMu.Lock()
	mu := e.skillLocks[skillName]
	if mu == nil {
		mu = &sync.Mutex{}
		if e.skillLocks == nil {
			e.skillLocks = make(map[string]*sync.Mutex)
		}
		e.skillLocks[skillName] = mu
	}
	e.skillLocksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// lockSkillMutex returns the per-skill mutex identity without locking it —
// test seam for asserting the map keys by skill.
func (e *Evolver) lockSkillMutex(skillName string) *sync.Mutex {
	e.skillLocksMu.Lock()
	defer e.skillLocksMu.Unlock()
	mu := e.skillLocks[skillName]
	if mu == nil {
		mu = &sync.Mutex{}
		e.skillLocks[skillName] = mu
	}
	return mu
}

// SetLowConfidenceObserver registers the post-commit sink for accepted skill
// evolves close to the judge admission boundary. It never participates in the
// commit decision.
func (e *Evolver) SetLowConfidenceObserver(fn func(EvolveResult)) {
	e.configMu.Lock()
	e.lowConfidenceObserver = fn
	e.configMu.Unlock()
}

func (e *Evolver) notifyLowConfidence(result EvolveResult) {
	e.configMu.RLock()
	fn := e.lowConfidenceObserver
	e.configMu.RUnlock()
	if fn != nil {
		fn(result)
	}
}

// SetPrimary updates the model/client used for rewrite generation. It mutates
// the existing evolver so RPC handlers and tools holding this pointer observe
// Settings changes without being re-registered.
func (e *Evolver) SetPrimary(client *llm.Client, model string) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.llmClient = client
	e.model = strings.TrimSpace(model)
}

// SetTeacher wires an optional stronger model (typically modelrole main) used
// to escalate a rewrite that fails the lightweight self-test. Safe to call
// with a nil client (no-op escalation).
func (e *Evolver) SetTeacher(client *llm.Client, model string) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.teacherClient = client
	e.teacherModel = strings.TrimSpace(model)
}

// SetJudge wires an optional independent judge model (typically modelrole main)
// used to grade a candidate rewrite. Decoupled from SetTeacher so that even when
// a dedicated coding model owns the rewrite path (teacher nil), the candidate is
// still judged by a non-producer model (pickCandidateJudge). Safe to call with
// nil (judge then falls back to teacher, then a logged self-judge).
func (e *Evolver) SetJudge(client *llm.Client, model string) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.judgeClient = client
	e.judgeModel = strings.TrimSpace(model)
}

// SetReplayExecutor wires the behavioral-replay executor model used by the
// held-out validation engine to score a candidate rewrite's tool-call behavior
// (SkillValidationEngine.EvaluateBehavior). Safe to call with nil (disables the
// behavioral gate). The engine guards the executor with its own lock, so this
// does not take configMu.
func (e *Evolver) SetReplayExecutor(client *llm.Client, model string) {
	if e.validationEngine != nil {
		e.validationEngine.SetExecutor(client, model)
	}
}

// SetThinkingKwargs wires per-model chat_template_kwargs thinking toggles so the
// evolver's judge/teacher/rewrite calls truly disable reasoning on dual-mode
// vLLM models (the only effective control on e.g. deepseek-v4). Keyed by bare
// model name. Safe to call with nil (the calls then fall back to the provider's
// reasoning-effort floor).
// SetMetaArtifacts wires the prompt-artifact resolver (RSI P1). Nil keeps
// compiled-in prompts.
func (e *Evolver) SetMetaArtifacts(m *generation.MetaArtifacts) {
	e.configMu.Lock()
	e.meta = m
	e.configMu.Unlock()
}

// catalogEntries returns the current skill catalog listing ([] when unwired) —
// the judge-degradation bench builds gold pairs from real skill bodies.
func (e *Evolver) catalogEntries() []skills.SkillEntry {
	if e.catalog == nil {
		return nil
	}
	return e.catalog.List()
}

// provenanceFromProducer seeds the certificate from the producer snapshot
// captured at the generate call. The judge fields are stamped later at the judge
// call (judgeCandidate, last verdict wins), and ProcedureRef is derived from the
// captured evolve+judge versions at log time (fillProcedureRef) — so every field
// reflects the LLM call that actually produced/judged the committed decision,
// not mutable config re-read afterward. The teacher-escalation path overrides
// EvolveModel when the committed body is the teacher's rewrite.
func provenanceFromProducer(snap producerSnapshot) evolveProvenance {
	return evolveProvenance{
		EvolveModel:           snap.model,
		EvolveArtifactVersion: snap.evolveVersion,
	}
}

// metaVersion pins the active version of one artifact under the config lock —
// used to capture a prompt version at the moment of its LLM call.
func (e *Evolver) metaVersion(name string) string {
	e.configMu.RLock()
	m := e.meta
	e.configMu.RUnlock()
	return m.Version(name, generation.DefaultMetaArtifacts()[name])
}

// producerSnapshotNow captures the CURRENT producer model + evolve-prompt
// version. Used by the text-in-hand parseAndApply entry (the producer call
// happened outside this function, so its true snapshot is unavailable) —
// best-available attribution, and the teacher-escalation path still overrides
// EvolveModel when the committed body is the teacher's.
func (e *Evolver) producerSnapshotNow() producerSnapshot {
	_, model := e.primaryModel()
	return producerSnapshot{model: model, evolveVersion: e.metaVersion(generation.MetaEvolveSystemPrompt)}
}

func (e *Evolver) metaLoad(name, fallback string) string {
	e.configMu.RLock()
	m := e.meta
	e.configMu.RUnlock()
	return m.Load(name, fallback)
}

// SetThinkingKwargs replaces the per-model thinking keyword overrides used by evolution runs.
func (e *Evolver) SetThinkingKwargs(kwargs map[string]string) {
	cloned := make(map[string]string, len(kwargs))
	for k, v := range kwargs {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		cloned[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.thinkingKwargs = cloned
}

// thinkingOff returns a disabled ThinkingConfig for model, naming the model's
// chat_template_kwargs toggle when known so the provider layer emits a real
// off-switch instead of merely lowering reasoning effort.
func (e *Evolver) thinkingOff(model string) *llm.ThinkingConfig {
	e.configMu.RLock()
	kwarg := e.thinkingKwargs[model]
	e.configMu.RUnlock()
	return &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: kwarg}
}

// EvolveSkill attempts to improve a single skill based on usage feedback.
// EvolveSkill improves one skill. reviewFinding is an optional improvement
// directive from a background skill-review (the LLM that just observed a
// session); when present it is the primary basis for the rewrite and lets the
// evolver proceed even with no usage data — usage-stat-driven evolution
// otherwise never fires because skill usage is sparsely recorded.
func (e *Evolver) EvolveSkill(ctx context.Context, skillName, reviewFinding string) (*EvolveResult, error) {
	// Hold the per-skill lock for the WHOLE operation (M4). Direct callers
	// (review fork, RPC) lock here; the periodic lane locks around round 1 +
	// its burst in EvolveUnderperformers, so a burst continuation stays
	// serialized against a concurrent direct evolve (Codex review).
	unlock := e.lockSkill(skillName)
	defer unlock()
	return e.evolveSkill(ctx, skillName, reviewFinding, false)
}

// evolveSkill is the shared implementation. burstContinuation is set only for
// rounds 2+ of runEvolveBurst: those rounds have no fresh usage (the evidence
// cutoff moved to round 1's evolve), so the evidence-sufficiency gate would
// stop the burst immediately — making the advertised loop-until-dry inert. When
// the skill is COVERED (a real held-out regression gate exists), a burst round
// may proceed on that bench alone; the per-round held-out + judge + rollback
// gates decide dryness. Uncovered skills keep the conservative gate (burst
// stops after round 1) so nothing churns on judge opinion without a bench.
// evolveSkill is the lock-free core (M4): every caller MUST already hold the
// per-skill lock (EvolveSkill for direct callers; EvolveUnderperformers around
// round 1 + burst). Acquiring it here would either deadlock the periodic lane
// (which locks around the whole burst) or leave burst rounds unserialized.
func (e *Evolver) evolveSkill(ctx context.Context, skillName, reviewFinding string, burstContinuation bool) (*EvolveResult, error) {
	if e.catalog == nil {
		return nil, fmt.Errorf("evolver: catalog not configured")
	}
	entry, err := e.resolveEvolveEntry(skillName)
	if err != nil {
		return nil, err
	}

	// Circuit breakers, checked before any LLM call is spent. Both previously
	// lived only in the periodic candidate selector (SkillsNeedingEvolution); the
	// background skill-review path (RunSkillEvolution) reaches EvolveSkill
	// directly with a review finding and so bypassed them, re-evolving a
	// non-converging skill on every review (topsolar-db: 18 evolves over 4 days,
	// all landing the same version, ~6 days after its last real use). Enforcing
	// them here — at the single choke point every caller funnels through — closes
	// that bypass. The suppression is logged as evolve_rejected so it is auditable
	// instead of a silent re-evolve.
	if blocked, reason := e.evolutionSuppressed(skillName, time.Now()); blocked {
		return e.suppressedEvolveResult(skillName, reason), nil
	}

	currentContent, err := os.ReadFile(entry.Skill.FilePath)
	if err != nil {
		return nil, fmt.Errorf("evolver: read skill file: %w", err)
	}

	// Get the bounded usage evidence this evolution is allowed to act on.
	// Lifetime Stats() can include old failures that should remain observable
	// but must not keep driving fresh rewrites.
	stats := e.evolutionStats(skillName)
	// Burst continuation on a covered skill bypasses the fresh-evidence
	// requirement — its held-out bench is the evidence, and the pre-commit
	// gates below still guard every round. Uncovered skills fall through to
	// the conservative stop.
	if !hasSufficientEvolutionEvidence(stats, reviewFinding) &&
		!(burstContinuation && hasScorableValidationCase(e.validationCasesForCoverage(skillName))) {
		return &EvolveResult{
			SkillName: skillName,
			Evolved:   false,
			Reason:    fmt.Sprintf("insufficient evolution evidence: need review finding or at least %d counted uses with %d real failures and recent error evidence", skillEvolutionMinEvidenceUses, skillEvolutionMinEvidenceFailures),
		}, nil
	}

	userPrompt := e.buildEvolveUserPrompt(ctx, skillName, string(currentContent), stats, reviewFinding)

	if primaryClient, _ := e.primaryModel(); primaryClient == nil {
		return nil, fmt.Errorf("evolver: primary client not configured")
	}

	return e.generateSelectAndApply(ctx, userPrompt, entry, string(currentContent), stats, reviewFinding)
}

// resolveEvolveEntry gets the current skill entry from the catalog. A miss
// usually means a BUNDLED (repo) skill: those are deliberately not seeded into
// this catalog (the curator's staleness archiver would eat the rarely-used
// ones), so an evolve verdict on one adopts it — copy into the managed dir,
// which overrides bundled at discovery — and evolves the copy. Never rewrite
// the repo checkout in place (deploy rsync --delete + auto git pull would
// clobber it). First production hit 2026-07-04: contract-review's evolve
// verdict died here.
func (e *Evolver) resolveEvolveEntry(skillName string) (*skills.SkillEntry, error) {
	entry, ok := e.catalog.Get(skillName)
	if !ok {
		adopted, aerr := e.adoptBundledSkill(skillName)
		if aerr != nil {
			return nil, fmt.Errorf("evolver: skill %q not found in catalog (bundled adoption: %w)", skillName, aerr)
		}
		entry = adopted
	}
	return entry, nil
}

// suppressedEvolveResult logs a circuit-breaker suppression (lifecycle +
// operator log) and returns the non-evolve result. Pure extraction of the
// evolveSkill suppression tail; audit records are unchanged.
func (e *Evolver) suppressedEvolveResult(skillName, reason string) *EvolveResult {
	if e.tracker != nil {
		if logErr := e.tracker.LogEvolveRejectedWithAudit(skillName, reason, HarnessEditAudit{}); logErr != nil && e.logger != nil {
			e.logger.Warn("evolver: lifecycle log write failed", "skill", skillName, "error", logErr)
		}
	}
	if e.logger != nil {
		e.logger.Info("evolver: evolve suppressed", "skill", skillName, "reason", reason)
	}
	return &EvolveResult{SkillName: skillName, Evolved: false, Reason: reason}
}

// evolutionStats returns the bounded usage evidence for skillName, or an empty
// stats value when the tracker is unwired or has nothing recorded.
func (e *Evolver) evolutionStats(skillName string) *UsageStats {
	var stats *UsageStats
	if e.tracker != nil {
		stats, _ = e.tracker.evolutionEvidenceStats(skillName)
	}
	if stats == nil {
		stats = &UsageStats{SkillName: skillName}
	}
	return stats
}

// buildEvolveUserPrompt assembles the rewrite prompt: current body, usage
// stats, and the tracker-derived evidence sections. A review-provided finding
// (when present) is the primary basis for improvement and lets the evolver
// proceed without usage data.
func (e *Evolver) buildEvolveUserPrompt(ctx context.Context, skillName, currentContent string, stats *UsageStats, reviewFinding string) string {
	var rejected []RejectedSkillEditRecord
	var optimizerMemory SkillOptimizerMemoryEntry
	var validationCases []SkillValidationCaseRecord
	if e.tracker != nil {
		var rejectedErr error
		rejected, rejectedErr = e.tracker.RecentRejectedSkillEdits(skillName, 3)
		if rejectedErr != nil && e.logger != nil {
			e.logger.Warn("evolver: rejected edit buffer unavailable",
				"skill", skillName, "error", rejectedErr)
		}
		var memoryErr error
		optimizerMemory, memoryErr = e.tracker.OptimizerMemory(skillName)
		if memoryErr != nil && e.logger != nil {
			e.logger.Warn("evolver: optimizer memory unavailable",
				"skill", skillName, "error", memoryErr)
		}
		validationCases = e.validationCasesForPrompt(skillName)
	}
	disclosure := e.unmetBlindDisclosure(skillName, currentContent)

	findingSection := ""
	if strings.TrimSpace(reviewFinding) != "" {
		findingSection = "\n\n## Review Finding (개선 지시 — 우선 반영)\n" + strings.TrimSpace(reviewFinding)
	}
	rejectedSection := formatRejectedSkillEdits(rejected)
	memorySection := formatOptimizerMemory(optimizerMemory)
	leverSection := e.formatLowYieldLevers()
	exemplarSection := e.formatEvolveExemplarSection(ctx, skillName, stats)
	validationSection := formatValidationCasesForPrompt(validationCases)
	failurePatternSection := formatFailurePatternsForPrompt(stats)
	return fmt.Sprintf(`## 현재 SKILL.md
%s

## 사용 통계
- 총 사용: %d회
- 성공: %d회 (%.0f%%)
- 실패: %d회
- 최근 에러: %s%s%s%s%s%s%s%s%s`,
		currentContent,
		stats.TotalUses, stats.SuccessCount, stats.SuccessRate*100,
		stats.FailureCount,
		formatRecentErrors(stats.RecentErrors),
		failurePatternSection,
		rejectedSection,
		memorySection,
		leverSection,
		exemplarSection,
		validationSection,
		findingSection,
		disclosure)
}

// formatEvolveExemplarSection renders confirmed evolve exemplars matched to the
// skill's recent failure signatures (up to 3), or "" when the tracker is
// unwired or lookup fails.
func (e *Evolver) formatEvolveExemplarSection(ctx context.Context, skillName string, stats *UsageStats) string {
	if e.tracker == nil || stats == nil {
		return ""
	}
	var sigs []string
	for _, tr := range stats.RecentFailureTraces {
		if s := strings.TrimSpace(tr.Signature); s != "" {
			sigs = append(sigs, s)
		}
		if len(sigs) >= 3 {
			break
		}
	}
	exemplars, exErr := e.tracker.confirmedEvolveExemplarsContext(ctx, sigs, skillName, 3)
	if exErr != nil {
		return ""
	}
	return formatConfirmedEvolveExemplars(exemplars)
}

const (
	skillLeverYieldScanLimit      = 300 // lifecycle entries scanned for lever yield
	skillLeverYieldMinShips       = 3   // only flag levers shipped at least this often
	skillLeverYieldMaxConfirmRate = 0.4 // ...that confirm at or below this rate
)

// formatLowYieldLevers surfaces (target-signature × edited-surface) edit
// strategies that have shipped repeatedly yet rarely held up, so the evolver
// stops re-proposing fleet-wide dead ends (#2 lever-yield, finally wired into the
// prompt — previously computed but unread). Empty when no lever clears the bar.
func (e *Evolver) formatLowYieldLevers() string {
	if e == nil || e.tracker == nil {
		return ""
	}
	levers, err := e.tracker.lowYieldLevers(skillLeverYieldScanLimit, skillLeverYieldMinShips, skillLeverYieldMaxConfirmRate)
	if err != nil || len(levers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 저수율 lever (반복 ship됐지만 실제로 안 버팀 — 이 방향 피하라)\n")
	for _, l := range levers {
		surface := l.Surface
		if surface == "" {
			surface = "(unspecified)"
		}
		fmt.Fprintf(&b, "- %s → %s: %d ship, confirm %.0f%%\n", l.Signature, surface, l.Committed, l.ConfirmRate*100)
	}
	return b.String()
}

// generateSelectAndApply runs the K-candidate generate-and-select loop (#3): it
// streams up to skillEvolveCandidateCount candidate bodies from the producer
// (each after the first nudged to differ), evaluates every candidate through the
// full pre-commit gate stack without committing, and commits the best
// non-regressive one (ranked by held-out score margin). When K==1, or only one
// candidate survives, this is behaviorally identical to the old single-candidate
// parseAndApply path — including the self-test's own teacher escalation. If no
// candidate is committable, the last evaluated rejection/skip result is returned
// so the lifecycle log and reason match the prior single-candidate behavior.
func (e *Evolver) generateSelectAndApply(ctx context.Context, userPrompt string, entry *skills.SkillEntry, originalContent string, stats *UsageStats, reviewFinding string) (*EvolveResult, error) {
	k := skillEvolveCandidateCount
	if k < 1 {
		k = 1
	}

	var best *evaluatedCandidate
	var lastResult *EvolveResult
	var firstGenErr error
	generated := 0
	for attempt := 0; attempt < k; attempt++ {
		if ctx.Err() != nil {
			break
		}
		text, snap, genErr := e.generateCandidateText(ctx, userPrompt, attempt)
		if genErr != nil {
			// A producer call failing on the first attempt is fatal (no candidate
			// at all); later attempts are best-effort — keep any candidate already
			// in hand rather than discarding the whole cycle for one flaky stream.
			if attempt == 0 {
				firstGenErr = genErr
			} else if e.logger != nil {
				e.logger.Warn("evolver: candidate generation failed, continuing with earlier candidates",
					"skill", entry.Skill.Name, "attempt", attempt, "error", genErr)
			}
			continue
		}
		generated++
		eval, err := e.evaluateCandidateText(ctx, text, snap, entry, originalContent, stats, reviewFinding)
		if err != nil {
			if attempt == 0 {
				return nil, err
			}
			if e.logger != nil {
				e.logger.Warn("evolver: candidate evaluation failed, continuing",
					"skill", entry.Skill.Name, "attempt", attempt, "error", err)
			}
			continue
		}
		if eval.result != nil {
			// Skip or gate rejection (already lifecycle-logged). Remember it so a
			// fully-failing cycle still returns a faithful reason.
			lastResult = eval.result
			continue
		}
		// Committable (non-regressive: it cleared every gate). Keep the highest
		// held-out margin; ties resolve to the earlier candidate (stable).
		if best == nil || eval.margin > best.margin {
			winner := eval
			best = &winner
		}
	}

	return e.finishCandidateSelection(entry, originalContent, best, lastResult, firstGenErr, generated)
}

// finishCandidateSelection resolves the K-candidate loop's outcome: commit the
// best committable candidate, else return the last per-candidate skip/rejection
// (already lifecycle-logged), else the fatal first-attempt producer error. Pure
// extraction of generateSelectAndApply's tail.
func (e *Evolver) finishCandidateSelection(entry *skills.SkillEntry, originalContent string, best *evaluatedCandidate, lastResult *EvolveResult, firstGenErr error, generated int) (*EvolveResult, error) {
	if best != nil {
		if generated > 1 && e.logger != nil {
			e.logger.Info("evolver: selected best candidate",
				"skill", entry.Skill.Name, "candidates", generated, "heldOutMargin", best.margin)
		}
		return e.commitEvaluatedCandidate(entry, originalContent, *best)
	}
	if lastResult != nil {
		return lastResult, nil
	}
	if firstGenErr != nil {
		return nil, firstGenErr
	}
	// No candidate generated and no per-candidate result (e.g. context cancelled
	// before the first stream completed) — surface a non-evolve rather than nil.
	return &EvolveResult{SkillName: entry.Skill.Name, Evolved: false, Reason: "no candidate generated"}, nil
}

// generateCandidateText streams one rewrite candidate body from the producer
// model and returns its raw assistant text. attempt 0 uses the base rewrite
// prompt unchanged (so K=1 is byte-identical to the old path); later attempts
// append a small variation note so the K candidates differ without changing the
// rewrite contract.
// producerSnapshot pins the model and evolve-prompt version that ACTUALLY
// generated a candidate, captured at the producer LLM call. Carried into the
// provenance so a later config refresh (SetPrimary / meta-adoption) can't make
// the record claim a model or prompt that never produced the committed body.
type producerSnapshot struct {
	model         string // primary producer model at call time
	evolveVersion string // MetaEvolveSystemPrompt version of the text used as System
}

func (e *Evolver) generateCandidateText(ctx context.Context, userPrompt string, attempt int) (string, producerSnapshot, error) {
	primaryClient, primaryModel := e.primaryModel()
	if primaryClient == nil {
		return "", producerSnapshot{}, fmt.Errorf("evolver: primary client not configured")
	}
	// Load the evolve prompt ONCE: the same text becomes the System message and
	// the pinned version, so the snapshot is exactly what this call ran on.
	evolvePrompt := e.metaLoad(generation.MetaEvolveSystemPrompt, generation.DefaultMetaArtifacts()[generation.MetaEvolveSystemPrompt])
	snap := producerSnapshot{model: primaryModel, evolveVersion: generation.ShortContentVersion(evolvePrompt)}
	prompt := userPrompt
	if attempt > 0 {
		prompt = userPrompt + candidateVariationNote(attempt)
	}
	// Non-streaming on purpose: glm-5.2 (the coding-role producer) is
	// unreliable when STREAMING structured JSON — live 2026-07-04 the reasoning
	// prose leaked into the content stream ("Let me analyze...", no JSON), and
	// a reproduction probe got a stray trailing quote after the closing brace;
	// the same payload non-streaming returned clean, valid JSON every time.
	// Complete() also guards the empty-content-after-reasoning trap.
	text, err := primaryClient.Complete(ctx, llm.ChatRequest{
		Model:    primaryModel,
		Messages: []llm.Message{llm.NewTextMessage("user", prompt)},
		System:   llm.SystemString(evolvePrompt),
		// 12288, not 4096: GLM bills reasoning INSIDE the completion budget and
		// the rewrite must carry a full SKILL.md body (cap 15KB ≈ 5.5K tokens)
		// plus audit fields — at 4096 the live drill (2026-07-04) truncated
		// mid-field ("target_signature" cut) even after the non-stream fix.
		MaxTokens:      12288,
		Temperature:    evolveTemperature(),
		Thinking:       e.thinkingOff(primaryModel),
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", producerSnapshot{}, fmt.Errorf("evolver LLM call: %w", err)
	}
	return text, snap, nil
}

// candidateVariationLenses are deterministic orthogonal exploration directives
// for the K-candidate loop. A generic "be different" note lets the producer
// re-run its prior-favored direction with cosmetic variation (the deterministic
// repetition Bilevel Autoresearch 2603.23420 observed in its baseline: same
// state → same proposals). Each attempt instead gets a NAMED lens that forces
// a direction LLM priors under-explore — the same idea as their generated
// Tabu/Orthogonal mechanisms, applied at the proposal side. The gates still
// judge every candidate, so a lens that fits a skill badly just yields a
// rejected candidate, never a loosened contract.
var candidateVariationLenses = [...]string{
	"직교 섹션: 직전 후보가 손댔을 법한 최우선 섹션이 아니라, 실패 서명과 두 번째로 관련 깊은 다른 섹션을 고쳐서 같은 실패를 막아보세요.",
	"축소 방향: 지시를 추가하지 말고, 모호하거나 과잉된 지시를 제거·압축하는 것만으로 실패를 막아보세요 (추가 편향 금지 — 삭제가 이번 후보의 유일한 수단).",
	"구조 재배열: 내용을 새로 쓰지 말고, 기존 절들의 순서·구획을 재배열해 (예: 실패와 관련된 규칙을 앞으로) 같은 실패를 막아보세요.",
	"경계 명시: 일반 규칙을 다듬는 대신, 실패 케이스가 밟은 경계 조건을 명시적 예외/반례 절로 못박는 접근을 시도하세요.",
}

// candidateVariationNote nudges the producer toward a distinct rewrite on the
// nth (>0) candidate without loosening the rewrite contract. Deterministic:
// attempt n always gets the same lens, rotating through
// candidateVariationLenses. Inert instruction text appended to the user
// prompt; the gates judge the result, so a candidate that drifts is simply
// rejected.
func candidateVariationNote(attempt int) string {
	lens := candidateVariationLenses[(attempt-1)%len(candidateVariationLenses)]
	return fmt.Sprintf(
		"\n\n## 후보 다양화 지시 (candidate #%d — 탐색 렌즈 고정)\n%s\n검증 계약(필수/금지 항목, 구조 보존, 실제 도구만)은 그대로 지키세요.",
		attempt+1, lens,
	)
}

// evolutionSuppressed reports whether an automated evolve of skillName should be
// skipped before an LLM call is spent, returning a human-readable reason for the
// lifecycle log. Two circuit breakers, enforced here so every EvolveSkill caller
// (periodic underperformer sweep, background review, manual RPC) obeys them —
// the guard previously sat only in the periodic candidate selector, which the
// review path bypassed:
//
//   - Guard (thrash): a skill that dominates the recent evolve budget without
//     converging is paused for evolutionThrashCooldown after its last evolve.
//   - Gate (recency): a skill with no real use inside the evolution-evidence
//     window has no fresh signal a rewrite could act on, so re-evolving it just
//     burns model budget. Reuses the same freshness horizon the periodic path
//     already enforces via the evidence cutoff. Never-used skills (LastUsed == 0)
//     are exempt — seeding a sparse or brand-new skill from a review finding is
//     the review path's legitimate purpose.
func (e *Evolver) evolutionSuppressed(skillName string, now time.Time) (bool, string) {
	if e.tracker == nil {
		return false, ""
	}
	if h := e.tracker.EvolutionHealth(); h.Thrash && h.TopEvolvedSkill == skillName &&
		h.ThrashCooldownUntil > now.UnixMilli() {
		return true, fmt.Sprintf(
			"thrash cooldown: %q evolved %d times in 7d without converging; paused until %s",
			skillName, h.TopEvolvedCount, time.UnixMilli(h.ThrashCooldownUntil).Format(time.RFC3339),
		)
	}
	// Rejection backoff: repeated same-window rejections mean the producer has
	// no working strategy for this skill right now. Another unattended attempt
	// is the same coin in the same slot — stop until the window drains (or a
	// human/heartbeat changes something, which shows up as a non-rejected entry
	// resetting nothing but simply aging the streak out).
	if n := e.tracker.RecentEvolveRejections(skillName, evolutionRejectionBackoffWindow); n >= evolutionRejectionBackoffMin {
		return true, fmt.Sprintf(
			"rejection backoff: %q rejected %d times in the last %s with no completed evolve; pausing unattended attempts",
			skillName, n, evolutionRejectionBackoffWindow,
		)
	}
	if window := skillEvolutionEvidenceWindow(); window > 0 {
		if stats, err := e.tracker.Stats(skillName); err == nil && stats.LastUsed > 0 &&
			stats.LastUsed < now.Add(-window).UnixMilli() {
			return true, fmt.Sprintf(
				"recency gate: %q last really used %s, older than the %d-day evidence window; no fresh signal to evolve on",
				skillName, time.UnixMilli(stats.LastUsed).Format("2006-01-02"), int(window/(24*time.Hour)),
			)
		}
	}
	return false, ""
}

// EvolutionSuppressed reports whether an unattended evolve of skillName would
// be refused right now by the deterministic suppression gates (thrash cooldown,
// rejection backoff, recency gate), and why. The proposal lane asks this BEFORE
// executing a route=evolve proposal: a gated skill is answered with the gate
// reason instead of an evolve attempt that can only end as a rejection row
// (2026-08: 17 morning-letter proposals in 21 days, every one stopped by the
// recency gate after it had already been executed).
func (e *Evolver) EvolutionSuppressed(skillName string) (bool, string) {
	if e == nil {
		return false, ""
	}
	return e.evolutionSuppressed(strings.TrimSpace(skillName), time.Now())
}

// evolveUnderperformers finds and evolves skills with poor success rates.
// Used as a periodic background task.
func (e *Evolver) evolveUnderperformers(ctx context.Context) ([]EvolveResult, error) {
	if e.tracker == nil {
		return nil, nil
	}
	// Serialize cycles: the 6h periodic task and the event trigger both call
	// this; if one is already running, the other skips rather than double-work.
	if !e.runMu.TryLock() {
		return nil, nil
	}
	defer e.runMu.Unlock()

	candidates, err := e.tracker.skillsNeedingEvolution(skillEvolutionMinEvidenceUses, 0.7)
	if err != nil {
		return nil, err
	}

	var results []EvolveResult
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		// Hold the per-skill lock for round 1 AND the burst continuation, so a
		// concurrent direct/RPC evolve of the same skill cannot interleave a
		// read-gate-write with a burst round (Codex review of M4). The lock is
		// released before the next candidate. A closure bounds the defer to
		// this iteration.
		results = append(results, func() []EvolveResult {
			unlock := e.lockSkill(candidate.SkillName)
			defer unlock()
			result, err := e.evolveSkill(ctx, candidate.SkillName, "", false)
			if err != nil {
				e.logger.Warn("evolver: failed to evolve",
					"skill", candidate.SkillName, "error", err)
				return []EvolveResult{{
					SkillName: candidate.SkillName,
					Evolved:   false,
					Reason:    err.Error(),
				}}
			}
			if result == nil {
				return nil
			}
			return e.runEvolveBurst(ctx, candidate.SkillName, *result,
				func(ctx context.Context, name string) (*EvolveResult, error) {
					// burstContinuation=true: rounds 2+ ride the held-out bench of
					// a covered skill (A4 — otherwise the evidence gate makes burst
					// inert after round 1). Lock already held for the whole burst.
					return e.evolveSkill(ctx, name, "", true)
				})
		}()...)
	}
	return results, nil //nolint:nilerr // individual skill errors collected in results, not propagated
}

// runEvolveBurst is the loop-until-dry accelerator (2026-07-09): while rounds
// keep landing accepted evolves, immediately try another on the same skill
// instead of waiting for the next 6h cycle. Every round re-runs the FULL gate
// stack against the freshly-committed body — the held-out/no-trade-off gates
// decide when the skill is dry, the round cap bounds cost, and the thrash
// breaker inside EvolveSkill still applies unchanged. The first (already
// obtained) result is included in the returned slice.
func (e *Evolver) runEvolveBurst(ctx context.Context, skillName string, first EvolveResult, evolve func(context.Context, string) (*EvolveResult, error)) []EvolveResult {
	out := []EvolveResult{first}
	last := first
	for round := 1; round < skillEvolveBurstMaxRounds && last.Evolved; round++ {
		if ctx.Err() != nil {
			break
		}
		next, err := evolve(ctx, skillName)
		if err != nil || next == nil {
			if err != nil {
				e.logger.Warn("evolver: burst round failed", "skill", skillName, "round", round+1, "error", err)
			}
			break
		}
		e.logger.Info("evolver: burst round finished",
			"skill", skillName, "round", round+1, "evolved", next.Evolved, "reason", next.Reason)
		out = append(out, *next)
		last = *next
	}
	return out
}

func (e *Evolver) parseAndApply(ctx context.Context, text string, entry *skills.SkillEntry, originalContent string, stats *UsageStats, reviewFinding string) (*EvolveResult, error) {
	eval, err := e.evaluateCandidateText(ctx, text, e.producerSnapshotNow(), entry, originalContent, stats, reviewFinding)
	if err != nil {
		return nil, err
	}
	if eval.result != nil {
		// Skip / rejection (already lifecycle-logged) — no commit.
		return eval.result, nil
	}
	return e.commitEvaluatedCandidate(entry, originalContent, eval)
}

// EvolutionTask implements autonomous.PeriodicTask for background skill evolution.
type EvolutionTask struct {
	Evolver *Evolver
	// Bootstrap, when set, runs exactly once before the first cycle — the
	// anchor for boot-only side effects (RSI P1 meta-artifact
	// materialization) that must never fire from unit tests, which register
	// tasks but never start the autonomous service.
	Bootstrap func()

	bootstrapOnce sync.Once
	Logger        *slog.Logger
}

// Name returns the task identifier.
func (t *EvolutionTask) Name() string { return "skill-evolution" }

// Interval returns how often to check for underperforming skills.
func (t *EvolutionTask) Interval() time.Duration { return 6 * time.Hour }

// watchMaxAge is how long a rollback watch may stay open before the
// time-based sweep resolves it (small-sample confirm, or expiry at zero
// uses). Env knob DENEB_SKILL_WATCH_MAX_AGE_DAYS accelerates label
// accumulation without faking usage.
func watchMaxAge() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_SKILL_WATCH_MAX_AGE_DAYS")); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	return 14 * 24 * time.Hour
}

// Run executes one evolution cycle.
func (t *EvolutionTask) Run(ctx context.Context) error {
	if t.Bootstrap != nil {
		t.bootstrapOnce.Do(t.Bootstrap)
	}
	// Time-based watch resolution first: without it a watch on a rarely-used
	// skill never resolves and the e-process label pipeline starves (backtest
	// 2026-07-11: zero historical resolutions).
	if t.Evolver != nil && t.Evolver.tracker != nil {
		if n := t.Evolver.tracker.resolveStaleWatches(watchMaxAge()); n > 0 && t.Evolver.logger != nil {
			t.Evolver.logger.Info("evolver: stale rollback watches resolved time-based", "count", n)
		}
	}
	results, err := t.Evolver.evolveUnderperformers(ctx)
	// Heartbeat: records that the evolve cycle actually ran (liveness on /health).
	if t.Evolver != nil && t.Evolver.tracker != nil {
		t.Evolver.tracker.RecordEvolutionActivity(skillActivityEvolve, err == nil, genesiscommon.ErrorString(err))
	}
	if err != nil {
		return err
	}
	evolved := 0
	for _, r := range results {
		if r.Evolved {
			evolved++
		}
	}
	if evolved > 0 {
		t.Logger.Info("skill-evolution: cycle complete",
			"evolved", evolved, "total", len(results))
	}
	return nil
}

// SetKnownTools wires the registered gateway tool names. Optional — with no
// registry the judge payload simply omits the authoritative list and behavior
// is unchanged from before, rather than asserting an empty toolset.
func (e *Evolver) SetKnownTools(fn func() []string) {
	if e == nil {
		return
	}
	e.configMu.Lock()
	e.knownTools = fn
	e.configMu.Unlock()
}
