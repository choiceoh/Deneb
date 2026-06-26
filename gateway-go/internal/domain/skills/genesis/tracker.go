package genesis

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Propus activity kinds for the liveness heartbeat.
const (
	SkillActivityReview             = "review"
	SkillActivityReviewAttempt      = "review_attempt"
	SkillActivityReviewSkipped      = "review_skipped"
	SkillActivityValidationRejected = "validation_rejected"
	SkillActivityEvolve             = "evolve"
	SkillActivityGenesis            = "genesis"
)

// SkillLivenessState is a persisted heartbeat for the Propus loop.
// Its sole purpose is to make silent death observable: every past failure of
// this loop (#1932 bare model id, #2031 token-budget underflow, #2035 restart
// interval reset) was silent — nothing logged that an operator would notice.
// If LastReviewAt stops advancing, the nudger→review→evolve pipeline has
// stalled. Surfaced on /health.
type SkillLivenessState struct {
	LastReviewAt  int64  `json:"lastReviewAt,omitempty"`
	LastReviewOK  bool   `json:"lastReviewOK"`
	LastEvolveAt  int64  `json:"lastEvolveAt,omitempty"`
	LastGenesisAt int64  `json:"lastGenesisAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	LastErrorAt   int64  `json:"lastErrorAt,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
	// Attempt counters make "low threshold, high gate" observable: operators can
	// tell whether the loop is not trying, trying but skipping weak transcripts,
	// or trying and being rejected by validation.
	ReviewAttempts       int `json:"reviewAttempts,omitempty"`
	ReviewSkips          int `json:"reviewSkips,omitempty"`
	ValidationRejections int `json:"validationRejections,omitempty"`
	// GenesisSinceEvolve counts new skills created since the last event-driven
	// evolve fired. Persisted so the count survives the frequent SIGUSR1
	// restarts (the failure mode behind #2035). Event-trigger for evolve.
	GenesisSinceEvolve int `json:"genesisSinceEvolve,omitempty"`
}

// Usage record sources. Only real-use records feed the evolver's success-rate
// gate; the skill-review fork's own introspection (consult + verdict) must not —
// conflating the loop's self-activity with the skill's real-world outcome is
// what drove the email-analysis evolve thrash (PR #2328). Legacy records carry
// no Source; ingest falls back to the session prefix for those.
const (
	UsageSourceReal          = "real"           // genuine use in a client/cron turn
	UsageSourceReviewVerdict = "review-verdict" // the review fork's no-op/evolve judgment
	UsageSourceReviewConsult = "review-consult" // the review fork reading a skill to judge it
)

// UsageRecord represents a single skill usage event.
type UsageRecord struct {
	SkillName    string             `json:"skillName"`
	SessionKey   string             `json:"sessionKey"`
	Success      bool               `json:"success"`
	ErrorMsg     string             `json:"errorMsg,omitempty"`
	FailureTrace *UsageFailureTrace `json:"failureTrace,omitempty"`
	UsedAt       int64              `json:"usedAt"`           // unix millis
	Source       string             `json:"source,omitempty"` // "" = legacy (classified by session prefix)
}

// UsageFailureTrace is the structured failure evidence carried by a real skill
// use. ErrorMsg is still kept for backward compatibility; this trace gives
// Propus/Self-Harness a stable terminal signature and the tool boundary that
// produced it when transcript data is available.
type UsageFailureTrace struct {
	Signature      string `json:"signature,omitempty"`
	TerminalCause  string `json:"terminalCause,omitempty"`
	CausalStatus   string `json:"causalStatus,omitempty"`
	AgentMechanism string `json:"agentMechanism,omitempty"`
	ToolName       string `json:"toolName,omitempty"`
	ToolInput      string `json:"toolInput,omitempty"`
	ToolOutput     string `json:"toolOutput,omitempty"`
	ToolError      bool   `json:"toolError,omitempty"`
	ErrorMsg       string `json:"errorMsg,omitempty"`
}

// UsageStats aggregates usage metrics for a skill.
type UsageStats struct {
	SkillName           string              `json:"skillName"`
	TotalUses           int                 `json:"totalUses"`
	SuccessCount        int                 `json:"successCount"`
	FailureCount        int                 `json:"failureCount"`
	SuccessRate         float64             `json:"successRate"`
	LastUsed            int64               `json:"lastUsed,omitempty"`
	RecentErrors        []string            `json:"recentErrors,omitempty"`
	RecentFailureTraces []UsageFailureTrace `json:"recentFailureTraces,omitempty"`
}

const defaultSkillEvolutionEvidenceWindowDays = 7
const evolveRollbackReason = "post-evolve rollback fired"

// Tracker records and queries skill usage for evolution decisions.
type Tracker struct {
	logger              *slog.Logger
	mu                  sync.Mutex
	usagePath           string
	logPath             string
	curatorPath         string
	livenessPath        string
	rejectedPath        string
	opportunityPath     string
	optimizerMemoryPath string
	validationPath      string
	selfCorrectionPath  string

	// In-memory aggregated stats, rebuilt from JSONL on startup.
	stats               map[string]*usageAgg
	recentErrors        map[string][]string            // skill -> last 5 error messages
	recentFailureTraces map[string][]UsageFailureTrace // skill -> last 5 structured failures

	// Event-driven evolve trigger (set via SetEvolveTrigger). When N new
	// skills accumulate, evolveTrigger is fired in the background. All guarded
	// by mu.
	evolveTrigger   func()
	evolveThreshold int
	evolveMinGap    time.Duration
	triggerInflight bool

	// Post-evolve rollback watch (set via SetRollback). After a skill is
	// evolved (LogEvolve), its next uses are watched; rollbackThreshold failures
	// within the observation window fire `rollback` to revert the evolution
	// (windowed, not strict-consecutive, so an alternating pass/fail regression
	// still trips it). Guarded by mu. postEvolve is empty at startup (populated
	// only by runtime LogEvolve), so replaying usage history never rolls back.
	rollback          func(skillName string)
	rollbackThreshold int
	postEvolve        map[string]*evolveWatch

	// Cached evolve-health summary (EvolutionHealth) so frequent /health polls
	// don't rescan the growing lifecycle log every call. Guarded by mu.
	evoHealth   EvolutionHealthSummary
	evoHealthAt time.Time
}

// evolveWatch tracks consecutive failures of a skill since its last evolve.
type evolveWatch struct {
	version string
	audit   HarnessEditAudit
	// postUses counts real uses observed since the evolve shipped (the rollback
	// window); postFails counts failures among them; recurred counts failures
	// matching the evolve's target signature (surfaced in the rollback log).
	postUses  int
	postFails int
	recurred  int
}

// usageAgg holds running aggregates per skill.
type usageAgg struct {
	total    int
	success  int
	failure  int
	lastUsed int64
}

// NewTracker opens or creates the skill usage tracker.
func NewTracker(logger *slog.Logger) (*Tracker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: home dir: %w", err)
	}
	dir := filepath.Join(home, ".deneb", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("genesis-tracker: mkdir: %w", err)
	}

	t := &Tracker{
		logger:              logger,
		usagePath:           filepath.Join(dir, "skill_usage.jsonl"),
		logPath:             filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:         filepath.Join(dir, "skill_curator_state.json"),
		livenessPath:        filepath.Join(dir, "skill_liveness.json"),
		rejectedPath:        filepath.Join(dir, "skill_rejected_edits.jsonl"),
		opportunityPath:     filepath.Join(dir, "skill_opportunities.jsonl"),
		optimizerMemoryPath: filepath.Join(dir, "skill_optimizer_memory.json"),
		validationPath:      filepath.Join(dir, "skill_validation_cases.jsonl"),
		selfCorrectionPath:  filepath.Join(dir, "self_correction_candidates.jsonl"),
		stats:               make(map[string]*usageAgg),
		recentErrors:        make(map[string][]string),
		recentFailureTraces: make(map[string][]UsageFailureTrace),
		postEvolve:          make(map[string]*evolveWatch),
	}

	// Rebuild in-memory state from existing JSONL.
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load usage: %w", err)
	}
	for _, r := range records {
		t.ingest(r)
	}

	return t, nil
}

// isConsultInfraError reports whether a usage failure was caused by the skills
// consult mechanism itself failing to load the skill (a gateway path/catalog
// bug, e.g. #2125's "tool skills errored") rather than the skill running badly.
// Such failures must not count against a skill's success rate: they pinned
// email-analysis below the evolver's threshold long after the gateway bug was
// fixed, triggering a fresh "fix" every 6h that chased an error the skill could
// not influence (and over-fit the skill body to that phantom error string).
func isConsultInfraError(errMsg string) bool {
	return strings.Contains(errMsg, "tool skills errored")
}

// isUnactionableLegacyFailure reports legacy failure records that carry neither
// a session nor an error. srv1 had a topsolar-db backlog dominated by these
// empty failures; counting them as real evidence pinned the skill below the
// evolution threshold even though there was nothing a rewrite could learn from.
func isUnactionableLegacyFailure(r UsageRecord) bool {
	return !r.Success &&
		r.Source == "" &&
		strings.TrimSpace(r.SessionKey) == "" &&
		strings.TrimSpace(r.ErrorMsg) == "" &&
		usageFailureTraceFromRecord(r) == nil
}

// reviewSessionPrefix marks sessions spawned by the skill-review fork. The fork
// reads and judges skills as introspection, not real use, so its records (both
// the verdict and the consult turn) must never feed the real-use success rate.
const reviewSessionPrefix = "system:skill-review:"

func isReviewUsageRecord(r UsageRecord) bool {
	switch r.Source {
	case UsageSourceReviewVerdict, UsageSourceReviewConsult:
		return true
	default:
		return strings.HasPrefix(r.SessionKey, reviewSessionPrefix)
	}
}

// isRealUsageRecord reports whether r reflects a genuine, fair execution of the
// skill — the only signal the evolver's success-rate gate should see. Excluded:
// records explicitly tagged as a review source, the skill-review fork's own
// sessions (legacy records carry no Source, so fall back to the session prefix),
// consult-infrastructure failures (the skill could not even be loaded), and
// legacy empty failures with no actionable session/error evidence.
func isRealUsageRecord(r UsageRecord) bool {
	if isReviewUsageRecord(r) {
		return false
	}
	if !r.Success && isConsultInfraError(r.ErrorMsg) {
		return false
	}
	if isUnactionableLegacyFailure(r) {
		return false
	}
	return true
}

func normalizeUsageFailureTrace(record UsageRecord) UsageRecord {
	if record.Success {
		record.FailureTrace = nil
		return record
	}
	trace := usageFailureTraceFromRecord(record)
	if trace == nil {
		return record
	}
	record.FailureTrace = trace
	return record
}

func usageFailureTraceFromRecord(record UsageRecord) *UsageFailureTrace {
	if record.Success {
		return nil
	}
	var trace UsageFailureTrace
	if record.FailureTrace != nil {
		trace = *record.FailureTrace
	}
	trace.ErrorMsg = firstNonBlank(trace.ErrorMsg, record.ErrorMsg)
	trace.Signature = strings.TrimSpace(trace.Signature)
	trace.TerminalCause = strings.TrimSpace(trace.TerminalCause)
	trace.CausalStatus = strings.TrimSpace(trace.CausalStatus)
	trace.AgentMechanism = strings.TrimSpace(trace.AgentMechanism)
	trace.ToolName = truncateRunes(strings.TrimSpace(trace.ToolName), 120)
	trace.ToolInput = truncateRunes(strings.TrimSpace(trace.ToolInput), 1000)
	trace.ToolOutput = truncateRunes(strings.TrimSpace(trace.ToolOutput), 1000)
	trace.ErrorMsg = truncateRunes(strings.TrimSpace(trace.ErrorMsg), 1000)

	classifyText := usageFailureTraceText(trace)
	if strings.TrimSpace(classifyText) == "" {
		return nil
	}
	if trace.Signature == "" || trace.TerminalCause == "" || trace.AgentMechanism == "" {
		signature, terminalCause, mechanism := classifySkillFailure(classifyText)
		if trace.Signature == "" {
			trace.Signature = signature
		}
		if trace.TerminalCause == "" {
			trace.TerminalCause = terminalCause
		}
		if trace.AgentMechanism == "" {
			trace.AgentMechanism = mechanism
		}
	}
	if trace.CausalStatus == "" {
		if trace.ToolName != "" || trace.ToolInput != "" || trace.ToolOutput != "" {
			trace.CausalStatus = "real-use tool trace classified from transcript/error boundary"
		} else {
			trace.CausalStatus = "filtered real-use failure; trace-level causality unavailable"
		}
	}
	if trace.Signature == "" {
		return nil
	}
	return &trace
}

func usageFailureTraceText(trace UsageFailureTrace) string {
	parts := make([]string, 0, 4)
	for _, part := range []string{trace.ErrorMsg, trace.ToolName, trace.ToolInput, trace.ToolOutput} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func usageFailureTraceExample(trace UsageFailureTrace) string {
	var parts []string
	if trace.ToolName != "" {
		parts = append(parts, "tool="+trace.ToolName)
	}
	if trace.ErrorMsg != "" {
		parts = append(parts, "error="+trace.ErrorMsg)
	}
	if trace.ToolOutput != "" {
		parts = append(parts, "output="+trace.ToolOutput)
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateRunes(strings.Join(parts, "; "), 160)
}

// ingest updates in-memory aggregates from a single usage record. Only real-use
// records (isRealUsageRecord) count toward the success-rate aggregate the
// evolver gates on — both live and on startup replay, which also discards the
// historical review/infra backlog that would otherwise re-pollute the rate on
// every restart and pin a healthy skill as a phantom underperformer.
func (t *Tracker) ingest(r UsageRecord) {
	if !isRealUsageRecord(r) {
		return
	}
	agg := t.stats[r.SkillName]
	if agg == nil {
		agg = &usageAgg{}
		t.stats[r.SkillName] = agg
	}
	agg.total++
	if r.Success {
		agg.success++
	} else {
		agg.failure++
	}
	if r.UsedAt > agg.lastUsed {
		agg.lastUsed = r.UsedAt
	}

	if !r.Success && r.ErrorMsg != "" {
		errs := t.recentErrors[r.SkillName]
		errs = append(errs, r.ErrorMsg)
		if len(errs) > 5 {
			errs = errs[len(errs)-5:]
		}
		t.recentErrors[r.SkillName] = errs
	}
	if trace := usageFailureTraceFromRecord(r); trace != nil {
		traces := t.recentFailureTraces[r.SkillName]
		traces = append(traces, *trace)
		if len(traces) > 5 {
			traces = traces[len(traces)-5:]
		}
		t.recentFailureTraces[r.SkillName] = traces
	}
}

// RecordUsage logs a skill usage event.
func (t *Tracker) RecordUsage(record UsageRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.UsedAt == 0 {
		record.UsedAt = time.Now().UnixMilli()
	}
	record = normalizeUsageFailureTrace(record)

	if err := jsonlstore.Append(t.usagePath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append usage: %w", err)
	}
	t.ingest(record)
	t.maybeFireRollbackLocked(record)
	// Curator use-count / staleness must reflect real use only (consistent with
	// the success rate): a review verdict isn't an execution, so it must not bump
	// lastUsed and mask a never-really-used skill from staleness pruning — that
	// masking is what lets dead self-generated skills linger as net-negative cost.
	if isRealUsageRecord(record) {
		if err := t.touchCuratorUsageLocked(record.SkillName, record.UsedAt); err != nil {
			return err
		}
	}
	return nil
}

// maybeFireRollbackLocked advances the post-evolve watch for the used skill and
// reverts the evolution when failures reach rollbackThreshold within the
// observation window. Windowed rather than strict-consecutive: a single success
// no longer clears the watch, so a regression that alternates pass/fail (the
// sub-threshold coupling HarnessX §6.6 documents) still rolls back. Caller holds
// t.mu; the callback runs lock-free in a recovered goroutine (it re-enters the
// tracker via LogEvolveRolledBack, so it must not run under the lock).
func (t *Tracker) maybeFireRollbackLocked(r UsageRecord) {
	w := t.postEvolve[r.SkillName]
	if w == nil || t.rollback == nil {
		return
	}
	// Only real use is fair evidence for or against a fresh evolve: a review-fork
	// record or a consult-infra failure must neither advance the failure count
	// (reverting a good evolve) nor count toward proving it out.
	if !isRealUsageRecord(r) {
		return
	}
	w.postUses++
	if !r.Success {
		w.postFails++
		if selfHarnessTargetRecurred(w.audit, r) {
			w.recurred++
		}
	}
	// Regression: threshold failures within the window — fire the rollback.
	if w.postFails >= t.rollbackThreshold {
		recurred := w.recurred
		delete(t.postEvolve, r.SkillName)
		fn := t.rollback
		skill := r.SkillName
		go func() {
			defer func() {
				if rec := recover(); rec != nil && t.logger != nil {
					t.logger.Error("genesis: rollback callback panic", "panic", rec)
				}
			}()
			if t.logger != nil {
				t.logger.Info("genesis: post-evolve regression window tripped, rolling back",
					"skill", skill, "fails", t.rollbackThreshold, "targetRecurrences", recurred)
			}
			fn(skill)
		}()
		return
	}
	// Proven out: the evolve survived the observation window below the failure
	// threshold. Close the loop with a positive confirmation (#1) — the watch
	// used to be dropped silently here, so a shipped evolve only ever produced a
	// rollback event, never an "it held up" event. Now every shipped evolve ends
	// in either evolve_rolled_back or evolve_confirmed.
	if w.postUses >= t.rollbackWindowLocked() {
		uses, fails, recurred := w.postUses, w.postFails, w.recurred
		audit := w.audit
		skill := r.SkillName
		delete(t.postEvolve, r.SkillName)
		go func() {
			defer func() {
				if rec := recover(); rec != nil && t.logger != nil {
					t.logger.Error("genesis: confirm callback panic", "panic", rec)
				}
			}()
			t.confirmEvolve(skill, audit, uses, fails, recurred)
		}()
	}
}

// rollbackWindowLocked is the number of post-evolve real uses observed before an
// evolve that has not hit the failure threshold is considered proven and its
// watch cleared. Twice the threshold, so reaching the threshold means at least a
// ~50% failure rate over the window. Caller holds t.mu.
func (t *Tracker) rollbackWindowLocked() int {
	if t.rollbackThreshold <= 0 {
		return 0
	}
	return t.rollbackThreshold * 2
}

func selfHarnessTargetRecurred(audit HarnessEditAudit, r UsageRecord) bool {
	target := normalizedSelfHarnessSignature(audit.TargetSignature)
	if target == "" || r.Success || !isRealUsageRecord(r) {
		return false
	}
	trace := usageFailureTraceFromRecord(r)
	if trace == nil {
		return false
	}
	return selfHarnessSignatureMatches(target, normalizedSelfHarnessSignature(trace.Signature))
}

// Stats returns aggregated usage stats for a skill.
func (t *Tracker) Stats(skillName string) (*UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.getStatsLocked(skillName), nil
}

func (t *Tracker) getStatsLocked(skillName string) *UsageStats {
	stats := &UsageStats{SkillName: skillName}
	agg := t.stats[skillName]
	if agg == nil {
		return stats
	}
	stats.TotalUses = agg.total
	stats.SuccessCount = agg.success
	stats.FailureCount = agg.failure
	stats.LastUsed = agg.lastUsed
	if agg.total > 0 {
		stats.SuccessRate = float64(agg.success) / float64(agg.total)
	}
	if errs := t.recentErrors[skillName]; len(errs) > 0 {
		stats.RecentErrors = make([]string, len(errs))
		copy(stats.RecentErrors, errs)
	}
	if traces := t.recentFailureTraces[skillName]; len(traces) > 0 {
		stats.RecentFailureTraces = make([]UsageFailureTrace, len(traces))
		copy(stats.RecentFailureTraces, traces)
	}
	return stats
}

// EvolutionEvidenceStats returns the bounded usage evidence that the automatic
// evolver is allowed to act on. It intentionally differs from Stats(), which is
// a lifetime observability aggregate: stale failures should remain visible in
// status output, but they must not keep triggering fresh rewrites forever.
func (t *Tracker) EvolutionEvidenceStats(skillName string) (*UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	statsBySkill, err := t.evolutionEvidenceStatsBySkillLocked(time.Now())
	if err != nil {
		return nil, err
	}
	if stats := statsBySkill[skillName]; stats != nil {
		return stats, nil
	}
	return &UsageStats{SkillName: skillName}, nil
}

// ListAllStats returns usage stats for all tracked skills.
func (t *Tracker) ListAllStats() ([]UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make([]UsageStats, 0, len(t.stats))
	for name := range t.stats {
		result = append(result, *t.getStatsLocked(name))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalUses > result[j].TotalUses
	})
	return result, nil
}

// Evolve-health window + thrash thresholds. A single skill eating most of a
// non-trivial recent evolve budget is the thrash signature (email-analysis ran
// 6 evolves in ~2 days, all one skill, undetected — PR #2328).
const (
	evolutionHealthWindow   = 7 * 24 * time.Hour
	evolutionHealthCacheTTL = 60 * time.Second
	evolutionThrashCooldown = 24 * time.Hour
	// Thrash = one skill re-evolved >= evolutionThrashMinEvolves times in the
	// window AND accounting for >= evolutionThrashDominancePct of all evolves. A
	// good evolve should stick, so a skill needing 3+ fixes in a week while
	// dominating the budget is the non-convergence signature (email-analysis hit
	// 6). Tuned to flag early; a false positive only shows an operator a glance.
	evolutionThrashMinEvolves   = 3
	evolutionThrashDominancePct = 60
)

// EvolutionHealthSummary surfaces evolve-loop productivity for /health so a
// silent thrash (the loop burning its budget re-evolving one skill) is visible
// without log spelunking — the failure mode behind every past silent death.
type EvolutionHealthSummary struct {
	Evolves7d               int    `json:"evolves7d"`
	EvolveRejected7d        int    `json:"evolveRejected7d"`
	EvolveRolledBack7d      int    `json:"evolveRolledBack7d"`
	Genesis7d               int    `json:"genesis7d"`
	DistinctSkillsEvolved7d int    `json:"distinctSkillsEvolved7d"`
	TopEvolvedSkill         string `json:"topEvolvedSkill,omitempty"`
	TopEvolvedCount         int    `json:"topEvolvedCount,omitempty"`
	LastRejectedSkill       string `json:"lastRejectedSkill,omitempty"`
	LastRejectedReason      string `json:"lastRejectedReason,omitempty"`
	Thrash                  bool   `json:"thrash"`
	ThrashCooldownUntil     int64  `json:"thrashCooldownUntil,omitempty"`
}

// EvolutionHealth summarizes evolve/genesis activity over the last 7 days from
// the persisted lifecycle log (so the counts survive the frequent SIGUSR1
// restarts). Cached for evolutionHealthCacheTTL to bound rescans of the growing
// log under frequent /health polls.
func (t *Tracker) EvolutionHealth() EvolutionHealthSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if !t.evoHealthAt.IsZero() && now.Sub(t.evoHealthAt) < evolutionHealthCacheTTL {
		return t.evoHealth
	}
	t.evoHealth = t.computeEvolutionHealthLocked(now)
	t.evoHealthAt = now
	return t.evoHealth
}

func (t *Tracker) computeEvolutionHealthLocked(now time.Time) EvolutionHealthSummary {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return EvolutionHealthSummary{}
	}
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	perSkill := map[string]int{}
	var s EvolutionHealthSummary
	for _, e := range entries {
		if e.CreatedAt < cutoff {
			continue
		}
		switch e.Type {
		case "evolved":
			s.Evolves7d++
			if e.SkillName != "" {
				perSkill[e.SkillName]++
			}
		case "evolve_rejected":
			s.EvolveRejected7d++
			if s.LastRejectedSkill == "" {
				s.LastRejectedSkill = e.SkillName
				s.LastRejectedReason = e.Reason
			}
		case "evolve_rolled_back":
			s.EvolveRolledBack7d++
		case "genesis", "": // legacy genesis entries have no Type
			s.Genesis7d++
		}
	}
	s.DistinctSkillsEvolved7d = len(perSkill)
	for name, n := range perSkill {
		if n > s.TopEvolvedCount {
			s.TopEvolvedCount, s.TopEvolvedSkill = n, name
		}
	}
	if s.TopEvolvedCount >= evolutionThrashMinEvolves &&
		s.TopEvolvedCount*100 >= s.Evolves7d*evolutionThrashDominancePct {
		s.Thrash = true
		for _, e := range entries {
			if e.CreatedAt < cutoff || e.Type != "evolved" || e.SkillName != s.TopEvolvedSkill {
				continue
			}
			cooldownUntil := time.UnixMilli(e.CreatedAt).Add(evolutionThrashCooldown).UnixMilli()
			if cooldownUntil > s.ThrashCooldownUntil {
				s.ThrashCooldownUntil = cooldownUntil
			}
		}
	}
	return s
}

// AgentSkillValueSummary counts agent-created (genesis) skills and how many have
// zero real uses. Self-generated skills are net-negative unless they earn their
// keep (SoK -1.3pp); an unused one is pure cost (catalog + prompt tokens with no
// payoff). Surfaced on /health so dead weight is visible without inspecting the
// curator file. Real-use only: the curator use-count is now gated to genuine
// executions, so a verdict-only skill correctly reads as unused.
func (t *Tracker) AgentSkillValueSummary() (total, unused int) {
	report, err := t.SkillCuratorReport("")
	if err != nil {
		return 0, 0
	}
	for _, r := range report {
		if r.CreatedBy != SkillCuratorCreatedByAgent {
			continue
		}
		total++
		if r.UseCount == 0 {
			unused++
		}
	}
	return total, unused
}

// LifecycleLogEntry is the combined JSONL view for genesis and evolution
// proposal events. Older genesis entries may not have Type populated; readers
// normalize those to "genesis".
type LifecycleLogEntry struct {
	Type             string            `json:"type,omitempty"`
	SkillName        string            `json:"skillName,omitempty"`
	Source           string            `json:"source,omitempty"`
	SessionKey       string            `json:"sessionKey,omitempty"`
	CreatedAt        int64             `json:"createdAt,omitempty"`
	Category         string            `json:"category,omitempty"`
	Description      string            `json:"description,omitempty"`
	Candidate        string            `json:"candidate,omitempty"`
	Route            string            `json:"route,omitempty"`
	Evidence         string            `json:"evidence,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	Executed         bool              `json:"executed,omitempty"`
	Result           string            `json:"result,omitempty"`
	NewVersion       string            `json:"newVersion,omitempty"`
	SelfHarnessAudit *HarnessEditAudit `json:"selfHarnessAudit,omitempty"`
}

// genesisLogEntry is the JSONL format for genesis log events.
type genesisLogEntry struct {
	Type        string `json:"type"`
	SkillName   string `json:"skillName"`
	Source      string `json:"source"`
	SessionKey  string `json:"sessionKey,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}

// EvolutionProposalRecord records an agent decision about whether recent
// experience should become a new skill, evolve an existing skill, or be skipped.
type EvolutionProposalRecord struct {
	Type       string `json:"type"`
	Candidate  string `json:"candidate"`
	Route      string `json:"route"`
	SessionKey string `json:"sessionKey,omitempty"`
	SkillName  string `json:"skillName,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Executed   bool   `json:"executed,omitempty"`
	Result     string `json:"result,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

// LogGenesis records that a skill was auto-generated.
func (t *Tracker) LogGenesis(skillName, source, sessionKey, category, description string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	createdAt := time.Now().UnixMilli()
	if err := jsonlstore.Append(t.logPath, genesisLogEntry{
		Type:        "genesis",
		SkillName:   skillName,
		Source:      source,
		SessionKey:  sessionKey,
		CreatedAt:   createdAt,
		Category:    category,
		Description: description,
	}); err != nil {
		return err
	}
	t.recordEvolutionActivityLocked(SkillActivityGenesis, true, "")
	t.maybeFireEvolveLocked()
	return t.markSkillAgentCreatedLocked(skillName, createdAt)
}

// LogEvolutionProposal records a Propus routing decision.
func (t *Tracker) LogEvolutionProposal(record EvolutionProposalRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.Type == "" {
		record.Type = "evolution_proposal"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if err := jsonlstore.Append(t.logPath, record); err != nil {
		return err
	}
	return nil
}

// evolveLogEntry is the JSONL format for evolve outcome events. Unlike the
// curator's MarkSkillPatched (which only tracks agent-created skills), this
// records every committed or rejected evolve — including ones on user-authored
// skills — so the native client can render a complete evolution timeline.
type evolveLogEntry struct {
	Type             string            `json:"type"` // "evolved" | "evolve_rejected" | "evolve_rolled_back" | "evolve_confirmed"
	SkillName        string            `json:"skillName"`
	NewVersion       string            `json:"newVersion,omitempty"`
	Description      string            `json:"description,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	CreatedAt        int64             `json:"createdAt"`
	SelfHarnessAudit *HarnessEditAudit `json:"selfHarnessAudit,omitempty"`
}

// LogEvolve records a committed skill evolution (rewrite applied to disk) and
// starts the post-evolve rollback watch so the next few uses are monitored.
func (t *Tracker) LogEvolve(skillName, newVersion, description string) error {
	return t.LogEvolveWithAudit(skillName, newVersion, description, HarnessEditAudit{})
}

// LogEvolveWithAudit records a committed skill evolution with structured
// Self-Harness transition metadata.
func (t *Tracker) LogEvolveWithAudit(skillName, newVersion, description string, audit HarnessEditAudit) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	if t.rollbackThreshold > 0 {
		t.postEvolve[skillName] = &evolveWatch{version: newVersion, audit: audit}
	}
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolved",
		SkillName:        skillName,
		NewVersion:       newVersion,
		Description:      description,
		CreatedAt:        now,
		SelfHarnessAudit: audit.ptr(),
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "accepted", description, now)
	return nil
}

// LogEvolveRolledBack records that an evolution was reverted after it regressed
// (rollbackThreshold post-evolve failures within the observation window).
func (t *Tracker) LogEvolveRolledBack(skillName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:      "evolve_rolled_back",
		SkillName: skillName,
		Reason:    evolveRollbackReason,
		CreatedAt: now,
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "rolled_back", evolveRollbackReason, now)
	return nil
}

// confirmEvolve records that an evolve survived its post-evolve observation
// window below the rollback threshold (#1 falsification closure). It runs
// lock-free off the tracker (LogEvolveConfirmed re-enters t.mu). A clean
// confirmation means the target failure signature never recurred and the skill
// logged no failures within the window; otherwise it is partial (held up
// overall, but the targeted mechanism still surfaced below threshold).
func (t *Tracker) confirmEvolve(skillName string, audit HarnessEditAudit, uses, fails, recurred int) {
	clean := fails == 0 && recurred == 0
	if t.logger != nil {
		t.logger.Info("genesis: post-evolve window cleared, evolve confirmed",
			"skill", skillName, "uses", uses, "fails", fails, "targetRecurrences", recurred,
			"clean", clean, "expectedBehaviorChange", audit.ExpectedBehaviorChange)
	}
	if err := t.LogEvolveConfirmed(skillName, audit, clean); err != nil && t.logger != nil {
		t.logger.Warn("genesis: confirm lifecycle log failed", "skill", skillName, "error", err)
	}
}

// LogEvolveConfirmed records that an evolution proved out over its post-evolve
// observation window (#1). It is the positive counterpart to LogEvolveRolledBack,
// so the lifecycle log now carries the outcome of every shipped evolve. clean
// reports whether the target failure signature never recurred and no failures
// occurred within the window.
func (t *Tracker) LogEvolveConfirmed(skillName string, audit HarnessEditAudit, clean bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	reason := "partial: held up but target signature recurred below threshold"
	if clean {
		reason = "clean: target signature did not recur within the window"
	}
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolve_confirmed",
		SkillName:        skillName,
		Reason:           reason,
		CreatedAt:        time.Now().UnixMilli(),
		SelfHarnessAudit: audit.ptr(),
	})
}

// LogEvolveRejected records an evolve attempt whose rewrite the self-test
// refused to commit (the original skill was kept).
func (t *Tracker) LogEvolveRejected(skillName, reason string) error {
	return t.LogEvolveRejectedWithAudit(skillName, reason, HarnessEditAudit{})
}

// LogEvolveRejectedWithAudit records a rejected skill evolution with structured
// Self-Harness transition metadata from the candidate that failed validation.
func (t *Tracker) LogEvolveRejectedWithAudit(skillName, reason string, audit HarnessEditAudit) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolve_rejected",
		SkillName:        skillName,
		Reason:           reason,
		CreatedAt:        now,
		SelfHarnessAudit: audit.ptr(),
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "rejected", reason, now)
	return nil
}

// RecentLifecycleLog returns recent genesis/proposal events, newest first.
func (t *Tracker) RecentLifecycleLog(limit int) ([]LifecycleLogEntry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load lifecycle log: %w", err)
	}
	for i := range entries {
		if entries[i].Type == "" {
			entries[i].Type = "genesis"
		}
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// SkillsNeedingEvolution returns skills with recent unresolved failure rates.
func (t *Tracker) SkillsNeedingEvolution(minUses int, maxSuccessRate float64) ([]UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	statsBySkill, err := t.evolutionEvidenceStatsBySkillLocked(time.Now())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	evoHealth := t.computeEvolutionHealthLocked(now)

	var candidates []UsageStats
	for _, stats := range statsBySkill {
		s := *stats
		if s.TotalUses < minUses || s.FailureCount == 0 || s.SuccessRate > maxSuccessRate {
			continue
		}
		if evoHealth.Thrash &&
			s.SkillName == evoHealth.TopEvolvedSkill &&
			evoHealth.ThrashCooldownUntil > now.UnixMilli() {
			continue
		}
		candidates = append(candidates, s)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TotalUses > candidates[j].TotalUses
	})
	return candidates, nil
}

func (t *Tracker) evolutionEvidenceStatsBySkillLocked(now time.Time) (map[string]*UsageStats, error) {
	lastAttemptAt, err := t.lastEvolutionAttemptBySkillLocked()
	if err != nil {
		return nil, err
	}
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load usage for evolution evidence: %w", err)
	}

	statsBySkill := make(map[string]*UsageStats)
	for _, r := range records {
		if r.SkillName == "" || !isRealUsageRecord(r) {
			continue
		}
		if cutoff := evolutionEvidenceCutoff(now, lastAttemptAt[r.SkillName]); cutoff > 0 && r.UsedAt <= cutoff {
			continue
		}
		stats := statsBySkill[r.SkillName]
		if stats == nil {
			stats = &UsageStats{SkillName: r.SkillName}
			statsBySkill[r.SkillName] = stats
		}
		addUsageRecordToStats(stats, r)
	}
	for _, stats := range statsBySkill {
		if stats.TotalUses > 0 {
			stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalUses)
		}
	}
	return statsBySkill, nil
}

func evolutionEvidenceCutoff(now time.Time, lastAttemptAt int64) int64 {
	cutoff := lastAttemptAt
	if window := skillEvolutionEvidenceWindow(); window > 0 {
		windowCutoff := now.Add(-window).UnixMilli()
		if windowCutoff > cutoff {
			cutoff = windowCutoff
		}
	}
	return cutoff
}

func skillEvolutionEvidenceWindow() time.Duration {
	days := envInt("DENEB_SKILL_EVOLVE_EVIDENCE_DAYS", defaultSkillEvolutionEvidenceWindowDays)
	if days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

func addUsageRecordToStats(stats *UsageStats, r UsageRecord) {
	stats.TotalUses++
	if r.Success {
		stats.SuccessCount++
	} else {
		stats.FailureCount++
	}
	if r.UsedAt > stats.LastUsed {
		stats.LastUsed = r.UsedAt
	}
	if !r.Success && r.ErrorMsg != "" {
		stats.RecentErrors = append(stats.RecentErrors, r.ErrorMsg)
		if len(stats.RecentErrors) > 5 {
			stats.RecentErrors = stats.RecentErrors[len(stats.RecentErrors)-5:]
		}
	}
	if trace := usageFailureTraceFromRecord(r); trace != nil {
		stats.RecentFailureTraces = append(stats.RecentFailureTraces, *trace)
		if len(stats.RecentFailureTraces) > 5 {
			stats.RecentFailureTraces = stats.RecentFailureTraces[len(stats.RecentFailureTraces)-5:]
		}
	}
}

func (t *Tracker) lastEvolutionAttemptBySkillLocked() (map[string]int64, error) {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load lifecycle log for evolution candidates: %w", err)
	}
	out := make(map[string]int64)
	for _, entry := range entries {
		if entry.SkillName == "" || !isEvolutionAttemptType(entry.Type) {
			continue
		}
		if entry.CreatedAt > out[entry.SkillName] {
			out[entry.SkillName] = entry.CreatedAt
		}
	}
	return out, nil
}

func isEvolutionAttemptType(typ string) bool {
	switch typ {
	case "evolved", "evolve_rejected", "evolve_rolled_back":
		return true
	default:
		return false
	}
}

// RecordEvolutionActivity updates the Propus liveness heartbeat.
// kind is one of SkillActivityReview/Evolve/Genesis. ok=false also records the
// error so an operator can see WHY the loop stalled. Best-effort: a liveness
// write failure must never break the caller (this is observability, not state).
func (t *Tracker) RecordEvolutionActivity(kind string, ok bool, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordEvolutionActivityLocked(kind, ok, errMsg)
}

func (t *Tracker) recordEvolutionActivityLocked(kind string, ok bool, errMsg string) {
	state, err := t.loadLivenessLocked()
	if err != nil {
		// Start from a clean heartbeat rather than dropping the update.
		state = &SkillLivenessState{}
	}
	now := time.Now().UnixMilli()
	metricOnly := false
	switch kind {
	case SkillActivityReview:
		state.LastReviewAt = now
		state.LastReviewOK = ok
	case SkillActivityReviewAttempt:
		state.ReviewAttempts++
		metricOnly = true
	case SkillActivityReviewSkipped:
		state.ReviewSkips++
		metricOnly = true
	case SkillActivityValidationRejected:
		state.ValidationRejections++
		metricOnly = true
	case SkillActivityEvolve:
		state.LastEvolveAt = now
	case SkillActivityGenesis:
		state.LastGenesisAt = now
	}
	if !metricOnly && !ok && errMsg != "" {
		// Truncate by rune, not byte: this surfaces in /health JSON, and a
		// byte slice can split a multi-byte UTF-8 sequence into replacement runes.
		state.LastError = truncateRunes(errMsg, 200)
		state.LastErrorAt = now
	} else if !metricOnly && ok {
		// A successful activity clears a stale error so /health doesn't keep
		// surfacing a failure that has since recovered (false-red).
		state.LastError = ""
		state.LastErrorAt = 0
	}
	if writeErr := t.saveLivenessLocked(state); writeErr != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: liveness write failed", "error", writeErr)
	}
}

// LivenessSnapshot returns the current Propus heartbeat for /health.
func (t *Tracker) LivenessSnapshot() SkillLivenessState {
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.loadLivenessLocked()
	if err != nil || state == nil {
		return SkillLivenessState{}
	}
	return *state
}

// SetEvolveTrigger wires the event-driven evolve. After `threshold` new skills
// are created (counted across restarts via the persisted sidecar), `fn` runs in
// the background; `minGap` suppresses a re-fire too soon after the previous
// evolve. threshold<=0 disables the trigger.
func (t *Tracker) SetEvolveTrigger(fn func(), threshold int, minGap time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evolveTrigger = fn
	t.evolveThreshold = threshold
	t.evolveMinGap = minGap
}

// SetRollback wires the post-evolve rollback. After a skill is evolved, its
// next uses are watched; `threshold` failures within the observation window
// (windowed, not strict-consecutive) fire `fn` to revert the evolution.
// threshold<=0 disables the watch.
func (t *Tracker) SetRollback(fn func(skillName string), threshold int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollback = fn
	t.rollbackThreshold = threshold
}

// maybeFireEvolveLocked bumps the genesis counter and fires the evolve trigger
// in the background when it reaches the threshold and minGap has elapsed.
// Caller holds t.mu. The trigger (typically EvolutionTask.Run) updates
// LastEvolveAt itself, which feeds the next minGap check.
func (t *Tracker) maybeFireEvolveLocked() {
	if t.evolveTrigger == nil || t.evolveThreshold <= 0 {
		return
	}
	state, err := t.loadLivenessLocked()
	if err != nil {
		return
	}
	state.GenesisSinceEvolve++
	fire := false
	if state.GenesisSinceEvolve >= t.evolveThreshold && !t.triggerInflight {
		gapOK := t.evolveMinGap <= 0 || state.LastEvolveAt == 0 ||
			time.Since(time.UnixMilli(state.LastEvolveAt)) >= t.evolveMinGap
		if gapOK {
			state.GenesisSinceEvolve = 0
			t.triggerInflight = true
			fire = true
		}
	}
	if err := t.saveLivenessLocked(state); err != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: liveness counter write failed", "error", err)
	}
	if !fire {
		return
	}
	fn := t.evolveTrigger
	go func() {
		defer func() {
			if r := recover(); r != nil && t.logger != nil {
				t.logger.Error("genesis: evolve trigger panic", "panic", r)
			}
			t.mu.Lock()
			t.triggerInflight = false
			t.mu.Unlock()
		}()
		fn()
	}()
}

func (t *Tracker) loadLivenessLocked() (*SkillLivenessState, error) {
	state := &SkillLivenessState{}
	if t.livenessPath == "" {
		return state, nil
	}
	data, err := os.ReadFile(t.livenessPath)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: read liveness: %w", err)
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("genesis-tracker: parse liveness: %w", err)
	}
	return state, nil
}

func (t *Tracker) saveLivenessLocked(state *SkillLivenessState) error {
	if t.livenessPath == "" || state == nil {
		return nil
	}
	state.UpdatedAt = time.Now().UnixMilli()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("genesis-tracker: encode liveness: %w", err)
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(t.livenessPath, data, &atomicfile.Options{Perm: 0o600, Fsync: true})
}

// Close is a no-op (JSONL files are opened/closed per write).
func (t *Tracker) Close() error {
	return nil
}
