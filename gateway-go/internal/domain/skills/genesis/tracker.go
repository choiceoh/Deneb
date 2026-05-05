package genesis

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

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
	logger      *slog.Logger
	mu          sync.Mutex
	usagePath   string
	logPath     string
	curatorPath string

	// In-memory aggregated stats, rebuilt from JSONL on startup.
	stats        map[string]*usageAgg
	recentErrors map[string][]string // skill -> last 5 error messages
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

// Close is a no-op (JSONL files are opened/closed per write).
func (t *Tracker) Close() error {
	return nil
}
