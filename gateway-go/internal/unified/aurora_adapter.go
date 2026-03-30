package unified

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
)

// NewAuroraStore creates an aurora.Store backed by the unified database.
// The returned store uses the same underlying *sql.DB as the unified store,
// so all aurora operations (context items, messages, summaries, compaction)
// operate on the unified DB.
func (s *Store) NewAuroraStore() (*aurora.Store, error) {
	return aurora.NewStoreFromDB(s.db, s.logger)
}

// NewAuroraStoreWithLogger creates an aurora.Store with a custom logger.
func (s *Store) NewAuroraStoreWithLogger(logger *slog.Logger) (*aurora.Store, error) {
	return aurora.NewStoreFromDB(s.db, logger)
}
