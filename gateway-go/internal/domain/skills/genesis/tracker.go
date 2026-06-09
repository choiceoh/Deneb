package genesis

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Self-evolution activity kinds for the liveness heartbeat.
const (
	SkillActivityReview  = "review"
	SkillActivityEvolve  = "evolve"
	SkillActivityGenesis = "genesis"
)

// SkillLivenessState is a persisted heartbeat for the self-evolution loop.
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
	// GenesisSinceEvolve counts new skills created since the last event-driven
	// evolve fired. Persisted so the count survives the frequent SIGUSR1
	// restarts (the failure mode behind #2035). Event-trigger for evolve.
	GenesisSinceEvolve int `json:"genesisSinceEvolve,omitempty"`
}

// UsageRecord represents a single skill usage event.
type UsageRecord struct {
	SkillName  string `json:"skillName"`
	SessionKey string `json:"sessionKey"`
	Success    bool   `json:"success"`
	ErrorMsg   string `json:"errorMsg,omitempty"`
	UsedAt     int64  `json:"usedAt"` // unix millis
}

// UsageStats aggregates usage metrics for a skill.
type UsageStats struct {
	SkillName    string   `json:"skillName"`
	TotalUses    int      `json:"totalUses"`
	SuccessCount int      `json:"successCount"`
	FailureCount int      `json:"failureCount"`
	SuccessRate  float64  `json:"successRate"`
	LastUsed     int64    `json:"lastUsed,omitempty"`
	RecentErrors []string `json:"recentErrors,omitempty"`
}

// Tracker records and queries skill usage for evolution decisions.
type Tracker struct {
	logger       *slog.Logger
	mu           sync.Mutex
	usagePath    string
	logPath      string
	curatorPath  string
	livenessPath string

	// In-memory aggregated stats, rebuilt from JSONL on startup.
	stats        map[string]*usageAgg
	recentErrors map[string][]string // skill -> last 5 error messages

	// Event-driven evolve trigger (set via SetEvolveTrigger). When N new
	// skills accumulate, evolveTrigger is fired in the background. All guarded
	// by mu.
	evolveTrigger   func()
	evolveThreshold int
	evolveMinGap    time.Duration
	triggerInflight bool
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
		logger:       logger,
		usagePath:    filepath.Join(dir, "skill_usage.jsonl"),
		logPath:      filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:  filepath.Join(dir, "skill_curator_state.json"),
		livenessPath: filepath.Join(dir, "skill_liveness.json"),
		stats:        make(map[string]*usageAgg),
		recentErrors: make(map[string][]string),
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

// ingest updates in-memory aggregates from a single usage record.
func (t *Tracker) ingest(r UsageRecord) {
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
}

// RecordUsage logs a skill usage event.
func (t *Tracker) RecordUsage(record UsageRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.UsedAt == 0 {
		record.UsedAt = time.Now().UnixMilli()
	}

	if err := jsonlstore.Append(t.usagePath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append usage: %w", err)
	}
	t.ingest(record)
	if err := t.touchCuratorUsageLocked(record.SkillName, record.UsedAt); err != nil {
		return err
	}
	return nil
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
	return stats
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

// LifecycleLogEntry is the combined JSONL view for genesis and evolution
// proposal events. Older genesis entries may not have Type populated; readers
// normalize those to "genesis".
type LifecycleLogEntry struct {
	Type        string `json:"type,omitempty"`
	SkillName   string `json:"skillName,omitempty"`
	Source      string `json:"source,omitempty"`
	SessionKey  string `json:"sessionKey,omitempty"`
	CreatedAt   int64  `json:"createdAt,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Candidate   string `json:"candidate,omitempty"`
	Route       string `json:"route,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Executed    bool   `json:"executed,omitempty"`
	Result      string `json:"result,omitempty"`
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

// LogEvolutionProposal records a self-evolution routing decision.
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

// SkillsNeedingEvolution returns skills with high failure rates.
func (t *Tracker) SkillsNeedingEvolution(minUses int, maxSuccessRate float64) ([]UsageStats, error) {
	all, err := t.ListAllStats()
	if err != nil {
		return nil, err
	}

	var candidates []UsageStats
	for _, s := range all {
		if s.TotalUses >= minUses && s.SuccessRate <= maxSuccessRate {
			candidates = append(candidates, s)
		}
	}
	return candidates, nil
}

// RecordEvolutionActivity updates the self-evolution liveness heartbeat.
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
	switch kind {
	case SkillActivityReview:
		state.LastReviewAt = now
		state.LastReviewOK = ok
	case SkillActivityEvolve:
		state.LastEvolveAt = now
	case SkillActivityGenesis:
		state.LastGenesisAt = now
	}
	if !ok && errMsg != "" {
		// Truncate by rune, not byte: this surfaces in /health JSON, and a
		// byte slice can split a multi-byte UTF-8 sequence into replacement runes.
		state.LastError = truncateRunes(errMsg, 200)
		state.LastErrorAt = now
	} else if ok {
		// A successful activity clears a stale error so /health doesn't keep
		// surfacing a failure that has since recovered (false-red).
		state.LastError = ""
		state.LastErrorAt = 0
	}
	if writeErr := t.saveLivenessLocked(state); writeErr != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: liveness write failed", "error", writeErr)
	}
}

// LivenessSnapshot returns the current self-evolution heartbeat for /health.
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
