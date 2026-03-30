// store_meta.go — User model, dreaming log, and metadata operations.
package memory

import (
	"context"
	"database/sql"
	"time"
)

// ── User Model ───────────────────────────────────────────────────────────────

// SetUserModel upserts a user model entry.
func (s *Store) SetUserModel(ctx context.Context, key, value string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_model (key, value, confidence, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, confidence = excluded.confidence, updated_at = excluded.updated_at`,
		key, value, confidence, now,
	)
	return err
}

// GetUserModelEntry returns a single user model entry by key.
// Returns nil if the key does not exist.
func (s *Store) GetUserModelEntry(ctx context.Context, key string) (*UserModelEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var e UserModelEntry
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT key, value, confidence, updated_at FROM user_model WHERE key = ?`, key,
	).Scan(&e.Key, &e.Value, &e.Confidence, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &e, nil
}

// GetUserModel returns all user model entries.
func (s *Store) GetUserModel(ctx context.Context) ([]UserModelEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT key, value, confidence, updated_at FROM user_model ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []UserModelEntry
	for rows.Next() {
		var e UserModelEntry
		var updatedAt string
		if err := rows.Scan(&e.Key, &e.Value, &e.Confidence, &updatedAt); err != nil {
			return nil, err
		}
		e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Dreaming Log ─────────────────────────────────────────────────────────────

// InsertDreamingLog records a dreaming cycle.
func (s *Store) InsertDreamingLog(ctx context.Context, entry DreamingLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dreaming_log (ran_at, facts_verified, facts_merged, facts_expired, facts_pruned, patterns_extracted, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.RanAt.UTC().Format(time.RFC3339),
		entry.FactsVerified, entry.FactsMerged, entry.FactsExpired, entry.FactsPruned,
		entry.PatternsExtracted, entry.DurationMs,
	)
	return err
}

// LastDreamingLog returns the most recent dreaming log entry, or nil if none exist.
func (s *Store) LastDreamingLog(ctx context.Context) (*DreamingLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var e DreamingLogEntry
	var ranAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, ran_at, facts_verified, facts_merged, facts_expired, facts_pruned, patterns_extracted, duration_ms
		 FROM dreaming_log ORDER BY id DESC LIMIT 1`,
	).Scan(&e.ID, &ranAt, &e.FactsVerified, &e.FactsMerged, &e.FactsExpired, &e.FactsPruned, &e.PatternsExtracted, &e.DurationMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.RanAt, _ = time.Parse(time.RFC3339, ranAt)
	return &e, nil
}

// ── Metadata ─────────────────────────────────────────────────────────────────

// GetMeta retrieves a metadata value.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetMeta upserts a metadata value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metadata (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}
