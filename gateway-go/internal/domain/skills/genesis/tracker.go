package genesis

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Propus activity kinds for the liveness heartbeat.
const (
	skillActivityReview             = "review"
	SkillActivityReviewAttempt      = "review_attempt"
	SkillActivityReviewSkipped      = "review_skipped"
	SkillActivityValidationRejected = "validation_rejected"
	skillActivityEvolve             = "evolve"
	skillActivityGenesis            = "genesis"
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
	usageSourceReviewConsult = "review-consult" // the review fork reading a skill to judge it
	usageSourceWorkout       = "workout"        // synthetic exercise lane (workout.go) — evidence only, never real usage
)

// Delivery and Exercised values carried by UsageRecord. Mirrors of the chatport
// constants that produce them; genesis owns the on-disk vocabulary.
const (
	UsageDeliveryAutoLoad  = "auto-load"
	UsageDeliveryModelRead = "model-read"

	UsageExercisedYes     = "yes"
	UsageExercisedNo      = "no"
	UsageExercisedUnknown = "unknown"
)

// UsageRecord represents a single skill usage event.
type UsageRecord struct {
	SkillName  string `json:"skillName"`
	SessionKey string `json:"sessionKey"`
	// Model is the resolved LLM id the turn ran on. Failure clusters gain a
	// per-model dimension from it (Self-Harness: model-specific weaknesses need
	// model-specific fixes); empty on legacy rows.
	Model        string             `json:"model,omitempty"`
	Success      bool               `json:"success"`
	ErrorMsg     string             `json:"errorMsg,omitempty"`
	FailureTrace *UsageFailureTrace `json:"failureTrace,omitempty"`
	UsedAt       int64              `json:"usedAt"`           // unix millis
	Source       string             `json:"source,omitempty"` // "" = legacy (classified by session prefix)
	// Delivery and Exercised locate the outcome on the skill's path
	// (2608.14036): a consult is recorded with the WHOLE run's success, so
	// without them a skill that was merely loaded and then ignored is
	// indistinguishable from one whose procedure ran and failed. Empty on
	// every record written before attribution existed.
	Delivery  string `json:"delivery,omitempty"`  // auto-load | model-read
	Exercised string `json:"exercised,omitempty"` // yes | no | unknown
}

// UsageFailureTrace is the structured failure evidence carried by a real skill
// use. ErrorMsg is still kept for backward compatibility; this trace gives
// Propus/Self-Harness a stable terminal signature and the tool boundary that
// produced it when transcript data is available.
type UsageFailureTrace struct {
	Signature        string                     `json:"signature,omitempty"`
	TerminalCause    string                     `json:"terminalCause,omitempty"`
	CausalStatus     string                     `json:"causalStatus,omitempty"`
	AgentMechanism   string                     `json:"agentMechanism,omitempty"`
	HarnessDiagnosis *HarnessDimensionDiagnosis `json:"harnessDiagnosis,omitempty"`
	ToolName         string                     `json:"toolName,omitempty"`
	ToolInput        string                     `json:"toolInput,omitempty"`
	ToolOutput       string                     `json:"toolOutput,omitempty"`
	ToolError        bool                       `json:"toolError,omitempty"`
	ErrorMsg         string                     `json:"errorMsg,omitempty"`
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

const (
	defaultSkillEvolutionEvidenceWindowDays = 7
	evolveRollbackReason                    = "post-evolve rollback fired"
)

// Tracker records and queries skill usage for evolution decisions.
//
// Locking: mu and exemplarEmbedderMu are independent and are never acquired
// together. In particular, embedding network calls run after both are released.
type Tracker struct {
	logger              *slog.Logger
	mu                  sync.Mutex
	usagePath           string
	logPath             string
	curatorPath         string
	livenessPath        string
	watchPath           string
	rejectedPath        string
	opportunityPath     string
	optimizerMemoryPath string
	validationPath      string
	ablationPath        string
	selfCorrectionPath  string
	// skillsRoot overrides the managed skills root used to resolve candidate
	// target paths (tests); empty means skills.DefaultManagedSkillsDir().
	skillsRoot string

	// metaLedgerImplausible is the count of meta-revision rows the latest
	// RecentMetaRevisions read excluded as implausible (outside the
	// metaRevisionPlausible time band); metaLedgerImplausibleLogged dedups the
	// Warn so a dirty ledger is logged once per count, not once per read.
	metaLedgerImplausible       atomic.Int64
	metaLedgerImplausibleLogged atomic.Int64

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
	rollback          func(skillName string) bool
	rollbackThreshold int
	postEvolve        map[string]*evolveWatch
	// pendingBaselineTest stashes the e-process verdict captured under lock at
	// the moment a watch resolves, for the Log* call that follows in the
	// resolver callback — avoids threading a new param through the rollback
	// callback signature.
	pendingBaselineTest map[string]*rollbackBaselineTest

	// Cached evolve-health summary (EvolutionHealth) so frequent /health polls
	// don't rescan the growing lifecycle log every call. Guarded by mu.
	evoHealth   EvolutionHealthSummary
	evoHealthAt time.Time

	// exemplarEmbedder is an advisory retrieval dependency only. It may select
	// confirmed exhibits for a prompt but is never consulted by acceptance,
	// validation, adoption, or rollback gates.
	exemplarEmbedderMu sync.RWMutex
	exemplarEmbedder   embedindex.Embedder
}

// SetExemplarEmbedder enables semantic lookup of confirmed cross-skill
// exemplars. It is safe to call during initialization or a runtime rewire.
func (t *Tracker) SetExemplarEmbedder(embedder embedindex.Embedder) {
	if t == nil {
		return
	}
	t.exemplarEmbedderMu.Lock()
	t.exemplarEmbedder = embedder
	t.exemplarEmbedderMu.Unlock()
}

func (t *Tracker) exemplarEmbedderSnapshot() embedindex.Embedder {
	if t == nil {
		return nil
	}
	t.exemplarEmbedderMu.RLock()
	defer t.exemplarEmbedderMu.RUnlock()
	return t.exemplarEmbedder
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
	// Pre-evolve baseline snapshot (RSI P1.5, PACE): captured when the watch
	// opens so a future baseline-aware rollback can test "worse than before"
	// instead of the baseline-blind absolute threshold, and so P3 can audit
	// which rollbacks were true regressions. Recorded now, consumed later.
	baselineUses  int
	baselineFails int
	// ep runs the baseline-aware anytime-valid test alongside the legacy
	// absolute threshold (RSI P1.5, PACE). Default: OBSERVATION MODE — the
	// legacy threshold owns firing while ep's verdict is recorded on every
	// resolving lifecycle entry as an agreement/disagreement label. Once the
	// labels justify it (eProcessCutoverReadiness), the operator flips
	// DENEB_EPROCESS_OWNS_ROLLBACK=1 and ep owns the firing decision — with
	// the threshold verdict still recorded, so labeling never stops.
	ep *EProcess
	// createdAt (unix millis) starts the time-based resolution clock: a watch
	// on a rarely-used skill would otherwise stay open forever (backtest
	// 2026-07-11: ZERO watches ever resolved in production history — the
	// label pipeline was starved by design, not by time).
	createdAt int64
}

// persistedEvolveWatch is the JSON shape of one in-flight rollback watch.
// Persisted so the frequent SIGUSR1 deploy restarts stop silently discarding
// active watches (a shipped evolve then never resolved to rolled_back OR
// confirmed — a confirmed liveness gap).
type persistedEvolveWatch struct {
	Version       string           `json:"version"`
	Audit         HarnessEditAudit `json:"audit,omitempty"`
	PostUses      int              `json:"postUses,omitempty"`
	PostFails     int              `json:"postFails,omitempty"`
	Recurred      int              `json:"recurred,omitempty"`
	BaselineUses  int              `json:"baselineUses,omitempty"`
	BaselineFails int              `json:"baselineFails,omitempty"`
	EProcess      *EProcess        `json:"eProcess,omitempty"`
	CreatedAt     int64            `json:"createdAt,omitempty"`
}

// usageAgg holds running aggregates per skill.
type usageAgg struct {
	total    int
	success  int
	failure  int
	lastUsed int64
}

// NewTracker opens or creates the skill usage tracker under THIS process's
// state dir ({DENEB_STATE_DIR}/data, else ~/.deneb/data). Live-test/dev
// instances must not append to the production JSONL ledgers.
func NewTracker(logger *slog.Logger) (*Tracker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	stateDir := config.ResolveStateDir()
	// The sentence above is a rule, so enforce it: a test binary that resolved
	// the live state dir fails here instead of appending to the ledgers the
	// gateway is using. Test rows once tripped the self-brake's
	// adoption-monotony detector and cost three manual freeze clearings.
	if err := config.GuardProductionState(stateDir, "genesis-tracker"); err != nil {
		return nil, err
	}
	dir := filepath.Join(stateDir, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("genesis-tracker: mkdir: %w", err)
	}

	t := &Tracker{
		logger:              logger,
		usagePath:           filepath.Join(dir, "skill_usage.jsonl"),
		logPath:             filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:         filepath.Join(dir, "skill_curator_state.json"),
		livenessPath:        filepath.Join(dir, "skill_liveness.json"),
		watchPath:           filepath.Join(dir, "skill_evolve_watch.json"),
		rejectedPath:        filepath.Join(dir, "skill_rejected_edits.jsonl"),
		opportunityPath:     filepath.Join(dir, "skill_opportunities.jsonl"),
		optimizerMemoryPath: filepath.Join(dir, "skill_optimizer_memory.json"),
		validationPath:      filepath.Join(dir, "skill_validation_cases.jsonl"),
		ablationPath:        filepath.Join(dir, "skill_ablation.jsonl"),
		selfCorrectionPath:  filepath.Join(dir, "self_correction_candidates.jsonl"),
		stats:               make(map[string]*usageAgg),
		recentErrors:        make(map[string][]string),
		recentFailureTraces: make(map[string][]UsageFailureTrace),
		postEvolve:          make(map[string]*evolveWatch),
		pendingBaselineTest: make(map[string]*rollbackBaselineTest),
	}

	// Rebuild in-memory state from existing JSONL.
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load usage: %w", err)
	}
	for _, r := range records {
		t.ingest(r)
	}

	// Restore in-flight rollback watches (RSI P1.5): before persistence every
	// SIGUSR1 deploy restart silently dropped active watches, so evolves that
	// shipped shortly before a deploy never resolved to rolled_back OR
	// confirmed. Best-effort: a missing/corrupt file starts clean.
	t.loadWatchesLocked()

	return t, nil
}

// StatusRevision fingerprints the persisted state read by skill_lifecycle
// status. It is a cache key only: gates and lifecycle decisions still read the
// ledgers themselves.
func (t *Tracker) StatusRevision() string {
	if t == nil {
		return ""
	}
	paths := []string{
		t.usagePath,
		t.logPath,
		t.curatorPath,
		t.rejectedPath,
		t.opportunityPath,
		t.optimizerMemoryPath,
		t.validationPath,
		t.ablationPath,
		t.selfCorrectionPath,
	}
	var b strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&b, "%s:error:%s\n", filepath.Base(path), err)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d\n", filepath.Base(path), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
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
	case UsageSourceReviewVerdict, usageSourceReviewConsult:
		return true
	default:
		return strings.HasPrefix(r.SessionKey, reviewSessionPrefix)
	}
}

// realUsageSources is the ALLOWLIST of source tags that count as genuine use.
// Fail closed by construction: an empty source is the legacy/RPC real path
// (records on disk from before source tagging, plus the usage-record RPC), and
// UsageSourceReal is the explicit tag; every OTHER non-empty tag — workout,
// review-*, curriculum, or any future lane that forgets it should be excluded —
// is NOT real. The previous exclusion-list form silently counted an unknown
// new tag as real (RSI code eval M3).
var realUsageSources = map[string]bool{
	"":              true, // legacy on-disk + usage-record RPC (untagged real use)
	UsageSourceReal: true,
}

// isRealUsageRecord reports whether r reflects a genuine, fair execution of the
// skill — the only signal the evolver's success-rate gate should see. A source
// tag outside the real allowlist is excluded outright; within it, review-fork
// sessions (legacy records carry no Source, so fall back to the session
// prefix), consult-infrastructure failures (the skill could not even be
// loaded), and legacy empty failures with no actionable evidence are also
// excluded.
func isRealUsageRecord(r UsageRecord) bool {
	if !realUsageSources[r.Source] {
		// Any tagged non-real lane (workout, review-*, curriculum, unknown
		// future source): evidence for clustering at most, never success rate.
		return false
	}
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
	trace.ErrorMsg = textutil.FirstNonBlank(trace.ErrorMsg, record.ErrorMsg)
	trace.Signature = strings.TrimSpace(trace.Signature)
	trace.TerminalCause = strings.TrimSpace(trace.TerminalCause)
	trace.CausalStatus = strings.TrimSpace(trace.CausalStatus)
	trace.AgentMechanism = strings.TrimSpace(trace.AgentMechanism)
	trace.ToolName = genesiscommon.TruncateRunes(strings.TrimSpace(trace.ToolName), 120)
	trace.ToolInput = genesiscommon.TruncateRunes(strings.TrimSpace(trace.ToolInput), 1000)
	trace.ToolOutput = genesiscommon.TruncateRunes(strings.TrimSpace(trace.ToolOutput), 1000)
	trace.ErrorMsg = genesiscommon.TruncateRunes(strings.TrimSpace(trace.ErrorMsg), 1000)

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
	// Derive the dimension from verifier-grounded classification on every read.
	// This gives legacy JSONL rows the same per-case diagnosis and prevents a
	// caller/model-authored diagnosis from becoming authority.
	trace.HarnessDiagnosis = harnessDiagnosisForFailurePattern(
		trace.Signature,
		trace.TerminalCause,
		trace.AgentMechanism,
	)
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
	return genesiscommon.TruncateRunes(strings.Join(parts, "; "), 160)
}
