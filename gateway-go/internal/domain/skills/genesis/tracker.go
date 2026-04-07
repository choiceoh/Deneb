package genesis

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // register sqlite3 driver
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
	db     *sql.DB
	logger *slog.Logger
	mu     sync.Mutex
}

// NewTracker opens or creates the skill usage database.
func NewTracker(logger *slog.Logger) (*Tracker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dbPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".deneb", "data")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("genesis-tracker: mkdir: %w", err)
		}
		dbPath = filepath.Join(dir, "skill_usage.db")
	}
	if dbPath == "" {
		return nil, fmt.Errorf("genesis-tracker: cannot determine db path")
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: open db: %w", err)
	}

	if err := initTrackerSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Tracker{db: db, logger: logger}, nil
}

func initTrackerSchema(db *sql.DB) error {
	_, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS skill_usage (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_name  TEXT NOT NULL,
			session_key TEXT NOT NULL,
			success     INTEGER NOT NULL DEFAULT 1,
			error_msg   TEXT DEFAULT '',
			used_at     INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_skill_usage_name ON skill_usage(skill_name);
		CREATE INDEX IF NOT EXISTS idx_skill_usage_time ON skill_usage(used_at);

		CREATE TABLE IF NOT EXISTS skill_genesis_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_name  TEXT NOT NULL,
			source      TEXT NOT NULL DEFAULT 'session',
			session_key TEXT DEFAULT '',
			created_at  INTEGER NOT NULL,
			category    TEXT DEFAULT '',
			description TEXT DEFAULT ''
		);
	`)
	if err != nil {
		return fmt.Errorf("genesis-tracker: init schema: %w", err)
	}
	return nil
}

// RecordUsage logs a skill usage event.
func (t *Tracker) RecordUsage(record UsageRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.UsedAt == 0 {
		record.UsedAt = time.Now().UnixMilli()
	}
	successInt := 0
	if record.Success {
		successInt = 1
	}

	_, err := t.db.ExecContext(context.Background(),
		`INSERT INTO skill_usage (skill_name, session_key, success, error_msg, used_at)
		 VALUES (?, ?, ?, ?, ?)`,
		record.SkillName, record.SessionKey, successInt, record.ErrorMsg, record.UsedAt,
	)
	if err != nil {
		return fmt.Errorf("genesis-tracker: insert usage: %w", err)
	}
	return nil
}

// Stats returns aggregated usage stats for a skill.
func (t *Tracker) Stats(skillName string) (*UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats := &UsageStats{SkillName: skillName}

	row := t.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*), SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),
		       MAX(used_at)
		FROM skill_usage WHERE skill_name = ?`, skillName)

	var lastUsed sql.NullInt64
	if err := row.Scan(&stats.TotalUses, &stats.SuccessCount, &stats.FailureCount, &lastUsed); err != nil {
		return stats, nil // Empty stats for unknown skill.
	}
	if lastUsed.Valid {
		stats.LastUsed = lastUsed.Int64
	}
	if stats.TotalUses > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalUses)
	}

	// Fetch recent errors (last 5).
	rows, err := t.db.QueryContext(context.Background(), `
		SELECT error_msg FROM skill_usage
		WHERE skill_name = ? AND success = 0 AND error_msg != ''
		ORDER BY used_at DESC LIMIT 5`, skillName)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var msg string
			if rows.Scan(&msg) == nil && msg != "" {
				stats.RecentErrors = append(stats.RecentErrors, msg)
			}
		}
	}

	return stats, nil
}

// ListAllStats returns usage stats for all tracked skills.
func (t *Tracker) ListAllStats() ([]UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rows, err := t.db.QueryContext(context.Background(), `
		SELECT skill_name, COUNT(*),
		       SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),
		       MAX(used_at)
		FROM skill_usage GROUP BY skill_name ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: list stats: %w", err)
	}
	defer rows.Close()

	var result []UsageStats
	for rows.Next() {
		var s UsageStats
		var lastUsed sql.NullInt64
		if err := rows.Scan(&s.SkillName, &s.TotalUses, &s.SuccessCount, &s.FailureCount, &lastUsed); err != nil {
			continue
		}
		if lastUsed.Valid {
			s.LastUsed = lastUsed.Int64
		}
		if s.TotalUses > 0 {
			s.SuccessRate = float64(s.SuccessCount) / float64(s.TotalUses)
		}
		result = append(result, s)
	}
	return result, nil
}

// LogGenesis records that a skill was auto-generated.
func (t *Tracker) LogGenesis(skillName, source, sessionKey, category, description string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, err := t.db.ExecContext(context.Background(),
		`INSERT INTO skill_genesis_log (skill_name, source, session_key, created_at, category, description)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		skillName, source, sessionKey, time.Now().UnixMilli(), category, description,
	)
	return err
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
			// Fetch recent errors for context.
			stats, _ := t.Stats(s.SkillName)
			if stats != nil {
				s.RecentErrors = stats.RecentErrors
			}
			candidates = append(candidates, s)
		}
	}
	return candidates, nil
}

// Close closes the database connection.
func (t *Tracker) Close() error {
	if t.db != nil {
		return t.db.Close()
	}
	return nil
}
